package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadJiraConfigFromTokenFile(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "jira-token")
	if err := os.WriteFile(tokenPath, []byte("secret-token\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	configPath := filepath.Join(dir, "jira.json")
	rawConfig := `{
		"base_url":"https://example.atlassian.net",
		"email":"bot@example.com",
		"api_token_file":"` + tokenPath + `",
		"project_key":"KAN",
		"issue_type":"Task",
		"status_mappings":{"To Do":"Backlog","In Progress":"In Progress","Blocked":"Blocked","Done":"Done"},
		"transitions":{"In Progress":"21","Blocked":"31","Done":"41","Deleted":"51"}
	}`
	if err := os.WriteFile(configPath, []byte(rawConfig), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	config, err := loadJiraConfig(context.Background(), configPath)
	if err != nil {
		t.Fatalf("loadJiraConfig returned error: %v", err)
	}
	if config.APIToken != "secret-token" {
		t.Fatalf("APIToken = %q, want secret-token", config.APIToken)
	}
	if got := config.mapJiraStatus("Blocked"); got != kanbanStatusBlocked {
		t.Fatalf("Blocked maps to %q, want %q", got, kanbanStatusBlocked)
	}
}

func TestJiraToolRequiresSync(t *testing.T) {
	if !jiraToolRequiresSync("move_ticket", `{"card_id":"KAN-7","status":"In Progress"}`, nil) {
		t.Fatal("move_ticket should require Jira sync")
	}
	if !jiraToolRequiresSync("prioritize_ticket", `{"card_id":"KAN-7","above_card_id":"KAN-8"}`, nil) {
		t.Fatal("prioritize_ticket should require Jira sync")
	}
	if !jiraToolRequiresSync("confirm_action", `{}`, map[string]any{"original_tool_name": "assign_ticket"}) {
		t.Fatal("confirmed assign_ticket should require Jira sync")
	}
	if !jiraToolRequiresSync("record_participant_update", `{}`, map[string]any{"update": scrumParticipantUpdate{CardID: "KAN-7"}}) {
		t.Fatal("participant update tied to a card should require Jira sync")
	}
	if jiraToolRequiresSync("record_participant_update", `{"participant":"Scott","summary":"No ticket"}`, map[string]any{}) {
		t.Fatal("participant update without a card should not require Jira sync")
	}
	if jiraToolRequiresSync("record_meeting_memory", `{"kind":"decision"}`, nil) {
		t.Fatal("meeting memory should not require Jira sync")
	}
}

func TestAnnotateJiraSyncResult(t *testing.T) {
	previous := jiraSync
	t.Cleanup(func() { jiraSync = previous })

	jiraSync = nil
	result := map[string]any{}
	annotateJiraSyncResult(result, true, nil)
	status, ok := result["jira_sync"].(map[string]any)
	if !ok || status["ok"] != false || status["configured"] != false {
		t.Fatalf("jira_sync without configured syncer = %#v, want ok=false configured=false", result["jira_sync"])
	}

	jiraSync = &jiraSyncer{}
	result = map[string]any{}
	annotateJiraSyncResult(result, true, context.Canceled)
	status, ok = result["jira_sync"].(map[string]any)
	errorMessage, _ := status["error"].(string)
	if !ok || status["ok"] != false || status["configured"] != true || errorMessage == "" {
		t.Fatalf("jira_sync with error = %#v, want ok=false configured=true with error", result["jira_sync"])
	}

	result = map[string]any{}
	annotateJiraSyncResult(result, true, nil)
	status, ok = result["jira_sync"].(map[string]any)
	if !ok || status["ok"] != true || status["configured"] != true {
		t.Fatalf("jira_sync success = %#v, want ok=true configured=true", result["jira_sync"])
	}

	result = map[string]any{}
	annotateJiraSyncResult(result, false, nil)
	if _, ok := result["jira_sync"]; ok {
		t.Fatalf("jira_sync was annotated for non-Jira action: %#v", result["jira_sync"])
	}
}

func TestJiraClientSearchKanbanCards(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rest/api/3/search/jql" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var requestBody struct {
			Fields []string `json:"fields"`
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode search request: %v", err)
		}
		for _, field := range []string{"summary", "description", "labels", "status", "assignee", "duedate", "priority", "comment", "customfield_10021"} {
			if !boardTestContainsString(requestBody.Fields, field) {
				t.Fatalf("search fields = %v, want %q", requestBody.Fields, field)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"issues":[{
				"key":"KAN-7",
				"fields":{
					"summary":"Wire Jira sync",
					"description":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"Hydrate the board from Jira."}]}]},
					"labels":["jira","sync"],
					"status":{"name":"In Progress"},
					"assignee":{"accountId":"account-123","displayName":"Scott Moore","emailAddress":"somoore2025@gmail.com","active":true},
					"duedate":"2026-05-20",
					"priority":{"name":"High"},
					"customfield_10021":[{"value":"Impediment","id":"10019"}],
					"comment":{"comments":[{"id":"10001","author":{"displayName":"Scott Moore"},"created":"2026-05-15T12:00:00.000+0000","body":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"Blocked: Waiting on workflow setup."}]}]}}]}
				}
			}],
			"isLast":true
		}`))
	}))
	defer server.Close()

	client := newJiraClient(&jiraConfig{
		BaseURL:          server.URL,
		Email:            "bot@example.com",
		APIToken:         "token",
		ProjectKey:       "KAN",
		IssueType:        "Task",
		StatusMappings:   map[string]string{"In Progress": "In Progress"},
		Transitions:      map[string]string{},
		BlockedFlagField: "customfield_10021",
		BlockedFlagValue: "Impediment",
	})

	cards, err := client.SearchKanbanCards(context.Background())
	if err != nil {
		t.Fatalf("SearchKanbanCards returned error: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("cards length = %d, want 1", len(cards))
	}
	card := cards[0]
	if card.ID != "KAN-7" || card.Status != kanbanStatusBlocked || card.Title != "Wire Jira sync" {
		t.Fatalf("unexpected card: %+v", card)
	}
	if card.Notes != "Hydrate the board from Jira." {
		t.Fatalf("card notes = %q", card.Notes)
	}
	if card.Assignee == nil || card.Assignee.DisplayName != "Scott Moore" {
		t.Fatalf("card assignee = %+v, want Scott Moore", card.Assignee)
	}
	if card.DueDate != "2026-05-20" {
		t.Fatalf("card due date = %q, want 2026-05-20", card.DueDate)
	}
	if card.Priority != "High" {
		t.Fatalf("card priority = %q, want High", card.Priority)
	}
	if card.BlockedReason != "Waiting on workflow setup." {
		t.Fatalf("card blocked reason = %q", card.BlockedReason)
	}
	if len(card.Comments) != 1 || card.Comments[0].Body != "Blocked: Waiting on workflow setup." {
		t.Fatalf("card comments = %+v", card.Comments)
	}
}

func TestJiraClientRejectsSearchResultsOutsideConfiguredProject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rest/api/3/search/jql" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"issues":[{
				"key":"OTHER-7",
				"fields":{
					"summary":"Should not hydrate",
					"status":{"name":"To Do"}
				}
			}],
			"isLast":true
		}`))
	}))
	defer server.Close()

	client := newJiraClient(&jiraConfig{
		BaseURL:        server.URL,
		Email:          "bot@example.com",
		APIToken:       "token",
		ProjectKey:     "KAN",
		IssueType:      "Task",
		StatusMappings: map[string]string{"To Do": "Backlog"},
		Transitions:    map[string]string{},
	})

	_, err := client.SearchKanbanCards(context.Background())
	if err == nil {
		t.Fatal("SearchKanbanCards returned nil error for issue outside configured project")
	}
	if !strings.Contains(err.Error(), `outside configured project "KAN"`) {
		t.Fatalf("SearchKanbanCards error = %q, want configured project guard", err)
	}
}

