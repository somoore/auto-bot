package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/somoore/auto-bot/internal/agent"
)

// TestRunCoordinatorCancelSurfacesPersistenceError is the SE-1 F3 regression
// test. Before the fix, persistAgentRun used context.Background() and dropped
// every SaveRun failure to a log line; updateAgentRun was a void function;
// the RunCoordinator surface returned nil from Cancel even when the
// underlying SaveRun returned an error. Callers thought the run had been
// marked cancelled when nothing had been written through.
//
// The fix threads the caller's context, returns the error from
// persistAgentRun, returns the error from updateAgentRun, and propagates it
// out of every RunCoordinator method that mutates a Run. This test pins
// that contract for Cancel by injecting a failingAgentRunStore that always
// returns an error from SaveRun. The coordinator must surface that error
// instead of silently returning nil.
func TestRunCoordinatorCancelSurfacesPersistenceError(t *testing.T) {
	board, runID := newAgentCoordinatorPersistenceFixture(t)

	sentinel := errors.New("SaveRun failed under test injection")
	board.store = &failingAgentRunStore{saveErr: sentinel}

	orchestrator := &agentRunOrchestrator{board: board}
	err := orchestrator.Cancel(context.Background(), runID, "test cancellation")
	if err == nil {
		t.Fatal("Cancel returned nil error after SaveRun injection failed; persistence error was swallowed")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("Cancel error chain = %v, want wrapped sentinel %v", err, sentinel)
	}
}

// TestRunCoordinatorCheckpointSurfacesPersistenceError pins the same
// contract for the Checkpoint path, which the task brief calls out
// explicitly. Checkpoint writes both AppendRunCheckpoint and (through
// updateAgentRun) SaveRun; either failure must surface.
func TestRunCoordinatorCheckpointSurfacesPersistenceError(t *testing.T) {
	board, runID := newAgentCoordinatorPersistenceFixture(t)

	sentinel := errors.New("SaveRun failed under test injection")
	board.store = &failingAgentRunStore{saveErr: sentinel}

	orchestrator := &agentRunOrchestrator{board: board}
	err := orchestrator.Checkpoint(context.Background(), runID, agent.RunStepCheckpoint{
		StepIndex: 1,
		Kind:      agent.CheckpointKindStarted,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err == nil {
		t.Fatal("Checkpoint returned nil error after SaveRun injection failed; persistence error was swallowed")
	}
	if !strings.Contains(err.Error(), "persist run plan") && !strings.Contains(err.Error(), "SaveRun failed") {
		t.Fatalf("Checkpoint error = %v, want wrapped persistence error", err)
	}
}

// newAgentCoordinatorPersistenceFixture creates a minimal in-memory board
// with one agentRun in a non-terminal state so coordinator persistence
// failures can be exercised without driving the full assignTicketToAgent
// path (which itself depends on the same store).
func newAgentCoordinatorPersistenceFixture(t *testing.T) (*kanbanBoard, string) {
	t.Helper()
	board := newKanbanBoard()
	runID := "agent-run-persistence-1"
	now := time.Now().UTC().Format(time.RFC3339Nano)
	board.mu.Lock()
	board.agentRuns = append(board.agentRuns, agentRun{
		RunID:        runID,
		TenantID:     board.tenantID,
		BoardID:      board.boardID,
		CardID:       "fixture-card",
		Objective:    "test fixture run",
		AgentProfile: "project_manager",
		RequestType:  "code_review",
		Specialist:   "project_manager",
		Status:       agentRunReviewing,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	board.mu.Unlock()
	return board, runID
}

// failingAgentRunStore is the minimal boardStore + agentRunStore +
// agent.RunStore implementation needed to drive the persistence error path.
// SaveRun returns saveErr on every call; other methods return zero values so
// the board can still bootstrap.
type failingAgentRunStore struct {
	saveErr error
}

var (
	_ boardStore         = (*failingAgentRunStore)(nil)
	_ agentRunStore      = (*failingAgentRunStore)(nil)
	_ agent.RunStore     = (*failingAgentRunStore)(nil)
	_ meetingReportStore = (*failingAgentRunStore)(nil)
)

// boardStore surface.
func (s *failingAgentRunStore) LoadBoard(ctx context.Context, tenantID string, boardID string) (kanbanBoardState, bool, error) {
	return kanbanBoardState{}, false, nil
}
func (s *failingAgentRunStore) SaveSnapshot(ctx context.Context, tenantID string, boardID string, state kanbanBoardState) error {
	return nil
}
func (s *failingAgentRunStore) AppendEvent(ctx context.Context, tenantID string, boardID string, event boardEventRecord, state kanbanBoardState) error {
	return nil
}
func (s *failingAgentRunStore) Close() error { return nil }

// agentRunStore surface — SaveRun is the failure injection point.
func (s *failingAgentRunStore) SaveRun(ctx context.Context, tenantID string, boardID string, run agentRun) error {
	return s.saveErr
}
func (s *failingAgentRunStore) LoadRun(ctx context.Context, tenantID string, boardID string, runID string) (agentRun, error) {
	return agentRun{}, agent.ErrRunNotFound
}
func (s *failingAgentRunStore) ListAgentRuns(ctx context.Context, tenantID string, boardID string, limit int) ([]agentRun, error) {
	return nil, nil
}

// agent.RunStore surface — additional methods the interface requires beyond
// SaveRun/LoadRun. The checkpoint path calls AppendRunCheckpoint inside
// Coordinator.Checkpoint before updateAgentRun, so it must not block the
// SaveRun failure under test.
func (s *failingAgentRunStore) AppendRunCheckpoint(ctx context.Context, tenantID, boardID, runID string, cp agent.RunStepCheckpoint) error {
	return nil
}
func (s *failingAgentRunStore) ListRunCheckpoints(ctx context.Context, tenantID, boardID, runID string) ([]agent.RunStepCheckpoint, error) {
	return nil, nil
}
func (s *failingAgentRunStore) SaveRunQuestion(ctx context.Context, q agent.RunQuestion) error {
	return nil
}
func (s *failingAgentRunStore) LoadRunQuestion(ctx context.Context, tenantID, boardID, questionID string) (agent.RunQuestion, error) {
	return agent.RunQuestion{}, agent.ErrRunQuestionNotFound
}
func (s *failingAgentRunStore) ListOpenRunQuestions(ctx context.Context, tenantID, boardID string) ([]agent.RunQuestion, error) {
	return nil, nil
}
func (s *failingAgentRunStore) MarkRunQuestionAnswered(ctx context.Context, tenantID, boardID, questionID, answer, answeredBy, answeredVia string) error {
	return nil
}
func (s *failingAgentRunStore) ExpireRunQuestions(ctx context.Context, tenantID, boardID string, now time.Time) (int, error) {
	return 0, nil
}

// meetingReportStore surface — board bootstrap calls into this on load.
func (s *failingAgentRunStore) SaveMeetingReport(ctx context.Context, report meetingIntelligenceReport) error {
	return nil
}
func (s *failingAgentRunStore) LoadMeetingReport(ctx context.Context, tenantID string, boardID string, meetingID string) (meetingIntelligenceReport, bool, error) {
	return meetingIntelligenceReport{}, false, nil
}
func (s *failingAgentRunStore) ListMeetingReports(ctx context.Context, tenantID string, boardID string, limit int) ([]meetingReportSummary, error) {
	return nil, nil
}
