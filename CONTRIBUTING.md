# Contributing

## Welcome

`auto-bot` is a voice-first agentic standup platform: a canonical board, durable agent Runs, an ask-the-human loop, async intake from Slack and a React form, an MCP server (`cmd/mcpd`) that exposes the board to Claude Code / Cursor / `claude-agent-sdk`, and a Nova Sonic / OpenAI Realtime voice layer. The product is one Go binary plus a sibling MCP daemon and a Vite + React SPA. This guide describes how to land changes in that codebase without breaking the boundaries that keep it sane.

## Code of conduct

Read [`CODE_OF_CONDUCT.md`](./CODE_OF_CONDUCT.md) before posting. Discussion is technical, direct, and respectful; secrets and customer data stay out of the repo and the issue tracker.

## Quickstart for contributors

```bash
# 1. Fork on GitHub, then clone your fork.
git clone git@github.com:<your-user>/auto-bot.git
cd auto-bot
git remote add upstream git@github.com:somoore/auto-bot.git

# 2. Install the local pre-commit hook (wraps scripts/pre-commit).
scripts/install-hooks.sh

# 3. Build and start the stack. APP_API_TOKEN authenticates browser sessions
#    and is also the bearer cmd/mcpd uses to call /internal/tools/dispatch.
#    MCPD_TOKEN authenticates MCP clients (Claude Code, Cursor) against mcpd.
export APP_API_TOKEN=$(openssl rand -hex 32)
export MCPD_TOKEN=$(openssl rand -hex 32)
docker compose build
docker compose up -d

# 4. Build the React SPA served at /app/.
cd web/app && npm install && npm run build && cd ../..

# 5. Smoke test.
curl -fsS http://localhost:3001/healthz   # → {"ok":true}
```

The blessed macOS path is `scripts/local-up.sh` (mints local tokens into Keychain, starts the AWS credential refresh broker, builds and launches docker compose). On Linux or without Keychain, the four-command path above is the supported alternative; the only required env var declared in `docker-compose.yml:20` is `APP_API_TOKEN`. AWS credentials are only needed for the voice path (see "The voice path" below).

## The package layout

Two binaries; eighteen Go packages under `internal/`. The boundaries are enforced — see "Import boundary rules" below.

- `cmd/server/` — the main HTTP/WebSocket binary. Process startup, route table, voice provider selection, LiveKit signaling, OpenAI WebRTC, security headers. Nothing imports this.
- `cmd/mcpd/` — the MCP daemon. Connects to `cmd/server` over HTTP and re-exposes the board, cards, and runs as MCP tools for Claude Code, Cursor, and `claude-agent-sdk`.
- `internal/core/` — universal vocabulary (`Action`, `RiskLevel`, `Decision`, `Actor`, `ActionLedger`). Stdlib only. The most-isolated tier.
- `internal/board/` — canonical Board / Issue / Sprint types that every external system projects into.
- `internal/agent/` — `Run`, `Plan`, `Cost`, `WaitingOn`, `RunCoordinator`. The runtime state machine for agent work.
- `internal/meetings/` — scrum + transcript types shared between voice providers and the meeting intelligence layer.
- `internal/projection/` — provider-neutral `Projection` contract plus per-system projections (`jira/` is the first; Linear and GitHub Issues are W5 work).
- `internal/intake/` — async standup intake: typed payload, Slack adapter (HMAC verification), and shared validation. Slack and the React `IntakeForm` both land here.
- `internal/standup/` — agenda builder (pre-meeting) and post-meeting closer (creates cards + kicks Runs).
- `internal/mcp/` — MCP protocol surface: tool registry, JSON-RPC handlers, board client. `cmd/mcpd` wires it.
- `internal/auth/`, `internal/tenant/`, `internal/cost/`, `internal/http/`, `internal/httpapi/`, `internal/jira/`, `internal/voice/` — focused single-responsibility packages extracted from the original monolith. See `docs/codebase-map.md` for the full file-by-file map.
- `internal/extensions/` — registration shims for voice providers, connectors, and model providers.
- `internal/mocks/` — test-only fakes. The in-memory `mocks.BoardClient` doubles as `cmd/mcpd`'s offline fallback for the S2.0 foundation.

For a deeper map (every file in `cmd/server`, every responsibility group), read [`docs/codebase-map.md`](./docs/codebase-map.md). For the architectural rationale, [`docs/architecture.md`](./docs/architecture.md). For the four ADRs that shape package boundaries, [`docs/adrs/`](./docs/adrs/).

