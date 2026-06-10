package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestScrumMasterMeetingToolContracts(t *testing.T) {
	board := newKanbanBoard()

	startResult, _ := requireContractToolOK(t, board, "start_meeting", `{
		"meeting_id":"daily-2026-05-15",
		"meeting_type":"daily_standup",
		"sprint_id":"sprint-42",
		"sprint_name":"Platform Sprint 42",
		"participants":[
			{"participant_id":"acct-1","display_name":"Scott Moore","role":"engineering manager"},
			{"participant_id":"acct-2","display_name":"Avery Chen","role":"backend engineer"}
		],
		"agenda":["yesterday","today","blockers","risks"]
	}`)
	requireContractStringField(t, startResult, "meeting_id", "daily-2026-05-15")
	requireContractAnyField(t, startResult, "status", "meeting")

	recordResult, _ := requireContractToolOK(t, board, "record_participant_update", `{
		"meeting_id":"daily-2026-05-15",
		"participant_id":"acct-2",
		"display_name":"Avery Chen",
		"spoken_text":"Yesterday I finished the LiveKit token hardening. Today I am wiring sprint metadata. I am blocked by the Jira custom field ids.",
		"completed":["LiveKit token hardening"],
		"planned":["Wire sprint metadata"],
		"blockers":["Need Jira custom field ids"],
		"risks":["Sprint metadata may slip without Jira field discovery"],
		"ticket_refs":["EMAL-14"]
	}`)
	requireContractAnyField(t, recordResult, "participant_update", "recorded", "update")

	nextResult, _ := requireContractToolOK(t, board, "next_speaker", `{
		"meeting_id":"daily-2026-05-15",
		"current_participant_id":"acct-2"
	}`)
	requireContractAnyField(t, nextResult, "next_participant", "speaker", "prompt")

	summaryResult, _ := requireContractToolOK(t, board, "summarize_meeting", `{
		"meeting_id":"daily-2026-05-15",
		"include_participants":true,
		"include_ticket_changes":true,
		"include_blockers":true,
		"include_action_items":true
	}`)
	requireContractAnyField(t, summaryResult, "summary")
	requireContractAnyField(t, summaryResult, "action_items", "blockers", "ticket_changes")

	endResult, _ := requireContractToolOK(t, board, "end_meeting", `{
		"meeting_id":"daily-2026-05-15",
		"outcome":"completed",
		"publish_summary":true
	}`)
	requireContractAnyField(t, endResult, "ended", "status", "summary")
}

