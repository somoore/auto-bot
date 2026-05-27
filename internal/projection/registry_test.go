package projection_test

import (
	"context"
	"testing"

	"github.com/somoore/auto-bot/internal/board"
	"github.com/somoore/auto-bot/internal/projection"
	"github.com/somoore/auto-bot/internal/projection/contracttest"
)

func TestRegistryRejectsDuplicatesAndListsDeterministically(t *testing.T) {
	registry := projection.NewRegistry()
	if err := registry.Register(stubProjection{name: "jira"}); err != nil {
		t.Fatalf("register jira: %v", err)
	}
	if err := registry.Register(stubProjection{name: "linear"}); err != nil {
		t.Fatalf("register linear: %v", err)
	}
	if err := registry.Register(stubProjection{name: "jira"}); err == nil {
		t.Fatal("expected duplicate projection registration to fail")
	}
	if err := registry.Register(nil); err == nil {
		t.Fatal("expected nil projection registration to fail")
	}
	if err := registry.Register(stubProjection{name: "  "}); err == nil {
		t.Fatal("expected blank projection name to fail")
	}
	names := registry.Names()
	if len(names) != 2 || names[0] != "jira" || names[1] != "linear" {
		t.Fatalf("names = %#v, want sorted jira,linear", names)
	}
	if _, ok := registry.Get("JIRA"); !ok {
		t.Fatal("registry lookup should be case-insensitive")
	}
}

func TestStubProjectionSatisfiesContract(t *testing.T) {
	contracttest.RunProjectionContract(t, func() projection.Projection {
		return stubProjection{name: "stub"}
	})
}

type stubProjection struct{ name string }

func (stub stubProjection) Name() string                          { return stub.name }
func (stub stubProjection) Capabilities() projection.Capabilities { return projection.Capabilities{} }
func (stub stubProjection) Project(_ context.Context, _ projection.BoardDelta) error {
	return nil
}
func (stub stubProjection) Reconcile(_ context.Context) ([]board.Card, error) { return nil, nil }
func (stub stubProjection) ResolveConflict(_ context.Context, _ projection.Conflict) (projection.Resolution, error) {
	return projection.Resolution{Strategy: projection.ResolutionKeepRemote}, nil
}
func (stub stubProjection) Health(_ context.Context) projection.Health {
	return projection.Health{OK: true, Status: "stub"}
}
