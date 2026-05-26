package main

import (
	"bytes"
	"encoding/json"
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
