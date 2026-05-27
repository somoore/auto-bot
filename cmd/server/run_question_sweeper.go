package main

import (
	"context"
	"time"

	"github.com/somoore/auto-bot/internal/agent"
)

// defaultRunQuestionSweepInterval is the cadence at which the background
// sweeper expires open RunQuestions whose TTL has elapsed. 60s is a
// compromise between drawer responsiveness and DB churn; the constant is
// exported as a var so tests can shrink it.
var defaultRunQuestionSweepInterval = 60 * time.Second

// startRunQuestionSweeper launches a background goroutine that calls
// RunStore.ExpireRunQuestions on every tick and broadcasts
// `"run_question_expired"` events for each question that transitions to
// the expired state. The goroutine exits cleanly when ctx is cancelled,
// satisfying the "do not leak goroutines on shutdown" contract from S1.4.
//
// The sweeper is a no-op (returns without spawning a goroutine) when the
// board has no agent.RunStore-backed store, which is the case for the
// in-memory single-process default. Production deployments configure
// BOARD_SQLITE_PATH and get the sweeper automatically.
func startRunQuestionSweeper(ctx context.Context, board *kanbanBoard, interval time.Duration) {
	if board == nil || board.store == nil {
		return
	}
	store, ok := board.store.(agent.RunStore)
	if !ok {
		return
	}
	if interval <= 0 {
		interval = defaultRunQuestionSweepInterval
	}

	go runRunQuestionSweeper(ctx, board, store, interval)
}

func runRunQuestionSweeper(ctx context.Context, board *kanbanBoard, store agent.RunStore, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepRunQuestionsOnce(ctx, board, store)
		}
	}
}

// sweepRunQuestionsOnce performs one expiry pass and broadcasts an event
// per expired question. It snapshots the open set before expiring so the
// emitted payloads carry the original question shape (with `status =
// expired` synthesized on the broadcast copy).
//
// Exposed at package scope so tests can drive the sweep deterministically
// without spinning up the goroutine and worrying about ticker timing.
func sweepRunQuestionsOnce(ctx context.Context, board *kanbanBoard, store agent.RunStore) {
	open, err := store.ListOpenRunQuestions(ctx, board.tenantID, board.boardID)
	if err != nil {
		log.Errorf("run-question sweeper: list open questions: %v", err)
		return
	}
	if len(open) == 0 {
		return
	}
	now := time.Now().UTC()
	expired, err := store.ExpireRunQuestions(ctx, board.tenantID, board.boardID, now)
	if err != nil {
		log.Errorf("run-question sweeper: expire: %v", err)
		return
	}
	if expired <= 0 {
		return
	}

	// Re-list to identify which IDs actually flipped to expired. The
	// difference between the pre-sweep open set and the post-sweep open
	// set is the set of questions that just transitioned.
	//
	// Known limitation (acceptable for S1.4 cadence; revisit if Sprint 2
	// MCP sweeps more aggressively): if a question is answered between
	// the pre-sweep ListOpenRunQuestions and ExpireRunQuestions, it will
	// be absent from stillOpen with status "answered" rather than
	// "expired" and we will falsely emit run_question_expired for it.
	// Idempotent UI clients tolerate this; the long-term fix is to have
	// ExpireRunQuestions return the IDs it actually expired.
	stillOpen, err := store.ListOpenRunQuestions(ctx, board.tenantID, board.boardID)
	if err != nil {
		log.Errorf("run-question sweeper: relist after expire: %v", err)
		return
	}
	stillOpenIDs := make(map[string]struct{}, len(stillOpen))
	for _, q := range stillOpen {
		stillOpenIDs[q.ID] = struct{}{}
	}
	for _, q := range open {
		if _, ok := stillOpenIDs[q.ID]; ok {
			continue
		}
		expiredCopy := q
		expiredCopy.Status = "expired"
		broadcastKanbanEventForBoard(board.tenantID, board.boardID, "run_question_expired", expiredCopy)
	}
	broadcastKanbanEventForBoard(board.tenantID, board.boardID, "board", board.SnapshotState())
}
