package core_test

import (
	"context"
	"testing"

	"github.com/somoore/auto-bot/internal/core"
)

func TestInMemoryActionLedgerReplaysIntentToolAndExternalConfirmation(t *testing.T) {
	ledger := core.NewInMemoryActionLedger()
	ctx := context.Background()

	intent, err := ledger.RecordIntent(ctx, core.ActionIntent{
		MeetingID: "meeting-1",
		Actor:     "scott",
		Connector: "jira",
		Action:    "move_issue",
		Target:    "EMAL-12",
		Risk:      core.RiskLow,
		Evidence: []core.Evidence{
			{Kind: "transcript", Source: "livekit", Text: "move EMAL-12 to in progress"},
		},
		Confidence: core.Confidence{Score: 0.94, Reasons: []string{"matched explicit issue key"}},
	})
	if err != nil {
		t.Fatalf("record intent: %v", err)
	}
	call, err := ledger.RecordToolCall(ctx, core.ToolCallRecord{
		IntentID: intent.ID,
		Tool:     "move_ticket",
		Arguments: map[string]any{
			"card_id": "EMAL-12",
			"status":  "In Progress",
		},
	})
	if err != nil {
		t.Fatalf("record tool call: %v", err)
	}
	if _, err := ledger.RecordExternalConfirmation(ctx, core.ExternalConfirmation{
		IntentID:   intent.ID,
		ToolCallID: call.ID,
		Connector:  "jira",
		Status:     "api_confirmed",
		ExternalID: "EMAL-12",
		Message:    "Jira transition returned 204",
	}); err != nil {
		t.Fatalf("record external confirmation: %v", err)
	}

	replay, err := ledger.Replay(ctx, intent.ID)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if replay.Intent.ID != intent.ID {
		t.Fatalf("replay intent id = %q, want %q", replay.Intent.ID, intent.ID)
	}
	if len(replay.ToolCalls) != 1 || replay.ToolCalls[0].Tool != "move_ticket" {
		t.Fatalf("tool calls = %#v", replay.ToolCalls)
	}
	if len(replay.Confirmations) != 1 || replay.Confirmations[0].Status != "api_confirmed" {
		t.Fatalf("confirmations = %#v", replay.Confirmations)
	}
	if len(replay.Steps) != 3 {
		t.Fatalf("steps = %d, want 3", len(replay.Steps))
	}
}
