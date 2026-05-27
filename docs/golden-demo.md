# Golden Demo Path

The golden demo is the canonical happy-path narrative customer-facing engineers walk through to prove auto-bot v2 works end-to-end. Every step lists the exact command or click, the expected response (HTTP code + sample JSON), and what to observe in the UI. File:line citations point to the code that produces each behaviour.

The demo touches every v2 surface: async intake, voice meeting, agent assignment, ask-the-human pauses, MCP-driven updates, the dry-run safety net, the pause-all kill switch, and the post-meeting closer. Run it before a sales call. Re-run it after every cmd/server change that touches mutation flow.

---

## 0. Pre-flight

The repo's preflight gate is `scripts/validate-golden-demo.sh`. Run it once before the live demo to check setup readiness without mocks:

```bash
AUTO_BOT_BASE_URL=http://localhost:3001 \
AUTO_BOT_ACCESS_TOKEN="$(scripts/keychain-get-secret.sh auto-bot/app-api-token "$USER")" \
scripts/validate-golden-demo.sh
```

Expected:

- `setup/status` returns `{"ready": true, ...}` with non-empty `voice`, `livekit`, `bedrock`, `jira_configured`.
- `voice/status` reports `nova_sonic` and `livekit` both `ok`.
- No mocks active.

If any check fails, fix the underlying configuration before continuing.

---

## 1. Setup

```bash
export APP_API_TOKEN=dev
export MCP_SIGNING_KEYS="k1:$(openssl rand -base64 32)"
docker compose build
docker compose up -d
```

The compose file (`docker-compose.yml`) starts LiveKit and the `app` service. `APP_API_TOKEN` is the shared bearer for HTTP + WebSocket auth and for `cmd/mcpd`'s callbacks into `/internal/tools/dispatch` (propagated as `BOARD_TOKEN`). `MCP_SIGNING_KEYS` is the symmetric secret cmd/server uses to sign MCP bearer tokens and cmd/mcpd uses to verify them (#58 hard cut removed the static `MCPD_TOKEN`). See [docs/api/mcp-tools.md#authentication](api/mcp-tools.md#authentication) for the token model.

Expected:

```
$ docker compose ps
NAME                 STATUS                  PORTS
auto-bot-app-1       Up (healthy)            127.0.0.1:3001->3000/tcp
auto-bot-livekit-1   Up                      127.0.0.1:7880->7880/tcp, ...
```

Sanity check:

```bash
curl -s http://localhost:3001/healthz
# → ok
curl -s -H "Authorization: Bearer dev" http://localhost:3001/setup/status | jq .ready
# → true
```

---

## 2. Open the board

Open **http://localhost:3001/app/** in a browser.

The React SPA is served by `cmd/server/main.go:258` (`http.FileServer(http.Dir("web/app/dist"))`). If `/app/` returns 404, build the SPA first: `cd web/app && npm install && npm run build`.

Expected:

