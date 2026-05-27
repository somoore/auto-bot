# Codebase Map

This is the document a new senior engineer reads to get their bearings. It walks the repository top-down: top-level directories, then `cmd/`, then every package under `internal/`, then the web tier, the scripts, and the infrastructure. Every meaningful claim is anchored to a file:line.

Conventions:

- "Sprint N.x" tags trace the slice the code landed in. The agent-first v2 plan ran through Sprint 4 (`Merge Sprint 4: trust ceremony + closed-loop standup`).
- "cmd/server is the monolith" — most live behaviour still lands in `cmd/server/`. The agent-first v2 work moved the *contracts* into `internal/` but several runtime adapters are still package-`main` files that wrap an internal interface.

---

## 1. Top-level

```
cmd/         — Go process entry points.
internal/    — provider-neutral Go packages, the long-term home for behaviour.
web/         — browser tier. Legacy HTML pages + the new React/Vite SPA.
public/      — static assets served at /. Only the README screenshot today.
scripts/     — local dev, AWS deploy, validation, pre-commit, Keychain helpers.
infra/       — Terragrunt live stack and reusable Terraform modules for AWS.
config/      — runtime configuration loaded by cmd/server (none committed yet).
docs/        — architecture, ADRs, OpenAPI, persona feedback, planning, security.
evaluation/  — Go test harness + fixtures that runs the "real path" gates.
examples/    — copy-paste templates for the three core extension contracts.
```

The repo is still one deployable Go binary (`cmd/server`) plus a small auxiliary binary (`cmd/mcpd`). The browser tier is now bimodal: a legacy LiveKit/Nova Sonic HTML page at `/` and the React SPA at `/app/`.

### Why two binaries

`cmd/server` is the canonical board, voice runtime, Jira sync, agent orchestrator, audit ledger, and HTTP/WebSocket API. `cmd/mcpd` is the Model Context Protocol bridge — it speaks JSON-RPC over stdio or HTTP and routes board mutations back into cmd/server through `/internal/tools/dispatch` so every MCP-driven mutation flows through the same `ApplyToolCall` audit path the voice tools use (`cmd/server/internal_dispatch.go:36`, `cmd/mcpd/main.go:5-9`).

---

## 2. `cmd/`

### `cmd/server` — the monolith

`cmd/server/main.go:92` is `func main()`. It registers HTTP routes, opens the WebSocket upgrader, wires the voice provider, and starts the HTTP server. The route table at `cmd/server/main.go:200-258` is the source of truth for the HTTP control plane:

| Group | Routes | Owner file |
| --- | --- | --- |
| Auth + session | `/auth/local-login`, `/auth/session` | `cmd/server/auth.go` |
| Realtime | `/websocket`, `/livekit-token` | `cmd/server/main.go:215`, `cmd/server/main.go:323` |
| Meeting lifecycle | `/meeting/setup`, `/meeting/join`, `/meeting/leave`, `/meeting/status`, `/meeting/type`, `/meeting/intelligence`, `/meetings`, `/post-meeting` | `cmd/server/meeting_access.go`, `cmd/server/meeting_report_handlers.go` |
| Setup + ops | `/setup/status`, `/setup/aws/refresh`, `/observability/status`, `/voice/model`, `/voice/providers`, `/voice/status`, `/identity/status`, `/workspace/status` | `cmd/server/voice_status.go`, `cmd/server/meeting_report_handlers.go`, `cmd/server/aws_refresh.go`, `cmd/server/workspace.go` |
| Internal RPC | `/internal/tools/dispatch`, `/internal/board/cards`, `/internal/board/cards/{id}` | `cmd/server/internal_dispatch.go:36` |
| Async intake | `/intake/standup`, `/intake/slack` | `cmd/server/intake_handler.go:58`, `cmd/server/intake_slack.go` |
| Trust ceremony (Sprint 4.0) | `/tenant/settings`, `/tenant/pending_actions`, `/tenant/pending_actions/{id}/{approve\|reject}`, `/tenant/mutations/{id}` | `cmd/server/tenant_settings.go:131`, `cmd/server/dry_run_http.go:17`, `cmd/server/dry_run_http.go:48` |
| Jira webhook | `/jira/webhook` | `cmd/server/jira_webhook.go` |
| Browser SPA | `/app/*` (StripPrefix → `web/app/dist`) | `cmd/server/main.go:258` |
| Legacy index | `/` (LiveKit/Nova Sonic HTML) | `cmd/server/main.go:261` |

A one-liner per non-obvious file in `cmd/server/`:

