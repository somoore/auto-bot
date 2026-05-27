package main

import (
	"context"
	"testing"
)

// TestPreviewPendingActionShowsCreatedCard ensures the diff exposes the
// newly-created card under CreatedCardIDs without mutating the live board.
func TestPreviewPendingActionShowsCreatedCard(t *testing.T) {
	settingsStore := newMemoryTenantSettingsStore()
	actionStore := newMemoryPendingActionStore()
	manager := newTenantSettingsManager(settingsStore)
	restore := withDryRunRuntime(manager, actionStore)
	defer restore()

	ctx := context.Background()
	if _, err := manager.Set(ctx, tenantSettings{TenantID: defaultTenantID, DryRunEnabled: true}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	board := newKanbanBoard()
	beforeCount := len(board.SnapshotState().Cards)
	rawArgs := `{"title":"Preview me","notes":"queued","tags":["preview"],"status":"Backlog"}`
	result, _, err := board.ApplyToolCall("create_ticket", rawArgs)
	if err != nil {
		t.Fatalf("ApplyToolCall: %v", err)
	}
	actionID, _ := result["action_id"].(string)

	diff, err := board.PreviewPendingAction(ctx, actionID)
	if err != nil {
		t.Fatalf("PreviewPendingAction: %v", err)
	}
	if diff.Error != "" {
		t.Fatalf("diff carried error %q", diff.Error)
	}
	if len(diff.CreatedCardIDs) != 1 {
		t.Fatalf("expected exactly one created card, got %v", diff.CreatedCardIDs)
	}
	if len(diff.After) != beforeCount+1 {
		t.Fatalf("expected after count=%d, got %d", beforeCount+1, len(diff.After))
	}
	if len(diff.Before) != beforeCount {
		t.Fatalf("expected before count=%d, got %d", beforeCount, len(diff.Before))
	}
	if diff.SequenceAfter <= diff.SequenceBefore {
		t.Fatalf("expected sequence_after > sequence_before, got %d/%d", diff.SequenceAfter, diff.SequenceBefore)
	}
	// And confirm the live board is still the original size — preview must
	// not leak state.
	if got := len(board.SnapshotState().Cards); got != beforeCount {
		t.Fatalf("preview mutated board state: cards now %d, want %d", got, beforeCount)
	}
}
