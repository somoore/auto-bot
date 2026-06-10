//go:build integration

package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

type liveJiraIssue struct {
	Key    string `json:"key"`
	Fields struct {
		Summary     string   `json:"summary"`
		Description any      `json:"description"`
		Labels      []string `json:"labels"`
		DueDate     string   `json:"duedate"`
		Priority    struct {
			Name string `json:"name"`
		} `json:"priority"`
		Comment struct {
			Comments []struct {
				Body any `json:"body"`
			} `json:"comments"`
		} `json:"comment"`
		Flagged []struct {
			Value string `json:"value"`
		} `json:"customfield_10021"`
		Status struct {
			Name string `json:"name"`
		} `json:"status"`
	} `json:"fields"`
}

func TestLiveJiraWriteThrough(t *testing.T) {
	if strings.TrimSpace(getEnvDefault("JIRA_LIVE_TEST", "")) != "1" {
		t.Skip("set JIRA_LIVE_TEST=1 to run the live Jira write-through test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	config, err := loadJiraConfig(ctx, strings.TrimSpace(getEnvDefault("JIRA_CONFIG_PATH", "")))
	if err != nil {
		t.Fatalf("load Jira config: %v", err)
	}

	board := newKanbanBoard()
	syncer := &jiraSyncer{
		board:  board,
		client: newJiraClient(config),
		config: config,
	}

	title := "auto-bot live sync smoke " + time.Now().UTC().Format("20060102T150405Z")
	createArgs := `{"title":"` + title + `","notes":"Created by auto-bot live Jira integration test.","tags":["auto-bot-live","sync-smoke"],"status":"Backlog"}`
	result, changed, err := board.ApplyToolCall("create_ticket", createArgs)
	if err != nil {
		t.Fatalf("create_ticket local mutation: %v", err)
	}
	if !changed {
		t.Fatal("create_ticket did not report a local change")
	}
	if err := syncer.ApplyToolCall(ctx, "create_ticket", createArgs, result); err != nil {
		t.Fatalf("create_ticket Jira sync: %v", err)
	}

	issueKey := findCardIDByTitle(board.SnapshotState(), title)
	if issueKey == "" {
		t.Fatalf("created card was not renamed to a Jira issue key")
	}
	t.Logf("created Jira issue %s", issueKey)

	issue := getLiveJiraIssue(t, ctx, syncer.client, issueKey)
	if issue.Fields.Summary != title {
		t.Fatalf("created issue summary = %q, want %q", issue.Fields.Summary, title)
	}
	if issue.Fields.Status.Name != "To Do" {
		t.Fatalf("created issue status = %q, want To Do", issue.Fields.Status.Name)
	}

	updatedTitle := title + " updated"
	updatedNotes := "Updated by auto-bot live Jira integration test."
	updateArgs := `{"card_id":"` + issueKey + `","title":"` + updatedTitle + `","notes":"` + updatedNotes + `"}`
	result, changed, err = board.ApplyToolCall("update_ticket", updateArgs)
	if err != nil {
		t.Fatalf("update_ticket local mutation: %v", err)
	}
	if !changed {
		t.Fatal("update_ticket did not report a local change")
	}
	if err := syncer.ApplyToolCall(ctx, "update_ticket", updateArgs, result); err != nil {
		t.Fatalf("update_ticket Jira sync: %v", err)
	}

	issue = getLiveJiraIssue(t, ctx, syncer.client, issueKey)
	if issue.Fields.Summary != updatedTitle {
		t.Fatalf("updated issue summary = %q, want %q", issue.Fields.Summary, updatedTitle)
	}
	if !strings.Contains(jiraADFPlainText(issue.Fields.Description), updatedNotes) {
		t.Fatalf("updated issue description did not include %q", updatedNotes)
	}

	addTagsArgs := `{"card_id":"` + issueKey + `","tags":["auto_bot_live","sync_smoke","jira_write"]}`
	result, changed, err = board.ApplyToolCall("add_tags", addTagsArgs)
	if err != nil {
		t.Fatalf("add_tags local mutation: %v", err)
	}
	if !changed {
		t.Fatal("add_tags did not report a local change")
	}
	if err := syncer.ApplyToolCall(ctx, "add_tags", addTagsArgs, result); err != nil {
		t.Fatalf("add_tags Jira sync: %v", err)
	}

	issue = getLiveJiraIssue(t, ctx, syncer.client, issueKey)
	for _, label := range []string{"auto_bot_live", "sync_smoke", "jira_write"} {
		if !containsString(issue.Fields.Labels, label) {
			t.Fatalf("labels = %v, want %q", issue.Fields.Labels, label)
		}
	}

	removeTagsArgs := `{"card_id":"` + issueKey + `","tags":["sync_smoke"]}`
	result, changed, err = board.ApplyToolCall("remove_tags", removeTagsArgs)
	if err != nil {
		t.Fatalf("remove_tags local mutation: %v", err)
	}
	if !changed {
		t.Fatal("remove_tags did not report a local change")
	}
	if err := syncer.ApplyToolCall(ctx, "remove_tags", removeTagsArgs, result); err != nil {
		t.Fatalf("remove_tags Jira sync: %v", err)
	}

	issue = getLiveJiraIssue(t, ctx, syncer.client, issueKey)
	if containsString(issue.Fields.Labels, "sync_smoke") {
		t.Fatalf("labels = %v, want sync_smoke removed", issue.Fields.Labels)
	}

	etaArgs := `{"card_id":"` + issueKey + `","eta":"2026-05-20"}`
	result, changed, err = board.ApplyToolCall("set_eta", etaArgs)
	if err != nil {
		t.Fatalf("set_eta local mutation: %v", err)
	}
	if !changed {
		t.Fatal("set_eta did not report a local change")
	}
	if err := syncer.ApplyToolCall(ctx, "set_eta", etaArgs, result); err != nil {
		t.Fatalf("set_eta Jira sync: %v", err)
	}

	priorityArgs := `{"card_id":"` + issueKey + `","priority":"High"}`
	result, changed, err = board.ApplyToolCall("set_priority", priorityArgs)
	if err != nil {
		t.Fatalf("set_priority local mutation: %v", err)
	}
	if !changed {
		t.Fatal("set_priority did not report a local change")
	}
	if err := syncer.ApplyToolCall(ctx, "set_priority", priorityArgs, result); err != nil {
		t.Fatalf("set_priority Jira sync: %v", err)
	}

	commentText := "Comment from auto-bot live Jira integration test."
	commentArgs := `{"card_id":"` + issueKey + `","comment":"` + commentText + `"}`
	result, changed, err = board.ApplyToolCall("add_comment", commentArgs)
	if err != nil {
		t.Fatalf("add_comment local mutation: %v", err)
	}
	if !changed {
		t.Fatal("add_comment did not report a local change")
	}
	if err := syncer.ApplyToolCall(ctx, "add_comment", commentArgs, result); err != nil {
		t.Fatalf("add_comment Jira sync: %v", err)
	}

	blockedArgs := `{"card_id":"` + issueKey + `","reason":"Waiting on live test validation.","tags":["blocked-live"]}`
	result, changed, err = board.ApplyToolCall("set_blocked", blockedArgs)
	if err != nil {
		t.Fatalf("set_blocked local mutation: %v", err)
	}
	if !changed {
		t.Fatal("set_blocked did not report a local change")
	}
	if err := syncer.ApplyToolCall(ctx, "set_blocked", blockedArgs, result); err != nil {
		t.Fatalf("set_blocked Jira sync: %v", err)
	}

	issue = getLiveJiraIssue(t, ctx, syncer.client, issueKey)
	if issue.Fields.Status.Name != "Blocked" {
		t.Fatalf("blocked issue status = %q, want Blocked", issue.Fields.Status.Name)
	}
	if issue.Fields.DueDate != "2026-05-20" {
		t.Fatalf("duedate = %q, want 2026-05-20", issue.Fields.DueDate)
	}
	if issue.Fields.Priority.Name != "High" {
		t.Fatalf("priority = %q, want High", issue.Fields.Priority.Name)
	}
	if !containsString(issue.Fields.Labels, "blocked") || !containsString(issue.Fields.Labels, "blocked-live") {
		t.Fatalf("labels = %v, want blocked labels", issue.Fields.Labels)
	}
	if !containsFlagValue(issue.Fields.Flagged, "Impediment") {
		t.Fatalf("flagged = %+v, want Impediment", issue.Fields.Flagged)
	}
	if !containsCommentText(issue.Fields.Comment.Comments, commentText) {
		t.Fatalf("comments = %+v, want %q", issue.Fields.Comment.Comments, commentText)
	}

	moveArgs := `{"card_id":"` + issueKey + `","status":"In Progress"}`
	result, changed, err = board.ApplyToolCall("move_ticket", moveArgs)
	if err != nil {
		t.Fatalf("move_ticket local mutation: %v", err)
	}
	if !changed {
		t.Fatal("move_ticket did not report a local change")
	}
	if err := syncer.ApplyToolCall(ctx, "move_ticket", moveArgs, result); err != nil {
		t.Fatalf("move_ticket Jira sync: %v", err)
	}

	issue = getLiveJiraIssue(t, ctx, syncer.client, issueKey)
	if issue.Fields.Status.Name != "In Progress" {
		t.Fatalf("moved issue status = %q, want In Progress", issue.Fields.Status.Name)
	}
	if containsFlagValue(issue.Fields.Flagged, "Impediment") {
		t.Fatalf("flagged = %+v, want Impediment cleared after leaving Blocked", issue.Fields.Flagged)
	}

	deleteArgs := `{"card_id":"` + issueKey + `"}`
	result, changed, err = board.ApplyToolCall("delete_ticket", deleteArgs)
	if err != nil {
		t.Fatalf("delete_ticket local mutation: %v", err)
	}
	if !changed {
		t.Fatal("delete_ticket did not report a local change")
	}
	if err := syncer.ApplyToolCall(ctx, "delete_ticket", deleteArgs, result); err != nil {
		t.Fatalf("delete_ticket Jira sync: %v", err)
	}

	issue = getLiveJiraIssue(t, ctx, syncer.client, issueKey)
	if issue.Fields.Status.Name != "Done" {
		t.Fatalf("closed issue status = %q, want Done", issue.Fields.Status.Name)
	}
}

func getLiveJiraIssue(t *testing.T, ctx context.Context, client *jiraClient, issueKey string) liveJiraIssue {
	t.Helper()

	var issue liveJiraIssue
	if err := client.doJSON(ctx, "GET", "/rest/api/3/issue/"+issueKey+"?fields=summary,description,labels,status,duedate,priority,comment,customfield_10021", nil, &issue); err != nil {
		t.Fatalf("get Jira issue %s: %v", issueKey, err)
	}
	return issue
}

func findCardIDByTitle(state kanbanBoardState, title string) string {
	for _, card := range state.Cards {
		if card.Title == title {
			return card.ID
		}
	}
	return ""
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsFlagValue(values []struct {
	Value string `json:"value"`
}, want string) bool {
	for _, value := range values {
		if value.Value == want {
			return true
		}
	}
	return false
}

func containsCommentText(comments []struct {
	Body any `json:"body"`
}, want string) bool {
	for _, comment := range comments {
		if strings.Contains(jiraADFPlainText(comment.Body), want) {
			return true
		}
	}
	return false
}
