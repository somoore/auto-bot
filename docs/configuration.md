# Configuration & layout

Reference for Auto Bot's voice provider, environment configuration, and repository
layout. (Moved out of the project README to keep it focused on getting started.)

## Voice provider

| Provider | Media path | Models | AWS needed |
|---|---|---|---|
| **AWS Nova Sonic** | LiveKit Cloud SFU | Bedrock Nova Sonic + Claude | Yes (Bedrock) |

The voice path is AWS Nova Sonic over LiveKit Cloud, with Bedrock Claude for board
reasoning and agent runs. `VOICE_PROVIDER` defaults to `nova-sonic`.

## Configuration

All settings are environment variables, surfaced through the Helm chart's `config` (non-secret)
and `secretEnvKeys` (from your Secret). Key ones:

| Var | Purpose |
|---|---|
| `APP_API_TOKEN` | App login token (required) |
| `VOICE_PROVIDER` | `nova-sonic` (default) |
| `LIVEKIT_URL` / `LIVEKIT_API_KEY` / `LIVEKIT_API_SECRET` | LiveKit Cloud (nova-sonic) |
| `AWS_REGION` | Bedrock region (us-east-1 / us-west-2) |
| `BOARD_SQLITE_PATH` | Board persistence (`/srv/data/board.sqlite`) |
| `GITHUB_*` / `JIRA_*` | Optional integrations |

See [`.env.example`](../.env.example) and [`deploy/helm/auto-bot/values.yaml`](../deploy/helm/auto-bot/values.yaml)
for the full list.

## Project layout

```
cmd/server/        Go HTTP server + agent runtime
internal/core/     stable extension contract (providers, connectors, ledger)
web/               browser UI
deploy/helm/       Helm chart
deploy/terraform/  IAM Roles Anywhere module (Bedrock auth)
docs/              architecture, deployment, contracts, threat model
```

See also [codebase-map.md](codebase-map.md) for a source → responsibility map.
