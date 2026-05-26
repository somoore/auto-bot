package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/somoore/auto-bot/internal/agent"
)

// Compile-time check that *agentRunOrchestrator satisfies the
// agent.RunCoordinator surface MCP tools (Sprint 2) consume. The
// per-method docs on each implementation note the existing PR-review
// loop it wraps.
var _ agent.RunCoordinator = (*agentRunOrchestrator)(nil)

// Start mints a Run ID, persists the Run via assignTicketToAgent, and kicks
// off the PR-review loop in the background. The returned Run is the
// freshly-persisted snapshot — callers can immediately surface it in the UI
// even though execution is still in flight.
//
// This is the constructor side of the coordinator. The legacy run-loop
// driver lives at (*agentRunOrchestrator).executeRun.
func (orchestrator *agentRunOrchestrator) Start(ctx context.Context, req agent.RunRequest) (agent.Run, error) {
	if orchestrator == nil || orchestrator.board == nil {
		return agent.Run{}, fmt.Errorf("agent orchestrator is not configured")
	}
	if err := ctx.Err(); err != nil {
		return agent.Run{}, err
	}
	if req.CardID == "" {
		return agent.Run{}, fmt.Errorf("run request requires card_id")
	}
	if req.Objective == "" {
		return agent.Run{}, fmt.Errorf("run request requires objective")
	}

	args := map[string]any{
		"card_id":       req.CardID,
		"objective":     req.Objective,
		"agent_profile": req.AgentProfile,
		"request_type":  req.RequestType,
		"requested_by":  req.RequestedBy,
	}
	if repo, ok := req.Metadata["repo"].(string); ok {
		args["repo"] = repo
	}
	if branch, ok := req.Metadata["branch"].(string); ok {
		args["branch"] = branch
	}
	if pr, ok := metadataInt(req.Metadata, "pull_request_number"); ok {
		args["pull_request_number"] = pr
	}

	result, _, err := orchestrator.board.assignTicketToAgent(args)
	if err != nil {
		return agent.Run{}, err
	}
	runID, _ := result["run_id"].(string)
	if runID == "" {
		return agent.Run{}, fmt.Errorf("assign_ticket_to_agent did not return a run_id")
	}
	run, ok := orchestrator.board.agentRunByID(runID)
	if !ok {
		return agent.Run{}, fmt.Errorf("agent run %s vanished after persistence", runID)
	}
	return run, nil
}

// Checkpoint appends a durable RunStepCheckpoint to the audit log and
// reflects the transition on Run.Plan (started -> running, completed ->
// done, paused -> paused, failed -> failed). Plan entries are matched by
// 1-based StepIndex.
func (orchestrator *agentRunOrchestrator) Checkpoint(ctx context.Context, runID string, cp agent.RunStepCheckpoint) error {
	if orchestrator == nil || orchestrator.board == nil {
		return fmt.Errorf("agent orchestrator is not configured")
	}
	if runID == "" {
		return fmt.Errorf("checkpoint requires run_id")
	}
	if cp.Kind == "" {
		return fmt.Errorf("checkpoint requires kind")
	}
	if cp.CreatedAt == "" {
		cp.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}

	if store, ok := orchestrator.board.store.(agent.RunStore); ok {
		if err := store.AppendRunCheckpoint(ctx, orchestrator.board.tenantID, orchestrator.board.boardID, runID, cp); err != nil {
			return fmt.Errorf("append run checkpoint: %w", err)
		}
	}

	if err := orchestrator.board.updateAgentRun(ctx, runID, func(next *agentRun) {
		agent.ApplyCheckpointToPlan(next, cp)
	}); err != nil {
		return fmt.Errorf("checkpoint: persist run plan: %w", err)
	}
	return nil
}

