package main

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHostSetupMeetingCreatesJoinCodeAndHostAccess(t *testing.T) {
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

	hostCookie := createTestSessionCookie(t, "host-token", "Scott_1")

	req := httptest.NewRequest(http.MethodPost, "/meeting/setup", strings.NewReader(`{"meeting_type":"standup"}`))
	req.AddCookie(hostCookie)
	rec := httptest.NewRecorder()
	setupMeetingHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setupMeetingHandler status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var response struct {
		OK      bool                  `json:"ok"`
		Meeting meetingAccessSnapshot `json:"meeting"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || !response.Meeting.Active {
		t.Fatalf("setup response = %+v", response)
	}
	if response.Meeting.JoinCode == "" || len(response.Meeting.JoinCode) != joinCodeLength {
		t.Fatalf("join code = %q, want %d character random code", response.Meeting.JoinCode, joinCodeLength)
	}
	if response.Meeting.MeetingType != meetingTypeStandup {
		t.Fatalf("meeting type = %q, want %q", response.Meeting.MeetingType, meetingTypeStandup)
	}

	authorizedReq := httptest.NewRequest(http.MethodGet, "/websocket?room_id=team-room&board_id=team-board", nil)
	authorizedReq.AddCookie(hostCookie)
	ctx, ok := authorizeRequest(authorizedReq)
	if !ok {
		t.Fatal("host session was not authorized after setup")
	}
	if ctx.Role != meetingRoleHost || ctx.MeetingType != meetingTypeStandup {
		t.Fatalf("auth context = %+v", ctx)
	}
}

func TestRandomJoinCodeMeetsEntropyRequirement(t *testing.T) {
	entropyBits := float64(joinCodeLength) * math.Log2(float64(len(joinCodeChars)))
	if entropyBits < 72 {
		t.Fatalf("join code entropy = %.1f bits, want at least 72", entropyBits)
	}
}

func TestParticipantMustJoinWithExactCodeBeforeRoomAccess(t *testing.T) {
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

	hostCookie := createTestSessionCookie(t, "host-token", "Scott_1")
	setupRec := httptest.NewRecorder()
	setupReq := httptest.NewRequest(http.MethodPost, "/meeting/setup", strings.NewReader(`{"meeting_type":"general meeting"}`))
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

	unjoinedSession, err := authStore.create("Unjoined")
	if err != nil {
		t.Fatal(err)
	}
	unjoinedReq := httptest.NewRequest(http.MethodGet, "/livekit-token?room_id=team-room&board_id=team-board", nil)
	unjoinedReq.AddCookie(&http.Cookie{Name: authCookieName, Value: unjoinedSession.ID})
	if _, ok := authorizeRequest(unjoinedReq); ok {
		t.Fatal("unjoined session was authorized while a join-code meeting is active")
	}

	wrongJoinReq := httptest.NewRequest(http.MethodPost, "/meeting/join", strings.NewReader(`{"join_code":"WRONG123","identity":"Sarah_1"}`))
	wrongJoinRec := httptest.NewRecorder()
	joinMeetingHandler(wrongJoinRec, wrongJoinReq)
	if wrongJoinRec.Code != http.StatusForbidden {
		t.Fatalf("wrong join status = %d, want 403", wrongJoinRec.Code)
	}

	displayCode := setupResponse.Meeting.JoinCode[:3] + "-" + setupResponse.Meeting.JoinCode[3:]
	joinBody := `{"join_code":"` + displayCode + `","identity":"Sarah_1"}`
	joinReq := httptest.NewRequest(http.MethodPost, "/meeting/join", strings.NewReader(joinBody))
	joinRec := httptest.NewRecorder()
	joinMeetingHandler(joinRec, joinReq)
	if joinRec.Code != http.StatusOK {
		t.Fatalf("joinMeetingHandler status = %d, body = %s", joinRec.Code, joinRec.Body.String())
	}
	cookies := joinRec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != authCookieName {
		t.Fatalf("join cookies = %#v", cookies)
	}

	participantReq := httptest.NewRequest(http.MethodGet, "/websocket?room_id=team-room&board_id=team-board", nil)
	participantReq.AddCookie(cookies[0])
	ctx, ok := authorizeRequest(participantReq)
	if !ok {
		t.Fatal("joined participant was not authorized")
	}
	if ctx.Role != meetingRoleParticipant || ctx.Identity != "Sarah_1" {
		t.Fatalf("participant auth context = %+v", ctx)
	}
}

func TestParticipantCannotRunHostOnlyKanbanCommands(t *testing.T) {
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

	hostCookie := createTestSessionCookie(t, "host-token", "Scott_1")
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

	joinReq := httptest.NewRequest(http.MethodPost, "/meeting/join", strings.NewReader(`{"join_code":"`+setupResponse.Meeting.JoinCode+`","identity":"Sarah_1"}`))
	joinRec := httptest.NewRecorder()
	joinMeetingHandler(joinRec, joinReq)
	if joinRec.Code != http.StatusOK {
		t.Fatalf("join status = %d, body = %s", joinRec.Code, joinRec.Body.String())
	}
	participantReq := httptest.NewRequest(http.MethodGet, "/websocket?room_id=team-room&board_id=team-board", nil)
	participantReq.AddCookie(joinRec.Result().Cookies()[0])
	participantCtx, ok := authorizeRequest(participantReq)
	if !ok {
		t.Fatal("participant was not authorized after joining")
	}

	hostReq := httptest.NewRequest(http.MethodGet, "/websocket?room_id=team-room&board_id=team-board", nil)
	hostReq.AddCookie(hostCookie)
	hostCtx, ok := authorizeRequest(hostReq)
	if !ok {
		t.Fatal("host was not authorized")
	}

	for _, toolName := range []string{"confirm_action", "cancel_confirmation", "resolve_jira_conflict", "undo_last_mutation", "switch_meeting_type", "end_meeting"} {
		if kanbanCommandAllowed(participantCtx, toolName) {
			t.Fatalf("participant was allowed to run host-only tool %q", toolName)
		}
		if !kanbanCommandAllowed(hostCtx, toolName) {
			t.Fatalf("host was blocked from host-only tool %q", toolName)
		}
	}
	if !kanbanCommandAllowed(participantCtx, "move_ticket") {
		t.Fatal("participant should still be allowed to run non-host-only board commands")
	}
}

func TestSwitchMeetingTypeEndpointRequiresHost(t *testing.T) {
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

	hostCookie := createTestSessionCookie(t, "host-token", "Scott_1")
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

	joinReq := httptest.NewRequest(http.MethodPost, "/meeting/join", strings.NewReader(`{"join_code":"`+setupResponse.Meeting.JoinCode+`","identity":"Sarah_1"}`))
	joinRec := httptest.NewRecorder()
	joinMeetingHandler(joinRec, joinReq)
	if joinRec.Code != http.StatusOK {
		t.Fatalf("join status = %d, body = %s", joinRec.Code, joinRec.Body.String())
	}
	participantCookie := joinRec.Result().Cookies()[0]

	participantSwitchReq := httptest.NewRequest(http.MethodPost, "/meeting/type", strings.NewReader(`{"meeting_type":"sprint_review"}`))
	participantSwitchReq.AddCookie(participantCookie)
	participantSwitchRec := httptest.NewRecorder()
	switchMeetingTypeHandler(participantSwitchRec, participantSwitchReq)
	if participantSwitchRec.Code != http.StatusForbidden {
		t.Fatalf("participant switch status = %d, want 403", participantSwitchRec.Code)
	}

	hostSwitchReq := httptest.NewRequest(http.MethodPost, "/meeting/type", strings.NewReader(`{"meeting_type":"sprint review"}`))
	hostSwitchReq.AddCookie(hostCookie)
	hostSwitchRec := httptest.NewRecorder()
	switchMeetingTypeHandler(hostSwitchRec, hostSwitchReq)
	if hostSwitchRec.Code != http.StatusOK {
		t.Fatalf("host switch status = %d, body = %s", hostSwitchRec.Code, hostSwitchRec.Body.String())
	}

	snapshot := meetingAccess.snapshot(false)
	if snapshot.MeetingType != meetingTypeSprintReview {
		t.Fatalf("meeting access type = %q, want sprint_review", snapshot.MeetingType)
	}
	state := sharedBoard.SnapshotState()
	if state.Meeting == nil || state.Meeting.Mode != scrumMeetingModeReview {
		t.Fatalf("board meeting = %#v, want sprint review mode", state.Meeting)
	}
}

func TestSwitchMeetingTypeToolUpdatesBoardState(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	meetingAccess = newMeetingAccessManager()
	board := newKanbanBoard()
	result, changed, err := board.ApplyToolCall("switch_meeting_type", `{"meeting_type":"1:1"}`)
	if err != nil {
		t.Fatalf("switch_meeting_type returned error: %v", err)
	}
	if !changed {
		t.Fatal("switch_meeting_type should mutate board meeting mode")
	}
	if result["meeting_type"] != meetingTypeOneOnOne {
		t.Fatalf("meeting_type result = %#v", result["meeting_type"])
	}
	state := board.SnapshotState()
	if state.Meeting == nil || state.Meeting.Mode != scrumMeetingModeOneOnOne {
		t.Fatalf("meeting state = %#v, want one_on_one", state.Meeting)
	}
}

func TestParticipantLeaveDoesNotEndMeeting(t *testing.T) {
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

	hostCookie := createTestSessionCookie(t, "host-token", "Scott_1")
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

	joinReq := httptest.NewRequest(http.MethodPost, "/meeting/join", strings.NewReader(`{"join_code":"`+setupResponse.Meeting.JoinCode+`","identity":"Sarah_1"}`))
	joinRec := httptest.NewRecorder()
	joinMeetingHandler(joinRec, joinReq)
	if joinRec.Code != http.StatusOK {
		t.Fatalf("join status = %d, body = %s", joinRec.Code, joinRec.Body.String())
	}
	participantCookie := joinRec.Result().Cookies()[0]

	leaveReq := httptest.NewRequest(http.MethodPost, "/meeting/leave", nil)
	leaveReq.AddCookie(participantCookie)
	leaveRec := httptest.NewRecorder()
	leaveMeetingHandler(leaveRec, leaveReq)
	if leaveRec.Code != http.StatusOK {
		t.Fatalf("participant leave status = %d, body = %s", leaveRec.Code, leaveRec.Body.String())
	}
	snapshot := meetingAccess.snapshot(false)
	if !snapshot.Active {
		t.Fatal("participant leave ended the meeting")
	}
	if len(snapshot.Participants) != 1 || snapshot.Participants[0].Role != meetingRoleHost {
		t.Fatalf("participants after leave = %#v, want only host", snapshot.Participants)
	}

	hostReq := httptest.NewRequest(http.MethodGet, "/livekit-token?room_id=team-room&board_id=team-board", nil)
	hostReq.AddCookie(hostCookie)
	if _, ok := authorizeRequest(hostReq); !ok {
		t.Fatal("host was not authorized after participant left")
	}
	participantReq := httptest.NewRequest(http.MethodGet, "/livekit-token?room_id=team-room&board_id=team-board", nil)
	participantReq.AddCookie(participantCookie)
	if _, ok := authorizeRequest(participantReq); ok {
		t.Fatal("participant remained authorized after leaving")
	}
}

func TestHostLeaveEndsMeetingAndRevokesParticipants(t *testing.T) {
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

	hostCookie := createTestSessionCookie(t, "host-token", "Scott_1")
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

	joinReq := httptest.NewRequest(http.MethodPost, "/meeting/join", strings.NewReader(`{"join_code":"`+setupResponse.Meeting.JoinCode+`","identity":"Sarah_1"}`))
	joinRec := httptest.NewRecorder()
	joinMeetingHandler(joinRec, joinReq)
	if joinRec.Code != http.StatusOK {
		t.Fatalf("join status = %d, body = %s", joinRec.Code, joinRec.Body.String())
	}
	participantCookie := joinRec.Result().Cookies()[0]

	leaveReq := httptest.NewRequest(http.MethodPost, "/meeting/leave", nil)
	leaveReq.AddCookie(hostCookie)
	leaveRec := httptest.NewRecorder()
	leaveMeetingHandler(leaveRec, leaveReq)
	if leaveRec.Code != http.StatusOK {
		t.Fatalf("host leave status = %d, body = %s", leaveRec.Code, leaveRec.Body.String())
	}
	var leaveResponse struct {
		Ended   bool                  `json:"ended"`
		Meeting meetingAccessSnapshot `json:"meeting"`
	}
	if err := json.Unmarshal(leaveRec.Body.Bytes(), &leaveResponse); err != nil {
		t.Fatal(err)
	}
	if !leaveResponse.Ended || leaveResponse.Meeting.Active {
		t.Fatalf("leave response = %+v, want ended inactive meeting", leaveResponse)
	}
	if meetingAccess.snapshot(false).Active {
		t.Fatal("host leave did not end meeting access")
	}

	secondLeaveReq := httptest.NewRequest(http.MethodPost, "/meeting/leave", nil)
	secondLeaveReq.AddCookie(hostCookie)
	secondLeaveRec := httptest.NewRecorder()
	leaveMeetingHandler(secondLeaveRec, secondLeaveReq)
	if secondLeaveRec.Code != http.StatusOK {
		t.Fatalf("idempotent host leave status = %d, body = %s", secondLeaveRec.Code, secondLeaveRec.Body.String())
	}
	var secondLeaveResponse struct {
		Ended bool `json:"ended"`
	}
	if err := json.Unmarshal(secondLeaveRec.Body.Bytes(), &secondLeaveResponse); err != nil {
		t.Fatal(err)
	}
	if !secondLeaveResponse.Ended {
		t.Fatalf("idempotent leave response = %+v, want ended", secondLeaveResponse)
	}

	state := sharedBoard.SnapshotState()
	if state.Meeting == nil || state.Meeting.Active || state.Meeting.EndedAt == "" {
		t.Fatalf("board meeting after host leave = %#v, want ended", state.Meeting)
	}

	participantReq := httptest.NewRequest(http.MethodGet, "/livekit-token?room_id=team-room&board_id=team-board", nil)
	participantReq.AddCookie(participantCookie)
	if _, ok := authorizeRequest(participantReq); ok {
		t.Fatal("participant remained authorized after host ended the meeting")
	}
}

func createTestSessionCookie(t *testing.T, token string, identity string) *http.Cookie {
	t.Helper()
	body, err := json.Marshal(createSessionRequest{
		Token:    token,
		Identity: identity,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/auth/session", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	createSessionHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create session status = %d, body = %s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("session cookies = %#v", cookies)
	}
	return cookies[0]
}
