package agent

// RunStepCheckpoint is the durable per-step audit-log entry persisted in the
// run_checkpoints table. It is intentionally distinct from Checkpoint:
//   - Checkpoint is the UI projection threaded through Run.Checkpoints,
//     capped at 50 entries and shown in the live run drawer.
//   - RunStepCheckpoint is the durable per-step audit log keyed by
//     (run_id, step_index, kind, created_at) and used by the
//     RunCoordinator.Checkpoint method to record step transitions.
//
// Kinds map onto the orchestrator's plan-step state machine:
//
//	started   — the step has begun executing.
//	completed — the step finished successfully; Plan[step].Status -> "done".
//	paused    — the step paused waiting on an external signal (e.g. human
//	            answer); Plan[step].Status -> "paused".
//	failed    — the step terminated with an error.
//
// PayloadJSON is opaque to the store so the schema can evolve without
// migrations; coordinator-level code is responsible for marshalling.
type RunStepCheckpoint struct {
	StepIndex   int    `json:"step_index"`
	Kind        string `json:"kind"`
	PayloadJSON string `json:"payload_json"`
	CreatedAt   string `json:"created_at"`
}

// Canonical RunStepCheckpoint kinds. Keep in sync with the documentation on
// RunStepCheckpoint and the orchestrator's plan-step transitions.
const (
	CheckpointKindStarted   = "started"
	CheckpointKindCompleted = "completed"
	CheckpointKindPaused    = "paused"
	CheckpointKindFailed    = "failed"
)