## Import boundary rules

`scripts/check-import-boundaries.sh` is the single source of truth. It runs on every commit (`scripts/pre-commit` step 6) and the rules are:

- **`internal/core`** — may import only itself + stdlib. The `check_internal_package` regex at line 47-48 enforces this.
- **`internal/agent`** — may import `internal/core`, itself, and `github.com/oklog/ulid/v2`. The ULID exception is whitelisted at line 36 of the script — every external dependency here needs an ADR.
- **`internal/mcp`** — may import `internal/core`, `internal/agent`, `internal/board`, and itself. No provider SDKs.
- **`internal/mocks`** — may import `internal/core`, `internal/agent`, `internal/board`, `internal/mcp`, and itself.
- **`internal/projection`** — may import `internal/core`, `internal/board`, and itself.
- **`internal/intake`** — may import `internal/core`, `internal/board`, `internal/agent`, and itself.
- **`cmd/server`** — application entrypoint. **Nothing may import this.** Lines 120-124 of the script `rg` the entire tree for `github.com/somoore/auto-bot/cmd/server` and fail if any Go file references it.

Other `internal/*` packages (board, meetings, standup, auth, tenant, cost, http, httpapi, jira, voice, extensions) are not pinned by the script today — they are de-facto leaf packages, but if you start cycling through them, propose an ADR before adding them to the enforcer. Read the script before you push; it is short and it is the spec.

## Commit message style

Format: `<Verb>(<scope>): <one-line summary> (<finding ID if applicable>)`. Imperative mood, present tense, short. Real examples from the current branch:

```
Fix(security): tenant-scope WebSocket fanout (SecArch-001)
Fix(audit): fail-loud on Checkpoint audit miss (SE-1 F2)
Fix(correctness): block Resume on expired questions (DA-1)
Fix(mcp): map RunStore sentinels to JSON-RPC -32602 (SE-1 F6)
Fix(reliability): surface persistAgentRun errors to coordinator callers (SE-1 F3)
Gate intake self-assignment on caller identity (SecArch-002)
```

Sprint deliverables follow a parallel style:

```
Sprint 4.0: pending_actions table + dry-run staging
Sprint 4.1: internal/standup agenda builder
Sprint 4.1: post-meeting closer creates cards + kicks Runs
```

Plain feature commits use the imperative bare form:

```
Broadcast RunQuestion + Plan over WebSocket
Add Actor discriminated type for human + agent assignees
Extract internal/board domain types
```

The body, when present, explains the *why* and references the relevant finding, ADR, or sprint plan. If the change closes a security finding, the finding ID belongs in the title.

## Atomic commits

One logical change per commit. The branch history *is* the spec — reviewers read it, the security review walks SHA ranges, and the erratum doc (`docs/erratum-commit-title-swaps.md`) exists because earlier work mixed up which commit shipped which deliverable. Don't repeat that.

- Do not bundle "fix typo + add feature + refactor" into one commit.
- Do not split a single feature across three commits because each subdirectory feels independent.
- `git commit --amend` is fine on commits you have not pushed. Once shared, treat the SHA as immutable. The erratum doc explains why we did not rebase six racing commit titles: it would have broken every existing reference and forced every reviewer to reset.

## Pre-commit hook gate

`scripts/install-hooks.sh` installs a thin wrapper at `.git/hooks/pre-commit` that execs `scripts/pre-commit`. Read the script — it is ~400 lines, all in `scripts/pre-commit`, and it is the source of truth. In order, the hook runs:

