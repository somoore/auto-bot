package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// dryRunRegistry holds the process-wide tenantSettingsManager and pending
// action store. They are set once in main() and read by the hot-path code in
// board.go and the HTTP handlers below.
type dryRunRegistry struct {
	mu       sync.RWMutex
	settings *tenantSettingsManager
	actions  pendingActionStore
}

var dryRunReg = &dryRunRegistry{}

// installDryRunRuntime is called from main once the boardStore is up. It
// wires the production stores into the registry. Tests reach for
// withDryRunRuntime instead so they get an isolated in-memory pair.
func installDryRunRuntime(store boardStore) {
	dryRunReg.mu.Lock()
	defer dryRunReg.mu.Unlock()
	if settingsStore, ok := store.(tenantSettingsStore); ok {
		dryRunReg.settings = newTenantSettingsManager(settingsStore)
	} else {
		dryRunReg.settings = newTenantSettingsManager(newMemoryTenantSettingsStore())
	}
	if actionStore, ok := store.(pendingActionStore); ok {
		dryRunReg.actions = actionStore
	} else {
		dryRunReg.actions = newMemoryPendingActionStore()
	}
}

// withDryRunRuntime swaps in test stores and returns a restore function. Tests
// must defer the returned function to keep the global state clean for the
// next test in the same package.
func withDryRunRuntime(settings *tenantSettingsManager, actions pendingActionStore) func() {
	dryRunReg.mu.Lock()
	prevSettings, prevActions := dryRunReg.settings, dryRunReg.actions
	dryRunReg.settings = settings
	dryRunReg.actions = actions
	dryRunReg.mu.Unlock()
	return func() {
		dryRunReg.mu.Lock()
		dryRunReg.settings = prevSettings
		dryRunReg.actions = prevActions
		dryRunReg.mu.Unlock()
	}
}

func globalTenantSettingsManager() *tenantSettingsManager {
	dryRunReg.mu.RLock()
	defer dryRunReg.mu.RUnlock()
	return dryRunReg.settings
}

func globalPendingActionStore() pendingActionStore {
	dryRunReg.mu.RLock()
	defer dryRunReg.mu.RUnlock()
	return dryRunReg.actions
}

// metaTools enumerates tool names that bypass dry-run staging because they
// operate on the staging queue itself, audit log, or replay surface.
var metaTools = map[string]struct{}{
	"confirm_action":             {},
	"cancel_confirmation":        {},
	"list_pending_confirmations": {},
	"undo_last_mutation":         {},
	"get_audit_events":           {},
	"replay_audit_event":         {},
	"resolve_jira_conflict":      {},
	"get_board":                  {},
	"get_card":                   {},
	"list_priorities":            {},
	"list_agent_runs":            {},
	"get_agent_run":              {},
	"search_jira_users":          {},
	"list_pending_actions":       {},
	"approve_pending_action":     {},
	"reject_pending_action":      {},
	"get_pending_action_preview": {},
}

// shouldStageInDryRun returns true when the tool is a write-side operation
// that should be queued instead of applied while dry-run is enabled. The
// allow-list of pass-through meta-tools keeps the queue itself usable.
func shouldStageInDryRun(toolName string) bool {
	if _, ok := metaTools[toolName]; ok {
		return false
	}
	return true
}

