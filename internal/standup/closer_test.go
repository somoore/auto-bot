package standup

import (
	"context"
	"errors"
	"testing"

	"github.com/somoore/auto-bot/internal/agent"
	"github.com/somoore/auto-bot/internal/board"
	"github.com/somoore/auto-bot/internal/meetings"
)

type stubCardCreator struct {
	requests []CardRequest
	nextID   int
	fail     bool
}

func (s *stubCardCreator) CreateCard(_ context.Context, req CardRequest) (board.Card, error) {
	if s.fail {
		return board.Card{}, errors.New("create card disabled")
	}
	s.nextID++
	s.requests = append(s.requests, req)
	return board.Card{
		ID:     "card-" + itoa(s.nextID),
		Title:  req.Title,
		Status: req.Status,
	}, nil
}

type stubRunCoordinator struct {
	starts []agent.RunRequest
	nextID int
	fail   bool
}

func (s *stubRunCoordinator) Start(_ context.Context, req agent.RunRequest) (agent.Run, error) {
	if s.fail {
		return agent.Run{}, errors.New("start disabled")
	}
	s.nextID++
	s.starts = append(s.starts, req)
	return agent.Run{RunID: "run-" + itoa(s.nextID), CardID: req.CardID, Status: agent.StatusQueued}, nil
}

func (s *stubRunCoordinator) Checkpoint(_ context.Context, _ string, _ agent.RunStepCheckpoint) error {
	return nil
}

func (s *stubRunCoordinator) AskHuman(_ context.Context, _ string, _ agent.RunQuestion) (string, error) {
	return "", nil
}

func (s *stubRunCoordinator) Resume(_ context.Context, _ agent.HumanAnswer) (agent.Run, error) {
	return agent.Run{}, nil
}

func (s *stubRunCoordinator) Cancel(_ context.Context, _ string, _ string) error { return nil }

type stubArtifactSink struct {
	persisted []MeetingArtifact
}

func (s *stubArtifactSink) PersistMeetingReport(_ context.Context, report MeetingArtifact) error {
	s.persisted = append(s.persisted, report)
	return nil
}

func TestCloseCreatesCardsAndRuns(t *testing.T) {
	cards := &stubCardCreator{}
	runs := &stubRunCoordinator{}
	sink := &stubArtifactSink{}
	closer := &Closer{Cards: cards, Runs: runs, Sink: sink}

	artifact := MeetingArtifact{
		TenantID:     "default",
		BoardID:      "default",
		MeetingID:    "meeting-1",
		HostIdentity: "scott",
		FollowUps: []meetings.FollowUp{
			{ID: "fu1", Owner: "Scott", Text: "Sync with infra on rollout"},
			{ID: "fu2", Owner: "agent:swe-1", Text: "Add unit tests for new gate"},
			{ID: "fu3", Owner: "Anna", Text: "Already has a card", CardID: "EMAL-7"},
		},
		UnresolvedBlockers: []meetings.Blocker{
			{ID: "b1", Owner: "Sarah", Text: "Waiting on infra"},
		},
	}
	result, err := closer.Close(context.Background(), artifact)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	// fu1 + fu2 + b1 = 3 cards; fu3 already has a card so skipped.
	if got := len(result.CardsCreated); got != 3 {
		t.Fatalf("expected 3 cards created, got %d (%v)", got, result.CardsCreated)
	}
	// fu2 was the agent-assigned one — exactly 1 run starts.
	if got := len(result.RunsStarted); got != 1 {
		t.Fatalf("expected 1 run started, got %d (%v)", got, result.RunsStarted)
	}
	if len(sink.persisted) != 1 || sink.persisted[0].MeetingID != "meeting-1" {
		t.Fatalf("artifact was not persisted: %+v", sink.persisted)
	}
	// Verify the agent-driven request carried the right profile.
	if runs.starts[0].AgentProfile != "swe-1" {
		t.Fatalf("expected agent profile swe-1, got %q", runs.starts[0].AgentProfile)
	}
	// Verify the blocker card landed in Blocked status.
	var blockerCard *CardRequest
	for i := range cards.requests {
		if cards.requests[i].Status == board.StatusBlocked {
			blockerCard = &cards.requests[i]
			break
		}
	}
	if blockerCard == nil {
		t.Fatalf("expected a blocker card to be created in Blocked status")
	}
}

func TestCloseHandlesNilCoordinatorGracefully(t *testing.T) {
	cards := &stubCardCreator{}
	closer := &Closer{Cards: cards}
	artifact := MeetingArtifact{
		MeetingID: "meeting-2",
		FollowUps: []meetings.FollowUp{
			{Owner: "agent:swe-1", Text: "Do the thing"},
		},
	}
	result, err := closer.Close(context.Background(), artifact)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if len(result.CardsCreated) != 1 {
		t.Fatalf("expected 1 card created even without coordinator, got %d", len(result.CardsCreated))
	}
	if len(result.RunsStarted) != 0 {
		t.Fatalf("expected 0 runs when coordinator is nil, got %d", len(result.RunsStarted))
	}
}

func TestCloseRequiresCardCreator(t *testing.T) {
	closer := &Closer{}
	_, err := closer.Close(context.Background(), MeetingArtifact{})
	if err == nil {
		t.Fatalf("expected error when Cards is nil")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
