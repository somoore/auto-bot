# New Contributor Onboarding

Goal: from `git clone` to your first PR merged in 90 minutes. If something here is
wrong or out of date, the fix is your first PR.

## 0. Prerequisites

Pinned versions are load-bearing. The Dockerfile, `go.mod`, and
`web/app/package.json` are the source of truth — these are mirrored here so you
can install in one pass.

| Tool | Version | Source of truth |
|------|---------|-----------------|
| Go | 1.26 | `go.mod` line 3 |
| Docker | 29+ (Desktop or Engine, with Compose v2) | `docker-compose.yml` |
| Node | no `engines` pin in repo; Node 18+ required by Vite 6.0.7, Node 22 LTS recommended | `web/app/package.json` |
| TypeScript | 5.6.3 | `web/app/package.json` |
| Vite | 6.0.7 | `web/app/package.json` |
| libopus | 1.5.2 (build) / 1.3.1 (runtime) | `Dockerfile` lines 3-4, 17 |
| golangci-lint | 2.12.2 | `scripts/pre-commit` |
| goimports | v0.45.0 | `scripts/install-hooks.sh` |
| govulncheck | v1.3.0 | `scripts/install-hooks.sh` |
| gosec | v2.26.1 | `scripts/pre-commit` |

macOS install (one-shot):

```bash
brew install go@1.26 node opus pkg-config docker golangci-lint terraform
go install golang.org/x/tools/cmd/goimports@v0.45.0
go install golang.org/x/vuln/cmd/govulncheck@v1.3.0
go install github.com/securego/gosec/v2/cmd/gosec@v2.26.1
```

AWS credentials are optional but required for the voice path (Bedrock Nova
Sonic, OpenAI Realtime via Secrets Manager). Without them, the server boots,
the board works, and tests pass — only voice meetings fail.

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

Use the simpler docker-compose path. `APP_API_TOKEN` is the only required env
var (see `docker-compose.yml:20`):

```bash
export APP_API_TOKEN=$(openssl rand -hex 32)
docker compose build
docker compose up -d
curl -fsS http://localhost:3001/healthz   # → {"ok":true}
```

Open <http://localhost:3001/app/> to confirm the React SPA loads.

To stop: `scripts/local-down.sh` (macOS) or `docker compose down`.

## 2. First read — the 10 files that give you the mental model

Read in this order. Skim, don't memorize. Each file under 200 LoC; this is ~90
minutes of reading at a comfortable pace.

1. **`README.md`** — what we're building, in one screen.
2. **`docs/architecture.md`** — the canonical board + projections model; this
   is the *why* behind every package boundary you'll see.
3. **`internal/core/types.go`** — the universal vocabulary
   (`Action`, `RiskLevel`, `Decision`). If a type isn't here, it's
   provider-specific.
4. **`internal/core/ledger.go`** — append-only audit ledger. Every mutation
   that matters lands here. Read this before you write any handler.
5. **`internal/board/types.go`** — the canonical Board / Issue / Sprint shape
   that all external systems project into.
6. **`internal/agent/types.go`** — `Run`, `Plan`, `Cost`, `WaitingOn`, `Actor`.
   The runtime state machine for agent work.
7. **`internal/agent/coordinator.go`** — the `RunCoordinator` interface +
   default implementation. Read alongside `simple_coordinator.go` to see the
   two compositional layers.
8. **`cmd/server/main.go`** — start at line 188. The HTTP route table is the
   complete external surface of the binary. Everything else is wired off these
   routes.
9. **`cmd/server/scrum_tools.go`** — the agent-facing tool surface. If you're
   adding a capability an LLM can call, it terminates here.
10. **`scripts/check-import-boundaries.sh`** — the enforced architecture. Read
    this last; it explains why files 3-7 can't import from files 8-9.

Bonus: `docs/adrs/0001-core-extension-boundaries.md` through `0004` capture the
four decisions that shaped this layout. Read them when something surprises you.

## 3. Your first PR — add `RiskCritical`

A good starter task that touches the layers without breaking them: introduce a
new `RiskLevel` value `RiskCritical`, ranked above `RiskHigh`, for actions that
require two-human approval. You will not wire the two-human flow itself — that
needs an ADR — but you will land the type, the classification, the surface,
and the test.

### Change locations

1. **`internal/core/types.go:13-25`** — add the constant. Follow the existing
   pattern; the doc comment should say "actions that require two distinct
   human approvers before execution":

   ```go
   // RiskCritical identifies actions that require two distinct human approvers
   // before execution (e.g. production data deletion, billing changes).
   RiskCritical RiskLevel = "critical"
   ```

2. **`internal/meetings/types.go:17-18`** — re-export the alias so
   `cmd/server` doesn't have to import `internal/core` directly for risk
   labels:

   ```go
   RiskCritical = core.RiskCritical
   ```

3. **`cmd/server/meeting_intelligence.go:107-116`** — classify at least one
   tool name into the new bucket. `delete_ticket` is currently `RiskHigh`;
   you might add `wipe_board` or `delete_sprint` as `RiskCritical`. Update
   `requiresConfirmation` if it shouldn't change (it shouldn't — confirmation
   is independent of approver count).

4. **`cmd/server/scrum_tools.go`** — if you added a new tool name, register
   it in the tool list near the existing `delete_ticket` definition. If you
   only re-classified, no change here.

5. **`cmd/server/board_test.go:163`** — extend
   `TestRiskyVoiceToolsRequireConfirmationAndCanBeConfirmed` with a sibling
   test (`TestCriticalActionsClassifyCorrectly`) that asserts
   `riskForTool("delete_sprint") == toolRiskCritical`. Stay small.

