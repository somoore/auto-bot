package main

import (
	"context"
	"time"

	"github.com/somoore/auto-bot/internal/agent"
	"github.com/somoore/auto-bot/internal/board"
	"github.com/somoore/auto-bot/internal/standup"
)

// standupBoardReader adapts the in-process kanbanBoard to the
// standup.BoardReader interface so internal/standup can stay free of
// cmd/server imports. The adapter snapshots the live board for Cards and
// reads the in-memory mutation history (capped at 200 entries) for the
// recent-mutation card list.
type standupBoardReader struct {
	board *kanbanBoard
}

func newStandupBoardReader(b *kanbanBoard) *standupBoardReader {
	return &standupBoardReader{board: b}
}

// Cards returns the current board cards as the internal/board domain type.
// The slice is freshly cloned via the snapshot path so callers can hold it
// past the call without racing the live board.
func (r *standupBoardReader) Cards(_ context.Context, _, _ string) ([]board.Card, error) {
	if r == nil || r.board == nil {
		return nil, nil
	}
	state := r.board.SnapshotState()
	out := make([]board.Card, len(state.Cards))
	copy(out, state.Cards)
	return out, nil
}

// RecentMutationCardIDs returns the set of card IDs that appear in mutation
// records within the lookback window. Entries are de-duped and the slice
// preserves the newest-first order so callers that only care about the top
// N see the most recent first.
func (r *standupBoardReader) RecentMutationCardIDs(_ context.Context, _, _ string, since time.Time) ([]string, error) {
	if r == nil || r.board == nil {
		return nil, nil
	}
	r.board.mu.Lock()
	records := append([]boardMutationRecord(nil), r.board.mutationHistory...)
	r.board.mu.Unlock()

	seen := map[string]struct{}{}
	out := make([]string, 0, len(records))
	for i := len(records) - 1; i >= 0; i-- {
		record := records[i]
		occurred, err := time.Parse(time.RFC3339Nano, record.OccurredAt)
		if err != nil {
			occurred, err = time.Parse(time.RFC3339, record.OccurredAt)
			if err != nil {
				continue
			}
		}
		if occurred.Before(since) {
			continue
		}
		for _, id := range record.CardIDs {
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out, nil
}

// standupRunReader adapts an agent.RunStore + agentRunStore pair to the
// standup.RunReader interface. The implementation forwards to the same
// store the rest of cmd/server uses; nil-safety lets the adapter be wired
// even when persistence is in-memory only.
type standupRunReader struct {
	agentStore   agentRunStore
	questionRead agent.RunStore
}

func newStandupRunReader(legacy agentRunStore, runStore agent.RunStore) *standupRunReader {
	return &standupRunReader{agentStore: legacy, questionRead: runStore}
}

func (r *standupRunReader) ListAgentRuns(ctx context.Context, tenantID, boardID string, limit int) ([]agent.Run, error) {
	if r == nil || r.agentStore == nil {
		return nil, nil
	}
	return r.agentStore.ListAgentRuns(ctx, tenantID, boardID, limit)
}

func (r *standupRunReader) ListOpenRunQuestions(ctx context.Context, tenantID, boardID string) ([]agent.RunQuestion, error) {
	if r == nil || r.questionRead == nil {
		return nil, nil
	}
	return r.questionRead.ListOpenRunQuestions(ctx, tenantID, boardID)
}

// agendaBuilderFor returns the AgendaBuilder wired to the supplied board's
// stores. Returns nil when the board has no persistent backing (in-memory
// tests); callers branch on nil to skip injection.
func agendaBuilderFor(b *kanbanBoard) *standup.AgendaBuilder {
	if b == nil {
		return nil
	}
	boardReader := newStandupBoardReader(b)
	var (
		legacyStore   agentRunStore
		questionStore agent.RunStore
	)
	if b.store != nil {
		if rs, ok := b.store.(agentRunStore); ok {
			legacyStore = rs
		}
		if as, ok := b.store.(agent.RunStore); ok {
			questionStore = as
		}
	}
	return &standup.AgendaBuilder{
		Board: boardReader,
		Runs:  newStandupRunReader(legacyStore, questionStore),
	}
}
