# MCP Tool Reference (Sprint 2.0)

This document describes the Model Context Protocol (MCP) tools exposed
by the auto-bot MCP server. The MCP server is the universal external
surface for the canonical board — see
`docs/adrs/0003-mcp-server-as-universal-external-surface.md` for the
architectural rationale, and
`docs/adrs/0002-canonical-board-with-external-projections.md` for how
these tools relate to the projection layer.

The tool registry, JSON schemas, and handlers are defined in
`internal/mcp/tools.go:130` (`BuildTools`). The JSON-RPC envelope and
transports (stdio + HTTP) are defined in the MCP server core that lives
alongside `tools.go` (search for `ServeStdio`, `HTTPHandler`, and
`HandleRequest`).

## Authentication

The MCP server runs two transports with **different trust models**:

- **stdio** (default for editor clients such as Claude Code / Cursor)
  — no authentication. The MCP client launches the MCP server as a
  child process; the process tree itself is the perimeter.

- **HTTP** (used by automation, CI, remote agents) — Bearer-token
  auth. When the server is started with a non-empty `AuthToken`, every
  POST must carry an `Authorization: Bearer <token>` header. The check
  is constant-time (`checkBearer`).

The bearer token in S2.0 is a single shared secret; S2.1 will replace
this with scoped per-agent tokens minted at agent-profile registration
time. The call site stays the same — only the verification widens.

Tenant scoping: each tool call is implicitly scoped by the
`(tenant_id, board_id)` configured in `ToolDeps`
(`internal/mcp/tools.go:114`). Cross-tenant access is not possible
through this surface. ADR 0004 covers the multi-tenant model.

## JSON-RPC envelope

Every request is JSON-RPC 2.0. Tool invocation goes through
`tools/call`:

```json
{
  "jsonrpc": "2.0",
  "id": "1",
  "method": "tools/call",
  "params": {
    "name": "board.list_cards",
    "arguments": {}
  }
}
```

Successful responses wrap the tool output in a `ToolCallResult`
envelope (one text content block with the JSON-encoded result, plus a
`data` mirror for programmatic callers):

```json
{
  "jsonrpc": "2.0",
  "id": "1",
  "result": {
    "content": [
      { "type": "text", "text": "<JSON-encoded tool output>" }
    ],
    "data": { "<structured-mirror-of-text>": "..." }
  }
}
```

Errors return JSON-RPC error envelopes with codes `-32600`
(InvalidRequest), `-32601` (MethodNotFound), `-32602` (InvalidParams),
`-32603` (Internal), or `-32700` (Parse).

The schemas below show the tool-specific `arguments` and the structured
result inside `data` / the decoded `text` block.

## Tool index

| Tool                 | Sprint | Risk    | Purpose                                                |
| -------------------- | ------ | ------- | ------------------------------------------------------ |
| `board.list_cards`   | 2.0    | Low     | Read filtered card list.                               |
| `board.get_card`     | 2.0    | Low     | Read one card with thread + active-run summary.        |
| `card.create`        | 2.0    | Medium  | Create a card.                                         |
| `card.update`        | 2.0    | Medium  | Patch a card.                                          |
| `card.comment`       | 2.0    | Low     | Append a comment to a card thread.                     |
| `run.start`          | 2.1    | High    | (Coming next) Kick off an agent Run on a card.         |
| `run.checkpoint`     | 2.1    | Low     | (Coming next) Append a checkpoint to a Run timeline.   |
| `run.ask_human`      | 2.1    | Medium  | (Coming next) Pause a Run on a `RunQuestion`.          |
| `run.complete`       | 2.1    | Medium  | (Coming next) Terminate a Run (success or failure).    |

Risk levels mirror the voice tool gates (`riskForTool` at
`cmd/server/meeting_intelligence.go:107`): **Low** runs without
prompting, **Medium** routes through a host confirmation, **High**
requires explicit host approval and is rate-limited. Reads are always
Low; mutations are classified by impact and reversibility.

---

## `board.list_cards`

**Risk:** Low (read-only).

**Purpose.** List cards on the active board. Optional filters narrow by
status, assignee ID, or agent-owned cards only. Returns a slim
`CardSummary` view (full details flow through `board.get_card`). The
adapter scans cards in stable ID order. Source:
`internal/mcp/tools.go:142` (`buildListCardsTool`).

