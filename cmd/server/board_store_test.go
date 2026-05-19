package main

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSQLiteBoardStorePersistsSnapshotsAndEvents(t *testing.T) {
	store, err := newSQLiteBoardStore(filepath.Join(t.TempDir(), "board.sqlite"))
	if err != nil {
		t.Fatalf("newSQLiteBoardStore returned error: %v", err)
	}
	defer store.Close()

	board, err := newPersistentKanbanBoard("team-board", store)
	if err != nil {
		t.Fatalf("newPersistentKanbanBoard returned error: %v", err)
	}
	result, changed, err := board.ApplyToolCall("create_ticket", `{"title":"Persisted ticket","notes":"Stored in SQLite","tags":["db"],"status":"In Progress"}`)
	if err != nil {
		t.Fatalf("create_ticket returned error: %v", err)
	}
	if !changed {
		t.Fatal("create_ticket should mark board changed")
	}
	created := result["card"].(kanbanCard)

	reloaded, err := newPersistentKanbanBoard("team-board", store)
	if err != nil {
		t.Fatalf("reload board returned error: %v", err)
	}
	state := reloaded.SnapshotState()
	if state.SequenceNumber != board.SnapshotState().SequenceNumber {
		t.Fatalf("reloaded sequence = %d, want %d", state.SequenceNumber, board.SnapshotState().SequenceNumber)
	}
	var found bool
	for _, card := range state.Cards {
		if card.ID == created.ID && card.Title == "Persisted ticket" && card.Status == kanbanStatusInProgress {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("reloaded state did not include created card: %+v", state.Cards)
	}

	var eventCount int
	if err := store.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM board_events WHERE board_id = ?`, "team-board").Scan(&eventCount); err != nil {
		t.Fatalf("count board events: %v", err)
	}
	if eventCount < 2 {
		t.Fatalf("eventCount = %d, want at least initial snapshot and create event", eventCount)
	}
}

func TestSQLiteBoardStorePersistsMeetingReports(t *testing.T) {
	store, err := newSQLiteBoardStore(filepath.Join(t.TempDir(), "board.sqlite"))
	if err != nil {
		t.Fatalf("newSQLiteBoardStore returned error: %v", err)
	}
	defer store.Close()

	board, err := newPersistentKanbanBoard("team-board", store)
	if err != nil {
		t.Fatalf("newPersistentKanbanBoard returned error: %v", err)
	}
	previousSharedBoard := sharedBoard
	sharedBoard = board
	t.Cleanup(func() { sharedBoard = previousSharedBoard })

	if _, changed, err := board.ApplyToolCallWithMeta("start_meeting", `{"meeting_id":"standup-report-1","meeting_type":"standup","participants":["Scott","Sarah"],"agenda":["blockers","owners"]}`, toolCallMeta{Source: "nova-sonic"}); err != nil {
		t.Fatalf("start_meeting returned error: %v", err)
	} else if !changed {
		t.Fatal("start_meeting should mutate")
	}

	createResult, changed, err := board.ApplyToolCallWithMeta("create_ticket", `{"title":"Report persistence work","notes":"Need archived intelligence","status":"In Progress","tags":["reporting"]}`, toolCallMeta{Source: "nova-sonic"})
	if err != nil {
		t.Fatalf("create_ticket returned error: %v", err)
	}
	if !changed {
		t.Fatal("create_ticket should mutate")
	}
	card := createResult["card"].(kanbanCard)

	if _, changed, err := board.ApplyToolCallWithMeta("record_participant_update", `{"participant":"Sarah","card_id":"`+card.ID+`","spoken_text":"I will finish the report page today.","follow_up":"Sarah to validate post-meeting report","eta":"2026-05-20"}`, toolCallMeta{Source: "nova-sonic"}); err != nil {
		t.Fatalf("record_participant_update returned error: %v", err)
	} else if !changed {
		t.Fatal("record_participant_update should mutate")
	}

	if _, changed, err := board.ApplyToolCallWithMeta("end_meeting", `{"decision":"Ship the report archive.","action_items":["Scott to review the recap"]}`, toolCallMeta{Source: "nova-sonic"}); err != nil {
		t.Fatalf("end_meeting returned error: %v", err)
	} else if !changed {
		t.Fatal("end_meeting should mutate")
	}

	summaries, err := store.ListMeetingReports(context.Background(), "team-board", 10)
	if err != nil {
		t.Fatalf("ListMeetingReports returned error: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("summaries length = %d, want 1", len(summaries))
	}
	if summaries[0].MeetingID != "standup-report-1" {
		t.Fatalf("MeetingID = %q, want standup-report-1", summaries[0].MeetingID)
	}
	if summaries[0].JiraChangeCnt == 0 || summaries[0].ActionItemCnt == 0 {
		t.Fatalf("summary counts = %#v, want Jira changes and action items", summaries[0])
	}

	report, found, err := store.LoadMeetingReport(context.Background(), "team-board", "standup-report-1")
	if err != nil {
		t.Fatalf("LoadMeetingReport returned error: %v", err)
	}
	if !found {
		t.Fatal("meeting report not found")
	}
	if report.Active {
		t.Fatal("archived report should not be active")
	}
	if report.SlackSummary == "" || len(report.JiraChanges) == 0 {
		t.Fatalf("report missing recap or mutations: %#v", report.SummaryView())
	}
}

func TestSQLiteBoardStorePersistsAgentRuns(t *testing.T) {
	store, err := newSQLiteBoardStore(filepath.Join(t.TempDir(), "board.sqlite"))
	if err != nil {
		t.Fatalf("newSQLiteBoardStore returned error: %v", err)
	}
	defer store.Close()

	board, err := newPersistentKanbanBoard("team-board", store)
	if err != nil {
		t.Fatalf("newPersistentKanbanBoard returned error: %v", err)
	}
	previousOrchestrator := agentOrchestrator
	agentOrchestrator = nil
	t.Cleanup(func() { agentOrchestrator = previousOrchestrator })

	createResult, changed, err := board.ApplyToolCall("create_ticket", `{"title":"Review persistence PR","notes":"Agent should review this.","tags":["pr-7"],"status":"Backlog"}`)
	if err != nil {
		t.Fatalf("create_ticket returned error: %v", err)
	}
	if !changed {
		t.Fatal("create_ticket should mutate")
	}
	card := createResult["card"].(kanbanCard)
	runResult, changed, err := board.ApplyToolCall("assign_ticket_to_agent", `{"card_id":"`+card.ID+`","objective":"conduct a code review","repo":"scottmoore/auto-bot","pull_request_number":7}`)
	if err != nil {
		t.Fatalf("assign_ticket_to_agent returned error: %v", err)
	}
	if !changed {
		t.Fatal("assign_ticket_to_agent should mutate")
	}
	run := runResult["agent_run"].(agentRunView)

	stored, found, err := store.LoadAgentRun(context.Background(), "team-board", run.RunID)
	if err != nil {
		t.Fatalf("LoadAgentRun returned error: %v", err)
	}
	if !found {
		t.Fatal("agent run not found")
	}
	if stored.CardID != card.ID || stored.PullRequestNumber != 7 {
		t.Fatalf("stored run = %#v, want card/pr", stored)
	}

	reloaded, err := newPersistentKanbanBoard("team-board", store)
	if err != nil {
		t.Fatalf("reload board returned error: %v", err)
	}
	if got := reloaded.SnapshotState().AgentRuns; len(got) != 1 || got[0].RunID != run.RunID {
		t.Fatalf("reloaded agent runs = %#v, want persisted run", got)
	}
}
