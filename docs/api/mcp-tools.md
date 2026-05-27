# MCP Tool Reference (Sprint 2.0 + S2.1 wire-through)

This document describes the Model Context Protocol (MCP) tools exposed
by the auto-bot MCP server. The MCP server is the universal external
surface for the canonical board — see
`docs/adrs/0003-mcp-server-as-universal-external-surface.md` for the
architectural rationale, and
`docs/adrs/0002-canonical-board-with-external-projections.md` for how
these tools relate to the projection layer.

The tool registry, JSON schemas, and handlers are defined in
`internal/mcp/tools.go:132` (`BuildTools`). The JSON-RPC envelope and
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
(`internal/mcp/tools.go:116`). Cross-tenant access is not possible
through this surface. ADR 0004 covers the multi-tenant model.

## How dispatch works (S2.1 wire-through)

As of Sprint 2.1, the five active tools are no longer terminal: a
mutation tool call on the MCP side now reaches cmd/server's canonical
`ApplyToolCall` path over HTTP, so MCP-driven changes flow through the
same `ActionLedger`, risk classification, and confirmation gates as
voice and UI callers. Reads take a separate, lighter HTTP path.

The chain for a mutating tool (`card.create` / `card.update` /
`card.comment`):

```
MCP client (Claude, Cursor, automation)
    │   JSON-RPC tools/call over stdio or HTTP
    ▼
cmd/mcpd  →  internal/mcp/tools.go  (handler closure)
    │            (`buildCreateCardTool` / `buildUpdateCardTool` /
    │             `buildCommentTool` at :278 / :320 / :374)
    ▼
internal/mcp.HTTPBoardClient.post  (internal/mcp/tools.go:483)
    │   POST <BoardURL>/internal/tools/dispatch
    │   { tool, args, dispatcher, tenant_id, board_id }
    │   Authorization: Bearer <APP_API_TOKEN>
    ▼
cmd/server.internalToolsDispatchHandler  (cmd/server/internal_dispatch.go:36)
    │   tool switch at :70 — translates MCP names to cmd/server
    │   internal names and fans out across multiple legs as needed
    │   (e.g. card.update → update_ticket + move_ticket + assign_ticket +
    │   add_tags / remove_tags). Each leg runs through
    │   sharedBoard.ApplyToolCallWithMeta with the caller-supplied
    │   dispatcher label.
    ▼
cmd/server.ApplyToolCallWithMeta
    │   ActionLedger + risk gates + tenant dry-run check
    ▼
  Apply OR stage as PendingAction (when tenant has dry_run_enabled)
```

Reads (`board.list_cards`, `board.get_card`) do **not** go through
`/internal/tools/dispatch`. They use the read-side endpoint instead:

```
HTTPBoardClient.get (internal/mcp/tools.go:545)
    GET <BoardURL>/internal/board/cards         → { cards: [...] }
    GET <BoardURL>/internal/board/cards/{id}    → { card:  ... }
```

The dispatch endpoint switch at `cmd/server/internal_dispatch.go:70`
only recognizes `card.create`, `card.update`, and `card.comment`; any
other tool name returns 400 with body
`{"error":"unknown tool \"<name>\""}`. The read-side handler is at
`cmd/server/internal_dispatch.go:311`
(`internalBoardCardsHandler`).

### Dry-run staging envelope

When the tenant has `dry_run_enabled=true`
(`cmd/server/tenant_settings.go:14`), any mutating tool call routed
through `ApplyToolCallWithMeta` is queued as a `PendingAction` rather
than applied. The caller sees this envelope (source:
`cmd/server/dry_run.go:148`–`:157`):

```json
{
  "ok": false,
  "dry_run": true,
  "requires_approval": true,
  "action_id": "pa_<24 hex chars>",
  "tool": "<cmd/server internal tool name>",
  "expires_at": "<RFC3339Nano deadline; 24h default>",
  "prompt": "I would have run <tool> but dry-run mode is enabled..."
}
```

The MCP tool layer surfaces this envelope to the agent — it is not
silently swallowed. The agent's next step is to wait for the human to
approve via `POST /tenant/pending_actions/{action_id}/approve` (see
`docs/api/openapi.yaml`).

### Wire envelope reference

The exact JSON shapes for `/internal/tools/dispatch` and
`/internal/board/cards[/{id}]` live in
`docs/api/openapi.yaml` under the `DispatchCardResult`,
`DispatchCommentResult`, and `RequiresApprovalEnvelope` schemas.

### Auth pass-through

`HTTPBoardClient` injects the configured bearer token
(`internal/mcp/tools.go:511`) and cmd/server's
`internalToolsDispatchHandler` runs `authorizeBaseRequest` first thing
(`cmd/server/internal_dispatch.go:42`). Browser session cookies are
also accepted, which is what lets the React drawer dry-run the same
endpoints as the MCP path. The token is shared with the rest of
cmd/server (`APP_API_TOKEN`); per-tool scoping arrives in a later
sprint.

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