### Input schema

```json
{
  "type": "object",
  "properties": {
    "filter": {
      "type": "object",
      "properties": {
        "status":      { "type": "string" },
        "assignee_id": { "type": "string" },
        "agent_only":  { "type": "boolean" }
      }
    }
  }
}
```

All fields are optional. Empty `filter` returns every card.

### Output schema

```json
{
  "type": "object",
  "required": ["cards"],
  "properties": {
    "cards": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["id", "title", "status"],
        "properties": {
          "id":       { "type": "string" },
          "title":    { "type": "string" },
          "status":   { "type": "string", "enum": ["Backlog", "In Progress", "Blocked", "Done"] },
          "assignee": { "$ref": "internal/board/types.go#Actor" },
          "tags":     { "type": "array", "items": { "type": "string" } },
          "run_id":   { "type": "string", "description": "Set when this card has an open RunQuestion." }
        }
      }
    }
  }
}
```

`Actor` shape: `internal/board/types.go` (`Actor`).

### Example

Request:

```json
{
  "jsonrpc": "2.0", "id": "1",
  "method": "tools/call",
  "params": {
    "name": "board.list_cards",
    "arguments": { "filter": { "agent_only": true } }
  }
}
```

Response `data`:

```json
{
  "cards": [
    {
      "id": "card-001",
      "title": "Finish RTP HEVC Packetizer",
      "status": "In Progress",
      "assignee": { "kind": "agent", "id": "swe-1", "agentProfile": "swe" },
      "tags": ["webrtc", "rtp", "hevc"],
      "run_id": "run-01H..."
    }
  ]
}
```

### Errors

| Condition         | Surface                                  |
| ----------------- | ---------------------------------------- |
| Malformed JSON    | `-32602 invalid params: <decoder error>` |
| Adapter failure   | `-32603 <wrapped error>`                 |

---

## `board.get_card`

**Risk:** Low (read-only).

**Purpose.** Fetch one card by ID, including its recent comment thread
and any active agent run summary. The active-run lookup scans open
`RunQuestion` records for this card and loads the matching `Run` —
best-effort; returns `active_run: null` when no run is in flight.
Source: `internal/mcp/tools.go:202` (`buildGetCardTool`).

### Input schema

```json
{
  "type": "object",
  "required": ["card_id"],
  "properties": {
    "card_id": { "type": "string" }
  }
}
```

### Output schema

```json
{
  "type": "object",
  "required": ["card"],
  "properties": {
    "card":   { "$ref": "internal/board/types.go#Card" },
    "thread": {
      "type": "array",
      "items": { "$ref": "internal/board/types.go#Comment" }
    },
    "active_run": {
      "type": "object",
      "properties": {
        "run_id":        { "type": "string" },
        "status":        { "type": "string" },
        "agent_profile": { "type": "string" },
        "objective":     { "type": "string" },
        "current_step":  { "type": "string" },
        "waiting_on":    { "$ref": "internal/agent/types.go:159#RunQuestionRef" },
        "updated_at":    { "type": "string" }
      }
    }
  }
}
```

### Example

Request:

```json
{
  "jsonrpc": "2.0", "id": "2",
  "method": "tools/call",
  "params": { "name": "board.get_card", "arguments": { "card_id": "card-001" } }
}
```

Response `data`:

```json
{
  "card": {
    "id": "card-001",
    "title": "Finish RTP HEVC Packetizer",
    "status": "In Progress",
    "notes": "Complete HEVC payload fragmentation...",
    "tags": ["webrtc", "rtp", "hevc"],
    "assignee": { "kind": "agent", "id": "swe-1", "agentProfile": "swe" }
  },
  "thread": [
    { "id": "cmt-1", "body": "Started on the fragmentation path.", "author": "swe-1", "createdAt": "2026-05-26T14:02:00Z" }
  ],
  "active_run": {
    "run_id": "run-01HXYZ...",
    "status": "reviewing",
    "agent_profile": "swe",
    "objective": "Complete HEVC packetizer",
    "current_step": "running pre-merge review",
    "updated_at": "2026-05-26T14:05:00Z"
  }
}
```

### Errors

