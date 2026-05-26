package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestDryRunStagesToolCall verifies that when a tenant has DryRunEnabled the
// ApplyToolCallWithMeta path queues the call into pending_actions instead of
// mutating the board.
func TestDryRunStagesToolCall(t *testing.T) {
	settingsStore := newMemoryTenantSettingsStore()
	actionStore := newMemoryPendingActionStore()
	manager := newTenantSettingsManager(settingsStore)
	restore := withDryRunRuntime(manager, actionStore)
	defer restore()

	ctx := context.Background()
	if _, err := manager.Set(ctx, tenantSettings{TenantID: defaultTenantID, DryRunEnabled: true}); err != nil {
		t.Fatalf("Set tenant settings: %v", err)
	}

	board := newKanbanBoard()

	rawArgs := `{"title":"Stage me","notes":"queued via dry-run","tags":["dry"],"status":"Backlog"}`
	result, changed, err := board.ApplyToolCall("create_ticket", rawArgs)
	if err != nil {
		t.Fatalf("ApplyToolCall returned err: %v", err)
	}
	if changed {
		t.Fatalf("changed=true while dry-run is enabled; board should not have mutated")
	}
	if result["requires_approval"] != true {
		t.Fatalf("expected requires_approval=true, got %#v", result)
	}
	actionID, _ := result["action_id"].(string)
	if actionID == "" {
		t.Fatalf("expected an action_id in result, got %#v", result)
	}

	// confirm only one queued item exists and it carries the right tool
	pending, err := actionStore.ListPendingActions(ctx, defaultTenantID, board.boardID, false, 10)
	if err != nil {
		t.Fatalf("ListPendingActions: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending action, got %d", len(pending))
	}
	if pending[0].Tool != "create_ticket" {
		t.Fatalf("expected pending tool=create_ticket, got %s", pending[0].Tool)
	}
	if pending[0].Status != pendingActionStatusPending {
		t.Fatalf("expected status=pending, got %s", pending[0].Status)
	}
	if pending[0].Args["title"] != "Stage me" {
		t.Fatalf("expected args.title=Stage me, got %#v", pending[0].Args)
	}

	// board should still be empty (no cards created)
	state := board.SnapshotState()
	for _, c := range state.Cards {
		if c.Title == "Stage me" {
			t.Fatalf("dry-run leaked a card into board state: %#v", c)
		}
	}
}

// TestDryRunApproveExecutesAction queues a tool call in dry-run mode, then
// approves it and confirms the underlying mutation lands on the board.
func TestDryRunApproveExecutesAction(t *testing.T) {
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
	rawArgs := `{"title":"Approve me","notes":"queued via dry-run","tags":["dry"],"status":"Backlog"}`
	result, _, err := board.ApplyToolCall("create_ticket", rawArgs)
	if err != nil {
		t.Fatalf("ApplyToolCall: %v", err)
	}
	actionID, _ := result["action_id"].(string)

	// Disable dry-run before approve so the underlying execution path is
	// the regular trusted one. SkipConfirmation in approvePendingAction
	// covers the case where dry-run remains on, but the simpler trail keeps
	// the test focused.
	if _, err := manager.Set(ctx, tenantSettings{TenantID: defaultTenantID, DryRunEnabled: false}); err != nil {
		t.Fatalf("Disable dry-run: %v", err)
	}

	action, applyResult, _, err := board.approvePendingAction(ctx, actionID, "tester", "ui", "approved in test")
	if err != nil {
		t.Fatalf("approvePendingAction: %v", err)
	}
	if action.Status != pendingActionStatusApplied {
		t.Fatalf("expected applied status, got %s", action.Status)
	}
	if applyResult == nil {
		t.Fatalf("expected apply result, got nil")
	}

	// Board should now contain the card.
	found := false
	for _, c := range board.SnapshotState().Cards {
		if c.Title == "Approve me" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected board to contain newly-applied card after approve")
	}
}

// TestDryRunRejectKeepsBoardClean ensures a rejected action does not run.
func TestDryRunRejectKeepsBoardClean(t *testing.T) {
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
	rawArgs := `{"title":"Reject me","notes":"queued via dry-run","tags":["dry"],"status":"Backlog"}`
	result, _, err := board.ApplyToolCall("create_ticket", rawArgs)
	if err != nil {
		t.Fatalf("ApplyToolCall: %v", err)
	}
	actionID, _ := result["action_id"].(string)

	action, err := board.rejectPendingAction(ctx, actionID, "tester", "ui", "no")
	if err != nil {
		t.Fatalf("rejectPendingAction: %v", err)
	}
	if action.Status != pendingActionStatusRejected {
		t.Fatalf("expected rejected status, got %s", action.Status)
	}
	for _, c := range board.SnapshotState().Cards {
		if c.Title == "Reject me" {
			t.Fatalf("board contained a rejected card: %#v", c)
		}
	}

	// Second reject attempt should error as the action is terminal.
	_, err = board.rejectPendingAction(ctx, actionID, "tester", "ui", "again")
	if !errors.Is(err, ErrPendingActionTerminal) {
		t.Fatalf("expected ErrPendingActionTerminal, got %v", err)
	}
}

// TestDryRunExpireSweep marks past-deadline actions as expired.
func TestDryRunExpireSweep(t *testing.T) {
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
	// Stage an action whose expires_at is already in the past.
	past := time.Now().UTC().Add(-time.Hour)
	if err := actionStore.SavePendingAction(ctx, pendingAction{
		TenantID:  defaultTenantID,
		BoardID:   board.boardID,
		ActionID:  "pa_expired",
		Tool:      "create_ticket",
		Args:      map[string]any{"title": "old"},
		CreatedAt: past.Add(-time.Hour).Format(time.RFC3339Nano),
		ExpiresAt: past.Format(time.RFC3339Nano),
		Status:    pendingActionStatusPending,
	}); err != nil {
		t.Fatalf("seed pending action: %v", err)
	}

	count, err := actionStore.ExpirePendingActions(ctx, defaultTenantID, board.boardID, time.Now().UTC())
	if err != nil {
		t.Fatalf("ExpirePendingActions: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 expired action, got %d", count)
	}
	loaded, err := actionStore.LoadPendingAction(ctx, defaultTenantID, board.boardID, "pa_expired")
	if err != nil {
		t.Fatalf("LoadPendingAction: %v", err)
	}
	if loaded.Status != pendingActionStatusExpired {
		t.Fatalf("expected expired, got %s", loaded.Status)
	}
}

// TestDryRunOffPassesThrough ensures regular operation is unchanged when the
// tenant has not opted into dry-run.
func TestDryRunOffPassesThrough(t *testing.T) {
	settingsStore := newMemoryTenantSettingsStore()
	actionStore := newMemoryPendingActionStore()
	manager := newTenantSettingsManager(settingsStore)
	restore := withDryRunRuntime(manager, actionStore)
	defer restore()

	board := newKanbanBoard()
	rawArgs := `{"title":"Direct","notes":"no dry-run","tags":["direct"],"status":"Backlog"}`
	_, changed, err := board.ApplyToolCall("create_ticket", rawArgs)
	if err != nil {
		t.Fatalf("ApplyToolCall: %v", err)
	}
	if !changed {
		t.Fatalf("expected board mutation when dry-run is off")
	}
	pending, err := actionStore.ListPendingActions(context.Background(), defaultTenantID, board.boardID, true, 10)
	if err != nil {
		t.Fatalf("ListPendingActions: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected no pending actions when dry-run is off, got %d", len(pending))
	}
}
