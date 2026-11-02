package main

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestSessionlessSSOParticipantIsNotMeetingHost locks in the Cloudflare Access
// fix: a verified but sessionless SSO caller is host only when its
// server-derived identity is the meeting creator. The injected admin bearer
// (sessionless, no SSO marker) must keep its host authority.
func TestSessionlessSSOParticipantIsNotMeetingHost(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	prevALB := albAuthEnabled
	albAuthEnabled = false
	defer func() { albAuthEnabled = prevALB }()

	appAuthMode = "token"
	appEnvironment = "production"
	appRoomID = "team-room"
	appBoardID = "team-board"
	meetingAccess = newMeetingAccessManager()

	host := requestAuthContext{Identity: "scott", SessionlessSSO: true, RoomID: appRoomID, BoardID: appBoardID}
	if _, err := meetingAccess.setup(host, meetingTypeStandup); err != nil {
		t.Fatalf("setup: %v", err)
	}
	participant := requestAuthContext{Identity: "alice", SessionlessSSO: true, RoomID: appRoomID, BoardID: appBoardID}
	if meetingAccess.isHost(participant) {
		t.Fatal("sessionless SSO participant must not be meeting host")
	}
	authorized, ok := meetingAccess.authorize(participant)
	if !ok || authorized.Role != meetingRoleParticipant {
		t.Fatalf("authorize(participant) = role %q ok %v, want participant", authorized.Role, ok)
	}
	if !meetingAccess.isHost(host) {
		t.Fatal("the SSO identity that created the meeting must remain host")
	}

	// The Traefik-injected admin bearer is deliberately sessionless and is NOT
	// an SSO context, so it must stay host.
	bearer := requestAuthContext{Identity: "api-token", RoomID: appRoomID, BoardID: appBoardID}
	if !meetingAccess.isHost(bearer) {
		t.Fatal("injected admin bearer must remain host")
	}
}

// TestCFAccessContextMarksSessionlessSSO asserts the credential-kind marker the
// meeting layer depends on is actually set on a verified Cloudflare Access ctx.
func TestCFAccessContextMarksSessionlessSSO(t *testing.T) {
	const kid = "cf-sso-marker-kid"
	key := withCFAccessTestKey(t, kid)
	token := makeCFToken(t, key, map[string]any{"alg": "RS256", "kid": kid}, map[string]any{
		"email": "alice@example.com",
		"sub":   "abc",
		"aud":   []string{"test-aud-123"},
		"iss":   "https://team.cloudflareaccess.com",
		"exp":   int64(1 << 62),
	})
	ctx, ok := cfAccessContext(newCFRequestWithCookie(token))
	if !ok {
		t.Fatal("valid CF token was not accepted")
	}
	if !ctx.SessionlessSSO {
		t.Fatal("CF Access context must be marked SessionlessSSO")
	}
	if ctx.Role != "" {
		t.Fatalf("CF context role = %q, want unset (role is decided by meeting action)", ctx.Role)
	}
}