| Condition          | Surface                                     |
| ------------------ | ------------------------------------------- |
| Missing `card_id`  | `-32602 card_id is required`                |
| No matching card   | `-32603 mcp: card not found` (`ErrCardNotFound` in `internal/mcp/tools.go`) |

---

## `card.create`

**Risk:** Medium — creates persistent board state.

**Purpose.** Create a new card on the active board. Routes through the
same mutation path as voice tools. In S2.0 the `ActionLedger` + risk
gates are not yet wired through MCP (the cross-process state-sharing
question lands in S2.1); the tool itself is therefore safe to expose to
trusted automation only — gate by transport (stdio process tree, or
HTTP bearer scope). See `internal/mcp/tools.go:282`
(`buildCreateCardTool`).

### Input schema

```json
{
  "type": "object",
  "required": ["title"],
  "properties": {
    "title":       { "type": "string" },
    "description": { "type": "string" },
    "status":      { "type": "string", "enum": ["Backlog", "In Progress", "Blocked", "Done"] },
    "assignee": {
      "type": "object",
      "properties": {
        "kind":         { "type": "string", "enum": ["human", "agent"] },
        "id":           { "type": "string" },
        "displayName":  { "type": "string" },
        "agentProfile": { "type": "string" }
      }
    },
    "tags": { "type": "array", "items": { "type": "string" } }
  }
}
```

`title` is the only required field. Omitted `status` defaults to
`Backlog` (see `InMemoryBoardAdapter.CreateCard` in
`internal/mcp/tools.go`).

### Output schema

```json
{
  "type": "object",
  "required": ["card_id", "card"],
  "properties": {
    "card_id": { "type": "string" },
    "card":    { "$ref": "internal/board/types.go#Card" }
  }
}
```

### Example

Request:

```json
{
  "jsonrpc": "2.0", "id": "3",
  "method": "tools/call",
  "params": {
    "name": "card.create",
    "arguments": {
      "title": "Add MCP smoke test",
      "description": "End-to-end smoke test for the MCP transport.",
      "status": "Backlog",
      "tags": ["sprint-2", "mcp"]
    }
  }
}
```

Response `data`:

```json
{
  "card_id": "mcp-0000000001",
  "card": {
    "id": "mcp-0000000001",
    "title": "Add MCP smoke test",
    "notes": "End-to-end smoke test for the MCP transport.",
    "status": "Backlog",
    "tags": ["sprint-2", "mcp"]
  }
}
```

### Errors

| Condition         | Surface                       |
| ----------------- | ----------------------------- |
| Missing `title`   | `-32602 title is required`    |
| Adapter failure   | `-32603 <wrapped error>`      |

---

## `card.update`

**Risk:** Medium — mutates persistent board state.

**Purpose.** Patch one card. Pointer-typed fields in the Go shape
(`CardPatch` at `internal/mcp/tools.go:62`) distinguish "no change"
from "set to empty"; the JSON surface mirrors this by treating absent
keys as unchanged. Status changes use the canonical status vocabulary
(`Backlog` / `In Progress` / `Blocked` / `Done`). `tags` follows
explicit set-tracking — the field is updated only when the key is
present in `patch`. Source: `internal/mcp/tools.go:324`
(`buildUpdateCardTool`).

### Input schema

```json
{
  "type": "object",
  "required": ["card_id", "patch"],
  "properties": {
    "card_id": { "type": "string" },
    "patch": {
      "type": "object",
      "properties": {
        "title":    { "type": "string" },
        "status":   { "type": "string" },
        "notes":    { "type": "string" },
        "assignee": { "$ref": "internal/board/types.go#Actor" },
        "tags":     { "type": "array", "items": { "type": "string" } }
      }
    }
  }
}
```

### Output schema

```json
{
  "type": "object",
  "required": ["card"],
  "properties": {
    "card": { "$ref": "internal/board/types.go#Card" }
  }
}
```

### Example

Request:

```json
{
  "jsonrpc": "2.0", "id": "4",
  "method": "tools/call",
  "params": {
    "name": "card.update",
    "arguments": {
      "card_id": "card-100",
      "patch": { "title": "Renamed", "status": "In Progress" }
    }
  }
}
```

Response `data`:

```json
{
  "card": {
    "id": "card-100",
    "title": "Renamed",
    "status": "In Progress",
    "tags": ["sprint-2"]
  }
}
```

### Errors

