# Living Kanban Board Progress

Last updated: 2026-05-15

## Current Rule

Testing is complete for this pass. Scott explicitly requested a `jira-scrum` branch commit and push on 2026-05-15.

## Implemented In This Pass

- Added board freshness tracking with `sequenceNumber` and `updatedAt` in every board snapshot.
- Added provider-agnostic `get_board` tool that returns cards, timestamp, and `sequence_number`.
- Added board unit tests for freshness, mutation sequence increments, context JSON, and tool definitions.
- Added `failure_inventory.md` as the seed replay inventory for ambiguous voice-command failures.
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
  - `set_sprint`, `rank_issue`, `set_components`, `set_fix_versions`, `set_custom_field`
  - `add_remote_link`, `set_reporter`, `add_watcher`
- Added structured scrum-master meeting state and tools:
  - `start_meeting`, `register_participant`, `record_participant_update`
  - `next_speaker`, `summarize_meeting`, `end_meeting`
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
- Updated README and `.env.example` for Jira, audit, and rate-limit proxy behavior.
- Added Terragrunt/Terraform AWS deployment shape:
  - Root Terragrunt config generates S3 state, DynamoDB locking, and AWS provider config in `us-east-1`.
  - AWS provider is pinned to `hashicorp/aws = 6.45.0`.
  - Reusable module deploys ECS Fargate app service, ECS Fargate LiveKit service, ALB, NLB, ECR, CloudWatch logs, Secrets Manager injection, EFS board persistence, and Bedrock task-role permissions.
  - LiveKit is modeled with TCP signaling, TCP fallback, and one muxed UDP media port for the Fargate path.
  - Added AWS helper scripts for secrets, image push, and dev deploy.
  - Added Cursor rule for Terraform/Terragrunt conventions.
  - Updated pre-commit checks to use Dockerized Terraform/Terragrunt format checks when local binaries are absent.

## Phase Status

| Phase | Status | What is built | What is not done yet |
| --- | --- | --- | --- |
| Prove the Product | Partial | Plan has meeting-hour targets. | No automated meeting-hour tracking or dashboard. |
| Phase 1 - OpenAI Realtime Baseline | Partial | OpenAI provider path, shared board tools, audio mixer, local quickstart, board regression tests, and seed failure inventory exist. | Manual voice validation, two-tab audio validation evidence, fork confirmation, and live replay results are not complete. |
| Phase 2 - Jira Sync | Partial | Jira config/client/startup hydration/write-through/polling foundation is implemented. `get_board` freshness contract is implemented. Voice tools now cover assignment, reporter/watchers, notes, comments, ETA/due date, priority, tag removal, subtasks, story points, estimates, worklogs, issue links, sprint assignment, ranking, components, fix versions, custom fields, remote links, real Blocked workflow transitions, blocked flag fallback, metadata/transition discovery, and project-key write safety. Live read hydration and basic write-through passed against `EMAL`. | Webhooks, sync-failure surfacing in the UI, conflict logging, and failure replay against real Jira are not complete. |
| Phase 2.5 - Workflow Config | Partial | JSON config supports status mappings, transition IDs, required fields, delete transition, polling, advanced field IDs, custom field mappings, metadata discovery, and transition option discovery. | Needs validation against three real workflows and a published known-limitations matrix. |
| Phase 3 - Nova Sonic 2 via LiveKit | Partial | Provider selection, LiveKit media path, Nova Sonic Bedrock path, tool handling, transcription broadcast, stream lifecycle renewal foundation, and post-mutation sanitized board-context refresh exist. | LiveKit data-channel board events, full 8-minute renewal proof, VAD calibration, A/B provider comparison, and real end-to-end tests are not complete. |
| Phase 4 - Agent-First Task Execution | Missing / blocked | None beyond board tags and Jira foundation. | Requires persistent task/agent state, classifier, cold-start labels, cost caps, sandboxed runners, checkpoints, take_over, retry_with, typed escalations, standup summary, and metrics. |
| Phase 5 - Auth, Hardening, AWS Deployment | Partial | Docker, non-root runtime, origin checks, headers, timeouts, read limits, max clients, HttpOnly session auth, board/room request authorization, rate limits, audit JSONL, SQLite board event history, Terragrunt remote state, ECS/Fargate app, ECS/Fargate LiveKit with muxed UDP media, ALB/NLB, ECR, CloudWatch logs, Secrets Manager wiring, EFS board persistence, and Bedrock task-role permissions exist. | OIDC/Cognito auth, true multi-room agent orchestration, CloudWatch dashboards, TURN deployment, DNS/certificate wiring, and validated LiveKit self-hosting are not complete. |

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

Then run locally with:

