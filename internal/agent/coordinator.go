package agent

import "context"

// RunRequest captures everything needed to start a Run. Identity is scoped
// (TenantID + BoardID + CardID); the coordinator picks the model and plan.
type RunRequest struct {
	TenantID     string
	BoardID      string
	CardID       string
	Objective    string
	RequestedBy  string         // human identity that initiated the run
	AgentProfile string         // e.g. "swe-1", "project_manager"
	RequestType  string         // e.g. "code_review", "refactor", "auto"
	Constraints  []string       // human-readable guardrails surfaced to the agent
	Metadata     map[string]any // free-form, persisted with the Run
}

// HumanAnswer is the response posted by a human (or by another agent acting
// as a proxy) to resolve a RunQuestion. RunCoordinator.Resume consumes it.
type HumanAnswer struct {
	TenantID    string
	BoardID     string
	QuestionID  string
	Answer      string
	AnsweredBy  string // identity of the responder
	AnsweredVia string // "ui" | "voice" | "mcp"
}

// RunCoordinator drives Run lifecycle: start, advance, pause for human
// input, resume, and cancel. Implementations are provider-specific — the
// existing GitHub PR-review orchestrator in cmd/server is one; future MCP-
// driven runs are another. The contract:
//
//   - Start mints a Run ID, persists the Run in queued/running state, and
//     kicks off execution (typically async). Returns the persisted Run so
//     callers can immediately surface it in the UI.
//   - Checkpoint appends a step transition (started / completed / paused /
//     failed) to the durable audit log and reflects it on Run.Plan.
//   - AskHuman pauses the Run on a RunQuestion attached to the Card. The
//     Run transitions to StatusWaitingOnHuman; the returned QuestionID is
//     ULID-typed (see NewQuestionID).
//   - Resume marks a RunQuestion answered, clears Run.WaitingOn, and
//     transitions the Run back to a running status. Returns the updated
//     Run.
//   - Cancel marks the Run cancelled with the supplied reason. Cancel is
//     idempotent: invoking it on an already-terminal Run is a no-op.
type RunCoordinator interface {
	Start(ctx context.Context, req RunRequest) (Run, error)
	Checkpoint(ctx context.Context, runID string, cp RunStepCheckpoint) error
	AskHuman(ctx context.Context, runID string, q RunQuestion) (string, error)
	Resume(ctx context.Context, answer HumanAnswer) (Run, error)
	Cancel(ctx context.Context, runID string, reason string) error
}
