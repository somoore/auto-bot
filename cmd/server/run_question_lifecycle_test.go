package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/somoore/auto-bot/internal/agent"
)

// TestResumeOnExpiredQuestionReturnsError is the DA-1 regression test. Before
// the fix, Resume only refused to advance when the question status was
// "answered"; an "expired" question was silently marked answered, which
// zombied the Run because the TTL sweeper had already cleared WaitingOn,
// broadcast run_question_expired, and the answer was being applied to a
// timed-out prompt. The fix adds an `existing.Status == "expired"` guard
// that surfaces agent.ErrRunQuestionExpired so callers can branch.
//
// The test walks the lifecycle: ask -> sweep past TTL -> answer attempt
// fails with ErrRunQuestionExpired.
func TestResumeOnExpiredQuestionReturnsError(t *testing.T) {
	store := newTestSQLiteBoardStore(t)
	board, err := newPersistentKanbanBoard("team-board", store)
	if err != nil {
		t.Fatalf("newPersistentKanbanBoard: %v", err)
	}

	runID := "agent-run-expiry-1"
	cardID := "EMAL-expiry-1"
	seedAgentRunForQuestion(t, board, runID, cardID)

	orchestrator := &agentRunOrchestrator{board: board}
	ctx := context.Background()

	// Step 1: AskHuman with a TTL of 1 second; backdate AskedAt so the
	// sweeper sees an expired question on the next call.
	questionID, err := orchestrator.AskHuman(ctx, runID, agent.RunQuestion{
		CardID:     cardID,
		StepIndex:  1,
		Prompt:     "Should the failover proceed?",
		Reasoning:  "Need confirmation before us-east-1 cutover.",
		AskedAt:    time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339Nano),
		TTLSeconds: 1,
	})
	if err != nil {
		t.Fatalf("AskHuman: %v", err)
	}
	if questionID == "" {
		t.Fatal("AskHuman returned empty question id")
	}

	// Step 2: drive the sweeper so the question flips to "expired".
	sweepRunQuestionsOnce(ctx, board, store)

	expired, err := store.LoadRunQuestion(ctx, board.tenantID, board.boardID, questionID)
	if err != nil {
		t.Fatalf("LoadRunQuestion after sweep: %v", err)
	}
	if expired.Status != "expired" {
		t.Fatalf("question status after sweep = %q, want expired (sweeper did not transition the question)", expired.Status)
	}

	// Step 3: attempt to resume — must surface ErrRunQuestionExpired.
	_, err = orchestrator.Resume(ctx, agent.HumanAnswer{
		QuestionID:  questionID,
		Answer:      "us-east-1 canary first",
		AnsweredBy:  "scott",
		AnsweredVia: "ui",
	})
	if err == nil {
		t.Fatal("Resume returned nil error against an expired question; DA-1 guard missing")
	}
	if !errors.Is(err, agent.ErrRunQuestionExpired) {
		t.Fatalf("Resume error = %v, want errors.Is(err, agent.ErrRunQuestionExpired)", err)
	}

	// Step 4: defense-in-depth — the question must not have been
	// transitioned to "answered" by the failed Resume attempt.
	post, err := store.LoadRunQuestion(ctx, board.tenantID, board.boardID, questionID)
	if err != nil {
		t.Fatalf("LoadRunQuestion after failed resume: %v", err)
	}
	if post.Status != "expired" {
		t.Fatalf("question status after failed resume = %q, want expired (Resume must not zombie an expired question)", post.Status)
	}
}
