# Living Kanban Board Progress

Last updated: 2026-05-19

## Current Rule

Do not commit or push until the current feature set has been locally validated and Scott explicitly asks for the git action.

## Implemented In This Pass

- Added platform/open-source readiness scaffolding:
  - `internal/core` extension contracts for voice providers, connectors, model providers, and action-ledger replay.
  - `internal/mocks` plus reusable contract tests so integrations can be developed without live AWS/Jira/GitHub credentials.
  - Runtime descriptors for Nova Sonic, OpenAI Realtime, LiveKit Cloud, local board actions, Jira, GitHub, and Bedrock.
  - SQLite-backed action replay ledger persistence; recent replay records reload on restart when `BOARD_SQLITE_PATH` is configured.
  - Workspace scope endpoint for the current single-workspace deployment model and future tenant isolation shape.
  - `docs/architecture.md`, `CONTRIBUTING.md`, ADR 0001, extension contract docs, OpenAPI docs, threat model, security policy, code of conduct, integration templates, and Cursor extension rules.
  - GitHub Actions CI/dependency-review workflows with official actions pinned to immutable SHAs.
  - Real-stack golden demo and LiveKit multi-person proof scripts; no product mock runtime path.
  - `scripts/check-import-boundaries.sh`, `scripts/check-go-dependencies.sh`, `.pre-commit-config.yaml`, and `Makefile` contributor targets.
  - Evaluation fixture coverage for extension contracts and quality gates.
- Added board freshness tracking with `sequenceNumber` and `updatedAt` in every board snapshot.
- Added provider-agnostic `get_board` tool that returns cards, timestamp, and `sequence_number`.
- Added board unit tests for freshness, mutation sequence increments, context JSON, and tool definitions.
- Added `evaluation/failure-inventory.md` as the seed replay inventory for ambiguous voice-command failures.
- Added optional Jira Cloud REST API v3 sync:
  - Loads a board from Jira on startup when `JIRA_CONFIG_PATH` or `JIRA_CONFIG_JSON` is set.
  - Maps Jira issue key to `card.ID`.
  - Maps Jira status, summary, description, labels, assignee, due date/ETA, priority, comments, and optional Flagged/Impediment metadata to Kanban cards.
  - Writes local mutations back to Jira for create, move, update, append notes, add comments, add/remove tags, assign/unassign, due date/ETA, priority, blocked flag, and delete/close.
  - Renames locally created cards to the Jira issue key after Jira issue creation.
  - Supports polling refresh with `poll_interval_seconds`.
  - Supports JSON workflow config with status mappings, transition IDs, required transition fields, and delete transition.
  - Supports real Jira `Blocked` workflow transitions plus optional `blocked_flag_field` / `blocked_flag_value` fallback for Jira Software Flagged/Impediment.
  - Enforces the configured `project_key` as a safety boundary for all existing-issue Jira writes and startup hydration.
  - Supports API token from file, command, env var, or inline config, with file/command preferred.
  - Uses Jira's current `/rest/api/3/search/jql` endpoint for board hydration.
- Expanded voice tool surface for human-like Jira task operations:
  - `search_jira_users`, `assign_ticket`, `unassign_ticket`
  - `append_notes`, `add_comment`
  - `set_eta`, `list_priorities`, `set_priority`
  - `remove_tags`, `set_blocked`
  - `create_subtask`, `set_story_points`, `set_estimate`, `add_worklog`, `link_issues`
  - `set_sprint`, `prioritize_ticket`, `rank_issue`, `set_components`, `set_fix_versions`, `set_custom_field`
  - `add_remote_link`, `set_reporter`, `add_watcher`
- Added structured scrum-master meeting state and tools:
  - `start_meeting`, `register_participant`, `record_participant_update`
  - `next_speaker`, `summarize_meeting`, `end_meeting`
- Added host/participant meeting access:
  - Hosts create an eight-character random meeting code through `POST /meeting/setup`.
  - Participants join through `POST /meeting/join`; the server accepts display codes with separators, rejects wrong codes, sets the HttpOnly session cookie, and lets joined participants mint LiveKit tokens without the app access token.
  - Hosts can set and switch meeting type across general meeting, standup, 1:1, sprint review, and open-ended modes through `/meeting/type`, the UI, or the `switch_meeting_type` tool.
