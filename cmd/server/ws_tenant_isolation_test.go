package main

import (
	"sync"
	"testing"
)

// TestWSFanoutKeyedByTenantAndBoard is the SecArch-001 regression test.
// Before the fix, wsClients was keyed by boardID alone; the WebSocket
// fanout matched on boardID and ignored authCtx.TenantID, so two clients
// authenticated under different tenants but registered against the same
// boardID would see each other's broadcasts.
//
// The fix rekeys wsClients to (tenantID, boardID) and threads tenantID
// through registerWSClient and broadcastKanbanEventForBoard. This test
// pins the contract: a broadcast targeted at tenant-a/board-1 must
// select only the tenant-a client even when a tenant-b client is
// registered against the same boardID.
func TestWSFanoutKeyedByTenantAndBoard(t *testing.T) {
	// Snapshot and restore the global registry so this test does not leak
	// state into siblings.
	wsClientsLock.Lock()
	originalClients := make(map[*threadSafeWriter]wsClientKey, len(wsClients))
	for k, v := range wsClients {
		originalClients[k] = v
	}
	for k := range wsClients {
		delete(wsClients, k)
	}
	wsClientsLock.Unlock()
	t.Cleanup(func() {
		wsClientsLock.Lock()
		for k := range wsClients {
			delete(wsClients, k)
		}
		for k, v := range originalClients {
			wsClients[k] = v
		}
		wsClientsLock.Unlock()
	})

	tenantA := &threadSafeWriter{Mutex: sync.Mutex{}}
	tenantB := &threadSafeWriter{Mutex: sync.Mutex{}}

	if !registerWSClient(tenantA, "tenant-a", "board-1") {
		t.Fatal("registerWSClient(tenant-a) returned false")
	}
	if !registerWSClient(tenantB, "tenant-b", "board-1") {
		t.Fatal("registerWSClient(tenant-b) returned false")
	}

	// Broadcast targeted at tenant-a/board-1 must hit only tenant A.
	matched := wsClientsForTenantBoard("tenant-a", "board-1")
	if len(matched) != 1 {
		t.Fatalf("tenant-a fanout matched %d clients, want 1; matched=%v", len(matched), matched)
	}
	if matched[0] != tenantA {
		t.Fatalf("tenant-a fanout matched the wrong client: got %p, want %p (tenant A); tenant B = %p", matched[0], tenantA, tenantB)
	}

	// And the cross check: tenant-b must see only its own client.
	matched = wsClientsForTenantBoard("tenant-b", "board-1")
	if len(matched) != 1 {
		t.Fatalf("tenant-b fanout matched %d clients, want 1", len(matched))
	}
	if matched[0] != tenantB {
		t.Fatalf("tenant-b fanout matched the wrong client: got %p, want %p (tenant B); tenant A = %p", matched[0], tenantB, tenantA)
	}

	// A broadcast for a tenant that has no client must match nobody.
	matched = wsClientsForTenantBoard("tenant-c", "board-1")
	if len(matched) != 0 {
		t.Fatalf("tenant-c fanout matched %d clients, want 0", len(matched))
	}
}

// TestWSFanoutCrossBoardWithinTenantIsolated complements the SecArch-001
// test by pinning the orthogonal axis: two boards under the same tenant
// must not see each other's broadcasts either.
func TestWSFanoutCrossBoardWithinTenantIsolated(t *testing.T) {
	wsClientsLock.Lock()
	originalClients := make(map[*threadSafeWriter]wsClientKey, len(wsClients))
	for k, v := range wsClients {
		originalClients[k] = v
	}
	for k := range wsClients {
		delete(wsClients, k)
	}
	wsClientsLock.Unlock()
	t.Cleanup(func() {
		wsClientsLock.Lock()
		for k := range wsClients {
			delete(wsClients, k)
		}
		for k, v := range originalClients {
			wsClients[k] = v
		}
		wsClientsLock.Unlock()
	})

	boardOne := &threadSafeWriter{Mutex: sync.Mutex{}}
	boardTwo := &threadSafeWriter{Mutex: sync.Mutex{}}

	registerWSClient(boardOne, "tenant-a", "board-1")
	registerWSClient(boardTwo, "tenant-a", "board-2")

	matched := wsClientsForTenantBoard("tenant-a", "board-1")
	if len(matched) != 1 || matched[0] != boardOne {
		t.Fatalf("board-1 fanout = %v, want only board-1 client", matched)
	}
	matched = wsClientsForTenantBoard("tenant-a", "board-2")
	if len(matched) != 1 || matched[0] != boardTwo {
		t.Fatalf("board-2 fanout = %v, want only board-2 client", matched)
	}
}
