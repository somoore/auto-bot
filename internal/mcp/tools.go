package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/somoore/auto-bot/internal/agent"
	"github.com/somoore/auto-bot/internal/board"
)

// ErrCardNotFound is returned by BoardClient.GetCard / UpdateCard when no
// card matches the supplied ID. Tool handlers translate this into a
// human-readable error surfaced to the MCP client.
var ErrCardNotFound = errors.New("mcp: card not found")

// BoardClient abstracts the board-state operations the five MCP tools need.
// The MCP package owns this interface so the protocol layer stays
// provider-neutral. In production, cmd/mcpd injects an HTTPBoardClient that
// fans out to cmd/server's /internal endpoints — every MCP-driven mutation
// then flows through cmd/server's ApplyToolCall, so ActionLedger, risk
// classification, and confirmation gates apply uniformly.
//
// All methods are scoped by (tenantID, boardID). Empty values mean
// "default" — the implementation normalizes them.
type BoardClient interface {
	ListCards(ctx context.Context, tenantID, boardID string, filter CardFilter) ([]board.Card, error)
	GetCard(ctx context.Context, tenantID, boardID, cardID string) (board.Card, error)
	CreateCard(ctx context.Context, tenantID, boardID string, input CardCreate) (board.Card, error)
	UpdateCard(ctx context.Context, tenantID, boardID, cardID string, patch CardPatch) (board.Card, error)
	AddComment(ctx context.Context, tenantID, boardID, cardID string, body, author string) (board.Comment, error)
	StartRun(ctx context.Context, tenantID, boardID string, req RunStartRequest) (RunStartResult, error)
}

// RunStartRequest is the input shape for runs.start. CardID and Objective are
// required. AgentProfile defaults to "project_manager" on the server side;
// the rest are optional fields the existing assign_ticket_to_agent tool
// accepts (repo, branch, pull_request_number are surfaced by GitHub PR-review
// runs).
type RunStartRequest struct {
	CardID            string `json:"card_id"`
	Objective         string `json:"objective"`
	AgentProfile      string `json:"agent_profile,omitempty"`
	RequestType       string `json:"request_type,omitempty"`
	RequestedBy       string `json:"requested_by,omitempty"`
	Repo              string `json:"repo,omitempty"`
	Branch            string `json:"branch,omitempty"`
	PullRequestNumber int    `json:"pull_request_number,omitempty"`
}

// RunStartResult is the response from runs.start. Two shapes are possible:
//
//  1. Immediate start: RunID is populated and the Run is now queued/running.
//     RequiresConfirmation is false.
//  2. Pending confirmation: medium-risk tools (assign_ticket_to_agent is
//     medium-risk) queue a pending action in the Trust Ceremony rather
//     than applying immediately. RunID is empty; RequiresConfirmation is
//     true and ConfirmationID/Prompt are populated. The MCP caller can
//     surface this to the operator (or block on the Trust queue).
type RunStartResult struct {
	RunID                string `json:"run_id,omitempty"`
	Status               string `json:"status,omitempty"`
	AgentProfile         string `json:"agent_profile,omitempty"`
	CardID               string `json:"card_id"`
	RequiresConfirmation bool   `json:"requires_confirmation,omitempty"`
	ConfirmationID       string `json:"confirmation_id,omitempty"`
	RiskLevel            string `json:"risk_level,omitempty"`
	Prompt               string `json:"prompt,omitempty"`
}

// CardFilter is the input shape for board.list_cards. Empty fields mean "no
// filter on this dimension". AgentOnly returns only cards whose assignee is
// an agent (any agent profile).
type CardFilter struct {
	Status     string `json:"status,omitempty"`
	AssigneeID string `json:"assignee_id,omitempty"`
	AgentOnly  bool   `json:"agent_only,omitempty"`
}