- Added meeting intelligence:
  - Meeting memory now tracks agenda, decisions, risks, action items, parking-lot topics, follow-ups, unresolved blockers, and ownership.
  - Meeting start generates a 60-second scrum-master briefing from recent board movement, ready-PR signals, blocked work, unassigned work, stale cards, and unresolved blockers.
  - Added `record_meeting_memory` and `generate_scrum_briefing` tools.
- Added selective confirmation gates for risky Jira actions:
  - Medium-risk actions: assignment/unassignment, ETA, priority, and reporter changes.
  - High-risk actions: close/delete, sprint assignment, and Jira ranking.
  - Added `confirm_action`, `cancel_confirmation`, and `list_pending_confirmations` tools plus UI confirmation prompts.
- Added undo and audit replay:
  - Voice/UI mutations keep bounded before/after mutation history with transcript evidence when provider transcripts are available.
  - Replay now includes the selected tool, confidence/guardrail decision, and external API confirmation so the UI can explain: speech -> tool -> Jira/GitHub result.
  - Jira-backed tool results separate local board mutation from Jira API confirmation, and failed/unconfigured write-through returns explicit assistant instructions not to claim Jira success.
  - Added `undo_last_mutation`, `get_audit_events`, and `replay_audit_event` tools.
  - Added Undo and Audit controls to both web clients.
- Added authenticated Jira webhook refresh and conflict resolution:
  - `POST /jira/webhook` accepts `Authorization: Bearer <secret>` or `X-Auto-Bot-Jira-Webhook-Secret`.
  - Webhook payload issue keys are project-key checked before refresh.
  - Poll/webhook refreshes surface local-vs-Jira conflicts when meeting-local changes would be overwritten.
  - Added `resolve_jira_conflict` tool and UI conflict prompts.
- Updated both web UIs to display real assignee, priority, ETA, blocked reason, and latest comments, and aligned card detail open/close events across OpenAI and LiveKit frontends.
- Added `config/jira.example.json`.
- Added Jira tests using an `httptest` fake Jira server.
- Added structured audit events for board mutations and Jira refreshes, with optional JSONL output via `AUDIT_LOG_PATH`.
- Added fixed-window rate limiting for WebSocket upgrades and LiveKit token minting.
- Replaced browser-visible app-token auth with an HttpOnly session flow:
  - Served HTML no longer contains `APP_API_TOKEN` or `window.__APP_TOKEN__`.
  - `/auth/session` creates and checks server-side browser sessions.
  - `/websocket` and `/livekit-token` require a session cookie or non-browser Bearer token.
  - Query-string tokens are rejected.
- Added explicit room/board/participant session metadata:
  - `APP_ROOM_ID` and `APP_BOARD_ID` define the only authorized room/board for the deployment.
  - LiveKit JWT grants are scoped to that room.
  - WebSocket clients are registered by board ID and broadcasts are board-scoped.
- Added LiveKit production secret safety:
  - `APP_ENV=production` rejects disabled auth and rejects the `devkey`/`secret` LiveKit development pair.
  - Docker Compose is explicitly marked `APP_ENV=local`.
- Added optional SQLite board persistence and event history via `BOARD_SQLITE_PATH`.
  - Docker Compose persists `/srv/data/board.sqlite` in a named volume.
  - AWS Fargate mounts `/srv/data` from EFS for the app task.
- Added prompt-injection guardrails:
  - Treat Jira/card titles, notes, comments, tags, assignee names, due dates, priorities, and board tool outputs as untrusted data.
  - Redact detected prompt-injection payloads from model-facing board context and tool results while preserving raw data for Jira/UI.
  - Reject prompt-injection-like text in mutating tool arguments before any board/Jira write.
  - Update provider instructions and tool schemas so only live user speech can authorize mutations.
- Fixed Nova Sonic stream lifecycle basics:
  - Audio packets can restart Bedrock after a stream ends.
  - Per-stream audio forwarding goroutine stops when that stream ends.
  - Bedrock stream renews before the expected session limit.
  - Nova Sonic receives a sanitized full-board context refresh after successful board mutations.
  - Board-context refreshes are sent as non-interactive user/application data instead of a second `SYSTEM` block, avoiding Bedrock's duplicate-system validation failure.
  - The Bedrock audio input stream sends periodic silent frames during meeting pauses to avoid idle input timeouts while the agent is still present.
  - Nova Sonic output audio now publishes to LiveKit through a bounded real-time pacer: one 20 ms Opus frame every 20 ms, 80 ms pre-roll, 120 ms short-utterance timeout, queue cap/drop protection, and output queue/drop/underrun/pps/jitter health metrics.
  - Browser join now runs a Nova Sonic readiness preflight before LiveKit entry: authenticated `/voice/status` validates AWS credentials with STS in `us-east-1`, ensures the server-side Nova participant is connected, and `/livekit-token` refuses room tokens while that preflight is failing.
