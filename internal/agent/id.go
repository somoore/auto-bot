package agent

import (
	cryptorand "crypto/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// idMu and idEntropy back NewQuestionID / NewRunID. ULID generation is
// monotonic per-process so two IDs minted in the same millisecond still
// sort deterministically; the underlying entropy comes from crypto/rand.
var (
	idMu      sync.Mutex
	idEntropy = ulid.Monotonic(cryptorand.Reader, 0)
)

// NewQuestionID returns a fresh Crockford-base32 ULID for a RunQuestion.
// ULIDs sort lexicographically by mint time, which keeps "open questions
// asked in order" queries cheap and matches the RunQuestion table's
// asked_at ordering. This is the implementation that retires the
// sprint-1.2 TODO in RunQuestion.ID.
func NewQuestionID() string {
	idMu.Lock()
	defer idMu.Unlock()
	return ulid.MustNew(ulid.Timestamp(time.Now().UTC()), idEntropy).String()
}

// NewRunID returns a fresh Crockford-base32 ULID with the "run_" prefix.
// The prefix keeps Run IDs visually distinguishable from RunQuestion IDs
// in logs and Jira comments while preserving lexical sortability inside
// the ULID portion.
func NewRunID() string {
	idMu.Lock()
	defer idMu.Unlock()
	return "run_" + ulid.MustNew(ulid.Timestamp(time.Now().UTC()), idEntropy).String()
}
