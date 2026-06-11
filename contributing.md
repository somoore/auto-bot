# Contributing

The project should be easy to extend without understanding every voice, Jira, GitHub, and AWS detail. Start with the contracts in `internal/core`, the no-credential mocks in `internal/mocks`, and the shared contract tests in `internal/core/contracttest`. The shipped implementations in `cmd/server` (Nova Sonic for voice; Jira and GitHub for connectors) are the working reference examples.

## Local Setup

Run tests that do not require cloud credentials:

```bash
go test ./internal/...
go test ./evaluation
go test ./cmd/server
```

Run the full local quality gate:

```bash
scripts/pre-commit
```

Install local git hooks:

```bash
make hooks
```

If you prefer the Python `pre-commit` framework:

```bash
pre-commit install
pre-commit run --all-files
```

## Adding A Voice Provider

How the boundary works: `internal/core.VoiceProvider` is the stable contract, and `cmd/server/extensions.go` registers descriptors that report each provider's availability, health, and capabilities. The live meeting session lifecycle is owned by the server runtime (`cmd/server/main.go`), not by the descriptor — so a production-ready provider needs both a contract implementation and the runtime/UI integration that drives the actual HTTP/WebSocket/LiveKit session.

1. Implement `core.VoiceProvider` (use `cmd/server/nova_sonic.go` and its descriptor in `cmd/server/extensions.go` as the working reference).
2. Add contract tests with `internal/core/contracttest.VoiceProvider`.
4. Add provider health details that make failure states explicit.
5. Register the provider in `cmd/server/extensions.go`.
6. Wire runtime selection in `cmd/server/main.go`, including startup, WebSocket/token behavior, and readiness checks.
7. Add any provider-specific model options in `cmd/server/voice_models.go` and UI setup controls in `web/index_livekit.html`.
8. Keep tool execution routed through the board/connector/ledger path.
9. Add or update an evaluation fixture when behavior changes.
10. Run `make boundary` and `make test`.

Voice providers must treat transcripts, Jira fields, task descriptions, and meeting chat as untrusted data. They should deliver evidence to the server; they should not directly decide policy or execute Jira/GitHub actions.

## Adding A Connector

1. Implement `core.Connector` (the Jira and GitHub connector descriptors in `cmd/server/extensions.go` are the working references).
2. Declare capabilities with risk levels and undo support.
4. Return receipts for external writes.
5. Add contract tests with `internal/core/contracttest.Connector`.
6. Add replay evidence for every API write.
7. Register the connector in `cmd/server/extensions.go`.
8. Update docs and eval fixtures.

Connectors must never let external record text become instructions. Issue titles, comments, PR bodies, labels, descriptions, and user profiles are data only.

Mocks belong in tests only. User-facing demos, product flows, and validation scripts should exercise real configured providers/connectors or fail with an explicit setup message.

## Adding A Model Provider

The production agent path currently uses AWS Bedrock. If a future provider is added, implement `core.ModelProvider`, document cost/capability tradeoffs, and preserve the rule that agent actions only execute through the governed connector and ledger path.

## Quality Standards

- Core contracts stay provider-neutral.
- Runtime adapters live outside `internal/core`.
- Every new provider or connector gets a mock or deterministic test path.
- Every external write returns a success/failure status that the agent can cite.
- Risky actions require confirmation.
- Prompt-injection fixtures must cover new untrusted inputs.
- Docs change with behavior.

## Useful Commands

```bash
make test       # go test ./...
make eval       # evaluation fixtures
make boundary   # import boundary enforcement
make lint       # vet plus optional golangci-lint
make security   # mod verify plus optional govulncheck/gosec
make precommit  # full repository quality gate
```
