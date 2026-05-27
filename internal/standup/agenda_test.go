package standup

import (
	"context"
	"testing"
	"time"

	"github.com/somoore/auto-bot/internal/agent"
	"github.com/somoore/auto-bot/internal/board"
)

type stubBoardReader struct {
	cards         []board.Card
	recentChanged []string
}

func (s *stubBoardReader) Cards(_ context.Context, _, _ string) ([]board.Card, error) {
	return s.cards, nil
}

func (s *stubBoardReader) RecentMutationCardIDs(_ context.Context, _, _ string, _ time.Time) ([]string, error) {
	return s.recentChanged, nil
}

type stubRunReader struct {
	runs      []agent.Run
	questions []agent.RunQuestion
}

func (s *stubRunReader) ListAgentRuns(_ context.Context, _, _ string, _ int) ([]agent.Run, error) {
	return s.runs, nil
}

func (s *stubRunReader) ListOpenRunQuestions(_ context.Context, _, _ string) ([]agent.RunQuestion, error) {
	return s.questions, nil
}

func TestBuildAgendaShape(t *testing.T) {
	cards := []board.Card{
		{ID: "c1", Title: "Ship feature flag", Status: board.StatusDone, Assignee: &board.Actor{DisplayName: "Scott"}},
		{ID: "c2", Title: "Investigate flaky CI", Status: board.StatusBlocked, BlockedReason: "Waiting on infra", Assignee: &board.Actor{DisplayName: "Sarah"}},
		{ID: "c3", Title: "Refactor auth", Status: board.StatusInProgress, Assignee: &board.Actor{DisplayName: "Anna"}},
		{ID: "c4", Title: "Old completed work", Status: board.StatusDone},
	}
	reader := &stubBoardReader{
		cards:         cards,
		recentChanged: []string{"c1", "c3"},
	}
	runs := []agent.Run{
		{RunID: "r1", CardID: "c2", Status: agent.StatusWaitingOnHuman, AgentProfile: "swe-1"},
		{RunID: "r2", CardID: "c4", Status: agent.StatusCompleted, PullRequestNumber: 42, PRReviewPosted: false},
		{RunID: "r3", CardID: "c1", Status: agent.StatusCompleted, PRReviewPosted: true},
	}
	questions := []agent.RunQuestion{
		{ID: "q1", RunID: "r1", CardID: "c2", Prompt: "Which Jira board?", AskedAt: time.Now().Format(time.RFC3339)},
	}
	runReader := &stubRunReader{runs: runs, questions: questions}
	builder := &AgendaBuilder{Board: reader, Runs: runReader}

	agenda, err := builder.BuildAgenda(context.Background(), "default", "default", 0)
	if err != nil {
		t.Fatalf("BuildAgenda: %v", err)
	}

	if agenda.TenantID != "default" || agenda.BoardID != "default" {
		t.Fatalf("agenda tenant/board mismatch: %#v", agenda)
	}

	// c1 (done, recent), c3 (in progress, recent) — both highlights.
	if got := len(agenda.Highlights); got != 2 {
		t.Fatalf("expected 2 highlights, got %d (%+v)", got, agenda.Highlights)
	}
	if got := len(agenda.Blockers); got != 1 || agenda.Blockers[0].CardID != "c2" {
		t.Fatalf("expected blocker c2, got %+v", agenda.Blockers)
	}
	// r1 (waiting_on_human) and r2 (completed but PR review not posted) should
	// surface; r3 should not.
	if got := len(agenda.RunsAwaitingReview); got != 2 {
		t.Fatalf("expected 2 runs awaiting review, got %d (%+v)", got, agenda.RunsAwaitingReview)
	}
	if got := len(agenda.OpenQuestions); got != 1 || agenda.OpenQuestions[0].QuestionID != "q1" {
		t.Fatalf("expected question q1, got %+v", agenda.OpenQuestions)
	}
	// Speaker order should include Scott (highlight), Sarah (blocker), Anna
	// (highlight) — sorted alphabetically.
	wantOrder := []string{"Anna", "Sarah", "Scott"}
	if got := agenda.ProposedSpeakerOrder; !equalStringSlice(got, wantOrder) {
		t.Fatalf("speaker order = %v, want %v", got, wantOrder)
	}
	if agenda.Summary == "" {
		t.Fatalf("expected non-empty summary")
	}
}

func TestBuildAgendaEmpty(t *testing.T) {
	builder := &AgendaBuilder{Board: &stubBoardReader{}}
	agenda, err := builder.BuildAgenda(context.Background(), "default", "default", time.Hour)
	if err != nil {
		t.Fatalf("BuildAgenda: %v", err)
	}
	if agenda.Summary == "" {
		t.Fatalf("expected fallback summary on empty agenda")
	}
}

func TestBuildAgendaNilReceiver(t *testing.T) {
	var b *AgendaBuilder
	_, err := b.BuildAgenda(context.Background(), "default", "default", 0)
	if err == nil {
		t.Fatalf("expected error on nil receiver")
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