// CardCreate is the input shape for card.create. Description maps to
// Card.Notes on the canonical board side.
type CardCreate struct {
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Status      string         `json:"status,omitempty"`
	Assignee    *board.Actor   `json:"assignee,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// CardPatch is the input shape for card.update. Pointer fields distinguish
// "absent" from "explicit zero value". Tags follow the same rule via
// TagsSet — without it, `nil` could mean either "leave tags alone" or
// "clear the tag list".
type CardPatch struct {
	Title    *string      `json:"title,omitempty"`
	Status   *string      `json:"status,omitempty"`
	Assignee *board.Actor `json:"assignee,omitempty"`
	Notes    *string      `json:"notes,omitempty"`
	Tags     []string     `json:"tags,omitempty"`
	TagsSet  bool         `json:"-"`
}

// CardSummary is the slim card view returned by board.list_cards. It exposes
// only the fields agents actually need for navigation; full card details
// flow through board.get_card.
type CardSummary struct {
	ID       string       `json:"id"`
	Title    string       `json:"title"`
	Status   string       `json:"status"`
	Assignee *board.Actor `json:"assignee,omitempty"`
	Tags     []string     `json:"tags,omitempty"`
	RunID    string       `json:"run_id,omitempty"`
}

// CardDetail is the rich view returned by board.get_card. ActiveRun and
// Thread are populated only when present.
type CardDetail struct {
	Card      board.Card      `json:"card"`
	Thread    []board.Comment `json:"thread,omitempty"`
	ActiveRun *RunSummary     `json:"active_run,omitempty"`
}

// RunSummary is the slim Run shape attached to CardDetail. It mirrors the
// fields the live drawer reads; the full Run flows through a dedicated
// runs.get tool (lands later).
type RunSummary struct {
	RunID        string                `json:"run_id"`
	Status       agent.RunStatus       `json:"status"`
	AgentProfile string                `json:"agent_profile,omitempty"`
	Objective    string                `json:"objective,omitempty"`
	CurrentStep  string                `json:"current_step,omitempty"`
	WaitingOn    *agent.RunQuestionRef `json:"waiting_on,omitempty"`
	UpdatedAt    string                `json:"updated_at,omitempty"`
}

// CommentResult is the response from card.comment.
type CommentResult struct {
	CardID  string        `json:"card_id"`
	Comment board.Comment `json:"comment"`
}

// ToolDeps is the bundle of dependencies the tool handlers close over.
// Constructing once at server boot keeps the tool definitions pure (no
// hidden globals) and makes them trivial to test with a fake BoardClient
// and an in-memory RunStore.
type ToolDeps struct {
	Board       BoardClient
	RunStore    agent.RunStore
	Coordinator agent.RunCoordinator
	TenantID    string
	BoardID     string

	// DefaultActor is the identity tool calls run under when the request
	// payload does not override it (e.g. card.comment's as_actor field).
	// For S2.1 this is a single shared "mcp" actor; later sprints will
	// switch to per-token actor identity.
	DefaultActor string
}

// BuildTools returns the MCP tools wired against deps. The order matches
// the canonical list in the Sprint 2.0 spec (5 board tools), followed by
// the runs.start tool added in #59 to close the "MCP can't kick a Run"
// gap surfaced by the Operations Scribe.
func BuildTools(deps ToolDeps) []Tool {
	return []Tool{
		buildListCardsTool(deps),
		buildGetCardTool(deps),
		buildCreateCardTool(deps),
		buildUpdateCardTool(deps),
		buildCommentTool(deps),
		buildStartRunTool(deps),
	}
}

// ---- board.list_cards -----------------------------------------------------

func buildListCardsTool(deps ToolDeps) Tool {
	return Tool{
		Name:        "board.list_cards",
		Description: "List cards on the active board. Optional filters narrow by status, assignee, or agent-owned cards only.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"filter": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"status":      map[string]any{"type": "string"},
						"assignee_id": map[string]any{"type": "string"},
						"agent_only":  map[string]any{"type": "boolean"},
					},
				},
			},
		},
		Handler: func(ctx context.Context, params json.RawMessage) (any, error) {
			var args struct {
				Filter CardFilter `json:"filter"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return nil, fmt.Errorf("invalid params: %w", err)
				}
			}
			cards, err := deps.Board.ListCards(ctx, deps.TenantID, deps.BoardID, args.Filter)
			if err != nil {
				return nil, err
			}
			runByCard := map[string]string{}
			if deps.RunStore != nil {
				qs, qErr := deps.RunStore.ListOpenRunQuestions(ctx, deps.TenantID, deps.BoardID)
				if qErr == nil {
					for _, q := range qs {
						runByCard[q.CardID] = q.RunID
					}
				}
			}
			out := make([]CardSummary, 0, len(cards))
			for _, c := range cards {
				out = append(out, CardSummary{
					ID:       c.ID,
					Title:    c.Title,
					Status:   string(c.Status),
					Assignee: c.Assignee,
					Tags:     c.Tags,
					RunID:    runByCard[c.ID],
				})
			}
			return map[string]any{"cards": out}, nil
		},
	}
}

