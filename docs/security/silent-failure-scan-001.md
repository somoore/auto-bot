# SecEng-001: Silent-failure scan of Sprint 1 code

Status: SecEng-001 deliverable. Author: Security Engineer #1. Date: 2026-05-26. Commit: see git log for "Add SecEng-001".

Scope: commits `0f5bfaf..9aa953d` on branch `agent-first-v2-sprint-0`.
Files audited: `internal/agent/{coordinator,simple_coordinator,run}.go`, `cmd/server/{board_store,run_question_sweeper,agent_coordinator,agent_runs}.go`, `internal/board/types.go`, `internal/mcp/tools.go` (spot-check).

Severities calibrated to the Sprint 2 threat model: paths reachable from MCP-driven external clients are at least High.

---

## F1 — `Actor.UnmarshalJSON` fabricates a Human assignee from `{}` or unrecognized objects
**Severity: High** — `internal/board/types.go:118-127`

The legacy-User fallback accepts any object without a `"kind"` key and produces `Actor{Kind: ActorKindHuman, ID: legacy.AccountID, ...}` with no validation that the payload looked like a `User`. `{}`, `{"foo": 42}`, and (via the line 113-115 normalization) `{"kind": null}` all yield a phantom Human assignee with empty ID. Attackers writing `customFields` JSON via MCP could plant such a payload to defeat any risk gate that branches on `Actor.Kind`.

**Repro:** `Actor.UnmarshalJSON([]byte("{}"))` returns `nil` and `Actor{Kind: "human"}`.

**Fix:** require at least one populated legacy field; reject `{"kind": null}` rather than coercing.
```go
if legacy.AccountID == "" && legacy.DisplayName == "" && legacy.EmailAddress == "" {
    return fmt.Errorf("actor: neither kind nor user fields present: %s", string(data))
}
```

---

## F2 — `agent_coordinator.Checkpoint` silently skips audit-log persistence when store isn't a `RunStore`
**Severity: High** — `cmd/server/agent_coordinator.go:89-93`

```go
if store, ok := orchestrator.board.store.(agent.RunStore); ok {
    if err := store.AppendRunCheckpoint(...); err != nil { return ... }
}
orchestrator.board.updateAgentRun(...)  // continues even if assertion failed
```

`AskHuman` (line 131-134) and `Resume` (192-195) return an error on the same assertion failure — `Checkpoint` is the inconsistent odd-one-out. In-memory stores produce a Plan timeline that never reaches durable storage; after restart the audit log is gone with zero log lines.

**Fix:** mirror the other coordinator methods — `return fmt.Errorf("checkpoint requires an agent.RunStore-backed board store")`.

---

## F3 — `agent_coordinator.Cancel` returns nil on persistence failure
**Severity: High** — `cmd/server/agent_coordinator.go:261-272` + `cmd/server/agent_runs.go:778-788`

`Cancel` mutates in-memory and calls `persistAgentRun`, which uses `context.Background()` and logs-and-continues:

```go
func (board *kanbanBoard) persistAgentRun(run agentRun) {
    if store, ok := board.store.(agentRunStore); ok {
        if err := store.SaveRun(context.Background(), ...); err != nil {
            log.Errorf("Failed to persist agent run: %v", err)  // never returned
        }
    }
}
```

Two failures stacked: (a) `context.Background()` ignores shutdown; (b) SaveRun error is dropped to a log line. Cancel returns `nil` to its MCP client while durable state diverges. Same hazard affects AskHuman/Resume via the same persist call.

**Fix:** `persistAgentRun(ctx, run) error`; thread caller's ctx through `updateAgentRun`; propagate.

---

## F4 — `agent_coordinator.Resume` swallows reload error with a code-comment instead of `log.Errorf`
**Severity: Med** — `cmd/server/agent_coordinator.go:223-233`

After persisting the answer, a re-`LoadRunQuestion` is used to enrich the broadcast. On error the code silently falls back to a synthesized payload. The comment notes "audit concern but should not fail" — yet there is no log call, so Sentry never sees it.

**Fix:** add `log.Errorf("resume reload run_question %s: %v", answer.QuestionID, loadErr)` before the fallback.

---

## F5 — `agent_coordinator.Start` discards type-assertion ok on `run_id`
**Severity: Med** — `cmd/server/agent_coordinator.go:60`

`runID, _ := result["run_id"].(string)` collapses "key missing", "wrong type", and "empty string" into one error message. A future contributor returning `int64` will be reported as "did not return a run_id".

**Fix:** keep the `ok` and surface the actual type in the error.

---

## F6 — `internal/mcp/tools.go:265-267` collapses sentinel and transport errors to `return nil`
**Severity: High** — `internal/mcp/tools.go:250-268`

```go
if err != nil || len(qs) == 0 { return nil }            // line 250
run, err := store.LoadRun(ctx, tenantID, boardID, newest.RunID)
if err != nil { return nil }                            // line 266
```

Both arms drop *all* errors. The `ErrRunNotFound` sentinel introduced in bcb5008 exists to let callers tell "no such run" from "DB exploded" — and the first MCP consumer throws the distinction away. Clients cannot detect storage faults.

**Fix:** `errors.Is(err, agent.ErrRunNotFound)` returns nil; other errors `log.Errorf` then return nil. Audit every `LoadRun`/`LoadRunQuestion` call site as MCP transport lands.

---

## F7 — `Run.View()` shallow-copies `Findings`, aliasing each finding's `Tests []string`
**Severity: Med** — `internal/agent/run.go:48`

```go
Findings: append([]CodeReviewFinding(nil), run.Findings...),
```