1. `go mod tidy` cleanliness check (script lines 19-30).
2. `go mod verify` checksum check (lines 32-37).
3. `scripts/check-go-dependencies.sh` (ghost-import / package graph check, lines 39-44).
4. `go vet ./...` (lines 48-53).
5. `go test ./...` (lines 55-60).
6. `scripts/check-import-boundaries.sh` (lines 62-67).
7. `scripts/check-github-actions-pinning.sh` (immutable SHA pinning for any GitHub Actions workflows, lines 69-74).
8. Lock-file presence check across `go.sum`, `package-lock.json` / `pnpm-lock.yaml` / `yarn.lock` / `bun.lock(b)`, `poetry.lock`, `Pipfile.lock`, `Cargo.lock`, `Gemfile.lock`, `composer.lock`, and `.terraform.lock.hcl` (lines 76-138).
9. `.pre-commit-config.yaml` rev pinning to immutable 40-char SHAs (lines 140-147).
10. Terraform `required_version` exact-pin check — `>=`, `~>`, `!=`, `<=`, etc. are rejected (lines 149-154).
11. `golangci-lint run` (v2.12.2, lines 160-169). Skipped with a warning if the binary is not installed.
12. `goimports -l .` — fails if any file would change (lines 171-182).
13. `govulncheck ./...` (lines 184-195). Skipped with a warning if not installed.
14. `gosec -exclude-generated ./...` (lines 197-206). The `// #nosec G706` and `// #nosec G107 G704` annotations in `cmd/mcpd/main.go` and `internal/mcp/tools.go` are deliberate — each names the sanitizer or operator-controlled input the linter cannot see; copy the pattern, do not add bare `// #nosec`.
15. `terraform fmt -check -recursive infra/` (lines 210-225). Falls back to `alpine/terragrunt:1.15.2` pinned by digest if `terraform` is not installed locally.
16. Terragrunt `hclfmt --check` (lines 227-256).
17. Docker image digest pinning — every `FROM` and `image:` line in `Dockerfile` and `docker-compose.yml` must include `@sha256:` (lines 260-278).
18. Debian apt package pinning — every `apt-get install` argument must include `=<version>` (lines 280-319).
19. `:latest` / `@latest` forbidden in `Dockerfile`, `docker-compose.yml`, `infra/`, `scripts/` (lines 321-348).
20. CDN script SRI integrity attributes — every `<script src="https://…">` in `web/*.html` must include `integrity=` (lines 350-372).
21. Secrets scan against staged files: `AKIA…`, `sk-…`, PEM private keys, and high-entropy `password = "…"` strings (lines 374-390).

**Never use `--no-verify`.** Every step in this list exists because something broke before it was added. If a hook is wrong, fix the hook in its own PR; do not bypass it on the way to landing other work. If a hook fails on you, paste the error into your PR description so reviewers see what you saw.

## Tests

`go test ./...` must pass on every commit. The hook enforces this. Add a regression test for every bug fix; the bug-wave commits (`Fix(security): tenant-scope WebSocket fanout`, `Fix(correctness): block Resume on expired questions`, etc.) each landed with a test that fails before the fix and passes after.

Go test suites that exist on this branch:

- `cmd/server` — the broadest suite (board, auth, agent runs, confirmation gate, intake handlers, dry-run queue, projection replay, run-question lifecycle, tenant isolation).
- `internal/agent` — coordinator + run-store unit tests.
- `internal/core` — ledger + registry unit tests.
- `internal/intake` — intake validation + Slack-adapter signature tests.
- `internal/mcp` — MCP server protocol tests.
- `internal/projection` — registry + the Jira projection.
- `internal/standup` — agenda builder + post-meeting closer.

Frontend tests use Vitest 2.1.8 + React Testing Library 16.1.0 (pinned in `web/app/package.json`):

```bash
cd web/app && npm test
```

The frontend suite covers `CardDrawer`, `CardRunTab`, `IntakeForm`, and `SuggestedAnswerChip`.

## Parallel work with agents

**Read this section if you are using Claude Code, Cursor, or any AI assistant to parallelize work on this codebase.** Use per-agent git worktrees, not shared checkouts:

```bash
# From the repo root.
git worktree add ../agent-feature-x -b agent-feature-x
```

This branch's history (see [`docs/erratum-commit-title-swaps.md`](./docs/erratum-commit-title-swaps.md)) documents what happens when multiple agents race the same `.git/index`: commit titles get swapped onto the wrong deliverables, files get deleted by the wrong commit, and one ADR-chain went unwritten because the racing commits clobbered each other. Six commit titles on the published branch refer to deliverables they do not contain. We did not rebase because every downstream worktree and every doc that cites a SHA range would have to be reset; the erratum is cheaper than the rewrite, but the prevention is cheaper than either. Worktrees give each agent its own index and its own working tree. Use them.

When you delegate to multiple agents in parallel:

- One worktree per agent. Branch name should match the agent's task scope.
- Define ownership up front. The Sprint 4 plan assigned files to scribes by name. Do the same.
- Merge serially. Two agents resolving the same merge conflict at the same time produces the same race.

## The voice path

Local voice meetings need AWS credentials with permission to call Bedrock Nova Sonic (`amazon.nova-2-sonic-v1:0`) and, optionally, OpenAI Realtime. On macOS:

