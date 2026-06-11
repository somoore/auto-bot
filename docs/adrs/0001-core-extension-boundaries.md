# ADR 0001: Core Extension Boundaries

## Status

Accepted.

## Context

The project combines voice transport, model inference, meeting policy, Jira, GitHub, audit replay, UI state, Docker, and AWS infrastructure. Without strict boundaries, new full-duplex speech models or connectors would require contributors to understand too much application runtime code.

## Decision

Create `internal/core` as the provider-neutral extension contract package. Voice providers, connectors, model providers, and action-ledger implementations depend on these contracts. The core package may not import runtime providers or application packages.

Runtime code in `cmd/server` registers current adapters for:

- `nova-sonic`
- `livekit-cloud`
- `local-board`
- `jira`
- `github`
- `bedrock`

Import-boundary checks are part of `scripts/pre-commit`.

## Consequences

Positive:

- New contributors have a small API surface to learn first.
- Provider and connector contract tests are reusable.
- Future providers can be added without changing the meeting engine policy model.
- Specialized audio models can be registered without granting them Jira/GitHub tool authority.
- Code review can reject boundary drift automatically.

Tradeoff:

- The existing server still contains mature provider implementations. Moving those behind interfaces should happen incrementally, with tests before each migration.