- The Observatory Deck dark surfaces render (palette tokens in `web/app/tailwind.config.js`: `void` #0B0D14, `sky` #13161F, `atmos` #1B1F2B). The "Observatory" wordmark sits top-left (`web/app/src/components/BoardHeader.tsx:56`).
- Four kanban columns visible — Backlog, In Progress, Blocked, Done. They are empty on a fresh boot (no initial cards are seeded into the React board state for v2).
- The connection pill is solar-orange ("connecting") then aurora-green ("connected") within ~1 second.
- A SignInGate may appear first if `APP_AUTH_MODE=token` is on (`web/app/src/components/SignInGate.tsx:23`); paste the `APP_API_TOKEN` to proceed.

What to talk about while pointing: "This is the canonical board. Voice, the React drawer, MCP clients, and async intake all funnel through one mutation path so the audit log tells one story."

---

## 3. Async intake — first Blocked card

Card IDs in this doc are illustrative. On a fresh local board, `cmd/server/board.go:2136` (`createCardIDLocked`) mints sequential IDs like `kanban-card-001`, `kanban-card-002`. When Jira sync is configured, the Jira project key takes over (e.g. `ABV2-088`); the test fixtures in `web/app/src/test/fixtures.ts:9` use the `ABV2-` prefix. Substitute the actual minted ID from each response as you walk through.

```bash
curl -sX POST http://localhost:3001/intake/standup \
  -H "Authorization: Bearer dev" \
  -H "Content-Type: application/json" \
  -d '{
    "submitter": "scott@moore.cloud",
    "yesterday": "Shipped the trust ceremony slice.",
    "today": "Walking the golden demo.",
    "blockers": [{"text": "Need a decision on the Jira workflow mapping for blocked cards."}]
  }'
```

Endpoint: `cmd/server/main.go:246` → `intakeStandupHandler` at `cmd/server/intake_handler.go:58`. The body is normalized by `intake.Normalize` (`internal/intake/types.go:70-124`), persisted into the in-memory store (`internal/intake/store.go`), and then fanned out by `runIntakeFollowups` (`cmd/server/intake_followups.go:38`).

Expected response (HTTP 200):

```json
{
  "ok": true,
  "intake": {
    "submitter": "scott@moore.cloud",
    "submitted_at": "2026-05-26T18:42:01.123Z",
    "yesterday": "Shipped the trust ceremony slice.",
    "today": "Walking the golden demo.",
    "blockers": [{"text": "Need a decision on the Jira workflow mapping for blocked cards."}],
    "source": "form"
  },
  "created": [
    {"card_id": "ABV2-001", "title": "Need a decision on the Jira workflow mapping..."}
  ],
  "comments": []
}
```

Observe on the UI:

- Within ~1 second a new card appears in the **Blocked** column, titled with the blocker text.
- The card carries `assignee.kind = "human"` for `scott@moore.cloud` because the SecArch-002 self-assign rule applies (caller identity matches the submitter, so the assignment skips the confirmation queue — `cmd/server/intake_followups.go:38-49`).

Talking point: "The first card on a fresh board is not from voice, not from Jira, not from the UI. It is a Slack-style update an EM dropped into the form before the meeting. Async intake is how the board fills up between meetings."

---

## 4. Voice standup

Click the **Start Meeting** affordance on the legacy meeting page (`http://localhost:3001/`) and connect Nova Sonic.

`cmd/server/nova_sonic.go` is the Nova Sonic provider. It joins LiveKit as a participant; transcripts and tool calls flow through `cmd/server/board.go:354` (`applyToolCall`) which dispatches to the same `ApplyToolCallWithMeta` funnel the intake handler used.

Say into the meeting:

> "Create a ticket: harden the Jira webhook signature check. Move ABV2-001 to In Progress. Create another one: write the blameless retro template for this sprint."

Expected:

- Two new cards appear in **Backlog** within ~2-3 seconds of the spoken sentences ending.
- The ABV2-001 card slides from Blocked into In Progress.
- The board updates **live** via the WebSocket — there is no refresh, no polling. The hook lives at `web/app/src/lib/useBoardSocket.ts`.

Talking point: "The voice path is not a transcription that gets re-typed. Every spoken sentence becomes a tool call (`create_ticket`, `move_ticket`) that goes through the same audit ledger the intake form used. One mutation funnel."

---

## 5. Assign to an agent

Say:

> "Aki, take ABV2-002. Nova, you take ABV2-003."

The voice tool `assign_ticket_to_agent` (`cmd/server/agent_runs.go:88`) runs and:

1. Sets `card.assignee.kind = "agent"` (`board.ActorKindAgent` at `internal/board/types.go:81`) with `AgentProfile: "aki"` (or `"nova"`).
2. Mints a `RunID` via `agent.NewRunID()` (`internal/agent/id.go:34`).
3. Persists the Run via `RunStore.SaveRun` (`cmd/server/board_store.go`).
4. Kicks off the orchestrator loop in the background (`executeRun` at `cmd/server/agent_runs.go:358`).

Expected:

- Both cards now show a solar-orange agent assignee chip in the UI (`solar` = #FF8C42 in `web/app/tailwind.config.js:15`).
- Click ABV2-002 — the CardDrawer slides in from the right (`web/app/src/components/CardDrawer.tsx`). The Run tab shows the freshly-created Run with status `queued` → `classifying` → `fetching_context`.

Talking point: "The assignee column on a card has two value spaces in v2: humans, and agents. An agent assignee carries a Run. The Run has a status, a plan, checkpoints, and cost accounting. None of that is a Slack message — it is durable state."

---

## 6. Run paused — ask the human

Within ~10 seconds, ABV2-002's Run reaches a point where it cannot proceed without input. The PM-classification step (`agentRunOrchestrator.classifyRun` at `cmd/server/agent_runs.go:422`) decides it needs a clarification and the orchestrator calls `Coordinator.AskHuman` (`cmd/server/agent_coordinator.go:130`).

This triggers:

1. A `RunQuestion` is persisted with a fresh ULID (`internal/agent/id.go:24`) and TTL of 4 hours (`internal/agent/types.go:152`).
2. The Run transitions to `StatusWaitingOnHuman` (`internal/agent/types.go:34`).
3. Two WebSocket events fire: `run_question_asked` carries the full question, `run_paused` carries the `RunView` (`cmd/server/agent_coordinator.go:185-187`).
4. A `paused` checkpoint is appended to the audit log (`cmd/server/agent_coordinator.go:170-178`).

Expected:

- The ABV2-002 card now shows a **warm-copper question banner** inline on the card (`web/app/src/components/RunQuestionBanner.tsx:12` — "solar-copper banner that surfaces a Nova question").
- The CardDrawer's Run tab (`web/app/src/components/CardRunTab.tsx`) shows the same question, plus suggested-answer chips below the prompt (`web/app/src/components/SuggestedAnswerChip.tsx`).
- A solar-orange "Waiting on human" pill replaces the running indicator.

Talking point: "The agent has the right answer is the wrong outcome here. The agent has *uncertainty about scope*, and instead of guessing and apologizing later, it stops and asks. We surface the question on the card *and* in the drawer so a passing-by EM can resolve it without opening the full run."

---

## 7. Human answers

Click the recommended suggestion chip on the question banner (or type a free-form answer). The React tab posts to the answer endpoint, which calls `Coordinator.Resume` (`cmd/server/agent_coordinator.go:202`).

`Resume`:

1. Calls `RunStore.MarkRunQuestionAnswered` (`internal/agent/store.go:87`), stamping the answer body, identity (the caller), and `answered_via = "ui"` (`internal/agent/types.go:155-157`).
2. Clears `Run.WaitingOn`.
3. Transitions the Run back to a running status.
4. Broadcasts `run_resumed` over WebSocket carrying the updated `RunView` (`cmd/server/agent_coordinator.go:273`).

Expected:

- The warm-copper banner disappears from the card within ~1 second.
- The Run tab status returns to `classifying` / `fetching_context` / `reviewing` (whichever step the orchestrator advances to).
- The Run's `Checkpoints` array (visible in the Run tab) gains a new entry whose `Status` is the resumed-from-paused value.

If the question expired before the answer arrived, the answer endpoint returns 409 carrying `agent: run question expired` — the UI shows "the question timed out — restart the ask" rather than a generic error (`internal/agent/store.go:20-27`).

Talking point: "The Run did not restart from scratch. The plan steps the agent already completed are preserved. Resume means resume, not retry."

---

## 8. MCP-driven update — Claude Code

In a separate terminal, run a Claude Code session and point it at `cmd/mcpd`:

```bash
# In your Claude Code MCP config:
{
  "mcpServers": {
    "auto-bot": {
      "command": "docker",
      "args": ["compose", "exec", "-T", "mcpd", "/mcpd",
               "--transport=stdio", "--board-url=http://app:3000",
               "--tenant-id=default", "--board-id=default"],
      "env": {"BOARD_TOKEN": "dev"}
    }
  }
}
```

Or hit the HTTP transport directly:

```bash
curl -sX POST http://localhost:4000/mcp \
  -H "Authorization: Bearer dev" \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc":"2.0", "id":1, "method":"tools/call",
    "params": {
      "name":"card.update",
      "arguments": {
        "card_id": "ABV2-001",
        "status": "Done",
        "tags": ["intake","resolved"]
      }
    }
  }'
```

The MCP server is at `internal/mcp/server.go`. The HTTP transport (`:273-309`) validates the bearer via `checkBearer` (`internal/mcp/auth.go:16`), parses the JSON-RPC envelope, and dispatches to `card.update` (`internal/mcp/tools.go:320`). The `HTTPBoardClient` (`internal/mcp/tools.go:449-712`) then POSTs back to `cmd/server`'s `/internal/tools/dispatch` (`cmd/server/internal_dispatch.go:36`), which fans the call through `ApplyToolCallWithMeta` — so the MCP-driven update goes through the **same** audit ledger as the voice and intake mutations.

Expected response:

```json
{"jsonrpc":"2.0","id":1,"result":{
  "card": {
    "id":"ABV2-001",
    "status":"Done",
    "tags":["intake","resolved"],
    "assignee":{"kind":"human","id":"scott@moore.cloud", ...}
  }
}}
```

Observe on the UI:

- ABV2-001 slides from In Progress to Done within ~1 second.
- The card thread shows no new comment (this was `card.update`, not `card.comment`).

For an MCP write that *sets* an agent assignee (no Run kicked off yet — `card.create` fans out to `assign_ticket` but not to `assign_ticket_to_agent`; see `cmd/server/internal_dispatch.go:136-149`), call `card.create` with an agent `assignee`:

```json
{
  "title": "Run a security review of PR #42",
  "description": "GitHub PR auto-bot/auto-bot#42; focus on the new MCP auth path.",
  "assignee": {"kind":"agent","agent_profile":"aki","id":"agent:aki"}
}
```

The card lands with `assignee.kind = "agent"` (`internal/board/types.go:81`). To actually kick off the autonomous Run, voice the assignment ("Aki, take this") or wire `assign_ticket_to_agent` into your MCP client. Talking point: "Same audit log. The CEO running a Claude Code session and the EM running a voice meeting see one story — the difference today is which tool kicks the Run."

---

## 9. Dry-run safety net

Flip dry-run on for the tenant:

```bash
curl -sX POST http://localhost:3001/tenant/settings \
  -H "Authorization: Bearer dev" \
  -H "Content-Type: application/json" \
  -d '{"dry_run_enabled": true}'
```

Endpoint: `cmd/server/main.go:250` → `tenantSettingsHandler` at `cmd/server/dry_run_http.go:131`. The handler updates `tenantSettings.DryRunEnabled` (`cmd/server/tenant_settings.go:14`) and broadcasts a `tenant_settings` WebSocket event so the UI repaints.

Expected response:

```json
{
  "tenant_id":"default",
  "dry_run_enabled":true,
  "agents_paused":false,
  "updated_at":"2026-05-26T18:51:12.456Z"
}
```

Now try to delete ABV2-003 via MCP:

```bash
curl -sX POST http://localhost:4000/mcp \
  -H "Authorization: Bearer dev" \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc":"2.0", "id":2, "method":"tools/call",
    "params": {"name":"card.update", "arguments":{
      "card_id":"ABV2-003", "status":"Done", "tags":["abandoned"]
    }}
  }'
```

This time `ApplyToolCallWithMeta` enters the dry-run branch at `cmd/server/board.go:328-339`. The call is **diverted** into the pending-actions queue (`board.stagePendingAction`); no board mutation occurs.

Expected:

- A new entry appears in the React DryRunQueue (`web/app/src/components/DryRunQueue.tsx`). Click it.
- The `DiffPreview` (`web/app/src/components/DiffPreview.tsx`) renders the before/after of the staged mutation: the card's current shape vs the one the MCP caller intended. Behind it is the `PreviewPendingAction` endpoint (`cmd/server/diff_preview.go:34-87`).
- Verify the diff:
  ```bash
  curl -s -H "Authorization: Bearer dev" \
    http://localhost:3001/tenant/pending_actions | jq '.actions[0]'
  ```
- **Reject** by clicking the reject button or:
  ```bash
  curl -sX POST http://localhost:3001/tenant/pending_actions/{action_id}/reject \
    -H "Authorization: Bearer dev" -H "Content-Type: application/json" \
    -d '{"note":"abandoned tag is wrong"}'
  ```
  Endpoint at `cmd/server/dry_run_http.go:48`.

Expected:

- ABV2-003 is **unchanged** — still in Backlog, still tagged with its original tags, no `abandoned` tag.
- The pending-action row transitions to `rejected` in the queue.

Talking point: "Dry-run is not a separate execution path. The same MCP call that mutated the board in step 8 was diverted by one boolean. Everything the agent would have done is staged with full provenance; you approve or reject from the diff."

Turn dry-run back off when done:

```bash
curl -sX POST http://localhost:3001/tenant/settings \
  -H "Authorization: Bearer dev" -H "Content-Type: application/json" \
  -d '{"dry_run_enabled": false}'
```

---

## 10. Pause all agents

Toggle the **PauseAllPill** in the UI (`web/app/src/components/PauseAllPill.tsx:9` — "glows Magnetar copper (#FF3D7F via the magnetar token); when off it is a desaturated chip"), or call the API directly:

```bash
curl -sX POST http://localhost:3001/tenant/settings \
  -H "Authorization: Bearer dev" -H "Content-Type: application/json" \
  -d '{"agents_paused": true}'
```

When `AgentsPaused` flips from false to true, the handler calls `handleAgentsPausedTransition` (`cmd/server/dry_run_http.go:166-168`). The real implementation lives at `cmd/server/pause_all.go:23-58`:

1. Every non-terminal Run for the tenant transitions to `StatusPaused` (`internal/agent/types.go:40-46`).
2. A `run_paused` event fires per transitioned Run.
3. A paused `Checkpoint` is appended to each Run.

Now try to start a new Run via MCP (use a `card.create` with an agent assignee, or assign an existing card via voice):

Expected:

- The call returns an error wrapping `agent.ErrAgentsPaused` (`internal/agent/store.go:29-34`). The MCP-side surface shows "tenant has paused all agents" rather than a generic startup failure.
- The orchestrator does NOT persist a Run row. The kill switch is enforced at `cmd/server/agent_coordinator.go:46-48` before any database write.

Observe on the UI:

- The PauseAllPill is glowing magnetar (#FF3D7F).
- Every Run card shows a `paused` chip.
- The Run drawer for any in-flight Run shows the paused checkpoint.

Turn the kill switch off:

```bash
curl -sX POST http://localhost:3001/tenant/settings \
  -H "Authorization: Bearer dev" -H "Content-Type: application/json" \
  -d '{"agents_paused": false}'
```

Paused runs transition back to `StatusQueued` so the orchestrator can pick them up again.

Talking point: "Pause-all is the operator's red button. It does not kill in-flight network calls — that would be uncontrolled. It marks every Run paused and refuses to start new ones until you release the switch. The orchestrator and the MCP layer both honour the same boolean."

---

## 11. Post-meeting closer

End the voice meeting. The orchestrator builds a `meetings.MeetingArtifact` (`internal/standup/closer.go:58`), then `internal/standup.Closer.Close` runs:

1. For every `FollowUp` / `UnresolvedBlocker` in the report that does not already point at a card, create a new Blocked-column card via the `CardCreator` injected from cmd/server.
2. For each card whose assignee is an agent (`board.ActorKindAgent`), call `Coordinator.Start` to kick a follow-up Run.
3. Persist the meeting report via `ArtifactSink.PersistMeetingReport` (the cmd/server adapter wraps the meeting-report store).

Expected:

- Two-to-three new cards appear in **Blocked** within ~2 seconds of "End Meeting" being clicked, each carrying the follow-up text from the meeting report.
- Cards whose meeting follow-up named an agent assignee (e.g. "Aki to retest the auth flow") arrive with an agent assignee chip and a fresh Run already in `queued` state.
- A new entry appears in `/meetings` (`cmd/server/main.go:223`) — open `/post-meeting` to see the archived report.

Talking point: "The meeting did not just produce a transcript. It produced cards, runs, and a durable report. The board state at the end of the meeting *is* the meeting's output."

---

## Pass criteria

A demo passes when every step above produces its expected observable in the listed time budget. Specifically:

- **Async intake → card** within 2s of the `/intake/standup` 200.
- **Voice tool call → board update** within 3s of the spoken sentence ending.
- **Agent assignment → Run row** within 1s of the `assign_ticket_to_agent` tool call.
- **AskHuman → question banner** within 1s of the orchestrator's pause.
- **Human answer → resumed Run** within 1s of clicking the suggestion chip.
- **MCP write → board update** within 1s of the JSON-RPC 200.
- **Dry-run intercept** prevents mutation; the diff preview renders the intended change.
- **Pause-all** rejects new `Run.Start` with `ErrAgentsPaused`; release transitions queued runs back to `StatusQueued`.
- **Meeting closer** creates follow-up cards and kicks Runs for agent-assigned ones.

If any step fails, capture the WebSocket frames (browser devtools → Network → WS) and the server log line for the failing endpoint. The audit replay surface (`docs/architecture.md` and `cmd/server/audit.go`) is the second source of truth — every observable above is anchored to a ledger entry.