// TestJoinRejectsHostAndDuplicateIdentity locks in the token-mode join fix: a
// participant cannot choose the host's identity (case variant included) or an
// identity already in use, because that string is the LiveKit/speaker label the
// voice host gate authorizes.
func TestJoinRejectsHostAndDuplicateIdentity(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	apiToken = "host-token"
	appAuthMode = "token"
	appEnvironment = "production"
	appRoomID = "team-room"
	appBoardID = "team-board"
	authStore = newWebAuthStore(time.Hour)
	meetingAccess = newMeetingAccessManager()
	sharedBoard = newKanbanBoard()

	hostCookie := createTestSessionCookie(t, "host-token", "scott")
	setupRec := httptest.NewRecorder()
	setupReq := httptest.NewRequest(http.MethodPost, "/meeting/setup", strings.NewReader(`{"meeting_type":"standup"}`))
	setupReq.AddCookie(hostCookie)
	setupMeetingHandler(setupRec, setupReq)
	if setupRec.Code != http.StatusOK {
		t.Fatalf("setup status = %d, body = %s", setupRec.Code, setupRec.Body.String())
	}
	var setupResponse struct {
		Meeting meetingAccessSnapshot `json:"meeting"`
	}
	if err := json.Unmarshal(setupRec.Body.Bytes(), &setupResponse); err != nil {
		t.Fatal(err)
	}
	code := setupResponse.Meeting.JoinCode

	join := func(identity string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/meeting/join", strings.NewReader(`{"join_code":"`+code+`","identity":"`+identity+`"}`))
		rec := httptest.NewRecorder()
		joinMeetingHandler(rec, req)
		return rec.Code
	}

	for _, identity := range []string{"scott", "SCOTT"} {
		if status := join(identity); status != http.StatusForbidden {
			t.Fatalf("join as host identity %q status = %d, want 403", identity, status)
		}
	}
	if status := join("sarah"); status != http.StatusOK {
		t.Fatalf("normal participant join status = %d, want 200", status)
	}
	if status := join("SARAH"); status != http.StatusForbidden {
		t.Fatalf("duplicate identity join status = %d, want 403", status)
	}
	if meetingAccess.voiceSpeakerHasHostAccess("SCOTT") {
		t.Fatal("a case-variant of the host identity must not pass the voice host gate")
	}
}

// TestChatTurnAuthorizesAgainstAuthenticatedPrincipal reproduces the confused
// deputy: the host was the last ASR speaker, the room is quiet, and a
// participant types. The chat turn must authorize as the participant, not as
// the stale speaker, while the transcript label keeps its old fallback.
func TestChatTurnAuthorizesAgainstAuthenticatedPrincipal(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	appAuthMode = "token"
	appEnvironment = "production"
	appRoomID = "team-room"
	appBoardID = "team-board"
	meetingAccess = newMeetingAccessManager()
	sharedBoard = newKanbanBoard()

	hostSession := webSession{ID: "host-session", Identity: "scott", RoomID: appRoomID, BoardID: appBoardID}
	snapshot, err := meetingAccess.setup(requestAuthContext{
		SessionID: hostSession.ID,
		Identity:  hostSession.Identity,
		RoomID:    appRoomID,
		BoardID:   appBoardID,
	}, meetingTypeStandup)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	participantSession := webSession{ID: "participant-session", Identity: "sarah", RoomID: appRoomID, BoardID: appBoardID}
	if _, err := meetingAccess.joinSessionWithCode(participantSession, snapshot.JoinCode, meetingRoleParticipant); err != nil {
		t.Fatalf("participant join: %v", err)
	}

	app := newNovaSonicApp(sharedBoard)
	// Host was the last (and only) ASR speaker, long enough ago that the short
	// audio grace window has expired.
	app.speakerMu.Lock()
	app.lastUserSpeaker = "scott"
	app.lastUserSpeakerAt = time.Now().Add(-time.Minute)
	app.activeSpeakers = nil
	app.speakerMu.Unlock()
	if got := app.toolAuthorizationSpeakerLabel(); got != "" {
		t.Fatalf("stale audio speaker label = %q, want empty (deny-safe)", got)
	}
	if !activeMeetingRequiresAuthenticatedHostForVoiceTool("end_meeting", app.toolAuthorizationSpeakerLabel(), "") {
		t.Fatal("a quiet room with a stale host speaker must not authorize a host-only tool")
	}

	// A participant's chat turn is authorized as that participant.
	added := app.beginChatTurn("sarah")
	if label := app.toolAuthorizationSpeakerLabel(); label != "sarah" {
		t.Fatalf("chat principal = %q, want sarah", label)
	}
	if !activeMeetingRequiresAuthenticatedHostForVoiceTool("end_meeting", app.toolAuthorizationSpeakerLabel(), "") {
		t.Fatal("participant chat after a host utterance must not inherit host authority")
	}
	app.abortChatTurn("sarah", added)

	// The host's own chat turn still works.
	added = app.beginChatTurn("scott")
	if activeMeetingRequiresAuthenticatedHostForVoiceTool("end_meeting", app.toolAuthorizationSpeakerLabel(), "") {
		t.Fatal("host chat turn must be authorized as host")
	}
	app.abortChatTurn("scott", added)

	// The transcript/presence label keeps its historical last-speaker fallback.
	if got := app.currentOrLastSpeakerLabel(); got != "scott" {
		t.Fatalf("currentOrLastSpeakerLabel = %q, want scott", got)
	}
}