6. **`internal/core/registry_test.go:40`** and **`internal/core/ledger_test.go:20`** —
   add a table-driven case that round-trips a `RiskCritical` value through the
   ledger. Don't touch the existing rows.

Total diff target: under 80 lines. Run `go test ./...` and
`bash scripts/check-import-boundaries.sh` before committing. One commit, message
in the same style as `9aa953d` ("Broadcast RunQuestion + Plan over WebSocket")
or `043ca73` ("Add Actor discriminated type for human + agent assignees"):
imperative, present tense, ~50 chars.

Suggested message: `Add RiskCritical level for two-human-approval actions`.

## 4. Architectural rules you must not break

### Import boundaries (`scripts/check-import-boundaries.sh`)

Five tiers, enforced on every commit:

- **`internal/core`** — most isolated. May import only itself + stdlib. This is
  the universal vocabulary; if it pulled in `cmd/server`, the dependency graph
  would invert.
- **`internal/agent`** — provider-neutral domain. May import `internal/core` +
  `github.com/oklog/ulid/v2`. The ULID exception is whitelisted at line 36;
  every new external dependency here needs an ADR.
- **`internal/board`** — canonical board types (no script entry yet; same
  rules as core).
- **`internal/projection`** — per-system projections (jira, linear, github).
  May import `core` + `board` + itself. No SDKs. Provider code lives in
  `cmd/server/jira_ext.go` etc.
- **`internal/mcp`** — MCP protocol surface. May import `core` + `agent` +
  `board` + itself. `cmd/mcpd` wires it.
- **`cmd/server`** — application entrypoint. **Nothing may import this.** The
  script greps the entire tree to enforce this at line 109.

If you find yourself wanting to break a boundary, write an ADR first. See
`docs/adrs/0001-core-extension-boundaries.md` for the template.

### One atomic commit per logical change

The branch history is the spec. Recent examples worth imitating:

- `9aa953d Broadcast RunQuestion + Plan over WebSocket` — one feature, all the
  files that ship it, nothing else.
- `043ca73 Add Actor discriminated type for human + agent assignees` — adds a
  type, the migrations, and the tests, in a single commit.

Do not roll up "fix typo + add feature + refactor" into one commit. Do not split
a single feature across three commits.

### Pre-commit hooks must pass

`scripts/pre-commit` (installed by `scripts/install-hooks.sh`) runs, in order:

1. `go mod tidy` cleanliness
2. `go mod verify` checksums
3. `scripts/check-go-dependencies.sh` (ghost-import check)
4. `go vet ./...`
5. `go test ./...`
6. `scripts/check-import-boundaries.sh`
7. `scripts/check-github-actions-pinning.sh` (immutable SHAs only)
8. Lock-file presence (`go.sum`, `package-lock.json`, etc.)
9. Terraform `required_version` exact-pin check
10. `golangci-lint run`
11. `goimports -l .`
12. `govulncheck ./...`
13. `gosec -exclude-generated ./...`
14. `terraform fmt -check -recursive infra/`
15. Docker image digest pinning (`sha256:`)
16. Secrets scan + SRI integrity for any inline `<script integrity=>` tags

### No skipping hooks. Ever.

`--no-verify` is not on the table. If a hook fails, fix the underlying issue.
The hooks exist because each one prevented a real bug. If a hook is wrong,
fix the hook in its own PR.

## 5. How to run your changes locally

```bash
# Go tests (this is what pre-commit runs)
go test ./...

# Architecture boundaries
bash scripts/check-import-boundaries.sh

# Frontend
cd web/app && npm install && npm run build && cd -

# Full end-to-end against the running stack
docker compose build && docker compose up -d
curl -fsS http://localhost:3001/healthz
curl -fsS http://localhost:3001/app/ | head -5
# WebSocket: open the SPA at /app/ and check the browser devtools network tab
# for a successful upgrade to /websocket.

# Voice path (needs AWS credentials)
scripts/run-openai-keychain.sh    # OpenAI Realtime via Keychain-stored key
scripts/dc-up-keychain.sh         # Nova Sonic via Bedrock; assumes AWS profile
```

The voice scripts assume you have an `AWS_PROFILE` that can read the project's
secrets. If you don't, skip them — your PR doesn't need to exercise voice
unless it touches voice code.

## 6. Where to ask for help

- **Existing decisions:** `docs/adrs/` — read these before proposing a
  structural change. The four current ADRs cover core/extension boundaries,
  the canonical board, MCP as the universal external surface, and the
  multi-tenant model.
- **Security context:** `docs/threat-model.md` and
  `docs/security/application-security-review.md`. Read these before touching
  auth, audit, or anything that crosses a trust boundary.
- **Current sprint plan:** `docs/planning/plan.md` and
  `docs/planning/progress.md` — the live priority list.
- **Architecture diagrams:** `docs/architecture.md` and
  `docs/codebase-map.md`.

If you're still stuck after consulting those: open a draft PR with a
`Question:` prefix in the title. A draft PR with a failing test that
demonstrates your confusion is the highest-bandwidth way to ask for help here.

## 7. The "you're ready" checklist

You've internalized the project when you can do all five:

1. **Trace a tool call end-to-end** — from a WebSocket message arriving at
   `cmd/server/main.go:203`, through `scrum_tools.go`, through
   `ApplyToolCallWithMeta`, into the ledger, and back out as a response.
2. **Predict which tier a new file belongs in** without re-reading the import
   boundary script.
3. **Write a passing test** in `cmd/server/board_test.go` that exercises a new
   tool name end-to-end.
4. **Get a clean pre-commit run on a non-trivial change** the first time you
   try.
5. **Know what an ADR is for** and have an opinion on whether your next change
   needs one.

Welcome aboard.