- **`main.go`** — process startup; provider selection (`VOICE_PROVIDER=nova-sonic|openai`); HTTP route registration; WebSocket upgrade; security headers; restart broker for AWS credentials.
- **`auth.go`** — shared-token bootstrap, HttpOnly cookie sessions, host/participant authorization, request-auth context. Local-only login when `APP_AUTH_MODE=token`.
- **`rate_limiter.go`** — fixed-window limits for `/websocket` and `/livekit-token`.
- **`workspace.go`** — `/workspace/status` and the workspace-isolation invariants.
- **`board.go`** — kanban core: `kanbanBoard`, `kanbanCard` aliased to `internal/board` (`cmd/server/board.go:20-46`), tool dispatch (`ApplyToolCallWithMeta`), board snapshots, WebSocket broadcasts. `ApplyToolCallWithMeta` is the **single** mutation path; dry-run, pending-action staging, audit recording, and Jira sync all live in this funnel (`cmd/server/board.go:315-352`).
- **`board_store.go`** — SQLite-backed `boardStore`. Tables: `board_snapshots`, `mutation_log`, `agent_runs`, `run_checkpoints`, `run_questions`, `meeting_reports`, `pending_actions`, `tenant_settings`. Implements both `agent.RunStore` and the `pendingActionStore` and `tenantSettingsStore` interfaces.
- **`agent_runs.go`** — `agentRunOrchestrator` constructor (`cmd/server/agent_runs.go:71`), tool entry points (`assignTicketToAgent`, `listAgentRuns`, `getAgentRun`, `cancelAgentRun`, `takeOverAgentRun`, `retryAgentRun` at lines 88-358), and the PM-classification / code-review / publish loop (`executeRun`, `runCodeReview`, `reviewPullRequest`, `postRunJiraComment` at lines 358-692).
- **`agent_coordinator.go`** — the `agent.RunCoordinator` bridge. A compile-time check (`var _ agent.RunCoordinator = (*agentRunOrchestrator)(nil)` at `cmd/server/agent_coordinator.go:16`) ties the in-process orchestrator to the contract MCP tools consume. Methods (`Start`, `Checkpoint`, `AskHuman`, `Resume`, `Cancel`) translate `agent.RunRequest`/`HumanAnswer` into the existing board calls and broadcast `run_question_asked` / `run_resumed` over WebSocket (`cmd/server/agent_coordinator.go:185`, `:273`).
- **`run_question_sweeper.go`** + **`run_questions_test.go`** — background TTL sweeper that expires open `RunQuestion`s past their `asked_at + TTLSeconds` window.
- **`pending_actions.go`** — Sprint 4.0 dry-run queue. `pendingActionStore` interface (`cmd/server/pending_actions.go:73`), in-memory and SQLite implementations, decision/result records. Pending actions expire on a sweeper cadence (`cmd/server/cron.go:139`).
- **`dry_run.go`** — process-wide `dryRunRegistry` holding the `tenantSettingsManager` and `pendingActionStore`; `installDryRunRuntime` is called once from main (`cmd/server/dry_run.go:25`); tests reach for `withDryRunRuntime`.
- **`dry_run_http.go`** — HTTP handlers `pendingActionsHandler` (`cmd/server/dry_run_http.go:17`), `pendingActionDecisionHandler` (`:48`), `tenantSettingsHandler` (`:131`); also exports `handleAgentsPausedTransition` whose real implementation is installed by `cmd/server/pause_all.go:14`.
- **`pause_all.go`** — kill switch fanout. When `agents_paused` flips to true, every non-terminal Run for the tenant transitions to `StatusPaused` and broadcasts `run_paused` (`cmd/server/pause_all.go:23-58`). New `Run.Start` calls return `agent.ErrAgentsPaused` (`cmd/server/agent_coordinator.go:46`).
- **`tenant_settings.go`** — `tenantSettings` struct with `DryRunEnabled` + `AgentsPaused` (`cmd/server/tenant_settings.go:14`); `tenantSettingsManager` with a small in-process cache; SQLite + in-memory `tenantSettingsStore` implementations.
- **`diff_preview.go`** — `PreviewPendingAction` synthesizes a `pendingActionDiff` (changed/created/removed cards + meeting-state diff) so the React `DiffPreview` can render a before/after without applying the mutation (`cmd/server/diff_preview.go:14-35`).
- **`internal_dispatch.go`** — `/internal/tools/dispatch` handler that routes `card.create`, `card.update`, `card.comment` from `cmd/mcpd` back into `ApplyToolCallWithMeta` so the audit ledger fires uniformly.
- **`intake_handler.go`** — POST/GET `/intake/standup`. `intakeStore` (default `intake.NewMemoryStore(200)`) and `intakeParser` (default `intake.NewHeuristicParser()`) are package globals so tests can swap them (`cmd/server/intake_handler.go:22-28`).
- **`intake_slack.go`** — Slack webhook adapter, HMAC-verified via `SLACK_SIGNING_SECRET` (`cmd/server/intake_handler.go:30-41`).
- **`intake_followups.go`** — converts a stored Intake into board cards + thread comments. Self-assignment skips the confirmation queue only when the authenticated identity matches the submitter (SecArch-002, `cmd/server/intake_followups.go:38-49`).
- **`standup_adapter.go`** — wires `internal/standup` to cmd/server's board, RunCoordinator, and meeting-report store.
- **`standup_closer.go`** — the post-meeting closer: builds a `meetings.MeetingArtifact`, calls `internal/standup.Closer.Close`, persists the report (`cmd/server/standup_closer.go:127`).
- **`scrum_tools.go`** — scrum-master tool schemas: subtasks, estimates, worklogs, links, sprint/rank, meeting state, participants, summaries, Jira metadata helpers. Schemas are emitted to both Nova Sonic and OpenAI Realtime.
- **`meeting_access.go`** + **`meeting_access_test.go`** — host setup, meeting codes, join/leave, host-only meeting-type changes.
- **`meeting_intelligence.go`** — risk classification, pending confirmations, audit mutation records, transcript evidence, undo/replay, briefing generation.
- **`meeting_reports.go`** + **`meeting_report_handlers.go`** — post-meeting report assembly, recap generation, sprint risk signals, archived report persistence, and the HTTP surface that serves them.
- **`chat_messages.go`** — typed chat normalization and Bedrock-backed English translation for non-English message bodies.
- **`nova_sonic.go`** + **`nova_sonic_output.go`** + **`nova_sonic_mixer.go`** — AWS Nova Sonic provider as a LiveKit participant. The output file owns paced framing, queue limits, pre-roll, underrun metrics; the mixer downsamples to 16 kHz mono PCM.
- **`nova_sonic_agenda.go`** — pushes the pre-meeting `internal/standup.Agenda` into Nova Sonic via the `sendInitSequence` userContext event (Sprint 4.1).
- **`kanban.go`** — OpenAI Realtime/WebRTC path, model-profile validation, event handling, tool-call dispatch.
- **`audio_mixer.go`** — 48 kHz stereo PCM mixer feeding OpenAI Realtime.
- **`opus_encoder.go`** / **`opus_decoder.go`** — CGo Opus wrappers used by browser and LiveKit audio paths.
- **`voice_models.go`** — host-selectable voice models, same-provider model changes, restart-required provider switches.
- **`voice_status.go`** — voice readiness preflight (OpenAI / Nova / AWS / LiveKit agent presence) and user-facing recovery guidance.
- **`aws_refresh.go`** — local-only proxy to the credential refresh broker started by `scripts/local-up.sh`.
- **`bedrock_agents.go`** — Bedrock Claude Messages invocation used by PM and specialist agents; satisfies the `agentModelClient` interface consumed by `internal/core.ModelProvider`.
- **`jira.go`** + **`jira_ext.go`** + **`jira_conflicts.go`** + **`jira_webhook.go`** — the existing Jira client. Sprint 3.0 wrapped this client behind `internal/projection/jira.Client` (`internal/projection/jira/projection.go:22-29`) without duplicating HTTP code; the existing methods already use `board.Card` and `board.Status`.
- **`github_app.go`** — GitHub App key parsing, JWTs, installation tokens, repo allowlisting, PR file reads, optional PR review comments.
- **`extensions.go`** — runtime registration into `extensionRuntimeState` (`cmd/server/extensions.go:15-122`): voice provider descriptors (nova-sonic, openai-realtime, openai-realtime-translate, openai-realtime-whisper, livekit-cloud), connectors (`local-board`, `jira`, `github`), the Bedrock model provider, and the Jira projection (registered only when the syncer client is configured, `cmd/server/extensions.go:111-119`).
- **`audit.go`** — optional JSONL audit log for board mutations and Jira refreshes.
- **`guardrails.go`** — prompt-injection redaction, model-safe board/tool-result views, mutating tool-argument rejection.
- **`cron.go`** — periodic background jobs (pending-action expiry, agenda pre-builds at Sprint 4.1).