func TestJiraSyncCreateTicketRenamesLocalCardToIssueKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rest/api/3/issue" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode Jira create body: %v", err)
		}
		fields := body["fields"].(map[string]any)
		if fields["summary"] != "Create Jira bridge" {
			t.Fatalf("summary = %v", fields["summary"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key":"KAN-42"}`))
	}))
	defer server.Close()

	board := newKanbanBoard()
	syncer := &jiraSyncer{
		board: board,
		client: newJiraClient(&jiraConfig{
			BaseURL:        server.URL,
			Email:          "bot@example.com",
			APIToken:       "token",
			ProjectKey:     "KAN",
			IssueType:      "Task",
			StatusMappings: map[string]string{},
			Transitions:    map[string]string{},
		}),
	}

	result, changed, err := board.ApplyToolCall("create_ticket", `{"title":"Create Jira bridge","notes":"Write-through local mutations to Jira.","tags":["jira"],"status":"Backlog"}`)
	if err != nil {
		t.Fatalf("create_ticket returned error: %v", err)
	}
	if !changed {
		t.Fatal("create_ticket should mark board changed")
	}
	localID := result["card"].(kanbanCard).ID

	if err := syncer.ApplyToolCall(context.Background(), "create_ticket", `{}`, result); err != nil {
		t.Fatalf("ApplyToolCall returned error: %v", err)
	}
	renamedCard := result["card"].(kanbanCard)
	if renamedCard.ID != "KAN-42" {
		t.Fatalf("result card id = %q, want KAN-42", renamedCard.ID)
	}
	if result["card_id"] != "KAN-42" || result["previous_card_id"] != localID {
		t.Fatalf("result ids = %#v, want card_id KAN-42 and previous_card_id %s", result, localID)
	}

	state := board.SnapshotState()
	var foundJiraKey bool
	for _, card := range state.Cards {
		if card.ID == localID {
			t.Fatalf("local card id %s was not renamed", localID)
		}
		if card.ID == "KAN-42" {
			foundJiraKey = true
		}
	}
	if !foundJiraKey {
		t.Fatal("renamed Jira issue KAN-42 not found on board")
	}
}