`CodeReviewFinding.Tests` is `[]string` (`internal/agent/types.go:189`). The outer slice is copied but `Tests` on every element still aliases the persisted Run's backing array. Mutating `view.Findings[i].Tests = append(...)` can write back into canonical state if cap permits. `Plan` shallow-copy is fine (PlanStep has no reference fields); `Cost.Clone` is correct. Note that `cmd/server/agent_runs.go:827-829`'s `cloneAgentRun` *does* deep-copy `Tests` — `View()` is the regression.

**Fix:**
```go
findings := append([]CodeReviewFinding(nil), run.Findings...)
for i := range findings {
    findings[i].Tests = append([]string(nil), findings[i].Tests...)
}
```

---

## F8 — Sweeper emits false `run_question_expired` for questions answered mid-sweep
**Severity: Med** — `cmd/server/run_question_sweeper.go:81-108`

Documented in code (lines 84-91). Between the pre-sweep and post-sweep `ListOpenRunQuestions`, a question answered concurrently disappears from the open set and is broadcast as expired. "Idempotent UI clients tolerate this" — Sprint 2 MCP consumers may not. Downstream agents will see expired questions that were actually answered and re-ask the human.

**Fix:** change `ExpireRunQuestions` to return `(expiredIDs []string, err error)`. Sweeper broadcasts only the IDs the store actually flipped. Mock and sqlite implementations must match.

---

## F9 — `MarkRunQuestionAnswered` is a racy read-modify-write
**Severity: Med** — `cmd/server/board_store.go:907-921`

Concurrent answers (UI + voice + MCP) all load `status=open`, all write `status=answered`. The last writer wins; the first answer's `answered_by`/`answered_via`/`answer` are silently overwritten. The pre-check in `SimpleRunCoordinator.Resume` (`simple_coordinator.go:203`) is outside any transaction and is not a defense.

**Fix:** rewrite as one conditional UPDATE — `UPDATE run_questions SET status='answered', ... WHERE tenant_id=? AND board_id=? AND question_id=? AND status='open'`. Inspect `RowsAffected()`; return a new `ErrRunQuestionAlreadyAnswered` sentinel when zero.

---

## F10 — `ExpireRunQuestions` silently `continue`s on unparseable `asked_at`
**Severity: Low** — `cmd/server/board_store.go:957-963`

```go
askedAt, parseErr := time.Parse(time.RFC3339Nano, q.AskedAt)
if parseErr != nil {
    askedAt, parseErr = time.Parse(time.RFC3339, q.AskedAt)
    if parseErr != nil { continue }   // silent skip; row leaks open forever
}
```

A corrupted `asked_at` means the question is never expired and never logged. Combined with F8 broadcasting, this is an invisible accumulator of zombie questions.

**Fix:** `log.Errorf("expire: skipping question %s with unparseable asked_at %q: %v", q.ID, q.AskedAt, parseErr)` before continue.

---

## F11 — `UpdateMutationRecord` returns nil when `RowsAffected` itself errors
**Severity: Low** — `cmd/server/board_store.go:693`

```go
if rows, rowsErr := result.RowsAffected(); rowsErr == nil && rows == 0 {
    return fmt.Errorf("mutation record %s was not found", record.EventID)
}
```

If `rowsErr != nil`, "rowcount unknown" is reported as success. SQLite's driver rarely returns this, but the silent fallback violates fail-loud.

**Fix:** propagate `rowsErr` as `fmt.Errorf("update mutation record %s: rows-affected: %w", record.EventID, rowsErr)`.

---

## F12 — Sweeper does not check `ctx.Err()` between operations
**Severity: Low** — `cmd/server/run_question_sweeper.go:62-110` + `cmd/server/board_store.go:956-980`

On shutdown, the per-question save loop in `ExpireRunQuestions` keeps issuing DB writes until each fails on the cancelled context. Not catastrophic but slows shutdown by O(open-questions).

**Fix:** add `if err := ctx.Err(); err != nil { return expired, err }` inside the loop.

---

## Highest-leverage fixes to land BEFORE Sprint 2 MCP exposure

1. **F3 (with F2/F4) — Make `persistAgentRun` return its error and accept a ctx.** This single signature change converts in-memory coordinator paths from "log-and-lie" to "fail-loud" and unlocks proper error propagation through Cancel, AskHuman, Resume, and Checkpoint. Sprint 2's MCP tools today assume a successful return means the change is durable; that assumption is false.

2. **F6 — Restore `errors.Is(err, ErrRunNotFound)` discrimination at the MCP boundary.** The whole sentinel-error refactor in bcb5008 is wasted if every MCP caller maps both arms to `return nil`. Audit all `LoadRun`/`LoadRunQuestion` call sites in `internal/mcp/` before exposing tools.

3. **F1 — Tighten `Actor.UnmarshalJSON`.** Once MCP accepts inbound assignee/customFields JSON, the silent `{} → Actor{Kind: Human}` promotion is a privilege-confusion vector. Reject ambiguous shapes.

Honourable mention: **F9** (`MarkRunQuestionAnswered` race) and **F8** (false expired broadcasts) become user-visible the moment two clients can answer the same question concurrently — Sprint 2 introduces exactly that condition.

---

## Out of scope / noted

- `SimpleRunCoordinator.findRun` (`internal/agent/simple_coordinator.go:262-272`) requires cached scope and errors loud when missing — correct, but the MCP deployment story must thread scope through call args rather than rely on this cache. Sprint-2 design item.
- `boardEventRecord` fields `Source`/`Actor`/`RiskLevel` (`cmd/server/board_store.go:79-82`) are persisted but lack schema validation; once writable via MCP they need their own pass.