```bash
JIRA_CONFIG_PATH=/absolute/path/to/jira.json \
OPENAI_API_KEY=sk-... \
go run ./cmd/server/
```

## Test Checklist Before Git Actions

- Run `go test ./...`. Last local result: pass.
- Run `go test -race ./cmd/server`. Last local result: pass.
- Run `go vet ./...`. Last local result: pass.
- Run `go mod verify`. Last local result: pass.
- Run `scripts/pre-commit`. Last local result: pass.
- Run inline JavaScript syntax checks for both web clients. Last local result: pass via `node --check` on extracted inline scripts.
- Run Terraform/Terragrunt formatting and Terraform validation. Last local result: pass via Dockerized `hashicorp/terraform:latest` and `alpine/terragrunt:latest`; module validation used exact provider pin `hashicorp/aws = 6.45.0`; local `terraform` and `terragrunt` binaries are not installed.
- Start without Jira and verify the local demo still renders. Last local result: pass with `APP_API_TOKEN=test-token` and no provider credentials.
- Verify `/websocket` rejects missing token and reaches the WebSocket upgrader when token is present. Last local result: pass.
- Verify served HTML does not contain `APP_API_TOKEN`, `window.__APP_TOKEN__`, or query-string token wiring. Last local unit-test result: pass.
- Verify session cookies authenticate only the configured room/board and reject cross-board requests. Last local unit-test result: pass.
- Verify production mode rejects disabled auth and LiveKit `devkey`/`secret`. Last local unit-test result: pass.
- Verify SQLite board snapshots and event history survive board reload. Last local unit-test result: pass.
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
- Advanced Jira write-through unit coverage for subtasks, story points, estimates, worklogs, issue links, sprint/rank metadata, components, fix versions, custom fields, remote links, reporter/watchers, and transition metadata. Last local result: pass in `go test ./cmd/server`; live Jira validation still needed for sprint/rank because those use Jira Software Agile endpoints and scopes.
- Jira project-key safety guard. Last unit-test result: pass; cross-project write attempts are rejected before any HTTP request is sent, and mixed-project search results fail hydration.
- Let Nova Sonic run past the renewal window or force-close the stream and verify audio restarts it.

## Remaining High-Risk Gaps

- Auth is now meaningful against random web clients because the app token is not rendered into HTML, but it is still a shared bootstrap token. Public production should use OIDC/Cognito with per-user room membership instead of shared-token sessions.
- The current server authorizes one configured `APP_ROOM_ID`/`APP_BOARD_ID` per deployment. True multi-room operation still needs per-room agent orchestration, per-room Jira config, and per-user authorization records.
- Jira conflict handling is not implemented. Current behavior is write-through plus polling refresh, effectively last writer wins without losing-write records.
- Jira webhooks are not implemented. Polling is available as the current fallback.
- Jira assignable-user search is implemented and passed live with the replacement scoped token; the current `EMAL` project returns Scott Moore as the only assignable user.
- The current `EMAL` Jira workflow now has `To Do`, `In Progress`, `Blocked`, and `Done`; Blocked uses project-scoped status ID `10039` and transition ID `41`.
- Broader Jira issue actions still not exposed as voice tools: attachments, votes, issue security levels, bulk edits, release/version creation, sprint creation/closure, workflow administration, and full validator-aware conflict resolution. Issue links, watchers, ranking, worklogs, reporter changes, parent/subtask links, and custom fields now have voice tools and Jira write-through paths.
- Secrets Manager wiring exists for AWS ECS secrets. 1Password lookup is not wired; local OpenAI and LiveKit paths still support env-based secrets.
- Agent execution is not implemented. Phase 4 needs a separate state model and sandbox runner before any real dispatch should be trusted.
- AWS deployment is scaffolded but not applied. Terraform/Terragrunt validation and a real AWS deploy still need to run with your AWS credentials; local `terraform` and `terragrunt` binaries are not installed in this environment.
- LiveKit on Fargate is implemented as a testable self-host path, but WebRTC UDP reachability must be validated in AWS before treating it as production-ready.
- Jira Software board configuration reads are not wired into startup sync; the diagnostic script is available for token/scope verification if we decide to consume `/rest/agile/1.0/board/{boardId}/configuration`.

## Useful Next Build Steps

1. Live-test Jira Software sprint assignment and issue ranking with the final scoped token, because those call Agile endpoints rather than only Platform issue APIs.
2. Add Jira conflict detection, losing-write audit records, and UI-visible sync failure state.
3. Add OIDC/Cognito user login and per-room membership before exposing this beyond local/ngrok demos.
4. Add multi-room agent orchestration so each room has its own LiveKit agent, board store, Jira config, audit stream, and authorization policy.
