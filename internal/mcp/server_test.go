package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/somoore/auto-bot/internal/board"
)

func newTestServer(t *testing.T) (*Server, *InMemoryBoardAdapter) {
	t.Helper()
	adapter := NewInMemoryBoardAdapter()
	adapter.SeedCards("default", "default", []board.Card{
		{ID: "card-100", Title: "Wire MCP server", Status: board.StatusInProgress, Tags: []string{"sprint-2"}},
		{ID: "card-101", Title: "Document tools", Status: board.StatusBacklog, Tags: []string{"docs"}, Assignee: &board.Actor{Kind: board.ActorKindAgent, ID: "swe-1", AgentProfile: "swe-1"}},
	})
	deps := ToolDeps{
		Board:        adapter,
		TenantID:     "default",
		BoardID:      "default",
		DefaultActor: "mcp",
	}
	return NewServer(BuildTools(deps)), adapter
}

func decodeResp(t *testing.T, raw []byte) Response {
	t.Helper()
	var resp Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode response: %v\nraw: %s", err, raw)
	}
	return resp
}

func roundtrip(t *testing.T, s *Server, req Request) Response {
	t.Helper()
	r := s.HandleRequest(context.Background(), req)
	if r == nil {
		t.Fatalf("nil response for method %s", req.Method)
	}
	// JSON-marshal-and-back to exercise the wire shape, not just the struct.
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	return decodeResp(t, b)
}

func TestInitializeReturnsHandshake(t *testing.T) {
	s, _ := newTestServer(t)
	resp := roundtrip(t, s, Request{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "initialize"})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var result InitializeResult
	mustRemarshal(t, resp.Result, &result)
	if result.ServerInfo.Name != ServerName {
		t.Errorf("server name = %q, want %q", result.ServerInfo.Name, ServerName)
	}
	if result.ServerInfo.Version != ServerVersion {
		t.Errorf("server version = %q, want %q", result.ServerInfo.Version, ServerVersion)
	}
	if result.ProtocolVersion != ProtocolVersion {
		t.Errorf("protocol version = %q, want %q", result.ProtocolVersion, ProtocolVersion)
	}
	if len(result.Tools) != 5 {
		t.Fatalf("initialize advertised %d tools, want 5", len(result.Tools))
	}
}

func TestToolsListEnumeratesFive(t *testing.T) {
	s, _ := newTestServer(t)
	resp := roundtrip(t, s, Request{JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: "tools/list"})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var result ToolsListResult
	mustRemarshal(t, resp.Result, &result)
	want := []string{"board.list_cards", "board.get_card", "card.create", "card.update", "card.comment"}
	if len(result.Tools) != len(want) {
		t.Fatalf("tools/list returned %d tools, want %d", len(result.Tools), len(want))
	}
	for i, w := range want {
		if result.Tools[i].Name != w {
			t.Errorf("tools[%d] = %q, want %q", i, result.Tools[i].Name, w)
		}
		if result.Tools[i].InputSchema == nil {
			t.Errorf("tools[%d] %q missing inputSchema", i, w)
		}
	}
}

func TestToolsCallListCards(t *testing.T) {
	s, _ := newTestServer(t)
	resp := roundtrip(t, s, Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`3`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"board.list_cards","arguments":{}}`),
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var result ToolCallResult
	mustRemarshal(t, resp.Result, &result)
	if result.IsError {
		t.Fatalf("isError=true: %s", result.Content[0].Text)
	}
	if len(result.Content) == 0 {
		t.Fatalf("no content blocks")
	}
	var payload struct {
		Cards []CardSummary `json:"cards"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("decode tool result: %v\nraw: %s", err, result.Content[0].Text)
	}
	if len(payload.Cards) != 2 {
		t.Fatalf("expected 2 seeded cards, got %d", len(payload.Cards))
	}
	if payload.Cards[0].ID != "card-100" {
		t.Errorf("first card id = %q, want card-100", payload.Cards[0].ID)
	}
}

func TestToolsCallListCardsAgentOnlyFilter(t *testing.T) {
	s, _ := newTestServer(t)
	resp := roundtrip(t, s, Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`30`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"board.list_cards","arguments":{"filter":{"agent_only":true}}}`),
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var result ToolCallResult
	mustRemarshal(t, resp.Result, &result)
	var payload struct {
		Cards []CardSummary `json:"cards"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Cards) != 1 {
		t.Fatalf("expected 1 agent-assigned card, got %d", len(payload.Cards))
	}
	if payload.Cards[0].ID != "card-101" {
		t.Errorf("agent-filtered card id = %q, want card-101", payload.Cards[0].ID)
	}
}