| Tool                   | Sprint | Risk    | Status   | Purpose                                                |
| ---------------------- | ------ | ------- | -------- | ------------------------------------------------------ |
| `board.list_cards`     | 2.0    | Low     | Wired    | Read filtered card list.                               |
| `board.get_card`       | 2.0    | Low     | Wired    | Read one card with thread + active-run summary.        |
| `card.create`          | 2.0    | Medium  | Wired    | Create a card. Routes via `/internal/tools/dispatch`.  |
| `card.update`          | 2.0    | Medium  | Wired    | Patch a card. Routes via `/internal/tools/dispatch`.   |
| `card.comment`         | 2.0    | Low     | Wired    | Append a comment. Routes via `/internal/tools/dispatch`. |
| `run.start`            | 2.1    | High    | Stub     | Kick off an agent Run on a card. 400 today.            |
| `run.checkpoint`       | 2.1    | Low     | Stub     | Append a checkpoint to a Run timeline. 400 today.      |
| `run.ask_human`        | 2.1    | Medium  | Stub     | Pause a Run on a `RunQuestion`. 400 today.             |
| `run.answer_question`  | 2.1    | Medium  | Stub     | Answer an open `RunQuestion` and resume the Run.       |
| `run.complete`         | 2.1    | Medium  | Stub     | Terminate a Run (success or failure). 400 today.       |
| `agent.take_over_run`  | 2.1    | High    | Stub     | Reassign an in-flight Run to a human or other agent.   |
| `agent.cancel_run`     | 2.1    | Medium  | Stub     | Cancel an in-flight Run without applying side effects. |
| `agent.retry_run`      | 2.1    | High    | Stub     | Re-queue a failed / cancelled Run.                     |

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
handler also opens the run-question store and decorates any card with
an open question with the corresponding `run_id`
(`internal/mcp/tools.go:174`–`:182`). Source:
`internal/mcp/tools.go:144` (`buildListCardsTool`).

**Wire path.** Reads bypass `/internal/tools/dispatch` and go to
`GET /internal/board/cards` (`HTTPBoardClient.ListCards` at
`internal/mcp/tools.go:575`); the filter is applied client-side.

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
best-effort; returns `active_run: null` when no run is in flight. The
lookup picks the newest open question for the card and shapes its
`Run` into a `RunSummary` (`internal/mcp/tools.go:241`–`:274`,
`findActiveRunForCard`). Source: `internal/mcp/tools.go:201`
(`buildGetCardTool`).

**Wire path.** `GET /internal/board/cards/{card_id}` via
`HTTPBoardClient.GetCard` (`internal/mcp/tools.go:610`). The comment
thread is returned inline on `Card.Comments`; the active-run lookup
runs in-process against the local `RunStore` injected into `ToolDeps`.

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
        "waiting_on":    { "$ref": "internal/agent/types.go:165#RunQuestionRef" },
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

**Purpose.** Create a new card on the active board. As of S2.1 the
tool routes through `HTTPBoardClient.CreateCard`
(`internal/mcp/tools.go:634`), which posts a `card.create` envelope to
`/internal/tools/dispatch`. `cmd/server.dispatchCardCreate`
(`cmd/server/internal_dispatch.go:85`) translates the MCP-shaped args
to `create_ticket` and runs them through `ApplyToolCallWithMeta`, so
`ActionLedger` + risk gates + dry-run staging now apply uniformly with
the voice / UI surface. When the optional `assignee` is supplied, the
dispatcher fans out a follow-up `assign_ticket` call against the
freshly-created card so the MCP call site stays single-step
(`cmd/server/internal_dispatch.go:136`–`:149`).

Source on the MCP side: `internal/mcp/tools.go:278`
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
(`CardPatch` at `internal/mcp/tools.go:64`) distinguish "no change"
from "set to empty"; the JSON surface mirrors this by treating absent
keys as unchanged. Status changes use the canonical status vocabulary
(`Backlog` / `In Progress` / `Blocked` / `Done`). `tags` follows
explicit set-tracking — `TagsSet` (`internal/mcp/tools.go:70`) is set
to true only when the key is present in `patch`, so `tags: []` clears
the set while omission leaves it alone. Source:
`internal/mcp/tools.go:320` (`buildUpdateCardTool`).