The numerous `*_test.go` files (`board_test.go`, `agent_runs_test.go`, `agent_coordinator_persistence_test.go`, `tenant_isolation_test.go`, `pause_all_test.go`, `confirmation_gate_test.go`, `diff_preview_test.go`, `dry_run_queue_test.go`, `run_question_lifecycle_test.go`, `projection_replay_test.go`, `intake_handler_test.go`, `meeting_reports_test.go`, `chat_messages_test.go`, etc.) are colocated with the units they test. They are the canonical reference for tool-call shapes and HTTP response bodies.

### `cmd/mcpd` — the MCP server (Sprint 2.0)

`cmd/mcpd/main.go:35` is `func main()`. It accepts `--transport=stdio|http`, `--port`, `--board-id`, `--tenant-id`, `--board-url`. When `--board-url` is non-empty it uses `mcp.NewHTTPBoardClient` to route every tool call back to cmd/server's `/internal/tools/dispatch` (`cmd/mcpd/main.go:50-58`). When `--board-url` is empty it falls back to `mocks.NewBoardClient` and seeds a single onboarding card (`cmd/mcpd/main.go:60-65`, `:118-128`).

The mcpd binary wires:

- `mcp.BoardClient` — either the HTTP client (`internal/mcp/tools.go:449` for `NewHTTPBoardClient`) or the in-memory `mocks.BoardClient`.
- `agent.RunStore` — `mocks.NewRunStore()`. cmd/mcpd does not persist runs durably; production deployments wire MCP runs back through cmd/server's SQLite store via the HTTP board client.
- `agent.RunCoordinator` — `agent.NewSimpleRunCoordinator(runStore, nil)` (`cmd/mcpd/main.go:68`). See `internal/agent/simple_coordinator.go:24-54`.

