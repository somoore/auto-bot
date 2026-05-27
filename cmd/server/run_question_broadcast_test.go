package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/somoore/auto-bot/internal/agent"
)

// capturedBroadcast records a single broadcastKanbanEventForBoard call so
// tests can assert which typed events the coordinator emitted and what
// payload the WS layer would have shipped to clients.
type capturedBroadcast struct {
	TenantID string
	BoardID  string
	Event    string
	Data     any
}

// broadcastCapture is the test-side sink installed via withBroadcastCapture.
// Calls are recorded in arrival order under the mutex so concurrent test
// scenarios remain deterministic.
type broadcastCapture struct {
	mu     sync.Mutex
	events []capturedBroadcast
}

func (c *broadcastCapture) record(tenantID string, boardID string, event string, data any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, capturedBroadcast{TenantID: tenantID, BoardID: boardID, Event: event, Data: data})
}

func (c *broadcastCapture) snapshot() []capturedBroadcast {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]capturedBroadcast, len(c.events))
	copy(out, c.events)
	return out
}

func (c *broadcastCapture) eventsOfType(event string) []capturedBroadcast {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []capturedBroadcast
	for _, ev := range c.events {
		if ev.Event == event {
			out = append(out, ev)
		}
	}
	return out
}

// withBroadcastCapture swaps broadcastSink for the duration of a test so
// emitted events land in a capture struct instead of fanning out to
// websocket clients (which the unit test does not own). Restoration of the
// production sink runs via t.Cleanup.
func withBroadcastCapture(t *testing.T) *broadcastCapture {
	t.Helper()
	cap := &broadcastCapture{}
	previous := broadcastSink
	broadcastSink = cap.record
	t.Cleanup(func() { broadcastSink = previous })
	return cap
}

