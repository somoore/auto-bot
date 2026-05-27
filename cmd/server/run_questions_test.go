package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/somoore/auto-bot/internal/agent"
)

func newTestSQLiteBoardStore(t *testing.T) *sqliteBoardStore {
	t.Helper()
	store, err := newSQLiteBoardStore(filepath.Join(t.TempDir(), "board.sqlite"))
	if err != nil {
		t.Fatalf("newSQLiteBoardStore returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close board store: %v", err)
		}
	})
	return store
}

func sampleRunQuestion(id string, askedAt time.Time, ttlSeconds int) agent.RunQuestion {
	return agent.RunQuestion{
		ID:          id,
		TenantID:    defaultTenantID,
		BoardID:     "team-board",
		RunID:       "agent-run-1",
		CardID:      "EMAL-12",
		StepIndex:   3,
		Prompt:      "Which target environment should the rollout go to first?",
		Reasoning:   "Card mentions canary but does not say which region.",
		Suggestions: []string{"us-east-1 canary", "us-west-2 staging"},
		AskedAt:     askedAt.UTC().Format(time.RFC3339Nano),
		TTLSeconds:  ttlSeconds,
		Status:      "open",
	}
}

func TestRunQuestionRoundTrip(t *testing.T) {
	store := newTestSQLiteBoardStore(t)
	ctx := context.Background()

	q := sampleRunQuestion("q-roundtrip-1", time.Now(), 14400)
	if err := store.SaveRunQuestion(ctx, q); err != nil {
		t.Fatalf("SaveRunQuestion: %v", err)
	}

	loaded, err := store.LoadRunQuestion(ctx, defaultTenantID, "team-board", q.ID)
	if err != nil {
		t.Fatalf("LoadRunQuestion: %v", err)
	}
	if loaded.ID != q.ID || loaded.RunID != q.RunID || loaded.CardID != q.CardID {
		t.Fatalf("loaded ids = %+v, want %+v", loaded, q)
	}
	if loaded.Prompt != q.Prompt || loaded.Reasoning != q.Reasoning {
		t.Fatalf("loaded text = %+v, want %+v", loaded, q)
	}
	if len(loaded.Suggestions) != len(q.Suggestions) {
		t.Fatalf("loaded suggestions = %v, want %v", loaded.Suggestions, q.Suggestions)
	}
	for i, suggestion := range q.Suggestions {
		if loaded.Suggestions[i] != suggestion {
			t.Fatalf("loaded suggestion[%d] = %q, want %q", i, loaded.Suggestions[i], suggestion)
		}
	}
	if loaded.TTLSeconds != q.TTLSeconds {
		t.Fatalf("loaded TTLSeconds = %d, want %d", loaded.TTLSeconds, q.TTLSeconds)
	}
	if loaded.Status != "open" {
		t.Fatalf("loaded Status = %q, want open", loaded.Status)
	}
}

func TestRunQuestionListOpenOnlyReturnsOpen(t *testing.T) {
	store := newTestSQLiteBoardStore(t)
	ctx := context.Background()

	now := time.Now()
	open := sampleRunQuestion("q-open-1", now, 14400)
	answered := sampleRunQuestion("q-answered-1", now, 14400)
	expired := sampleRunQuestion("q-expired-1", now.Add(-2*time.Hour), 60)

	for _, q := range []agent.RunQuestion{open, answered, expired} {
		if err := store.SaveRunQuestion(ctx, q); err != nil {
			t.Fatalf("SaveRunQuestion(%s): %v", q.ID, err)
		}
	}

	if err := store.MarkRunQuestionAnswered(ctx, defaultTenantID, "team-board", answered.ID, "use us-east-1", "scott", "ui"); err != nil {
		t.Fatalf("MarkRunQuestionAnswered: %v", err)
	}
	if n, err := store.ExpireRunQuestions(ctx, defaultTenantID, "team-board", now); err != nil {
		t.Fatalf("ExpireRunQuestions: %v", err)
	} else if n != 1 {
		t.Fatalf("ExpireRunQuestions returned %d, want 1", n)
	}

	open_, err := store.ListOpenRunQuestions(ctx, defaultTenantID, "team-board")
	if err != nil {
		t.Fatalf("ListOpenRunQuestions: %v", err)
	}
	if len(open_) != 1 {
		t.Fatalf("ListOpenRunQuestions returned %d, want 1: %+v", len(open_), open_)
	}
	if open_[0].ID != open.ID {
		t.Fatalf("ListOpenRunQuestions returned %q, want %q", open_[0].ID, open.ID)
	}
}

func TestRunQuestionMarkAnswered(t *testing.T) {
	store := newTestSQLiteBoardStore(t)
	ctx := context.Background()

	q := sampleRunQuestion("q-answer-1", time.Now(), 14400)
	if err := store.SaveRunQuestion(ctx, q); err != nil {
		t.Fatalf("SaveRunQuestion: %v", err)
	}

	if err := store.MarkRunQuestionAnswered(ctx, defaultTenantID, "team-board", q.ID, "us-east-1 canary", "scott", "voice"); err != nil {
		t.Fatalf("MarkRunQuestionAnswered: %v", err)
	}

	loaded, err := store.LoadRunQuestion(ctx, defaultTenantID, "team-board", q.ID)
	if err != nil {
		t.Fatalf("LoadRunQuestion: %v", err)
	}
	if loaded.Status != "answered" {
		t.Fatalf("status = %q, want answered", loaded.Status)
	}
	if loaded.Answer != "us-east-1 canary" {
		t.Fatalf("answer = %q, want us-east-1 canary", loaded.Answer)
	}
	if loaded.AnsweredBy != "scott" {
		t.Fatalf("answered_by = %q, want scott", loaded.AnsweredBy)
	}
	if loaded.AnsweredVia != "voice" {
		t.Fatalf("answered_via = %q, want voice", loaded.AnsweredVia)
	}
	if loaded.AnsweredAt == "" {
		t.Fatalf("answered_at was not set")
	}
}