| Condition           | Surface                                        |
| ------------------- | ---------------------------------------------- |
| Missing `card_id`   | `-32602 card_id is required`                   |
| Malformed `patch`   | `-32602 invalid patch: <decoder error>`        |
| No matching card    | `-32603 mcp: card not found`                   |

---

## `card.comment`

**Risk:** Low — appends to a card thread; reversible.

**Purpose.** Append a comment to a card's thread. `as_actor` overrides
the default MCP actor for the duration of this call (`DefaultActor` in
`ToolDeps` at `internal/mcp/tools.go:114`). In S2.1 this will be scoped
to per-token identities rather than free-text. Source:
`internal/mcp/tools.go:383` (`buildCommentTool`).

### Input schema

```json
{
  "type": "object",
  "required": ["card_id", "body"],
  "properties": {
    "card_id":  { "type": "string" },
    "body":     { "type": "string" },
    "as_actor": { "type": "string" }
  }
}
```

### Output schema

```json
{
  "type": "object",
  "required": ["card_id", "comment"],
  "properties": {
    "card_id": { "type": "string" },
    "comment": { "$ref": "internal/board/types.go#Comment" }
  }
}
```

### Example

Request:

```json
{
  "jsonrpc": "2.0", "id": "5",
  "method": "tools/call",
  "params": {
    "name": "card.comment",
    "arguments": {
      "card_id": "card-100",
      "body": "Looks good",
      "as_actor": "swe-3"
    }
  }
}
```

Response `data`:

```json
{
  "card_id": "card-100",
  "comment": {
    "id": "cmt-1716738720000000000",
    "body": "Looks good",
    "author": "swe-3",
    "createdAt": "2026-05-26T14:32:00.000000000Z"
  }
}
```

### Errors

| Condition          | Surface                       |
| ------------------ | ----------------------------- |
| Missing `card_id`  | `-32602 card_id is required`  |
| Missing `body`     | `-32602 body is required`     |
| No matching card   | `-32603 mcp: card not found`  |

---

## Coming next (Sprint 2.1)

The four Run-lifecycle tools below are planned but not yet shipped.
Their shapes are illustrative; the canonical schemas will land
alongside the implementations and will live next to the S2.0 tools in
`internal/mcp/tools.go`.

### `run.start`

**Risk:** High — kicks off an autonomous agent loop with budget.

```json
{
  "type": "object",
  "required": ["card_id", "agent_profile", "objective"],
  "properties": {
    "card_id":           { "type": "string" },
    "agent_profile":     { "type": "string" },
    "objective":         { "type": "string" },
    "cost_budget_cents": { "type": "integer" }
  }
}
```

Returns `{ "run_id": "...", "run": <RunView> }`. `RunView` shape:
`internal/agent/types.go:204`.

### `run.checkpoint`

**Risk:** Low — append-only timeline entry.

```json
{
  "type": "object",
  "required": ["run_id", "status", "message"],
  "properties": {
    "run_id":  { "type": "string" },
    "status":  { "type": "string" },
    "step":    { "type": "string" },
    "message": { "type": "string" }
  }
}
```

Returns `{ "checkpoint": <Checkpoint> }`. `Checkpoint` shape:
`internal/agent/types.go:195`.

### `run.ask_human`

**Risk:** Medium — pauses the Run and surfaces a `RunQuestion`.

```json
{
  "type": "object",
  "required": ["run_id", "prompt"],
  "properties": {
    "run_id":      { "type": "string" },
    "prompt":      { "type": "string" },
    "reasoning":   { "type": "string" },
    "suggestions": { "type": "array", "items": { "type": "string" } },
    "ttl_seconds": { "type": "integer", "description": "Default 14400 (4h)." }
  }
}
```

Returns `{ "question": <RunQuestion>, "run": <RunView> }`. `RunQuestion`
shape: `internal/agent/types.go:129`.

### `run.complete`

**Risk:** Medium — terminates the Run; may post Jira / GitHub artifacts.

```json
{
  "type": "object",
  "required": ["run_id", "status"],
  "properties": {
    "run_id":  { "type": "string" },
    "status":  { "type": "string", "enum": ["completed", "failed", "cancelled"] },
    "summary": { "type": "string" },
    "error":   { "type": "string" }
  }
}
```

Returns `{ "run": <RunView> }`.
