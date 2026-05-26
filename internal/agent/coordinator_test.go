package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/somoore/auto-bot/internal/agent"
	"github.com/somoore/auto-bot/internal/mocks"
)

// TestRunCoordinatorLifecycle walks SimpleRunCoordinator through the full
// state-machine round trip the spec describes: Start -> Checkpoint(completed)
// -> AskHuman -> Resume -> Cancel. The mock RunStore is the persistence
// substrate; we assert on Run snapshots returned by each method and on the
// store's view via LoadRun / ListRunCheckpoints.
func TestRunCoordinatorLifecycle(t *testing.T) {
	store := mocks.NewRunStore()
	clock := newStepClock(time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC))
	store.WithClock(clock.Now)
	coord := agent.NewSimpleRunCoordinator(store, clock.Now)

	ctx := context.Background()
	tenant := "tenant-a"
	board := "board-a"

	// 1. Start: persists a Run, mints a ULID-prefixed ID, plan is empty.
	run, err := coord.Start(ctx, agent.RunRequest{
		TenantID:     tenant,
		BoardID:      board,
		CardID:       "EMAL-12",
		Objective:    "review the PR",
		RequestedBy:  "scott",
		AgentProfile: "swe-1",
		RequestType:  "code_review",
	})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if run.RunID == "" {
		t.Fatal("Start returned Run with empty RunID")
	}
	if run.RunID[:4] != "run_" {
		t.Fatalf("RunID = %q, want run_ prefix", run.RunID)
	}
	if len(run.Plan) != 0 {
		t.Fatalf("Plan = %+v, want empty on Start", run.Plan)
	}
	if isTerminal(run.Status) {
		t.Fatalf("Start returned terminal status %q", run.Status)
	}

	// Seed a one-step plan so Checkpoint has something to advance.
	run.Plan = []agent.PlanStep{{Index: 1, Title: "review", Status: "pending"}}
	if err := store.SaveRun(ctx, tenant, board, run); err != nil {
		t.Fatalf("SaveRun seed plan: %v", err)
	}

	// 2. Checkpoint kind="completed": Plan[step].Status -> "done".
	if err := coord.Checkpoint(ctx, run.RunID, agent.RunStepCheckpoint{
		StepIndex: 1,
		Kind:      agent.CheckpointKindCompleted,
		CreatedAt: clock.Now().Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("Checkpoint completed: %v", err)
	}
	after, err := store.LoadRun(ctx, tenant, board, run.RunID)
	if err != nil {
		t.Fatalf("LoadRun after checkpoint: %v", err)
	}
	if len(after.Plan) != 1 || after.Plan[0].Status != "done" {
		t.Fatalf("Plan after Checkpoint = %+v, want index 1 status=done", after.Plan)
	}
	cps, err := store.ListRunCheckpoints(ctx, tenant, board, run.RunID)
	if err != nil {
		t.Fatalf("ListRunCheckpoints: %v", err)
	}
	if len(cps) != 1 || cps[0].Kind != agent.CheckpointKindCompleted {
		t.Fatalf("audit log = %+v, want one completed entry", cps)
	}

	// 3. AskHuman: persists a RunQuestion, Run.WaitingOn populated,
	// Status = waiting_on_human.
	questionID, err := coord.AskHuman(ctx, run.RunID, agent.RunQuestion{
		StepIndex:   1,
		Prompt:      "Which target environment should the rollout go to first?",
		Suggestions: []string{"us-east-1 canary", "us-west-2 staging"},
	})
	if err != nil {
		t.Fatalf("AskHuman: %v", err)
	}
	if questionID == "" {
		t.Fatal("AskHuman returned empty questionID")
	}
	loadedQ, err := store.LoadRunQuestion(ctx, tenant, board, questionID)
	if err != nil {
		t.Fatalf("LoadRunQuestion: %v", err)
	}
	if loadedQ.Status != "open" {
		t.Fatalf("RunQuestion.Status = %q, want open", loadedQ.Status)
	}
	if loadedQ.RunID != run.RunID {
		t.Fatalf("RunQuestion.RunID = %q, want %q", loadedQ.RunID, run.RunID)
	}
	after, err = store.LoadRun(ctx, tenant, board, run.RunID)
	if err != nil {
		t.Fatalf("LoadRun after ask_human: %v", err)
	}
	if after.Status != agent.StatusWaitingOnHuman {
		t.Fatalf("Run.Status = %q, want %q", after.Status, agent.StatusWaitingOnHuman)
	}
	if after.WaitingOn == nil || after.WaitingOn.QuestionID != questionID {
		t.Fatalf("Run.WaitingOn = %+v, want ref to %q", after.WaitingOn, questionID)
	}

	// 4. Resume: marks the question answered, clears Run.WaitingOn,
	// Run.Status back to a non-terminal running-class state.
	resumed, err := coord.Resume(ctx, agent.HumanAnswer{
		TenantID:    tenant,
		BoardID:     board,
		QuestionID:  questionID,
		Answer:      "us-east-1 canary",
		AnsweredBy:  "scott",
		AnsweredVia: "ui",
	})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.WaitingOn != nil {
		t.Fatalf("Run.WaitingOn = %+v, want nil after resume", resumed.WaitingOn)
	}
	if resumed.Status == agent.StatusWaitingOnHuman {
		t.Fatalf("Run.Status = %q, want non-waiting after resume", resumed.Status)
	}
	if isTerminal(resumed.Status) {
		t.Fatalf("Run.Status = %q is terminal after resume", resumed.Status)
	}
	answeredQ, err := store.LoadRunQuestion(ctx, tenant, board, questionID)
	if err != nil {
		t.Fatalf("LoadRunQuestion after resume: %v", err)
	}
	if answeredQ.Status != "answered" || answeredQ.Answer != "us-east-1 canary" {
		t.Fatalf("RunQuestion after resume = %+v, want answered/us-east-1 canary", answeredQ)
	}

	// 4b. Re-resuming the same question is an error — the coordinator
	// refuses double-answers so MCP/voice retries surface explicitly.
	if _, err := coord.Resume(ctx, agent.HumanAnswer{
		TenantID:   tenant,
		BoardID:    board,
		QuestionID: questionID,
		Answer:     "us-west-2 staging",
	}); err == nil {
		t.Fatal("Resume on already-answered question should return an error")
	}

	// 5. Cancel: Run.Status = cancelled, second cancel is a no-op.
	if err := coord.Cancel(ctx, run.RunID, "stopping the demo"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	final, err := store.LoadRun(ctx, tenant, board, run.RunID)
	if err != nil {
		t.Fatalf("LoadRun after cancel: %v", err)
	}
	if final.Status != agent.StatusCancelled {
		t.Fatalf("final.Status = %q, want cancelled", final.Status)
	}
	saveCallsBefore := store.SaveRunCalls()
	if err := coord.Cancel(ctx, run.RunID, "stopping again"); err != nil {
		t.Fatalf("second Cancel returned error, want idempotent nil: %v", err)
	}
	if got := store.SaveRunCalls(); got != saveCallsBefore {
		t.Fatalf("second Cancel triggered %d additional SaveRun calls, want 0", got-saveCallsBefore)
	}
}

// TestRunCoordinatorStartValidatesInputs guards the minimal pre-flight checks
// MCP and UI clients depend on.
func TestRunCoordinatorStartValidatesInputs(t *testing.T) {
	store := mocks.NewRunStore()
	coord := agent.NewSimpleRunCoordinator(store, nil)
	ctx := context.Background()
	if _, err := coord.Start(ctx, agent.RunRequest{Objective: "x"}); err == nil {
		t.Fatal("Start with empty card_id should error")
	}
	if _, err := coord.Start(ctx, agent.RunRequest{CardID: "EMAL-1"}); err == nil {
		t.Fatal("Start with empty objective should error")
	}
}

// TestRunCoordinatorNewQuestionIDMintsUniqueULIDs exercises the ULID helper
// directly so it is covered without depending on the full lifecycle.
func TestRunCoordinatorNewQuestionIDMintsUniqueULIDs(t *testing.T) {
	seen := map[string]struct{}{}
	for i := 0; i < 16; i++ {
		id := agent.NewQuestionID()
		if id == "" {
			t.Fatal("NewQuestionID returned empty")
		}
		if len(id) != 26 {
			t.Fatalf("NewQuestionID %q has %d chars, want 26 (Crockford-base32 ULID)", id, len(id))
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("NewQuestionID returned duplicate %q", id)
		}
		seen[id] = struct{}{}
	}
}

// stepClock returns deterministic, monotonically-advancing timestamps so the
// audit log can be asserted without sleeps.
type stepClock struct {
	t time.Time
}

func newStepClock(start time.Time) *stepClock { return &stepClock{t: start} }

func (c *stepClock) Now() time.Time {
	c.t = c.t.Add(time.Millisecond)
	return c.t
}

// isTerminal mirrors the package-private helper so the external_test package
// can assert on terminal status without exposing it on the public surface.
func isTerminal(status agent.RunStatus) bool {
	switch status {
	case agent.StatusCompleted, agent.StatusFailed, agent.StatusUnsupported, agent.StatusCancelled, agent.StatusTakenOver, agent.StatusRetrying:
		return true
	}
	return false
}
