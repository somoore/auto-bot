// Package projection defines the Projection contract that external systems
// (Jira, Linear, GitHub Issues) implement to receive outbound writes from the
// canonical board and to reconcile inbound changes back into it.
package projection

import (
	"context"
	"time"

	"github.com/somoore/auto-bot/internal/board"
)

// Projection writes board state outbound to an external system and reconciles
// inbound changes back into the canonical board.
type Projection interface {
	Name() string
	Capabilities() Capabilities
	Project(ctx context.Context, delta BoardDelta) error
	Reconcile(ctx context.Context) ([]board.Card, error)
	ResolveConflict(ctx context.Context, conflict Conflict) (Resolution, error)
	Health(ctx context.Context) Health
}

// Capabilities declares the outbound + inbound operations a projection supports.
type Capabilities struct {
	SupportsCreate  bool `json:"supports_create"`
	SupportsUpdate  bool `json:"supports_update"`
	SupportsDelete  bool `json:"supports_delete"`
	SupportsWebhook bool `json:"supports_webhook"`
	BiDirectional   bool `json:"bi_directional"`
}

// BoardDelta is the unit of outbound projection.
type BoardDelta struct {
	TenantID string       `json:"tenant_id,omitempty"`
	BoardID  string       `json:"board_id,omitempty"`
	Changed  []board.Card `json:"changed,omitempty"`
	Deleted  []string     `json:"deleted,omitempty"`
}

// Conflict reports divergent state between the canonical board and a projection.
type Conflict struct {
	CardID string     `json:"card_id"`
	Local  board.Card `json:"local"`
	Remote board.Card `json:"remote"`
	Fields []string   `json:"fields,omitempty"`
}

// Resolution names the strategy a projection chose for a Conflict.
type Resolution struct {
	Strategy string      `json:"strategy"`
	Merged   *board.Card `json:"merged,omitempty"`
}

// Canonical resolution strategies returned by Projection.ResolveConflict.
const (
	ResolutionKeepLocal  = "keep-local"
	ResolutionKeepRemote = "keep-remote"
	ResolutionMerge      = "merge"
	ResolutionAskUser    = "ask-user"
)

// Health is the point-in-time readiness report for a projection.
type Health struct {
	OK        bool              `json:"ok"`
	Status    string            `json:"status"`
	Message   string            `json:"message,omitempty"`
	CheckedAt time.Time         `json:"checked_at,omitempty"`
	Details   map[string]string `json:"details,omitempty"`
}