`MCP_SIGNING_KEYS` is the symmetric secret (shared by cmd/server and mcpd) used to sign and verify MCP bearer tokens. cmd/server's `POST /admin/mcp-tokens` issues tokens with HMAC-SHA256 + ULID jti + scopes; cmd/mcpd's HTTP transport verifies them (alg pinned, jti replay-tracked). `BOARD_TOKEN` is the bearer the HTTP board client sends to cmd/server's `/internal/tools/dispatch` (equals `APP_API_TOKEN` in the standard deployment). The mcpd binary fails closed when `MCP_SIGNING_KEYS` is missing on the http transport (`cmd/mcpd/main.go`). The single-token gate was removed in #58.

---

## 3. `internal/`

### `internal/core` — provider-neutral contracts

This is the public extension surface. The package defines:

- `RiskLevel` (`internal/core/types.go:14-26`) — `low`, `medium`, `high`. Re-exported by `internal/meetings.RiskLow`/`Medium`/`High` (`internal/meetings/types.go:11-20`) so meetings and connectors share one risk vocabulary.
- `Evidence` (`internal/core/types.go:30-39`) — speech / transcript / external URL / system observation; carried on intents, tool calls, and connector results so the audit replay can paint the speech-to-API chain.
- `Confidence` (`internal/core/types.go:43-48`).
- `Connector`, `ConnectorCapability`, `ConnectorAction`, `ActionReceipt`, `ConnectorResult` (`internal/core/types.go:51-131`). `ConnectorRegistry` (`:134`) stores implementations by normalized lowercase name.
- `VoiceProvider`, `VoiceCapabilities`, `VoiceSession`, `VoiceSessionEvent` (`internal/core/types.go:196-262`) and `VoiceRegistry` (`:265`).
- `ModelProvider`, `ModelRequest`, `ModelResponse`, `ModelCapabilities` (`internal/core/types.go:317-357`) and `ModelRegistry` (`:360`).
- Audit substrate (NOT a `core` interface): every `ApplyToolCallWithMeta` writes a `boardEventRecord` to the SQLite `action_replay_events` table via `cmd/server/board_store.go`. `replay_audit_event` reconstructs the mutation from `audit_event_id`. The former `core.ActionLedger` interface was removed in #57 — it had zero non-test call sites; the SQLite table is the source of truth.

`internal/core/contracttest/contracts.go` provides `contracttest.Connector(t, connector, cases)` and `contracttest.VoiceProvider(t, provider, cases)` — shared test helpers any extension implementation can run to assert metadata, health, and execution behavior (`internal/core/contracttest/contracts.go:26-114`).

### `internal/agent` — Run orchestration contract

The contract package that the legacy in-process orchestrator (cmd/server) and the MCP-driven runtime both implement.