// ---- board.get_card -------------------------------------------------------

func buildGetCardTool(deps ToolDeps) Tool {
	return Tool{
		Name:        "board.get_card",
		Description: "Fetch one card by ID, including its recent comment thread and any active agent run summary.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"card_id"},
			"properties": map[string]any{
				"card_id": map[string]any{"type": "string"},
			},
		},
		Handler: func(ctx context.Context, params json.RawMessage) (any, error) {
			var args struct {
				CardID string `json:"card_id"`
			}
			if err := json.Unmarshal(params, &args); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}
			if strings.TrimSpace(args.CardID) == "" {
				return nil, fmt.Errorf("card_id is required")
			}
			card, err := deps.Board.GetCard(ctx, deps.TenantID, deps.BoardID, args.CardID)
			if err != nil {
				return nil, err
			}
			detail := CardDetail{Card: card, Thread: card.Comments}
			if deps.RunStore != nil {
				if run := findActiveRunForCard(ctx, deps.RunStore, deps.TenantID, deps.BoardID, card.ID); run != nil {
					detail.ActiveRun = run
				}
			}
			return detail, nil
		},
	}
}

// findActiveRunForCard scans open RunQuestions for the given card and, if
// any are present, loads the most recent Run. Returning a RunSummary keeps
// the tool response shape stable regardless of how many checkpoints a Run
// has accumulated.
func findActiveRunForCard(ctx context.Context, store agent.RunStore, tenantID, boardID, cardID string) *RunSummary {
	if store == nil {
		return nil
	}
	qs, err := store.ListOpenRunQuestions(ctx, tenantID, boardID)
	if err != nil || len(qs) == 0 {
		return nil
	}
	var newest *agent.RunQuestion
	for i := range qs {
		if qs[i].CardID != cardID {
			continue
		}
		if newest == nil || qs[i].AskedAt > newest.AskedAt {
			newest = &qs[i]
		}
	}
	if newest == nil {
		return nil
	}
	run, err := store.LoadRun(ctx, tenantID, boardID, newest.RunID)
	if err != nil {
		return nil
	}
	return &RunSummary{
		RunID:        run.RunID,
		Status:       run.Status,
		AgentProfile: run.AgentProfile,
		Objective:    run.Objective,
		CurrentStep:  run.CurrentStep,
		WaitingOn:    run.WaitingOn,
		UpdatedAt:    run.UpdatedAt,
	}
}

// ---- card.create ----------------------------------------------------------

func buildCreateCardTool(deps ToolDeps) Tool {
	return Tool{
		Name:        "card.create",
		Description: "Create a new card on the active board. Routes through cmd/server's ApplyToolCall path, so ActionLedger + risk gates apply (same as voice / UI callers).",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"title"},
			"properties": map[string]any{
				"title":       map[string]any{"type": "string"},
				"description": map[string]any{"type": "string"},
				"status":      map[string]any{"type": "string"},
				"assignee": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"kind":         map[string]any{"type": "string", "enum": []string{"human", "agent"}},
						"id":           map[string]any{"type": "string"},
						"displayName":  map[string]any{"type": "string"},
						"agentProfile": map[string]any{"type": "string"},
					},
				},
				"tags": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
		},
		Handler: func(ctx context.Context, params json.RawMessage) (any, error) {
			var input CardCreate
			if err := json.Unmarshal(params, &input); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}
			if strings.TrimSpace(input.Title) == "" {
				return nil, fmt.Errorf("title is required")
			}
			card, err := deps.Board.CreateCard(ctx, deps.TenantID, deps.BoardID, input)
			if err != nil {
				return nil, err
			}
			return map[string]any{"card_id": card.ID, "card": card}, nil
		},
	}
}