// TestOverlappingChatTurnsDoNotShareHostAuthority covers a participant's chat
// command still in flight when the host sends another chat message: the
// participant's tool call must not be authorized as the host.
func TestOverlappingChatTurnsDoNotShareHostAuthority(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	appAuthMode = "token"
	appEnvironment = "production"
	appRoomID = "team-room"
	appBoardID = "team-board"
	meetingAccess = newMeetingAccessManager()
	sharedBoard = newKanbanBoard()

	snapshot, err := meetingAccess.setup(requestAuthContext{
		SessionID: "host-session",
		Identity:  "scott",
		RoomID:    appRoomID,
		BoardID:   appBoardID,
	}, meetingTypeStandup)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	participant := webSession{ID: "participant-session", Identity: "sarah", RoomID: appRoomID, BoardID: appBoardID}
	if _, err := meetingAccess.joinSessionWithCode(participant, snapshot.JoinCode, meetingRoleParticipant); err != nil {
		t.Fatalf("participant join: %v", err)
	}

	app := newNovaSonicApp(sharedBoard)
	app.beginChatTurn("sarah")
	app.beginChatTurn("scott")
	if !activeMeetingRequiresAuthenticatedHostForVoiceTool("end_meeting", app.toolAuthorizationSpeakerLabel(), "") {
		t.Fatalf("overlapping chat turns (label %q) must not authorize a host-only tool", app.toolAuthorizationSpeakerLabel())
	}

	// A spoken turn by the host while the participant's chat turn is in flight
	// is just as ambiguous.
	app2 := newNovaSonicApp(sharedBoard)
	app2.beginChatTurn("sarah")
	app2.noteSpokenTurnDuringChat("scott")
	if !activeMeetingRequiresAuthenticatedHostForVoiceTool("end_meeting", app2.toolAuthorizationSpeakerLabel(), "") {
		t.Fatal("a host utterance during a participant chat turn must not authorize a host-only tool")
	}

	// Once the participant's turn has expired, the host's own chat is honored.
	app.speakerMu.Lock()
	app.chatTurnPrincipals["sarah"] = time.Now().Add(-2 * chatTurnPrincipalTTL)
	app.speakerMu.Unlock()
	if activeMeetingRequiresAuthenticatedHostForVoiceTool("end_meeting", app.toolAuthorizationSpeakerLabel(), "") {
		t.Fatal("host chat turn must be authorized as host once the participant turn expired")
	}

	// A participant's spoken command still awaiting its tool call when the host
	// starts a chat turn keeps the label mixed.
	app4 := newNovaSonicApp(sharedBoard)
	app4.rememberUserSpeaker("sarah")
	app4.beginChatTurn("scott")
	if !activeMeetingRequiresAuthenticatedHostForVoiceTool("end_meeting", app4.toolAuthorizationSpeakerLabel(), "") {
		t.Fatalf("host chat during an in-flight participant utterance (label %q) must not authorize a host-only tool", app4.toolAuthorizationSpeakerLabel())
	}

	// A failed send undoes only the entry it added.
	app3 := newNovaSonicApp(sharedBoard)
	app3.beginChatTurn("sarah")
	app3.abortChatTurn("sarah", app3.beginChatTurn("sarah"))
	if got := app3.toolAuthorizationSpeakerLabel(); got != "sarah" {
		t.Fatalf("label after aborted repeat send = %q, want sarah", got)
	}
}

