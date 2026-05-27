# auto-bot

[![MIT License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![CI](https://github.com/somoore/auto-bot/actions/workflows/ci.yml/badge.svg)](https://github.com/somoore/auto-bot/actions/workflows/ci.yml)
![Go](https://img.shields.io/badge/Built_with-Go_1.26-blue)
![MCP](https://img.shields.io/badge/Exposes-Model_Context_Protocol-3CDFB1)

**The kanban where agents do work.** auto-bot is an agent-native work surface: humans and agents are assigned the same cards, communicate through the same threads, and ship through the same audit log. Voice meetings move the board, MCP clients (Claude Code, Cursor) update the board, and durable Runs let agents pause to ask the human and resume on the same checkpoint.

![screenshot](./public/screenshot.png)

## Features

- **Agents as first-class assignees.** Durable, checkpointed `Run` objects bound to cards; agents pause on `RunQuestion`s and resume when a human answers in the same UI.
- **Voice standup that writes the board.** Nova Sonic (primary) and OpenAI Realtime (fallback) call the same `KanbanToolDefs()`; LiveKit is the SFU.
- **MCP server for editor agents.** `cmd/mcpd` exposes `board.list_cards`, `board.get_card`, `card.create`, `card.update`, `card.comment` over stdio + HTTP.
- **Outbound projections.** Canonical board projects to Jira today; Linear and GitHub Issues use the same contract.
- **Async intake.** `POST /intake/standup` and `POST /intake/slack` (HMAC-verified) fold into the next standup agenda.
- **Trust ceremony.** Per-tenant dry-run staging, diff preview, undo on every connector, and a pause-all kill switch.

## Quickstart

**macOS (recommended).** `scripts/local-up.sh` mints every secret in your Keychain (no `.env` files), assumes role into AWS, and brings the stack up:

```bash
git clone https://github.com/somoore/auto-bot
cd auto-bot
scripts/local-up.sh
open http://localhost:3001/app/
```

**Linux / non-macOS.** Supply the two required secrets via env, then start docker:

```bash
export APP_API_TOKEN=$(openssl rand -hex 32)
export MCP_SIGNING_KEYS="k1:$(openssl rand -base64 32)"
docker compose up -d
open http://localhost:3001/app/
```

That gives you the React board on `:3001/app/`, MCP HTTP on `:4000`, and a LiveKit dev server on `:7880`. For a stack with real Jira, Bedrock voice, and a GitHub App see [docs/golden-demo.md](docs/golden-demo.md).

Health checks:

```bash
curl -s http://localhost:3001/healthz
curl -s -H "Authorization: Bearer dev" http://localhost:3001/workspace/status
```

## MCP integration

Point any MCP client at the running mcpd. The HTTP transport requires a **signed bearer token** issued by `cmd/server`'s `POST /admin/mcp-tokens` (HMAC-SHA256, scoped, ~15-minute TTL, ULID jti replay defense). Mint one:

```bash
curl -s -X POST http://localhost:3001/admin/mcp-tokens \
  -H "Authorization: Bearer $APP_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"subject":"agent:claude-code","scopes":["board:read","card:write","runs:start"],"ttl_seconds":3600}' \
  | jq -r '.token'
```

Then point Claude Code at it (`~/.claude/mcp.json`):

```json
{
  "mcpServers": {
    "auto-bot": {
      "transport": "http",
      "url": "http://localhost:4000",
      "headers": { "Authorization": "Bearer <token-from-curl-above>" }
    }
  }
}
```

Cursor uses the same shape in `.cursor/mcp.json`. Full token model + rotation: [docs/api/mcp-tools.md#authentication](docs/api/mcp-tools.md#authentication).

Full JSON-RPC schemas and the dispatch flow: [docs/api/mcp-tools.md](docs/api/mcp-tools.md).

## Where to go next

- **Architecture and component map:** [docs/architecture.md](docs/architecture.md).
- **Extension contracts** (`Connector`, `VoiceProvider`, `ModelProvider`, `Projection`, `RunCoordinator`): [docs/extension-contracts.md](docs/extension-contracts.md).
- **What shipped in v2:** [docs/release-notes/v2.0.md](docs/release-notes/v2.0.md).
- **Security model:** [SECURITY.md](SECURITY.md), [docs/threat-model.md](docs/threat-model.md), and [docs/security/](docs/security/).
- **ADRs:** [0001 core extension boundaries](docs/adrs/0001-core-extension-boundaries.md), [0002 canonical board with external projections](docs/adrs/0002-canonical-board-with-external-projections.md), [0003 MCP as universal external surface](docs/adrs/0003-mcp-server-as-universal-external-surface.md), [0004 multi-tenant model](docs/adrs/0004-multi-tenant-model.md).
- **Contributing:** [CONTRIBUTING.md](CONTRIBUTING.md). New-contributor walk-through: [docs/onboarding/new-contributor.md](docs/onboarding/new-contributor.md). Pre-commit hooks (`make precommit`) gate every commit; do not use `--no-verify`.

## License

MIT. See [LICENSE](LICENSE).

## Authors

Built by Scott Moore (`scott@moore.cloud`) and the Sprint roster. Built on AWS Bedrock (Claude + Nova Sonic), LiveKit, Pion WebRTC, OpenAI Realtime, and the Model Context Protocol.