// ---- card.update ----------------------------------------------------------

func buildUpdateCardTool(deps ToolDeps) Tool {
	return Tool{
		Name:        "card.update",
		Description: "Patch one card. Pointer-typed fields distinguish 'no change' from 'set to empty'. Status changes pass through the canonical status vocabulary (Backlog / In Progress / Blocked / Done).",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"card_id", "patch"},
			"properties": map[string]any{
				"card_id": map[string]any{"type": "string"},
				"patch": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title":    map[string]any{"type": "string"},
						"status":   map[string]any{"type": "string"},
						"notes":    map[string]any{"type": "string"},
						"assignee": map[string]any{"type": "object"},
						"tags":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					},
				},
			},
		},
		Handler: func(ctx context.Context, params json.RawMessage) (any, error) {
			var envelope struct {
				CardID string          `json:"card_id"`
				Patch  json.RawMessage `json:"patch"`
			}
			if err := json.Unmarshal(params, &envelope); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}
			if strings.TrimSpace(envelope.CardID) == "" {
				return nil, fmt.Errorf("card_id is required")
			}
			var patch CardPatch
			if len(envelope.Patch) > 0 {
				if err := json.Unmarshal(envelope.Patch, &patch); err != nil {
					return nil, fmt.Errorf("invalid patch: %w", err)
				}
				var probe map[string]json.RawMessage
				_ = json.Unmarshal(envelope.Patch, &probe)
				if _, ok := probe["tags"]; ok {
					patch.TagsSet = true
				}
			}
			card, err := deps.Board.UpdateCard(ctx, deps.TenantID, deps.BoardID, envelope.CardID, patch)
			if err != nil {
				return nil, err
			}
			return map[string]any{"card": card}, nil
		},
	}
}

// ---- card.comment ---------------------------------------------------------

func buildCommentTool(deps ToolDeps) Tool {
	return Tool{
		Name:        "card.comment",
		Description: "Append a comment to a card's thread. as_actor overrides the default MCP actor for the duration of this call (per-token identities land in a later sprint).",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"card_id", "body"},
			"properties": map[string]any{
				"card_id":  map[string]any{"type": "string"},
				"body":     map[string]any{"type": "string"},
				"as_actor": map[string]any{"type": "string"},
			},
		},
		Handler: func(ctx context.Context, params json.RawMessage) (any, error) {
			var args struct {
				CardID  string `json:"card_id"`
				Body    string `json:"body"`
				AsActor string `json:"as_actor"`
			}
			if err := json.Unmarshal(params, &args); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}
			if strings.TrimSpace(args.CardID) == "" {
				return nil, fmt.Errorf("card_id is required")
			}
			if strings.TrimSpace(args.Body) == "" {
				return nil, fmt.Errorf("body is required")
			}
			author := args.AsActor
			if author == "" {
				author = deps.DefaultActor
			}
			comment, err := deps.Board.AddComment(ctx, deps.TenantID, deps.BoardID, args.CardID, args.Body, author)
			if err != nil {
				return nil, err
			}
			return CommentResult{CardID: args.CardID, Comment: comment}, nil
		},
	}
}

// ---- runs.start -----------------------------------------------------------

