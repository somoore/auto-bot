# SecArch-001: Tenant + MCP + Agent Identity Review

**Branch:** `agent-first-v2-sprint-0`
**Reviewer:** Security Architect #1
**Date:** 2026-05-26
**Scope:** commits `1dc7781`, `bcb5008`, `9aa953d`, `043ca73`; in-flight `cmd/mcpd`.

---

## 1. Tenant isolation correctness (board_store.go)

Every `*sqliteBoardStore` query on a tenant-scoped table includes `WHERE tenant_id = ? AND board_id = ?` and normalizes via `normalizeTenantID` first. Spot-verified across `LoadBoard:410`, `SaveSnapshot:434`, `AppendEvent:459`, the meeting-report trio `:480-523`, the run trio `:574-622`, the mutation trio `:663-710`, the checkpoint pair `:759-773`, and the run-question quintet `:831-929`. `migrateTenantSchema` stamps every legacy row `'default'` inside one transaction. No naked `SELECT … FROM <table>` found.

**Two issues remain:**

1. **`MarkRunQuestionAnswered` (`board_store.go:907`) trusts caller scope.** It loads via `LoadRunQuestion` (correctly scoped), then overwrites `q.TenantID = tenantID` before `SaveRunQuestion`. If a future caller resolves the wrong `(tenant, board)` for a given `question_id`, the answer lands in the wrong scope. Question IDs are ULIDs so accidental collision is negligible, but the method should defensively assert `q.TenantID == tenantID` after load.

2. **Card ID collisions across tenants are by design.** `nextOperationIDLocked` is a per-board counter; tenant A's `card-001` and tenant B's `card-001` coexist. Logs that drop tenant context (`jira_webhook.go:53`) become ambiguous; any future debug tool keyed only on `card_id` becomes a cross-tenant footgun.

---

## 2. WebSocket payload leakage — HIGHEST SEVERITY

`broadcastKanbanEventForBoard` (`board.go:2353`) enriches `"board"` events with `OpenRunQuestions` via `defaultOpenRunQuestionsProvider:2321`, which scopes correctly by `(board.tenantID, board.boardID)`. The *data* is tenant-correct.

The *fanout* is not. `wsClients` is declared `map[*threadSafeWriter]string{}` at `board.go:2270` — value is `boardID` only, not `(tenantID, boardID)`. Registration at `main.go:589` calls `registerWSClient(c, authCtx.BoardID)` and discards `authCtx.TenantID`. The fanout loop at `board.go:2380-2384` matches on `clientBoardID == boardID`.

Single-tenant today is safe because `boardID == "default"` everywhere. The moment two tenants share a board name — which nothing forbids, and `defaultAppBoardID = "default"` (`auth.go:24`) is the hard-coded fallback for everyone — **every WS client for board "default" across all tenants receives every other tenant's snapshots, RunQuestions, transcripts, and `agent_run` payloads**. `lookupBoardForBroadcast:2343` confirms exactly one in-process board today, so the bug ships pre-armed for S5.

---

## 3. MCP token model risks

The plan claims "per-tenant MCP token, short TTL, scoped capabilities." Reality (`internal/mcp/auth.go`) is a single static bearer (`checkBearer:16-33`), enforced only on the HTTP transport. Stdio is intentionally unauthenticated (`server.go:99`: "Stdio is always trusted").

| Vector | Severity | Notes |
|---|---|---|
| Token replay across tenants | **Critical** | No tenant binding. `tools.go:30-36` accepts `tenantID` as a parameter, nothing cross-checks it against token claims. |
| Rotation on offboarding | High | Single static `AuthToken`; no revocation. `/admin/mcp-tokens` is unspecified. |
| Scope grants too coarse | High | Binary in/out. A read-only agent can still call `run.start` and `card.update`. |
| Process-tree trust on stdio | Med | Any user-installed npm package on a developer laptop can drive the same authority as Cursor — confused-deputy. |
| No audience/signing | High | A leaked bearer is universally usable. JWT with `aud=mcpd`, `scope[]`, `exp ≤ 30m` fixes replay + scope + rotation. |

**Recommended:** short-lived JWT (≤30 min), claims `{ten, sub, scope[], aud=mcpd, jti, exp}`. Stdio carries the same JWT via env var or an `initialize`-time handshake — drop "stdio is trusted".

---

## 4. Agent identity spoofing

`assign_ticket_to_agent` lives in `cmd/server/agent_runs.go:88-187`, not in `scrum_tools.go` (plan reference is stale post-`bcb5008`). Construction at lines 159-165:

```go
card.Assignee = &kanbanActor{
    Kind:         kanbanActorKindAgent,
    ID:           "agent:" + truncateString(agentProfile, 80) + ":" + board.tenantID,
    DisplayName:  truncateString(agentProfile, 80),
    AgentProfile: truncateString(agentProfile, 80),
    OwnerHumanID: requestedBy,
}
```

**Validation logic: none.** `agentProfile` comes straight from `args["agent_profile"]` (line 97) with string truncation only. The OpenAI tool schema at `board.go:893-902` advertises an `enum` of six profiles, but JSON-schema enums are enforced **by the model, not the tool runtime** — any MCP client posting `tools/call` can supply any value. `requested_by` (line 108) is likewise a free string from args.

**Concrete spoofs available today:**

- `agent_profile: "ceo-bot"`, `requested_by: "ceo@victim.com"` — card shows fabricated CEO ownership with no audit trail of provenance.
- Cross-profile masquerade: any caller can become `agent:security_scanner:tenant-default` and ride the existing GitHub PR-write path through `executeRun` (line 175).