// AskHuman pauses the run on a RunQuestion and persists the question through
// the underlying RunStore. The question's ID is minted via agent.NewQuestionID
// when the caller leaves it empty so the orchestrator owns ULID assignment.
// The Run transitions to StatusWaitingOnHuman with WaitingOn set to a
// lightweight pointer for the drawer.
func (orchestrator *agentRunOrchestrator) AskHuman(ctx context.Context, runID string, q agent.RunQuestion) (string, error) {
	if orchestrator == nil || orchestrator.board == nil {
		return "", fmt.Errorf("agent orchestrator is not configured")
	}
	if runID == "" {
		return "", fmt.Errorf("ask_human requires run_id")
	}
	if q.Prompt == "" {
		return "", fmt.Errorf("ask_human requires prompt")
	}
	if q.ID == "" {
		q.ID = agent.NewQuestionID()
	}
	if q.TenantID == "" {
		q.TenantID = orchestrator.board.tenantID
	}
	if q.BoardID == "" {
		q.BoardID = orchestrator.board.boardID
	}
	q.RunID = runID
	if q.AskedAt == "" {
		q.AskedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	q.Status = "open"

	store, ok := orchestrator.board.store.(agent.RunStore)
	if !ok {
		return "", fmt.Errorf("ask_human requires an agent.RunStore-backed board store")
	}
	if err := store.SaveRunQuestion(ctx, q); err != nil {
		return "", fmt.Errorf("save run question: %w", err)
	}

	ref := agent.RunQuestionRef{QuestionID: q.ID, Prompt: q.Prompt, AskedAt: q.AskedAt}
	if err := orchestrator.board.updateAgentRun(ctx, runID, func(next *agentRun) {
		if agentRunIsTerminal(next.Status) {
			return
		}
		next.Status = agentRunWaitingOnHuman
		next.WaitingOn = &ref
		next.AddCheckpoint(agentRunWaitingOnHuman, "ask_human", q.Prompt)
		// Mark the plan step paused so timeline replay can see the gap.
		agent.ApplyCheckpointToPlan(next, agent.RunStepCheckpoint{
			StepIndex: q.StepIndex,
			Kind:      agent.CheckpointKindPaused,
			CreatedAt: q.AskedAt,
		})
	}); err != nil {
		return "", fmt.Errorf("ask_human: persist waiting state: %w", err)
	}

	// Surface the ask-the-human pause over WebSocket. run_question_asked
	// carries the full question for drawers/MCP consumers; run_paused
	// carries the updated Run view so timeline UIs reflect the
	// waiting_on_human transition without a separate fetch.
	broadcastKanbanEventForBoard(orchestrator.board.tenantID, orchestrator.board.boardID, "run_question_asked", q)
	if updated, ok := orchestrator.board.agentRunByID(runID); ok {
		broadcastKanbanEventForBoard(orchestrator.board.tenantID, orchestrator.board.boardID, "run_paused", updated.View())
	}
	broadcastKanbanEventForBoard(orchestrator.board.tenantID, orchestrator.board.boardID, "board", orchestrator.board.SnapshotState())
	return q.ID, nil
}

// Resume marks the RunQuestion answered, clears Run.WaitingOn, and pushes
// the Run back to a running status. The orchestrator-level execution loop
// is responsible for picking the next plan step on the next iteration; this
// method only owns the state transition.
//
// Resuming an already-answered question returns an error so the caller can
// distinguish double-answer attempts (UI race, voice retry, MCP retry) from
// genuine progress. Coordinator clients should idempotently treat
// already-answered as a no-op via errors.Is.
func (orchestrator *agentRunOrchestrator) Resume(ctx context.Context, answer agent.HumanAnswer) (agent.Run, error) {
	if orchestrator == nil || orchestrator.board == nil {
		return agent.Run{}, fmt.Errorf("agent orchestrator is not configured")
	}
	if answer.QuestionID == "" {
		return agent.Run{}, fmt.Errorf("resume requires question_id")
	}
	tenantID := answer.TenantID
	if tenantID == "" {
		tenantID = orchestrator.board.tenantID
	}
	boardID := answer.BoardID
	if boardID == "" {
		boardID = orchestrator.board.boardID
	}

	store, ok := orchestrator.board.store.(agent.RunStore)
	if !ok {
		return agent.Run{}, fmt.Errorf("resume requires an agent.RunStore-backed board store")
	}
	existing, err := store.LoadRunQuestion(ctx, tenantID, boardID, answer.QuestionID)
	if err != nil {
		return agent.Run{}, fmt.Errorf("load run question: %w", err)
	}
	if existing.Status == "answered" {
		return agent.Run{}, fmt.Errorf("run question %s is already answered", answer.QuestionID)
	}
	// DA-1: refuse to mark an expired question as answered. The TTL sweeper
	// may have transitioned the question to "expired" between the
	// ask-the-human broadcast and the human answer; advancing it through
	// "answered" would zombie the Run (the sweeper already cleared
	// WaitingOn and the answer would be applied to a timed-out prompt).
	// Callers branch on agent.ErrRunQuestionExpired to surface a distinct
	// "ask timed out; restart the question" affordance.
	if existing.Status == "expired" {
		return agent.Run{}, fmt.Errorf("resume: question %s: %w", answer.QuestionID, agent.ErrRunQuestionExpired)
	}
	if err := store.MarkRunQuestionAnswered(ctx, tenantID, boardID, answer.QuestionID, answer.Answer, answer.AnsweredBy, answer.AnsweredVia); err != nil {
		return agent.Run{}, fmt.Errorf("mark run question answered: %w", err)
	}

	if err := orchestrator.board.updateAgentRun(ctx, existing.RunID, func(next *agentRun) {
		next.WaitingOn = nil
		if next.Status == agentRunWaitingOnHuman {
			next.Status = agentRunReviewing
		}
		next.AddCheckpoint(next.Status, "resume", fmt.Sprintf("Resumed after answer from %s via %s.", answer.AnsweredBy, answer.AnsweredVia))
	}); err != nil {
		return agent.Run{}, fmt.Errorf("resume: persist run state: %w", err)
	}
	run, ok := orchestrator.board.agentRunByID(existing.RunID)
	if !ok {
		return agent.Run{}, fmt.Errorf("run %s vanished during resume", existing.RunID)
	}

	// Reload the question so the broadcast carries answered_at, answered_by,
	// and answered_via for clients that surface the answer alongside the
	// prompt. run_resumed carries the Run so timeline UIs can paint the
	// transition out of waiting_on_human.
	answered, loadErr := store.LoadRunQuestion(ctx, tenantID, boardID, answer.QuestionID)
	if loadErr != nil {
		// The answer was persisted; missing-question on reload is an audit
		// concern but should not fail the Resume. Fall back to the request
		// shape so downstream clients still see the transition.
		answered = existing
		answered.Answer = answer.Answer
		answered.AnsweredBy = answer.AnsweredBy
		answered.AnsweredVia = answer.AnsweredVia
		answered.Status = "answered"
	}
	broadcastKanbanEventForBoard(orchestrator.board.tenantID, orchestrator.board.boardID, "run_question_answered", answered)
	broadcastKanbanEventForBoard(orchestrator.board.tenantID, orchestrator.board.boardID, "run_resumed", run.View())
	broadcastKanbanEventForBoard(orchestrator.board.tenantID, orchestrator.board.boardID, "board", orchestrator.board.SnapshotState())
	return run, nil
}

// Cancel marks a Run cancelled with the supplied reason. The cancel is
// idempotent: calling it on an already-terminal Run returns nil so MCP
// retries do not produce errors. Errors only surface if the Run does not
// exist or the underlying mutation fails.
func (orchestrator *agentRunOrchestrator) Cancel(ctx context.Context, runID string, reason string) error {
	if orchestrator == nil || orchestrator.board == nil {
		return fmt.Errorf("agent orchestrator is not configured")
	}
	if runID == "" {
		return fmt.Errorf("cancel requires run_id")
	}
	existing, ok := orchestrator.board.agentRunByID(runID)
	if !ok {
		return fmt.Errorf("agent run %s not found", runID)
	}
	if agentRunIsTerminal(existing.Status) {
		return nil
	}
	if reason == "" {
		reason = "Cancelled by coordinator."
	}
	if err := orchestrator.board.updateAgentRun(ctx, runID, func(next *agentRun) {
		if agentRunIsTerminal(next.Status) {
			return
		}
		next.Status = agentRunCancelled
		next.CurrentStep = reason
		next.Summary = reason
		next.CompletedAt = nowRFC3339Nano()
		next.AddCheckpoint(agentRunCancelled, "cancelled", reason)
	}); err != nil {
		return fmt.Errorf("cancel: persist run state: %w", err)
	}
	return nil
}

// metadataInt pulls an int-shaped value out of a free-form RunRequest.Metadata
// map. JSON-decoded payloads typically arrive as float64; raw Go ints and
// string-encoded ints are also accepted.
func metadataInt(metadata map[string]any, key string) (int, bool) {
	if len(metadata) == 0 {
		return 0, false
	}
	raw, ok := metadata[key]
	if !ok {
		return 0, false
	}
	switch v := raw.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i), true
		}
	case string:
		if i, ok := asInt(v); ok {
			return i, true
		}
	}
	return 0, false
}
