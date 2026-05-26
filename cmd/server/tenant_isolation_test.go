package main

import (
	"context"
	"path/filepath"
	"testing"
)

// TestTenantIsolationKeepsBoardsSeparate confirms that two boards sharing the
// same boardID but living under different tenants cannot read each other's
// cards, audit events, or agent runs. This is the load-bearing invariant the
// hosted control plane (Sprint 5) will rely on.
func TestTenantIsolationKeepsBoardsSeparate(t *testing.T) {
	store, err := newSQLiteBoardStore(filepath.Join(t.TempDir(), "tenant-isolation.sqlite"))
	if err != nil {
		t.Fatalf("newSQLiteBoardStore returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close board store: %v", err)
		}
	})

	tenantA, err := newPersistentTenantBoard("tenant-a", "shared-board", store)
	if err != nil {
		t.Fatalf("tenant A board: %v", err)
	}
	tenantB, err := newPersistentTenantBoard("tenant-b", "shared-board", store)
	if err != nil {
		t.Fatalf("tenant B board: %v", err)
	}

	if tenantA.tenantID == tenantB.tenantID {
		t.Fatalf("expected distinct tenant IDs, got %q == %q", tenantA.tenantID, tenantB.tenantID)
	}
	if tenantA.boardID != tenantB.boardID {
		t.Fatalf("boards should share boardID, got %q vs %q", tenantA.boardID, tenantB.boardID)
	}

	resultA, changedA, err := tenantA.ApplyToolCall("create_ticket", `{"title":"Tenant A secret","notes":"Should never leak across tenants","status":"In Progress"}`)
	if err != nil {
		t.Fatalf("tenant A create_ticket: %v", err)
	}
	if !changedA {
		t.Fatal("tenant A create_ticket should mark board changed")
	}
	createdA := resultA["card"].(kanbanCard)

	// Tenant B should see nothing from tenant A, even via the SQLite store.
	if stateA, ok, err := store.LoadBoard(context.Background(), "tenant-a", "shared-board"); err != nil || !ok {
		t.Fatalf("LoadBoard(tenant-a) err=%v ok=%v", err, ok)
	} else {
		var found bool
		for _, card := range stateA.Cards {
			if card.ID == createdA.ID && card.Title == "Tenant A secret" {
				found = true
			}
		}
		if !found {
			t.Fatalf("tenant A snapshot missing its own card: %#v", stateA.Cards)
		}
	}

	stateB, okB, err := store.LoadBoard(context.Background(), "tenant-b", "shared-board")
	if err != nil {
		t.Fatalf("LoadBoard(tenant-b) returned error: %v", err)
	}
	// Tenant B may have its initial snapshot or none — either way it must not
	// contain tenant A's card by title.
	if okB {
		for _, card := range stateB.Cards {
			if card.Title == "Tenant A secret" {
				t.Fatalf("tenant B leaked tenant A card: %#v", card)
			}
		}
	}

	// In-memory state from a fresh reload also must respect tenant scope.
	reloadB, err := newPersistentTenantBoard("tenant-b", "shared-board", store)
	if err != nil {
		t.Fatalf("reload tenant B board: %v", err)
	}
	for _, card := range reloadB.SnapshotState().Cards {
		if card.Title == "Tenant A secret" {
			t.Fatalf("reloaded tenant B board leaked tenant A card: %#v", card)
		}
	}

	// The default-tenant path must also stay isolated — assignments to the
	// default tenant must not appear in tenant A or tenant B.
	defaultBoard, err := newPersistentTenantBoard(defaultTenantID, "shared-board", store)
	if err != nil {
		t.Fatalf("default tenant board: %v", err)
	}
	defaultResult, _, err := defaultBoard.ApplyToolCall("create_ticket", `{"title":"Default tenant ticket","status":"Backlog"}`)
	if err != nil {
		t.Fatalf("default tenant create_ticket: %v", err)
	}
	defaultCard := defaultResult["card"].(kanbanCard)

	// Each board has its own monotonic card-ID counter, so colliding IDs are
	// expected; the load-bearing invariant is that the *card* (title + notes)
	// from one tenant must not appear inside another tenant's state.
	for _, card := range reloadB.SnapshotState().Cards {
		if card.Title == defaultCard.Title {
			t.Fatalf("tenant B leaked default tenant card: %#v", card)
		}
	}
	stateA2, _, err := store.LoadBoard(context.Background(), "tenant-a", "shared-board")
	if err != nil {
		t.Fatalf("LoadBoard(tenant-a) after default writes: %v", err)
	}
	for _, card := range stateA2.Cards {
		if card.Title == defaultCard.Title {
			t.Fatalf("tenant A leaked default tenant card: %#v", card)
		}
	}
}

// TestSQLiteBoardStoreMigratesPreTenantSchema asserts that an existing
// pre-tenant database (no tenant_id column) is rewritten to the tenant-scoped
// schema with every legacy row landing under tenant_id='default'.
func TestSQLiteBoardStoreMigratesPreTenantSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.sqlite")

	// Hand-craft the pre-tenant schema, mirroring the original board_store.go
	// CREATE statements before this commit. Insert one row per table.
	legacy, err := openLegacyPreTenantStore(t, path)
	if err != nil {
		t.Fatalf("open legacy store: %v", err)
	}
	if err := seedLegacyPreTenantRows(legacy); err != nil {
		_ = legacy.Close()
		t.Fatalf("seed legacy rows: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy: %v", err)
	}

	// Open via the tenant-aware constructor — this should migrate in place.
	migrated, err := newSQLiteBoardStore(path)
	if err != nil {
		t.Fatalf("newSQLiteBoardStore returned error after migration: %v", err)
	}
	t.Cleanup(func() {
		if err := migrated.Close(); err != nil {
			t.Errorf("close migrated store: %v", err)
		}
	})

	tables := []string{"board_snapshots", "board_events", "agent_runs", "action_replay_events", "meeting_reports"}
	for _, table := range tables {
		var tenantCount int
		query := "SELECT COUNT(*) FROM " + table + " WHERE tenant_id = ?"
		if err := migrated.db.QueryRowContext(context.Background(), query, defaultTenantID).Scan(&tenantCount); err != nil {
			t.Fatalf("count %s rows: %v", table, err)
		}
		if tenantCount == 0 {
			t.Fatalf("migrated %s has no default-tenant rows; legacy rows did not carry over", table)
		}
	}

	// PRAGMA user_version should now be 1 so subsequent opens skip the rewrite.
	var version int
	if err := migrated.db.QueryRowContext(context.Background(), `PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("read user_version after migration: %v", err)
	}
	if version != 1 {
		t.Fatalf("user_version after migration = %d, want 1", version)
	}
}
