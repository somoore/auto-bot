package mcp

import (
	"sync"
	"time"
)

// MemoryReplayTracker is the default ReplayTracker — an in-memory map
// keyed by JTI, swept periodically of expired entries. For self-hosted
// deployments this is sufficient; the in-memory tracker does NOT
// survive process restart, so a JTI issued before a restart can be
// replayed within its exp window after the restart. With the default
// 15-minute TTL the window is small and acceptable; promoting this to
// a SQLite-backed tracker is a future hardening step documented in
// SECURITY.md.
type MemoryReplayTracker struct {
	mu        sync.Mutex
	seen      map[string]time.Time
	clock     func() time.Time
	lastSweep time.Time
}

// NewMemoryReplayTracker returns an empty tracker.
func NewMemoryReplayTracker() *MemoryReplayTracker {
	return &MemoryReplayTracker{
		seen:  map[string]time.Time{},
		clock: time.Now,
	}
}

// SeenOrRemember returns true if the JTI is already recorded; otherwise
// stores it with the supplied expiration and returns false. Sweeps
// expired entries opportunistically every minute so the map cannot
// grow without bound.
func (t *MemoryReplayTracker) SeenOrRemember(jti string, exp time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.clock().UTC()
	if now.Sub(t.lastSweep) > time.Minute {
		for k, v := range t.seen {
			if now.After(v) {
				delete(t.seen, k)
			}
		}
		t.lastSweep = now
	}
	if existing, ok := t.seen[jti]; ok {
		// Allow re-insert after expiration even if the caller hasn't
		// reaped — this matches the "the JTI has already been seen"
		// contract: a JTI whose token already expired is not a replay
		// concern because Verify will reject the exp before we get
		// here, but we keep the entry until sweep regardless.
		if now.After(existing) {
			t.seen[jti] = exp
			return false
		}
		return true
	}
	t.seen[jti] = exp
	return false
}
