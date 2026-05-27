// Package board owns the canonical domain types and state machine for the
// kanban board: cards, columns, statuses, actors, mutations, and the
// in-memory + persisted board snapshot.
//
// The board is the source of truth for all work tracked in auto-bot. External
// systems (Jira, Linear, GitHub Issues) are projections written through the
// internal/projection package — never the other way around.
//
// Sprint 0 status: skeleton package. Types and state are still in
// cmd/server during the extraction; this package will receive them in
// Sprint 0.2 and Sprint 1 (when the kanbanActor type lands).
package board
