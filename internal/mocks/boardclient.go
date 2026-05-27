package mocks

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/somoore/auto-bot/internal/board"
	"github.com/somoore/auto-bot/internal/mcp"
)

// BoardClient is a thread-safe in-memory implementation of mcp.BoardClient
// for tests and the cmd/mcpd offline fallback. Production deployments wire
// in mcp.HTTPBoardClient (which routes through cmd/server's ApplyToolCall);
// this mock exists so the protocol layer + tool handlers stay testable
// without a live cmd/server.
type BoardClient struct {
	mu     sync.Mutex
	boards map[boardKey]*boardSnapshot
	idSeed int64
	clock  func() time.Time

	// IDPrefix is the prefix attached to mock card IDs. Defaults to "mcp"
	// so they're trivially distinguishable from cmd/server-issued IDs.
	IDPrefix string
}

type boardKey struct{ tenant, board string }

type boardSnapshot struct {
	cards []board.Card
}

// NewBoardClient returns an empty BoardClient ready for use.
func NewBoardClient() *BoardClient {
	return &BoardClient{
		boards:   map[boardKey]*boardSnapshot{},
		clock:    func() time.Time { return time.Now().UTC() },
		IDPrefix: "mcp",
	}
}

// SeedCards installs cards into the named board, replacing any existing
// snapshot. Useful for tests and for cmd/mcpd to expose a non-empty board
// on first boot when running disconnected from cmd/server.
func (a *BoardClient) SeedCards(tenantID, boardID string, cards []board.Card) {
	a.mu.Lock()
	defer a.mu.Unlock()
	clone := make([]board.Card, len(cards))
	copy(clone, cards)
	a.boards[a.key(tenantID, boardID)] = &boardSnapshot{cards: clone}
}

func (a *BoardClient) key(tenantID, boardID string) boardKey {
	if tenantID == "" {
		tenantID = "default"
	}
	if boardID == "" {
		boardID = "default"
	}
	return boardKey{tenant: tenantID, board: boardID}
}

func (a *BoardClient) ensure(tenantID, boardID string) *boardSnapshot {
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
func (a *BoardClient) ListCards(_ context.Context, tenantID, boardID string, filter mcp.CardFilter) ([]board.Card, error) {
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
func (a *BoardClient) GetCard(_ context.Context, tenantID, boardID, cardID string) (board.Card, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	snap := a.ensure(tenantID, boardID)
	for _, c := range snap.cards {
		if c.ID == cardID {
			return c, nil
		}
	}
	return board.Card{}, mcp.ErrCardNotFound
}

// CreateCard appends a new card with a mock-prefixed ID.
func (a *BoardClient) CreateCard(_ context.Context, tenantID, boardID string, input mcp.CardCreate) (board.Card, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	snap := a.ensure(tenantID, boardID)
	a.idSeed++
	prefix := a.IDPrefix
	if prefix == "" {
		prefix = "mcp"
	}
	id := fmt.Sprintf("%s-%010d", prefix, a.idSeed)
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
func (a *BoardClient) UpdateCard(_ context.Context, tenantID, boardID, cardID string, patch mcp.CardPatch) (board.Card, error) {
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
	return board.Card{}, mcp.ErrCardNotFound
}

// StartRun is the mock equivalent of dispatching runs.start through cmd/server.
// It returns a synthetic Run identifier without touching any RunStore — the
// mock exists to keep the protocol layer testable when cmd/server is offline.
// Production deployments wire HTTPBoardClient.StartRun and the real run is
// minted by cmd/server's RunCoordinator.
func (a *BoardClient) StartRun(_ context.Context, tenantID, boardID string, req mcp.RunStartRequest) (mcp.RunStartResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	snap := a.ensure(tenantID, boardID)
	found := false
	for i := range snap.cards {
		if snap.cards[i].ID == req.CardID {
			found = true
			profile := strings.TrimSpace(req.AgentProfile)
			if profile == "" {
				profile = "project_manager"
			}
			snap.cards[i].Assignee = &board.Actor{
				Kind:         board.ActorKindAgent,
				ID:           "agent:" + profile + ":" + a.key(tenantID, boardID).tenant,
				DisplayName:  profile,
				AgentProfile: profile,
				OwnerHumanID: req.RequestedBy,
			}
			break
		}
	}
	if !found {
		return mcp.RunStartResult{}, mcp.ErrCardNotFound
	}
	a.idSeed++
	profile := strings.TrimSpace(req.AgentProfile)
	if profile == "" {
		profile = "project_manager"
	}
	return mcp.RunStartResult{
		RunID:        fmt.Sprintf("%s-run-%010d", a.IDPrefix, a.idSeed),
		Status:       "queued",
		AgentProfile: profile,
		CardID:       req.CardID,
	}, nil
}

// AddComment appends a comment to the named card's thread.
func (a *BoardClient) AddComment(_ context.Context, tenantID, boardID, cardID, body, author string) (board.Comment, error) {
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
	return board.Comment{}, mcp.ErrCardNotFound
}

// Compile-time assertion that *BoardClient satisfies mcp.BoardClient.
var _ mcp.BoardClient = (*BoardClient)(nil)

// ErrNotImplemented is returned by BoardClient methods that are intentionally
// left as stubs in this mock.
var ErrNotImplemented = errors.New("mocks: not implemented")
