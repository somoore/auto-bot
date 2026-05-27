package agent

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// SimpleRunCoordinator is the reference RunCoordinator implementation. It
// owns nothing beyond a RunStore: it does not start background workers,
// classify against an LLM, or talk to GitHub. Production lives in
// cmd/server's *agentRunOrchestrator; this exists so:
//
//  1. internal/agent has at least one in-package implementation tested
//     end-to-end against the mock store.
//  2. Sprint 2's MCP server can wrap it (or compose it) without pulling
//     in cmd/server's heavyweight orchestration loop.
//
// SimpleRunCoordinator is the contract executor — it owns the state
// transitions (queued -> running, ask -> waiting_on_human, resume ->
// running, cancel -> cancelled) and the durable audit-log appends. Plan
// generation, model calls, and external publish hooks are out of scope.
type SimpleRunCoordinator struct {
	store RunStore
	now   func() time.Time
	// scope maps run_id -> (tenant_id, board_id) so Checkpoint / AskHuman /
	// Cancel can look the Run up without re-passing scope through every
	// method. The map is populated by Start and Resume (the entry points
	// that know scope) and read by the scope-less mutators. Sprint 2's
	// MCP transport threads scope through the call args, not the
	// coordinator instance, so MCP-driven implementations can ignore this
	// helper.
	scopeMu sync.Mutex
	scope   map[string]runScope
}

type runScope struct {
	tenantID string
	boardID  string
}

// Compile-time check.
var _ RunCoordinator = (*SimpleRunCoordinator)(nil)

// NewSimpleRunCoordinator returns a RunCoordinator backed by the supplied
// RunStore. Now is the clock used for CreatedAt / UpdatedAt / checkpoint
// timestamps; pass nil for time.Now (UTC).
func NewSimpleRunCoordinator(store RunStore, now func() time.Time) *SimpleRunCoordinator {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &SimpleRunCoordinator{store: store, now: now, scope: map[string]runScope{}}
}

func (coord *SimpleRunCoordinator) rememberScope(runID, tenantID, boardID string) {
	coord.scopeMu.Lock()
	defer coord.scopeMu.Unlock()
	coord.scope[runID] = runScope{tenantID: tenantID, boardID: boardID}
}

func (coord *SimpleRunCoordinator) lookupScope(runID string) (runScope, bool) {
	coord.scopeMu.Lock()
	defer coord.scopeMu.Unlock()
	s, ok := coord.scope[runID]
	return s, ok
}

// Start mints a Run ID, persists the Run in "running" state, and returns
// the snapshot. Callers drive subsequent transitions via Checkpoint /
// AskHuman / Resume / Cancel.
func (coord *SimpleRunCoordinator) Start(ctx context.Context, req RunRequest) (Run, error) {
	if coord == nil || coord.store == nil {
		return Run{}, fmt.Errorf("simple coordinator: store is nil")
	}
	if req.CardID == "" {
		return Run{}, fmt.Errorf("simple coordinator Start: card_id is required")
	}
	if req.Objective == "" {
		return Run{}, fmt.Errorf("simple coordinator Start: objective is required")
	}
	now := coord.now().UTC().Format(time.RFC3339Nano)
	run := Run{
		RunID:        NewRunID(),
		TenantID:     req.TenantID,
		BoardID:      req.BoardID,
		CardID:       req.CardID,
		Objective:    req.Objective,
		RequestedBy:  req.RequestedBy,
		AgentProfile: req.AgentProfile,
		RequestType:  req.RequestType,
		Status:       StatusQueued,
		CurrentStep:  "Queued by coordinator.",
		CreatedAt:    now,
		UpdatedAt:    now,
		StartedAt:    now,
	}
	run.AddCheckpoint(StatusQueued, "queued", "Run queued by coordinator.")
	if err := coord.store.SaveRun(ctx, req.TenantID, req.BoardID, run); err != nil {
		return Run{}, fmt.Errorf("save run: %w", err)
	}
	coord.rememberScope(run.RunID, req.TenantID, req.BoardID)
	return run, nil
}

