# New Contributor Onboarding

Goal: from `git clone` to your first PR merged in 90 minutes. If something here
is wrong or out of date, the fix is your first PR.

## 0. Prerequisites

Pinned versions are load-bearing. `Dockerfile`, `go.mod`, and
`web/app/package.json` are the source of truth — these are mirrored here so you
can install in one pass.

| Tool          | Version                                                                            | Source of truth                      |
|---------------|------------------------------------------------------------------------------------|--------------------------------------|
| Go            | 1.26                                                                                | `go.mod` line 3                      |
| Docker        | 29+ (Desktop or Engine, with Compose v2)                                            | `docker-compose.yml`                 |
| Node          | no `engines` pin; Node 18+ required by Vite 6.0.7, Node 22 LTS recommended         | `web/app/package.json`               |
| TypeScript    | 5.6.3                                                                              | `web/app/package.json`               |
| Vite          | 6.0.7                                                                              | `web/app/package.json`               |
| Vitest        | 2.1.8                                                                              | `web/app/package.json`               |
| React Testing Library | 16.1.0                                                                     | `web/app/package.json`               |
| libopus       | 1.5.2 (build) / 1.3.1 (runtime)                                                    | `Dockerfile` lines 3-4, 17           |
| golangci-lint | 2.12.2                                                                             | `scripts/pre-commit`                 |
| goimports     | v0.45.0                                                                            | `scripts/install-hooks.sh`           |
| govulncheck   | v1.3.0                                                                             | `scripts/install-hooks.sh`           |
| gosec         | v2.26.1                                                                            | `scripts/pre-commit`                 |

macOS install (one-shot):

```bash
brew install go@1.26 node opus pkg-config docker golangci-lint terraform
go install golang.org/x/tools/cmd/goimports@v0.45.0
go install golang.org/x/vuln/cmd/govulncheck@v1.3.0
go install github.com/securego/gosec/v2/cmd/gosec@v2.26.1
```

AWS credentials are optional but required for the voice path (Bedrock Nova
Sonic, OpenAI Realtime via Secrets Manager). Without them, the server boots,
the React SPA loads, the board mutates, the MCP daemon serves tools, and
tests pass — only voice meetings fail with a friendly `"AWS config not ready"`
status on `/voice/status`.

## 1. Setup (target: 15 minutes)

```bash
git clone git@github.com:somoore/auto-bot.git
cd auto-bot
scripts/install-hooks.sh              # installs .git/hooks/pre-commit
```

### macOS (recommended)

`scripts/local-up.sh` is the blessed path. It mints local tokens into Keychain,
starts the AWS credential broker, and runs `docker compose up --build -d`:

```bash
scripts/local-up.sh
# Waits for http://localhost:3001/healthz, opens browser on local-login.
```

### Linux / no-Keychain

Use the bare docker-compose path. `APP_API_TOKEN` and `MCPD_TOKEN` are the
two required env vars; see `docker-compose.yml:20` and `docker-compose.yml:76`:

```bash
export APP_API_TOKEN=$(openssl rand -hex 32)
export MCPD_TOKEN=$(openssl rand -hex 32)
docker compose build
docker compose up -d
curl -fsS http://localhost:3001/healthz   # → {"ok":true}
```

Compose brings up three services: `livekit` (the SFU on 7880), `app`
(`cmd/server`, exposed on 3001), and `mcpd` (`cmd/mcpd`, exposed on 4000).
`mcpd` proxies MCP tool calls through `app`'s `/internal/tools/dispatch` so
that the `ActionLedger`, risk gates, and trust-ceremony confirmations apply
uniformly to voice, UI, and MCP callers.

Open <http://localhost:3001/app/> to confirm the React SPA loads.

To stop: `scripts/local-down.sh` (macOS) or `docker compose down`.

### Use a worktree if you are working in parallel

If you are driving this codebase with AI assistants — or just want a clean
branch separate from your main checkout — use a per-task `git worktree`:

```bash
git worktree add ../auto-bot-feature-x -b feature-x
cd ../auto-bot-feature-x
```