- **`types.go`** — `Run`, `RunStatus`, `Classification`, `CodeReviewFinding`, `Checkpoint`, `PlanStep`, `CostBreakdown`, `RunQuestion`, `RunQuestionRef`, `RunView` (`internal/agent/types.go:11-251`). The `RunStatus` constants cover the full lifecycle including `StatusWaitingOnHuman` (per-run ask-the-human pause) and `StatusPaused` (tenant-wide kill switch) — note the explicit "these are different" warning at `internal/agent/types.go:18-46`.
- **`coordinator.go`** — `RunCoordinator` interface with `Start`, `Checkpoint`, `AskHuman`, `Resume`, `Cancel` (`internal/agent/coordinator.go:48-54`); `RunRequest` and `HumanAnswer` input shapes.
- **`store.go`** — `RunStore` interface (`internal/agent/store.go:54-99`) with the persistence primitives (`SaveRun`, `LoadRun`, `AppendRunCheckpoint`, `ListRunCheckpoints`, `SaveRunQuestion`, `LoadRunQuestion`, `ListOpenRunQuestions`, `MarkRunQuestionAnswered`, `ExpireRunQuestions`). Defines `ErrRunNotFound`, `ErrRunQuestionNotFound`, `ErrRunQuestionExpired`, `ErrAgentsPaused`, `ErrCheckpointAuditFailed` (lines 13-44).
- **`id.go`** — `NewQuestionID()` / `NewRunID()` mint Crockford-base32 ULIDs with monotonic entropy (`internal/agent/id.go:24-38`). The ULID timestamp keeps `asked_at`-ordered listings lexically stable.
- **`run.go`** — `Run.AddCheckpoint` (caps the slice at 50 entries) and `Run.View()` (`internal/agent/run.go:6-67`) which deep-copies into the client-safe `RunView`.
- **`checkpoint.go`** — `RunStepCheckpoint` (durable audit log entry, distinct from the UI `Checkpoint`) and the four kinds: started, completed, paused, failed (`internal/agent/checkpoint.go:30-35`).
- **`simple_coordinator.go`** — `SimpleRunCoordinator` (`internal/agent/simple_coordinator.go:24-54`). Reference implementation used by `cmd/mcpd` and by tests; owns state transitions but not plan generation, model calls, or external publish. The `scope` map (`:34`) records `(tenantID, boardID)` per `run_id` so `Checkpoint`/`AskHuman`/`Cancel` can resolve scope without re-passing it through every call.
- **`util.go`** — `nowRFC3339Nano`, `truncateString`, `ApplyCheckpointToPlan` (the latter is the helper that translates a `RunStepCheckpoint` into a `Plan[step].Status` change).

### `internal/board` — pure board domain types

`internal/board/types.go` defines `Card`, `Actor` (with `Kind: human|agent`), `User`, `Comment`, `Estimate`, `Sprint`, `IssueLink`, `Worklog`, `RemoteLink`, `Field`, and the four canonical statuses (`StatusBacklog`, `StatusInProgress`, `StatusBlocked`, `StatusDone` at lines 22-27). `cmd/server/board.go:20-46` aliases `kanbanCard`, `kanbanStatus`, etc. to these types so the JSON shape is shared.

The package exports two invariants worth highlighting:

- `ErrInvalidActor` (`internal/board/types.go:15`) — a human-kind Actor must carry at least one of `ID`, `DisplayName`, or `Email`. Closes SE-1 F1: a previous shape allowed `{}` to deserialize into a fabricated human assignee.
- `Actor.UnmarshalJSON` accepts both the canonical `Actor` shape and the legacy `User` shape (`internal/board/types.go:105-138`) so pre-v2 snapshots load without a migration pass.

### `internal/projection` — outbound-write + reconciliation contract

`internal/projection/projection.go` defines:

- `Projection` interface (`:15-22`) — `Name`, `Capabilities`, `Project`, `Reconcile`, `ResolveConflict`, `Health`.
- `Capabilities` (`:25-31`) — five booleans: `SupportsCreate`, `SupportsUpdate`, `SupportsDelete`, `SupportsWebhook`, `BiDirectional`.
- `BoardDelta` (`:34-39`) — `Changed []board.Card` + `Deleted []string`. The unit of outbound projection.
- `Conflict` + `Resolution` (`:42-53`) and the four canonical resolution strategies: `keep-local`, `keep-remote`, `merge`, `ask-user` (`:56-61`).
- `Health` (`:64-70`).

`internal/projection/registry.go` is the `Registry` (`:12-15`) — same shape as the other registries in `internal/core`.

`internal/projection/contracttest/projection.go` runs the contract against a `Factory` (`:15-94`): asserts the name is lowercase, capabilities are callable, `Project(empty)` is accepted, `Project(delta)` accepts both changed and deleted, `Reconcile` returns `[]board.Card`, `ResolveConflict` returns a known strategy, and merge strategies require a non-nil `Merged` card.

#### `internal/projection/jira` — the canonical example

`internal/projection/jira/projection.go` adapts the existing cmd/server Jira client to the Projection contract:

- `Client` interface (`:22-29`) — six methods (`SearchKanbanCards`, `CreateIssue`, `UpdateIssue`, `CloseIssue`, `TransitionIssue`, `AssignIssue`). cmd/server's `*jiraClient` satisfies this via duck typing without an adapter shim.
- `Config` (`:32-36`) — base URL, project key, email.
- `JiraProjection` (`:39`) — wraps client + config and a clock injector for tests.
- `Capabilities` returns all five flags true (`:54-58`).
- `Project` upserts every Changed card and `CloseIssue`s every Deleted ID (`:62-79`). Agents-as-assignees fall back to an empty Jira assignee (`:104-107`) — agent identity does not exist in Jira's user model.
- `ResolveConflict` returns `keep-remote` (`:119-121`) — the default Jira policy.
- `Health` returns `not_configured` until the client is wired (`:124-138`).