// stagePendingAction is invoked by ApplyToolCallWithMeta when the tenant has
// dry-run enabled. It persists the action and broadcasts a `pending_action`
// WS event so the React queue can render the new entry in real time. The
// returned map is the same shape every other tool returns so callers see a
// uniform `ok=false, requires_approval=true, action_id=...` envelope.
func (board *kanbanBoard) stagePendingAction(toolName string, args map[string]any, rawArgs string, meta toolCallMeta) (map[string]any, error) {
	store := globalPendingActionStore()
	if store == nil {
		return nil, fmt.Errorf("dry-run mode is enabled but no pending action store is configured")
	}
	actionID := generatePendingActionID()
	now := time.Now().UTC()
	expiresAt := now.Add(defaultPendingActionTTL)
	intent := map[string]any{
		"dispatcher": meta.Dispatcher,
		"actor":      meta.Actor,
		"call_id":    meta.CallID,
		"transcript": meta.Transcript,
		"raw_args":   rawArgs,
	}
	action := pendingAction{
		TenantID:  board.tenantID,
		BoardID:   board.boardID,
		ActionID:  actionID,
		Tool:      toolName,
		Args:      cloneToolArgs(args),
		Intent:    intent,
		CreatedAt: now.Format(time.RFC3339Nano),
		ExpiresAt: expiresAt.Format(time.RFC3339Nano),
		Status:    pendingActionStatusPending,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := store.SavePendingAction(ctx, action); err != nil {
		return nil, fmt.Errorf("stage pending action: %w", err)
	}
	broadcastKanbanEventForBoard(board.tenantID, board.boardID, "pending_action", action)
	prompt := fmt.Sprintf("I would have run %s but dry-run mode is enabled. Action %s is queued for review.", toolName, actionID)
	return map[string]any{
		"ok":                false,
		"dry_run":           true,
		"requires_approval": true,
		"action_id":         actionID,
		"tool":              toolName,
		"expires_at":        action.ExpiresAt,
		"prompt":            prompt,
	}, nil
}

// approvePendingAction loads a staged action, executes it through the trusted
// in-process path (SkipConfirmation=true), and transitions the row to
// `applied`. The returned map mirrors the original tool's success envelope so
// callers can refresh the board without an extra fetch.
func (board *kanbanBoard) approvePendingAction(ctx context.Context, actionID string, decidedBy, decidedVia, note string) (pendingAction, map[string]any, bool, error) {
	store := globalPendingActionStore()
	if store == nil {
		return pendingAction{}, nil, false, fmt.Errorf("pending action store is not configured")
	}
	action, err := store.LoadPendingAction(ctx, board.tenantID, board.boardID, actionID)
	if err != nil {
		return pendingAction{}, nil, false, err
	}
	if action.Status != pendingActionStatusPending {
		return action, nil, false, ErrPendingActionTerminal
	}
	rawArgs, err := json.Marshal(action.Args)
	if err != nil {
		return action, nil, false, fmt.Errorf("encode args for approved action: %w", err)
	}
	// Execute via the trusted path that bypasses dry-run staging. The
	// confirmation gate still runs for risk-classified tools, but in
	// practice approve_pending_action is the human-supplied confirmation
	// so SkipConfirmation is set.
	result, changed, applyErr := board.ApplyToolCallWithMeta(action.Tool, string(rawArgs), toolCallMeta{
		Dispatcher:       "dry-run-approve",
		Actor:            decidedBy,
		SkipConfirmation: true,
	})
	disposition := string(pendingActionStatusApplied)
	if applyErr != nil {
		disposition = string(pendingActionStatusRejected)
	}
	decision := &pendingActionDecision{
		DecidedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		DecidedBy:   decidedBy,
		DecidedVia:  decidedVia,
		Disposition: disposition,
		Note:        note,
	}
	newStatus := pendingActionStatusApplied
	if applyErr != nil {
		// keep the action pending? No — record as rejected with an error note.
		newStatus = pendingActionStatusRejected
	}
	if err := store.UpdatePendingActionStatus(ctx, board.tenantID, board.boardID, actionID, newStatus, decision, result); err != nil {
		return action, result, changed, fmt.Errorf("update pending action status: %w", err)
	}
	action.Status = newStatus
	action.Decision = decision
	action.Result = result
	broadcastKanbanEventForBoard(board.tenantID, board.boardID, "pending_action_resolved", action)
	return action, result, changed, applyErr
}

// rejectPendingAction transitions a staged action to `rejected` without
// executing it. The decision metadata is required so the audit trail names
// the operator who declined.
func (board *kanbanBoard) rejectPendingAction(ctx context.Context, actionID, decidedBy, decidedVia, note string) (pendingAction, error) {
	store := globalPendingActionStore()
	if store == nil {
		return pendingAction{}, fmt.Errorf("pending action store is not configured")
	}
	action, err := store.LoadPendingAction(ctx, board.tenantID, board.boardID, actionID)
	if err != nil {
		return pendingAction{}, err
	}
	if action.Status != pendingActionStatusPending {
		return action, ErrPendingActionTerminal
	}
	decision := &pendingActionDecision{
		DecidedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		DecidedBy:   decidedBy,
		DecidedVia:  decidedVia,
		Disposition: string(pendingActionStatusRejected),
		Note:        note,
	}
	if err := store.UpdatePendingActionStatus(ctx, board.tenantID, board.boardID, actionID, pendingActionStatusRejected, decision, nil); err != nil {
		return pendingAction{}, fmt.Errorf("update pending action status: %w", err)
	}
	action.Status = pendingActionStatusRejected
	action.Decision = decision
	broadcastKanbanEventForBoard(board.tenantID, board.boardID, "pending_action_resolved", action)
	return action, nil
}
