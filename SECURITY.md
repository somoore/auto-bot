# Security Policy

This file is the responsible-disclosure entry point for auto-bot. If you
believe you have found a security vulnerability in this project, please
follow the process below before opening a public issue or pull request.

## Supported Versions

| Branch | Status | Security fixes? |
|---|---|---|
| `agent-first-v2-sprint-0` | Active development. Receives all security fixes. | Yes. |
| `main` | Pre-release tracking of `agent-first-v2-sprint-0`. | Yes; lags `agent-first-v2-sprint-0` by at most one sprint. |
| `mass-updates-fixes` | v1 — voice-first Jira/standup line. Maintenance only. | High-severity issues only. |
| Earlier branches | Unsupported. | No. |

Active development happens on `agent-first-v2-sprint-0`. Until a tagged
v1.0 release exists, the "supported version" for the purposes of
responsible-disclosure timelines is whichever sprint branch is the head
of active development at the time of the report. Operators running an
older sprint branch should expect to fast-forward to receive a fix; we
do not currently backport.

## Reporting A Vulnerability

Use the channel that fits the severity of what you have found.

**Preferred — private channel.** Email `security@autobot.invalid`
(placeholder until the project's vanity domain is registered; see
`docs/security/contact.md` for the live address once that lands).
Encrypt with the maintainer's GPG key:

```
GPG fingerprint: 0000 0000 0000 0000  0000 0000 0000 0000 0000 0000
(placeholder; see docs/security/gpg-keys.md once published)
```

If GPG is not workable for you, ask for an encrypted channel in the
clear and we will arrange one.

**Fallback — GitHub private vulnerability reporting.** If the maintainer
repository has private vulnerability reporting enabled, open a private
advisory at
`https://github.com/somoore/auto-bot/security/advisories/new`.

**Do not open a public issue, PR, or discussion thread** for
exploitable findings until coordinated disclosure has happened.

### Response SLA

- **Acknowledgement:** initial reply within 5 business days of receipt.
- **Triage and severity assignment:** within 10 business days. We will
  share the CVSS-equivalent severity and our planned remediation
  window with you.
- **Coordinated advisory:** published within 30 calendar days of
  acknowledgement for High and Critical findings, longer if the fix
  requires upstream coordination (Bedrock, LiveKit, Jira, GitHub) that
  is outside our control. We will keep you updated through the window.

If we miss any of these windows, escalate to the maintainer by name
through any public channel.

### What to include

A useful report contains:

- The affected branch and commit hash (most precise: `git rev-parse HEAD`
  output).
- Steps to reproduce, with the smallest payload that triggers the
  finding. Include the exact MCP method, HTTP endpoint, or WebSocket
  message if applicable.
- Impact and exploitability. State what data or capability the finding
  exposes; state any prerequisites (must hold an MCP bearer token, must
  be on the same LAN as the LiveKit edge, etc.).
- Logs, screenshots, or PCAPs as needed, with secrets redacted.
- A suggested fix, if one is obvious. This is not required.

We do not require a PoC for a working exploit; a credible reproduction
recipe is sufficient.

## Scope

### In scope

- The Go server at `cmd/server/` — HTTP, WebSocket, LiveKit token
  minting, voice provider integrations, and the canonical tool-dispatch
  surface in `cmd/server/board.go`.
- The MCP server at `cmd/mcpd/` and `internal/mcp/`, including both the
  HTTP and stdio transports.
- The Slack webhook handler and any future inbound Slack integration.
- The `/internal/*` endpoints and any other operator-only surfaces.
- The persistence layer in `cmd/server/board_store.go` and the tenant
  isolation invariants enforced by `cmd/server/tenant_isolation_test.go`.
- The audit substrate: `boardEventRecord`, `boardMutationRecord`, the
  `agent_runs` and `run_questions` SQLite tables, and the
  `internal/core` ActionLedger interface.
- The browser session boundary at `/auth/session` and the local-only
  `/auth/local-login`.
- The AWS deployment scaffolds (`terraform/`, `terragrunt/`) —
  configuration only.
- Supply-chain controls: Docker digest pinning, `go mod verify`,
  pre-commit hook, dependency review.

### Out of scope

- The AWS console and AWS-managed services themselves. Report those to
  AWS via [vulnerability-reports@amazon.com](mailto:vulnerability-reports@amazon.com).
- Third-party libraries vendored through `go.mod` or `package-lock.json`.
  See `go.mod` and `web/package-lock.json` for the dependency list;
  upstream those to the relevant maintainers. We will accept reports
  about the *usage* of a vulnerable library on our side.
- LiveKit Cloud and the upstream LiveKit project. Report to the LiveKit
  project; we will track the advisory and patch on our side.
- AWS Bedrock model behavior. Prompt-injection findings against the
  models themselves should go to Anthropic / Amazon. Findings about how
  *we* construct prompts and dispatch tool calls are in scope.
- Findings that require physical access to a developer laptop or a
  compromised AWS account. We assume those threat surfaces are owned
  by the operator.
- Local-development conveniences that are explicitly disabled in
  `APP_ENV=production`. See
  `docs/security/application-security-review.md` for the list. The
  carve-out applies only when production correctly rejects the
  development-mode value.

## Bug Bounty Status

There is no monetary bug bounty today. We will consider establishing one
based on report volume and the project's funding state. In the meantime
we will publish acknowledgements in the Hall of Fame section below for
any researcher who works with us under coordinated disclosure.

## Trust Boundaries

The Go server is the policy engine for every board mutation. Browsers
authenticate through an HttpOnly session cookie at `/auth/session`;
non-browser automation can carry `APP_API_TOKEN`; MCP HTTP requires its
own bearer (stdio is currently trusted unconditionally — tracked as
`R-MCP-STDIO-TRUST`). Multi-tenant isolation runs at the SQLite layer:
every store call carries an explicit `tenantID` and every query filters
by it.

Tool dispatch funnels through `ApplyToolCallWithMeta`
(`cmd/server/board.go:286`). Risk-classified tools default-deny without
an explicit `SkipConfirmation: true`; the `Dispatcher` label is
recorded for audit but no longer carries authorization weight
(SecArch-002 fix, commit `baa5c69a`).

See `docs/threat-model.md` for the full STRIDE breakdown.

## Hall of Fame

We thank the following researchers for working with us under
coordinated disclosure. Add your name here by reporting a finding we
patch.

_(Empty. Be the first.)_

## Related Documents

- `docs/threat-model.md` — STRIDE threat model with residual-risk register.
- `docs/security/sec-arch-review-001.md`, `sec-arch-review-002.md`,
  `silent-failure-scan-001.md`, `application-security-review.md` —
  underlying review artifacts.
- `scripts/pre-commit` — canonical local quality gate (CI replays it;
  `--no-verify` is not a route to merge).