func TestToolsCallCreateCard(t *testing.T) {
	s, adapter := newTestServer(t)
	resp := roundtrip(t, s, Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`4`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"card.create","arguments":{"title":"Add MCP smoke test","description":"e2e via curl","status":"Backlog","tags":["sprint-2"]}}`),
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var result ToolCallResult
	mustRemarshal(t, resp.Result, &result)
	if result.IsError {
		t.Fatalf("isError=true: %s", result.Content[0].Text)
	}
	var payload struct {
		CardID string     `json:"card_id"`
		Card   board.Card `json:"card"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, result.Content[0].Text)
	}
	if payload.CardID == "" {
		t.Fatalf("card.create returned empty card_id")
	}
	if payload.Card.Title != "Add MCP smoke test" {
		t.Errorf("created title = %q, want %q", payload.Card.Title, "Add MCP smoke test")
	}
	if payload.Card.Notes != "e2e via curl" {
		t.Errorf("created notes = %q, want %q", payload.Card.Notes, "e2e via curl")
	}
	// Confirm the adapter actually stored it.
	cards, err := adapter.ListCards(context.Background(), "default", "default", CardFilter{})
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	if len(cards) != 3 {
		t.Fatalf("adapter has %d cards after create, want 3", len(cards))
	}
}

func TestToolsCallUnknownToolReturnsMethodNotFound(t *testing.T) {
	s, _ := newTestServer(t)
	resp := roundtrip(t, s, Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`5`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"card.detonate","arguments":{}}`),
	})
	if resp.Error == nil {
		t.Fatalf("expected JSON-RPC error, got result: %+v", resp.Result)
	}
	if resp.Error.Code != ErrCodeMethodNotFound {
		t.Errorf("error code = %d, want %d", resp.Error.Code, ErrCodeMethodNotFound)
	}
	if !strings.Contains(resp.Error.Message, "card.detonate") {
		t.Errorf("error message should mention the unknown tool name; got %q", resp.Error.Message)
	}
}

func TestToolsCallUnknownMethodReturnsMethodNotFound(t *testing.T) {
	s, _ := newTestServer(t)
	resp := roundtrip(t, s, Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`6`),
		Method:  "nonsense/method",
	})
	if resp.Error == nil || resp.Error.Code != ErrCodeMethodNotFound {
		t.Fatalf("expected -32601 for unknown method, got %+v", resp.Error)
	}
}

func TestUpdateCardPatchSemantics(t *testing.T) {
	s, _ := newTestServer(t)
	// Title change only — status untouched.
	resp := roundtrip(t, s, Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`7`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"card.update","arguments":{"card_id":"card-100","patch":{"title":"Renamed"}}}`),
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var result ToolCallResult
	mustRemarshal(t, resp.Result, &result)
	if result.IsError {
		t.Fatalf("isError=true: %s", result.Content[0].Text)
	}
	var payload struct {
		Card board.Card `json:"card"`
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Card.Title != "Renamed" {
		t.Errorf("title = %q, want Renamed", payload.Card.Title)
	}
	if payload.Card.Status != board.StatusInProgress {
		t.Errorf("status changed unexpectedly: %q", payload.Card.Status)
	}
}

func TestCommentAppendsToCard(t *testing.T) {
	s, adapter := newTestServer(t)
	resp := roundtrip(t, s, Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`8`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"card.comment","arguments":{"card_id":"card-100","body":"Looks good","as_actor":"swe-3"}}`),
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	card, err := adapter.GetCard(context.Background(), "default", "default", "card-100")
	if err != nil {
		t.Fatalf("GetCard: %v", err)
	}
	if len(card.Comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(card.Comments))
	}
	if card.Comments[0].Author != "swe-3" {
		t.Errorf("author = %q, want swe-3", card.Comments[0].Author)
	}
}

func TestHTTPMissingBearerReturns401(t *testing.T) {
	s, _ := newTestServer(t)
	s.AuthToken = "secret-token"
	ts := httptest.NewServer(s.HTTPHandler())
	defer ts.Close()

	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if resp.Header.Get("WWW-Authenticate") == "" {
		t.Errorf("missing WWW-Authenticate header on 401")
	}
}

func TestHTTPWithBearerSucceeds(t *testing.T) {
	s, _ := newTestServer(t)
	s.AuthToken = "secret-token"
	ts := httptest.NewServer(s.HTTPHandler())
	defer ts.Close()

	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var decoded Response
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Error != nil {
		t.Errorf("unexpected jsonrpc error: %+v", decoded.Error)
	}
}

func TestServeStdioEndToEnd(t *testing.T) {
	s, _ := newTestServer(t)
	in := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")
	var out bytes.Buffer
	if err := s.ServeStdio(context.Background(), in, &out); err != nil {
		t.Fatalf("ServeStdio: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 response lines, got %d:\n%s", len(lines), out.String())
	}
}

// mustRemarshal re-encodes v through JSON into target. Convenient when a
// response.Result is typed as `any` but we want to assert a concrete shape.
func mustRemarshal(t *testing.T, v any, target any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := json.Unmarshal(b, target); err != nil {
		t.Fatalf("unmarshal into %T: %v\nraw: %s", target, err, b)
	}
}