func TestJiraExtendedIssueMetadataToolContracts(t *testing.T) {
	t.Run("create_ticket_accepts_issue_type", func(t *testing.T) {
		board := newKanbanBoard()
		result, _ := requireContractToolOK(t, board, "create_ticket", `{
			"title":"Build scrum master Jira metadata",
			"notes":"Track issue type on the local card and Jira payload.",
			"tags":["jira","metadata"],
			"status":"Backlog",
			"issue_type":"Story"
		}`)
		cardID := requireContractCardID(t, result)
		requireContractCardJSONField(t, board, cardID, "issueType")
	})

	t.Run("create_subtask_sets_parent_and_issue_type", func(t *testing.T) {
		board := newKanbanBoard()
		parentID := createContractCard(t, board, "Parent story for scrum automation")

		result, _ := requireContractToolOK(t, board, "create_subtask", fmt.Sprintf(`{
			"parent_card_id":%q,
			"title":"Wire sprint picker",
			"notes":"Implement sprint lookup and assignment for the parent story.",
			"tags":["jira","sprint"],
			"assignee_query":"Avery"
		}`, parentID))
		subtaskID := requireContractCardID(t, result)
		requireContractCardJSONField(t, board, subtaskID, "issueType")
		requireContractCardJSONField(t, board, subtaskID, "parentId")
	})

	t.Run("set_story_points", func(t *testing.T) {
		board := newKanbanBoard()
		cardID := createContractCard(t, board, "Estimate sprint metadata work")

		requireContractToolOK(t, board, "set_story_points", fmt.Sprintf(`{
			"card_id":%q,
			"story_points":5
		}`, cardID))
		requireContractCardJSONField(t, board, cardID, "storyPoints")
	})

	t.Run("set_estimate", func(t *testing.T) {
		board := newKanbanBoard()
		cardID := createContractCard(t, board, "Estimate board sync work")

		requireContractToolOK(t, board, "set_estimate", fmt.Sprintf(`{
			"card_id":%q,
			"original_estimate":"2d",
			"remaining_estimate":"1d 4h"
		}`, cardID))
		requireContractCardJSONField(t, board, cardID, "estimate")
	})

	t.Run("set_sprint", func(t *testing.T) {
		board := newKanbanBoard()
		cardID := createContractCard(t, board, "Assign work to active sprint")

		requireContractToolOK(t, board, "set_sprint", fmt.Sprintf(`{
			"card_id":%q,
			"sprint_id":"42",
			"sprint_name":"Platform Sprint 42",
			"state":"active"
		}`, cardID))
		requireContractCardJSONField(t, board, cardID, "sprint")
	})

	t.Run("rank_issue", func(t *testing.T) {
		board := newKanbanBoard()
		firstID := createContractCard(t, board, "First backlog item")
		secondID := createContractCard(t, board, "Second backlog item")

		requireContractToolOK(t, board, "rank_issue", fmt.Sprintf(`{
			"card_id":%q,
			"before_card_id":%q
		}`, secondID, firstID))
		requireContractCardJSONField(t, board, secondID, "rank")
	})

	t.Run("prioritize_ticket", func(t *testing.T) {
		board := newKanbanBoard()
		firstID := createContractCard(t, board, "First blocked item")
		secondID := createContractCard(t, board, "Second backlog item")

		requireContractToolOK(t, board, "move_ticket", fmt.Sprintf(`{
			"card_id":%q,
			"status":"Blocked"
		}`, firstID))
		requireContractToolOK(t, board, "prioritize_ticket", fmt.Sprintf(`{
			"card_id":%q,
			"above_card_id":%q
		}`, secondID, firstID))
		requireContractCardJSONField(t, board, secondID, "rank")
	})

	t.Run("set_components", func(t *testing.T) {
		board := newKanbanBoard()
		cardID := createContractCard(t, board, "Tag Jira components")

		requireContractToolOK(t, board, "set_components", fmt.Sprintf(`{
			"card_id":%q,
			"components":["Voice Agent","Jira Sync"]
		}`, cardID))
		requireContractCardJSONField(t, board, cardID, "components")
	})

	t.Run("set_fix_versions", func(t *testing.T) {
		board := newKanbanBoard()
		cardID := createContractCard(t, board, "Plan release vehicle")

		requireContractToolOK(t, board, "set_fix_versions", fmt.Sprintf(`{
			"card_id":%q,
			"fix_versions":["scrum-master-mvp"]
		}`, cardID))
		requireContractCardJSONField(t, board, cardID, "fixVersions")
	})

	t.Run("metadata_and_transition_discovery_are_non_mutating", func(t *testing.T) {
		board := newKanbanBoard()
		cardID := createContractCard(t, board, "Discover Jira metadata")
		before := board.SnapshotState().SequenceNumber

		metadataResult, metadataChanged := requireContractToolOK(t, board, "get_jira_metadata", `{
			"refresh":false,
			"include_fields":true,
			"include_issue_types":true,
			"include_sprints":true,
			"include_components":true,
			"include_fix_versions":true
		}`)
		if metadataChanged {
			t.Fatal("get_jira_metadata should not mutate board state")
		}
		requireContractAnyField(t, metadataResult, "issue_types", "fields", "sprints", "components", "fix_versions")

		transitionResult, transitionChanged := requireContractToolOK(t, board, "get_transition_options", fmt.Sprintf(`{
			"card_id":%q
		}`, cardID))
		if transitionChanged {
			t.Fatal("get_transition_options should not mutate board state")
		}
		requireContractAnyField(t, transitionResult, "transitions", "statuses")

		if got := board.SnapshotState().SequenceNumber; got != before {
			t.Fatalf("metadata discovery changed sequence number from %d to %d", before, got)
		}
	})
}

