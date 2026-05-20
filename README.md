# Living Kanban Board

[![MIT License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
![Go](https://img.shields.io/badge/Built_with-Go-blue)
![WebRTC](https://img.shields.io/badge/Uses-WebRTC-blueviolet)
![OpenAI API](https://img.shields.io/badge/Powered_by-OpenAI_API-orange)
![AWS Nova Sonic](https://img.shields.io/badge/Powered_by-AWS_Nova_Sonic-yellow)

A voice-operated Kanban board where standup happens by voice. The AI scrum master agent listens to the meeting, tracks speakers, and updates the board in real time — creating, moving, opening, and closing tickets hands-free. Multiple participants join with webcam and microphone, see and hear each other, and all interact with the same AI agent. Two voice provider paths are supported:

- **OpenAI Realtime 2** — Pion WebRTC SFU, browser connects via raw WebRTC
- **AWS Nova Sonic 2** — LiveKit SFU, browser connects via livekit-client SDK, Bedrock bidirectional streaming

Both paths share the same Kanban board state and tool definitions ([cmd/server/board.go](cmd/server/board.go)).

![screenshot](./public/screenshot.png)

## Project Shape

The repo now has an explicit extension layer for open-source contribution:

- [docs/architecture.md](docs/architecture.md) explains the core runtime boundaries.
- [docs/codebase-map.md](docs/codebase-map.md) maps source files to runtime responsibilities.
- [CONTRIBUTING.md](CONTRIBUTING.md) gives contributor setup and extension workflow.
- [docs/extension-contracts.md](docs/extension-contracts.md) documents voice provider, connector, model provider, and action-ledger contracts.
- [docs/api/openapi.yaml](docs/api/openapi.yaml) documents the HTTP control plane.
- [docs/threat-model.md](docs/threat-model.md), [SECURITY.md](SECURITY.md), and [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) cover open-source security and governance.
- [docs/golden-demo.md](docs/golden-demo.md) defines the narrow proof path for demos.
- [examples/connector-template](examples/connector-template), [examples/voice-provider-template](examples/voice-provider-template), and [examples/model-provider-template](examples/model-provider-template) provide starting points for new integrations.

The stable contract package is `internal/core`. Runtime implementations live outside that package, and `scripts/check-import-boundaries.sh` prevents provider-specific dependencies from leaking into the core extension surface.

> [!IMPORTANT]
> Browser control APIs are protected by an HttpOnly session cookie. The HTML shells contain no token and the page never receives `APP_API_TOKEN`; users enter it once to create a session, and `/websocket`, `/livekit-token`, and authenticated JSON endpoints require that session or a Bearer token. For public multi-user use, put OIDC/Cognito in front of this shared-token flow.

## Quickstart

Local secrets belong in macOS Keychain. Do not create a project `.env` file for local development. Some maintainer demo examples below use the AWS profile `test_AccountA/AdministratorAccess`, Jira account `somoore2025@gmail.com`, and repo `somoore/auto-bot`; replace those with your own AWS/Jira/GitHub values.

For the normal local stack, use the one-command launcher:

```bash
cd /path/to/auto-bot
scripts/local-up.sh
```

It generates missing local app/login/webhook secrets, stores them in macOS Keychain, prompts once for the Jira token if it is not already stored, forces AWS to `us-east-1`, runs `assume test_AccountA/AdministratorAccess`, starts Docker Compose, opens a local-only login URL to set the HttpOnly session cookie, and redirects to [http://localhost:3001](http://localhost:3001).

Use the local Compose wrapper for routine stack commands:

```bash
scripts/local-compose.sh ps
scripts/local-compose.sh logs -f app livekit
scripts/local-down.sh
```

Contributor quality checks:

```bash
make test
make eval
make boundary
make precommit
```

The pre-commit path checks Go module hygiene, package graph resolution, dependency lock files, immutable release pins, import boundaries, tests, formatting, Docker digest pinning, Debian package pinning, Terraform/Terragrunt formatting, CDN SRI, and optional installed scanners such as `golangci-lint`, `govulncheck`, and `gosec`. The Python `pre-commit` framework is also configured through [.pre-commit-config.yaml](.pre-commit-config.yaml) with hook revisions pinned to immutable commit SHAs.

Golden demo preflight against a real configured stack:

```bash
AUTO_BOT_BASE_URL=http://localhost:3001 \
AUTO_BOT_ACCESS_TOKEN="$(scripts/keychain-get-secret.sh auto-bot/app-api-token "$USER")" \
scripts/validate-golden-demo.sh
```

Recommended Keychain service names:

| Secret | Service | Account |
| --- | --- | --- |
| Local app/session bootstrap token | `auto-bot/app-api-token` | your macOS user |
| Local one-click login token | `auto-bot/local-login-token` | your macOS user |
| OpenAI API key | `auto-bot/openai-api-key` | your macOS user |
| Jira API token | `auto-bot/jira-api-token` | your Jira email |
| Jira webhook secret | `auto-bot/jira-webhook-secret` | your macOS user |
| GitHub App ID | `auto-bot/github-app-id` | your macOS user |
| GitHub App installation ID | `auto-bot/github-app-installation-id` | your macOS user |
| GitHub App private key PEM | `auto-bot/github-app-private-key` | your macOS user |

Store secrets with:

```bash
scripts/keychain-store-secret.sh auto-bot/app-api-token "$USER"
scripts/keychain-store-secret.sh auto-bot/jira-api-token somoore2025@gmail.com

# Only needed for the OpenAI provider:
scripts/keychain-store-secret.sh auto-bot/openai-api-key "$USER"

# Optional, for local webhook testing:
scripts/keychain-store-secret.sh auto-bot/jira-webhook-secret "$USER"

# Optional, for autonomous Jira-to-GitHub agent runs:
scripts/keychain-store-secret.sh auto-bot/github-app-id "$USER"
scripts/keychain-store-secret.sh auto-bot/github-app-installation-id "$USER"
scripts/keychain-store-secret.sh auto-bot/github-app-private-key "$USER"
```

Read the fallback app access token when you need to enter it manually:

```bash
scripts/keychain-get-secret.sh auto-bot/app-api-token "$USER"
```

### Option A: OpenAI Realtime Transport (developer path)

The OpenAI Realtime backend transport is still in the repo for provider work and tests, but the default served browser experience is currently the unified LiveKit UI. Until an OpenAI-to-LiveKit bridge or a restored OpenAI HTML route is added, use Nova Sonic for normal local meetings and use this launcher only when developing the OpenAI transport itself.

```bash
# Prerequisites: Go 1.26, Opus 1.5.2, and pkg-config 1.8.1.
# Use the Docker Compose path below for the fully pinned local dependency set.

# Run the OpenAI backend path for development
scripts/run-openai-keychain.sh
```

### Option B: Nova Sonic 2 (Docker Compose)

```bash
# Prerequisites: Docker, AWS CLI with granted credential-process, Keychain secrets above

# Optional if your AWS profile differs from the repo default:
export AWS_PROFILE=test_AccountA/AdministratorAccess

# Start the stack. Prefer the one-command launcher:
./scripts/local-up.sh
```

Open [http://localhost:3001](http://localhost:3001). As host, select **Host**, choose the meeting type, click **Create meeting code**, then click **Join room** and grant microphone access. Participants select **Participant**, enter the generated meeting code, and click **Join room**. Participant browsers do not need the app access token; the server creates their session only after the meeting code is accepted. The LiveKit SFU runs on port 7880, the app on port 3001.

The **Join room** button performs a server-side voice readiness preflight before entering LiveKit. For AWS Nova Sonic it validates AWS credentials with STS in `us-east-1` and makes sure the server-side Nova participant has joined the room. If credentials are missing or expired and the selected speech model is AWS-based, the host browser asks the local-only refresh broker to rerun the Keychain/`assume`/Docker path, waits for the app container to come back, and retries the join once. Non-AWS speech models never trigger AWS re-auth. If the broker is not available, the browser stays out of the room and shows the `scripts/local-up.sh` recovery command instead of leaving you alone in an empty meeting.

The Docker Compose stack is explicitly `APP_ENV=local`. `APP_API_TOKEN` is required and should come from Keychain through `scripts/dc-up-keychain.sh`; the app token is not baked into the image or served to the browser. `scripts/local-up.sh` also provisions a separate local-only `APP_LOCAL_LOGIN_TOKEN`, starts the token-protected local runtime restart broker on `127.0.0.1`, and opens `/auth/local-login` to set the HttpOnly session cookie without prompting. The local login and local restart paths only exist in local mode; production rejects `APP_LOCAL_LOGIN_TOKEN`.

For a local end-to-end stack with Jira sync, create an ignored local config at `config/jira.local.json` from [config/jira.example.json](config/jira.example.json), then store the Jira token in Keychain as `auto-bot/jira-api-token` for account `somoore2025@gmail.com`. The Keychain launcher sets `JIRA_CONFIG_PATH=/srv/config/jira.local.json` automatically when that local file and token are present. The app container mounts `./config` read-only at `/srv/config`.

For autonomous code-review agent runs, create a GitHub App and install it only on the repository you want the agents to inspect. Minimum analyzer permissions are `Contents: read`, `Metadata: read`, and `Pull requests: read`; enable `Pull requests: write` only if you want the app to leave PR review comments. Store the app id, installation id, and private key in Keychain using the service names above. The app mints short-lived GitHub App installation tokens per run, scopes them to the requested repo, and refuses repos outside `GITHUB_ALLOWED_REPOS` when that allowlist is set.

Use the helper when setting this up locally:

```bash
GITHUB_DEFAULT_REPO=somoore/auto-bot scripts/github-app-setup.sh
```

The helper uses GitHub's manifest flow, stores the App ID, installation ID, and private key in macOS Keychain, and creates no `.env` file. If GitHub returns the PEM key as Keychain hex data, the server decodes it before use.

Agent models run through AWS Bedrock only. The project-manager classifier defaults to Claude Haiku 4.5 on the Bedrock US inference profile and the code-review specialist defaults to Claude Sonnet 4.6 on the Bedrock US inference profile. Sonnet 4.6 is the normal cost/capability default; use Opus 4.7 or Opus 4.6 by setting `AGENT_REVIEW_MODEL` only for escalation-grade reviews on large or high-risk changes. Security-review requests route through the PR reviewer with a vulnerability/exploitability lens, including impact, exploit scenario, remediation, validation guidance, Jira writeback, and optional inline PR comments. Agent runs mark Jira/PR publishing as posted only after those APIs return success; publish failures are kept as visible warnings in the live drawer and post-meeting report. There is no direct Anthropic API key path for these agents, and an autonomous run fails visibly instead of dispatching if the Bedrock client cannot be initialized.

To run the UI, board, and Jira control plane without AWS voice credentials:

```bash
AUTO_BOT_SKIP_AWS=1 ./scripts/dc-up-keychain.sh --build -d
```

That mode is for board/Jira testing only. Nova Sonic meeting join is intentionally blocked until the stack is restarted with fresh AWS credentials.

### Option C: Nova Sonic 2 (local, no Docker)

```bash
# Prerequisites: Go 1.26, Opus 1.5.2, and pkg-config 1.8.1.
# Use the Docker Compose path above for the fully pinned local dependency set.

# Start LiveKit dev server in a separate terminal
docker run --rm -p 7880:7880 -p 7881:7881 -p 7882-7892:7882-7892/udp \
  livekit/livekit-server:v1.9.1@sha256:c039a1bfa154c8479ac369c380665638e92a7e9531e69664549c0c0d3eb65e63 \
  --dev --bind 0.0.0.0 --node-ip 127.0.0.1

# Authenticate and run the app
assume test_AccountA/AdministratorAccess
VOICE_PROVIDER=nova-sonic \
APP_ENV=local \
APP_API_TOKEN="$(scripts/keychain-get-secret.sh auto-bot/app-api-token "$USER")" \
JIRA_API_TOKEN="$(scripts/keychain-get-secret.sh auto-bot/jira-api-token somoore2025@gmail.com)" \
JIRA_CONFIG_PATH="$PWD/config/jira.local.json" \
AWS_REGION=us-east-1 \
LIVEKIT_API_KEY=devkey \
LIVEKIT_API_SECRET=secret \
go run ./cmd/server/
```

### Optional: Jira Sync

Create a Jira Cloud API token and store it in macOS Keychain. For this repo's local EMAL project config, the service/account pair is:

```bash
scripts/keychain-store-secret.sh auto-bot/jira-api-token somoore2025@gmail.com
```

Create `config/jira.local.json` by copying [config/jira.example.json](config/jira.example.json) and filling in non-secret site/project/status mappings. Keep that local file ignored, and read the token from `JIRA_API_TOKEN`. Local launchers load the token from Keychain and set `JIRA_API_TOKEN` only for the child process/container.

When `JIRA_CONFIG_PATH` or `JIRA_CONFIG_JSON` is set, the server loads the initial board from Jira using the configured JQL and writes board mutations back to Jira: create issues/sub-tasks, transition issues, update summary/description, append notes, add comments, add/remove labels, assign/unassign issues, set reporter/watchers, set due dates/ETAs, set priority, set story points and time estimates, add worklogs, link issues, assign sprint, prioritize issues above/below other issues in any Kanban column, rank backlog/sprint issues, set components/fix versions/custom fields, attach remote links, mark blocked, and close/cancel for deletes. Sub-task creation resolves the project metadata and sends Jira the real subtask issue-type id when available, so projects that call it `Subtask` instead of `Sub-task` still work. Keep the API token outside the repo; prefer `api_token_file`, `api_token_command`, or `api_token_env` in the config.

Jira webhooks are available at `POST /jira/webhook`. Set `webhook_secret` in the Jira config or `JIRA_WEBHOOK_SECRET`; Jira should send the same value as `Authorization: Bearer <secret>` or `X-Auto-Bot-Jira-Webhook-Secret`. Webhook payloads are project-key checked before refresh, then the board is refreshed from Jira. If Jira changed a card that also has newer local meeting changes, the UI surfaces a conflict and asks whether to keep the local meeting update or use Jira's latest value.

Jira writes are constrained to the configured `project_key`. Existing-issue mutations refuse issue keys outside that project, newly created Jira issue keys are verified before local cards are renamed, and startup hydration fails if the configured JQL returns issues from another project. This keeps a bad voice command or overly broad JQL from touching someone else's Jira board.

For Jira Cloud scoped API tokens, the base issue sync path needs `read:jira-work` and `write:jira-work`. The assignable-user picker, reporter setting, and watcher search need Jira user-read scope; without it, ticket writes can work while `search_jira_users` returns a Jira scope error. Worklogs and issue links are covered by the classic `write:jira-work` scope or granular worklog/link scopes. Jira Software sprint/rank APIs additionally require Jira Software scopes such as `write:sprint:jira-software` for moving issues to a sprint and `write:issue:jira-software` for ranking issues. A true `Blocked` column also requires the Jira workflow to contain a Blocked status and a configured transition. When a workflow lacks that status, `blocked_flag_field`/`blocked_flag_value` can map Jira Software's Flagged/Impediment field to the local Blocked column and keep blocked work visible.

Optional advanced fields in [config/jira.example.json](config/jira.example.json) enable richer scrum-master behavior: `board_id` for sprint discovery, `story_points_field`, `sprint_field`, `epic_link_field`, `rank_custom_field_id`, and `custom_field_mappings` for named custom fields. Use the voice tool `get_jira_metadata` or Jira field discovery to confirm these IDs for your site before relying on live write-through.

The Jira Software Agile board APIs use a separate scope family from the platform issue APIs. If you want to read board column metadata through `/rest/agile/1.0/board/{boardId}/configuration`, create a scoped token that also includes `read:board-scope:jira-software` and `read:issue-details:jira`. Add `read:project:jira` if you need to discover/list boards instead of checking a known board ID. You can verify a token with:

```bash
JIRA_API_TOKEN="$(scripts/keychain-get-secret.sh auto-bot/jira-api-token somoore2025@gmail.com)" \
scripts/jira-check-board-config.sh config/jira.local.json 1
```

Current sync does not depend on that Agile endpoint. For scoped API tokens, validate the working issue/status/transition path with Jira Platform APIs instead:

```bash
JIRA_API_TOKEN="$(scripts/keychain-get-secret.sh auto-bot/jira-api-token somoore2025@gmail.com)" \
scripts/jira-validate-workflow-config.sh config/jira.local.json EMAL-11
```

If the Agile board check still returns `Unauthorized; scope does not match` after adding the documented board scopes, keep using the platform validation path for this app. If direct Jira board metadata becomes mandatory later, use a classic/unscoped Jira API token against the `*.atlassian.net` site URL for that diagnostic path or move the board-metadata feature to OAuth/Forge.

## Voice Commands

The scrum master agent understands natural language commands:

| Say | Action |
| --- | --- |
| "I shipped the ICE restart handling" | Moves matching ticket to Done |
| "I'm working on the RTP buffer" | Moves matching ticket to In Progress |
| "I'm blocked on the DTLS cleanup" | Moves matching ticket to Blocked |
| "Create a ticket for WebSocket auth" | Creates a new ticket |
| "Open the HEVC packetizer task" | Opens the card detail modal |
| "Open the auth task" (no match) | Agent asks if you'd like to create it |
| "Close it" / "Thanks" | Closes the card detail modal |
| "Move the buffer task to backlog" | Moves ticket to specified column |
| "Add a tag 'urgent' to card-003" | Adds tags without replacing existing ones |
| "Remove the urgent label" | Removes matching Jira labels/tags |
| "Assign this to Scott" | Searches Jira assignable users, then assigns after a unique match |
| "Add a comment that QA is validating it" | Adds a Jira comment |
| "Set the ETA to Friday" | Sets the Jira due date/ETA |
| "Raise this to high priority" | Sets Jira priority |
| "Break this into a sub-task for sprint picker wiring" | Creates a Jira sub-task under the current parent |
| "Size this at five points" | Sets story points / agile estimate |
| "Log ninety minutes on the Jira metadata work" | Adds a Jira worklog |
| "Link this as blocked by the provider API" | Creates a Jira issue link |
| "Put this in Platform Sprint 42 and rank it before EMAL-8" | Sets sprint and backlog/sprint rank |
| "Prioritize Perform end-to-end testing above aws scanning" | Moves/reorders the card above the target card in that Kanban column |
| "Move the sub-task to the top of In Progress" | Reorders a sub-task within the column while preserving its parent |
| "Set component to Voice Agent and fix version to scrum-master-mvp" | Updates Jira planning metadata |
| "Delete the old task" | Removes a ticket from the board |

The agent also responds to implicit status updates during standup — if someone says "I finished X", it moves the matching card to Done automatically. In structured meetings it can start/end meetings, deliver a 60-second opening briefing, track participants, move to the next speaker, record blockers/risks/decisions/action items/follow-ups/parking-lot topics, track ownership, summarize the meeting, and keep Jira synchronized.

Meeting types are explicit. The host can start as a general meeting, standup, 1:1, sprint review, or open-ended meeting, and can switch modes in the UI or by asking the agent during the meeting. The agent is instructed to switch modes only from live host/facilitator speech or after host confirmation.

Medium-risk Jira actions such as assignment, ETA, priority, and reporter changes create a pending confirmation before they write. High-risk actions such as delete/close, sprint moves, and ranking changes also require explicit confirmation. Jira-backed tool results distinguish local board mutation from external API confirmation. The agent is instructed to say Jira was updated only when `jira_sync.ok=true` or `external_action_status=api_confirmed`; failed or unconfigured write-through returns an explicit `assistant_instruction` telling the agent not to claim success. The UI exposes **Undo** and **Audit** controls; audit replay shows the live speech evidence, selected tool, confidence/guardrail decision, external API result, and before/after context.

## Multi-Party Video & Layout Modes

The Nova Sonic frontend (LiveKit) supports multi-party video conferencing. All participants see each other's webcam feeds, hear each other, and interact with the shared AI agent. Active speakers are highlighted in real time.

The right-side operator panel includes the meeting code, Meeting Control Center, Voice Reliability Dashboard, agent confidence evidence, validation checklist, and a Slack-ready executive recap. It surfaces who has and has not spoken, blockers, decisions, action items, pending confirmations, Jira mutations, mic/LiveKit/Nova/Bedrock/participant-audio/agent-audio/transcription/Jira health, and precise failure states such as expired AWS credentials, missing agent participant, blocked mic permission, rejected Jira scope, unconfirmed Jira writes, or LiveKit audio-track problems.

## Meeting Intelligence

The scrum-master layer now has a durable post-meeting intelligence path. During and after a meeting the backend can generate a report with agenda, participants, decisions, risks, action items, parking-lot items, follow-ups, unresolved blockers, ownership, transcript evidence, Jira mutations, autonomous agent runs, sprint risk signals, GitHub/PR hints, setup readiness, and voice/LiveKit/Jira observability.

Open [http://localhost:3001/post-meeting](http://localhost:3001/post-meeting) after a local meeting, or click **Intelligence** in the LiveKit operator panel. The page includes:

- Slack-ready executive recap
- Jira changes summary and audit evidence
- Autonomous agent-run timeline with PM classification, specialist, repo/PR, checkpoints, findings, and Jira/PR publish state
- Blockers, risks, action items by owner, and unresolved questions
- Sprint intelligence for blocked, unassigned, missing-ETA, stale, PR-ready, and scope-change signals
- GitHub/PR context from Jira remote links, card tags/comments, or optional `GITHUB_CONTEXT_JSON`
- Setup readiness for auth, identity, Jira, persistence, LiveKit, and speech providers
- Observability for voice provider readiness, LiveKit mode, Jira configuration, storage, agent presence, Bedrock stream, and board sequence

Useful authenticated endpoints:

| Endpoint | Purpose |
| --- | --- |
| `GET /meeting/intelligence` | Current meeting intelligence report |
| `GET /meeting/intelligence?meeting_id=<id>` | Archived report loaded from SQLite |
| `GET /meeting/status` | Current host/participant meeting access, role, meeting type, and recent agent runs |
| `GET /meetings?limit=50` | Archived report summaries plus current summary |
| `GET /setup/status` | Setup/admin readiness checks |
| `POST /setup/aws/refresh` | Local-only host refresh trigger for expired AWS speech credentials |
| `GET /observability/status` | Current voice/Jira/storage/meeting observability |
| `GET /voice/model` | Active voice model, selectable model options, and restart-required provider paths |
| `POST /voice/model` | Host-only voice model/provider selection; provider-path changes may trigger local restart or return restart-required |
| `GET /voice/providers` | Active and available full-duplex speech provider options |
| `GET /voice/status` | Voice readiness JSON; failures are returned in `ready`, `requires_restart`, and `message` fields |
| `GET /identity/status` | Current identity, role, and meeting permissions |
| `GET /workspace/status` | Current workspace, board, room, provider, identity, and connector health scope |

When `BOARD_SQLITE_PATH` is set, ending a meeting archives the intelligence report into SQLite. Docker Compose uses `/srv/data/board.sqlite`; AWS mounts `/srv/data` on EFS for the Fargate app task.

### Layout Modes

Switch between four Google Meet-style layouts using the toolbar buttons:

| Layout | Description |
| --- | --- |
| **Filmstrip** | Horizontal video strip above the board (default) |
| **Sidebar** | Narrow video column on the right, board dominant on the left |
| **Grid** | All participants in a tiled grid above the board |
| **Spotlight** | Active speaker large, board and transcription to the right |

All panels between video, board, and transcription are **resizable** — drag the handles between them to adjust sizes. Panel sizes and layout preference are persisted to localStorage.

### Controls

- **Mic toggle** — mute/unmute microphone
- **Camera toggle** — enable/disable webcam
- **Leave room** — disconnect from the meeting
- **Meeting type** — switch facilitation between general meeting, standup, 1:1, sprint review, and open-ended modes
- **Undo/Audit** — reverse the latest voice-driven mutation or inspect replay evidence

The app is fully responsive: desktop, tablet, and mobile. On smaller screens, layouts collapse to stacked views, resize handles are hidden, and controls remain accessible without scrolling. The transcription panel auto-scrolls in place within a fixed viewport — the page itself never scrolls.

## Environment Variables

| Variable | Default | Provider | Purpose |
| --- | --- | --- | --- |
| `VOICE_PROVIDER` | `openai` | Both | `openai` or `nova-sonic` |
| `APP_ENV` | `production` | Both | Runtime safety mode. `production` rejects disabled auth and LiveKit dev credentials; Compose sets `local`. |
| `APP_AUTH_MODE` | `token` | Both | `token` for HttpOnly browser sessions/Bearer auth; `disabled` is only allowed with `APP_ENV=local`. |
| `APP_API_TOKEN` | _(required unless auth disabled)_ | Both | Shared bootstrap token for creating browser sessions and for non-browser Bearer auth. Never injected into served HTML. |
| `APP_LOCAL_LOGIN_TOKEN` | _unset_ | Both/local only | Local-only one-click browser login token used by `scripts/local-up.sh`; rejected unless `APP_ENV=local`. Do not set in AWS. |
| `APP_LOCAL_AWS_REFRESH_URL` | _unset_ | Local only | Token-protected localhost broker URL used to refresh expired AWS speech credentials from Docker development. Only used when the active voice model is AWS Nova Sonic. |
| `APP_LOCAL_AWS_REFRESH_TOKEN` | _unset_ | Local only | Broker auth token generated into macOS Keychain by `scripts/local-up.sh`. Do not set in AWS. |
| `APP_WORKSPACE_ID` | `default` | Both | Deployment/workspace identifier returned by `/workspace/status`; current runtime remains single-workspace scoped. |
| `APP_ROOM_ID` | `kanban-meeting` | Both | Authorized room ID for this deployment. |
| `APP_BOARD_ID` | `default` | Both | Authorized board ID for this deployment. |
| `APP_BASE_URL` | _(auto-detect)_ | Both | Override WebSocket base URL (e.g., `wss://example.com/websocket`) |
| `APP_IDENTITY_PROVIDER` | _(derived)_ | Both | Optional label for identity status/readiness reports. If unset, the server derives `cognito`, `trusted-proxy`, or `shared-token`. |
| `COGNITO_USER_POOL_ID` | _unset_ | AWS/future auth | Readiness signal for a future Cognito/OIDC front door; it does not replace current session auth by itself. |
| `TRUSTED_IDENTITY_HEADER` | _unset_ | Reverse proxy/future auth | Readiness signal for trusted ALB/proxy identity headers; only set behind a trusted proxy. |
| `BOARD_SQLITE_PATH` | _unset_ | Both | Optional SQLite path for durable board snapshots and event history. Compose uses `/srv/data/board.sqlite`. |
| `OPENAI_API_KEY` | _(required)_ | OpenAI | Auth for the OpenAI Realtime API |
| `OPENAI_REALTIME_MODEL` | `gpt-realtime-2` | OpenAI | Voice-to-action Realtime model. Must be a conversation/tool-capable model for Jira/GitHub actions. |
| `OPENAI_REALTIME_TRANSCRIPTION_MODEL` | `gpt-realtime-whisper` | OpenAI | Streaming transcription model used inside the OpenAI Realtime meeting session. |
| `OPENAI_REALTIME_TRANSCRIPTION_LANGUAGE` | _unset_ | OpenAI | Optional language hint passed to the OpenAI realtime transcription config. |
| `OPENAI_REALTIME_TRANSLATION_MODEL` | `gpt-realtime-translate` | OpenAI | Registered live-translation profile model for future translation sessions. It is not granted Jira/GitHub tools. |
| `OPENAI_REALTIME_TRANSLATION_TARGET_LANGUAGE` | `en` | OpenAI | Default target language for the registered translation profile. |
| `CONFERENCE_LOOPBACK_ONLY` | _unset_ | OpenAI | When `1`, restrict browser ICE to loopback (macOS same-machine) |
| `PION_NAT1TO1_IP` | _unset_ | OpenAI | Advertise this IP as host ICE candidate |
| `AWS_ACCESS_KEY_ID` | _unset_ | Nova Sonic | Explicit AWS credentials (preferred in Docker) |
| `AWS_SECRET_ACCESS_KEY` | _unset_ | Nova Sonic | Explicit AWS credentials |
| `AWS_SESSION_TOKEN` | _unset_ | Nova Sonic | Session token for assumed roles |
| `AWS_PROFILE` | `test_AccountA/AdministratorAccess` | Nova Sonic | AWS shared config profile (used when explicit keys not set) |
| `AWS_REGION` | `us-east-1` | Nova Sonic | Bedrock region |
| `NOVA_SONIC_MODEL` | `amazon.nova-2-sonic-v1:0` | Nova Sonic | Bedrock model ID |
| `NOVA_SONIC_VOICE` | `matthew` | Nova Sonic | TTS voice ID |
| `AGENT_PM_MODEL` | `us.anthropic.claude-haiku-4-5-20251001-v1:0` | Agents/AWS | Bedrock US inference-profile ID for PM classification |
| `AGENT_REVIEW_MODEL` | `us.anthropic.claude-sonnet-4-6` | Agents/AWS | Bedrock US inference-profile ID for code-review specialists |
| `CHAT_TRANSLATION_MODEL` | `AGENT_PM_MODEL` | Agents/AWS | Optional Bedrock model ID for translating non-English meeting chat text to English before agent processing. |
| `GITHUB_APP_ID` | _unset_ | Agents/GitHub | GitHub App id for short-lived installation tokens |
| `GITHUB_APP_INSTALLATION_ID` | _unset_ | Agents/GitHub | GitHub App installation id for the allowed repo/account |
| `GITHUB_APP_PRIVATE_KEY` | _unset_ | Agents/GitHub | PEM private key injected from Keychain or Secrets Manager; never shown to the model |
| `GITHUB_APP_PRIVATE_KEY_FILE` | _unset_ | Agents/GitHub | Optional operator-controlled file path for the GitHub App PEM when env injection is not used. |
| `GITHUB_DEFAULT_REPO` | _unset_ | Agents/GitHub | Optional default repo in `owner/name` form |
| `GITHUB_ALLOWED_REPOS` | _unset_ | Agents/GitHub | Comma-separated repo allowlist; agent GitHub access refuses anything else |
| `GITHUB_PR_COMMENTS_ENABLED` | `false` | Agents/GitHub | When `true`, post PR review comments using `Pull requests: write`; Jira comments still work without it |
| `GITHUB_CONTEXT_JSON` | _unset_ | Reports/GitHub | Optional JSON context for post-meeting GitHub/PR enrichment when no live GitHub App query is needed. |
| `GITHUB_TOKEN` | _unset_ | Reports/GitHub | Optional setup-readiness signal for GitHub enrichment; GitHub App credentials are still the agent-run path. |
| `LIVEKIT_DEPLOYMENT_MODE` | `self-hosted` | Nova Sonic/AWS | Terraform media-plane switch: `self-hosted` deploys LiveKit, `cloud` uses LiveKit Cloud with `LIVEKIT_CLOUD_URL`. |
| `LIVEKIT_CLOUD_URL` | _unset_ | Nova Sonic/AWS | LiveKit Cloud URL, for example `wss://project.livekit.cloud`, used when `LIVEKIT_DEPLOYMENT_MODE=cloud`. |
| `LIVEKIT_URL` | `ws://localhost:7880` | Nova Sonic | LiveKit server WebSocket URL |
| `LIVEKIT_BROWSER_URL` | _derived_ | Nova Sonic | Browser-facing LiveKit URL returned by `/livekit-token`; local Docker defaults this to `ws://127.0.0.1:7880` because LiveKit is bound to IPv4 loopback. |
| `LIVEKIT_API_KEY` | _(required)_ | Nova Sonic | LiveKit API key (no default — must be set) |
| `LIVEKIT_API_SECRET` | _(required)_ | Nova Sonic | LiveKit API secret (no default — must be set) |
| `JIRA_CONFIG_PATH` | _unset_ | Both | Optional Jira Cloud sync configuration JSON |
| `JIRA_CONFIG_JSON` | _unset_ | Both | Optional inline Jira Cloud sync configuration JSON; useful for AWS Secrets Manager injection |
| `JIRA_API_TOKEN` | _unset_ | Both | Optional Jira API token when the config uses `"api_token_env": "JIRA_API_TOKEN"`. Local launchers load it from macOS Keychain. |
| `JIRA_WEBHOOK_SECRET` | _unset_ | Both | Optional shared secret for `POST /jira/webhook`; also supported as `webhook_secret` in the Jira config |
| `AUDIT_LOG_PATH` | _unset_ | Both | Optional JSONL file for board mutation and Jira refresh audit events |
| `TRUST_PROXY_HEADERS` | _unset_ | Both | Set to `1` behind a trusted reverse proxy so rate limiting uses forwarded client IP headers |

Local development should use macOS Keychain via the scripts above. `.env.example` is retained only as an environment variable reference; do not copy it into a local `.env`.

## Speech-to-Speech Providers

The backend is already provider-switched with `VOICE_PROVIDER`:

- `VOICE_PROVIDER=openai` uses the OpenAI Realtime path and `OPENAI_REALTIME_MODEL`.
- `VOICE_PROVIDER=nova-sonic` uses LiveKit for the meeting room and Amazon Bedrock Nova Sonic for full-duplex speech-to-speech via `NOVA_SONIC_MODEL`.

The Meeting Settings drawer includes a host-only **Voice model** dropdown. It lists the action-capable full-duplex models known to the server. Same-provider Nova Sonic changes update the next Bedrock stream immediately and restart an active stream if needed. Provider-path changes, such as switching a running Nova Sonic deployment to OpenAI Realtime, are selectable in local Docker and trigger the local restart broker; in non-local environments they are shown as restart-required because they use a different browser/media path.

OpenAI model support is intentionally split by capability:

- `gpt-realtime-2` is the default voice-to-action meeting model. It is the only new OpenAI realtime model in this repo that is allowed to call Jira/GitHub tools.
- `gpt-realtime-whisper` is the default streaming transcription model via `OPENAI_REALTIME_TRANSCRIPTION_MODEL`.
- `gpt-realtime-translate` is registered as a dedicated live-translation provider profile using OpenAI's translation endpoints. It is not accepted as `OPENAI_REALTIME_MODEL` for the scrum agent because the translation model does not support function calling.

Startup fails clearly if a specialized transcription or translation model is configured as the Jira/GitHub action model. `/voice/status` returns HTTP 200 with failure details in the JSON body, including `ready=false`, `requires_restart`, and `message` when relevant. That prevents the agent from claiming an action-capable meeting session is ready when the selected OpenAI model cannot actually call tools.

To add another full-duplex speech model, keep the browser/session/Jira/tool surface unchanged and add a provider implementation beside `kanban.go` and `nova_sonic.go` that consumes the shared `KanbanToolDefs()` and `SessionInstructions()` contracts. The app should only need a new `VOICE_PROVIDER` value, model env vars, and any provider-specific secret ARN wiring in Terraform.

The Nova Sonic path sends exactly one Bedrock `SYSTEM` content block when a stream starts. Later board refreshes are sent as non-interactive application data, not new system prompts, because Bedrock rejects duplicate system content. The audio input stream also sends periodic silent frames while participants are paused so Bedrock does not close the meeting stream for lack of input events.

## Architecture

```
VOICE_PROVIDER=openai                    VOICE_PROVIDER=nova-sonic
  Browser ─── Pion SFU ─── OpenAI         Browser ─── LiveKit ─── Go Agent ─── Bedrock
            ↘                                       ↘
              Shared Board (board.go)                 Shared Board (board.go)
            ↙                                       ↙
       WebSocket (board events)              WebSocket (board events + transcription)
```

Both paths share the Kanban board state, tool definitions, and session instructions from `board.go`. The OpenAI path uses Pion WebRTC with data channels; the Nova Sonic path uses LiveKit as the SFU and sends audio to Bedrock via bidirectional HTTP/2 streaming.

The Nova Sonic Bedrock stream is **lazy** — it only starts when the first participant joins the room and auto-restarts if the stream ends or errors. After each board mutation, Nova Sonic receives a sanitized board-context refresh with the latest sequence number, and that refresh is explicitly framed as untrusted application data so Jira/task text cannot become instructions. Nova output audio is paced through a bounded 20 ms frame queue with an 80 ms pre-roll buffer, 120 ms short-utterance timeout, output drops/underruns/jitter metrics, and linear 16 kHz-to-48 kHz resampling before LiveKit publish.

## Project Layout

```
cmd/
  server/               Go source (single binary, package main)
    main.go             Entry point, provider switch, HTTP/WebSocket server
    board.go            Shared Kanban board state, card CRUD, tools, instructions
    scrum_tools.go      Scrum-master meeting tools and advanced local Jira metadata actions
    meeting_reports.go  Post-meeting intelligence report builder
    meeting_report_handlers.go
                        Intelligence, setup, observability, provider, and identity endpoints
    meeting_access.go   Host/participant meeting codes, roles, and access lifecycle
    meeting_intelligence.go
                        Confirmations, audit replay, transcript evidence, memory, and briefings
    chat_messages.go    Meeting text normalization and English translation helpers
    agent_runs.go       Bedrock PM/specialist agent-run lifecycle and Jira publish path
    bedrock_agents.go   Bedrock Claude Messages invoke client for PM/review agents
    github_app.go       GitHub App JWT, installation tokens, PR diff reads, optional PR reviews
    jira.go             Jira config, hydration, core issue write-through
    jira_ext.go         Advanced Jira metadata, Agile, worklog, link, and custom-field calls
    auth.go             HttpOnly session and Bearer auth
    board_store.go      SQLite board snapshots and event history
    guardrails.go       Prompt-injection redaction and tool-argument guardrails
    voice_status.go     Voice readiness preflight and user-facing recovery guidance
    voice_models.go     Host-selectable voice models and restart-required provider switches
    aws_refresh.go      Local-only proxy to refresh expired AWS speech credentials
    workspace.go        Current workspace/board/room/provider scope endpoint
    rate_limiter.go     Fixed-window HTTP/WebSocket rate limits
    audit.go            Structured board/Jira audit events
    kanban.go           OpenAI Realtime WebRTC transport
    nova_sonic.go       Nova Sonic 2 provider (LiveKit + Bedrock)
    nova_sonic_output.go
                        Paced Nova output audio queue and metrics
    nova_sonic_mixer.go 16kHz mono PCM mixer for Nova Sonic
    audio_mixer.go      48kHz stereo PCM mixer for OpenAI
    opus_encoder.go     CGo Opus encoder
    opus_decoder.go     CGo Opus decoder
internal/
  core/                 Stable extension contracts, registries, evidence, receipts, and action ledger
  core/contracttest/    Shared extension contract-test helpers
  mocks/                No-credential test implementations of the extension contracts
web/
  index.html            OpenAI frontend (Pion WebRTC)
  index_livekit.html    Nova Sonic frontend (LiveKit SDK, multi-party video, layout modes, transcription)
  post_meeting.html     Post-meeting intelligence dashboard
scripts/
  local-up.sh           One-command local startup: Keychain, assume, Docker Compose, browser
  local-aws-refresh-*   Local-only broker that refreshes AWS speech credentials for Docker
  local-compose.sh      Docker Compose wrapper that injects Keychain-backed local env
  local-down.sh         Stop the local stack through local-compose.sh
  dc-up-keychain.sh     Resolve macOS Keychain secrets, AWS credentials, and start Docker Compose
  run-openai-keychain.sh
                         Run the OpenAI provider with local secrets from macOS Keychain
  keychain-store-secret.sh / keychain-get-secret.sh
                         Store/read local development secrets in macOS Keychain
  dc-up.sh              Legacy AWS credential launcher; prefer dc-up-keychain.sh locally
  aws-upsert-secrets.sh Create/update AWS Secrets Manager values for ECS
  aws-build-push.sh     Build and push the app image to ECR
  aws-deploy-dev.sh     Source local AWS env exports and run Terragrunt apply
  jira-check-board-config.sh
                         Diagnose Jira Agile board-configuration token scopes
  jira-validate-workflow-config.sh
                         Validate Jira status and transition config via Platform APIs
config/
  jira.example.json     Copyable Jira Cloud sync configuration template
docs/
  architecture.md       Core runtime boundaries and extension ownership
  codebase-map.md       Source-to-responsibility map for contributors
  extension-contracts.md
                        Human-readable provider, connector, model, and ledger contracts
  golden-demo.md        Narrow real-stack proof path and pass criteria
  threat-model.md       Trust boundaries, assets, threats, and mitigations
  adrs/                 Architecture decision records
  api/                  API specifications
  planning/             Roadmap and working progress notes
  research/             Research notes for repository process decisions
  security/             Security review and hardening register
evaluation/
  failure-inventory.md  Voice-command replay inventory
  fixtures/             JSON fixtures for evaluation harness tests
examples/
  */                    Integration templates for connectors and providers
infra/
  terragrunt.hcl        Root Terragrunt config for S3 state, DynamoDB locking, AWS provider generation
  live/dev/             Dev ECS/Fargate stack wrapper
  modules/auto-bot/     Reusable AWS Terraform module
public/
  screenshot.png        README screenshot
Dockerfile              Multi-stage build (Go 1.26 + libopus)
docker-compose.yml      LiveKit + app for local Nova Sonic dev
.env.example            Environment variable reference only; do not copy to local .env
```

## Key Files

| File | Purpose |
| --- | --- |
| `cmd/server/board.go` | Shared Kanban board state, card/task model, tool definitions, session instructions, WebSocket broadcast |
| `cmd/server/scrum_tools.go` | Scrum-master meeting state plus advanced task actions: subtasks, estimates, worklogs, links, sprint/rank, components, fix versions, custom fields, remote links, reporter/watchers |
| `cmd/server/meeting_reports.go` | Meeting intelligence report builder: recap, transcript evidence, sprint signals, GitHub/PR hints, setup readiness, and observability |
| `cmd/server/meeting_report_handlers.go` | HTTP handlers for `/post-meeting`, report archives, setup status, observability, provider options, and identity status |
| `cmd/server/meeting_access.go` | Meeting code setup, participant join/leave, host access, and meeting type switching |
| `cmd/server/meeting_intelligence.go` | Confirmation gates, transcript evidence, audit replay, undo, meeting memory, ownership, and scrum briefings |
| `cmd/server/chat_messages.go` | Meeting text normalization and English translation helpers |
| `cmd/server/agent_runs.go` | Autonomous agent-run model, voice tool, Bedrock PM routing, code-review specialist, Jira result publishing |
| `cmd/server/bedrock_agents.go` | AWS Bedrock Claude Messages invocation used by PM and code-review agents |
| `cmd/server/github_app.go` | GitHub App JWT authentication, short-lived installation tokens, read-only PR diff fetch, optional PR review comments |
| `cmd/server/jira.go` | Optional Jira Cloud REST API v3 sync, config loading, core issue mapping, and write-through mutations |
| `cmd/server/jira_ext.go` | Advanced Jira Platform/Agile calls for metadata, transition discovery, worklogs, issue links, sprint assignment, ranking, custom fields, remote links, reporter, and watchers |
| `cmd/server/auth.go` | Token-backed HttpOnly browser sessions and non-browser Bearer auth |
| `cmd/server/board_store.go` | SQLite board snapshot persistence and mutation event history |
| `cmd/server/guardrails.go` | Prompt-injection redaction for model-facing context/tool output and mutating tool argument rejection |
| `cmd/server/voice_status.go` | Voice readiness preflight and recovery guidance for OpenAI, Nova Sonic, AWS, LiveKit, and Bedrock |
| `cmd/server/voice_models.go` | Active voice model status, selectable model options, and local restart integration |
| `cmd/server/workspace.go` | Current workspace, board, room, provider, identity, and connector health scope |
| `cmd/server/kanban.go` | OpenAI Realtime WebRTC transport (peer connection, data channel, tool dispatch) |
| `cmd/server/nova_sonic.go` | Nova Sonic 2 provider (LiveKit agent, Bedrock bidi stream, lazy connect, audio I/O, tool dispatch) |
| `cmd/server/nova_sonic_output.go` | Nova Sonic output audio pacing, pre-roll, queue limits, and metrics |
| `cmd/server/nova_sonic_mixer.go` | 16kHz mono PCM audio mixer for Nova Sonic path |
| `cmd/server/audio_mixer.go` | 48kHz stereo PCM audio mixer for OpenAI path |
| `cmd/server/main.go` | Entry point, provider switch, HTTP/WebSocket server, LiveKit token endpoint |
| `cmd/server/opus_encoder.go` | CGo Opus encoder wrapper |
| `cmd/server/opus_decoder.go` | CGo Opus decoder wrapper |

## Customization

- Initial cards: `initialKanbanBoardCards` in [cmd/server/board.go](cmd/server/board.go)
- Agent instructions: `SessionInstructions()` in [cmd/server/board.go](cmd/server/board.go)
- Tool definitions: `KanbanToolDefs()` in [cmd/server/board.go](cmd/server/board.go)
- OpenAI frontend: [web/index.html](web/index.html)
- Nova Sonic frontend: [web/index_livekit.html](web/index_livekit.html)
- HTTP bind address: `-addr` flag (default `:3000`)

## macOS Local Network Permission

When running the OpenAI path on the same Mac as the browser, set `CONFERENCE_LOOPBACK_ONLY=1`. This restricts the browser-facing Pion ICE agent to the loopback interface, avoiding macOS Local Network privacy blocks on LAN UDP. The OpenAI Realtime peer still uses public network for STUN.

## AWS Deployment

AWS infrastructure lives under [infra](infra/README.md) and uses Terragrunt with S3 remote state and DynamoDB locking in `us-east-1`. The generated Terraform CLI constraint is pinned to `1.15.2`; providers are pinned and locked to `hashicorp/aws = 6.45.0` and `hashicorp/cloudinit = 2.4.0`.

The dev stack deploys:

- VPC `10.20.0.0/16`, the AWS-canonical form of the requested `10.20.21.0/16`, with public subnets starting at `10.20.21.0/24` only for the app ALB, LiveKit NLB, and fck-nat
- ECS Fargate app and optional self-hosted LiveKit services in private subnets with no public task IPs
- Application Load Balancer with AWS WAF managed rules and a rate limit
- Network Load Balancer for self-hosted LiveKit signaling, media, and TURN
- TCP/TLS signaling, TCP fallback, one muxed UDP RTC media listener, embedded TURN/UDP, optional TURN/TLS, and Redis-backed distributed LiveKit routing
- Terraform bit flip from self-hosted LiveKit to LiveKit Cloud through `LIVEKIT_DEPLOYMENT_MODE=cloud` and `LIVEKIT_CLOUD_URL`
- ECR repository for the app image
- CloudWatch log groups and an operations dashboard
- fck-nat module `RaJiska/fck-nat/aws = 1.4.0` for private subnet egress; no AWS NAT Gateway is used
- Private VPC endpoints for S3, ECR, CloudWatch Logs, Secrets Manager, and Bedrock Runtime
- Secrets Manager injection for app, Jira, OpenAI, and LiveKit secrets
- EFS-backed `/srv/data` volume for the app's SQLite board snapshot/event store
- Least-privilege ECS execution/task policies, including narrowed Bedrock model ARNs and EFS access point authorization
- No `APP_LOCAL_LOGIN_TOKEN` in AWS; the local one-click login endpoint is rejected when `APP_ENV` is not `local`

Bootstrap flow:

```bash
# Pin a reviewed fck-nat AMI ID for us-east-1. Do not leave this to a latest lookup.
export FCK_NAT_AMI_ID=ami-xxxxxxxxxxxxxxxxx

# Optional but recommended for self-hosted LiveKit DNS/TLS.
export HOSTED_ZONE_ID=Z123...
export LIVEKIT_DOMAIN_NAME=livekit.example.com
export LIVEKIT_TURN_DOMAIN_NAME=turn.example.com
export LIVEKIT_CERTIFICATE_ARN=arn:aws:acm:us-east-1:...

# Optional LiveKit Cloud switch. Keep the API key/secret env vars set to the Cloud project keys.
# export LIVEKIT_DEPLOYMENT_MODE=cloud
# export LIVEKIT_CLOUD_URL=wss://your-project.livekit.cloud

AWS_REGION=us-east-1 ./scripts/aws-upsert-secrets.sh
set -a; source .env.aws.local; set +a

cd infra/live/dev
terragrunt init
terragrunt apply -target=aws_ecr_repository.app
cd ../../..

./scripts/aws-build-push.sh
./scripts/aws-deploy-dev.sh
```

To discover the current fck-nat AMI for review before pinning it, query AMIs owned by `568608671756` in `us-east-1`, then copy the returned `ImageId` into `FCK_NAT_AMI_ID`. The Terraform module requires that explicit AMI ID so deploys are reproducible.

```bash
aws ec2 describe-images \
  --region us-east-1 \
  --owners 568608671756 \
  --filters 'Name=name,Values=fck-nat-al2023-hvm-*' 'Name=architecture,Values=arm64' \
  --query 'sort_by(Images,&CreationDate)[-1].{ImageId:ImageId,Name:Name,CreationDate:CreationDate}' \
  --output table
```

`scripts/aws-build-push.sh` tags the app image with the current git SHA by default and updates `.env.aws.local` with `APP_IMAGE`. Full deploys fail if `APP_IMAGE` is missing or uses a moving tag.

For TURN/TLS, set `LIVEKIT_TURN_CERTIFICATE_ARN`, keep `LIVEKIT_TURN_DOMAIN_NAME` pointed at the LiveKit NLB, and set the Terraform input `livekit_turn_tls_enabled = true` in the Terragrunt inputs you are deploying. The module defaults TURN/UDP on 443 and keeps TURN/TLS off until both that boolean and a matching certificate ARN/domain are provided.

For Jira in ECS, set `JIRA_API_TOKEN` and `JIRA_CONFIG_JSON_FILE=/absolute/path/to/jira.json` before `aws-upsert-secrets.sh`; that Jira config should use `"api_token_env": "JIRA_API_TOKEN"`.

Self-hosted LiveKit on Fargate is supported for testing and now includes Redis-backed distributed routing plus TURN hooks, but it is still operationally more sensitive than the Go app because WebRTC depends on public UDP/TURN reachability and per-room node capacity. LiveKit Cloud remains a one-variable infrastructure switch when we decide the managed media edge is worth it.

## Security

This application includes several hardening measures:

- **Authentication**: Browser users create an HttpOnly `SameSite=Lax` session by entering `APP_API_TOKEN`; the token is not rendered into HTML or stored in JavaScript. `/websocket` and `/livekit-token` reject unauthenticated requests. Bearer auth remains available for non-browser automation.
- **Room/board authorization**: Requests are bound to configured `APP_ROOM_ID` and `APP_BOARD_ID`; LiveKit grants are minted only for that room and WebSocket broadcasts are scoped by board ID.
- **Meeting access codes**: Participant join codes use 80 bits of entropy, are scoped to the active meeting, and the unauthenticated join path is rate-limited.
- **LiveKit secret safety**: Production mode refuses missing LiveKit credentials and refuses the `devkey`/`secret` development pair.
- **Durable board state**: Set `BOARD_SQLITE_PATH` to persist board snapshots and event history. Docker Compose mounts `/srv/data`; AWS mounts that path on EFS for Fargate.
- **Durable meeting reports**: Ended meetings archive post-meeting intelligence reports when the board store supports report persistence.
- **WebSocket origin validation**: Same-origin by default; configurable via `--allowed-origins` flag.
- **HTTP security headers**: CSP, X-Frame-Options DENY, X-Content-Type-Options nosniff, Referrer-Policy.
- **HTTP server timeouts**: Read, write, idle, and header timeouts are all set to prevent slowloris attacks.
- **WebSocket limits**: 64KB max message size, 100 max concurrent connections.
- **Rate limiting**: Per-client fixed-window limits on WebSocket upgrades, participant join attempts, and LiveKit token minting.
- **Audit trail**: Board mutations and Jira refreshes are logged as structured audit events; Jira write-through confirmation is stored separately from local state changes; set `AUDIT_LOG_PATH` to also write JSONL.
- **Undo and replay**: Voice/UI mutations keep a bounded in-memory audit history with before/after board state, transcript evidence, external API confirmation, undo support, and UI replay controls.
- **Confirmation gates**: Medium/high-risk Jira actions create pending confirmations instead of writing immediately.
- **Jira webhook conflicts**: Authenticated Jira webhooks refresh the board and surface local-vs-Jira conflicts for human resolution.
- **Prompt-injection guardrails**: Jira/task text is treated as untrusted data, not instructions. Model-facing board context and tool outputs redact detected prompt-injection payloads, tool arguments are scanned before mutation, and tool schemas tell the model that only live user speech can authorize Jira changes.
- **Input validation**: Identity parameters validated to `[a-zA-Z0-9_-]{1,64}`. Card titles, notes, and tags have size caps.
- **Non-root container**: Docker image runs as `appuser`, not root.
- **LiveKit media hardening**: AWS self-host mode uses private ECS tasks, NLB edge listeners, Redis distributed routing, embedded TURN/UDP hooks, optional TURN/TLS, and CloudWatch dashboarding. LiveKit Cloud can be enabled by Terraform inputs without changing app code.
- **Supply chain**: All Docker images pinned to `@sha256:` digests, Debian packages pinned to exact versions, Terraform providers locked with `.terraform.lock.hcl`, CDN scripts use SRI, and Go modules are verified via `go.sum`.
- **Pre-commit hook**: Runs `go vet`, `goimports`, `govulncheck`, Docker digest checks, SRI checks, and secrets scanning before every commit.
- **No sensitive logging**: SDP, ICE candidates, transcripts, and tool arguments are redacted from logs.

See [docs/security/application-security-review.md](docs/security/application-security-review.md) for the full security audit and remediation details.

> [!IMPORTANT]
> While hardened, this is still a shared-token demo boundary. For production, put OIDC/Cognito in front of room membership and use per-user authorization instead of a shared bootstrap token.

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
