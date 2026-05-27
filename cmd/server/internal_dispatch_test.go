package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestInternalDispatchRejectsMissingBearer asserts /internal/tools/dispatch
// requires the same Bearer token as the rest of cmd/server. Anonymous
// callers must not reach ApplyToolCallWithMeta.
func TestInternalDispatchRejectsMissingBearer(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	apiToken = "internal-dispatch-secret"
	appAuthMode = "token"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()

	body := []byte(`{"tool":"card.create","args":{"title":"unauth"},"dispatcher":"mcp"}`)
	req := httptest.NewRequest(http.MethodPost, "/internal/tools/dispatch", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	internalToolsDispatchHandler(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("WWW-Authenticate"); got == "" {
		t.Errorf("missing WWW-Authenticate on 401")
	}
}

// TestInternalDispatchCardCreateRoutesThroughApplyToolCall asserts that a
// valid Bearer token admits the request and that card.create actually
// mutates sharedBoard via the canonical ApplyToolCall path (so the audit
// log + risk gates fire alongside).
func TestInternalDispatchCardCreateRoutesThroughApplyToolCall(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	apiToken = "internal-dispatch-secret"
	appAuthMode = "token"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()

	body := []byte(`{
		"tool":"card.create",
		"args":{"title":"Wired via MCP","description":"S2.1 smoke","status":"Backlog","tags":["mcp"]},
		"dispatcher":"mcp"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/internal/tools/dispatch", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer internal-dispatch-secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	internalToolsDispatchHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		CardID string     `json:"card_id"`
		Card   kanbanCard `json:"card"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, rec.Body.String())
	}
	if payload.CardID == "" {
		t.Fatalf("dispatch did not return card_id; body = %s", rec.Body.String())
	}
	if payload.Card.Title != "Wired via MCP" {
		t.Errorf("card title = %q, want Wired via MCP", payload.Card.Title)
	}

	// Confirm the mutation landed in the canonical board state.
	snap := sharedBoard.SnapshotState()
	var found bool
	for _, c := range snap.Cards {
		if c.ID == payload.CardID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("card %s not present in sharedBoard after dispatch", payload.CardID)
	}

	// And confirm the mutation history (boardMutationRecord) and audit log
	// (action_replay_events) recorded the call — the recordMutation path
	// runs inside ApplyToolCallWithMeta, so a non-empty mutation history is
	// proof we routed through the canonical path rather than mutating state
	// directly.
	if len(snap.RecentMutations) == 0 {
		t.Errorf("RecentMutations is empty; ApplyToolCallWithMeta path was not exercised")
	} else {
		latest := snap.RecentMutations[len(snap.RecentMutations)-1]
		if latest.ToolName != "create_ticket" {
			t.Errorf("latest mutation tool = %q, want create_ticket", latest.ToolName)
		}
		if latest.Source != "mcp" {
			t.Errorf("latest mutation source = %q, want mcp", latest.Source)
		}
	}
}

// TestInternalDispatchCardCommentRoutesThroughApplyToolCall covers the
// card.comment → add_comment translation including the as_actor override.
func TestInternalDispatchCardCommentRoutesThroughApplyToolCall(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	apiToken = "internal-dispatch-secret"
	appAuthMode = "token"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()

	// Seed a card via the dispatch path so test setup mirrors prod flow.
	createBody := []byte(`{"tool":"card.create","args":{"title":"Holder"},"dispatcher":"mcp"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/internal/tools/dispatch", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer internal-dispatch-secret")
	createRec := httptest.NewRecorder()
	internalToolsDispatchHandler(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("seed create status = %d", createRec.Code)
	}
	var createPayload struct {
		CardID string `json:"card_id"`
	}
	_ = json.Unmarshal(createRec.Body.Bytes(), &createPayload)

	commentBody := []byte(`{"tool":"card.comment","args":{"card_id":"` + createPayload.CardID + `","body":"hello from mcp","as_actor":"swe-1"},"dispatcher":"mcp"}`)
	req := httptest.NewRequest(http.MethodPost, "/internal/tools/dispatch", bytes.NewReader(commentBody))
	req.Header.Set("Authorization", "Bearer internal-dispatch-secret")
	rec := httptest.NewRecorder()
	internalToolsDispatchHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	snap := sharedBoard.SnapshotState()
	var foundComment bool
	for _, c := range snap.Cards {
		if c.ID != createPayload.CardID {
			continue
		}
		for _, comment := range c.Comments {
			if strings.Contains(comment.Body, "hello from mcp") {
				foundComment = true
			}
		}
	}
	if !foundComment {
		t.Fatalf("comment did not land on card %s", createPayload.CardID)
	}
}

// TestInternalDispatchRunsStartQueuesConfirmation asserts that an MCP-shaped
// runs.start payload routes through assign_ticket_to_agent (medium-risk) and
// returns a Trust Ceremony confirmation envelope rather than starting
// immediately. This is the canonical safety behavior — the MCP caller must
// either prompt the operator or wait for the pending action queue.
func TestInternalDispatchRunsStartQueuesConfirmation(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	apiToken = "internal-dispatch-secret"
	appAuthMode = "token"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()

	// Seed a card so runs.start has something to attach to.
	createBody := []byte(`{"tool":"card.create","args":{"title":"Ship MCP runs"},"dispatcher":"mcp"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/internal/tools/dispatch", bytes.NewReader(createBody))
	createReq.Header.Set("Authorization", "Bearer internal-dispatch-secret")
	createRec := httptest.NewRecorder()
	internalToolsDispatchHandler(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("seed create status = %d", createRec.Code)
	}
	var createPayload struct {
		CardID string `json:"card_id"`
	}
	_ = json.Unmarshal(createRec.Body.Bytes(), &createPayload)

	body := []byte(`{
		"tool":"runs.start",
		"args":{"card_id":"` + createPayload.CardID + `","objective":"finish the loop","agent_profile":"swe-1","requested_by":"mcp"},
		"dispatcher":"mcp"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/internal/tools/dispatch", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer internal-dispatch-secret")
	rec := httptest.NewRecorder()
	internalToolsDispatchHandler(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		RequiresConfirmation bool   `json:"requires_confirmation"`
		ConfirmationID       string `json:"confirmation_id"`
		RiskLevel            string `json:"risk_level"`
		ToolName             string `json:"tool_name"`
		CardID               string `json:"card_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, rec.Body.String())
	}
	if !payload.RequiresConfirmation {
		t.Fatalf("expected requires_confirmation=true; body = %s", rec.Body.String())
	}
	if payload.ConfirmationID == "" {
		t.Errorf("confirmation_id is empty")
	}
	if payload.ToolName != "assign_ticket_to_agent" {
		t.Errorf("tool_name = %q, want assign_ticket_to_agent", payload.ToolName)
	}
	if payload.CardID != createPayload.CardID {
		t.Errorf("card_id = %q, want %q", payload.CardID, createPayload.CardID)
	}
}

// TestInternalDispatchRunsStartRejectsMissingObjective covers input
// validation on the runs.start path.
func TestInternalDispatchRunsStartRejectsMissingObjective(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	apiToken = "internal-dispatch-secret"
	appAuthMode = "token"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()

	body := []byte(`{"tool":"runs.start","args":{"card_id":"missing-card"},"dispatcher":"mcp"}`)
	req := httptest.NewRequest(http.MethodPost, "/internal/tools/dispatch", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer internal-dispatch-secret")
	rec := httptest.NewRecorder()
	internalToolsDispatchHandler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
}

// TestInternalBoardCardsListAndGet asserts the read endpoints return the
// live snapshot and are gated by the same Bearer token.
func TestInternalBoardCardsListAndGet(t *testing.T) {
	restore := snapshotAuthGlobals()
	defer restore()

	apiToken = "internal-dispatch-secret"
	appAuthMode = "token"
	appRoomID = "kanban-meeting"
	appBoardID = "default"
	authStore = newWebAuthStore(time.Hour)
	sharedBoard = newKanbanBoard()

	// Unauthorized list — 401.
	listReq := httptest.NewRequest(http.MethodGet, "/internal/board/cards", nil)
	listRec := httptest.NewRecorder()
	internalBoardCardsHandler(listRec, listReq)
	if listRec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated list status = %d, want 401", listRec.Code)
	}

	// Authorized list — 200 and at least the seed cards.
	listReq2 := httptest.NewRequest(http.MethodGet, "/internal/board/cards", nil)
	listReq2.Header.Set("Authorization", "Bearer internal-dispatch-secret")
	listRec2 := httptest.NewRecorder()
	internalBoardCardsHandler(listRec2, listReq2)
	if listRec2.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body = %s", listRec2.Code, listRec2.Body.String())
	}
	var listPayload struct {
		Cards []kanbanCard `json:"cards"`
	}
	if err := json.Unmarshal(listRec2.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listPayload.Cards) == 0 {
		t.Fatalf("expected at least one seed card")
	}

	// GET by id — 200 with single card payload.
	first := listPayload.Cards[0].ID
	getReq := httptest.NewRequest(http.MethodGet, "/internal/board/cards/"+first, nil)
	getReq.Header.Set("Authorization", "Bearer internal-dispatch-secret")
	getRec := httptest.NewRecorder()
	internalBoardCardsHandler(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200; body = %s", getRec.Code, getRec.Body.String())
	}
	var getPayload struct {
		Card kanbanCard `json:"card"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &getPayload); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if getPayload.Card.ID != first {
		t.Errorf("card id = %q, want %q", getPayload.Card.ID, first)
	}

	// GET unknown id — 404.
	missingReq := httptest.NewRequest(http.MethodGet, "/internal/board/cards/does-not-exist", nil)
	missingReq.Header.Set("Authorization", "Bearer internal-dispatch-secret")
	missingRec := httptest.NewRecorder()
	internalBoardCardsHandler(missingRec, missingReq)
	if missingRec.Code != http.StatusNotFound {
		t.Errorf("missing status = %d, want 404", missingRec.Code)
	}
}