- Added macOS Keychain-backed local automation:
  - `scripts/local-up.sh` provisions missing local app/login/webhook secrets in Keychain, prompts once for the Jira token when missing, runs `assume test_AccountA/AdministratorAccess` in `us-east-1`, starts Docker Compose, and opens a local-only login URL.
  - `scripts/local-compose.sh`, `scripts/local-down.sh`, and `scripts/dc-up-keychain.sh` keep Docker commands Keychain-backed without a local `.env`.
  - `/auth/local-login` creates the browser session only when `APP_ENV=local`; production rejects `APP_LOCAL_LOGIN_TOKEN`.
- Added validation/evaluation scaffolding under `evaluation/`:
  - Fixture-driven Go tests cover host/participant generated-code access, meeting type switching, 2-4 participant meeting behaviors, risky Jira confirmations, prompt-injection no-op behavior, owner/ETA/blocker extraction, executive recap expectations, voice reliability signals, and beautiful failure states.
  - Added a synthetic multi-participant audio timing manifest for interruption, overlap, silence, reconnect, and late-join validation without committing binary audio.
  - Added an AWS LiveKit hardening proof checklist for UDP/TURN, reconnect, CloudWatch alarms, and the LiveKit Cloud Terraform switch.
- Added LiveKit meeting operator UI:
  - Meeting Control Center tracks who has spoken, who has not spoken, blockers, decisions, action items, pending confirmations, and Jira mutations.
  - Voice Reliability Dashboard shows mic, LiveKit, Nova Sonic, Bedrock stream, participant audio reaching the agent, paced agent audio output, transcription, Jira, and agent-participant health.
  - Agent Confidence UI explains matched cards, risk/confirmation reasons, and prompt-injection guardrail decisions.
  - Executive recap output includes Slack-ready summary sections for Jira changes, blockers, action items by owner, unresolved questions, and changes since meeting start.
  - Beautiful failure states classify missing AWS credentials, missing agent participant, blocked mic permission, Jira scope rejections, LiveKit failures, and audio playback issues.
- Added durable post-meeting intelligence:
  - `GET /meeting/intelligence` returns the current meeting report with agenda, participants, decisions, risks, action items, parking lot, follow-ups, blockers, ownership, Jira changes, transcript evidence, sprint intelligence, GitHub/PR hints, setup readiness, observability, and Slack-ready recap text.
  - Ending a meeting archives the report to SQLite when `BOARD_SQLITE_PATH` is enabled.
  - `GET /meetings` lists archived report summaries, and `GET /meeting/intelligence?meeting_id=<id>` reloads an archived report.
  - `web/post_meeting.html` provides the post-meeting intelligence page with report selection, Jira mutation timeline, sprint signals, GitHub/PR context, setup checks, observability, transcript evidence, and copyable Slack recap.
  - Added admin/status endpoints for `/setup/status`, `/observability/status`, `/voice/providers`, and `/identity/status`.
