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
// mutates sharedBoard via the canonical ApplyToolCall path (so ActionLedger
// + risk gates fire alongside).
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

	// And confirm the ActionLedger / mutation history recorded the call —
	// the recordMutation path runs inside ApplyToolCallWithMeta, so a
	// non-empty mutation history is proof we routed through the canonical
	// path rather than mutating state directly.
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