// Checkpoint appends a RunStepCheckpoint to the audit log and reflects the
// transition on Run.Plan. The Run is re-saved so consumers polling LoadRun
// see the plan progress.
func (coord *SimpleRunCoordinator) Checkpoint(ctx context.Context, runID string, cp RunStepCheckpoint) error {
	if coord == nil || coord.store == nil {
		return fmt.Errorf("simple coordinator: store is nil")
	}
	if runID == "" {
		return fmt.Errorf("simple coordinator Checkpoint: run_id is required")
	}
	if cp.Kind == "" {
		return fmt.Errorf("simple coordinator Checkpoint: kind is required")
	}
	if cp.CreatedAt == "" {
		cp.CreatedAt = coord.now().UTC().Format(time.RFC3339Nano)
	}

	run, tenantID, boardID, err := coord.findRun(ctx, runID)
	if err != nil {
		return err
	}
	if err := coord.store.AppendRunCheckpoint(ctx, tenantID, boardID, runID, cp); err != nil {
		return fmt.Errorf("append checkpoint: %w", err)
	}
	ApplyCheckpointToPlan(&run, cp)
	run.UpdatedAt = coord.now().UTC().Format(time.RFC3339Nano)
	return coord.store.SaveRun(ctx, tenantID, boardID, run)
}

// AskHuman pauses the run on a RunQuestion and returns the ULID that
// identifies it. Empty q.ID triggers ULID minting.
func (coord *SimpleRunCoordinator) AskHuman(ctx context.Context, runID string, q RunQuestion) (string, error) {
	if coord == nil || coord.store == nil {
		return "", fmt.Errorf("simple coordinator: store is nil")
	}
	if runID == "" {
		return "", fmt.Errorf("simple coordinator AskHuman: run_id is required")
	}
	if q.Prompt == "" {
		return "", fmt.Errorf("simple coordinator AskHuman: prompt is required")
	}
	run, tenantID, boardID, err := coord.findRun(ctx, runID)
	if err != nil {
		return "", err
	}
	if q.ID == "" {
		q.ID = NewQuestionID()
	}
	if q.AskedAt == "" {
		q.AskedAt = coord.now().UTC().Format(time.RFC3339Nano)
	}
	q.RunID = runID
	q.TenantID = tenantID
	q.BoardID = boardID
	q.CardID = run.CardID
	q.Status = "open"
	if err := coord.store.SaveRunQuestion(ctx, q); err != nil {
		return "", fmt.Errorf("save run question: %w", err)
	}

	run.Status = StatusWaitingOnHuman
	run.WaitingOn = &RunQuestionRef{
		QuestionID: q.ID,
		Prompt:     q.Prompt,
		AskedAt:    q.AskedAt,
	}
	run.AddCheckpoint(StatusWaitingOnHuman, "ask_human", q.Prompt)
	run.UpdatedAt = coord.now().UTC().Format(time.RFC3339Nano)
	ApplyCheckpointToPlan(&run, RunStepCheckpoint{
		StepIndex: q.StepIndex,
		Kind:      CheckpointKindPaused,
		CreatedAt: q.AskedAt,
	})
	if err := coord.store.SaveRun(ctx, tenantID, boardID, run); err != nil {
		return "", fmt.Errorf("save run after ask_human: %w", err)
	}
	return q.ID, nil
}

// Resume marks the RunQuestion answered, clears Run.WaitingOn, and pushes
// the Run back to a running status (StatusReviewing as a conservative
// default — production orchestrators may restore a more specific state).
//
// Resuming a question that is already answered returns an error. Callers
// that need idempotent retries should check this with a simple text match
// or treat it as a no-op at the MCP layer.
func (coord *SimpleRunCoordinator) Resume(ctx context.Context, answer HumanAnswer) (Run, error) {
	if coord == nil || coord.store == nil {
		return Run{}, fmt.Errorf("simple coordinator: store is nil")
	}
	if answer.QuestionID == "" {
		return Run{}, fmt.Errorf("simple coordinator Resume: question_id is required")
	}
	question, err := coord.store.LoadRunQuestion(ctx, answer.TenantID, answer.BoardID, answer.QuestionID)
	if err != nil {
		return Run{}, fmt.Errorf("load run question: %w", err)
	}
	if question.Status == "answered" {
		return Run{}, fmt.Errorf("run question %s is already answered", answer.QuestionID)
	}
	// DA-1: refuse to mark an expired question as answered. See the
	// matching guard in cmd/server/agent_coordinator.go's Resume — both
	// implementations must agree so MCP clients see consistent semantics.
	if question.Status == "expired" {
		return Run{}, fmt.Errorf("resume: question %s: %w", answer.QuestionID, ErrRunQuestionExpired)
	}
	if err := coord.store.MarkRunQuestionAnswered(ctx, answer.TenantID, answer.BoardID, answer.QuestionID, answer.Answer, answer.AnsweredBy, answer.AnsweredVia); err != nil {
		return Run{}, fmt.Errorf("mark run question answered: %w", err)
	}
	run, err := coord.store.LoadRun(ctx, question.TenantID, question.BoardID, question.RunID)
	if err != nil {
		return Run{}, fmt.Errorf("load run after resume: %w", err)
	}
	run.WaitingOn = nil
	if run.Status == StatusWaitingOnHuman {
		run.Status = StatusReviewing
	}
	run.UpdatedAt = coord.now().UTC().Format(time.RFC3339Nano)
	run.AddCheckpoint(run.Status, "resume", fmt.Sprintf("Resumed after answer from %s via %s.", answer.AnsweredBy, answer.AnsweredVia))
	if err := coord.store.SaveRun(ctx, question.TenantID, question.BoardID, run); err != nil {
		return Run{}, fmt.Errorf("save run after resume: %w", err)
	}
	// Cache scope so a later Cancel for the same Run (driven by a
	// different process or a coordinator that did not see the original
	// Start) can route without an extra LoadRun.
	coord.rememberScope(run.RunID, question.TenantID, question.BoardID)
	return run, nil
}