The branch's history records what happens when multiple agents share the same
`.git/index`: titles get swapped onto the wrong deliverables and files get
deleted by racing commits. See `docs/erratum-commit-title-swaps.md`. Worktrees
give each task its own index. Use them.

## 2. First read — the 10 files that give you the mental model

Read in this order. Skim, don't memorize. Each file under 400 LoC; this is
~90 minutes of reading at a comfortable pace.

1. **`README.md`** — what we're building, in one screen.
2. **`docs/architecture.md`** — the canonical board + projections model; the
   *why* behind every package boundary you'll see.
3. **`internal/core/types.go`** — universal vocabulary (`Action`, `RiskLevel`,
   `Decision`, `Actor`). If a type isn't here, it's provider-specific.
4. **`internal/core/ledger.go`** — the append-only audit ledger. Every
   mutation that matters lands here. Read this before you write any handler.
5. **`internal/board/types.go`** — the canonical Board / Issue / Sprint shape
   that all external systems project into.
6. **`internal/agent/types.go`** — `Run`, `Plan`, `Cost`, `WaitingOn`,
   `Actor`. The runtime state machine for agent work.
7. **`internal/agent/coordinator.go`** — the `RunCoordinator` interface +
   default implementation. Read alongside `simple_coordinator.go` to see the
   two compositional layers.
8. **`cmd/server/main.go`** — start at line 200, where `mux.HandleFunc(...)`
   begins. The HTTP route table is the complete external surface of the
   binary. Everything else is wired off these routes.
9. **`cmd/server/scrum_tools.go`** — the agent-facing tool surface. If you're
   adding a capability an LLM can call, it terminates here.
10. **`scripts/check-import-boundaries.sh`** — the enforced architecture.
    Read this last; it explains why files 3-7 cannot import from files 8-9.

Bonus reads, in increasing depth:

- **`cmd/mcpd/main.go`** — the MCP daemon entrypoint. ~150 lines; demonstrates
  how a binary other than `cmd/server` consumes `internal/mcp` cleanly.
- **`internal/intake/intake.go`** and **`internal/intake/slack.go`** — the
  async standup intake path (HTTP form + Slack webhook with HMAC verification).
- **`internal/standup/agenda.go`** and **`internal/standup/closer.go`** —
  pre-meeting agenda builder and post-meeting card-creator.
- **`internal/projection/jira/projection.go`** — the first projection that
  reads Jira into the canonical board. Linear and GitHub Issues come next.
- **`docs/adrs/0001`** through **`0004`** — the four ADRs that shaped this
  layout (core/extension boundaries, canonical board, MCP as the universal
  external surface, multi-tenant model). Read them when something surprises
  you.

## 3. Your first PR — add `RiskCritical`

A good starter task that touches the layers without breaking them: introduce
a new `RiskLevel` value `RiskCritical`, ranked above `RiskHigh`, for actions
that require two-human approval. You will not wire the two-human flow itself
— that needs an ADR — but you will land the type, the classification, the
surface, and the test.

### Change locations

1. **`internal/core/types.go` lines 13-25** — add the constant. Follow the
   existing `RiskLow` / `RiskMedium` / `RiskHigh` pattern; the doc comment
   should say "actions that require two distinct human approvers before
   execution":

   ```go
   // RiskCritical identifies actions that require two distinct human approvers
   // before execution (e.g. production data deletion, billing changes).
   RiskCritical RiskLevel = "critical"
   ```

2. **`internal/meetings/types.go` lines 17-19** — re-export the alias so
   `cmd/server` does not have to import `internal/core` directly for risk
   labels:

   ```go
   RiskCritical = core.RiskCritical
   ```

3. **`cmd/server/meeting_intelligence.go` `riskForTool` (around line 121)** —
   classify at least one tool name into the new bucket. `delete_ticket` is
   currently `RiskHigh`; add `delete_sprint` or `wipe_board` as
   `RiskCritical`. Leave `requiresConfirmation` alone — confirmation is
   independent of approver count.

4. **`cmd/server/scrum_tools.go`** — if you added a new tool name, register
   it in the tool list near the existing `delete_ticket` definition. If you
   only re-classified, no change here.

