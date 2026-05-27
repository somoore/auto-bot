// Package contracttest provides a small harness any Projection implementation
// can use to assert it satisfies the surface area Sprint 3 callers depend on.
package contracttest

import (
	"context"
	"strings"
	"testing"

	"github.com/somoore/auto-bot/internal/board"
	"github.com/somoore/auto-bot/internal/projection"
)

// Factory constructs a fresh projection.Projection for the harness.
type Factory func() projection.Projection

// RunProjectionContract exercises Name, Capabilities, Project, Reconcile,
// ResolveConflict, and Health against a fresh projection from factory.
func RunProjectionContract(t *testing.T, factory Factory) {
	t.Helper()
	if factory == nil {
		t.Fatal("contract harness requires a non-nil projection factory")
	}
	ctx := context.Background()

	t.Run("name_is_stable_and_lowercase", func(t *testing.T) {
		proj := factory()
		name := proj.Name()
		if strings.TrimSpace(name) == "" {
			t.Fatal("Projection.Name returned blank")
		}
		if name != strings.ToLower(name) {
			t.Fatalf("Projection.Name = %q, want lowercase machine name", name)
		}
	})
	t.Run("capabilities_callable", func(t *testing.T) {
		_ = factory().Capabilities()
	})
	t.Run("project_accepts_empty_delta", func(t *testing.T) {
		if err := factory().Project(ctx, projection.BoardDelta{}); err != nil {
			t.Fatalf("Projection.Project(empty) returned error: %v", err)
		}
	})
	t.Run("project_accepts_changed_and_deleted", func(t *testing.T) {
		delta := projection.BoardDelta{
			TenantID: "tenant-a", BoardID: "board-a",
			Changed: []board.Card{{ID: "PROJ-1", Title: "contract case", Status: board.StatusBacklog}},
			Deleted: []string{"PROJ-2"},
		}
		if err := factory().Project(ctx, delta); err != nil {
			t.Fatalf("Projection.Project(delta) returned error: %v", err)
		}
	})
	t.Run("reconcile_returns_card_slice", func(t *testing.T) {
		cards, err := factory().Reconcile(ctx)
		if err != nil {
			t.Fatalf("Projection.Reconcile returned error: %v", err)
		}
		for _, card := range cards {
			_ = card.ID
		}
	})
	t.Run("resolve_conflict_returns_known_strategy", func(t *testing.T) {
		conflict := projection.Conflict{
			CardID: "PROJ-1",
			Local:  board.Card{ID: "PROJ-1", Title: "local"},
			Remote: board.Card{ID: "PROJ-1", Title: "remote"},
			Fields: []string{"title"},
		}
		resolution, err := factory().ResolveConflict(ctx, conflict)
		if err != nil {
			t.Fatalf("Projection.ResolveConflict returned error: %v", err)
		}
		switch resolution.Strategy {
		case projection.ResolutionKeepLocal,
			projection.ResolutionKeepRemote,
			projection.ResolutionMerge,
			projection.ResolutionAskUser:
		case "":
			t.Fatal("Projection.ResolveConflict returned blank Strategy")
		default:
			t.Fatalf("Projection.ResolveConflict returned unknown Strategy %q", resolution.Strategy)
		}
		if resolution.Strategy == projection.ResolutionMerge && resolution.Merged == nil {
			t.Fatal("Projection.ResolveConflict strategy=merge requires a non-nil Merged card")
		}
	})
	t.Run("health_returns_status", func(t *testing.T) {
		health := factory().Health(ctx)
		if strings.TrimSpace(health.Status) == "" {
			t.Fatal("Projection.Health returned blank Status")
		}
	})
}