### `internal/mcp` — JSON-RPC server + tool definitions

A minimal MCP slice — `Sprint 2.0 status`: tools only; capabilities/prompts/resources land in 2.1.

- **`server.go`** — JSON-RPC 2.0 envelope, error codes (`:29-35`), `Tool` and `ToolHandler` (`:70-78`), `Server` with stdio + HTTP transports (`:82-103`). `HandleRequest` dispatches `initialize`, `tools/list`, `tools/call` (`:147-235`). `ServeStdio` newline-delimits frames; `HTTPHandler` validates the Authorization header and accepts JSON-RPC POSTs (`:243-309`).
- **`auth.go`** — constant-time bearer check (`internal/mcp/auth.go:16-33`). Empty expected token is permissive (used in tests and stdio mode).
- **`tools.go`** — the production tool surface. `BoardClient` interface (`:32-38`) is what the tool handlers depend on. `CardFilter`, `CardCreate`, `CardPatch`, `CardSummary`, `CardDetail`, `RunSummary`, `CommentResult` (`:43-110`) are the input/output shapes. `ToolDeps` (`:116`) is the dependency bundle. `BuildTools` (`:132`) returns the five MCP tools: `board.list_cards`, `board.get_card`, `card.create`, `card.update`, `card.comment` (`:144-374`). `HTTPBoardClient` (`:449-712`) is the production adapter that POSTs to `cmd/server/internal_dispatch.go` — it fans every card mutation back into `ApplyToolCall` so MCP-driven writes go through the same audit ledger as voice/UI writes.

### `internal/intake` — async standup parsing + storage

- **`types.go`** — `Source` constants (`SourceForm`, `SourceSlack`, `SourceAPI` at `:14-19`), `BlockerItem`, `Intake` (`:49-60`), and `Normalize` (`:70-124`) which trims, deduplicates `MentionedCards`, stamps `SubmittedAt`, and enforces the minimum-content invariants (`ErrEmptyIntake`, `ErrMissingSubmitter` at `:24-30`).
- **`parser.go`** — `Parser` interface and `HeuristicParser` that turns free-form text into yesterday/today/blockers/mentioned-cards.
- **`store.go`** — `Store` interface and `MemoryStore`. `NewMemoryStore(capacity)` is used by `cmd/server/intake_handler.go:24`.
- **`slack.go`** — Slack webhook signature verification helpers used by `cmd/server/intake_slack.go`.

### `internal/standup` — agenda + closer

- **`agenda.go`** — `Agenda`, `AgendaHighlight`, `AgendaBlocker`, `AgendaRun`, `AgendaQuestion` (`:21-90` ish) and `BuildAgenda(ctx, sources, window)`. Read by `cmd/server/nova_sonic_agenda.go` (`sendInitSequence` userContext) and by the React drawer's collapsible groups so silent meeting hosts have parity with voice.
- **`closer.go`** — `Closer` with `Cards CardCreator`, `Runs agent.RunCoordinator`, `Sink ArtifactSink` (`internal/standup/closer.go:18-22`). `Closer.Close` iterates `FollowUps + UnresolvedBlockers`, creates Blocked-column cards for entries with no existing card pointer, then calls `Runs.Start` for the subset whose assignees are agents. The cmd/server adapter is at `cmd/server/standup_adapter.go` + `cmd/server/standup_closer.go`.

### `internal/meetings` — meeting domain types

`internal/meetings/types.go` re-exports `core.RiskLevel` (`:11-20`) so the meeting layer, governance, and connectors share one value space. Defines `Mode` (`:22-38`), `Participant` (`:43-49`), `ParticipantUpdate` (`:53-67`), `FollowUp` (`:72-80`), and the rest of the post-meeting report shape. Currently consumed by `cmd/server/meeting_intelligence.go`, `cmd/server/meeting_reports.go`, and `internal/standup`.

### `internal/mocks` — provider-free test doubles

- **`boardclient.go`** — in-memory `mcp.BoardClient` for tests and the `cmd/mcpd` fallback when `BOARD_URL` is empty (`internal/mocks/boardclient.go:21-45`).
- **`runstore.go`** — in-memory `agent.RunStore`. A compile-time `var _ agent.RunStore = (*RunStore)(nil)` (`internal/mocks/runstore.go:44`) keeps the mock in lockstep with the production contract.
- **`connector.go`**, **`model.go`**, **`voice.go`** — no-credential implementations of the three core extension contracts.