// TestExpiredSpeakerDoesNotUseHostOnlyRoomFallback covers an ALB OIDC meeting
// where stateless participants are not in the session map, so the map holds
// only the host. A participant's delayed tool call, after their speaker label
// expired, must not fall through to the host-only-room fallback.
func TestExpiredSpeakerDoesNotUseHostOnlyRoomFallback(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	prevALB := albAuthEnabled
	albAuthEnabled = true
	defer func() { albAuthEnabled = prevALB }()

	appAuthMode = "token"
	appEnvironment = "production"
	appRoomID = "team-room"
	appBoardID = "team-board"
	meetingAccess = newMeetingAccessManager()
	sharedBoard = newKanbanBoard()

	hostCtx := requestAuthContext{Identity: "scott", RoomID: appRoomID, BoardID: appBoardID}
	if _, err := meetingAccess.setup(hostCtx, meetingTypeStandup); err != nil {
		t.Fatalf("setup: %v", err)
	}

	app := newNovaSonicApp(sharedBoard)
	// Before anyone speaks, the legacy fallback still lets the sole host act.
	if principal := app.toolAuthorizationPrincipal(); principal.Expired || !principal.hasHostAccess() {
		t.Fatalf("no speaker yet: principal = %+v, want legacy host fallback", principal)
	}

	// alice (a stateless participant) spoke, and her tool call arrives after
	// the grace window with no active speakers.
	app.speakerMu.Lock()
	app.lastUserSpeaker = "alice"
	app.lastUserSpeakerAt = time.Now().Add(-time.Minute)
	app.activeSpeakers = nil
	app.speakerMu.Unlock()

	principal := app.toolAuthorizationPrincipal()
	if !principal.Expired || principal.Label != "" {
		t.Fatalf("stale speaker principal = %+v, want expired with empty label", principal)
	}
	if principal.hasHostAccess() {
		t.Fatal("an expired speaker must not get host access from the host-only-room fallback")
	}
	if !voiceToolRequiresHost("end_meeting", "", principal.hasHostAccess) {
		t.Fatal("a delayed participant end_meeting must be denied")
	}

	// The same applies once a chat principal has expired.
	chatApp := newNovaSonicApp(sharedBoard)
	chatApp.beginChatTurn("alice")
	chatApp.speakerMu.Lock()
	chatApp.chatTurnPrincipals["alice"] = time.Now().Add(-2 * chatTurnPrincipalTTL)
	chatApp.speakerMu.Unlock()
	if principal := chatApp.toolAuthorizationPrincipal(); !principal.Expired || principal.hasHostAccess() {
		t.Fatalf("expired chat principal = %+v, want denied", principal)
	}
}

