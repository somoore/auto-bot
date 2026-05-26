package main

import (
	"context"
	"time"

	"github.com/somoore/auto-bot/internal/agent"
)

// init replaces the no-op stub in dry_run_http.go with the real
// kill-switch transition handler. Order is preserved: dry_run_http.go
// declares the variable so the HTTP handler compiles in commit 2; this
// commit replaces the implementation with the actual fanout.
func init() {
	handleAgentsPausedTransition = doHandleAgentsPausedTransition
}

// doHandleAgentsPausedTransition runs whenever a tenant flips the
// AgentsPaused switch. When paused (true), every non-terminal Run for the
// tenant transitions to StatusPaused and a `run_paused` event fires. When
// resumed (false), runs that were paused by the kill switch transition
// back to StatusQueued so the orchestrator can pick them up again.
func doHandleAgentsPausedTransition(ctx context.Context, tenantID string, paused bool) {
	board := sharedBoard
	if board == nil || board.tenantID != tenantID {
		// In a multi-board world we would look up the board by tenant.
		// Today there is exactly one board.
		return
	}
	store := agentRunStoreForBoard(board)
	if store == nil {
		return
	}
	runs, err := store.ListAgentRuns(ctx, tenantID, board.boardID, 200)
	if err != nil {
		log.Errorf("pause_all: list runs: %v", err)
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, run := range runs {
		if paused {
			if agentRunIsTerminal(run.Status) || run.Status == agent.StatusPaused {
				continue
			}
			previous := run.Status
			run.Status = agent.StatusPaused
			run.UpdatedAt = now
			run.Checkpoints = append(run.Checkpoints, agent.Checkpoint{
				At:      now,
				Status:  agent.StatusPaused,
				Step:    string(previous),
				Message: "Run paused by tenant-wide kill switch",
			})
			if err := saveAgentRunToStore(ctx, store, tenantID, board.boardID, run); err != nil {
				log.Errorf("pause_all: persist paused run %s: %v", run.RunID, err)
				continue
			}
			broadcastKanbanEventForBoard(tenantID, board.boardID, "run_paused", run.View())
		} else {
			if run.Status != agent.StatusPaused {
				continue
			}
			run.Status = agent.StatusQueued
			run.UpdatedAt = now
			run.Checkpoints = append(run.Checkpoints, agent.Checkpoint{
				At:      now,
				Status:  agent.StatusQueued,
				Message: "Run resumed by tenant-wide kill switch release",
			})
			if err := saveAgentRunToStore(ctx, store, tenantID, board.boardID, run); err != nil {
				log.Errorf("pause_all: persist resumed run %s: %v", run.RunID, err)
				continue
			}
			broadcastKanbanEventForBoard(tenantID, board.boardID, "run_resumed", run.View())
		}
	}
}

// agentRunStoreForBoard returns the legacy agentRunStore that backs the
// board, or nil when persistence is in-memory. Production wires the SQLite
// store; tests pass nil. The signature matches the existing
// (*sqliteBoardStore) methods.
func agentRunStoreForBoard(board *kanbanBoard) agentRunStore {
	if board == nil || board.store == nil {
		return nil
	}
	store, ok := board.store.(agentRunStore)
	if !ok {
		return nil
	}
	return store
}

func saveAgentRunToStore(ctx context.Context, store agentRunStore, tenantID, boardID string, run agentRun) error {
	return store.SaveRun(ctx, tenantID, boardID, run)
}
