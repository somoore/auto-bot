package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/somoore/auto-bot/internal/agent"
	"github.com/somoore/auto-bot/internal/board"
)

// ErrCardNotFound is returned by BoardAdapter.GetCard / UpdateCard when no
// card matches the supplied ID. Tool handlers translate this into a
// human-readable error surfaced to the MCP client.
var ErrCardNotFound = errors.New("mcp: card not found")

// BoardAdapter abstracts the board-state operations the five S2.0 tools
// need. The MCP package owns this interface so the protocol layer stays
// provider-neutral; concrete adapters live in cmd/mcpd (in-memory) and
// will live in cmd/server (shared in-memory cache) once we tackle the
// cross-process state-sharing question in S2.1.
//
// All methods are scoped by (tenantID, boardID). Empty values mean
// "default" — the adapter normalizes them.
type BoardAdapter interface {
	ListCards(ctx context.Context, tenantID, boardID string, filter CardFilter) ([]board.Card, error)
	GetCard(ctx context.Context, tenantID, boardID, cardID string) (board.Card, error)
	CreateCard(ctx context.Context, tenantID, boardID string, input CardCreate) (board.Card, error)
	UpdateCard(ctx context.Context, tenantID, boardID, cardID string, patch CardPatch) (board.Card, error)
	AddComment(ctx context.Context, tenantID, boardID, cardID string, body, author string) (board.Comment, error)
}

// CardFilter is the input shape for board.list_cards. Empty fields mean "no
// filter on this dimension". AgentOnly returns only cards whose assignee is
// an agent (any agent profile).
type CardFilter struct {
	Status     string `json:"status,omitempty"`
	AssigneeID string `json:"assignee_id,omitempty"`
	AgentOnly  bool   `json:"agent_only,omitempty"`
}

// CardCreate is the input shape for card.create. Notes mirrors Card.Notes
// (Description in the inbound JSON, but we use the field internally).
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
// runs.get tool (lands in S2.1).
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
// hidden globals) and makes them trivial to test with a fake BoardAdapter
// and an in-memory RunStore.
type ToolDeps struct {
	Board       BoardAdapter
	RunStore    agent.RunStore
	Coordinator agent.RunCoordinator
	TenantID    string
	BoardID     string

	// DefaultActor is the identity tool calls run under when the request
	// payload does not override it (e.g. card.comment's as_actor field).
	// For S2.0 this is a single shared "mcp" actor; S2.1 will switch to
	// per-token actor identity.
	DefaultActor string
}

