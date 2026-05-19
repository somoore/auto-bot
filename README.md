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

> [!IMPORTANT]
> Browser access is protected by an HttpOnly session cookie. The page never receives `APP_API_TOKEN`; users enter it once to create a session, and `/websocket` plus `/livekit-token` require that session or a Bearer token. For public multi-user use, put OIDC/Cognito in front of this shared-token flow.

## Quickstart

Local secrets belong in macOS Keychain. Do not create a project `.env` file for local development.

For the normal local stack, use the one-command launcher:

```bash
cd /Users/scottmoore/github/auto-bot
scripts/local-up.sh
```

It generates missing local app/login/webhook secrets, stores them in macOS Keychain, prompts once for the Jira token if it is not already stored, forces AWS to `us-east-1`, runs `assume test_AccountA/AdministratorAccess`, starts Docker Compose, opens a local-only login URL to set the HttpOnly session cookie, and redirects to [http://localhost:3001](http://localhost:3001).

Use the local Compose wrapper for routine stack commands:

```bash
scripts/local-compose.sh ps
scripts/local-compose.sh logs -f app livekit
scripts/local-down.sh
```

Recommended Keychain service names:

| Secret | Service | Account |
| --- | --- | --- |
| Local app/session bootstrap token | `auto-bot/app-api-token` | your macOS user |
| Local one-click login token | `auto-bot/local-login-token` | your macOS user |
| OpenAI API key | `auto-bot/openai-api-key` | your macOS user |
| Jira API token | `auto-bot/jira-api-token` | your Jira email |
| Jira webhook secret | `auto-bot/jira-webhook-secret` | your macOS user |

Store secrets with:

```bash
scripts/keychain-store-secret.sh auto-bot/app-api-token "$USER"
scripts/keychain-store-secret.sh auto-bot/jira-api-token somoore2025@gmail.com

# Only needed for the OpenAI provider:
scripts/keychain-store-secret.sh auto-bot/openai-api-key "$USER"

# Optional, for local webhook testing:
scripts/keychain-store-secret.sh auto-bot/jira-webhook-secret "$USER"
```

Read the fallback app access token when you need to enter it manually:

```bash
scripts/keychain-get-secret.sh auto-bot/app-api-token "$USER"
```

### Option A: OpenAI Realtime (local, no Docker)

```bash
# Prerequisites
brew install go opus pkg-config

# Run
scripts/run-openai-keychain.sh
```

Open [http://localhost:3000](http://localhost:3000), click **Join room**, and start talking.

### Option B: Nova Sonic 2 (Docker Compose)

```bash
# Prerequisites: Docker, AWS CLI with granted credential-process, Keychain secrets above

# Optional if your AWS profile differs from the repo default:
export AWS_PROFILE=test_AccountA/AdministratorAccess

# Start the stack. Prefer the one-command launcher:
./scripts/local-up.sh
```

Open [http://localhost:3001](http://localhost:3001). As host, select **Host**, choose the meeting type, click **Create meeting code**, then click **Join room** and grant microphone access. Participants select **Participant**, enter the generated meeting code, and click **Join room**. Participant browsers do not need the app access token; the server creates their session only after the meeting code is accepted. The LiveKit SFU runs on port 7880, the app on port 3001.

The **Join room** button performs a server-side voice readiness preflight before entering LiveKit. For Nova Sonic it validates AWS credentials with STS in `us-east-1` and makes sure the server-side Nova participant has joined the room. If credentials are missing or expired, the browser will stay out of the room and show the `scripts/local-up.sh` recovery command instead of leaving you alone in an empty meeting.

The Docker Compose stack is explicitly `APP_ENV=local`. `APP_API_TOKEN` is required and should come from Keychain through `scripts/dc-up-keychain.sh`; the app token is not baked into the image or served to the browser. `scripts/local-up.sh` also provisions a separate local-only `APP_LOCAL_LOGIN_TOKEN` and opens `/auth/local-login` to set the HttpOnly session cookie without prompting. That endpoint only exists in local mode; production rejects `APP_LOCAL_LOGIN_TOKEN`.

For a local end-to-end stack with Jira sync, use [config/jira.local.json](config/jira.local.json) and store the Jira token in Keychain as `auto-bot/jira-api-token` for account `somoore2025@gmail.com`. The Keychain launcher sets `JIRA_CONFIG_PATH=/srv/config/jira.local.json` automatically when that token is present. The app container mounts `./config` read-only at `/srv/config`.

To run the UI, board, and Jira control plane without AWS voice credentials:

```bash
AUTO_BOT_SKIP_AWS=1 ./scripts/dc-up-keychain.sh --build -d
```

That mode is for board/Jira testing only. Nova Sonic meeting join is intentionally blocked until the stack is restarted with fresh AWS credentials.

### Option C: Nova Sonic 2 (local, no Docker)

```bash
# Prerequisites
brew install go opus pkg-config

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

The checked-in local Jira config [config/jira.local.json](config/jira.local.json) already contains the non-secret EMAL site/project mapping and reads the token from `JIRA_API_TOKEN`. Local launchers load that token from Keychain and set `JIRA_API_TOKEN` only for the child process/container.

When `JIRA_CONFIG_PATH` or `JIRA_CONFIG_JSON` is set, the server loads the initial board from Jira using the configured JQL and writes board mutations back to Jira: create issues/sub-tasks, transition issues, update summary/description, append notes, add comments, add/remove labels, assign/unassign issues, set reporter/watchers, set due dates/ETAs, set priority, set story points and time estimates, add worklogs, link issues, assign sprint, rank backlog/sprint issues, set components/fix versions/custom fields, attach remote links, mark blocked, and close/cancel for deletes. Keep the API token outside the repo; prefer `api_token_file`, `api_token_command`, or `api_token_env` in the config.

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
| "Set component to Voice Agent and fix version to scrum-master-mvp" | Updates Jira planning metadata |
| "Delete the old task" | Removes a ticket from the board |

The agent also responds to implicit status updates during standup — if someone says "I finished X", it moves the matching card to Done automatically. In structured meetings it can start/end meetings, deliver a 60-second opening briefing, track participants, move to the next speaker, record blockers/risks/decisions/action items/follow-ups/parking-lot topics, track ownership, summarize the meeting, and keep Jira synchronized.

Meeting types are explicit. The host can start as a general meeting, standup, 1:1, sprint review, or open-ended meeting, and can switch modes in the UI or by asking the agent during the meeting. The agent is instructed to switch modes only from live host/facilitator speech or after host confirmation.

Medium-risk Jira actions such as assignment, ETA, priority, and reporter changes create a pending confirmation before they write. High-risk actions such as delete/close, sprint moves, and ranking changes also require explicit confirmation. The UI exposes **Undo** and **Audit** controls; audit replay shows the mutation, before/after context, and captured transcript evidence when available.

## Multi-Party Video & Layout Modes

The Nova Sonic frontend (LiveKit) supports multi-party video conferencing. All participants see each other's webcam feeds, hear each other, and interact with the shared AI agent. Active speakers are highlighted in real time.

The right-side operator panel includes the meeting code, Meeting Control Center, Voice Reliability Dashboard, agent confidence evidence, validation checklist, and a Slack-ready executive recap. It surfaces who has and has not spoken, blockers, decisions, action items, pending confirmations, Jira mutations, mic/LiveKit/Nova/Bedrock/transcription/Jira health, and precise failure states such as expired AWS credentials, missing agent participant, blocked mic permission, rejected Jira scope, or LiveKit audio-track problems.

## Meeting Intelligence

The scrum-master layer now has a durable post-meeting intelligence path. During and after a meeting the backend can generate a report with agenda, participants, decisions, risks, action items, parking-lot items, follow-ups, unresolved blockers, ownership, transcript evidence, Jira mutations, sprint risk signals, GitHub/PR hints, setup readiness, and voice/LiveKit/Jira observability.

Open [http://localhost:3001/post-meeting](http://localhost:3001/post-meeting) after a local meeting, or click **Intelligence** in the LiveKit operator panel. The page includes:

- Slack-ready executive recap
- Jira changes summary and audit evidence
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
| `GET /meetings?limit=50` | Archived report summaries plus current summary |
| `GET /setup/status` | Setup/admin readiness checks |
| `GET /observability/status` | Current voice/Jira/storage/meeting observability |
| `GET /voice/providers` | Active and available full-duplex speech provider options |
| `GET /identity/status` | Current identity, role, and meeting permissions |

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
| `APP_ROOM_ID` | `kanban-meeting` | Both | Authorized room ID for this deployment. |
| `APP_BOARD_ID` | `default` | Both | Authorized board ID for this deployment. |
| `APP_BASE_URL` | _(auto-detect)_ | Both | Override WebSocket base URL (e.g., `wss://example.com/websocket`) |
| `BOARD_SQLITE_PATH` | _unset_ | Both | Optional SQLite path for durable board snapshots and event history. Compose uses `/srv/data/board.sqlite`. |
| `OPENAI_API_KEY` | _(required)_ | OpenAI | Auth for the OpenAI Realtime API |
| `OPENAI_REALTIME_MODEL` | `gpt-realtime-2` | OpenAI | Realtime model to use |
| `CONFERENCE_LOOPBACK_ONLY` | _unset_ | OpenAI | When `1`, restrict browser ICE to loopback (macOS same-machine) |
| `PION_NAT1TO1_IP` | _unset_ | OpenAI | Advertise this IP as host ICE candidate |
| `AWS_ACCESS_KEY_ID` | _unset_ | Nova Sonic | Explicit AWS credentials (preferred in Docker) |
| `AWS_SECRET_ACCESS_KEY` | _unset_ | Nova Sonic | Explicit AWS credentials |
| `AWS_SESSION_TOKEN` | _unset_ | Nova Sonic | Session token for assumed roles |
| `AWS_PROFILE` | `test_AccountA/AdministratorAccess` | Nova Sonic | AWS shared config profile (used when explicit keys not set) |
| `AWS_REGION` | `us-east-1` | Nova Sonic | Bedrock region |
| `NOVA_SONIC_MODEL` | `amazon.nova-2-sonic-v1:0` | Nova Sonic | Bedrock model ID |
| `NOVA_SONIC_VOICE` | `matthew` | Nova Sonic | TTS voice ID |
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

The Nova Sonic Bedrock stream is **lazy** — it only starts when the first participant joins the room and auto-restarts if the stream ends or errors. After each board mutation, Nova Sonic receives a sanitized board-context refresh with the latest sequence number, and that refresh is explicitly framed as untrusted application data so Jira/task text cannot become instructions.

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
    jira.go             Jira config, hydration, core issue write-through
    jira_ext.go         Advanced Jira metadata, Agile, worklog, link, and custom-field calls
    auth.go             HttpOnly session and Bearer auth
    board_store.go      SQLite board snapshots and event history
    guardrails.go       Prompt-injection redaction and tool-argument guardrails
    rate_limiter.go     Fixed-window HTTP/WebSocket rate limits
    audit.go            Structured board/Jira audit events
    kanban.go           OpenAI Realtime WebRTC transport
    nova_sonic.go       Nova Sonic 2 provider (LiveKit + Bedrock)
    nova_sonic_mixer.go 16kHz mono PCM mixer for Nova Sonic
    audio_mixer.go      48kHz stereo PCM mixer for OpenAI
    opus_encoder.go     CGo Opus encoder
    opus_decoder.go     CGo Opus decoder
web/
  index.html            OpenAI frontend (Pion WebRTC)
  index_livekit.html    Nova Sonic frontend (LiveKit SDK, multi-party video, layout modes, transcription)
  post_meeting.html     Post-meeting intelligence dashboard
scripts/
  local-up.sh           One-command local startup: Keychain, assume, Docker Compose, browser
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
infra/
  terragrunt.hcl        Root Terragrunt config for S3 state, DynamoDB locking, AWS provider generation
  live/dev/             Dev ECS/Fargate stack wrapper
  modules/auto-bot/     Reusable AWS Terraform module
public/
  screenshot.png        README screenshot
Dockerfile              Multi-stage build (Go 1.26 + libopus)
docker-compose.yml      LiveKit + app for local Nova Sonic dev
.env.example            Environment variable reference only; do not copy to local .env
plan.md                 Project roadmap
```

## Key Files

| File | Purpose |
| --- | --- |
| `cmd/server/board.go` | Shared Kanban board state, card/task model, tool definitions, session instructions, WebSocket broadcast |
| `cmd/server/scrum_tools.go` | Scrum-master meeting state plus advanced task actions: subtasks, estimates, worklogs, links, sprint/rank, components, fix versions, custom fields, remote links, reporter/watchers |
| `cmd/server/meeting_reports.go` | Meeting intelligence report builder: recap, transcript evidence, sprint signals, GitHub/PR hints, setup readiness, and observability |
| `cmd/server/meeting_report_handlers.go` | HTTP handlers for `/post-meeting`, report archives, setup status, observability, provider options, and identity status |
| `cmd/server/jira.go` | Optional Jira Cloud REST API v3 sync, config loading, core issue mapping, and write-through mutations |
| `cmd/server/jira_ext.go` | Advanced Jira Platform/Agile calls for metadata, transition discovery, worklogs, issue links, sprint assignment, ranking, custom fields, remote links, reporter, and watchers |
| `cmd/server/auth.go` | Token-backed HttpOnly browser sessions and non-browser Bearer auth |
| `cmd/server/board_store.go` | SQLite board snapshot persistence and mutation event history |
| `cmd/server/guardrails.go` | Prompt-injection redaction for model-facing context/tool output and mutating tool argument rejection |
| `cmd/server/kanban.go` | OpenAI Realtime WebRTC transport (peer connection, data channel, tool dispatch) |
| `cmd/server/nova_sonic.go` | Nova Sonic 2 provider (LiveKit agent, Bedrock bidi stream, lazy connect, audio I/O, tool dispatch) |
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

AWS infrastructure lives under [infra](infra/README.md) and uses Terragrunt with S3 remote state and DynamoDB locking in `us-east-1`. The generated Terraform providers are pinned to `hashicorp/aws = 6.45.0` and `hashicorp/cloudinit = 2.4.0`.

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

For TURN/TLS, also set `LIVEKIT_TURN_CERTIFICATE_ARN` and keep `LIVEKIT_TURN_DOMAIN_NAME` pointed at the LiveKit NLB. The module defaults TURN/UDP on 443 and keeps TURN/TLS off until a matching certificate ARN is provided.

For Jira in ECS, set `JIRA_API_TOKEN` and `JIRA_CONFIG_JSON_FILE=/absolute/path/to/jira.json` before `aws-upsert-secrets.sh`; that Jira config should use `"api_token_env": "JIRA_API_TOKEN"`.

Self-hosted LiveKit on Fargate is supported for testing and now includes Redis-backed distributed routing plus TURN hooks, but it is still operationally more sensitive than the Go app because WebRTC depends on public UDP/TURN reachability and per-room node capacity. LiveKit Cloud remains a one-variable infrastructure switch when we decide the managed media edge is worth it.

## Security

This application includes several hardening measures:

- **Authentication**: Browser users create an HttpOnly `SameSite=Lax` session by entering `APP_API_TOKEN`; the token is not rendered into HTML or stored in JavaScript. `/websocket` and `/livekit-token` reject unauthenticated requests. Bearer auth remains available for non-browser automation.
- **Room/board authorization**: Requests are bound to configured `APP_ROOM_ID` and `APP_BOARD_ID`; LiveKit grants are minted only for that room and WebSocket broadcasts are scoped by board ID.
- **LiveKit secret safety**: Production mode refuses missing LiveKit credentials and refuses the `devkey`/`secret` development pair.
- **Durable board state**: Set `BOARD_SQLITE_PATH` to persist board snapshots and event history. Docker Compose mounts `/srv/data`; AWS mounts that path on EFS for Fargate.
- **Durable meeting reports**: Ended meetings archive post-meeting intelligence reports when the board store supports report persistence.
- **WebSocket origin validation**: Same-origin by default; configurable via `--allowed-origins` flag.
- **HTTP security headers**: CSP, X-Frame-Options DENY, X-Content-Type-Options nosniff, Referrer-Policy.
- **HTTP server timeouts**: Read, write, idle, and header timeouts are all set to prevent slowloris attacks.
- **WebSocket limits**: 64KB max message size, 100 max concurrent connections.
- **Rate limiting**: Per-client fixed-window limits on WebSocket upgrades and LiveKit token minting.
- **Audit trail**: Board mutations and Jira refreshes are logged as structured audit events; set `AUDIT_LOG_PATH` to also write JSONL.
- **Undo and replay**: Voice/UI mutations keep a bounded in-memory audit history with before/after board state, transcript evidence, undo support, and UI replay controls.
- **Confirmation gates**: Medium/high-risk Jira actions create pending confirmations instead of writing immediately.
- **Jira webhook conflicts**: Authenticated Jira webhooks refresh the board and surface local-vs-Jira conflicts for human resolution.
- **Prompt-injection guardrails**: Jira/task text is treated as untrusted data, not instructions. Model-facing board context and tool outputs redact detected prompt-injection payloads, tool arguments are scanned before mutation, and tool schemas tell the model that only live user speech can authorize Jira changes.
- **Input validation**: Identity parameters validated to `[a-zA-Z0-9_-]{1,64}`. Card titles, notes, and tags have size caps.
- **Non-root container**: Docker image runs as `appuser`, not root.
- **LiveKit media hardening**: AWS self-host mode uses private ECS tasks, NLB edge listeners, Redis distributed routing, embedded TURN/UDP hooks, optional TURN/TLS, and CloudWatch dashboarding. LiveKit Cloud can be enabled by Terraform inputs without changing app code.
- **Supply chain**: All Docker images pinned to `@sha256:` digests, CDN scripts use SRI, Go modules verified via `go.sum`.
- **Pre-commit hook**: Runs `go vet`, `goimports`, `govulncheck`, Docker digest checks, SRI checks, and secrets scanning before every commit.
- **No sensitive logging**: SDP, ICE candidates, transcripts, and tool arguments are redacted from logs.

See [scan.md](scan.md) for the full security audit and remediation details.

> [!IMPORTANT]
> While hardened, this is still a shared-token demo boundary. For production, put OIDC/Cognito in front of room membership and use per-user authorization instead of a shared bootstrap token.

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
