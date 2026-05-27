package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/somoore/auto-bot/internal/intake"
)

// snapshotIntakeGlobals captures and restores the package-level intake
// state so tests can run in parallel-safe order without leaking writes
// into each other. Mirrors snapshotAuthGlobals for consistency.
func snapshotIntakeGlobals() func() {
	oldStore := intakeStore
	oldParser := intakeParser
	oldSecret := slackSigningSecret
	intakeStore = intake.NewMemoryStore(0)
	return func() {
		intakeStore = oldStore
		intakeParser = oldParser
		slackSigningSecret = oldSecret
	}
}

// TestIntakeStandupPostRequiresBearer asserts /intake/standup is gated
// by the same Bearer-token check as the rest of cmd/server. Anonymous
// callers must not reach intakeStore.
func TestIntakeStandupPostRequiresBearer(t *testing.T) {
	restoreAuth := snapshotAuthGlobals()
	defer restoreAuth()
	restoreIntake := snapshotIntakeGlobals()
	defer restoreIntake()

	apiToken = "intake-secret"
	appAuthMode = "token"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()

	body := []byte(`{"submitter":"daria","today":"x"}`)
	req := httptest.NewRequest(http.MethodPost, "/intake/standup", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	intakeStandupHandler(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("WWW-Authenticate"); got == "" {
		t.Errorf("missing WWW-Authenticate on 401")
	}
}

// TestIntakeStandupPostRoundtrip asserts a structured form post lands
// in intakeStore and is echoed back. Then a GET retrieves it via the
// 24h window.
func TestIntakeStandupPostRoundtrip(t *testing.T) {
	restoreAuth := snapshotAuthGlobals()
	defer restoreAuth()
	restoreIntake := snapshotIntakeGlobals()
	defer restoreIntake()

	apiToken = "intake-secret"
	appAuthMode = "token"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()

	postBody := []byte(`{
		"submitter":"daria",
		"today":"working on auth refactor",
		"blockers":[{"text":"need Linear creds"}],
		"source":"form"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/intake/standup", bytes.NewReader(postBody))
	req.Header.Set("Authorization", "Bearer intake-secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	intakeStandupHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("post status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var postResp struct {
		OK     bool          `json:"ok"`
		Intake intake.Intake `json:"intake"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &postResp); err != nil {
		t.Fatalf("decode post: %v\nraw: %s", err, rec.Body.String())
	}
	if !postResp.OK || postResp.Intake.Submitter != "daria" {
		t.Fatalf("post payload = %+v", postResp)
	}
	if postResp.Intake.Today != "working on auth refactor" {
		t.Errorf("today not preserved: %q", postResp.Intake.Today)
	}
	if len(postResp.Intake.Blockers) != 1 || postResp.Intake.Blockers[0].Text != "need Linear creds" {
		t.Errorf("blockers not preserved: %+v", postResp.Intake.Blockers)
	}
	if postResp.Intake.SubmittedAt.IsZero() {
		t.Errorf("SubmittedAt not stamped")
	}
	if postResp.Intake.TenantID == "" || postResp.Intake.BoardID == "" {
		t.Errorf("tenant/board not stitched from auth ctx: %+v", postResp.Intake)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/intake/standup", nil)
	getReq.Header.Set("Authorization", "Bearer intake-secret")
	getRec := httptest.NewRecorder()
	intakeStandupHandler(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200; body = %s", getRec.Code, getRec.Body.String())
	}
	var getResp struct {
		OK      bool            `json:"ok"`
		Intakes []intake.Intake `json:"intakes"`
		Window  string          `json:"window"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if !getResp.OK || len(getResp.Intakes) != 1 {
		t.Fatalf("get payload = %+v", getResp)
	}
	if getResp.Intakes[0].Submitter != "daria" {
		t.Errorf("recent intake submitter = %q", getResp.Intakes[0].Submitter)
	}
	if !strings.Contains(getResp.Window, "h") {
		t.Errorf("window = %q, want duration string", getResp.Window)
	}
}

// TestIntakeStandupPostParsesFreeFormAPI asserts that a Source=api free-
// text body is run through the HeuristicParser to fill yesterday/today/
// blockers structure.
func TestIntakeStandupPostParsesFreeFormAPI(t *testing.T) {
	restoreAuth := snapshotAuthGlobals()
	defer restoreAuth()
	restoreIntake := snapshotIntakeGlobals()
	defer restoreIntake()

	apiToken = "intake-secret"
	appAuthMode = "token"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()

	postBody := []byte(`{
		"submitter":"daria",
		"source":"api",
		"raw_text":"Yesterday: shipped IPv6\nToday: continue auth\nBlockers:\n- need Linear creds"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/intake/standup", bytes.NewReader(postBody))
	req.Header.Set("Authorization", "Bearer intake-secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	intakeStandupHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Intake intake.Intake `json:"intake"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Intake.Today != "continue auth" {
		t.Errorf("today not parsed from raw_text: %q", resp.Intake.Today)
	}
	if len(resp.Intake.Blockers) != 1 || resp.Intake.Blockers[0].Text != "need Linear creds" {
		t.Errorf("blockers not parsed from raw_text: %+v", resp.Intake.Blockers)
	}
}

// TestIntakeStandupPostRejectsEmpty asserts the empty-intake invariant
// is enforced: yesterday=today=blockers=raw_text all blank returns 400.
func TestIntakeStandupPostRejectsEmpty(t *testing.T) {
	restoreAuth := snapshotAuthGlobals()
	defer restoreAuth()
	restoreIntake := snapshotIntakeGlobals()
	defer restoreIntake()

	apiToken = "intake-secret"
	appAuthMode = "token"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()

	postBody := []byte(`{"submitter":"daria","source":"form"}`)
	req := httptest.NewRequest(http.MethodPost, "/intake/standup", bytes.NewReader(postBody))
	req.Header.Set("Authorization", "Bearer intake-secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	intakeStandupHandler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
}

// TestIntakeStandupGetIsTenantScoped asserts a GET only returns intakes
// matching the caller's tenant + board.
func TestIntakeStandupGetIsTenantScoped(t *testing.T) {
	restoreAuth := snapshotAuthGlobals()
	defer restoreAuth()
	restoreIntake := snapshotIntakeGlobals()
	defer restoreIntake()

	apiToken = "intake-secret"
	appAuthMode = "token"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()

	now := time.Now()
	// Pre-seed an intake on a different tenant so we can prove
	// tenant isolation.
	intakeStore.Put(intake.Intake{
		TenantID: "other-tenant", BoardID: "default", Submitter: "mallory",
		Today: "data exfil attempt", Source: intake.SourceAPI,
		SubmittedAt: now.Add(-time.Hour),
	})
	// And a same-tenant intake.
	intakeStore.Put(intake.Intake{
		TenantID: "default", BoardID: "default", Submitter: "daria",
		Today: "ok", Source: intake.SourceForm,
		SubmittedAt: now.Add(-30 * time.Minute),
	})

	req := httptest.NewRequest(http.MethodGet, "/intake/standup", nil)
	req.Header.Set("Authorization", "Bearer intake-secret")
	rec := httptest.NewRecorder()
	intakeStandupHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp struct {
		Intakes []intake.Intake `json:"intakes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Intakes) != 1 {
		t.Fatalf("expected 1 same-tenant intake, got %d: %+v", len(resp.Intakes), resp.Intakes)
	}
	if resp.Intakes[0].Submitter != "daria" {
		t.Errorf("cross-tenant leak: %+v", resp.Intakes[0])
	}
}

// TestIntakeStandupCreatesBlockerCards asserts the runIntakeFollowups
// wiring lands: each unanchored blocker becomes a new Blocked-column
// card with the submitter as the assignee, and the response echoes
// the created cards back.
func TestIntakeStandupCreatesBlockerCards(t *testing.T) {
	restoreAuth := snapshotAuthGlobals()
	defer restoreAuth()
	restoreIntake := snapshotIntakeGlobals()
	defer restoreIntake()

	apiToken = "intake-secret"
	appAuthMode = "token"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()

	cardsBefore := len(sharedBoard.SnapshotState().Cards)

	postBody := []byte(`{
		"submitter":"daria",
		"today":"working on auth",
		"blockers":[{"text":"need Linear creds"}],
		"source":"form"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/intake/standup", bytes.NewReader(postBody))
	req.Header.Set("Authorization", "Bearer intake-secret")
	req.Header.Set("Content-Type", "application/json")
	// X-Participant-Identity flips the bearer-token path into a self-
	// assigning identity, which matches the intake submitter and
	// therefore takes the SecArch-002 skip-confirmation branch.
	req.Header.Set("X-Participant-Identity", "daria")
	rec := httptest.NewRecorder()
	intakeStandupHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Created []kanbanCard `json:"created"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Created) != 1 {
		t.Fatalf("expected 1 created card, got %d: %+v", len(resp.Created), resp.Created)
	}
	created := resp.Created[0]
	if created.Status != kanbanStatusBlocked {
		t.Errorf("created card status = %q, want Blocked", created.Status)
	}
	if created.Title != "need Linear creds" {
		t.Errorf("created card title = %q", created.Title)
	}
	if !containsTag(created.Tags, "intake") || !containsTag(created.Tags, "blocker") {
		t.Errorf("created card missing intake/blocker tags: %+v", created.Tags)
	}
	// The response card now reflects the post-assignment state so the
	// React confirmation can render attribution without a board refresh.
	if created.Assignee == nil || created.Assignee.DisplayName != "daria" {
		t.Errorf("response card missing self-assignment: %+v", created.Assignee)
	}

	// The board snapshot should now have one more card.
	cardsAfter := sharedBoard.SnapshotState().Cards
	if len(cardsAfter) != cardsBefore+1 {
		t.Errorf("board cards = %d, want %d", len(cardsAfter), cardsBefore+1)
	}

	// The card must be assignable to the submitter — assignTicket with
	// account_id set short-circuits the Jira resolution path so this
	// works even without jiraSync.
	foundAssigned := false
	for _, c := range cardsAfter {
		if c.ID == created.ID && c.Assignee != nil && c.Assignee.DisplayName == "daria" {
			foundAssigned = true
		}
	}
	if !foundAssigned {
		t.Errorf("created card not assigned to submitter; cards = %+v", cardsAfter)
	}
}

// TestIntakeStandupCrossUserQueuesConfirmation asserts the SecArch-002
// branch: when the authenticated caller is NOT the submitter (EM files
// a standup on behalf of a teammate), the assign_ticket call queues a
// confirmation instead of mutating directly. The blocker card is
// created (low-risk) but the assignment waits for human approval.
func TestIntakeStandupCrossUserQueuesConfirmation(t *testing.T) {
	restoreAuth := snapshotAuthGlobals()
	defer restoreAuth()
	restoreIntake := snapshotIntakeGlobals()
	defer restoreIntake()

	apiToken = "intake-secret"
	appAuthMode = "token"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()

	// Caller is the EM; submitter is the teammate — the assignment to
	// the teammate must NOT skip confirmation.
	postBody := []byte(`{
		"submitter":"priya",
		"today":"reviewing the auth refactor",
		"blockers":[{"text":"need staging access"}],
		"source":"form"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/intake/standup", bytes.NewReader(postBody))
	req.Header.Set("Authorization", "Bearer intake-secret")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Participant-Identity", "daria")
	rec := httptest.NewRecorder()
	intakeStandupHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}

	// The blocker card was created (low-risk path runs immediately).
	state := sharedBoard.SnapshotState()
	var blockerCard *kanbanCard
	for i := range state.Cards {
		if state.Cards[i].Title == "need staging access" {
			blockerCard = &state.Cards[i]
		}
	}
	if blockerCard == nil {
		t.Fatalf("expected blocker card to be created")
	}
	// The assignee MUST still be nil — the assign_ticket call queued.
	if blockerCard.Assignee != nil {
		t.Errorf("cross-user assignment was applied without confirmation: %+v", blockerCard.Assignee)
	}
	// And a pending confirmation must exist on the board.
	if len(state.PendingConfirmations) == 0 {
		t.Errorf("expected a pending assign_ticket confirmation, found none")
	}
}

// TestIntakeStandupCommentsOnMentionedCards asserts that MentionedCards
// references that resolve to real cards get a thread comment carrying
// the intake snippet.
func TestIntakeStandupCommentsOnMentionedCards(t *testing.T) {
	restoreAuth := snapshotAuthGlobals()
	defer restoreAuth()
	restoreIntake := snapshotIntakeGlobals()
	defer restoreIntake()

	apiToken = "intake-secret"
	appAuthMode = "token"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()

	// card-001 is one of the seed cards in newKanbanBoard.
	existing := sharedBoard.SnapshotState().Cards
	if len(existing) == 0 {
		t.Fatalf("seed board produced 0 cards")
	}
	targetID := existing[0].ID

	postBody := []byte(fmt.Sprintf(`{
		"submitter":"daria",
		"today":"following up on %s",
		"mentioned_cards":["%s","does-not-exist"],
		"source":"form"
	}`, targetID, targetID))
	req := httptest.NewRequest(http.MethodPost, "/intake/standup", bytes.NewReader(postBody))
	req.Header.Set("Authorization", "Bearer intake-secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	intakeStandupHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Comments []postedIntakeComment `json:"comments"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Comments) != 1 {
		t.Fatalf("expected 1 comment on existing card; got %d: %+v", len(resp.Comments), resp.Comments)
	}
	if resp.Comments[0].CardID != targetID {
		t.Errorf("comment posted to wrong card: %+v", resp.Comments[0])
	}
	if !strings.Contains(resp.Comments[0].Body, "Async intake from daria") {
		t.Errorf("comment body missing attribution: %q", resp.Comments[0].Body)
	}
}

// TestIntakeFoldsIntoBoardSnapshot asserts intakes recorded in the
// intakeStore surface on kanbanBoardState.RecentIntakes when the board
// snapshot is taken (the broadcast path).
func TestIntakeFoldsIntoBoardSnapshot(t *testing.T) {
	restoreAuth := snapshotAuthGlobals()
	defer restoreAuth()
	restoreIntake := snapshotIntakeGlobals()
	defer restoreIntake()

	apiToken = "intake-secret"
	appAuthMode = "token"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()

	now := time.Now()
	intakeStore.Put(intake.Intake{
		TenantID:    sharedBoard.tenantID,
		BoardID:     sharedBoard.boardID,
		Submitter:   "daria",
		Today:       "auth refactor",
		Source:      intake.SourceForm,
		SubmittedAt: now.Add(-1 * time.Hour),
	})
	// Older than 24h — should NOT appear.
	intakeStore.Put(intake.Intake{
		TenantID:    sharedBoard.tenantID,
		BoardID:     sharedBoard.boardID,
		Submitter:   "stale",
		Today:       "old standup",
		Source:      intake.SourceForm,
		SubmittedAt: now.Add(-72 * time.Hour),
	})

	state := sharedBoard.SnapshotState()
	if len(state.RecentIntakes) != 1 {
		t.Fatalf("RecentIntakes = %d, want 1 (24h window): %+v",
			len(state.RecentIntakes), state.RecentIntakes)
	}
	if state.RecentIntakes[0].Submitter != "daria" {
		t.Errorf("wrong intake surfaced: %+v", state.RecentIntakes[0])
	}
}

func containsTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}

// TestIntakeSlackRejectsMissingSecret asserts that with
// slackSigningSecret unset, /intake/slack rejects every request.
func TestIntakeSlackRejectsMissingSecret(t *testing.T) {
	restoreAuth := snapshotAuthGlobals()
	defer restoreAuth()
	restoreIntake := snapshotIntakeGlobals()
	defer restoreIntake()

	apiToken = "intake-secret"
	appAuthMode = "token"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()
	slackSigningSecret = ""

	body := []byte(`user_name=daria&text=Yesterday%3A+shipped`)
	req := httptest.NewRequest(http.MethodPost, "/intake/slack", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", "1748272800")
	req.Header.Set("X-Slack-Signature", "v0=deadbeef")
	rec := httptest.NewRecorder()
	intakeSlackHandler(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", rec.Code, rec.Body.String())
	}
	if all := intakeStore.All(defaultTenantID, appBoardID); len(all) != 0 {
		t.Errorf("intake recorded despite missing secret: %+v", all)
	}
}

// TestIntakeSlackRejectsBadSignature asserts a Slack body signed with the
// wrong secret is rejected and never reaches intakeStore.
func TestIntakeSlackRejectsBadSignature(t *testing.T) {
	restoreAuth := snapshotAuthGlobals()
	defer restoreAuth()
	restoreIntake := snapshotIntakeGlobals()
	defer restoreIntake()

	apiToken = "intake-secret"
	appAuthMode = "token"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()
	slackSigningSecret = "real-secret"

	now := time.Now()
	ts := unixSecondsStr(now)
	body := []byte(`user_name=daria&text=test`)
	// Sign with the wrong secret.
	sig := intake.ComputeSlackSignature("attacker-secret", ts, body)

	req := httptest.NewRequest(http.MethodPost, "/intake/slack", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", sig)
	rec := httptest.NewRecorder()
	intakeSlackHandler(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", rec.Code, rec.Body.String())
	}
}

// TestIntakeSlackHappyPathFormEncoded asserts a valid Slack form POST
// (with the correct signature) parses into an Intake and is persisted.
func TestIntakeSlackHappyPathFormEncoded(t *testing.T) {
	restoreAuth := snapshotAuthGlobals()
	defer restoreAuth()
	restoreIntake := snapshotIntakeGlobals()
	defer restoreIntake()

	apiToken = "intake-secret"
	appAuthMode = "token"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()
	slackSigningSecret = "slack-secret"

	now := time.Now()
	ts := unixSecondsStr(now)
	body := []byte("user_name=daria&text=" + urlEncode(
		"Yesterday: shipped IPv6\nToday: continue auth\nBlockers: need Linear creds"))
	sig := intake.ComputeSlackSignature("slack-secret", ts, body)

	req := httptest.NewRequest(http.MethodPost, "/intake/slack", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", sig)
	rec := httptest.NewRecorder()
	intakeSlackHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Intake intake.Intake `json:"intake"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Intake.Submitter != "daria" {
		t.Errorf("submitter = %q", resp.Intake.Submitter)
	}
	if resp.Intake.Source != intake.SourceSlack {
		t.Errorf("source = %q, want slack", resp.Intake.Source)
	}
	if resp.Intake.Today != "continue auth" {
		t.Errorf("today = %q", resp.Intake.Today)
	}
	if len(resp.Intake.Blockers) != 1 || resp.Intake.Blockers[0].Text != "need Linear creds" {
		t.Errorf("blockers = %+v", resp.Intake.Blockers)
	}
	if got := intakeStore.All(defaultTenantID, appBoardID); len(got) != 1 {
		t.Errorf("intakeStore count = %d, want 1", len(got))
	}
}

// TestIntakeSlackRejectsStaleTimestamp asserts a >5 minute drift is
// rejected with 401 even when the signature is otherwise valid.
func TestIntakeSlackRejectsStaleTimestamp(t *testing.T) {
	restoreAuth := snapshotAuthGlobals()
	defer restoreAuth()
	restoreIntake := snapshotIntakeGlobals()
	defer restoreIntake()

	apiToken = "intake-secret"
	appAuthMode = "token"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()
	slackSigningSecret = "slack-secret"

	// 10 minutes ago — outside the SlackReplayWindow.
	stale := time.Now().Add(-10 * time.Minute)
	ts := unixSecondsStr(stale)
	body := []byte("user_name=daria&text=x")
	sig := intake.ComputeSlackSignature("slack-secret", ts, body)

	req := httptest.NewRequest(http.MethodPost, "/intake/slack", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", sig)
	rec := httptest.NewRecorder()
	intakeSlackHandler(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for stale timestamp; body = %s", rec.Code, rec.Body.String())
	}
}

func unixSecondsStr(t time.Time) string {
	const digits = "0123456789"
	n := t.Unix()
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%10]
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func urlEncode(s string) string {
	// Minimal URL encoding for the test body. Replaces only the bytes
	// that matter for our payloads. Avoid net/url here because we already
	// import it from the handler and want the test self-contained.
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ':
			b.WriteByte('+')
		case c == '\n':
			b.WriteString("%0A")
		case c == ':':
			b.WriteString("%3A")
		case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9'):
			b.WriteByte(c)
		case c == '-' || c == '.' || c == '_' || c == '~':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// TestIntakeStandupPostAcceptsPlainText asserts a text/plain body is
// treated as raw text under SourceAPI and run through the parser.
func TestIntakeStandupPostAcceptsPlainText(t *testing.T) {
	restoreAuth := snapshotAuthGlobals()
	defer restoreAuth()
	restoreIntake := snapshotIntakeGlobals()
	defer restoreIntake()

	apiToken = "intake-secret"
	appAuthMode = "token"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()

	body := []byte("Yesterday: shipped X\nToday: continue Y\nBlockers: none")
	req := httptest.NewRequest(http.MethodPost, "/intake/standup", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer intake-secret")
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("X-Participant-Identity", "daria")
	rec := httptest.NewRecorder()
	intakeStandupHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Intake intake.Intake `json:"intake"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Intake.Submitter != "daria" {
		t.Errorf("submitter not lifted from X-Participant-Identity: %q", resp.Intake.Submitter)
	}
	if resp.Intake.Yesterday != "shipped X" || resp.Intake.Today != "continue Y" {
		t.Errorf("plain text not parsed: %+v", resp.Intake)
	}
}