- Updated README and `.env.example` for Jira, audit, and rate-limit proxy behavior.
- Added Terragrunt/Terraform AWS deployment shape:
  - Root Terragrunt config generates S3 state, DynamoDB locking, and AWS provider config in `us-east-1`.
  - AWS provider is pinned to `hashicorp/aws = 6.45.0`; `hashicorp/cloudinit = 2.4.0` is pinned for the fck-nat module.
  - Reusable module deploys ECS Fargate app service, optional ECS Fargate self-hosted LiveKit service, ALB, NLB, ECR, CloudWatch logs/dashboard, Secrets Manager injection, EFS board persistence, WAF, VPC endpoints, fck-nat private egress, ElastiCache Redis for LiveKit, and narrowed Bedrock task-role permissions.
  - VPC uses AWS-canonical `10.20.0.0/16` for the requested `10.20.21.0/16` range; public subnets start at `10.20.21.0/24`, and ECS/EFS live in private subnets.
  - AWS NAT Gateway is intentionally not used; fck-nat module `RaJiska/fck-nat/aws = 1.4.0` is pinned and full deploys require explicit `FCK_NAT_AMI_ID`.
  - Full deploys require an explicit pushed `APP_IMAGE`; the ECR `:bootstrap` fallback is only for initial targeted repository creation.
  - ECS task execution policy is inline/resource-scoped instead of using the broad AWS managed execution policy.
  - LiveKit Cloud is a Terraform bit flip with `LIVEKIT_DEPLOYMENT_MODE=cloud` plus `LIVEKIT_CLOUD_URL`; self-hosted resources are skipped in that mode.
  - ECR app image tags are immutable, app images cannot use `:latest`, Docker/Terraform/Terragrunt helper images are pinned by version and digest, and pre-commit checks forbid `:latest` / `@latest` in operational files.
  - Self-hosted LiveKit is modeled with TCP/TLS signaling, TCP fallback, one muxed UDP media port, embedded TURN/UDP, optional TURN/TLS, Redis distributed routing, and Prometheus metrics for the Fargate path.
  - Added AWS helper scripts for secrets, image push, and dev deploy.
  - Added Cursor rule for Terraform/Terragrunt conventions.
  - Updated pre-commit checks to use pinned Dockerized Terraform/Terragrunt format checks when local binaries are absent.

## Phase Status

| Phase | Status | What is built | What is not done yet |
| --- | --- | --- | --- |
| Prove the Product | Partial | Plan has meeting-hour targets. | No automated meeting-hour tracking or dashboard. |
| Phase 1 - OpenAI Realtime Baseline | Partial | OpenAI provider path, shared board tools, audio mixer, local quickstart, board regression tests, and seed failure inventory exist. | Manual voice validation, two-tab audio validation evidence, fork confirmation, and live replay results are not complete. |
| Phase 2 - Jira Sync | Partial | Jira config/client/startup hydration/write-through/polling/webhook foundation is implemented. `get_board` freshness contract is implemented. Voice tools now cover assignment, reporter/watchers, notes, comments, ETA/due date, priority, tag removal, subtasks, story points, estimates, worklogs, issue links, sprint assignment, above/below prioritization in any Kanban column, ranking, components, fix versions, custom fields, remote links, real Blocked workflow transitions, blocked flag fallback, metadata/transition discovery, project-key write safety, confirmation gates, undo, audit replay with external API confirmation evidence, and conflict resolution. Live read hydration and basic write-through passed against `EMAL`. | Live webhook delivery from Atlassian and live conflict drills are not complete. |
| Phase 2.5 - Workflow Config | Partial | JSON config supports status mappings, transition IDs, required fields, delete transition, polling, advanced field IDs, custom field mappings, metadata discovery, and transition option discovery. | Needs validation against three real workflows and a published known-limitations matrix. |
| Phase 3 - Nova Sonic 2 via LiveKit | Partial | Provider selection, LiveKit media path, Nova Sonic Bedrock path, tool handling, transcription broadcast, transcript evidence capture, stream lifecycle renewal foundation, duplicate-system-safe post-mutation board-context refresh, silence keepalive, authenticated voice readiness preflight, host-code meeting access, meeting-type switching, operator control center, reliability dashboard with participant-audio diagnostics, confidence UI, executive recap, durable post-meeting intelligence page/API/archive, and evaluation fixtures for multi-participant/reconnect/silence/overlap cases exist. | LiveKit data-channel board events, full 8-minute renewal proof, VAD calibration, A/B provider comparison, and real end-to-end multi-participant voice tests are not complete. |
| Phase 4 - Agent-First Task Execution | Partial | Voice/Jira tool `assign_ticket_to_agent` creates durable agent runs, persists them in SQLite, surfaces them in the live Meeting Settings drawer and post-meeting intelligence page, uses a Bedrock project-manager classifier through the US Claude Haiku inference profile, routes normal code-review and security-review requests to a Bedrock Sonnet 4.6 specialist through the US inference profile, fetches PR diffs through short-lived GitHub App installation tokens, posts findings back to Jira, and can optionally post PR review comments with inline file/line guidance when `GITHUB_PR_COMMENTS_ENABLED=true`. Runs fail visibly if the Bedrock client is unavailable, and Jira/PR publish warnings remain visible when external APIs reject writeback; there is no direct Anthropic API path. Live smoke tests passed against Jira issues `EMAL-22` and `EMAL-23` and GitHub PR `somoore/auto-bot#1`; the security-review path passed against Jira issue `EMAL-24` and GitHub PR `somoore/auto-bot#2`; `EMAL-23` validated the cost-balanced Haiku + Sonnet 4.6 pairing. | Cost caps, cancellation/takeover/retry controls, sandboxed code execution, automated code-change PR creation, non-code-review specialists, and metrics are not complete. |
| Phase 5 - Auth, Hardening, AWS Deployment | Partial | Docker, non-root runtime, origin checks, headers, timeouts, read limits, max clients, HttpOnly session auth, local-only Keychain login bootstrap, board/room request authorization, rate limits, audit JSONL, SQLite board event history, archived meeting reports, Jira webhook secret injection, Terragrunt remote state, private-subnet ECS/Fargate app and optional self-hosted LiveKit services, ALB with WAF, LiveKit NLB, ECR immutable tags, CloudWatch logs/dashboard, Secrets Manager wiring, private VPC endpoints, fck-nat private egress, ElastiCache Redis for LiveKit routing, embedded TURN/UDP, optional TURN/TLS, EFS board persistence with IAM access-point auth, LiveKit Cloud mode switch, and narrowed Bedrock task-role permissions exist. | OIDC/Cognito auth, true multi-room agent orchestration, CloudWatch alarms, AWS-applied DNS/certificate validation, pinned fck-nat AMI selection for the target account, and validated LiveKit self-hosting are not complete. |