```bash
scripts/dc-up-keychain.sh   # reads AWS_PROFILE from Keychain, exports envs, runs docker compose up
```

The script assumes the `local-aws-refresh-broker` (started by `scripts/local-up.sh`) is exporting refreshed credentials at `APP_LOCAL_AWS_REFRESH_URL`. Without AWS credentials, the server still boots, the React SPA loads, the board mutates, tests pass, and the voice agent reports `"AWS config not ready"` through `/voice/status` — the rest of the app degrades gracefully. Your PR does not need to exercise the voice path unless it touches voice code.

## The MCP path

`cmd/mcpd` runs as a separate container (see `docker-compose.yml:65-89`). It listens on `127.0.0.1:4000` and dispatches every MCP tool call through `cmd/server`'s `/internal/tools/dispatch` endpoint — so the `ActionLedger`, risk gates, and trust-ceremony confirmations apply uniformly to voice, UI, and MCP callers. The `MCPD_TOKEN` env var authenticates MCP clients; `BOARD_TOKEN` (which equals `APP_API_TOKEN`) authenticates `mcpd` against the app.

To connect Claude Code, add this to `~/.claude/mcp.json` (full snippet in [`docs/marketing/dev-adoption.md`](./docs/marketing/dev-adoption.md) line 27-43):

```json
{
  "mcpServers": {
    "auto-bot": {
      "command": "/usr/local/bin/mcpd",
      "args": ["--transport", "stdio"],
      "env": {
        "AUTO_BOT_BASE_URL": "http://localhost:3001",
        "AUTO_BOT_MCP_TOKEN": "abk_live_<paste-from-/admin/mcp-tokens>",
        "AUTO_BOT_TENANT_ID": "default"
      }
    }
  }
}
```

For the full HTTP / MCP tool surface, see [`docs/api/mcp-tools.md`](./docs/api/mcp-tools.md) and [`docs/api/openapi.yaml`](./docs/api/openapi.yaml).

## PR checklist

Before opening a PR:

- [ ] `make build && make test` clean. (`make test` is `go test ./...`.)
- [ ] `scripts/check-import-boundaries.sh` passes. (`make boundary`.)
- [ ] `cd web/app && npm test && npm run build` clean.
- [ ] Pre-commit hook green — full `scripts/pre-commit` run, not just the steps you remember. `make precommit` does this in one command.
- [ ] Commit messages match the project style above.
- [ ] New code has tests. Bug fixes have regression tests.
- [ ] Docs touched if behavior changed. The `docs/architecture.md`, `docs/codebase-map.md`, `docs/api/`, and the relevant `docs/adrs/` are the ones that drift first.
- [ ] If you touched a security boundary, [`docs/threat-model.md`](./docs/threat-model.md) and the relevant `docs/security/` review are updated.
- [ ] No `--no-verify`. No skipped hooks. No bare `// #nosec` without a justification comment.

## Architectural decisions

Big structural changes belong in an ADR. The current set:

- [`docs/adrs/0001-core-extension-boundaries.md`](./docs/adrs/0001-core-extension-boundaries.md) — provider-neutral core, runtime adapters live outside.
- [`docs/adrs/0002-canonical-board-with-external-projections.md`](./docs/adrs/0002-canonical-board-with-external-projections.md) — Jira (and Linear, GitHub Issues) become outbound projections of the canonical board.
- [`docs/adrs/0003-mcp-server-as-universal-external-surface.md`](./docs/adrs/0003-mcp-server-as-universal-external-surface.md) — MCP is the single external tool surface; voice, UI, and MCP callers all flow through the same dispatch path.
- [`docs/adrs/0004-multi-tenant-model.md`](./docs/adrs/0004-multi-tenant-model.md) — tenant scoping through auth, board, agent runs, and SQLite.

New ADRs go in `docs/adrs/` with the next sequential number. Write the ADR before the code; reviewers will not merge a structural change without one.

## Useful commands

```bash
make test       # go test ./...
make eval       # evaluation fixtures
make boundary   # scripts/check-import-boundaries.sh
make lint       # vet + golangci-lint (when installed) + boundary + actions
make security   # go mod verify + govulncheck + gosec
make precommit  # full scripts/pre-commit gate
make hooks      # scripts/install-hooks.sh
make web-app-build   # npm ci + npm run build in web/app
```

If anything in this document contradicts what `scripts/pre-commit` or `scripts/check-import-boundaries.sh` actually does, the script wins and the fix is your PR.
