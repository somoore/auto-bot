package jira_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/somoore/auto-bot/internal/board"
	"github.com/somoore/auto-bot/internal/projection"
	"github.com/somoore/auto-bot/internal/projection/contracttest"
	jiraproj "github.com/somoore/auto-bot/internal/projection/jira"
)

func TestJiraProjectionSatisfiesContract(t *testing.T) {
	contracttest.RunProjectionContract(t, func() projection.Projection {
		return jiraproj.NewProjection(&recordingClient{}, jiraproj.Config{
			BaseURL: "https://example.atlassian.net", ProjectKey: "PROJ",
		})
	})
}

func TestJiraProjectionUpsertsAndDeletes(t *testing.T) {
	client := &recordingClient{}
	proj := jiraproj.NewProjection(client, jiraproj.Config{ProjectKey: "PROJ"})
	delta := projection.BoardDelta{
		Changed: []board.Card{
			{ID: "PROJ-1", Title: "existing", Notes: "n", Status: board.StatusInProgress},
			{ID: "", Title: "new", Status: board.StatusBacklog},
			{ID: "PROJ-3", Title: "with human", Status: board.StatusDone, Assignee: &board.Actor{Kind: board.ActorKindHuman, ID: "acct-1"}},
			{ID: "PROJ-4", Title: "with agent", Status: board.StatusInProgress, Assignee: &board.Actor{Kind: board.ActorKindAgent, ID: "agent-1"}},
		},
		Deleted: []string{"PROJ-2", "   "},
	}
	if err := proj.Project(context.Background(), delta); err != nil {
		t.Fatalf("Project: %v", err)
	}
	want := []string{
		"UpdateIssue(PROJ-1,existing,n)",
		"TransitionIssue(PROJ-1,In Progress)",
		"AssignIssue(PROJ-1,)",
		"CreateIssue(new)",
		"UpdateIssue(PROJ-3,with human,)",
		"TransitionIssue(PROJ-3,Done)",
		"AssignIssue(PROJ-3,acct-1)",
		"UpdateIssue(PROJ-4,with agent,)",
		"TransitionIssue(PROJ-4,In Progress)",
		"AssignIssue(PROJ-4,)",
		"CloseIssue(PROJ-2)",
	}
	if got := client.calls; !equalStringSlices(got, want) {
		t.Fatalf("call sequence mismatch\ngot:  %v\nwant: %v", got, want)
	}
}

func TestJiraProjectionReconcilePassthrough(t *testing.T) {
	want := []board.Card{{ID: "PROJ-1", Title: "from jira"}}
	client := &recordingClient{searchCards: want}
	proj := jiraproj.NewProjection(client, jiraproj.Config{})
	got, err := proj.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if len(got) != 1 || got[0].ID != "PROJ-1" {
		t.Fatalf("Reconcile returned %#v, want PROJ-1", got)
	}
	if got := client.calls; len(got) != 1 || got[0] != "SearchKanbanCards" {
		t.Fatalf("Reconcile call sequence = %v, want [SearchKanbanCards]", got)
	}
}

func TestJiraProjectionShortCircuitsOnError(t *testing.T) {
	client := &recordingClient{updateErr: errors.New("network")}
	proj := jiraproj.NewProjection(client, jiraproj.Config{})
	delta := projection.BoardDelta{Changed: []board.Card{{ID: "PROJ-1", Title: "first"}, {ID: "PROJ-2", Title: "second"}}}
	if err := proj.Project(context.Background(), delta); err == nil {
		t.Fatal("expected Project to surface client error")
	}
	if got := client.calls; len(got) != 1 || got[0] != "UpdateIssue(PROJ-1,first,)" {
		t.Fatalf("expected short-circuit after first failure, got %v", got)
	}
}

func TestJiraProjectionNilClient(t *testing.T) {
	proj := jiraproj.NewProjection(nil, jiraproj.Config{})
	if err := proj.Project(context.Background(), projection.BoardDelta{Changed: []board.Card{{ID: "PROJ-1"}}}); err == nil {
		t.Fatal("Project with nil client should error")
	}
	if _, err := proj.Reconcile(context.Background()); err == nil {
		t.Fatal("Reconcile with nil client should error")
	}
	health := proj.Health(context.Background())
	if health.OK || health.Status != "not_configured" {
		t.Fatalf("Health with nil client = %+v, want not_configured", health)
	}
}

type recordingClient struct {
	mu          sync.Mutex
	calls       []string
	searchCards []board.Card
	updateErr   error
}

func (c *recordingClient) record(call string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, call)
}
func (c *recordingClient) SearchKanbanCards(_ context.Context) ([]board.Card, error) {
	c.record("SearchKanbanCards")
	return c.searchCards, nil
}
func (c *recordingClient) CreateIssue(_ context.Context, card board.Card) (string, error) {
	c.record(fmt.Sprintf("CreateIssue(%s)", card.Title))
	return "PROJ-CREATED", nil
}
func (c *recordingClient) UpdateIssue(_ context.Context, id, t, n string) error {
	c.record(fmt.Sprintf("UpdateIssue(%s,%s,%s)", id, t, n))
	return c.updateErr
}
func (c *recordingClient) CloseIssue(_ context.Context, id string) error {
	c.record(fmt.Sprintf("CloseIssue(%s)", id))
	return nil
}
func (c *recordingClient) TransitionIssue(_ context.Context, id string, s board.Status) error {
	c.record(fmt.Sprintf("TransitionIssue(%s,%s)", id, s))
	return nil
}
func (c *recordingClient) AssignIssue(_ context.Context, id, a string) error {
	c.record(fmt.Sprintf("AssignIssue(%s,%s)", id, a))
	return nil
}

func equalStringSlices(a, b []string) bool {
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
