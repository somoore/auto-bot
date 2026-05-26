# ADR 0002: Canonical Board With External Projections

## Status

Proposed (Sprint 3 deliverable).

## Context

The kanban board domain was originally entangled with Jira-specific fields. As we add Linear, GitHub Issues, and other connectors (W5), every new system would otherwise grow the central `kanbanCard` struct and force every consumer to learn fields that do not apply to it. We also need conflict resolution semantics that are connector-agnostic.

## Decision

Treat the in-process kanban board as the canonical source of record. Each external system (Jira, Linear, GitHub Issues, …) becomes a `Projection` keyed by `card_id`, holding only that system's sidecar state.

The `Projection` interface lives in `internal/projection/`:

```go
type Projection interface {
    Name() string
    Project(ctx, BoardDelta) error        // outbound writes
    Reconcile(ctx) ([]BoardDelta, error)  // inbound pull
    ResolveConflict(ctx, Conflict) (Resolution, error)
}
```

Jira sync is rewritten as `JiraProjection` with the existing field mapping preserved. New projections register through `internal/extensions`. A connector contract test in `internal/core/contracttest/` validates any new projection against the same semantics (idempotency, conflict shape, replay).

## Consequences

Positive:

- New connectors do not bloat the canonical card shape.
- Conflict resolution UI is one component, not one per integration.
- Replay of yesterday's events against the new projection must yield identical Jira output — gives us a sharp regression test.
- Projections can be disabled per-tenant without touching board code.

Tradeoff:

- Migration from the existing `jira_conflicts.go` is one-shot and must preserve historical conflict records.
- Two systems writing the same field require a documented precedence rule per projection.
