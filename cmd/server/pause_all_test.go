package main

import (
	"context"
	"errors"
	"testing"

	"github.com/somoore/auto-bot/internal/agent"
)

// TestPauseAllBlocksStart ensures Start returns ErrAgentsPaused when the
// tenant has the kill switch enabled.
func TestPauseAllBlocksStart(t *testing.T) {
	settingsStore := newMemoryTenantSettingsStore()
	actionStore := newMemoryPendingActionStore()
	manager := newTenantSettingsManager(settingsStore)
	restore := withDryRunRuntime(manager, actionStore)
	defer restore()

	ctx := context.Background()
	if _, err := manager.Set(ctx, tenantSettings{TenantID: defaultTenantID, AgentsPaused: true}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	board := newKanbanBoard()
	orchestrator := &agentRunOrchestrator{board: board}

	_, err := orchestrator.Start(ctx, agent.RunRequest{
		TenantID:  defaultTenantID,
		BoardID:   board.boardID,
		CardID:    "card-123",
		Objective: "do something",
	})
	if !errors.Is(err, agent.ErrAgentsPaused) {
		t.Fatalf("expected ErrAgentsPaused, got %v", err)
	}
}

// TestPauseAllAllowsStartWhenOff ensures Start proceeds (returns a real
// non-pause error path) when the switch is off — confirming the gate is
// genuinely conditional rather than always-on.
func TestPauseAllAllowsStartWhenOff(t *testing.T) {
	settingsStore := newMemoryTenantSettingsStore()
	actionStore := newMemoryPendingActionStore()
	manager := newTenantSettingsManager(settingsStore)
	restore := withDryRunRuntime(manager, actionStore)
	defer restore()

	board := newKanbanBoard()
	orchestrator := &agentRunOrchestrator{board: board}

	_, err := orchestrator.Start(context.Background(), agent.RunRequest{
		TenantID:  defaultTenantID,
		BoardID:   board.boardID,
		CardID:    "card-123",
		Objective: "do something",
	})
	// When the switch is off the gate passes; downstream
	// assignTicketToAgent then attempts persistence. The returned error
	// must NOT be ErrAgentsPaused.
	if errors.Is(err, agent.ErrAgentsPaused) {
		t.Fatalf("Start returned ErrAgentsPaused when the switch was off")
	}
}