// seedAgentRunForQuestion writes an in-progress agentRun into the board so
// AskHuman/Resume have a row to update. It bypasses the assign-ticket flow
// because that path requires a Bedrock client (out of scope for this WS
// surface test).
func seedAgentRunForQuestion(t *testing.T, board *kanbanBoard, runID, cardID string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	run := agentRun{
		RunID:        runID,
		TenantID:     board.tenantID,
		BoardID:      board.boardID,
		CardID:       cardID,
		Objective:    "answer the human-pause broadcast scenario",
		AgentProfile: "project_manager",
		RequestType:  "code_review",
		Specialist:   "project_manager",
		Status:       agentRunReviewing,
		CurrentStep:  "Awaiting human input",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	board.mu.Lock()
	board.agentRuns = append([]agentRun{run}, board.agentRuns...)
	board.mu.Unlock()
}

func TestRunQuestionBroadcastAskedAnsweredExpired(t *testing.T) {
	store := newTestSQLiteBoardStore(t)
	board, err := newPersistentKanbanBoard("team-board", store)
	if err != nil {
		t.Fatalf("newPersistentKanbanBoard: %v", err)
	}
	// Hook the broadcast enrichment to read from this specific board so the
	// "board" event carries open_run_questions even though sharedBoard may
	// be nil in unit tests.
	previousProvider := openRunQuestionsProvider
	openRunQuestionsProvider = func(boardID string) []agent.RunQuestion {
		if boardID != board.boardID {
			return nil
		}
		questions, listErr := store.ListOpenRunQuestions(context.Background(), board.tenantID, board.boardID)
		if listErr != nil {
			t.Errorf("test ListOpenRunQuestions: %v", listErr)
			return nil
		}
		return questions
	}
	t.Cleanup(func() { openRunQuestionsProvider = previousProvider })

	capture := withBroadcastCapture(t)

	runID := "agent-run-broadcast-1"
	cardID := "EMAL-77"
	seedAgentRunForQuestion(t, board, runID, cardID)

	orchestrator := &agentRunOrchestrator{board: board}
	ctx := context.Background()

	// Step 1: AskHuman emits run_question_asked + run_paused + board.
	askedAt := time.Now().UTC()
	questionID, err := orchestrator.AskHuman(ctx, runID, agent.RunQuestion{
		CardID:     cardID,
		StepIndex:  2,
		Prompt:     "Should the rollout target us-east-1 canary first?",
		Reasoning:  "Card mentions canary but does not say which region.",
		AskedAt:    askedAt.Format(time.RFC3339Nano),
		TTLSeconds: 14400,
	})
	if err != nil {
		t.Fatalf("AskHuman: %v", err)
	}
	if questionID == "" {
		t.Fatal("AskHuman returned empty question id")
	}

	asked := capture.eventsOfType("run_question_asked")
	if len(asked) != 1 {
		t.Fatalf("run_question_asked emitted %d times, want 1: %+v", len(asked), capture.snapshot())
	}
	q, ok := asked[0].Data.(agent.RunQuestion)
	if !ok {
		t.Fatalf("run_question_asked payload type = %T, want agent.RunQuestion", asked[0].Data)
	}
	if q.ID != questionID || q.RunID != runID || q.Status != "open" {
		t.Fatalf("run_question_asked payload = %+v, want id=%s run=%s status=open", q, questionID, runID)
	}

	if len(capture.eventsOfType("run_paused")) != 1 {
		t.Fatalf("run_paused not emitted: %+v", capture.snapshot())
	}

	boardEvents := capture.eventsOfType("board")
	if len(boardEvents) == 0 {
		t.Fatal("board event not emitted after AskHuman")
	}
	lastBoard := boardEvents[len(boardEvents)-1]
	state, ok := lastBoard.Data.(kanbanBoardState)
	if !ok {
		t.Fatalf("board payload type = %T, want kanbanBoardState", lastBoard.Data)
	}
	if len(state.OpenRunQuestions) != 1 || state.OpenRunQuestions[0].ID != questionID {
		t.Fatalf("OpenRunQuestions after AskHuman = %+v, want [%s]", state.OpenRunQuestions, questionID)
	}

	// Step 2: Resume emits run_question_answered + run_resumed + board with
	// the question removed from OpenRunQuestions.
	if _, err := orchestrator.Resume(ctx, agent.HumanAnswer{
		QuestionID:  questionID,
		Answer:      "us-east-1 canary",
		AnsweredBy:  "scott",
		AnsweredVia: "ui",
	}); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	answered := capture.eventsOfType("run_question_answered")
	if len(answered) != 1 {
		t.Fatalf("run_question_answered emitted %d times, want 1", len(answered))
	}
	answeredQ, ok := answered[0].Data.(agent.RunQuestion)
	if !ok {
		t.Fatalf("run_question_answered payload type = %T", answered[0].Data)
	}
	if answeredQ.Status != "answered" || answeredQ.Answer != "us-east-1 canary" {
		t.Fatalf("answered payload = %+v, want status=answered answer=us-east-1 canary", answeredQ)
	}

	if len(capture.eventsOfType("run_resumed")) != 1 {
		t.Fatalf("run_resumed not emitted: %+v", capture.snapshot())
	}

	boardEvents = capture.eventsOfType("board")
	lastBoard = boardEvents[len(boardEvents)-1]
	state = lastBoard.Data.(kanbanBoardState)
	for _, oq := range state.OpenRunQuestions {
		if oq.ID == questionID {
			t.Fatalf("answered question %s still present in OpenRunQuestions: %+v", questionID, state.OpenRunQuestions)
		}
	}

	// Step 3: Ask another question with a tiny TTL, then drive the sweeper
	// past its deadline and assert run_question_expired fires and the
	// question disappears from OpenRunQuestions.
	expiringQuestion := agent.RunQuestion{
		ID:         "q-expiring-1",
		TenantID:   board.tenantID,
		BoardID:    board.boardID,
		RunID:      runID,
		CardID:     cardID,
		StepIndex:  3,
		Prompt:     "Should we proceed with the failover?",
		AskedAt:    time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339Nano),
		TTLSeconds: 1,
		Status:     "open",
	}
	if err := store.SaveRunQuestion(ctx, expiringQuestion); err != nil {
		t.Fatalf("SaveRunQuestion (expiring): %v", err)
	}

	// Pre-sweep: confirm it shows in the open list so we know the sweeper
	// drove the transition rather than the question simply not existing.
	openBefore, err := store.ListOpenRunQuestions(ctx, board.tenantID, board.boardID)
	if err != nil {
		t.Fatalf("ListOpenRunQuestions pre-sweep: %v", err)
	}
	var seen bool
	for _, oq := range openBefore {
		if oq.ID == expiringQuestion.ID {
			seen = true
			break
		}
	}
	if !seen {
		t.Fatalf("expiring question not in open list pre-sweep: %+v", openBefore)
	}

	sweepRunQuestionsOnce(ctx, board, store)

	expired := capture.eventsOfType("run_question_expired")
	if len(expired) != 1 {
		t.Fatalf("run_question_expired emitted %d times, want 1: %+v", len(expired), capture.snapshot())
	}
	expiredQ, ok := expired[0].Data.(agent.RunQuestion)
	if !ok {
		t.Fatalf("run_question_expired payload type = %T", expired[0].Data)
	}
	if expiredQ.ID != expiringQuestion.ID || expiredQ.Status != "expired" {
		t.Fatalf("expired payload = %+v, want id=%s status=expired", expiredQ, expiringQuestion.ID)
	}

	boardEvents = capture.eventsOfType("board")
	lastBoard = boardEvents[len(boardEvents)-1]
	state = lastBoard.Data.(kanbanBoardState)
	for _, oq := range state.OpenRunQuestions {
		if oq.ID == expiringQuestion.ID {
			t.Fatalf("expired question %s still present in OpenRunQuestions: %+v", expiringQuestion.ID, state.OpenRunQuestions)
		}
	}
}
