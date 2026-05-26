package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/somoore/auto-bot/internal/agent"
)

// TestCheckpointFailsLoudOnBrokenAuditSink is the SE-1 F2 regression test.
// Before the fix, RunCoordinator.Checkpoint used a `if store, ok := ...; ok`
// pattern to opt into AppendRunCheckpoint and silently skipped the audit
// append on a type-assertion miss; callers got nil back from Checkpoint
// even though nothing had been written through to the durable audit log.
//
// The fix:
//   - A non-nil board store that does not implement agent.RunStore now
//     returns ErrCheckpointAuditFailed wrapped with the store's concrete
//     type — the configuration bug is surfaced rather than swallowed.
//   - AppendRunCheckpoint errors are wrapped with ErrCheckpointAuditFailed
//     so callers can branch via errors.Is.
//   - A nil store remains acceptable (tests bootstrap kanbanBoards
//     without persistence).
//
// This test pins the "broken audit sink" branch: AppendRunCheckpoint
// returns a sentinel and Checkpoint must surface it wrapped in
// ErrCheckpointAuditFailed.
func TestCheckpointFailsLoudOnBrokenAuditSink(t *testing.T) {
	board, runID := newAgentCoordinatorPersistenceFixture(t)

	sentinel := errors.New("AppendRunCheckpoint failed under test injection")
	board.store = &failingCheckpointAuditStore{appendErr: sentinel}

	orchestrator := &agentRunOrchestrator{board: board}
	err := orchestrator.Checkpoint(context.Background(), runID, agent.RunStepCheckpoint{
		StepIndex: 1,
		Kind:      agent.CheckpointKindCompleted,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err == nil {
		t.Fatal("Checkpoint returned nil error against a broken audit sink; SE-1 F2 fail-loud guard missing")
	}
	if !errors.Is(err, agent.ErrCheckpointAuditFailed) {
		t.Fatalf("Checkpoint error = %v, want errors.Is(err, agent.ErrCheckpointAuditFailed)", err)
	}
}

// TestCheckpointFailsLoudOnStoreNotImplementingRunStore covers the type-
// assertion-miss branch the task brief calls out explicitly: a board with a
// non-nil store that does not implement agent.RunStore must surface
// ErrCheckpointAuditFailed rather than silently dropping the audit append.
func TestCheckpointFailsLoudOnStoreNotImplementingRunStore(t *testing.T) {
	board, runID := newAgentCoordinatorPersistenceFixture(t)
	board.store = &incompatibleBoardStore{}

	orchestrator := &agentRunOrchestrator{board: board}
	err := orchestrator.Checkpoint(context.Background(), runID, agent.RunStepCheckpoint{
		StepIndex: 1,
		Kind:      agent.CheckpointKindStarted,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err == nil {
		t.Fatal("Checkpoint returned nil error against a store that does not implement agent.RunStore")
	}
	if !errors.Is(err, agent.ErrCheckpointAuditFailed) {
		t.Fatalf("Checkpoint error = %v, want errors.Is(err, agent.ErrCheckpointAuditFailed)", err)
	}
}

// TestCheckpointSucceedsAgainstNilStore confirms the explicit carve-out the
// fix preserves: kanbanBoards bootstrapped without a store (the common
// test path via newKanbanBoard) must keep working. The nil-store branch
// is not a "miss" — it is the documented in-memory mode.
func TestCheckpointSucceedsAgainstNilStore(t *testing.T) {
	board, runID := newAgentCoordinatorPersistenceFixture(t)
	if board.store != nil {
		t.Fatalf("test fixture should leave board.store nil, got %T", board.store)
	}
	orchestrator := &agentRunOrchestrator{board: board}
	if err := orchestrator.Checkpoint(context.Background(), runID, agent.RunStepCheckpoint{
		StepIndex: 1,
		Kind:      agent.CheckpointKindStarted,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("Checkpoint against nil store returned error: %v", err)
	}
}

// failingCheckpointAuditStore satisfies all the store interfaces the board
// needs (boardStore, agentRunStore, agent.RunStore, meetingReportStore) so
// the board can bootstrap, but injects appendErr from AppendRunCheckpoint
// so the SE-1 F2 failure path can be exercised in isolation. SaveRun
// returns nil so the Bug 3 persistence path stays clean and only the
// checkpoint audit append surfaces the failure.
type failingCheckpointAuditStore struct {
	appendErr error
}

var (
	_ boardStore         = (*failingCheckpointAuditStore)(nil)
	_ agentRunStore      = (*failingCheckpointAuditStore)(nil)
	_ agent.RunStore     = (*failingCheckpointAuditStore)(nil)
	_ meetingReportStore = (*failingCheckpointAuditStore)(nil)
)

func (s *failingCheckpointAuditStore) LoadBoard(ctx context.Context, tenantID string, boardID string) (kanbanBoardState, bool, error) {
	return kanbanBoardState{}, false, nil
}
func (s *failingCheckpointAuditStore) SaveSnapshot(ctx context.Context, tenantID string, boardID string, state kanbanBoardState) error {
	return nil
}
func (s *failingCheckpointAuditStore) AppendEvent(ctx context.Context, tenantID string, boardID string, event boardEventRecord, state kanbanBoardState) error {
	return nil
}
func (s *failingCheckpointAuditStore) Close() error { return nil }

func (s *failingCheckpointAuditStore) SaveRun(ctx context.Context, tenantID string, boardID string, run agentRun) error {
	return nil
}
func (s *failingCheckpointAuditStore) LoadRun(ctx context.Context, tenantID string, boardID string, runID string) (agentRun, error) {
	return agentRun{}, agent.ErrRunNotFound
}
func (s *failingCheckpointAuditStore) ListAgentRuns(ctx context.Context, tenantID string, boardID string, limit int) ([]agentRun, error) {
	return nil, nil
}

func (s *failingCheckpointAuditStore) AppendRunCheckpoint(ctx context.Context, tenantID, boardID, runID string, cp agent.RunStepCheckpoint) error {
	return s.appendErr
}
func (s *failingCheckpointAuditStore) ListRunCheckpoints(ctx context.Context, tenantID, boardID, runID string) ([]agent.RunStepCheckpoint, error) {
	return nil, nil
}
func (s *failingCheckpointAuditStore) SaveRunQuestion(ctx context.Context, q agent.RunQuestion) error {
	return nil
}
func (s *failingCheckpointAuditStore) LoadRunQuestion(ctx context.Context, tenantID, boardID, questionID string) (agent.RunQuestion, error) {
	return agent.RunQuestion{}, agent.ErrRunQuestionNotFound
}
func (s *failingCheckpointAuditStore) ListOpenRunQuestions(ctx context.Context, tenantID, boardID string) ([]agent.RunQuestion, error) {
	return nil, nil
}
func (s *failingCheckpointAuditStore) MarkRunQuestionAnswered(ctx context.Context, tenantID, boardID, questionID, answer, answeredBy, answeredVia string) error {
	return nil
}
func (s *failingCheckpointAuditStore) ExpireRunQuestions(ctx context.Context, tenantID, boardID string, now time.Time) (int, error) {
	return 0, nil
}

func (s *failingCheckpointAuditStore) SaveMeetingReport(ctx context.Context, report meetingIntelligenceReport) error {
	return nil
}
func (s *failingCheckpointAuditStore) LoadMeetingReport(ctx context.Context, tenantID string, boardID string, meetingID string) (meetingIntelligenceReport, bool, error) {
	return meetingIntelligenceReport{}, false, nil
}
func (s *failingCheckpointAuditStore) ListMeetingReports(ctx context.Context, tenantID string, boardID string, limit int) ([]meetingReportSummary, error) {
	return nil, nil
}

// incompatibleBoardStore satisfies the boardStore interface but does NOT
// implement agent.RunStore. It is used to exercise the type-assertion-miss
// branch the SE-1 F2 fix protects against.
type incompatibleBoardStore struct{}

var _ boardStore = (*incompatibleBoardStore)(nil)

func (s *incompatibleBoardStore) LoadBoard(ctx context.Context, tenantID string, boardID string) (kanbanBoardState, bool, error) {
	return kanbanBoardState{}, false, nil
}
func (s *incompatibleBoardStore) SaveSnapshot(ctx context.Context, tenantID string, boardID string, state kanbanBoardState) error {
	return nil
}
func (s *incompatibleBoardStore) AppendEvent(ctx context.Context, tenantID string, boardID string, event boardEventRecord, state kanbanBoardState) error {
	return nil
}
func (s *incompatibleBoardStore) Close() error { return nil }