func TestJiraClientCreateSubtaskUsesProjectIssueTypeID(t *testing.T) {
	var sawProjectLookup bool
	var sawCreate bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/project/KAN":
			sawProjectLookup = true
			_, _ = w.Write([]byte(`{
				"key":"KAN",
				"issueTypes":[
					{"id":"10001","name":"Task","subtask":false},
					{"id":"10002","name":"Subtask","subtask":true}
				]
			}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/3/issue":
			sawCreate = true
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode Jira create body: %v", err)
			}
			fields := body["fields"].(map[string]any)
			issueType := fields["issuetype"].(map[string]any)
			if issueType["id"] != "10002" {
				t.Fatalf("issuetype = %#v, want id 10002", issueType)
			}
			parent := fields["parent"].(map[string]any)
			if parent["key"] != "KAN-1" {
				t.Fatalf("parent = %#v, want KAN-1", parent)
			}
			_, _ = w.Write([]byte(`{"key":"KAN-42"}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := newJiraClient(&jiraConfig{
		BaseURL:    server.URL,
		Email:      "bot@example.com",
		APIToken:   "token",
		ProjectKey: "KAN",
		IssueType:  "Task",
	})

	issueKey, err := client.CreateIssue(context.Background(), kanbanCard{
		Title:     "Talk to the dev team",
		Notes:     "Follow-up from standup.",
		IssueType: "Sub-task",
		ParentID:  "KAN-1",
	})
	if err != nil {
		t.Fatalf("CreateIssue returned error: %v", err)
	}
	if issueKey != "KAN-42" || !sawProjectLookup || !sawCreate {
		t.Fatalf("issueKey=%q sawProjectLookup=%v sawCreate=%v, want KAN-42 true true", issueKey, sawProjectLookup, sawCreate)
	}
}

func TestJiraRenamedCardAliasAllowsConfirmedAssignment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rest/api/3/issue" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key":"KAN-42"}`))
	}))
	defer server.Close()

	board := newKanbanBoard()
	syncer := &jiraSyncer{
		board: board,
		client: newJiraClient(&jiraConfig{
			BaseURL:        server.URL,
			Email:          "bot@example.com",
			APIToken:       "token",
			ProjectKey:     "KAN",
			IssueType:      "Task",
			StatusMappings: map[string]string{},
			Transitions:    map[string]string{},
		}),
	}

	result, changed, err := board.ApplyToolCall("create_ticket", `{"title":"Assign after Jira rename","status":"Backlog"}`)
	if err != nil {
		t.Fatalf("create_ticket returned error: %v", err)
	}
	if !changed {
		t.Fatal("create_ticket should mark board changed")
	}
	localID := result["card"].(kanbanCard).ID
	if err := syncer.ApplyToolCall(context.Background(), "create_ticket", `{}`, result); err != nil {
		t.Fatalf("create_ticket Jira sync returned error: %v", err)
	}

	assignArgs := `{"card_id":"` + localID + `","account_id":"account-123","display_name":"Scott Moore"}`
	result, changed, err = board.ApplyToolCallWithMeta("assign_ticket", assignArgs, toolCallMeta{Dispatcher: "nova-sonic"})
	if err != nil {
		t.Fatalf("assign_ticket returned error: %v", err)
	}
	if changed {
		t.Fatal("assign_ticket should wait for confirmation")
	}
	if result["confirmation_id"] == "" {
		t.Fatalf("assign_ticket result = %#v, want confirmation id", result)
	}
	pending := board.SnapshotState().PendingConfirmations
	if len(pending) != 1 {
		t.Fatalf("pending confirmations = %d, want 1", len(pending))
	}
	if pending[0].MatchedCardID != "KAN-42" {
		t.Fatalf("pending matched card = %q, want KAN-42", pending[0].MatchedCardID)
	}

	result, changed, err = board.ApplyToolCallWithMeta("confirm_action", `{}`, toolCallMeta{Dispatcher: "nova-sonic"})
	if err != nil {
		t.Fatalf("confirm_action returned error: %v", err)
	}
	if !changed {
		t.Fatal("confirm_action should apply assignment")
	}
	if result["card_id"] != "KAN-42" {
		t.Fatalf("confirmed card_id = %v, want KAN-42", result["card_id"])
	}
	originalArgs, ok := result["original_arguments"].(map[string]any)
	if !ok || originalArgs["card_id"] != "KAN-42" {
		t.Fatalf("original arguments = %#v, want card_id KAN-42", result["original_arguments"])
	}

	card := findBoardTestCard(t, board.SnapshotState().Cards, "KAN-42")
	if card.Assignee == nil || card.Assignee.DisplayName != "Scott Moore" {
		t.Fatalf("Assignee = %+v, want Scott Moore", card.Assignee)
	}
}

func TestJiraClientRejectsCreatedIssueOutsideConfiguredProject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rest/api/3/issue" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key":"OTHER-42"}`))
	}))
	defer server.Close()

	client := newJiraClient(&jiraConfig{
		BaseURL:    server.URL,
		Email:      "bot@example.com",
		APIToken:   "token",
		ProjectKey: "KAN",
		IssueType:  "Task",
	})

	_, err := client.CreateIssue(context.Background(), kanbanCard{Title: "Create Jira bridge"})
	if err == nil {
		t.Fatal("CreateIssue returned nil error for created issue outside configured project")
	}
	if !strings.Contains(err.Error(), `outside configured project "KAN"`) {
		t.Fatalf("CreateIssue error = %q, want configured project guard", err)
	}
}

