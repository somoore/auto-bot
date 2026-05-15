package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJiraClientAdvancedScrumMasterRequests(t *testing.T) {
	type requestRecord struct {
		Method string
		Path   string
		Query  string
		Body   any
	}
	var requests []requestRecord

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record := requestRecord{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&record.Body)
		}
		requests = append(requests, record)

		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/issue":
			_, _ = w.Write([]byte(`{"key":"KAN-99"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/issue/KAN-7/transitions":
			_, _ = w.Write([]byte(`{"transitions":[{"id":"21","name":"Start Progress","to":{"name":"In Progress"},"fields":{}}]}`))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	points := 5.0
	client := newJiraClient(&jiraConfig{
		BaseURL:           server.URL,
		Email:             "bot@example.com",
		APIToken:          "token",
		ProjectKey:        "KAN",
		IssueType:         "Task",
		StoryPointsField:  "customfield_10016",
		SprintField:       "customfield_10020",
		EpicLinkField:     "customfield_10014",
		RankCustomFieldID: 10019,
	})

	if _, err := client.CreateIssue(context.Background(), kanbanCard{
		Title:       "Create advanced story",
		Notes:       "With planning metadata.",
		IssueType:   "Story",
		ParentID:    "KAN-1",
		EpicKey:     "KAN-2",
		StoryPoints: &points,
		Components:  []string{"Voice Agent"},
		FixVersions: []string{"scrum-master-mvp"},
	}); err != nil {
		t.Fatalf("CreateIssue returned error: %v", err)
	}
	if err := client.SetStoryPoints(context.Background(), "KAN-7", 8); err != nil {
		t.Fatalf("SetStoryPoints returned error: %v", err)
	}
	if err := client.SetEstimate(context.Background(), "KAN-7", "2d", "1d"); err != nil {
		t.Fatalf("SetEstimate returned error: %v", err)
	}
	if err := client.AddWorklog(context.Background(), "KAN-7", "1h", 3600, "2026-05-15T14:00:00Z", "Paired on sync"); err != nil {
		t.Fatalf("AddWorklog returned error: %v", err)
	}
	if err := client.LinkIssues(context.Background(), "KAN-7", "KAN-8", "Blocks", "outward", "Blocked by API"); err != nil {
		t.Fatalf("LinkIssues returned error: %v", err)
	}
	if err := client.SetSprint(context.Background(), "KAN-7", 42); err != nil {
		t.Fatalf("SetSprint returned error: %v", err)
	}
	if err := client.RankIssue(context.Background(), "KAN-7", "KAN-8", ""); err != nil {
		t.Fatalf("RankIssue returned error: %v", err)
	}
	if err := client.SetComponents(context.Background(), "KAN-7", []string{"Voice Agent", "Jira Sync"}); err != nil {
		t.Fatalf("SetComponents returned error: %v", err)
	}
	if err := client.SetFixVersions(context.Background(), "KAN-7", []string{"scrum-master-mvp"}); err != nil {
		t.Fatalf("SetFixVersions returned error: %v", err)
	}
	if err := client.SetCustomField(context.Background(), "KAN-7", "customfield_10042", "High"); err != nil {
		t.Fatalf("SetCustomField returned error: %v", err)
	}
	if err := client.AddRemoteLink(context.Background(), "KAN-7", "https://example.com/design", "Design brief", "Planning link"); err != nil {
		t.Fatalf("AddRemoteLink returned error: %v", err)
	}
	if err := client.SetReporter(context.Background(), "KAN-7", "account-123"); err != nil {
		t.Fatalf("SetReporter returned error: %v", err)
	}
	if err := client.AddWatcher(context.Background(), "KAN-7", "account-456"); err != nil {
		t.Fatalf("AddWatcher returned error: %v", err)
	}
	transitions, err := client.GetTransitions(context.Background(), "KAN-7")
	if err != nil {
		t.Fatalf("GetTransitions returned error: %v", err)
	}
	if len(transitions) != 1 || transitions[0]["id"] != "21" {
		t.Fatalf("transitions = %#v, want transition 21", transitions)
	}

	if len(requests) != 14 {
		t.Fatalf("request count = %d, want 14: %#v", len(requests), requests)
	}
	createFields := requests[0].Body.(map[string]any)["fields"].(map[string]any)
	if got := createFields["issuetype"].(map[string]any)["name"]; got != "Story" {
		t.Fatalf("create issuetype = %v, want Story", got)
	}
	if got := createFields["parent"].(map[string]any)["key"]; got != "KAN-1" {
		t.Fatalf("create parent = %v, want KAN-1", got)
	}
	if got := createFields["customfield_10014"]; got != "KAN-2" {
		t.Fatalf("create epic link = %v, want KAN-2", got)
	}
	if got := createFields["customfield_10016"]; got != 5.0 {
		t.Fatalf("create story points = %v, want 5", got)
	}

	pointsFields := requests[1].Body.(map[string]any)["fields"].(map[string]any)
	if got := pointsFields["customfield_10016"]; got != 8.0 {
		t.Fatalf("story points payload = %v, want 8", got)
	}
	worklogBody := requests[3].Body.(map[string]any)
	if got := worklogBody["timeSpent"]; got != "1h" {
		t.Fatalf("worklog timeSpent = %v, want 1h", got)
	}
	linkBody := requests[4].Body.(map[string]any)
	if got := linkBody["type"].(map[string]any)["name"]; got != "Blocks" {
		t.Fatalf("link type = %v, want Blocks", got)
	}
	if requests[5].Path != "/rest/agile/1.0/sprint/42/issue" {
		t.Fatalf("sprint request path = %s", requests[5].Path)
	}
	rankBody := requests[6].Body.(map[string]any)
	if got := rankBody["rankBeforeIssue"]; got != "KAN-8" {
		t.Fatalf("rankBeforeIssue = %v, want KAN-8", got)
	}
	if got := rankBody["rankCustomFieldId"]; got != 10019.0 {
		t.Fatalf("rankCustomFieldId = %v, want 10019", got)
	}
	remoteObject := requests[10].Body.(map[string]any)["object"].(map[string]any)
	if got := remoteObject["url"]; got != "https://example.com/design" {
		t.Fatalf("remote link url = %v", got)
	}
	if watcherAccount, ok := requests[12].Body.(string); !ok || watcherAccount != "account-456" {
		t.Fatalf("watcher body = %#v, want account-456", requests[12].Body)
	}
}

func TestJiraClientAdvancedWritesRejectCrossProjectIssues(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := newJiraClient(&jiraConfig{
		BaseURL:          server.URL,
		Email:            "bot@example.com",
		APIToken:         "token",
		ProjectKey:       "KAN",
		IssueType:        "Task",
		StoryPointsField: "customfield_10016",
	})

	actions := []struct {
		name string
		run  func() error
	}{
		{"SetStoryPoints", func() error { return client.SetStoryPoints(context.Background(), "OTHER-7", 5) }},
		{"SetEstimate", func() error { return client.SetEstimate(context.Background(), "OTHER-7", "2d", "1d") }},
		{"AddWorklog", func() error { return client.AddWorklog(context.Background(), "OTHER-7", "1h", 0, "", "") }},
		{"LinkIssuesSource", func() error {
			return client.LinkIssues(context.Background(), "OTHER-7", "KAN-8", "Blocks", "outward", "")
		}},
		{"LinkIssuesTarget", func() error {
			return client.LinkIssues(context.Background(), "KAN-7", "OTHER-8", "Blocks", "outward", "")
		}},
		{"SetSprint", func() error { return client.SetSprint(context.Background(), "OTHER-7", 42) }},
		{"RankIssue", func() error { return client.RankIssue(context.Background(), "OTHER-7", "KAN-8", "") }},
		{"SetComponents", func() error { return client.SetComponents(context.Background(), "OTHER-7", []string{"Voice Agent"}) }},
		{"SetFixVersions", func() error { return client.SetFixVersions(context.Background(), "OTHER-7", []string{"v1"}) }},
		{"SetCustomField", func() error {
			return client.SetCustomField(context.Background(), "OTHER-7", "customfield_10042", "High")
		}},
		{"AddRemoteLink", func() error {
			return client.AddRemoteLink(context.Background(), "OTHER-7", "https://example.com", "Example", "")
		}},
		{"SetReporter", func() error { return client.SetReporter(context.Background(), "OTHER-7", "account-123") }},
		{"AddWatcher", func() error { return client.AddWatcher(context.Background(), "OTHER-7", "account-456") }},
	}

	for _, action := range actions {
		err := action.run()
		if err == nil {
			t.Fatalf("%s returned nil error for issue outside configured project", action.name)
		}
		if !strings.Contains(err.Error(), `outside configured project "KAN"`) {
			t.Fatalf("%s error = %q, want configured project guard", action.name, err)
		}
	}
	if requests != 0 {
		t.Fatalf("Jira server received %d request(s), want zero", requests)
	}
}