## Jira Setup Needed From The New Account

Create a local copy of `config/jira.example.json`, then fill in:

- `base_url`: your Jira site URL, for example `https://your-site.atlassian.net`. For scoped API tokens, use the Atlassian API gateway URL, for example `https://api.atlassian.com/ex/jira/<cloud-id>`.
- `email`: the Atlassian account email for the API token.
- `api_token_file`: an absolute path to a local file containing the Jira API token.
- `project_key`: the Jira project key, for example `KAN`.
- `issue_type`: usually `Task` for first testing.
- `jql`: a small filter for the first board, for example `project = KAN ORDER BY updated DESC`.
- `status_mappings`: exact Jira status names mapped to `Backlog`, `In Progress`, `Blocked`, or `Done`.
- `transitions`: Jira transition IDs for each target Kanban status.
- `delete_transition`: transition ID for cancelled/closed/delete behavior.
- `blocked_flag_field` and `blocked_flag_value`: optional metadata for Jira Software's Flagged/Impediment field, used alongside the real Blocked transition when configured and as a fallback if another workflow lacks Blocked.
- `board_id`, `story_points_field`, `sprint_field`, `epic_link_field`, `rank_custom_field_id`, and `custom_field_mappings`: optional advanced metadata for scrum-master planning, sprint/rank operations, epics, estimates, and named custom fields.

`project_key` is also the Jira safety boundary. The app refuses existing-issue mutations for issue keys whose prefix does not match the configured project, verifies newly created issue keys before renaming local cards, and fails Jira startup hydration if the configured JQL returns issues from another project.

For scoped Jira Cloud tokens, `read:jira-work` and `write:jira-work` cover issue reads/writes in the current path. The assignable-user picker needs a Jira user-read scope as well. Worklogs and issue links are covered by classic `write:jira-work` or granular worklog/link scopes. Jira Software sprint/rank endpoints need Jira Software scopes such as `write:sprint:jira-software` and `write:issue:jira-software`. The current `EMAL` workflow now exposes a real project-scoped `Blocked` status with status ID `10039` and transition ID `41`, and the app also keeps the Jira Software Flagged/Impediment metadata in sync through `customfield_10021`.

The Jira Software Agile board configuration endpoint is separate from the issue/workflow APIs. Root cause confirmed: every `/rest/agile/1.0/...` board call returned `401` with `Unauthorized; scope does not match`, while `/rest/api/3/...` issue, status, and transition calls returned `200`. A replacement scoped token that included `read:board-scope:jira-software`, `read:issue-details:jira`, and `read:project:jira` still returned the same Agile API 401, so this is not blocking current app sync. Fix for this app: validate and sync through Jira Platform endpoints only. Added `scripts/jira-check-board-config.sh` for optional Agile diagnostics and `scripts/jira-validate-workflow-config.sh` for the scoped-token-safe platform validation path. The platform validator passed against `EMAL-11` and confirmed statuses plus transitions including `Blocked -> 41`.