// TestExpiredParticipantSessionReleasesIdentity covers a token-mode meeting
// that outlives a participant's session: once the session expires or is
// deleted, the participant can rejoin with the same identity, and the host
// identity stays reserved.
func TestExpiredParticipantSessionReleasesIdentity(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()
	prevStore := authStore
	defer func() { authStore = prevStore }()

	appAuthMode = "token"
	appEnvironment = "production"
	appRoomID = "team-room"
	appBoardID = "team-board"
	authStore = newWebAuthStore(defaultSessionTTL)
	meetingAccess = newMeetingAccessManager()
	sharedBoard = newKanbanBoard()

	snapshot, err := meetingAccess.setup(requestAuthContext{
		SessionID: "host-session",
		Identity:  "scott",
		RoomID:    appRoomID,
		BoardID:   appBoardID,
	}, meetingTypeStandup)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	first, err := authStore.create("sarah")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := meetingAccess.joinSessionWithCode(first, snapshot.JoinCode, meetingRoleParticipant); err != nil {
		t.Fatalf("first join: %v", err)
	}

	// While the first session is live, the identity is reserved.
	dup, err := authStore.create("sarah")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := meetingAccess.joinSessionWithCode(dup, snapshot.JoinCode, meetingRoleParticipant); err != errIdentityInUse {
		t.Fatalf("duplicate live join err = %v, want errIdentityInUse", err)
	}
	authStore.delete(dup.ID)

	// The first session expires.
	authStore.mu.Lock()
	expiring := authStore.sessions[first.ID]
	expiring.ExpiresAt = time.Now().UTC().Add(-time.Second)
	authStore.sessions[first.ID] = expiring
	authStore.mu.Unlock()

	again, err := authStore.create("sarah")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := meetingAccess.joinSessionWithCode(again, snapshot.JoinCode, meetingRoleParticipant); err != nil {
		t.Fatalf("rejoin after session expiry: %v", err)
	}

	// Signing out (DELETE /auth/session) releases the identity as well.
	authStore.delete(again.ID)
	afterSignOut, err := authStore.create("sarah")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := meetingAccess.joinSessionWithCode(afterSignOut, snapshot.JoinCode, meetingRoleParticipant); err != nil {
		t.Fatalf("rejoin after sign-out: %v", err)
	}

	// The host identity is still refused.
	impostor, err := authStore.create("scott")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := meetingAccess.joinSessionWithCode(impostor, snapshot.JoinCode, meetingRoleParticipant); err != errIdentityInUse {
		t.Fatalf("host identity join err = %v, want errIdentityInUse", err)
	}
}

// TestConfirmationBoundToRequester locks in the pending-confirmation binding:
// another participant cannot confirm or cancel someone else's staged action,
// while the requester and a meeting host still can.
func TestConfirmationBoundToRequester(t *testing.T) {
	board := newKanbanBoard()
	createResult, _, err := board.ApplyToolCall("create_ticket", `{"title":"Bound action","notes":"n","status":"Backlog"}`)
	if err != nil {
		t.Fatalf("create_ticket: %v", err)
	}
	card := createResult["card"].(kanbanCard)

	stage := func() string {
		t.Helper()
		res, _, err := board.ApplyToolCallWithMeta("set_priority", `{"card_id":"`+card.ID+`","priority":"High"}`, toolCallMeta{
			Source:    "ui",
			Requester: "alice",
		})
		if err != nil {
			t.Fatalf("stage set_priority: %v", err)
		}
		id, _ := res["confirmation_id"].(string)
		if id == "" {
			t.Fatalf("no confirmation staged: %#v", res)
		}
		return id
	}

	id := stage()
	res, _, err := board.ApplyToolCallWithMeta("confirm_action", `{"confirmation_id":"`+id+`"}`, toolCallMeta{
		Source:    "ui",
		Requester: "bob",
	})
	if err != nil {
		t.Fatalf("bob confirm_action: %v", err)
	}
	if asBool(res["ok"]) {
		t.Fatalf("bob confirmed alice's action: %#v", res)
	}
	if len(board.SnapshotState().PendingConfirmations) != 1 {
		t.Fatal("a denied confirmation must leave the pending action intact")
	}

	res, _, err = board.ApplyToolCallWithMeta("confirm_action", `{"confirmation_id":"`+id+`"}`, toolCallMeta{
		Source:    "ui",
		Requester: "alice",
	})
	if err != nil {
		t.Fatalf("alice confirm_action: %v", err)
	}
	if !asBool(res["confirmed"]) {
		t.Fatalf("requester could not confirm their own action: %#v", res)
	}

	// A host may confirm anyone's pending action.
	hostID := stage()
	res, _, err = board.ApplyToolCallWithMeta("confirm_action", `{"confirmation_id":"`+hostID+`"}`, toolCallMeta{
		Source:          "ui",
		Requester:       "scott",
		RequesterIsHost: true,
	})
	if err != nil {
		t.Fatalf("host confirm_action: %v", err)
	}
	if !asBool(res["confirmed"]) {
		t.Fatalf("host could not confirm a participant's action: %#v", res)
	}

	// Cancel is bound the same way.
	cancelID := stage()
	res, _, err = board.ApplyToolCallWithMeta("cancel_confirmation", `{"confirmation_id":"`+cancelID+`"}`, toolCallMeta{
		Source:    "ui",
		Requester: "bob",
	})
	if err != nil {
		t.Fatalf("bob cancel_confirmation: %v", err)
	}
	if asBool(res["cancelled"]) {
		t.Fatalf("bob cancelled alice's action: %#v", res)
	}
	if len(board.SnapshotState().PendingConfirmations) != 1 {
		t.Fatal("a denied cancellation must leave the pending action intact")
	}
}