### `internal/board`, `internal/projection`, `internal/agent`, `internal/intake`, `internal/standup`, `internal/meetings`, `internal/mocks`, `internal/core`, `internal/mcp`

All eight packages have been described above.

### Skeleton packages (Sprint 0 stubs)

These directories contain only a `doc.go` documenting where the code *will* live. They are reserved namespaces — `cmd/server` still owns the behaviour today, and a later sprint will move the implementation here:

| Package | Doc claim | Where the code lives today |
| --- | --- | --- |
| `internal/auth` | "Owns session identity, request authentication, requestAuthContext. Sprint 0 status: skeleton. cmd/server/auth.go moves here in Sprint 0.4 together with the TenantID threading." (`internal/auth/doc.go`) | `cmd/server/auth.go` |
| `internal/cost` | "Meters per-meeting and per-run cost across Bedrock token usage, LiveKit + Nova Sonic audio seconds. Sprint 0 status: skeleton. The cost meter lands in Sprint 5." (`internal/cost/doc.go`) | inline in `cmd/server/agent_runs.go` (`reserveAgentRunCost` at `:705`) and `agent.CostBreakdown` |
| `internal/extensions` | "Owns runtime registries. Sprint 0 status: skeleton. cmd/server/extensions.go moves here once the underlying types stabilize." (`internal/extensions/doc.go`) | `cmd/server/extensions.go` |
| `internal/http` | "Will host HTTP handlers extracted from cmd/server. Sprint 0 status: skeleton." (`internal/http/doc.go`) | `cmd/server/main.go` + per-feature `*_handler.go` files |
| `internal/httpapi` | "Hosts shared HTTP middleware (tenant resolution, authentication, audit). Sprint 0 status: skeleton." (`internal/httpapi/doc.go`) | `cmd/server/auth.go` |
| `internal/tenant` | "Owns tenant identity, per-tenant secret storage. Sprint 0 status: skeleton. Default tenant ID 'default' is used everywhere until the hosted control plane lands." (`internal/tenant/doc.go`) | the `default` literal threaded through `cmd/server/board.go`, `cmd/server/tenant_settings.go`, `cmd/server/agent_runs.go` |
| `internal/voice` | "Owns the voice-meeting runtime. Sprint 0 status: skeleton. cmd/server/nova_sonic*.go and scrum_tools.go migrate in a later sprint." (`internal/voice/doc.go`) | `cmd/server/nova_sonic*.go`, `cmd/server/kanban.go`, `cmd/server/scrum_tools.go` |

Note: the Jira projection lives at `internal/projection/jira/projection.go`. An earlier `internal/jira/` placeholder package was removed once the projection landed in its final home — there is no `internal/jira` namespace.

---

## 4. `web/`

Two browser tiers, served by the same Go binary:

- **`web/index_livekit.html`** — the legacy LiveKit / Nova Sonic meeting UI. Host/participant access flow, video layouts, operator panel, voice reliability dashboard, board rendering, audit replay, model settings. Served at `/` (`cmd/server/main.go:261`).
- **`web/index.html`** — the OpenAI Realtime / Pion WebRTC UI used when `VOICE_PROVIDER=openai`.
- **`web/post_meeting.html`** — post-meeting intelligence dashboard and archived report viewer.
- **`web/app/`** — the React + Vite SPA, served at `/app/*` (`cmd/server/main.go:258`). The Go server uses `http.FileServer(http.Dir("web/app/dist"))`; if the SPA has not been built yet, `/app/` returns 404 cleanly. `web/app/package.json` pins React 18.3.1, Vite 6.0.7, Tailwind 3.4.17, Vitest 2.1.8, `livekit-client` 2.7.5.

The React SPA layout:

```
web/app/src/
  App.tsx                — root component; reads ?card= from the URL to mount the drawer.
  main.tsx               — Vite entry; wraps App with the QueryClient provider.
  index.css              — Tailwind base + Observatory Deck dark mode tokens.
  lib/useBoardSocket.ts  — WebSocket connection hook; exposes BoardSocketState + dispatch.
  types/board.ts         — TypeScript view of the JSON shapes (Card, AgentRunView, etc.).
  components/
    BoardHeader.tsx      — "Observatory" wordmark + connection pill (web/app/src/components/BoardHeader.tsx:56).
    BoardSubBar.tsx      — meeting-mode chips + PauseAllPill mount.
    BoardColumn.tsx      — one kanban column; renders the Card list.
    Card.tsx             — the card chip in the column.
    CardDrawer.tsx       — sliding drawer with Run / Thread / Diff tabs.
    CardRunTab.tsx       — run timeline, plan steps, question banner; answer / take-over / cancel / retry.
    CardThreadTab.tsx    — comments + composer.
    RunQuestionBanner.tsx — solar-copper banner for ask-the-human pauses (web/app/src/components/RunQuestionBanner.tsx:12).
    SuggestedAnswerChip.tsx — the recommended-answer chips on the question banner.
    DryRunQueue.tsx      — pending-action list + diff preview.
    DiffPreview.tsx      — before/after card-diff renderer.
    PauseAllPill.tsx     — agents_paused toggle; magnetar (#FF3D7F) when on (web/app/src/components/PauseAllPill.tsx:9-22).
    IntakeForm.tsx       — async standup form posted to /intake/standup.
    SignInGate.tsx       — local-only sign-in screen behind APP_AUTH_MODE=token.
    ConnectionPill.tsx   — WebSocket status indicator.
    EmptyState.tsx       — "no cards yet" copy.
```