func TestJiraDependencyWorklogAndCustomFieldToolContracts(t *testing.T) {
	t.Run("link_issues_records_dependency", func(t *testing.T) {
		board := newKanbanBoard()
		sourceID := createContractCard(t, board, "Consumer feature")
		targetID := createContractCard(t, board, "Provider API")

		requireContractToolOK(t, board, "link_issues", fmt.Sprintf(`{
			"source_card_id":%q,
			"target_card_id":%q,
			"link_type":"blocks",
			"relationship":"Consumer feature is blocked by Provider API"
		}`, sourceID, targetID))
		requireContractCardJSONField(t, board, sourceID, "issueLinks")
	})

	t.Run("add_worklog_records_time_spent", func(t *testing.T) {
		board := newKanbanBoard()
		cardID := createContractCard(t, board, "Record implementation worklog")

		requireContractToolOK(t, board, "add_worklog", fmt.Sprintf(`{
			"card_id":%q,
			"time_spent":"1h 30m",
			"started_at":"2026-05-15T14:00:00Z",
			"comment":"Paired on Jira metadata discovery and sprint assignment."
		}`, cardID))
		requireContractCardJSONField(t, board, cardID, "worklogs")
	})

	t.Run("set_custom_field_records_typed_value", func(t *testing.T) {
		board := newKanbanBoard()
		cardID := createContractCard(t, board, "Capture Jira custom field")

		requireContractToolOK(t, board, "set_custom_field", fmt.Sprintf(`{
			"card_id":%q,
			"field_id":"customfield_10042",
			"field_name":"Customer Impact",
			"value_type":"string",
			"value":"High"
		}`, cardID))
		requireContractCardJSONField(t, board, cardID, "customFields")
	})
}

func TestExpandedScrumMasterToolDefinitions(t *testing.T) {
	board := newKanbanBoard()
	defs := map[string]kanbanToolDef{}
	for _, def := range board.KanbanToolDefs() {
		defs[def.Name] = def
	}

	expectedNewTools := map[string][]string{
		"start_meeting":             {"meeting_id", "meeting_type", "participants"},
		"record_participant_update": {"meeting_id", "participant_id", "spoken_text"},
		"next_speaker":              {"meeting_id"},
		"summarize_meeting":         {"meeting_id"},
		"end_meeting":               {"meeting_id"},
		"create_subtask":            {"parent_card_id", "title", "notes"},
		"set_story_points":          {"card_id", "story_points"},
		"set_estimate":              {"card_id", "original_estimate", "remaining_estimate"},
		"add_worklog":               {"card_id", "time_spent", "comment"},
		"link_issues":               {"source_card_id", "target_card_id", "link_type"},
		"set_sprint":                {"card_id", "sprint_id"},
		"prioritize_ticket":         {"card_id", "above_card_id", "below_card_id", "position"},
		"rank_issue":                {"card_id"},
		"set_components":            {"card_id", "components"},
		"set_fix_versions":          {"card_id", "fix_versions"},
		"set_custom_field":          {"card_id", "field_id", "value"},
		"get_jira_metadata":         {},
		"get_transition_options":    {"card_id"},
		"assign_ticket_to_agent":    {"card_id", "objective", "repo", "pull_request_number"},
		"list_agent_runs":           {},
		"get_agent_run":             {"run_id"},
	}

	for name, properties := range expectedNewTools {
		def, ok := defs[name]
		if !ok {
			t.Errorf("tool definition %q not found", name)
			continue
		}
		if strings.TrimSpace(def.Description) == "" {
			t.Errorf("tool definition %q has an empty description", name)
		}
		if got := def.Parameters["type"]; got != "object" {
			t.Errorf("tool definition %q parameter type = %v, want object", name, got)
		}
		if got := def.Parameters["additionalProperties"]; got != false {
			t.Errorf("tool definition %q additionalProperties = %v, want false", name, got)
		}
		requireContractToolProperties(t, def, properties...)
	}

	createTicketDef, ok := defs["create_ticket"]
	if !ok {
		t.Fatal("create_ticket tool definition not found")
	}
	requireContractToolProperties(t, createTicketDef, "issue_type")
}