Transition IDs are workflow-specific. Use any issue in the project and call Jira Cloud REST API v3:

```bash
curl -u "you@example.com:$(cat /absolute/path/to/jira-api-token)" \
  -H "Accept: application/json" \
  "https://your-site.atlassian.net/rest/api/3/issue/KAN-1/transitions"
```

Then run locally with macOS Keychain-backed secrets:

```bash
scripts/local-up.sh
```

## Test Checklist Before Git Actions

- Run `go test ./...`. Last local result: pass.
- Run `go test -race ./cmd/server`. Last local result: pass.
- Run `go vet ./...`. Last local result: pass.
- Run `go mod verify`. Last local result: pass.
- Run `scripts/pre-commit`. Last local result: pass.
- Run inline JavaScript syntax checks for both web clients. Last local result: pass via `node --check` on extracted inline scripts.
- Run `go test ./evaluation`. Last local result: pass.
- Run captured-result grading with `AUTO_BOT_EVAL_RESULTS_DIR=/absolute/path/to/results go test ./evaluation` after a real or simulated multi-participant run. Last local result: not run; no captured results have been generated yet.
- Run Terraform/Terragrunt formatting and Terraform validation. Last local result: pass via Dockerized `hashicorp/terraform:1.14.0@sha256:3abcdb56739bf9c61a0290cfd1a2e41ef9c3799c0e6fa7f3c467f883367d3ecb` and `alpine/terragrunt:1.15.2@sha256:002defed150fa617710d6c5c208c1d54dd7ad60821d83f0408457d116e39f191`; module validation used `hashicorp/aws = 6.45.0` and `hashicorp/cloudinit = 2.4.0`; local `terraform` and `terragrunt` binaries are not installed.
- Scan for forbidden `:latest` / `@latest` references in operational Docker/Terraform/script files. Last local result: pass.
- Start without Jira and verify the local demo still renders. Last local result: pass with `APP_API_TOKEN=test-token` and no provider credentials.
- Verify `/websocket` rejects missing token and reaches the WebSocket upgrader when token is present. Last local result: pass.
- Verify served HTML does not contain `APP_API_TOKEN`, `window.__APP_TOKEN__`, or query-string token wiring. Last local unit-test result: pass.
- Verify session cookies authenticate only the configured room/board and reject cross-board requests. Last local unit-test result: pass.
- Verify local-only `/auth/local-login` sets a browser session from the Keychain login token and is rejected outside `APP_ENV=local`. Last local unit-test/curl result: pass.
- Verify host creates a browser meeting code, wrong participant code is rejected, and correct participant code can mint a LiveKit token without the app access token. Last local browser/curl result: pass on 2026-05-19.
- Verify meeting leave lifecycle: participant leave removes only that participant and keeps the host meeting active; host leave ends the meeting, revokes participant access, records the board meeting end, and broadcasts the inactive meeting snapshot. Last local unit/curl result: pass on 2026-05-19.
- Verify local Docker browser-facing LiveKit URL uses IPv4 loopback when the server-internal LiveKit URL is `ws://livekit:7880`. Last local unit result: pass on 2026-05-19.
- Verify the CSP allows LiveKit browser validation calls only for local loopback and the configured LiveKit browser/cloud origin, so `/rtc/v1/validate` is not blocked during room join. Last local unit/browser result: pass on 2026-05-19.
- Verify Nova Sonic readiness blocks LiveKit token minting when AWS credentials are missing or expired. Last local unit-test result: pass.
- Verify production mode rejects disabled auth and LiveKit `devkey`/`secret`. Last local unit-test result: pass.
- Verify SQLite board snapshots and event history survive board reload. Last local unit-test result: pass.
- Verify SQLite meeting intelligence reports archive on meeting end and can be listed/loaded. Last local unit-test result: pass.
- Verify post-meeting report builder includes sprint intelligence, GitHub/PR hints, agent runs, setup readiness, observability, Jira changes, and transcript evidence. Last local unit-test result: pass.
- Verify autonomous agent-run state is created, exposed in board snapshots, and persisted through SQLite reload. Last local unit-test result: pass.
- Run `docker compose config`. Last local result: pass; Compose warns if AWS credential env vars are unset.
- Run `docker build -t auto-bot:local .`. Last local result: pass.
- Run app container smoke test and curl `/`. Last local result: pass.
- Start with `JIRA_CONFIG_PATH` and verify initial cards come from Jira. Last local result: pass against `EMAL`; 7 cards loaded through the scoped-token gateway URL.
- Create a card by voice or tool call and verify the local card is renamed to the Jira issue key. Last local result: pass with live test issue `EMAL-8`.
- Move a card and verify the Jira issue transitions. Last local result: pass, `EMAL-8` moved to `In Progress`.
- Update notes/title and verify Jira fields update. Last local result: pass, `EMAL-8` summary and description updated.
- Add tags and verify Jira labels update. Last local result: pass, `EMAL-8` labels updated.
- Delete a card and verify Jira uses the configured close/cancel transition. Last local result: pass, `EMAL-8` transitioned to `Done`.
- Assign/unassign, comment, ETA, priority, label removal, real Blocked transition, and blocked flag write-through. Last unit-test result: pass against `httptest`; live extended write-through passed against `EMAL-14` with the replacement scoped token on 2026-05-15, including an assertion that Jira reported status `Blocked` after `set_blocked`.
- Scrum-master contract tests for meeting start/update/next-speaker/summary/end and advanced Jira task tools. Last local result: pass in `go test ./cmd/server`.
- OpenAI realtime voice model support now defaults the action-capable meeting agent to `gpt-realtime-2`, uses `gpt-realtime-whisper` for streaming transcription, registers `gpt-realtime-translate` as a dedicated non-tooling translation profile, and rejects translation/transcription-only models if configured as the Jira/GitHub action model. Last local result: pass in `go test ./...`, `scripts/pre-commit`, and `pre-commit run --all-files` on 2026-05-19.
- Added a host-only Voice model dropdown in Meeting Settings backed by `GET/POST /voice/model`. Same-provider Nova Sonic model changes update the runtime selection and restart the active Bedrock stream when needed; inactive provider-path options remain visible but restart-required instead of pretending to hot-swap media transports. Last local result: pass in `go test ./...`, `go vet ./...`, and `scripts/pre-commit` on 2026-05-19.
- Made restart-required voice provider options actionable in local Docker. OpenAI Realtime models are no longer disabled in the LiveKit Meeting Settings dropdown; selecting one asks the local restart broker to recreate the app container with `VOICE_PROVIDER=openai` and the chosen OpenAI model. Local launchers now load OpenAI keys from `auto-bot/openai-api-key` or the existing `argus-openai-api-key`/`argus-cli` Keychain services, and Compose now passes OpenAI model env vars into the app container. Last local result: pass in `go test ./...`, `go test -race ./cmd/server`, `go vet ./...`, `scripts/pre-commit`, shell syntax checks, Python broker compile, browser dropdown smoke, and a live local restart from Nova Sonic to OpenAI `gpt-realtime-2` then back to Nova Sonic on 2026-05-19.
- Added a local-only AWS credential refresh broker for Docker development. Join Room now auto-refreshes/recreates the app container and retries once only when the selected speech path is AWS Nova Sonic and STS reports expired credentials; non-AWS voice models never trigger AWS re-auth. Last local result: pass in `go test ./...`, `go vet ./...`, `scripts/pre-commit`, shell/Python syntax checks, and local endpoint smoke on 2026-05-19.
- Fixed Jira subtask create-through for EMAL's actual issue type metadata: the app now resolves the project subtask issue type and posts the id Jira expects, while incomplete placeholder subtask titles are rejected before local board mutation. Meeting Settings no longer force-opens for passive Jira write-through failures during an active meeting; the button is marked, the control center records the failure, and a toast is shown. Last local result: pass in `go test ./cmd/server`; read-only Jira check confirmed EMAL issue type `10002` is `Subtask` on 2026-05-19.
- Fixed Nova Sonic agent playback pacing: Bedrock output is queued, resampled with interpolation/windowed averaging, and emitted to LiveKit at real-time 20 ms cadence instead of burst-writing Opus frames. `/voice/status` now exposes Nova output queue depth, drops, underruns, publish pps, and jitter; the Voice Reliability Dashboard shows those metrics as Agent Audio health. Last local result: pass in `go test ./...`, `go test -race ./cmd/server`, `go vet ./...`, `scripts/pre-commit`, Docker rebuild, authenticated `/voice/status`, and browser dashboard smoke on 2026-05-19.
- Advanced Jira write-through unit coverage for subtasks, story points, estimates, worklogs, issue links, sprint/rank/prioritization metadata, components, fix versions, custom fields, remote links, reporter/watchers, and transition metadata. Last local result: pass in `go test ./cmd/server`; live Jira validation still needed for sprint/rank/prioritization because those use Jira Software Agile endpoints and scopes.
- Jira project-key safety guard. Last unit-test result: pass; cross-project write attempts are rejected before any HTTP request is sent, and mixed-project search results fail hydration.
- Let Nova Sonic run past the renewal window or force-close the stream and verify audio restarts it.
- Trigger a Nova Sonic board mutation while the agent is speaking and verify the stream does not abort with duplicate `SYSTEM` content.
- Leave a Nova Sonic room quiet long enough to verify Bedrock does not abort with `Timed out waiting for input events`.

