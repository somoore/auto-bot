package main

import (
	"strings"
	"testing"
)

func TestMeetingIntelligenceReportIncludesMeetingSprintGitHubAndObservability(t *testing.T) {
	board := newKanbanBoard()
	previousSharedBoard := sharedBoard
	sharedBoard = board
	t.Cleanup(func() { sharedBoard = previousSharedBoard })

	if _, changed, err := board.ApplyToolCallWithMeta("start_meeting", `{
		"meeting_id":"standup-intel-1",
		"meeting_type":"standup",
		"participants":["Scott","Sarah"],
		"agenda":["status","blockers","owners"]
	}`, toolCallMeta{Source: "nova-sonic"}); err != nil {
		t.Fatalf("start_meeting returned error: %v", err)
	} else if !changed {
		t.Fatal("start_meeting should mutate")
	}

	prResult, changed, err := board.ApplyToolCallWithMeta("create_ticket", `{
		"title":"Review LiveKit PR",
		"notes":"Pull request is ready for review.",
		"status":"In Progress",
		"tags":["pr-ready"]
	}`, toolCallMeta{Source: "nova-sonic"})
	if err != nil {
		t.Fatalf("create_ticket returned error: %v", err)
	}
	if !changed {
		t.Fatal("create_ticket should mutate")
	}
	prCard := prResult["card"].(kanbanCard)

	if _, changed, err := board.ApplyToolCallWithMeta("add_remote_link", `{
		"card_id":"`+prCard.ID+`",
		"url":"https://github.com/example/auto-bot/pull/42",
		"title":"Pull request 42 ready for review",
		"summary":"Ready for review"
	}`, toolCallMeta{Source: "nova-sonic"}); err != nil {
		t.Fatalf("add_remote_link returned error: %v", err)
	} else if !changed {
		t.Fatal("add_remote_link should mutate")
	}

	blockedResult, changed, err := board.ApplyToolCallWithMeta("create_ticket", `{
		"title":"Deploy LiveKit media path",
		"notes":"Needs AWS credentials and TURN validation.",
		"status":"In Progress"
	}`, toolCallMeta{Source: "nova-sonic"})
	if err != nil {
		t.Fatalf("create blocked ticket returned error: %v", err)
	}
	if !changed {
		t.Fatal("create blocked ticket should mutate")
	}
	blockedCard := blockedResult["card"].(kanbanCard)

	board.RecordTranscript("user", "Scott", "EMAL media path is blocked by AWS credentials.")
	if _, changed, err := board.ApplyToolCallWithMeta("set_blocked", `{
		"card_id":"`+blockedCard.ID+`",
		"reason":"AWS credentials expired during TURN validation",
		"tags":["livekit","aws"]
	}`, toolCallMeta{Source: "nova-sonic", Actor: "Scott", Transcript: "EMAL media path is blocked by AWS credentials."}); err != nil {
		t.Fatalf("set_blocked returned error: %v", err)
	} else if !changed {
		t.Fatal("set_blocked should mutate")
	}

	if _, changed, err := board.ApplyToolCallWithMeta("record_participant_update", `{
		"participant":"Sarah",
		"card_id":"`+blockedCard.ID+`",
		"spoken_text":"I will validate AWS credentials and TURN before the next demo.",
		"blocker":"AWS credentials expired during TURN validation",
		"follow_up":"Sarah to validate TURN from a mobile hotspot",
		"eta":"2026-05-20"
	}`, toolCallMeta{Source: "nova-sonic", Actor: "Sarah"}); err != nil {
		t.Fatalf("record_participant_update returned error: %v", err)
	} else if !changed {
		t.Fatal("record_participant_update should mutate")
	}

	report := board.BuildMeetingIntelligenceReport("unit-test")
	if !report.OK {
		t.Fatal("report OK = false")
	}
	if report.MeetingID != "standup-intel-1" {
		t.Fatalf("MeetingID = %q, want standup-intel-1", report.MeetingID)
	}
	if len(report.Participants) < 2 {
		t.Fatalf("participants = %d, want at least 2", len(report.Participants))
	}
	if len(report.JiraChanges) == 0 || len(report.TranscriptEvidence) == 0 {
		t.Fatalf("report missing mutation/evidence: changes=%d evidence=%d", len(report.JiraChanges), len(report.TranscriptEvidence))
	}
	if len(report.SprintIntelligence.BlockedCards) == 0 || len(report.SprintIntelligence.MissingETACards) == 0 {
		t.Fatalf("sprint intelligence missing blocked or missing ETA cards: %#v", report.SprintIntelligence)
	}
	if len(report.SprintIntelligence.PRReadyCards) == 0 {
		t.Fatalf("PRReadyCards empty: %#v", report.SprintIntelligence)
	}
	if len(report.GitHubContext.Signals) == 0 {
		t.Fatalf("GitHubContext.Signals empty: %#v", report.GitHubContext)
	}
	if report.Observability.VoiceProvider == "" || report.Setup.Region == "" || len(report.Setup.ProviderOptions) == 0 {
		t.Fatalf("report missing observability/setup: %#v %#v", report.Observability, report.Setup)
	}
	if report.ProductProof.EstimatedNetMinutesSaved == 0 || report.ProductProof.MeasurementQuality == "" {
		t.Fatalf("report missing product proof metrics: %#v", report.ProductProof)
	}
	if !strings.Contains(report.SlackSummary, "*Jira changes:*") {
		t.Fatalf("SlackSummary missing Jira section: %q", report.SlackSummary)
	}
	if !strings.Contains(report.SlackSummary, "*Product proof:*") {
		t.Fatalf("SlackSummary missing product proof section: %q", report.SlackSummary)
	}
}