func buildStartRunTool(deps ToolDeps) Tool {
	return Tool{
		Name:        "runs.start",
		Description: "Start an agent run against an existing card. Routes through cmd/server's assign_ticket_to_agent so ActionLedger, risk gates, and the Run lifecycle apply uniformly with voice and UI callers.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"card_id", "objective"},
			"properties": map[string]any{
				"card_id":             map[string]any{"type": "string", "description": "Existing card to attach the Run to."},
				"objective":           map[string]any{"type": "string", "description": "Human-readable goal for the agent."},
				"agent_profile":       map[string]any{"type": "string", "description": "Specialist profile (e.g. project_manager, swe-1). Defaults to project_manager on the server."},
				"request_type":        map[string]any{"type": "string", "description": "Optional classification hint (e.g. code_review, refactor, auto)."},
				"requested_by":        map[string]any{"type": "string", "description": "Identity of the human (or proxy) initiating the Run."},
				"repo":                map[string]any{"type": "string", "description": "Optional repo for GitHub PR-review runs (e.g. owner/name)."},
				"branch":              map[string]any{"type": "string", "description": "Optional branch name for repo-scoped runs."},
				"pull_request_number": map[string]any{"type": "integer", "description": "Optional PR number for code-review runs."},
			},
		},
		Handler: func(ctx context.Context, params json.RawMessage) (any, error) {
			var input RunStartRequest
			if err := json.Unmarshal(params, &input); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}
			if strings.TrimSpace(input.CardID) == "" {
				return nil, fmt.Errorf("card_id is required")
			}
			if strings.TrimSpace(input.Objective) == "" {
				return nil, fmt.Errorf("objective is required")
			}
			if strings.TrimSpace(input.RequestedBy) == "" {
				input.RequestedBy = deps.DefaultActor
			}
			result, err := deps.Board.StartRun(ctx, deps.TenantID, deps.BoardID, input)
			if err != nil {
				return nil, err
			}
			return result, nil
		},
	}
}

// ---------------------------------------------------------------------------
// HTTPBoardClient
// ---------------------------------------------------------------------------

// HTTPBoardClient implements BoardClient by calling cmd/server's
// /internal/tools/dispatch (mutations) and /internal/board/cards (reads).
// All requests carry a bearer token shared with cmd/server's APP_API_TOKEN
// so the same auth path admits MCP, voice, and UI callers.
//
// The endpoint contract:
//
//	POST /internal/tools/dispatch
//	{
//	  "tool":       "card.create",       // MCP tool name (server translates)
//	  "args":       { ... },              // MCP-shaped arguments
//	  "dispatcher": "mcp",                // sets toolCallMeta.Source
//	  "tenant_id":  "default",
//	  "board_id":   "default"
//	}
//	→  { "card": {...} } | { "card_id": "...", "card": {...} } | { "comment": {...} }
//
//	GET /internal/board/cards            → { "cards": [Card...] }
//	GET /internal/board/cards/{id}       → { "card":  Card    }
type HTTPBoardClient struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
	Dispatcher string
}

// NewHTTPBoardClient returns a client with a sane default timeout. baseURL
// is the cmd/server root (e.g. http://app:3000); token authenticates as the
// shared APP_API_TOKEN. dispatcher labels the caller in audit ledger
// entries (defaults to "mcp" when empty).
func NewHTTPBoardClient(baseURL, token, dispatcher string) *HTTPBoardClient {
	d := strings.TrimSpace(dispatcher)
	if d == "" {
		d = "mcp"
	}
	return &HTTPBoardClient{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Token:      token,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		Dispatcher: d,
	}
}

// dispatchEnvelope is the JSON shape POST /internal/tools/dispatch consumes.
type dispatchEnvelope struct {
	Tool       string          `json:"tool"`
	Args       json.RawMessage `json:"args"`
	Dispatcher string          `json:"dispatcher,omitempty"`
	TenantID   string          `json:"tenant_id,omitempty"`
	BoardID    string          `json:"board_id,omitempty"`
}

// dispatchResult is the JSON shape /internal/tools/dispatch returns. The
// payload is intentionally permissive — different MCP tools surface
// different fields (card, comment, card_id, run_id, confirmation_id) —
// and the caller picks out what it needs.
type dispatchResult struct {
	Card                 *board.Card    `json:"card,omitempty"`
	CardID               string         `json:"card_id,omitempty"`
	Comment              *board.Comment `json:"comment,omitempty"`
	Cards                []board.Card   `json:"cards,omitempty"`
	RunID                string         `json:"run_id,omitempty"`
	RunStatus            string         `json:"status,omitempty"`
	AgentProfile         string         `json:"agent_profile,omitempty"`
	RequiresConfirmation bool           `json:"requires_confirmation,omitempty"`
	ConfirmationID       string         `json:"confirmation_id,omitempty"`
	RiskLevel            string         `json:"risk_level,omitempty"`
	Prompt               string         `json:"prompt,omitempty"`
	Error                string         `json:"error,omitempty"`
}