## Remaining High-Risk Gaps

- Auth is now meaningful against random web clients because the app token is not rendered into HTML, but it is still a shared bootstrap token. Public production should use OIDC/Cognito with per-user room membership instead of shared-token sessions.
- The current server authorizes one configured `APP_ROOM_ID`/`APP_BOARD_ID` per deployment. True multi-room operation still needs per-room agent orchestration, per-room Jira config, and per-user authorization records.
- Jira conflict handling now exists for webhook/poll refreshes and Jira write-through failures, with UI-visible resolution. It still needs live Atlassian webhook testing and richer field-level merge policies.
- Jira webhooks are implemented with a shared secret and project-key safety, but have not been exercised against live Atlassian webhook delivery yet.
- Jira assignable-user search is implemented and passed live with the replacement scoped token; the current `EMAL` project returns Scott Moore as the only assignable user.
- The current `EMAL` Jira workflow now has `To Do`, `In Progress`, `Blocked`, and `Done`; Blocked uses project-scoped status ID `10039` and transition ID `41`.
- The Nova Sonic duplicate-`SYSTEM` stream abort and idle-input timeout paths have code fixes, but still need long live-room replay with real speech before calling the provider flawless.
- Broader Jira issue actions still not exposed as voice tools: attachments, votes, issue security levels, bulk edits, release/version creation, sprint creation/closure, workflow administration, and full validator-aware conflict resolution. Issue links, watchers, ranking, worklogs, reporter changes, parent/subtask links, and custom fields now have voice tools and Jira write-through paths.
- Secrets Manager wiring exists for all AWS ECS runtime secrets, including GitHub App agent credentials. 1Password lookup is not wired; local OpenAI and LiveKit paths still support env-based secrets for local-only development.
- Agent execution is partially implemented for Jira-triggered code review. Remaining agent risk is live GitHub/Bedrock validation, cost controls, cancellation/retry/takeover, sandboxed execution, and broader specialist coverage.
- AWS deployment is scaffolded but not applied. A real AWS deploy still needs AWS credentials, a reviewed/pinned `FCK_NAT_AMI_ID`, DNS/cert inputs if using TLS/TURN/TLS, and LiveKit network validation; local `terraform` and `terragrunt` binaries are not installed in this environment.
- LiveKit on Fargate is implemented as a testable self-host path with Redis and TURN hooks, but WebRTC UDP/TURN reachability must be validated in AWS before treating it as production-ready. LiveKit Cloud mode is available as a Terraform input switch.
- Evaluation scaffolding now defines the required host-code, meeting-type, multi-participant, prompt-injection, recap, and AWS LiveKit proof cases, but it does not yet drive real browsers or compare against captured production meeting outputs.
- Post-meeting intelligence now has backend/API/UI/persistence coverage, but it still needs a live meeting with real participants to verify the report quality, transcript evidence, and Slack-ready recap in a realistic room.
- Jira Software board configuration reads are not wired into startup sync; the diagnostic script is available for token/scope verification if we decide to consume `/rest/agile/1.0/board/{boardId}/configuration`.

## Useful Next Build Steps

1. Live-test Jira Software sprint assignment and issue ranking with the final scoped token, because those call Agile endpoints rather than only Platform issue APIs.
2. Live-test Jira webhook delivery, conflict prompts, and undo/replay against the real EMAL board.
3. Add OIDC/Cognito user login and per-room membership before exposing this beyond local/ngrok demos.
4. Add multi-room agent orchestration so each room has its own LiveKit agent, board store, Jira config, audit stream, and authorization policy.