func createContractCard(t *testing.T, board *kanbanBoard, title string) string {
	t.Helper()
	result, _ := requireContractToolOK(t, board, "create_ticket", fmt.Sprintf(`{
		"title":%q,
		"notes":"Contract-test setup card.",
		"tags":["contract"],
		"status":"Backlog"
	}`, title))
	return requireContractCardID(t, result)
}

func requireContractToolOK(t *testing.T, board *kanbanBoard, name string, args string) (map[string]any, bool) {
	t.Helper()
	result, changed, err := board.ApplyToolCall(name, args)
	if err != nil {
		t.Fatalf("%s returned error: %v", name, err)
	}
	if result == nil {
		t.Fatalf("%s returned nil result", name)
	}
	ok, exists := result["ok"]
	if !exists {
		t.Fatalf("%s result missing ok field: %#v", name, result)
	}
	if okBool, _ := ok.(bool); !okBool {
		t.Fatalf("%s ok = %v, want true; result = %#v", name, ok, result)
	}
	return result, changed
}

func requireContractStringField(t *testing.T, result map[string]any, field string, want string) {
	t.Helper()
	got, _ := result[field].(string)
	if got != want {
		t.Fatalf("%s = %q, want %q; result = %#v", field, got, want, result)
	}
}

func requireContractAnyField(t *testing.T, result map[string]any, fields ...string) {
	t.Helper()
	for _, field := range fields {
		if value, ok := result[field]; ok && !contractValueIsEmpty(value) {
			return
		}
	}
	t.Fatalf("result missing non-empty field from %v: %#v", fields, result)
}

func requireContractCardID(t *testing.T, result map[string]any) string {
	t.Helper()
	if cardID, _ := result["card_id"].(string); cardID != "" {
		return cardID
	}
	switch card := result["card"].(type) {
	case kanbanCard:
		if card.ID != "" {
			return card.ID
		}
	case map[string]any:
		if cardID, _ := card["id"].(string); cardID != "" {
			return cardID
		}
	}
	t.Fatalf("could not find card id in result: %#v", result)
	return ""
}

func requireContractCardJSONField(t *testing.T, board *kanbanBoard, cardID string, field string) any {
	t.Helper()
	card := requireContractCardJSON(t, board, cardID)
	value, ok := card[field]
	if !ok || contractValueIsEmpty(value) {
		t.Fatalf("card %s missing non-empty JSON field %q: %#v", cardID, field, card)
	}
	return value
}

func requireContractCardJSON(t *testing.T, board *kanbanBoard, cardID string) map[string]any {
	t.Helper()
	var state struct {
		Cards []map[string]any `json:"cards"`
	}
	if err := json.Unmarshal([]byte(board.BoardContextJSON()), &state); err != nil {
		t.Fatalf("BoardContextJSON did not return valid JSON: %v", err)
	}
	for _, card := range state.Cards {
		if got, _ := card["id"].(string); got == cardID {
			return card
		}
	}
	t.Fatalf("card %s not found in board JSON: %#v", cardID, state.Cards)
	return nil
}

func requireContractToolProperties(t *testing.T, def kanbanToolDef, properties ...string) {
	t.Helper()
	rawProperties, ok := def.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("tool %q properties have type %T, want map[string]any", def.Name, def.Parameters["properties"])
	}
	for _, property := range properties {
		if _, ok := rawProperties[property]; !ok {
			t.Errorf("tool %q missing property %q in %#v", def.Name, property, rawProperties)
		}
	}
}

func contractValueIsEmpty(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case []any:
		return len(typed) == 0
	case []string:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	default:
		return false
	}
}
