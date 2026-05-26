package mocks

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/somoore/auto-bot/internal/agent"
)

// RunStore is an in-memory agent.RunStore for tests. It satisfies the full
// agent.RunStore contract: Runs, RunStepCheckpoints, and RunQuestions are
// keyed by (tenant_id, board_id, ...) so coordinator and MCP tests can share
// a single instance across multiple tenants without bleed.
//
// The store is safe for concurrent use; every method takes the mutex.
type RunStore struct {
	mu            sync.Mutex
	runs          map[runKey]agent.Run
	checkpoints   map[runKey][]agent.RunStepCheckpoint
	questions     map[questionKey]agent.RunQuestion
	now           func() time.Time
	saveRunCalls  int
	loadRunCalls  int
	questionMints int
}

type runKey struct {
	tenant string
	board  string
	run    string
}

type questionKey struct {
	tenant   string
	board    string
	question string
}

// Compile-time check: the mock satisfies the production interface so MCP
// tests can swap it in without an adapter shim.
var _ agent.RunStore = (*RunStore)(nil)

// NewRunStore returns an empty in-memory store. Callers can override the
// clock for deterministic tests.
func NewRunStore() *RunStore {
	return &RunStore{
		runs:        map[runKey]agent.Run{},
		checkpoints: map[runKey][]agent.RunStepCheckpoint{},
		questions:   map[questionKey]agent.RunQuestion{},
		now:         func() time.Time { return time.Now().UTC() },
	}
}

// WithClock injects a deterministic time source. Used by tests that need
// to assert checkpoint ordering or RunQuestion TTLs without sleeps.
func (store *RunStore) WithClock(now func() time.Time) *RunStore {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.now = now
	return store
}

// SaveRunCalls returns the number of SaveRun invocations observed since the
// store was created. Tests use it to assert how many times the coordinator
// persisted state changes.
func (store *RunStore) SaveRunCalls() int {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.saveRunCalls
}

// SaveRun upserts a Run keyed by (tenant, board, run.RunID).
func (store *RunStore) SaveRun(_ context.Context, tenantID, boardID string, run agent.Run) error {
	if run.RunID == "" {
		return fmt.Errorf("mock SaveRun requires run.RunID")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if run.TenantID == "" {
		run.TenantID = tenantID
	}
	if run.BoardID == "" {
		run.BoardID = boardID
	}
	store.runs[runKey{tenantID, boardID, run.RunID}] = run
	store.saveRunCalls++
	return nil
}

// LoadRun returns the persisted Run or agent.ErrRunNotFound.
func (store *RunStore) LoadRun(_ context.Context, tenantID, boardID, runID string) (agent.Run, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.loadRunCalls++
	run, ok := store.runs[runKey{tenantID, boardID, runID}]
	if !ok {
		return agent.Run{}, fmt.Errorf("mock LoadRun %s: %w", runID, agent.ErrRunNotFound)
	}
	return run, nil
}

// AppendRunCheckpoint appends an audit-log entry. Order preserved per Run.
func (store *RunStore) AppendRunCheckpoint(_ context.Context, tenantID, boardID, runID string, cp agent.RunStepCheckpoint) error {
	if runID == "" {
		return fmt.Errorf("mock AppendRunCheckpoint requires run_id")
	}
	if cp.Kind == "" {
		return fmt.Errorf("mock AppendRunCheckpoint requires kind")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if cp.CreatedAt == "" {
		cp.CreatedAt = store.now().Format(time.RFC3339Nano)
	}
	key := runKey{tenantID, boardID, runID}
	store.checkpoints[key] = append(store.checkpoints[key], cp)
	return nil
}

// ListRunCheckpoints returns the audit log for a Run in append order.
func (store *RunStore) ListRunCheckpoints(_ context.Context, tenantID, boardID, runID string) ([]agent.RunStepCheckpoint, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	src := store.checkpoints[runKey{tenantID, boardID, runID}]
	if len(src) == 0 {
		return nil, nil
	}
	out := make([]agent.RunStepCheckpoint, len(src))
	copy(out, src)
	return out, nil
}

// SaveRunQuestion upserts a RunQuestion keyed by (tenant, board, q.ID).
func (store *RunStore) SaveRunQuestion(_ context.Context, q agent.RunQuestion) error {
	if q.ID == "" {
		return fmt.Errorf("mock SaveRunQuestion requires q.ID")
	}
	if q.BoardID == "" {
		return fmt.Errorf("mock SaveRunQuestion requires q.BoardID")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if q.Status == "" {
		q.Status = "open"
	}
	if q.AskedAt == "" {
		q.AskedAt = store.now().Format(time.RFC3339Nano)
	}
	store.questions[questionKey{q.TenantID, q.BoardID, q.ID}] = q
	if q.Status == "open" {
		store.questionMints++
	}
	return nil
}

// LoadRunQuestion returns the question or agent.ErrRunQuestionNotFound.
func (store *RunStore) LoadRunQuestion(_ context.Context, tenantID, boardID, questionID string) (agent.RunQuestion, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	q, ok := store.questions[questionKey{tenantID, boardID, questionID}]
	if !ok {
		return agent.RunQuestion{}, fmt.Errorf("mock LoadRunQuestion %s: %w", questionID, agent.ErrRunQuestionNotFound)
	}
	return q, nil
}

// ListOpenRunQuestions returns open questions in asked_at order.
func (store *RunStore) ListOpenRunQuestions(_ context.Context, tenantID, boardID string) ([]agent.RunQuestion, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	var out []agent.RunQuestion
	for _, q := range store.questions {
		if q.TenantID != tenantID || q.BoardID != boardID {
			continue
		}
		if q.Status != "open" {
			continue
		}
		out = append(out, q)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AskedAt < out[j].AskedAt })
	return out, nil
}

// MarkRunQuestionAnswered transitions a question to "answered". Returns an
// error if the question does not exist; double-answering is allowed by the
// mock (the coordinator layer is responsible for refusing re-answers).
func (store *RunStore) MarkRunQuestionAnswered(_ context.Context, tenantID, boardID, questionID, answer, answeredBy, answeredVia string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	key := questionKey{tenantID, boardID, questionID}
	q, ok := store.questions[key]
	if !ok {
		return fmt.Errorf("mock MarkRunQuestionAnswered %s: %w", questionID, agent.ErrRunQuestionNotFound)
	}
	q.Answer = answer
	q.AnsweredBy = answeredBy
	q.AnsweredVia = answeredVia
	q.AnsweredAt = store.now().Format(time.RFC3339Nano)
	q.Status = "answered"
	store.questions[key] = q
	return nil
}