// TestStoryPointsRejectNonFinite guards against board-wide JSON poisoning via
// NaN/Inf story points or custom-field numbers.
func TestStoryPointsRejectNonFinite(t *testing.T) {
	for _, value := range []any{"NaN", "+Inf", "-Inf", "nan", math.NaN(), math.Inf(1), math.Inf(-1)} {
		if parsed, ok := asFloat64(value); ok {
			t.Fatalf("asFloat64(%v) = %v, true; want rejected", value, parsed)
		}
	}

	board := newKanbanBoard()
	createResult, _, err := board.ApplyToolCall("create_ticket", `{"title":"Estimable","notes":"n","status":"Backlog"}`)
	if err != nil {
		t.Fatalf("create_ticket: %v", err)
	}
	card := createResult["card"].(kanbanCard)
	if _, _, err := board.ApplyToolCall("set_story_points", `{"card_id":"`+card.ID+`","points":"NaN"}`); err == nil {
		t.Fatal("set_story_points accepted NaN")
	}
	if _, err := json.Marshal(board.SnapshotState()); err != nil {
		t.Fatalf("board state is no longer JSON-encodable: %v", err)
	}
}

// TestAssignTicketToAgentRequiresHostDuringMeeting pins the host gate on the
// agent dispatch that can publish a GitHub PR review.
func TestAssignTicketToAgentRequiresHostDuringMeeting(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	appAuthMode = "token"
	appEnvironment = "production"
	appRoomID = "team-room"
	appBoardID = "team-board"
	meetingAccess = newMeetingAccessManager()
	sharedBoard = newKanbanBoard()

	hostSession := webSession{ID: "host-session", Identity: "scott", RoomID: appRoomID, BoardID: appBoardID}
	snapshot, err := meetingAccess.setup(requestAuthContext{
		SessionID: hostSession.ID,
		Identity:  hostSession.Identity,
		RoomID:    appRoomID,
		BoardID:   appBoardID,
	}, meetingTypeStandup)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	participantSession := webSession{ID: "participant-session", Identity: "sarah", RoomID: appRoomID, BoardID: appBoardID}
	if _, err := meetingAccess.joinSessionWithCode(participantSession, snapshot.JoinCode, meetingRoleParticipant); err != nil {
		t.Fatalf("participant join: %v", err)
	}

	participantCtx := requestAuthContext{SessionID: participantSession.ID, Identity: "sarah", RoomID: appRoomID, BoardID: appBoardID}
	hostCtx := requestAuthContext{SessionID: hostSession.ID, Identity: "scott", RoomID: appRoomID, BoardID: appBoardID}
	if kanbanCommandAllowed(participantCtx, "assign_ticket_to_agent") {
		t.Fatal("participant must not dispatch an agent (and its PR review write) during a meeting")
	}
	if !kanbanCommandAllowed(hostCtx, "assign_ticket_to_agent") {
		t.Fatal("host must be able to dispatch an agent during a meeting")
	}
}