// Cancel marks the Run cancelled with the supplied reason. Idempotent:
// calling Cancel on an already-terminal Run is a no-op and returns nil.
func (coord *SimpleRunCoordinator) Cancel(ctx context.Context, runID string, reason string) error {
	if coord == nil || coord.store == nil {
		return fmt.Errorf("simple coordinator: store is nil")
	}
	if runID == "" {
		return fmt.Errorf("simple coordinator Cancel: run_id is required")
	}
	run, tenantID, boardID, err := coord.findRun(ctx, runID)
	if err != nil {
		return err
	}
	if isTerminal(run.Status) {
		return nil
	}
	if reason == "" {
		reason = "Cancelled by coordinator."
	}
	run.Status = StatusCancelled
	run.CurrentStep = reason
	run.Summary = reason
	now := coord.now().UTC().Format(time.RFC3339Nano)
	run.CompletedAt = now
	run.UpdatedAt = now
	run.AddCheckpoint(StatusCancelled, "cancelled", reason)
	return coord.store.SaveRun(ctx, tenantID, boardID, run)
}

// findRun loads a Run by ID using the scope that Start (or Resume) saw at
// creation time. SimpleRunCoordinator caches that scope so Checkpoint /
// AskHuman / Cancel can route to the right tenant/board partition without
// re-passing it on every call.
func (coord *SimpleRunCoordinator) findRun(ctx context.Context, runID string) (Run, string, string, error) {
	scope, ok := coord.lookupScope(runID)
	if !ok {
		return Run{}, "", "", fmt.Errorf("run %s has no cached scope; call Start first", runID)
	}
	run, err := coord.store.LoadRun(ctx, scope.tenantID, scope.boardID, runID)
	if err != nil {
		return Run{}, "", "", fmt.Errorf("load run %s: %w", runID, err)
	}
	return run, scope.tenantID, scope.boardID, nil
}

// ApplyCheckpointToPlan reflects a RunStepCheckpoint onto Run.Plan. Both
// SimpleRunCoordinator and cmd/server's PR-review orchestrator call this
// helper so plan-step transitions stay in lockstep regardless of which
// coordinator drove them. Plan entries are addressed by 1-based StepIndex;
// missing entries are ignored so the audit log can record runs that
// pre-date a plan.
func ApplyCheckpointToPlan(run *Run, cp RunStepCheckpoint) {
	if cp.StepIndex <= 0 {
		return
	}
	for i := range run.Plan {
		if run.Plan[i].Index != cp.StepIndex {
			continue
		}
		switch cp.Kind {
		case CheckpointKindStarted:
			run.Plan[i].Status = "running"
			if run.Plan[i].StartedAt == "" {
				run.Plan[i].StartedAt = cp.CreatedAt
			}
		case CheckpointKindCompleted:
			run.Plan[i].Status = "done"
			run.Plan[i].CompletedAt = cp.CreatedAt
		case CheckpointKindPaused:
			run.Plan[i].Status = "paused"
		case CheckpointKindFailed:
			run.Plan[i].Status = "failed"
			run.Plan[i].CompletedAt = cp.CreatedAt
		}
		return
	}
}

func isTerminal(status RunStatus) bool {
	switch status {
	case StatusCompleted, StatusFailed, StatusUnsupported, StatusCancelled, StatusTakenOver, StatusRetrying:
		return true
	}
	return false
}
