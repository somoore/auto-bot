# Codebase Map

This map is the fastest way to find the owner of a behavior before changing it.
The runtime is still one Go binary, but the repo now separates stable extension
contracts, server adapters, browser UI, infrastructure, scripts, and evaluation
fixtures.

## Runtime Entry Points

| Path | Responsibility |
| --- | --- |
| `cmd/server/main.go` | Process startup, provider selection, HTTP route registration, WebSocket upgrade, LiveKit token minting, OpenAI WebRTC signaling, security headers, and origin policy. |
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
| `cmd/server/kanban.go` | OpenAI Realtime WebRTC path, OpenAI session configuration, model profile validation, event handling, and tool-call dispatch. |
| `cmd/server/nova_sonic.go` | AWS Nova Sonic provider, LiveKit agent participant, Bedrock bidirectional stream lifecycle, board-context refresh, and tool-call dispatch. |
| `cmd/server/nova_sonic_output.go` | Paced Nova Sonic output audio framing, queue limits, pre-roll, underrun/drop metrics, and frame padding. |
| `cmd/server/nova_sonic_mixer.go` | 16 kHz mono PCM mixer feeding Nova Sonic. |
| `cmd/server/audio_mixer.go` | 48 kHz stereo PCM mixer feeding OpenAI Realtime. |
| `cmd/server/opus_encoder.go` / `cmd/server/opus_decoder.go` | CGo Opus wrappers used by browser and LiveKit audio paths. |
| `cmd/server/voice_models.go` | Host-selectable voice model options, same-provider model changes, restart-required provider switches, and local restart broker integration. |
| `cmd/server/voice_status.go` | Voice readiness preflight for OpenAI/Nova, AWS credential checks, LiveKit agent presence, and user-facing recovery guidance. |
| `cmd/server/aws_refresh.go` | Local-only proxy to the credential refresh broker started by `scripts/local-up.sh`. |

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
| `examples/connector-template` | Copyable connector implementation template and README. |
| `examples/voice-provider-template` | Copyable voice-provider implementation template and README. |
| `examples/model-provider-template` | Copyable model-provider implementation template and README. |

## Browser UI

| Path | Responsibility |
| --- | --- |
| `web/index_livekit.html` | Main LiveKit/Nova Sonic meeting UI, host/participant access flow, video layouts, operator panel, voice reliability dashboard, board rendering, audit replay, and model settings. |
| `web/index.html` | OpenAI Realtime/Pion browser UI for the raw WebRTC path. |
| `web/post_meeting.html` | Post-meeting intelligence dashboard and archived report viewer. |
| `public/screenshot.png` | README screenshot. |

## Infrastructure And Operations

| Path | Responsibility |
| --- | --- |
| `Dockerfile` | Multi-stage Go/libopus image running as a non-root app user. |
| `docker-compose.yml` | Local Nova Sonic stack with app, LiveKit, persistent board data, and Keychain-injected environment. |
| `Makefile` | Common developer commands: local stack, tests, evals, lint, security, import-boundary checks, and pre-commit. |
| `scripts/local-up.sh` | One-command local startup: Keychain secrets, AWS assume flow, Docker Compose, local login, and browser open. |
| `scripts/local-compose.sh` / `scripts/local-down.sh` | Docker Compose wrappers that preserve the repo's local secret handling. |
| `scripts/dc-up-keychain.sh` / `scripts/run-openai-keychain.sh` | Provider-specific local launchers using macOS Keychain. |
| `scripts/local-aws-refresh-broker.*` | Local-only restart broker for expired AWS speech credentials. |
| `scripts/aws-*.sh` | AWS secret upload, image build/push, and dev deploy helpers. |
| `scripts/check-*.sh` | Import-boundary, dependency, GitHub Actions pinning, and quality-gate checks. |
| `infra/` | Terragrunt live stack and reusable Terraform module for ECS/Fargate, ALB/WAF, LiveKit, Redis, EFS, Secrets Manager, VPC endpoints, and dashboards. |

## Evaluation And Docs

| Path | Responsibility |
| --- | --- |
| `evaluation/` | Fixture-backed validation harness for meeting behavior, Jira safety, voice reliability, post-meeting intelligence, agent runs, AWS/LiveKit hardening, and extension contracts. |
| `evaluation/failure-inventory.md` | Seed replay inventory for ambiguous voice-command failures. |
| `docs/architecture.md` | Core runtime boundaries and extension ownership. |
| `docs/api/openapi.yaml` | HTTP control-plane reference. WebSocket message contracts are currently documented through tool schemas and frontend/server code rather than OpenAPI. |
| `docs/extension-contracts.md` | Human-readable contract rules for providers, connectors, model backends, and action replay. |
| `docs/golden-demo.md` | Narrow real-stack proof path and pass criteria. |
| `docs/threat-model.md` | Trust boundaries, assets, threats, and mitigations. |
| `docs/adrs/` | Architecture decision records. |
| `docs/planning/` | Roadmap and working progress notes. |
| `docs/research/` | Research notes for repository process decisions. |
| `docs/security/` | Application security review and hardening register. |

## Change Checklist

When changing behavior, update the docs nearest to the behavior:

1. Public setup/runtime change: `README.md` and `.env.example`.
2. HTTP route change: `docs/api/openapi.yaml` and the README endpoint table when user-facing.
3. Extension contract change: `internal/core` Go docs, `docs/extension-contracts.md`, templates, and contract tests.
4. Provider or connector behavior change: relevant `examples/` README and evaluation fixture.
5. Infrastructure input or security posture change: `infra/README.md`, module README, `SECURITY.md`, and threat model when the trust boundary changes.