**Fix:** allowlist `agentProfile` against a server-side per-tenant agent registry; populate `OwnerHumanID` from `authCtx.Identity` (or `meta.Actor`), never from args.

---

## 5. ActionLedger as audit substrate

**The claim that ActionLedger is the audit substrate is false in this codebase.** `internal/core/ledger.go` defines the interface and `InMemoryActionLedger`. Grep across `cmd/server` returns **zero** non-test call sites of `RecordIntent`/`RecordToolCall`/`RecordExternalConfirmation`. Only `internal/mcp/doc.go:6` and `internal/mcp/tools.go:285` reference it, both aspirationally.

What actually persists is `boardEventRecord` via `AppendEvent` and `boardMutationRecord` via `SaveMutationRecord`. Tenant-scoped, but with **documented bypasses**:

1. **Inbound Jira via `ReplaceCards`** (`board.go:524`, called from `jira.go:82`) writes a snapshot but no mutation record. Jira-driven changes are invisible to `ListMutationRecords`.
2. **Jira webhook** (`jira_webhook.go:61`) calls `RefreshFromJira` → `ReplaceCards`. Same bypass.
3. **Async `executeRun` status updates** (kicked off at `agent_runs.go:176`) write to `agent_runs` but emit no mutation record. Run lifecycle is invisible to the audit trail.
4. `take_over_agent_run` (`agent_runs.go:256+`) routes through `applyToolCall`, so the outer wrapper records. OK.
5. WS-driven mutations (`handleClientKanbanCommand`, `chat_messages.go`) flow through `ApplyToolCallWithMeta`. OK.

The threat-model entry "SQLite action replay ledger persists mutation replay records" is a documented control covering ~60% of state changes.

---

## 6. Threat model deltas vs `docs/threat-model.md`

| New / Elevated Threat | Severity | Why |
|---|---|---|
| Cross-tenant WS leakage (new) | Critical | `wsClients` registry not tenant-keyed (Section 2). |
| MCP stdio supply-chain compromise (new) | High | Any npm package on a dev laptop can drive `cmd/mcpd`. |
| Agent profile spoof (new) | High | No server-side allowlist (Section 4). |
| Forged `requested_by` provenance (new) | High | Arg-derived, not authn-derived; defeats "live speech only". |
| Run hijack via question answer (new) | Med | Open RunQuestions broadcast in WS payload (S1.4). |
| Prompt injection in Jira text | **Elevated** | Agents now own PR write + `run.start`; payload reach is wider. |
| Stolen session token | **Elevated** | Sessions tenant-bound but boards globally keyed; "default" board is shared. |
| Audit loss after restart | **Elevated** | ActionLedger unused in prod; inbound Jira + async run updates bypass `action_replay_events`. |

All old threats remain. v2 is strictly additive in risk.

---

## 7. Top 5 recommendations

| # | Severity | Finding | Fix |
|---|---|---|---|
| 1 | **Critical** | `wsClients` keyed only by `boardID`; multi-tenant deploys leak board state cross-tenant on duplicate board names. | Change `board.go:2270` to `map[*threadSafeWriter]struct{Tenant, Board string}`. Update `registerWSClient:2273` to read `authCtx.TenantID` and the fanout at `:2380` to compare both fields. Regression test: two clients with `(tenA, default)` + `(tenB, default)` — only one receives a tenant-A broadcast. |
| 2 | **Critical** | MCP token is a single static bearer; no tenant binding, no scopes, no rotation. | Replace `checkBearer` (`internal/mcp/auth.go:16`) with a JWT verifier. Mint tokens with `{ten, sub, scope[], aud=mcpd, exp<=30m, jti}`. In `tools.go:30-36`, refuse if request `tenantID` ≠ `ten` claim. Add per-tool scope gates: `board.read`, `board.write`, `run.start`, `run.answer`. Drop the stdio carve-out at `server.go:99` — require JWT via env var. |
| 3 | **High** | `assign_ticket_to_agent` lets a caller set `agent_profile` to any string and `requested_by` to any human ID. Agent identity is forgeable. | At `agent_runs.go:97`, validate `agentProfile` against a server-side allowlist (start with the existing six). At line 108, replace the args lookup with `meta.Actor` from `ApplyToolCallWithMeta` (thread through if missing). Regression test: `agent_profile: "ceo-bot"` returns an error. |
| 4 | **High** | The cited "ActionLedger" audit substrate has zero production callers. Inbound Jira via `ReplaceCards` and async run updates bypass `action_replay_events`. | Either delete `internal/core/ActionLedger` and update `docs/threat-model.md` to name `action_replay_events` as the substrate, or wire the ledger into `ApplyToolCallWithMeta` (`board.go:319`) **and** the Jira hydrator (`jira.go:82`). Add a mutation record on every `executeRun` status transition. |
| 5 | **Med** | `MarkRunQuestionAnswered` (`board_store.go:907`) accepts caller-supplied tenant scope without re-validating against the loaded row; `OwnerHumanID` on agents comes from unauthenticated args. | After `LoadRunQuestion` at `:909`, refuse if loaded `q.TenantID != tenantID`. For agent assignees, sanitize `OwnerHumanID` via `authCtx.Identity` per recommendation #3. |

---

## Verdict

Sprint 0 tenant threading is well executed at the store boundary. Risk has migrated up the stack: the WS fanout and the MCP token surface are the next dam to build, and the "audit substrate" needs to be either made real or removed from the threat model so we stop trusting a control that doesn't run.
