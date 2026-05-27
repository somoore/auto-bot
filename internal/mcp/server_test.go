package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/somoore/auto-bot/internal/agent"
	"github.com/somoore/auto-bot/internal/board"
)

// fakeBoardClient is a minimal in-memory BoardClient used by the protocol
// tests. It mirrors what mocks.BoardClient does but lives inside the mcp
// package to avoid an internal/mcp ←→ internal/mocks import cycle.
type fakeBoardClient struct {
	mu     sync.Mutex
	cards  []board.Card
	idSeed int64
}

func newFakeBoardClient(cards []board.Card) *fakeBoardClient {
	f := &fakeBoardClient{}
	if cards != nil {
		f.cards = append(f.cards, cards...)
	}
	return f
}

func (f *fakeBoardClient) ListCards(_ context.Context, _ string, _ string, filter CardFilter) ([]board.Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]board.Card, 0, len(f.cards))
	for _, c := range f.cards {
		if filter.Status != "" && string(c.Status) != filter.Status {
			continue
		}
		if filter.AssigneeID != "" {
			if c.Assignee == nil || c.Assignee.ID != filter.AssigneeID {
				continue
			}
		}
		if filter.AgentOnly {
			if c.Assignee == nil || c.Assignee.Kind != board.ActorKindAgent {
				continue
			}
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (f *fakeBoardClient) GetCard(_ context.Context, _ string, _ string, cardID string) (board.Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.cards {
		if c.ID == cardID {
			return c, nil
		}
	}
	return board.Card{}, ErrCardNotFound
}

func (f *fakeBoardClient) CreateCard(_ context.Context, _ string, _ string, input CardCreate) (board.Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.idSeed++
	status := board.Status(input.Status)
	if strings.TrimSpace(string(status)) == "" {
		status = board.StatusBacklog
	}
	card := board.Card{
		ID:       fmt.Sprintf("fake-%010d", f.idSeed),
		Title:    input.Title,
		Notes:    input.Description,
		Status:   status,
		Tags:     append([]string{}, input.Tags...),
		Assignee: input.Assignee,
	}
	f.cards = append(f.cards, card)
	return card, nil
}

func (f *fakeBoardClient) UpdateCard(_ context.Context, _ string, _ string, cardID string, patch CardPatch) (board.Card, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.cards {
		if f.cards[i].ID != cardID {
			continue
		}
		c := &f.cards[i]
		if patch.Title != nil {
			c.Title = *patch.Title
		}
		if patch.Status != nil {
			c.Status = board.Status(*patch.Status)
		}
		if patch.Notes != nil {
			c.Notes = *patch.Notes
		}
		if patch.Assignee != nil {
			actor := *patch.Assignee
			c.Assignee = &actor
		}
		if patch.TagsSet {
			c.Tags = append([]string{}, patch.Tags...)
		}
		return *c, nil
	}
	return board.Card{}, ErrCardNotFound
}

func (f *fakeBoardClient) StartRun(_ context.Context, _ string, _ string, req RunStartRequest) (RunStartResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.cards {
		if c.ID != req.CardID {
			continue
		}
		profile := req.AgentProfile
		if profile == "" {
			profile = "project_manager"
		}
		return RunStartResult{
			RunID:        fmt.Sprintf("test-run-%d", time.Now().UnixNano()),
			Status:       "queued",
			AgentProfile: profile,
			CardID:       req.CardID,
		}, nil
	}
	return RunStartResult{}, ErrCardNotFound
}

func (f *fakeBoardClient) AddComment(_ context.Context, _ string, _ string, cardID, body, author string) (board.Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.cards {
		if f.cards[i].ID != cardID {
			continue
		}
		comment := board.Comment{
			ID:        fmt.Sprintf("cmt-%d", time.Now().UnixNano()),
			Body:      body,
			Author:    author,
			CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		}
		f.cards[i].Comments = append(f.cards[i].Comments, comment)
		return comment, nil
	}
	return board.Comment{}, ErrCardNotFound
}

func newTestServer(t *testing.T) (*Server, *fakeBoardClient) {
	t.Helper()
	client := newFakeBoardClient([]board.Card{
		{ID: "card-100", Title: "Wire MCP server", Status: board.StatusInProgress, Tags: []string{"sprint-2"}},
		{ID: "card-101", Title: "Document tools", Status: board.StatusBacklog, Tags: []string{"docs"}, Assignee: &board.Actor{Kind: board.ActorKindAgent, ID: "swe-1", AgentProfile: "swe-1"}},
	})
	deps := ToolDeps{
		Board:        client,
		TenantID:     "default",
		BoardID:      "default",
		DefaultActor: "mcp",
	}
	return NewServer(BuildTools(deps)), client
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
	if len(result.Tools) != 6 {
		t.Fatalf("initialize advertised %d tools, want 6", len(result.Tools))
	}
}

func TestToolsListEnumeratesTools(t *testing.T) {
	s, _ := newTestServer(t)
	resp := roundtrip(t, s, Request{JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: "tools/list"})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var result ToolsListResult
	mustRemarshal(t, resp.Result, &result)
	want := []string{"board.list_cards", "board.get_card", "card.create", "card.update", "card.comment", "runs.start"}
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
	s, client := newTestServer(t)
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
	cards, err := client.ListCards(context.Background(), "default", "default", CardFilter{})
	if err != nil {
		t.Fatalf("ListCards: %v", err)
	}
	if len(cards) != 3 {
		t.Fatalf("client has %d cards after create, want 3", len(cards))
	}
}

func TestToolsCallStartRun(t *testing.T) {
	s, _ := newTestServer(t)
	resp := roundtrip(t, s, Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`40`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"runs.start","arguments":{"card_id":"card-100","objective":"ship the demo","agent_profile":"swe-1"}}`),
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var result ToolCallResult
	mustRemarshal(t, resp.Result, &result)
	if result.IsError {
		t.Fatalf("isError=true: %s", result.Content[0].Text)
	}
	var payload RunStartResult
	if err := json.Unmarshal([]byte(result.Content[0].Text), &payload); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, result.Content[0].Text)
	}
	if payload.RunID == "" {
		t.Fatalf("runs.start returned empty run_id")
	}
	if payload.AgentProfile != "swe-1" {
		t.Errorf("agent_profile = %q, want %q", payload.AgentProfile, "swe-1")
	}
	if payload.CardID != "card-100" {
		t.Errorf("card_id = %q, want %q", payload.CardID, "card-100")
	}
}

func TestToolsCallStartRunRejectsMissingObjective(t *testing.T) {
	s, _ := newTestServer(t)
	resp := roundtrip(t, s, Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`41`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"runs.start","arguments":{"card_id":"card-100"}}`),
	})
	if resp.Error != nil {
		t.Fatalf("unexpected transport error: %+v", resp.Error)
	}
	var result ToolCallResult
	mustRemarshal(t, resp.Result, &result)
	if !result.IsError {
		t.Fatalf("expected isError=true for missing objective")
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
	s, client := newTestServer(t)
	resp := roundtrip(t, s, Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`8`),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"card.comment","arguments":{"card_id":"card-100","body":"Looks good","as_actor":"swe-3"}}`),
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	card, err := client.GetCard(context.Background(), "default", "default", "card-100")
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

// TestHTTPBoardClientDispatchesCreate exercises the wire contract between
// HTTPBoardClient and a server impersonating cmd/server's
// /internal/tools/dispatch endpoint. The fake endpoint asserts the
// dispatcher field is set (so the risk-gate trigger fires) and that the
// MCP tool name + args round-trip cleanly.
func TestHTTPBoardClientDispatchesCreate(t *testing.T) {
	var captured dispatchEnvelope
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/tools/dispatch" {
			http.Error(w, "wrong path", http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"card": board.Card{ID: "card-999", Title: "from server", Status: board.StatusBacklog},
		})
	}))
	defer srv.Close()

	client := NewHTTPBoardClient(srv.URL, "test-token", "mcp")
	card, err := client.CreateCard(context.Background(), "default", "default", CardCreate{Title: "from server"})
	if err != nil {
		t.Fatalf("CreateCard: %v", err)
	}
	if card.ID != "card-999" {
		t.Errorf("card id = %q, want card-999", card.ID)
	}
	if captured.Tool != "card.create" {
		t.Errorf("tool = %q, want card.create", captured.Tool)
	}
	if captured.Dispatcher != "mcp" {
		t.Errorf("dispatcher = %q, want mcp (required to fire risk gate)", captured.Dispatcher)
	}
}

// TestToolsCallMapsRunNotFoundToInvalidParams is the SE-1 F6 regression
// test. Before the fix, every tool error was wrapped into the
// IsError-styled ToolCallResult envelope; MCP clients could not
// distinguish "the run id you passed does not exist" (which is a
// caller-supplied bad parameter) from a generic internal failure.
//
// The fix detects agent.ErrRunNotFound / agent.ErrRunQuestionNotFound in
// the tool error chain (via errors.Is so wrapped chains like
// "load run %s: %w" still map) and surfaces JSON-RPC -32602 (Invalid
// Params). Other errors continue to land in IsError. This test
// registers a custom tool whose handler returns each sentinel and
// asserts the wire response carries the -32602 code.
func TestToolsCallMapsRunNotFoundToInvalidParams(t *testing.T) {
	s := NewServer([]Tool{
		{
			Name:        "test.lookup_run",
			Description: "Always returns agent.ErrRunNotFound to exercise SE-1 F6 mapping.",
			InputSchema: map[string]any{"type": "object"},
			Handler: func(_ context.Context, _ json.RawMessage) (any, error) {
				return nil, fmt.Errorf("lookup_run %s: %w", "run-missing", agent.ErrRunNotFound)
			},
		},
		{
			Name:        "test.lookup_run_question",
			Description: "Always returns agent.ErrRunQuestionNotFound to exercise SE-1 F6 mapping.",
			InputSchema: map[string]any{"type": "object"},
			Handler: func(_ context.Context, _ json.RawMessage) (any, error) {
				return nil, fmt.Errorf("lookup_question %s: %w", "q-missing", agent.ErrRunQuestionNotFound)
			},
		},
		{
			Name:        "test.generic_error",
			Description: "Returns a generic error to confirm the default IsError path still applies.",
			InputSchema: map[string]any{"type": "object"},
			Handler: func(_ context.Context, _ json.RawMessage) (any, error) {
				return nil, errors.New("something else broke")
			},
		},
	})

	t.Run("ErrRunNotFound maps to -32602", func(t *testing.T) {
		resp := roundtrip(t, s, Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`100`),
			Method:  "tools/call",
			Params:  json.RawMessage(`{"name":"test.lookup_run","arguments":{}}`),
		})
		if resp.Error == nil {
			t.Fatalf("expected JSON-RPC error, got result: %+v", resp.Result)
		}
		if resp.Error.Code != ErrCodeInvalidParams {
			t.Fatalf("error code = %d, want %d (InvalidParams)", resp.Error.Code, ErrCodeInvalidParams)
		}
		if !strings.Contains(resp.Error.Message, "run not found") {
			t.Errorf("error message = %q, want it to mention 'run not found'", resp.Error.Message)
		}
	})

	t.Run("ErrRunQuestionNotFound maps to -32602", func(t *testing.T) {
		resp := roundtrip(t, s, Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`101`),
			Method:  "tools/call",
			Params:  json.RawMessage(`{"name":"test.lookup_run_question","arguments":{}}`),
		})
		if resp.Error == nil {
			t.Fatalf("expected JSON-RPC error, got result: %+v", resp.Result)
		}
		if resp.Error.Code != ErrCodeInvalidParams {
			t.Fatalf("error code = %d, want %d (InvalidParams)", resp.Error.Code, ErrCodeInvalidParams)
		}
		if !strings.Contains(resp.Error.Message, "run question not found") {
			t.Errorf("error message = %q, want it to mention 'run question not found'", resp.Error.Message)
		}
	})

	t.Run("generic error preserves IsError envelope", func(t *testing.T) {
		resp := roundtrip(t, s, Request{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`102`),
			Method:  "tools/call",
			Params:  json.RawMessage(`{"name":"test.generic_error","arguments":{}}`),
		})
		if resp.Error != nil {
			t.Fatalf("generic error must not be promoted to a JSON-RPC error code; got %+v", resp.Error)
		}
		var result ToolCallResult
		mustRemarshal(t, resp.Result, &result)
		if !result.IsError {
			t.Fatalf("expected IsError envelope for generic error; got result %+v", result)
		}
	})
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
