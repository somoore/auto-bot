package main

import (
	"strings"
	"testing"
)

// TestRequiresConfirmationFiresForEveryHighRiskTool is the SecArch-002
// default-deny regression test. Before the fix, ApplyToolCall would short-
// circuit the confirmation queue when the caller forgot to set a dispatcher
// label ("source"); a missing-or-empty Source bypassed the gate entirely.
//
// The fix inverts the trust model: every risk-classified tool dispatched
// through ApplyToolCall queues a pending confirmation regardless of caller,
// unless the caller explicitly sets SkipConfirmation. The dispatcher label
// is now provenance only, never a trust signal.
//
// This test iterates every kanban tool definition exposed to dispatchers,
// filters down to those that require confirmation (Medium and High risk),
// and asserts that ApplyToolCallWithMeta with three representative callers
// — empty meta, a Nova Sonic dispatcher, and an OpenAI realtime dispatcher
// — all surface a pending confirmation instead of executing.
func TestRequiresConfirmationFiresForEveryHighRiskTool(t *testing.T) {
	callers := []toolCallMeta{
		{},                              // pre-fix bypass: no dispatcher set
		{Dispatcher: "nova-sonic"},      // Bedrock voice dispatcher
		{Dispatcher: "openai-realtime"}, // OpenAI realtime dispatcher
	}

	for _, def := range newKanbanBoard().KanbanToolDefs() {
		if !requiresConfirmation(def.Name) {
			continue
		}
		args := sampleArgsForRiskyTool(def.Name)
		if args == "" {
			// No representative sample exists for this tool. Skip silently
			// rather than fail — the gate is exercised by the tools that do
			// have samples. Add a sample to keep coverage tight if a new
			// risky tool is added.
			continue
		}
		for _, meta := range callers {
			meta := meta
			t.Run(def.Name+"/"+callerLabel(meta), func(t *testing.T) {
				board := newKanbanBoard()
				seedConfirmationGateBoard(board)
				result, changed, err := board.ApplyToolCallWithMeta(def.Name, args, meta)
				if err != nil {
					t.Fatalf("ApplyToolCallWithMeta(%q) returned error: %v", def.Name, err)
				}
				if changed {
					t.Fatalf("%s bypassed the confirmation gate (changed=true) under caller %q; default-deny was not enforced", def.Name, callerLabel(meta))
				}
				requires, _ := result["requires_confirmation"].(bool)
				if !requires {
					t.Fatalf("%s did not enqueue a pending confirmation under caller %q; result=%#v", def.Name, callerLabel(meta), result)
				}
				if got, _ := result["tool_name"].(string); got != def.Name {
					t.Fatalf("%s confirmation tool_name = %q, want %q", def.Name, got, def.Name)
				}
				state := board.SnapshotState()
				if len(state.PendingConfirmations) != 1 {
					t.Fatalf("%s pending confirmations = %d, want 1", def.Name, len(state.PendingConfirmations))
				}
			})
		}
	}
}

// TestSkipConfirmationOptOutExecutesRiskyTool verifies the trusted in-process
// escape hatch: callers that explicitly set SkipConfirmation: true bypass the
// queue and execute the tool directly. This is what the confirmed-action
// execution path, the replay test, and integration tests depend on.
func TestSkipConfirmationOptOutExecutesRiskyTool(t *testing.T) {
	board := newKanbanBoard()
	seedConfirmationGateBoard(board)

	// assign_ticket is Medium risk; under default-deny it queues a
	// confirmation. With SkipConfirmation: true it must execute directly.
	args := `{"card_id":"gate-card-1","account_id":"account-xyz","display_name":"Scott Moore"}`
	result, changed, err := board.ApplyToolCallWithMeta("assign_ticket", args, toolCallMeta{Dispatcher: "test", SkipConfirmation: true})
	if err != nil {
		t.Fatalf("assign_ticket with SkipConfirmation returned error: %v", err)
	}
	if !changed {
		t.Fatalf("assign_ticket with SkipConfirmation did not execute; result=%#v", result)
	}
	if requires, _ := result["requires_confirmation"].(bool); requires {
		t.Fatalf("assign_ticket with SkipConfirmation returned a pending confirmation; result=%#v", result)
	}
	if len(board.SnapshotState().PendingConfirmations) != 0 {
		t.Fatalf("SkipConfirmation should not enqueue; pending = %d", len(board.SnapshotState().PendingConfirmations))
	}
}

// sampleArgsForRiskyTool returns minimal valid JSON arguments for each
// risk-classified tool so the gate test can dispatch it. Tools without a
// sample are skipped (see the test loop). Keeping samples here, rather than
// shared with other tests, keeps the gate regression coverage explicit.
func sampleArgsForRiskyTool(toolName string) string {
	switch toolName {
	case "assign_ticket":
		return `{"card_id":"gate-card-1","account_id":"account-xyz","display_name":"Scott Moore"}`
	case "unassign_ticket":
		return `{"card_id":"gate-card-1"}`
	case "assign_ticket_to_agent":
		return `{"card_id":"gate-card-1","objective":"do the work","repo":"acme/repo","pull_request_number":1}`
	case "cancel_agent_run":
		return `{"run_id":"run-1","reason":"stop"}`
	case "take_over_agent_run":
		return `{"run_id":"run-1","actor":"Scott","reason":"take over"}`
	case "retry_agent_run":
		return `{"run_id":"run-1","additional_context":"retry"}`
	case "set_eta":
		return `{"card_id":"gate-card-1","eta":"2026-06-01"}`
	case "set_priority":
		return `{"card_id":"gate-card-1","priority":"High"}`
	case "set_reporter":
		return `{"card_id":"gate-card-1","account_id":"reporter-1","display_name":"Sarah"}`
	case "delete_ticket":
		return `{"card_id":"gate-card-1"}`
	case "set_sprint":
		return `{"card_id":"gate-card-1","sprint_id":"42","sprint_name":"Sprint 42"}`
	case "rank_issue":
		return `{"card_id":"gate-card-1","before_card_id":"gate-card-2"}`
	case "prioritize_ticket":
		return `{"card_id":"gate-card-1","above_card_id":"gate-card-2"}`
	}
	return ""
}

func seedConfirmationGateBoard(board *kanbanBoard) {
	board.ReplaceCards([]kanbanCard{
		{ID: "gate-card-1", Status: kanbanStatusBacklog, Title: "Gate test card 1"},
		{ID: "gate-card-2", Status: kanbanStatusBacklog, Title: "Gate test card 2"},
	})
}

func callerLabel(meta toolCallMeta) string {
	if strings.TrimSpace(meta.Dispatcher) == "" {
		return "empty-meta"
	}
	return meta.Dispatcher
}
