package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/somoore/auto-bot/internal/standup"
)

// TestInternalStandupAgendaRejectsMissingBearer asserts the agenda endpoint
// is gated by the same APP_API_TOKEN check as the rest of /internal/*.
func TestInternalStandupAgendaRejectsMissingBearer(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	apiToken = "agenda-secret"
	appAuthMode = "token"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()

	req := httptest.NewRequest(http.MethodGet, "/internal/standup/agenda", nil)
	rec := httptest.NewRecorder()
	internalStandupAgendaHandler(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("WWW-Authenticate"); got == "" {
		t.Errorf("missing WWW-Authenticate header on 401")
	}
}

// TestInternalStandupAgendaHappyPath covers the happy path: with a valid
// token the handler returns a standup.Agenda envelope with the
// tenant_id / board_id / generated_at scaffolding. The in-memory board
// has no persistent store but the AgendaBuilder degrades gracefully
// (returns an empty agenda with summary "No items today…").
func TestInternalStandupAgendaHappyPath(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	apiToken = "agenda-secret"
	appAuthMode = "token"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()

	req := httptest.NewRequest(http.MethodGet, "/internal/standup/agenda", nil)
	req.Header.Set("Authorization", "Bearer agenda-secret")
	rec := httptest.NewRecorder()
	internalStandupAgendaHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var agenda standup.Agenda
	if err := json.Unmarshal(rec.Body.Bytes(), &agenda); err != nil {
		t.Fatalf("decode agenda: %v; raw = %s", err, rec.Body.String())
	}
	if agenda.TenantID == "" || agenda.BoardID == "" || agenda.GeneratedAt == "" {
		t.Errorf("agenda envelope missing scaffolding: %+v", agenda)
	}
}

// TestInternalStandupAgendaShapeRoundTrip sanity-checks that the Go-side
// JSON tags on standup.Agenda match what the React translateAgenda helper
// keys off. If a tag drifts the React layer breaks silently — this test
// catches that.
func TestInternalStandupAgendaShapeRoundTrip(t *testing.T) {
	raw, err := json.Marshal(standup.Agenda{TenantID: "default", BoardID: "default", Window: "24h0m0s"})
	if err != nil {
		t.Fatalf("marshal Agenda: %v", err)
	}
	got := string(raw)
	for _, want := range []string{`"tenant_id":"default"`, `"board_id":"default"`, `"window":"24h0m0s"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("Agenda JSON missing %q; got %s", want, got)
		}
	}
}

// TestMeetingReportByIDRejectsMissingBearer asserts /meetings/{id} is
// gated the same as the rest of the meeting surface.
func TestMeetingReportByIDRejectsMissingBearer(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	apiToken = "report-secret"
	appAuthMode = "token"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()

	req := httptest.NewRequest(http.MethodGet, "/meetings/abc123", nil)
	rec := httptest.NewRecorder()
	meetingReportByIDHandler(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", rec.Code, rec.Body.String())
	}
}

// TestMeetingReportByIDMissingReturns404 confirms an unknown meeting id
// surfaces as a 404 so the React fallback knows to render sample data.
func TestMeetingReportByIDMissingReturns404(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	apiToken = "report-secret"
	appAuthMode = "token"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()

	req := httptest.NewRequest(http.MethodGet, "/meetings/does-not-exist", nil)
	req.Header.Set("Authorization", "Bearer report-secret")
	rec := httptest.NewRecorder()
	meetingReportByIDHandler(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
}

// TestDevSeedRunQuestionRefusesOutsideLocal asserts the seed endpoint is a
// no-op (404) when APP_ENV is not "local". Production callers must not be
// able to manufacture Runs via this surface.
func TestDevSeedRunQuestionRefusesOutsideLocal(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	apiToken = "seed-secret"
	appAuthMode = "token"
	appEnvironment = "production"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()

	body := []byte(`{"card_id":"card-1","prompt":"choose"}`)
	req := httptest.NewRequest(http.MethodPost, "/internal/dev/seed-run-question", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer seed-secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	devSeedRunQuestionHandler(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 in non-local env; body = %s", rec.Code, rec.Body.String())
	}
}

// TestDevSeedRunQuestionRejectsMissingBearer asserts the seed endpoint
// still requires auth even in local mode.
func TestDevSeedRunQuestionRejectsMissingBearer(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	apiToken = "seed-secret"
	appAuthMode = "token"
	appEnvironment = "local"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()

	body := []byte(`{"card_id":"card-1","prompt":"choose"}`)
	req := httptest.NewRequest(http.MethodPost, "/internal/dev/seed-run-question", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	devSeedRunQuestionHandler(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", rec.Code, rec.Body.String())
	}
}

// TestDevSeedRunQuestionRequiresPersistentStore covers the case where the
// in-memory kanbanBoard has no agent.RunStore — the handler should refuse
// rather than panic. Mirrors the agenda 503 path.
func TestDevSeedRunQuestionRequiresPersistentStore(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	apiToken = "seed-secret"
	appAuthMode = "token"
	appEnvironment = "local"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()

	body := []byte(`{"card_id":"card-1","prompt":"choose"}`)
	req := httptest.NewRequest(http.MethodPost, "/internal/dev/seed-run-question", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer seed-secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	devSeedRunQuestionHandler(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}
}
