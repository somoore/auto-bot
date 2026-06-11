# Codebase Map

This map is the fastest way to find the owner of a behavior before changing it.
The runtime is still one Go binary, but the repo now separates stable extension
contracts, server adapters, browser UI, infrastructure, scripts, and evaluation
fixtures.

## Runtime Entry Points

| Path | Responsibility |
| --- | --- |
| `cmd/server/main.go` | Process startup, Nova Sonic agent bootstrap, HTTP route registration, WebSocket upgrade, LiveKit token minting, security headers, and origin policy. |
| `cmd/server/auth.go` | Shared-token bootstrap, HttpOnly browser sessions, local-only login, room/board authorization, and production safety checks. |
| `cmd/server/rate_limiter.go` | Fixed-window limits for WebSocket upgrades and LiveKit token requests. |
| `cmd/server/workspace.go` | Current deployment/workspace scope returned by `/workspace/status`; documents the future workspace isolation model. |

## Board And Meeting Domain

| Path | Responsibility |
| --- | --- |
| `cmd/server/board.go` | Shared board/card model, initial cards, provider-agnostic tool schemas, agent instructions, tool dispatch, board snapshots, and WebSocket board broadcasts. |
| `cmd/server/scrum_tools.go` | Scrum-master tools for subtasks, estimates, worklogs, links, sprint/rank, planning metadata, meeting state, participants, summaries, and Jira metadata helpers. |
| `cmd/server/meeting_access.go` | Host setup, generated meeting codes, participant join/leave, host-only meeting type changes, and meeting access snapshots. |
| `cmd/server/meeting_intelligence.go` | Risk classification, pending confirmations, audit mutation records, transcript evidence, undo/replay, meeting memory, ownership, and briefing generation. |
| `cmd/server/meeting_reports.go` | Post-meeting intelligence report assembly, recap generation, sprint risk signals, GitHub/PR hints, setup readiness, observability, and archived report persistence. |
| `cmd/server/meeting_report_handlers.go` | HTTP handlers for `/post-meeting`, `/meeting/intelligence`, `/meetings`, setup, observability, voice-provider, and identity status endpoints. |
| `cmd/server/chat_messages.go` | Meeting-chat normalization and Bedrock-backed English translation helpers for typed meeting input paths. |

## Voice Providers

| Path | Responsibility |
| --- | --- |
| `cmd/server/nova_sonic.go` | AWS Nova Sonic provider, LiveKit agent participant, Bedrock bidirectional stream lifecycle, board-context refresh, and tool-call dispatch. |
| `cmd/server/nova_sonic_output.go` | Paced Nova Sonic output audio framing, queue limits, pre-roll, underrun/drop metrics, and frame padding. |
| `cmd/server/nova_sonic_mixer.go` | 16 kHz mono PCM mixer feeding Nova Sonic. |
| `cmd/server/room_audio.go` | Shared room-audio format constants (48 kHz stereo) and PCM helpers for decoding LiveKit room audio. |
| `cmd/server/opus_encoder.go` / `cmd/server/opus_decoder.go` | CGo Opus wrappers used by browser and LiveKit audio paths. |
| `cmd/server/voice_models.go` | Host-selectable Nova Sonic model options, same-provider model changes, and local restart broker integration. |
| `cmd/server/voice_status.go` | Nova Sonic voice readiness preflight, AWS credential checks, LiveKit agent presence, and user-facing recovery guidance. |
| `cmd/server/aws_refresh.go` | Optional local-only proxy for refreshing short-lived AWS speech credentials (enabled via `APP_LOCAL_AWS_REFRESH_URL`). |

## Jira, GitHub, And Agent Runs

| Path | Responsibility |
| --- | --- |
| `cmd/server/jira.go` | Jira config loading, board hydration, core issue mapping, create/update/transition/comment/assignment writes, project-key safety, and sync result annotation. |
| `cmd/server/jira_ext.go` | Advanced Jira Platform and Agile APIs: metadata, transition discovery, worklogs, issue links, sprint assignment, ranking, custom fields, remote links, reporter, and watchers. |
| `cmd/server/jira_conflicts.go` | Local-vs-Jira conflict snapshots and conflict resolution helpers. |
| `cmd/server/jira_webhook.go` | Authenticated Jira webhook refresh endpoint. |
| `cmd/server/agent_runs.go` | Autonomous agent-run lifecycle, voice tool integration, PM classification, code/security review flow, checkpoints, Jira writeback, and optional PR review publishing. |
| `cmd/server/bedrock_agents.go` | Bedrock Claude Messages invocation used by PM and specialist agents. |
| `cmd/server/github_app.go` | GitHub App key parsing, JWTs, installation tokens, repo allowlisting, PR file reads, and optional PR review comments. |