**Wire path.** `HTTPBoardClient.UpdateCard`
(`internal/mcp/tools.go:662`) sends the patch with
`tags` rendered as a pointer (`updatePatchPayloadInner.Tags` at
`internal/mcp/tools.go:658`) so the dispatch endpoint preserves
omit-vs-clear semantics. `cmd/server.dispatchCardUpdate`
(`cmd/server/internal_dispatch.go:162`) fans the patch out across
`update_ticket` (title/notes), `move_ticket` (status),
`assign_ticket` / `unassign_ticket` (assignee), and the
add/remove-tag pair (`cmd/server/internal_dispatch.go:236`–`:253`).
Each leg is a separate `ApplyToolCallWithMeta` call so the risk gate
fires per-field.

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
`ToolDeps` at `internal/mcp/tools.go:127`); per-token identities arrive
in a later sprint. Source: `internal/mcp/tools.go:374`
(`buildCommentTool`).

**Wire path.** `HTTPBoardClient.AddComment`
(`internal/mcp/tools.go:693`) posts a `card.comment` envelope to
`/internal/tools/dispatch`. `cmd/server.dispatchCardComment`
(`cmd/server/internal_dispatch.go:267`) translates the MCP-shaped
args to `add_comment` (`body` → `comment`) and runs them through
`ApplyToolCallWithMeta`. When `as_actor` is non-empty the dispatcher
overrides `toolCallMeta.Actor` so the comment is attributed correctly
(`cmd/server/internal_dispatch.go:285`–`:287`).

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

## Coming next (Sprint 2.1+)

The Run-lifecycle and agent-control tools below are planned but not
yet wired through the dispatch endpoint. Their shapes are illustrative;
the canonical schemas will land alongside the implementations and will
live next to the S2.0 tools in `internal/mcp/tools.go`.

**Today's behavior:** the dispatch endpoint switch at
`cmd/server/internal_dispatch.go:70`–`:79` only recognizes
`card.create`, `card.update`, and `card.comment`. Any of the names
below sent to `/internal/tools/dispatch` returns 400 with body
`{"error":"unknown tool \"<name>\""}`. The MCP UI surfaces that error
inline rather than swallowing it, so agents see the failure on their
next planning step.

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
`internal/agent/types.go:210`.

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
`internal/agent/types.go:201`.

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
shape: `internal/agent/types.go:135`.

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

### `run.answer_question`

**Risk:** Medium — resolves the human gate that paused a Run; the
agent loop resumes from the same step.

```json
{
  "type": "object",
  "required": ["question_id", "answer"],
  "properties": {
    "question_id": { "type": "string" },
    "answer":      { "type": "string" },
    "answered_by": { "type": "string" }
  }
}
```

The server-side handler will mirror `RunCoordinator.AnswerQuestion`
(see `internal/agent/coordinator.go` `AnswerQuestion`). Today the
voice / UI surfaces are the only callers; once wired the MCP path will
share the `answered_via=mcp` branch
(`internal/agent/types.go:155`–`:157`). Returns `{ "question":
<RunQuestion>, "run": <RunView> }`.

### `agent.take_over_run`

**Risk:** High — transfers responsibility for an in-flight Run to a
human or to a different agent profile. The original agent's checkpoint
trail is preserved.

```json
{
  "type": "object",
  "required": ["run_id"],
  "properties": {
    "run_id":      { "type": "string" },
    "new_owner":   { "$ref": "internal/board/types.go#Actor" },
    "note":        { "type": "string" }
  }
}
```

Returns `{ "run": <RunView> }`. The new owner is recorded as an
`Actor` so an agent can hand off to a human or to a peer agent.

### `agent.cancel_run`

**Risk:** Medium — terminates the Run cleanly without applying any
remaining side effects. The audit trail records cancellation as a
terminal `cancelled` status.

```json
{
  "type": "object",
  "required": ["run_id"],
  "properties": {
    "run_id": { "type": "string" },
    "reason": { "type": "string" }
  }
}
```

Returns `{ "run": <RunView> }`.

### `agent.retry_run`

**Risk:** High — re-queues a failed or cancelled Run. The retry Run
inherits the original objective and card binding; `Run.RetryOf`
(`internal/agent/types.go:59`) points back at the source run for
audit-trail continuity.

```json
{
  "type": "object",
  "required": ["run_id"],
  "properties": {
    "run_id":          { "type": "string" },
    "new_objective":   { "type": "string" },
    "cost_budget_cents": { "type": "integer" }
  }
}
```

Returns `{ "run_id": "...", "run": <RunView> }`.

---

## Cross-references

- `docs/api/openapi.yaml` — canonical HTTP schema. The
  `/internal/tools/dispatch` and `/internal/board/cards[/{id}]` paths
  document the request / response envelopes
  `HTTPBoardClient` (`internal/mcp/tools.go:438`) speaks.
  `DispatchCardResult`, `DispatchCommentResult`, and
  `RequiresApprovalEnvelope` schemas live in the same file.
- `docs/adrs/0003-mcp-server-as-universal-external-surface.md` —
  architectural rationale for the MCP server as the canonical external
  surface.
- `docs/adrs/0002-canonical-board-with-external-projections.md` —
  how MCP tools relate to the Jira / GitHub projection layer.