5. **`cmd/server/board_test.go` (around line 166)** — extend
   `TestRiskyVoiceToolsRequireConfirmationAndCanBeConfirmed` with a sibling
   test (`TestCriticalActionsClassifyCorrectly`) that asserts
   `riskForTool("delete_sprint") == toolRiskCritical`. Stay small.

6. **`internal/core/ledger_test.go`** and **`internal/core/registry_test.go`**
   — add a table-driven case that round-trips a `RiskCritical` value through
   the ledger. Don't touch the existing rows.

Total diff target: under 80 lines. Run `go test ./...` and
`bash scripts/check-import-boundaries.sh` before committing. One commit,
message in the same style as the recent log:

- `Broadcast RunQuestion + Plan over WebSocket` (9aa953d4)
- `Add Actor discriminated type for human + agent assignees` (043ca73b)
- `Fix(security): tenant-scope WebSocket fanout (SecArch-001)` (50217b68)

Suggested message: `Add RiskCritical level for two-human-approval actions`.

## 4. Architectural rules you must not break

### Import boundaries (`scripts/check-import-boundaries.sh`)

The script enforces six tiers on every commit. Reproduced verbatim from the
script for clarity:

- **`internal/core`** — most isolated. May import only itself + stdlib. The
  `check_internal_package` regex at lines 47-48 enforces this.
- **`internal/agent`** — provider-neutral domain. May import `internal/core`,
  itself, and `github.com/oklog/ulid/v2` (the only whitelisted external,
  line 36). Every new external dependency here needs an ADR.
- **`internal/mcp`** — MCP protocol surface. May import `core`, `agent`,
  `board`, and itself. Lines 84-91 of the script.
- **`internal/mocks`** — test-only fakes. May import `core`, `agent`,
  `board`, `mcp`, and itself. Lines 69-76. The `mocks.BoardClient` is also
  `cmd/mcpd`'s offline foundation for S2.0.
- **`internal/projection`** — per-system projections (jira, linear,
  github-issues). May import `core`, `board`, and itself. Lines 97-104.
- **`internal/intake`** — async-standup intake (form, Slack, api). May import
  `core`, `board`, `agent`, and itself. Lines 111-118. The Slack adapter
  uses only stdlib `crypto/hmac` so the boundary stays clean.

The remaining `internal/*` packages (board, meetings, standup, auth, tenant,
cost, http, httpapi, jira, voice, extensions) are de-facto leaf packages but
are not pinned by the script today. If you start cycling through them,
propose an ADR before adding them to the enforcer.

**`cmd/server` is the application entrypoint. Nothing may import it.** Lines
120-124 of the script `rg` the entire Go tree for
`github.com/somoore/auto-bot/cmd/server` and fail if any file references it.

If you find yourself wanting to break a boundary, write an ADR first. See
`docs/adrs/0001-core-extension-boundaries.md` for the template.

### One atomic commit per logical change

The branch history is the spec. Recent examples worth imitating:

- `9aa953d Broadcast RunQuestion + Plan over WebSocket` — one feature, all
  the files that ship it, nothing else.
- `043ca73 Add Actor discriminated type for human + agent assignees` — adds
  a type, the migrations, and the tests, in a single commit.
- `Sprint 4.1: internal/standup agenda builder` — a self-contained sprint
  deliverable with both production code and tests.

Do not roll up "fix typo + add feature + refactor" into one commit. Do not
split a single feature across three commits because each subdirectory feels
independent.

### Pre-commit hooks must pass

`scripts/pre-commit` (installed by `scripts/install-hooks.sh`) is the source
of truth. Read it. The full ordered list lives in `CONTRIBUTING.md` section
"Pre-commit hook gate"; the short summary is: tidy/verify, ghost-import
graph, vet, go test, import boundaries, GitHub Actions SHA pinning,
lock-file presence, terraform exact-pin, golangci-lint, goimports,
govulncheck, gosec (with explicit `// #nosec` justifications), terraform
fmt, terragrunt fmt, Docker `@sha256:` pinning, apt `=<version>` pinning,
`:latest` ban, CDN SRI integrity, secrets scan.