## Persistence, Audit, And Guardrails

| Path | Responsibility |
| --- | --- |
| `cmd/server/board_store.go` | SQLite board snapshots, event history, action replay ledger records, meeting report archives, and agent-run persistence. |
| `cmd/server/audit.go` | Optional JSONL audit log for board mutations and Jira refreshes. |
| `cmd/server/guardrails.go` | Prompt-injection redaction, model-safe board/tool-result views, and mutating tool argument rejection. |

## Stable Extension Surface

| Path | Responsibility |
| --- | --- |
| `internal/core` | Provider-neutral contracts for `VoiceProvider`, `Connector`, `ModelProvider`, `ActionLedger`, registries, evidence, confidence, and receipts. |
| `internal/core/contracttest` | Shared contract-test helpers for extension implementations. |
| `internal/mocks` | No-credential mock connectors, voice providers, and model providers for tests only. |
| `cmd/server/extensions.go` | Runtime registration of current voice provider descriptors, Jira/GitHub connector descriptors, local board connector, and Bedrock model provider. |

## Browser UI

| Path | Responsibility |
| --- | --- |
| `web/index_livekit.html` | Main LiveKit/Nova Sonic meeting UI, host/participant access flow, video layouts, operator panel, voice reliability dashboard, board rendering, audit replay, and model settings. |
| `web/post_meeting.html` | Post-meeting intelligence dashboard and archived report viewer. |
| `public/screenshot.png` | README screenshot. |

## Infrastructure And Operations

| Path | Responsibility |
| --- | --- |
| `Dockerfile` | Multi-stage Go/libopus image running as a non-root app user. |
| `docker-compose.yml` | Local Nova Sonic stack with app, LiveKit, and persistent board data, configured from a `.env` file. |
| `.env.example` | Template for local development environment variables. |
| `Makefile` | Common developer commands: local stack (`up`/`down`/`logs`), build, tests, lint, security, import-boundary checks, and pre-commit. |
| `deploy/helm/auto-bot/` | Helm chart for Kubernetes deployment. |
| `deploy/terraform/roles-anywhere/` | Terraform module + cert helper for AWS Bedrock auth via IAM Roles Anywhere. |
| `scripts/check-*.sh` | Import-boundary, dependency, and GitHub Actions pinning checks. |
| `scripts/pre-commit` / `scripts/install-hooks.sh` | Local quality gates and hook installation. |

## Docs

| Path | Responsibility |
| --- | --- |
| `docs/deployment.md` | Kubernetes deployment guide (Helm, ingress/TLS, Bedrock auth, troubleshooting). |
| `docs/architecture.md` | Core runtime boundaries and extension ownership. |
| `docs/api/openapi.yaml` | HTTP control-plane reference. WebSocket message contracts are currently documented through tool schemas and frontend/server code rather than OpenAPI. |
| `docs/extension-contracts.md` | Human-readable contract rules for providers, connectors, model backends, and action replay. |
| `docs/golden-demo.md` | Narrow real-stack proof path and pass criteria. |
| `docs/threat-model.md` | Trust boundaries, assets, threats, and mitigations. |
| `docs/adrs/` | Architecture decision records. |
| `docs/deployment.md` | Kubernetes deployment guide. |
| `docs/research/` | Research notes for repository process decisions. |
| `docs/security/` | Application security review and hardening register. |

## Change Checklist

When changing behavior, update the docs nearest to the behavior:

1. Public setup/runtime change: `README.md`, `docs/deployment.md`, and the Helm chart values.
2. HTTP route change: `docs/api/openapi.yaml` and the README endpoint table when user-facing.
3. Extension contract change: `internal/core` Go docs, `docs/extension-contracts.md`, templates, and contract tests.
4. Provider or connector behavior change: `cmd/server/extensions.go` and the relevant contract tests in `internal/core/contracttest`.
5. Deployment or security posture change: `docs/deployment.md`, the Helm chart, `security.md`, and the threat model when the trust boundary changes.
