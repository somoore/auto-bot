package intake

import (
	"sort"
	"sync"
	"time"
)

// Store is the in-memory index of recent intakes. The board store remains
// the durable source of truth for cards and comments produced by intake
// processing; intakes themselves only need to live long enough to fold
// into the next standup agenda (S4.1) or surface to the UI as
// kanbanBoardState.RecentIntakes (Sprint 3 hook in cmd/server).
//
// Implementations must be safe for concurrent use. The default in-memory
// implementation uses a single mutex and a per-(tenant,board) slice; the
// access pattern is write-rare/read-rare so a tighter shard is not
// justified yet.
type Store interface {
	// Put records an intake. The caller is expected to have already
	// normalized the intake; Put does not validate. Returns the stored
	// intake (which may differ from in only in trim/index housekeeping).
	Put(in Intake) Intake

	// Recent returns intakes for the given tenant and board with
	// SubmittedAt >= since, newest first. Returned slice is a copy so
	// the caller may mutate it freely.
	Recent(tenantID, boardID string, since time.Time) []Intake

	// All returns every stored intake regardless of window. Intended for
	// debug and test surfaces only; callers should prefer Recent.
	All(tenantID, boardID string) []Intake
}

// MemoryStore is the default in-memory Store. Zero value is ready to use.
type MemoryStore struct {
	mu      sync.Mutex
	intakes map[key][]Intake
	cap     int
}

type key struct {
	tenant string
	board  string
}

// NewMemoryStore constructs a MemoryStore with the given per-(tenant,
// board) capacity. When the cap is exceeded, oldest entries are evicted.
// cap <= 0 means unlimited.
func NewMemoryStore(perBoardCap int) *MemoryStore {
	return &MemoryStore{
		intakes: map[key][]Intake{},
		cap:     perBoardCap,
	}
}

// Put implements Store.
func (s *MemoryStore) Put(in Intake) Intake {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.intakes == nil {
		s.intakes = map[key][]Intake{}
	}
	k := key{tenant: in.TenantID, board: in.BoardID}
	bucket := append(s.intakes[k], in)
	if s.cap > 0 && len(bucket) > s.cap {
		// Drop the oldest entries; the bucket is append-ordered which is
		// usually but not guaranteed time-ordered (clock skew or batched
		// imports can reorder). Sorting before trimming costs O(n log n)
		// per insert but n is tiny in practice (per-board cap = 200).
		sort.SliceStable(bucket, func(i, j int) bool {
			return bucket[i].SubmittedAt.Before(bucket[j].SubmittedAt)
		})
		bucket = bucket[len(bucket)-s.cap:]
	}
	s.intakes[k] = bucket
	return in
}

// Recent implements Store.
func (s *MemoryStore) Recent(tenantID, boardID string, since time.Time) []Intake {
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket, ok := s.intakes[key{tenant: tenantID, board: boardID}]
	if !ok {
		return nil
	}
	out := make([]Intake, 0, len(bucket))
	for _, in := range bucket {
		if in.SubmittedAt.Before(since) {
			continue
		}
		out = append(out, in)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].SubmittedAt.After(out[j].SubmittedAt)
	})
	return out
}

// All implements Store.
func (s *MemoryStore) All(tenantID, boardID string) []Intake {
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket, ok := s.intakes[key{tenant: tenantID, board: boardID}]
	if !ok {
		return nil
	}
	out := make([]Intake, len(bucket))
	copy(out, bucket)
	return out
}