Design tokens are defined in `web/app/tailwind.config.js`: `void`/`sky`/`atmos`/`edge` for the dark surfaces, `star`/`twilight`/`farstar` for text, and `aurora`/`solar`/`magnetar`/`comet` for accents.

---

## 5. `scripts/`

| Script | Purpose |
| --- | --- |
| `local-up.sh` | One-command local start: Keychain secrets, AWS assume flow, Docker Compose, local login, browser open. |
| `local-compose.sh`, `local-down.sh` | Docker Compose wrappers that preserve the local secret handling. |
| `dc-up-keychain.sh`, `run-openai-keychain.sh` | Provider-specific local launchers using macOS Keychain. |
| `local-aws-refresh-broker.{py,sh}` | Local-only restart broker for expired AWS speech credentials. |
| `keychain-get-secret.sh`, `keychain-store-secret.sh` | Keychain shim used by the launchers and `validate-golden-demo.sh`. |
| `aws-build-push.sh`, `aws-deploy-dev.sh`, `aws-upsert-secrets.sh` | AWS image build/push, dev deploy, and Secrets Manager upsert. |
| `check-import-boundaries.sh` | Enforces the layering rule (internal/* cannot depend on cmd/*). |
| `check-go-dependencies.sh`, `check-github-actions-pinning.sh` | Dependency hygiene and Actions pinning gates. |
| `install-hooks.sh`, `pre-commit` | Installs and runs the pre-commit hook (gofmt, govet, eslint, etc.). |
| `validate-golden-demo.sh` | Real-stack preflight gate for the golden demo path. |
| `jira-check-board-config.sh`, `jira-validate-workflow-config.sh` | Pre-flight checks for Jira board + workflow shape. |
| `livekit-multiperson-proof.sh`, `github-app-setup.sh` | One-off proofs and setup helpers. |

---

## 6. `infra/`

Terragrunt live stack (`infra/live/`) + reusable Terraform modules (`infra/modules/`). Targets ECS/Fargate, ALB/WAF, LiveKit, Redis, EFS, Secrets Manager, VPC endpoints, and dashboards. v2 did not touch infra; the stack still maps onto cmd/server as a single binary.

---

## 7. `evaluation/` and `examples/`

`evaluation/` is the fixture-backed validation harness (`evaluation/evaluation_harness_test.go`) for meeting behavior, Jira safety, voice reliability, post-meeting intelligence, agent runs, AWS/LiveKit hardening, and extension contracts. `evaluation/failure-inventory.md` is the seed replay inventory for ambiguous voice-command failures. `evaluation/aws-livekit-hardening-proof.md` and `evaluation/multi-participant-validation.md` are signed-off proofs for security-sensitive deploys.

`examples/connector-template/`, `examples/voice-provider-template/`, `examples/model-provider-template/` each carry a `*.go.tmpl` and a README. They are the starting point for new extensions and are referenced from `docs/extension-contracts.md`.

---

## Change Checklist

When changing behaviour, update the docs nearest to the behaviour:

1. **Public setup or runtime change** → `README.md` and the relevant Keychain / Secrets Manager scripts or docs.
2. **HTTP route change** → `docs/api/openapi.yaml` and the README endpoint table when user-facing.
3. **Extension contract change** → `internal/core` (or `internal/agent` / `internal/projection` / `internal/mcp`) Go docs, `docs/extension-contracts.md`, templates, and contract tests.
4. **Provider or connector behaviour change** → relevant `examples/` README, evaluation fixture, and `extensions.go` registration.
5. **Trust ceremony surface (dry-run, kill switch, pending actions, diff preview)** → the dry-run owner files (`cmd/server/dry_run.go`, `cmd/server/dry_run_http.go`, `cmd/server/pending_actions.go`, `cmd/server/diff_preview.go`, `cmd/server/tenant_settings.go`) and the React `DryRunQueue` + `PauseAllPill`.
6. **Infrastructure input or security posture** → `infra/README.md`, module README, `SECURITY.md`, and the threat model when the trust boundary changes.