// BuildTools returns the five MCP tools wired against deps. The order
// matches the canonical list in the Sprint 2.0 spec.
func BuildTools(deps ToolDeps) []Tool {
	return []Tool{
		buildListCardsTool(deps),
		buildGetCardTool(deps),
		buildCreateCardTool(deps),
		buildUpdateCardTool(deps),
		buildCommentTool(deps),
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
				// Best-effort: associate the most recent open question's
				// Run with each card so the slim view can surface "this
				// card has an agent run in flight".
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
			// Active run lookup is best-effort against the run store. We
			// match on CardID; if multiple runs exist, the most recently
			// updated wins.
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
		Description: "Create a new card on the active board. Routes through the same mutation path as voice tools (S2.0: ActionLedger / risk gates land in S2.1 once cross-process state-sharing is resolved).",
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
			// Decode into a raw map first so we can detect which patch
			// fields were actually supplied. Pointer fields capture
			// non-Tags scalars; tags need explicit set-tracking because
			// `null` and absent both unmarshal to a nil slice.
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
				// Set-tracking for Tags.
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
		Description: "Append a comment to a card's thread. as_actor overrides the default MCP actor for the duration of this call (S2.1 will scope this to per-token identities).",
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

// ---------------------------------------------------------------------------
// In-memory adapter
// ---------------------------------------------------------------------------

// InMemoryBoardAdapter is a thread-safe BoardAdapter that stores cards in a
// per-(tenant, board) map. It exists so the foundational slice is fully
// self-contained — cmd/mcpd uses it as the default backend, and the test
// suite uses it directly.
//
// S2.1's open architectural question is how this gets replaced with a
// shared-state-with-cmd/server adapter. Today's volume-mounted SQLite is
// not safe to read concurrently from two processes because cmd/server
// keeps an in-memory snapshot the file alone does not describe.
type InMemoryBoardAdapter struct {
	mu       sync.Mutex
	boards   map[boardKey]*boardSnapshot
	idSeed   int64
	clock    func() time.Time
	idPrefix string
}

type boardKey struct{ tenant, board string }

type boardSnapshot struct {
	cards []board.Card
}

// NewInMemoryBoardAdapter returns an empty adapter. SeedCards installs a
// pre-populated state (used in tests and for the default mcpd seed).
func NewInMemoryBoardAdapter() *InMemoryBoardAdapter {
	return &InMemoryBoardAdapter{
		boards:   map[boardKey]*boardSnapshot{},
		clock:    func() time.Time { return time.Now().UTC() },
		idPrefix: "mcp",
	}
}

// SeedCards installs cards into the named board, replacing any existing
// snapshot. Useful for tests and for cmd/mcpd to expose a non-empty board
// on first boot.
func (a *InMemoryBoardAdapter) SeedCards(tenantID, boardID string, cards []board.Card) {
	a.mu.Lock()
	defer a.mu.Unlock()
	clone := make([]board.Card, len(cards))
	copy(clone, cards)
	a.boards[a.key(tenantID, boardID)] = &boardSnapshot{cards: clone}
}

func (a *InMemoryBoardAdapter) key(tenantID, boardID string) boardKey {
	if tenantID == "" {
		tenantID = "default"
	}
	if boardID == "" {
		boardID = "default"
	}
	return boardKey{tenant: tenantID, board: boardID}
}

func (a *InMemoryBoardAdapter) ensure(tenantID, boardID string) *boardSnapshot {
	k := a.key(tenantID, boardID)
	snap, ok := a.boards[k]
	if !ok {
		snap = &boardSnapshot{}
		a.boards[k] = snap
	}
	return snap
}

// ListCards returns a filtered copy of the named board's cards in stable
// ID order. Copying isolates callers from concurrent mutation.
func (a *InMemoryBoardAdapter) ListCards(_ context.Context, tenantID, boardID string, filter CardFilter) ([]board.Card, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	snap := a.ensure(tenantID, boardID)
	out := make([]board.Card, 0, len(snap.cards))
	for _, c := range snap.cards {
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

// GetCard returns one card by ID.
func (a *InMemoryBoardAdapter) GetCard(_ context.Context, tenantID, boardID, cardID string) (board.Card, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	snap := a.ensure(tenantID, boardID)
	for _, c := range snap.cards {
		if c.ID == cardID {
			return c, nil
		}
	}
	return board.Card{}, ErrCardNotFound
}

// CreateCard appends a new card with a mcp-prefixed ID. The ID prefix lets
// human auditors tell at a glance which mutations came through the MCP
// surface vs cmd/server.
func (a *InMemoryBoardAdapter) CreateCard(_ context.Context, tenantID, boardID string, input CardCreate) (board.Card, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	snap := a.ensure(tenantID, boardID)
	a.idSeed++
	id := fmt.Sprintf("%s-%010d", a.idPrefix, a.idSeed)
	status := board.Status(input.Status)
	if strings.TrimSpace(string(status)) == "" {
		status = board.StatusBacklog
	}
	card := board.Card{
		ID:       id,
		Title:    input.Title,
		Notes:    input.Description,
		Status:   status,
		Tags:     append([]string{}, input.Tags...),
		Assignee: input.Assignee,
	}
	snap.cards = append(snap.cards, card)
	return card, nil
}

// UpdateCard applies a patch and returns the resulting card.
func (a *InMemoryBoardAdapter) UpdateCard(_ context.Context, tenantID, boardID, cardID string, patch CardPatch) (board.Card, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	snap := a.ensure(tenantID, boardID)
	for i := range snap.cards {
		if snap.cards[i].ID != cardID {
			continue
		}
		c := &snap.cards[i]
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

// AddComment appends a comment to the named card's thread.
func (a *InMemoryBoardAdapter) AddComment(_ context.Context, tenantID, boardID, cardID, body, author string) (board.Comment, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	snap := a.ensure(tenantID, boardID)
	for i := range snap.cards {
		if snap.cards[i].ID != cardID {
			continue
		}
		comment := board.Comment{
			ID:        fmt.Sprintf("cmt-%d", a.clock().UnixNano()),
			Body:      body,
			Author:    author,
			CreatedAt: a.clock().Format(time.RFC3339Nano),
		}
		snap.cards[i].Comments = append(snap.cards[i].Comments, comment)
		return comment, nil
	}
	return board.Comment{}, ErrCardNotFound
}
