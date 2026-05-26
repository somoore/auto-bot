package agent

import (
	"context"
	"errors"
)

// ErrRunNotFound is returned by RunStore.LoadRun when no Run exists for the
// requested (tenant, board, runID) tuple. Callers should check with
// errors.Is(err, agent.ErrRunNotFound) to distinguish missing rows from
// transport errors.
var ErrRunNotFound = errors.New("agent: run not found")

// ErrRunQuestionNotFound is returned by RunStore.LoadRunQuestion when no
// RunQuestion exists for the requested (tenant, board, questionID) tuple.
// Use errors.Is(err, agent.ErrRunQuestionNotFound) to detect missing rows.
var ErrRunQuestionNotFound = errors.New("agent: run question not found")

// RunStore is the persistence surface RunCoordinator depends on.
// Implementations live outside internal/agent (cmd/server's sqliteBoardStore
// today, MCP servers tomorrow) so the agent package stays provider-neutral.
//
// Methods are scoped by tenantID + boardID so a single store can host
// multiple tenants' Runs without leakage. Implementations should normalize
// empty tenant IDs to a canonical default (cmd/server uses "default") to
// preserve compatibility with the legacy single-tenant data model.
type RunStore interface {
	// SaveRun persists a Run. The Run is created if (tenantID, boardID,
	// run.RunID) does not exist, or updated in place if it does.
	SaveRun(ctx context.Context, tenantID, boardID string, run Run) error

	// LoadRun returns the persisted Run for (tenantID, boardID, runID).
	// Implementations return ErrRunNotFound (wrappable with %w) when the
	// run does not exist; other errors indicate transport failures.
	LoadRun(ctx context.Context, tenantID, boardID, runID string) (Run, error)

	// AppendRunCheckpoint records one step transition (started, completed,
	// paused, failed) in the durable audit log. Multiple checkpoints per
	// step index are allowed.
	AppendRunCheckpoint(ctx context.Context, tenantID, boardID, runID string, cp RunStepCheckpoint) error

	// ListRunCheckpoints returns the audit-log checkpoints for a Run in
	// chronological (created_at, step_index) order.
	ListRunCheckpoints(ctx context.Context, tenantID, boardID, runID string) ([]RunStepCheckpoint, error)

	// SaveRunQuestion persists a RunQuestion (ask-the-human pause) for a
	// Run. Implementations upsert by (tenantID, boardID, q.ID) so the same
	// question can be transitioned through open -> answered/expired.
	SaveRunQuestion(ctx context.Context, q RunQuestion) error

	// LoadRunQuestion returns the persisted RunQuestion for (tenantID,
	// boardID, questionID). Implementations return ErrRunQuestionNotFound
	// when no row exists.
	LoadRunQuestion(ctx context.Context, tenantID, boardID, questionID string) (RunQuestion, error)

	// ListOpenRunQuestions returns all RunQuestions in the "open" state for
	// the given tenant + board, oldest asked_at first.
	ListOpenRunQuestions(ctx context.Context, tenantID, boardID string) ([]RunQuestion, error)

	// MarkRunQuestionAnswered transitions a RunQuestion from "open" to
	// "answered", stamping the answer body, identity, and channel. It is
	// a no-op-but-error if the question is already terminal.
	MarkRunQuestionAnswered(ctx context.Context, tenantID, boardID, questionID, answer, answeredBy, answeredVia string) error
}
