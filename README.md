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
> This demo does not include built-in authentication or access control. While the server is running, anyone who can reach the app URL can join and access the meeting room.

## Quickstart

### Option A: OpenAI Realtime (local, no Docker)

```bash
# Prerequisites
brew install go opus pkg-config

# Run
CONFERENCE_LOOPBACK_ONLY=1 \
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
AWS_REGION=us-east-1 \
go run ./cmd/server/
```

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
| "Delete the old task" | Removes a ticket from the board |

The agent also responds to implicit status updates during standup — if someone says "I finished X", it moves the matching card to Done automatically.

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
| `APP_API_TOKEN` | _(auto-generated)_ | Both | Auth token for `/livekit-token` endpoint; auto-generated per session if unset |
| `APP_BASE_URL` | _(auto-detect)_ | Both | Override WebSocket base URL (e.g., `wss://example.com/websocket`) |
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
| `LIVEKIT_API_KEY` | _(required)_ | Nova Sonic | LiveKit API key (no default — must be set) |
| `LIVEKIT_API_SECRET` | _(required)_ | Nova Sonic | LiveKit API secret (no default — must be set) |

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
| `cmd/server/board.go` | Shared Kanban board state, card CRUD, tool definitions (create/move/update/delete/show/close), session instructions, WebSocket broadcast |
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

## Security

This application includes several hardening measures:

- **Authentication**: The `/livekit-token` endpoint requires an `APP_API_TOKEN` (auto-generated per session if unset). The token is injected into the page at render time.
- **WebSocket origin validation**: Same-origin by default; configurable via `--allowed-origins` flag.
- **HTTP security headers**: CSP, X-Frame-Options DENY, X-Content-Type-Options nosniff, Referrer-Policy.
- **HTTP server timeouts**: Read, write, idle, and header timeouts are all set to prevent slowloris attacks.
- **WebSocket limits**: 64KB max message size, 100 max concurrent connections.
- **Input validation**: Identity parameters validated to `[a-zA-Z0-9_-]{1,64}`. Card titles, notes, and tags have size caps.
- **Non-root container**: Docker image runs as `appuser`, not root.
- **Supply chain**: All Docker images pinned to `@sha256:` digests, CDN scripts use SRI, Go modules verified via `go.sum`.
- **Pre-commit hook**: Runs `go vet`, `goimports`, `govulncheck`, Docker digest checks, SRI checks, and secrets scanning before every commit.
- **No sensitive logging**: SDP, ICE candidates, transcripts, and tool arguments are redacted from logs.

See [scan.md](scan.md) for the full security audit and remediation details.

> [!IMPORTANT]
> While hardened, this is a demo application. For production deployment, add TLS termination, a real identity provider, and secrets management (e.g., AWS Secrets Manager).

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
