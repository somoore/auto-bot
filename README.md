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

### Option A: OpenAI Realtime (local, no Docker)

```bash
# Prerequisites
brew install go opus pkg-config

# Run
CONFERENCE_LOOPBACK_ONLY=1 \
APP_ENV=local \
APP_API_TOKEN=local-dev-only-change-me \
OPENAI_API_KEY=sk-... \
go run ./cmd/server/
```

Open [http://localhost:3000](http://localhost:3000), click **Join room**, and start talking.

### Option B: Nova Sonic 2 (Docker Compose)

```bash
# Prerequisites: Docker, AWS CLI with granted credential-process

# 1. Authenticate with AWS
assume test_AccountA/AdministratorAccess

# 2. Start the stack (resolves credentials and passes them to Docker)
./scripts/dc-up.sh --build -d

# Or manually export credentials and run docker compose:
export AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=... AWS_SESSION_TOKEN=...
docker compose up --build -d
```

Open [http://localhost:3001](http://localhost:3001), click **Join room**, grant microphone access, and start talking. The LiveKit SFU runs on port 7880, the app on port 3001.

The Docker Compose stack is explicitly `APP_ENV=local` and uses `local-dev-only-change-me` as the default app access token. Change `APP_API_TOKEN` before sharing the server with anyone else.

For a local end-to-end stack with Jira sync, copy [config/jira.example.json](config/jira.example.json), set `JIRA_CONFIG_PATH=/srv/config/<your-file>.json` or set `JIRA_CONFIG_JSON`, export `JIRA_API_TOKEN`, and run Docker Compose. The app container mounts `./config` read-only at `/srv/config`.

### Option C: Nova Sonic 2 (local, no Docker)

```bash
# Prerequisites
brew install go opus pkg-config

# Start LiveKit dev server in a separate terminal
docker run --rm -p 7880:7880 -p 7881:7881 -p 7882-7892:7882-7892/udp \
  livekit/livekit-server:latest --dev --bind 0.0.0.0 --node-ip 127.0.0.1

# Authenticate and run the app
assume test_AccountA/AdministratorAccess
VOICE_PROVIDER=nova-sonic \
APP_ENV=local \
APP_API_TOKEN=local-dev-only-change-me \
AWS_REGION=us-east-1 \
LIVEKIT_API_KEY=devkey \
LIVEKIT_API_SECRET=secret \
go run ./cmd/server/
```

### Optional: Jira Sync

Create a Jira Cloud API token, save it in a local file or expose it with `JIRA_API_TOKEN`, copy [config/jira.example.json](config/jira.example.json), and fill in your Jira base URL, email, project key, status mappings, and transition IDs. Scoped API tokens should use the Atlassian API gateway base URL: `https://api.atlassian.com/ex/jira/<cloud-id>`. Then start the app with:

```bash
JIRA_CONFIG_PATH=/absolute/path/to/jira.json \
OPENAI_API_KEY=sk-... \
go run ./cmd/server/
```

When `JIRA_CONFIG_PATH` or `JIRA_CONFIG_JSON` is set, the server loads the initial board from Jira using the configured JQL and writes board mutations back to Jira: create issues/sub-tasks, transition issues, update summary/description, append notes, add comments, add/remove labels, assign/unassign issues, set reporter/watchers, set due dates/ETAs, set priority, set story points and time estimates, add worklogs, link issues, assign sprint, rank backlog/sprint issues, set components/fix versions/custom fields, attach remote links, mark blocked, and close/cancel for deletes. Keep the API token outside the repo; prefer `api_token_file`, `api_token_command`, or `api_token_env` in the config.

Jira writes are constrained to the configured `project_key`. Existing-issue mutations refuse issue keys outside that project, newly created Jira issue keys are verified before local cards are renamed, and startup hydration fails if the configured JQL returns issues from another project. This keeps a bad voice command or overly broad JQL from touching someone else's Jira board.

For Jira Cloud scoped API tokens, the base issue sync path needs `read:jira-work` and `write:jira-work`. The assignable-user picker, reporter setting, and watcher search need Jira user-read scope; without it, ticket writes can work while `search_jira_users` returns a Jira scope error. Worklogs and issue links are covered by the classic `write:jira-work` scope or granular worklog/link scopes. Jira Software sprint/rank APIs additionally require Jira Software scopes such as `write:sprint:jira-software` for moving issues to a sprint and `write:issue:jira-software` for ranking issues. A true `Blocked` column also requires the Jira workflow to contain a Blocked status and a configured transition. When a workflow lacks that status, `blocked_flag_field`/`blocked_flag_value` can map Jira Software's Flagged/Impediment field to the local Blocked column and keep blocked work visible.

Optional advanced fields in [config/jira.example.json](config/jira.example.json) enable richer scrum-master behavior: `board_id` for sprint discovery, `story_points_field`, `sprint_field`, `epic_link_field`, `rank_custom_field_id`, and `custom_field_mappings` for named custom fields. Use the voice tool `get_jira_metadata` or Jira field discovery to confirm these IDs for your site before relying on live write-through.

The Jira Software Agile board APIs use a separate scope family from the platform issue APIs. If you want to read board column metadata through `/rest/agile/1.0/board/{boardId}/configuration`, create a scoped token that also includes `read:board-scope:jira-software` and `read:issue-details:jira`. Add `read:project:jira` if you need to discover/list boards instead of checking a known board ID. You can verify a token with:

```bash
JIRA_API_TOKEN=... \
scripts/jira-check-board-config.sh config/jira.local.json 1
```

Current sync does not depend on that Agile endpoint. For scoped API tokens, validate the working issue/status/transition path with Jira Platform APIs instead:

```bash
JIRA_API_TOKEN=... \
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

The agent also responds to implicit status updates during standup — if someone says "I finished X", it moves the matching card to Done automatically. In structured meetings it can start/end meetings, track participants, move to the next speaker, record blockers/risks/action items, summarize the meeting, and keep Jira synchronized.

## Multi-Party Video & Layout Modes

The Nova Sonic frontend (LiveKit) supports multi-party video conferencing. All participants see each other's webcam feeds, hear each other, and interact with the shared AI agent. Active speakers are highlighted in real time.

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

The app is fully responsive: desktop, tablet, and mobile. On smaller screens, layouts collapse to stacked views, resize handles are hidden, and controls remain accessible without scrolling. The transcription panel auto-scrolls in place within a fixed viewport — the page itself never scrolls.

## Environment Variables

| Variable | Default | Provider | Purpose |
| --- | --- | --- | --- |
| `VOICE_PROVIDER` | `openai` | Both | `openai` or `nova-sonic` |
| `APP_ENV` | `production` | Both | Runtime safety mode. `production` rejects disabled auth and LiveKit dev credentials; Compose sets `local`. |
| `APP_AUTH_MODE` | `token` | Both | `token` for HttpOnly browser sessions/Bearer auth; `disabled` is only allowed with `APP_ENV=local`. |
| `APP_API_TOKEN` | _(required unless auth disabled)_ | Both | Shared bootstrap token for creating browser sessions and for non-browser Bearer auth. Never injected into served HTML. |
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
| `NOVA_SONIC_MODEL` | `amazon.nova-sonic-v1:0` | Nova Sonic | Bedrock model ID |
| `NOVA_SONIC_VOICE` | `matthew` | Nova Sonic | TTS voice ID |
| `LIVEKIT_URL` | `ws://localhost:7880` | Nova Sonic | LiveKit server WebSocket URL |
| `LIVEKIT_BROWSER_URL` | _derived_ | Nova Sonic | Browser-facing LiveKit URL returned by `/livekit-token`; useful when server-internal `LIVEKIT_URL` is not browser reachable. |
| `LIVEKIT_API_KEY` | _(required)_ | Nova Sonic | LiveKit API key (no default — must be set) |
| `LIVEKIT_API_SECRET` | _(required)_ | Nova Sonic | LiveKit API secret (no default — must be set) |
| `JIRA_CONFIG_PATH` | _unset_ | Both | Optional Jira Cloud sync configuration JSON |
| `JIRA_CONFIG_JSON` | _unset_ | Both | Optional inline Jira Cloud sync configuration JSON; useful for AWS Secrets Manager injection |
| `JIRA_API_TOKEN` | _unset_ | Both | Optional Jira API token when the config uses `"api_token_env": "JIRA_API_TOKEN"` |
| `AUDIT_LOG_PATH` | _unset_ | Both | Optional JSONL file for board mutation and Jira refresh audit events |
| `TRUST_PROXY_HEADERS` | _unset_ | Both | Set to `1` behind a trusted reverse proxy so rate limiting uses forwarded client IP headers |

See [.env.example](.env.example) for a copyable template.

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

The Nova Sonic Bedrock stream is **lazy** — it only starts when the first participant joins the room and auto-restarts if the stream ends or errors.

## Project Layout

```
cmd/
  server/               Go source (single binary, package main)
    main.go             Entry point, provider switch, HTTP/WebSocket server
    board.go            Shared Kanban board state, card CRUD, tools, instructions
    scrum_tools.go      Scrum-master meeting tools and advanced local Jira metadata actions
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
scripts/
  dc-up.sh              Resolve AWS credentials and start Docker Compose
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
.env.example            All env vars documented
plan.md                 Project roadmap
```

## Key Files

| File | Purpose |
| --- | --- |
| `cmd/server/board.go` | Shared Kanban board state, card/task model, tool definitions, session instructions, WebSocket broadcast |
| `cmd/server/scrum_tools.go` | Scrum-master meeting state plus advanced task actions: subtasks, estimates, worklogs, links, sprint/rank, components, fix versions, custom fields, remote links, reporter/watchers |
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

AWS infrastructure lives under [infra](infra/README.md) and uses Terragrunt with S3 remote state and DynamoDB locking in `us-east-1`. The generated Terraform provider is pinned to `hashicorp/aws = 6.45.0`.

The dev stack deploys:

- ECS Fargate app service behind an Application Load Balancer
- ECS Fargate LiveKit service behind a Network Load Balancer
- TCP signaling, TCP fallback, and one muxed UDP RTC media listener for LiveKit
- ECR repository for the app image
- CloudWatch log groups
- Secrets Manager injection for app, Jira, OpenAI, and LiveKit secrets
- EFS-backed `/srv/data` volume for the app's SQLite board snapshot/event store
- Task role access to Bedrock for Nova Sonic

Bootstrap flow:

```bash
AWS_REGION=us-east-1 ./scripts/aws-upsert-secrets.sh
set -a; source .env.aws.local; set +a

cd infra/live/dev
terragrunt init
terragrunt apply -target=aws_ecr_repository.app
cd ../../..

./scripts/aws-build-push.sh
./scripts/aws-deploy-dev.sh
```

For Jira in ECS, set `JIRA_API_TOKEN` and `JIRA_CONFIG_JSON_FILE=/absolute/path/to/jira.json` before `aws-upsert-secrets.sh`; that Jira config should use `"api_token_env": "JIRA_API_TOKEN"`.

Self-hosted LiveKit on Fargate is supported for testing, but it is operationally more sensitive than the Go app because WebRTC requires public UDP reachability. LiveKit Cloud remains the lower-ops path until self-hosting is validated for your network profile.

## Security

This application includes several hardening measures:

- **Authentication**: Browser users create an HttpOnly `SameSite=Lax` session by entering `APP_API_TOKEN`; the token is not rendered into HTML or stored in JavaScript. `/websocket` and `/livekit-token` reject unauthenticated requests. Bearer auth remains available for non-browser automation.
- **Room/board authorization**: Requests are bound to configured `APP_ROOM_ID` and `APP_BOARD_ID`; LiveKit grants are minted only for that room and WebSocket broadcasts are scoped by board ID.
- **LiveKit secret safety**: Production mode refuses missing LiveKit credentials and refuses the `devkey`/`secret` development pair.
- **Durable board state**: Set `BOARD_SQLITE_PATH` to persist board snapshots and event history. Docker Compose mounts `/srv/data`; AWS mounts that path on EFS for Fargate.
- **WebSocket origin validation**: Same-origin by default; configurable via `--allowed-origins` flag.
- **HTTP security headers**: CSP, X-Frame-Options DENY, X-Content-Type-Options nosniff, Referrer-Policy.
- **HTTP server timeouts**: Read, write, idle, and header timeouts are all set to prevent slowloris attacks.
- **WebSocket limits**: 64KB max message size, 100 max concurrent connections.
- **Rate limiting**: Per-client fixed-window limits on WebSocket upgrades and LiveKit token minting.
- **Audit trail**: Board mutations and Jira refreshes are logged as structured audit events; set `AUDIT_LOG_PATH` to also write JSONL.
- **Prompt-injection guardrails**: Jira/task text is treated as untrusted data, not instructions. Model-facing board context and tool outputs redact detected prompt-injection payloads, tool arguments are scanned before mutation, and tool schemas tell the model that only live user speech can authorize Jira changes.
- **Input validation**: Identity parameters validated to `[a-zA-Z0-9_-]{1,64}`. Card titles, notes, and tags have size caps.
- **Non-root container**: Docker image runs as `appuser`, not root.
- **Supply chain**: All Docker images pinned to `@sha256:` digests, CDN scripts use SRI, Go modules verified via `go.sum`.
- **Pre-commit hook**: Runs `go vet`, `goimports`, `govulncheck`, Docker digest checks, SRI checks, and secrets scanning before every commit.
- **No sensitive logging**: SDP, ICE candidates, transcripts, and tool arguments are redacted from logs.

See [scan.md](scan.md) for the full security audit and remediation details.

> [!IMPORTANT]
> While hardened, this is still a shared-token demo boundary. For production, put OIDC/Cognito in front of room membership and use per-user authorization instead of a shared bootstrap token.

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