func TestRunQuestionTTLExpiry(t *testing.T) {
	store := newTestSQLiteBoardStore(t)
	ctx := context.Background()

	askedAt := time.Now()
	q := sampleRunQuestion("q-ttl-1", askedAt, 1)
	if err := store.SaveRunQuestion(ctx, q); err != nil {
		t.Fatalf("SaveRunQuestion: %v", err)
	}

	// Advance the injected clock past asked_at + TTL instead of sleeping; the
	// store contract takes `now` as a parameter for exactly this reason.
	future := askedAt.Add(10 * time.Second)
	expired, err := store.ExpireRunQuestions(ctx, defaultTenantID, "team-board", future)
	if err != nil {
		t.Fatalf("ExpireRunQuestions: %v", err)
	}
	if expired != 1 {
		t.Fatalf("ExpireRunQuestions returned %d, want 1", expired)
	}

	loaded, err := store.LoadRunQuestion(ctx, defaultTenantID, "team-board", q.ID)
	if err != nil {
		t.Fatalf("question %q vanished after expiry: %v", q.ID, err)
	}
	if loaded.Status != "expired" {
		t.Fatalf("status = %q, want expired", loaded.Status)
	}

	// A second pass must not re-expire it.
	if expired, err := store.ExpireRunQuestions(ctx, defaultTenantID, "team-board", future); err != nil {
		t.Fatalf("ExpireRunQuestions (second pass): %v", err)
	} else if expired != 0 {
		t.Fatalf("second pass returned %d, want 0", expired)
	}
}

func TestRunQuestionTenantIsolation(t *testing.T) {
	store := newTestSQLiteBoardStore(t)
	ctx := context.Background()

	q := sampleRunQuestion("q-tenant-a-1", time.Now(), 14400)
	q.TenantID = "tenant-a"
	if err := store.SaveRunQuestion(ctx, q); err != nil {
		t.Fatalf("SaveRunQuestion(tenant-a): %v", err)
	}

	open, err := store.ListOpenRunQuestions(ctx, "tenant-b", "team-board")
	if err != nil {
		t.Fatalf("ListOpenRunQuestions(tenant-b): %v", err)
	}
	if len(open) != 0 {
		t.Fatalf("tenant-b saw %d questions, want 0: %+v", len(open), open)
	}

	if _, err := store.LoadRunQuestion(ctx, "tenant-b", "team-board", q.ID); err == nil {
		t.Fatalf("tenant-b loaded tenant-a's question %q", q.ID)
	} else if !errors.Is(err, agent.ErrRunQuestionNotFound) {
		t.Fatalf("LoadRunQuestion(tenant-b) error = %v, want ErrRunQuestionNotFound", err)
	}

	// And the original tenant still sees it.
	openA, err := store.ListOpenRunQuestions(ctx, "tenant-a", "team-board")
	if err != nil {
		t.Fatalf("ListOpenRunQuestions(tenant-a): %v", err)
	}
	if len(openA) != 1 || openA[0].ID != q.ID {
		t.Fatalf("tenant-a list = %+v, want single question %q", openA, q.ID)
	}
}

func TestRunCheckpointAppendAndList(t *testing.T) {
	store := newTestSQLiteBoardStore(t)
	ctx := context.Background()

	base := time.Now().UTC()
	checkpoints := []runCheckpoint{
		{StepIndex: 1, Kind: "started", PayloadJSON: `{"note":"begin classify"}`, CreatedAt: base.Format(time.RFC3339Nano)},
		{StepIndex: 1, Kind: "completed", PayloadJSON: `{"outcome":"ok"}`, CreatedAt: base.Add(1 * time.Millisecond).Format(time.RFC3339Nano)},
		{StepIndex: 2, Kind: "paused", PayloadJSON: `{"reason":"awaiting answer"}`, CreatedAt: base.Add(2 * time.Millisecond).Format(time.RFC3339Nano)},
	}
	for _, cp := range checkpoints {
		if err := store.AppendRunCheckpoint(ctx, defaultTenantID, "team-board", "agent-run-1", cp); err != nil {
			t.Fatalf("AppendRunCheckpoint(%+v): %v", cp, err)
		}
	}

	got, err := store.ListRunCheckpoints(ctx, defaultTenantID, "team-board", "agent-run-1")
	if err != nil {
		t.Fatalf("ListRunCheckpoints: %v", err)
	}
	if len(got) != len(checkpoints) {
		t.Fatalf("ListRunCheckpoints returned %d, want %d: %+v", len(got), len(checkpoints), got)
	}
	for i := range got {
		if got[i].Kind != checkpoints[i].Kind || got[i].StepIndex != checkpoints[i].StepIndex {
			t.Fatalf("checkpoint[%d] = %+v, want %+v", i, got[i], checkpoints[i])
		}
		if got[i].PayloadJSON != checkpoints[i].PayloadJSON {
			t.Fatalf("checkpoint[%d] payload = %q, want %q", i, got[i].PayloadJSON, checkpoints[i].PayloadJSON)
		}
	}
}