func TestJiraClientTaskActionRequests(t *testing.T) {
	type requestRecord struct {
		Method string
		Path   string
		Query  string
		Body   map[string]any
	}
	var requests []requestRecord

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record := requestRecord{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.RawQuery,
		}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&record.Body)
		}
		requests = append(requests, record)

		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/user/assignable/search":
			_, _ = w.Write([]byte(`[{"accountId":"account-123","displayName":"Scott Moore","emailAddress":"somoore2025@gmail.com","active":true}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/3/priority":
			_, _ = w.Write([]byte(`[{"name":"High"},{"name":"Medium"}]`))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	client := newJiraClient(&jiraConfig{
		BaseURL:          server.URL,
		Email:            "bot@example.com",
		APIToken:         "token",
		ProjectKey:       "KAN",
		IssueType:        "Task",
		BlockedFlagField: "customfield_10021",
		BlockedFlagValue: "Impediment",
	})

	users, err := client.SearchAssignableUsers(context.Background(), "scott")
	if err != nil {
		t.Fatalf("SearchAssignableUsers returned error: %v", err)
	}
	if len(users) != 1 || users[0].AccountID != "account-123" {
		t.Fatalf("users = %+v, want account-123", users)
	}
	priorities, err := client.ListPriorities(context.Background())
	if err != nil {
		t.Fatalf("ListPriorities returned error: %v", err)
	}
	if len(priorities) != 2 || priorities[0] != "High" {
		t.Fatalf("priorities = %+v, want High first", priorities)
	}
	if err := client.AssignIssue(context.Background(), "KAN-7", "account-123"); err != nil {
		t.Fatalf("AssignIssue returned error: %v", err)
	}
	if err := client.AddComment(context.Background(), "KAN-7", "Ready for review"); err != nil {
		t.Fatalf("AddComment returned error: %v", err)
	}
	if err := client.SetDueDate(context.Background(), "KAN-7", "2026-05-20"); err != nil {
		t.Fatalf("SetDueDate returned error: %v", err)
	}
	if err := client.SetPriority(context.Background(), "KAN-7", "High"); err != nil {
		t.Fatalf("SetPriority returned error: %v", err)
	}
	if err := client.RemoveLabels(context.Background(), "KAN-7", []string{"Needs Review"}); err != nil {
		t.Fatalf("RemoveLabels returned error: %v", err)
	}
	if err := client.SetBlockedFlag(context.Background(), "KAN-7", true); err != nil {
		t.Fatalf("SetBlockedFlag returned error: %v", err)
	}

	if len(requests) != 8 {
		t.Fatalf("request count = %d, want 8: %+v", len(requests), requests)
	}
	if requests[0].Path != "/rest/api/3/user/assignable/search" || requests[0].Query == "" {
		t.Fatalf("assignable user request = %+v", requests[0])
	}
	if got := requests[2].Body["accountId"]; got != "account-123" {
		t.Fatalf("assign body accountId = %v, want account-123", got)
	}
	commentBody := requests[3].Body["body"].(map[string]any)
	if got := jiraADFPlainText(commentBody); got != "Ready for review" {
		t.Fatalf("comment ADF text = %q, want Ready for review", got)
	}
	dueFields := requests[4].Body["fields"].(map[string]any)
	if got := dueFields["duedate"]; got != "2026-05-20" {
		t.Fatalf("duedate = %v, want 2026-05-20", got)
	}
	priorityFields := requests[5].Body["fields"].(map[string]any)
	priority := priorityFields["priority"].(map[string]any)
	if got := priority["name"]; got != "High" {
		t.Fatalf("priority name = %v, want High", got)
	}
	labelUpdate := requests[6].Body["update"].(map[string]any)["labels"].([]any)[0].(map[string]any)
	if got := labelUpdate["remove"]; got != "needs-review" {
		t.Fatalf("removed label = %v, want needs-review", got)
	}
	flagFields := requests[7].Body["fields"].(map[string]any)
	flagValues := flagFields["customfield_10021"].([]any)[0].(map[string]any)
	if got := flagValues["value"]; got != "Impediment" {
		t.Fatalf("blocked flag value = %v, want Impediment", got)
	}
}

func TestJiraClientRejectsCrossProjectWrites(t *testing.T) {
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
		Transitions:      map[string]string{"In Progress": "21"},
		DeleteTransition: "31",
		BlockedFlagField: "customfield_10021",
		BlockedFlagValue: "Impediment",
	})

	actions := []struct {
		name string
		run  func() error
	}{
		{"UpdateIssue", func() error { return client.UpdateIssue(context.Background(), "OTHER-7", "New title", "") }},
		{"AddLabels", func() error { return client.AddLabels(context.Background(), "OTHER-7", []string{"urgent"}) }},
		{"RemoveLabels", func() error { return client.RemoveLabels(context.Background(), "OTHER-7", []string{"urgent"}) }},
		{"AssignIssue", func() error { return client.AssignIssue(context.Background(), "OTHER-7", "account-123") }},
		{"AddComment", func() error { return client.AddComment(context.Background(), "OTHER-7", "Ready for review") }},
		{"SetDueDate", func() error { return client.SetDueDate(context.Background(), "OTHER-7", "2026-05-20") }},
		{"SetPriority", func() error { return client.SetPriority(context.Background(), "OTHER-7", "High") }},
		{"SetBlockedFlag", func() error { return client.SetBlockedFlag(context.Background(), "OTHER-7", true) }},
		{"TransitionIssue", func() error { return client.TransitionIssue(context.Background(), "OTHER-7", kanbanStatusInProgress) }},
		{"CloseIssue", func() error { return client.CloseIssue(context.Background(), "OTHER-7") }},
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
