# ADR 0003: MCP Server as Universal External Surface

## Status

Proposed (Sprint 2 deliverable).

## Context

External agents (Claude Code, Cursor, `claude-agent-sdk` scripts) need to read and mutate the board. Building a bespoke REST surface for each integration duplicates the voice-tool authorization, ledger, and ask-the-human plumbing that already exists in `internal/agent` and `internal/core/ledger`.

## Decision

Expose the kanban board as an MCP server through a new binary `cmd/mcpd` supporting both stdio and HTTP transports. The tool surface mirrors the voice-tool model:

- `board.list_cards(filter)`, `board.get_card(id)`
- `card.create`, `card.update`, `card.comment`
- `run.start`, `run.checkpoint`, `run.ask_human`, `run.complete`

All MCP tools route through the same `RunCoordinator` and `ActionLedger` used by voice tools — no parallel audit path. Per-tenant MCP tokens with scoped capabilities are issued and rotated through `/admin/mcp-tokens`.

## Consequences

Positive:

- One audit path for every agent mutation, regardless of transport.
- Any MCP-aware client (Claude Code, Cursor, IDE plugins) becomes a first-class board citizen with zero bespoke integration.
- `run.ask_human` blocks the agent through the same `RunQuestion` table the UI consumes — humans see one queue.
- Token rotation and scope reduction are admin-grade controls, not code changes.

Tradeoff:

- Stdio and HTTP transports double the auth surface; the HTTP path needs the same token-scoping rigor as the existing API.
- MCP clients that hold long-lived sessions need careful reconnection semantics for `run.ask_human`.