### No skipping hooks. Ever.

`--no-verify` is not on the table. If a hook fails, fix the underlying
issue. The hooks exist because each one prevented a real bug. If a hook is
wrong, fix the hook in its own PR.

## 5. How to run your changes locally

```bash
# Go tests (this is what pre-commit runs).
go test ./...

# Per-package test suites currently green on this branch:
go test ./cmd/server/...
go test ./internal/agent/...
go test ./internal/core/...
go test ./internal/intake/...
go test ./internal/mcp/...
go test ./internal/projection/...
go test ./internal/standup/...

# Architecture boundaries.
bash scripts/check-import-boundaries.sh

# Frontend (Vitest + RTL).
cd web/app && npm install && npm test && npm run build && cd -

# Full end-to-end against the running stack.
docker compose build && docker compose up -d
curl -fsS http://localhost:3001/healthz
curl -fsS http://localhost:3001/app/ | head -5
# WebSocket: open the SPA at /app/ and check the browser devtools network
# tab for a successful upgrade to /websocket.

# MCP daemon health: hit the mcpd container directly.
curl -fsS -H "Authorization: Bearer $MCPD_TOKEN" http://localhost:4000/healthz

# Voice path (needs AWS credentials).
scripts/run-openai-keychain.sh    # OpenAI Realtime via Keychain-stored key
scripts/dc-up-keychain.sh         # Nova Sonic via Bedrock; assumes AWS profile

# Async intake (no voice, no AWS needed).
curl -fsS -H "Authorization: Bearer $APP_API_TOKEN" \
     -H "Content-Type: application/json" \
     -d '{"author":"daria","items":[{"summary":"finish onboarding doc"}]}' \
     http://localhost:3001/intake/standup
```

The voice scripts assume you have an `AWS_PROFILE` that can read the
project's secrets. If you don't, skip them — your PR does not need to
exercise voice unless it touches voice code.

## 6. Where to ask for help

- **Existing decisions:** `docs/adrs/` — read these before proposing a
  structural change. The four current ADRs cover core/extension boundaries
  (0001), the canonical board with external projections (0002), MCP as the
  universal external surface (0003), and the multi-tenant model (0004).
- **Security context:** `docs/threat-model.md` and the reviews under
  `docs/security/` (SecArch-001 tenant + MCP + agent identity; SecArch-002
  agent permission + trust ceremony; the silent-failure scans). Read these
  before touching auth, audit, or anything that crosses a trust boundary.
- **Current sprint plan:** `docs/planning/plan.md` and
  `docs/planning/progress.md` — the live priority list.
- **Architecture diagrams:** `docs/architecture.md` and
  `docs/codebase-map.md`.
- **Race-condition postmortem:** `docs/erratum-commit-title-swaps.md`. If
  the commit log says something different than the code, read this first.

If you're still stuck after consulting those: open a draft PR with a
`Question:` prefix in the title. A draft PR with a failing test that
demonstrates your confusion is the highest-bandwidth way to ask for help
here.

## 7. The "you're ready" checklist

You've internalized the project when you can do all six:

1. **Trace a tool call end-to-end** — from a WebSocket message arriving at
   `cmd/server/main.go:215` (the `/websocket` handler), through
   `scrum_tools.go`, through `ApplyToolCallWithMeta`, into the
   `ActionLedger`, and back out as a response.
2. **Trace an MCP tool call end-to-end** — from `claude-code` calling
   `board.list_cards` against `cmd/mcpd`, through `internal/mcp/tools.go`,
   over HTTP to `cmd/server`'s `/internal/tools/dispatch`, into the same
   dispatch path that voice and the SPA use.
3. **Predict which tier a new file belongs in** without re-reading the
   import boundary script.
4. **Write a passing test** in `cmd/server/board_test.go` (or
   `intake_handler_test.go`, or `run_questions_test.go`) that exercises a
   new tool name end-to-end.
5. **Get a clean pre-commit run on a non-trivial change** the first time
   you try.
6. **Know what an ADR is for** and have an opinion on whether your next
   change needs one.

Welcome aboard.
