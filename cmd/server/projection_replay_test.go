package main

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/somoore/auto-bot/internal/board"
	"github.com/somoore/auto-bot/internal/projection"
	jiraproj "github.com/somoore/auto-bot/internal/projection/jira"
)

// TestJiraProjectionReplayMatchesLegacy replays the same set of board events
// through (a) a direct sequence of jiraproj.Client API calls — the shape of
// cmd/server's existing jira_conflicts.go restoreJiraCard path — and (b) the
// new JiraProjection.Project path. Both runs record method invocations
// against a shared client double; the test asserts the recorded sequences are
// identical so the refactor is provably behavior-preserving for the slice of
// surface JiraProjection now owns.
func TestJiraProjectionReplayMatchesLegacy(t *testing.T) {
	events := []board.Card{
		{ID: "PROJ-101", Title: "first card", Notes: "n1", Status: board.StatusInProgress, Assignee: &board.Actor{Kind: board.ActorKindHuman, ID: "acct-1"}},
		{ID: "PROJ-102", Title: "second card", Notes: "", Status: board.StatusDone},
		{ID: "PROJ-103", Title: "agent owned", Status: board.StatusBlocked, Assignee: &board.Actor{Kind: board.ActorKindAgent, ID: "agent-1"}},
	}
	deletions := []string{"PROJ-200"}

	legacy := &replayRecorder{}
	if err := legacyApplyDelta(context.Background(), legacy, events, deletions); err != nil {
		t.Fatalf("legacy path: %v", err)
	}
	projected := &replayRecorder{}
	proj := jiraproj.NewProjection(projected, jiraproj.Config{ProjectKey: "PROJ"})
	if err := proj.Project(context.Background(), projection.BoardDelta{
		BoardID: "board-a", Changed: events, Deleted: deletions,
	}); err != nil {
		t.Fatalf("projection path: %v", err)
	}
	if !equalCallSequences(legacy.calls, projected.calls) {
		t.Fatalf("call sequence drift\nlegacy:    %v\nprojected: %v", legacy.calls, projected.calls)
	}
}

func legacyApplyDelta(ctx context.Context, client jiraproj.Client, changed []board.Card, deleted []string) error {
	for _, card := range changed {
		if card.ID == "" {
			if _, err := client.CreateIssue(ctx, card); err != nil {
				return err
			}
			continue
		}
		if err := client.UpdateIssue(ctx, card.ID, card.Title, card.Notes); err != nil {
			return err
		}
		if string(card.Status) != "" {
			if err := client.TransitionIssue(ctx, card.ID, card.Status); err != nil {
				return err
			}
		}
		accountID := ""
		if card.Assignee != nil && card.Assignee.Kind == board.ActorKindHuman {
			accountID = card.Assignee.ID
		}
		if err := client.AssignIssue(ctx, card.ID, accountID); err != nil {
			return err
		}
	}
	for _, cardID := range deleted {
		if cardID == "" {
			continue
		}
		if err := client.CloseIssue(ctx, cardID); err != nil {
			return err
		}
	}
	return nil
}

type replayRecorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *replayRecorder) record(call string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
}
func (r *replayRecorder) SearchKanbanCards(_ context.Context) ([]board.Card, error) {
	r.record("SearchKanbanCards")
	return nil, nil
}
func (r *replayRecorder) CreateIssue(_ context.Context, card board.Card) (string, error) {
	r.record(fmt.Sprintf("CreateIssue(%s)", card.Title))
	return "PROJ-NEW", nil
}
func (r *replayRecorder) UpdateIssue(_ context.Context, id, t, n string) error {
	r.record(fmt.Sprintf("UpdateIssue(%s,%s,%s)", id, t, n))
	return nil
}
func (r *replayRecorder) CloseIssue(_ context.Context, id string) error {
	r.record(fmt.Sprintf("CloseIssue(%s)", id))
	return nil
}
func (r *replayRecorder) TransitionIssue(_ context.Context, id string, s board.Status) error {
	r.record(fmt.Sprintf("TransitionIssue(%s,%s)", id, s))
	return nil
}
func (r *replayRecorder) AssignIssue(_ context.Context, id, a string) error {
	r.record(fmt.Sprintf("AssignIssue(%s,%s)", id, a))
	return nil
}

func equalCallSequences(a, b []string) bool {
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

// Compile-time assertion that the existing cmd/server *jiraClient satisfies
// the narrow jiraproj.Client interface via duck typing. If anyone changes a
// *jiraClient method signature in a way that breaks the projection contract,
// this var declaration fails to compile.
var _ jiraproj.Client = (*jiraClient)(nil)