func (c *HTTPBoardClient) post(ctx context.Context, tool string, tenantID, boardID string, args any) (dispatchResult, error) {
	if c == nil {
		return dispatchResult{}, errors.New("http board client is nil")
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return dispatchResult{}, fmt.Errorf("marshal args: %w", err)
	}
	envelope := dispatchEnvelope{
		Tool:       tool,
		Args:       raw,
		Dispatcher: c.Dispatcher,
		TenantID:   tenantID,
		BoardID:    boardID,
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return dispatchResult{}, fmt.Errorf("marshal envelope: %w", err)
	}
	// #nosec G107 G704 -- BaseURL is operator-configured at process start (--board-url flag / BOARD_URL env), not user input.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/internal/tools/dispatch", bytes.NewReader(body))
	if err != nil {
		return dispatchResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTPClient.Do(req) // #nosec G107 G704 -- BaseURL is operator-configured at process start, not user input.
	if err != nil {
		return dispatchResult{}, fmt.Errorf("dispatch %s: %w", tool, err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return dispatchResult{}, fmt.Errorf("read dispatch response: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return dispatchResult{}, ErrCardNotFound
	}
	if resp.StatusCode >= 400 {
		// Try to decode an {error: "..."} body; otherwise surface raw text.
		var errBody struct {
			Error string `json:"error"`
		}
		if jsonErr := json.Unmarshal(respBody, &errBody); jsonErr == nil && errBody.Error != "" {
			return dispatchResult{}, fmt.Errorf("dispatch %s: %s", tool, errBody.Error)
		}
		return dispatchResult{}, fmt.Errorf("dispatch %s: http %d: %s", tool, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var out dispatchResult
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &out); err != nil {
			return dispatchResult{}, fmt.Errorf("decode dispatch response: %w", err)
		}
	}
	return out, nil
}

func (c *HTTPBoardClient) get(ctx context.Context, path string) ([]byte, int, error) {
	if c == nil {
		return nil, 0, errors.New("http board client is nil")
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	// #nosec G107 G704 -- BaseURL is operator-configured at process start (--board-url flag / BOARD_URL env), not user input. The `path` argument is constructed from hard-coded strings + sanitized cardIDs.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, 0, err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTPClient.Do(req) // #nosec G107 G704 -- BaseURL is operator-configured at process start, not user input.
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

// ListCards calls GET /internal/board/cards and applies the filter on the
// MCP side. (Server-side filtering can land later; today's boards are small
// enough that round-trip + filter is fine.)
func (c *HTTPBoardClient) ListCards(ctx context.Context, tenantID, boardID string, filter CardFilter) ([]board.Card, error) {
	body, status, err := c.get(ctx, "/internal/board/cards")
	if err != nil {
		return nil, fmt.Errorf("list cards: %w", err)
	}
	if status >= 400 {
		return nil, fmt.Errorf("list cards: http %d: %s", status, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Cards []board.Card `json:"cards"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode cards: %w", err)
	}
	out := make([]board.Card, 0, len(payload.Cards))
	for _, c := range payload.Cards {
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
	return out, nil
}

// GetCard calls GET /internal/board/cards/{id}.
func (c *HTTPBoardClient) GetCard(ctx context.Context, tenantID, boardID, cardID string) (board.Card, error) {
	body, status, err := c.get(ctx, "/internal/board/cards/"+cardID)
	if err != nil {
		return board.Card{}, fmt.Errorf("get card: %w", err)
	}
	if status == http.StatusNotFound {
		return board.Card{}, ErrCardNotFound
	}
	if status >= 400 {
		return board.Card{}, fmt.Errorf("get card: http %d: %s", status, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Card board.Card `json:"card"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return board.Card{}, fmt.Errorf("decode card: %w", err)
	}
	if payload.Card.ID == "" {
		return board.Card{}, ErrCardNotFound
	}
	return payload.Card, nil
}

// CreateCard dispatches card.create through /internal/tools/dispatch.
func (c *HTTPBoardClient) CreateCard(ctx context.Context, tenantID, boardID string, input CardCreate) (board.Card, error) {
	out, err := c.post(ctx, "card.create", tenantID, boardID, input)
	if err != nil {
		return board.Card{}, err
	}
	if out.Card == nil {
		return board.Card{}, fmt.Errorf("card.create: response missing card")
	}
	return *out.Card, nil
}

// updatePatchPayload is the wire shape /internal/tools/dispatch expects for
// card.update. Tags is rendered as a pointer (nil → omit; empty slice →
// explicit clear) so the dispatch endpoint can preserve patch semantics.
type updatePatchPayload struct {
	CardID string                  `json:"card_id"`
	Patch  updatePatchPayloadInner `json:"patch"`
}

type updatePatchPayloadInner struct {
	Title    *string      `json:"title,omitempty"`
	Status   *string      `json:"status,omitempty"`
	Assignee *board.Actor `json:"assignee,omitempty"`
	Notes    *string      `json:"notes,omitempty"`
	Tags     *[]string    `json:"tags,omitempty"`
}

// UpdateCard dispatches card.update through /internal/tools/dispatch.
func (c *HTTPBoardClient) UpdateCard(ctx context.Context, tenantID, boardID, cardID string, patch CardPatch) (board.Card, error) {
	inner := updatePatchPayloadInner{
		Title:    patch.Title,
		Status:   patch.Status,
		Assignee: patch.Assignee,
		Notes:    patch.Notes,
	}
	if patch.TagsSet {
		tags := append([]string{}, patch.Tags...)
		inner.Tags = &tags
	}
	payload := updatePatchPayload{CardID: cardID, Patch: inner}
	out, err := c.post(ctx, "card.update", tenantID, boardID, payload)
	if err != nil {
		return board.Card{}, err
	}
	if out.Card == nil {
		return board.Card{}, fmt.Errorf("card.update: response missing card")
	}
	return *out.Card, nil
}

// commentPayload is the wire shape /internal/tools/dispatch expects for
// card.comment.
type commentPayload struct {
	CardID  string `json:"card_id"`
	Body    string `json:"body"`
	AsActor string `json:"as_actor,omitempty"`
}

// StartRun dispatches runs.start through /internal/tools/dispatch. The
// server side translates this into assign_ticket_to_agent so the Run is
// minted by cmd/server's RunCoordinator and recorded against the canonical
// board state. When the Trust Ceremony queues a pending confirmation
// instead of applying immediately, the returned RunStartResult has
// RequiresConfirmation=true and an empty RunID.
func (c *HTTPBoardClient) StartRun(ctx context.Context, tenantID, boardID string, req RunStartRequest) (RunStartResult, error) {
	out, err := c.post(ctx, "runs.start", tenantID, boardID, req)
	if err != nil {
		return RunStartResult{}, err
	}
	if out.RequiresConfirmation {
		return RunStartResult{
			CardID:               req.CardID,
			RequiresConfirmation: true,
			ConfirmationID:       out.ConfirmationID,
			RiskLevel:            out.RiskLevel,
			Prompt:               out.Prompt,
		}, nil
	}
	if out.RunID == "" {
		return RunStartResult{}, fmt.Errorf("runs.start: response missing run_id")
	}
	return RunStartResult{
		RunID:        out.RunID,
		Status:       out.RunStatus,
		AgentProfile: out.AgentProfile,
		CardID:       req.CardID,
	}, nil
}

// AddComment dispatches card.comment through /internal/tools/dispatch.
func (c *HTTPBoardClient) AddComment(ctx context.Context, tenantID, boardID, cardID, body, author string) (board.Comment, error) {
	out, err := c.post(ctx, "card.comment", tenantID, boardID, commentPayload{
		CardID:  cardID,
		Body:    body,
		AsActor: author,
	})
	if err != nil {
		return board.Comment{}, err
	}
	if out.Comment == nil {
		return board.Comment{}, fmt.Errorf("card.comment: response missing comment")
	}
	return *out.Comment, nil
}
