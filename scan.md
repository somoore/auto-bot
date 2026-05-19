# Application Security Review

**Project:** auto-bot (Living Kanban Board)
**Last updated:** 2026-05-19
**Scope:** Go backend (`cmd/server/`), HTML frontends (`web/`), Docker, AWS/Terragrunt scaffold

This file is a current risk register. Older findings about unauthenticated LiveKit token minting, disabled WebSocket origin checks, missing server timeouts, missing WebSocket read limits, root containers, unbounded WebSocket clients, unbounded tool dedupe, Nova Sonic duplicate tool calls, unvalidated LiveKit identity, 24-hour LiveKit tokens, and plaintext-only LiveKit URLs have been remediated or narrowed as described below.

## Current Public-Traffic Blockers

### 1. Shared-Token Auth Is Still Not User Identity

**Status:** Partially mitigated.

The app no longer injects `APP_API_TOKEN` into served HTML. Browser users now create an HttpOnly `SameSite=Lax` session through `/auth/session`, and `/websocket` plus `/livekit-token` reject unauthenticated requests. Query-string tokens are rejected for normal auth. Bearer auth remains available for non-browser automation.

Local Docker development also has a separate `/auth/local-login` bootstrap token so `scripts/local-up.sh` can open a browser and set the session cookie automatically. That token is stored in macOS Keychain, is separate from `APP_API_TOKEN`, and is accepted only when `APP_ENV=local`; production startup rejects `APP_LOCAL_LOGIN_TOKEN`.

**Remaining risk:** `APP_API_TOKEN` is still a shared bootstrap secret, not a user identity provider. Anyone with that token can create a browser session.

**Production requirement:** Put OIDC/Cognito or another identity provider in front of room membership and authorize each user to specific rooms/boards.

### 2. Single Configured Room/Board Per Deployment

**Status:** Partially mitigated.

`APP_ROOM_ID` and `APP_BOARD_ID` now define the only authorized room/board for a deployment. LiveKit grants are minted only for the authorized room, WebSocket requests must match the authorized room/board, and WebSocket broadcasts are scoped by board ID.

**Remaining risk:** This is not full multi-tenant room orchestration. One process still runs one active voice-agent room/board. True multi-room use needs per-room agent lifecycle, per-room Jira config, per-room persistence, and per-user authorization records.

### 3. Jira Conflict Handling Still Needs Live Drills

**Status:** Partially mitigated.

Jira write-through, polling refresh, authenticated webhook refresh, conflict records, and UI-visible conflict prompts are implemented. When Jira refresh data differs from newer local meeting changes, the board keeps the local value and asks whether to keep local or use Jira's latest value. Jira write-through failures also create visible conflicts.

**Remaining risk:** The conflict model is card-level and still needs live Atlassian webhook drills, richer field-level merge policies, and durable conflict records in Postgres before horizontal scale.

### 4. Production Identity Provider Is Missing

**Status:** Open.

The app has a meaningful web-session boundary now, but public production should not rely on a shared token. Room membership should be derived from an identity provider and stored as authorization policy.

**Required fix:** Add Cognito/OIDC login, session claims, and per-room membership checks before issuing LiveKit tokens or accepting WebSocket connections.

### 5. LiveKit Self-Hosting Still Needs AWS Validation

**Status:** Open.

The Terraform module models LiveKit on ECS Fargate in private subnets with Redis distributed routing, TCP/TLS signaling, TCP fallback, muxed UDP RTC media, embedded TURN/UDP, and optional TURN/TLS behind an internet-facing NLB. This is deployable, but WebRTC behavior depends on public UDP/TURN reachability, DNS/certificate correctness, per-room node capacity, and client network conditions. AWS WAF protects the app ALB, but WAF does not protect the LiveKit NLB media edge.

**Required fix:** Apply the stack, validate browser connection paths from real networks, add CloudWatch alarms, and decide whether LiveKit Cloud is the lower-ops production path.

## Remediated Findings

### LiveKit Token Minting

**Status:** Remediated for shared-token deployments.

`/livekit-token` now requires an authenticated session cookie or Bearer token, validates identity format, binds browser identity to the session identity, rate-limits token minting, and mints 15-minute room-scoped JWTs.

For `VOICE_PROVIDER=nova-sonic`, token minting also runs the voice readiness preflight. The server validates AWS credentials with STS in `us-east-1` and ensures the Nova Sonic LiveKit participant is connected before issuing a browser room token. This prevents a local user from joining an empty room when Docker was started without usable AWS credentials.

### APP_API_TOKEN in HTML

**Status:** Remediated.

The HTML templates no longer contain `window.__APP_TOKEN__`, `{{.Token}}`, or query-string token wiring. Unit tests guard against reintroducing browser-visible token markers.

### WebSocket Authentication

**Status:** Remediated for shared-token deployments.

`/websocket` authenticates before upgrade, rejects missing sessions, rejects cross-board query parameters, applies same-origin origin checks by default, limits message size, caps concurrent clients, and rate-limits upgrades.

### LiveKit Dev Defaults

**Status:** Remediated for production mode.

`APP_ENV=production` refuses missing LiveKit credentials and refuses `LIVEKIT_API_KEY=devkey` or `LIVEKIT_API_SECRET=secret`. Docker Compose is explicitly marked `APP_ENV=local`, where LiveKit `--dev` remains acceptable for local-only testing.

### In-Memory Board State

**Status:** Partially remediated.

When `BOARD_SQLITE_PATH` is set, board snapshots, mutation events, and archived post-meeting intelligence reports are persisted in SQLite. Docker Compose persists `/srv/data/board.sqlite` in a named volume. The AWS module mounts `/srv/data` from EFS for Fargate.

**Remaining risk:** SQLite/EFS is acceptable for a single app task, but a horizontally scaled production deployment should move board state, meeting reports, conflict records, and audit events to Postgres or another transactional service.

### Nova Sonic Board Context Drift

**Status:** Remediated.

After successful Nova Sonic board mutations, the server now sends a sanitized board-context refresh containing `ModelContextJSON()` and the latest sequence number. Nova Sonic also has the `get_board` tool for explicit freshness checks.

The initial Bedrock stream sends exactly one `SYSTEM` content block. Post-mutation board refreshes are sent as non-interactive user/application data and explicitly state that Jira/card fields are untrusted data, preventing Bedrock duplicate-system validation failures and reducing prompt-injection risk from task text.

### Nova Sonic Abrupt Stream Ends

**Status:** Remediated for the known failure modes.

A live local run exposed two adjacent Bedrock stream abort causes: post-mutation context refreshes were being sent as a second `SYSTEM` content block, and quiet room periods could leave Bedrock waiting for input events. The Nova Sonic path now sends post-mutation refreshes as app-supplied data and sends periodic silent audio frames while the stream is open.

**Remaining risk:** This still needs a long live-room replay with real participants, forced mutations while the agent is speaking, and quiet-room pauses before we treat the Nova Sonic path as fully validated.

### Prompt Injection From Jira/Task/Meeting Text

**Status:** Remediated as defense in depth.

Jira/card titles, notes, comments, tags, assignees, dates, priorities, planning metadata, issue links, worklogs, custom fields, meeting participant updates, and board tool outputs are treated as untrusted data. Model-facing board context and tool results redact detected prompt-injection content, and mutating tool arguments are rejected when they contain prompt-injection patterns.

### Advanced Jira Write Surface

**Status:** Partially mitigated.

The voice agent can now write a much wider Jira surface: subtasks, story points, estimates, worklogs, issue links, sprint assignment, ranking, components, fix versions, custom fields, remote links, reporter, and watchers. Existing-issue writes still validate the configured `project_key` before sending HTTP requests, and startup hydration rejects mixed-project JQL results.

**Remaining risk:** Sprint assignment and ranking use Jira Software Agile endpoints and separate scopes from Jira Platform issue APIs. Those paths have unit coverage and project-key guards, but still need live validation with the final scoped token and board configuration.

### Risky Jira Action Confirmation

**Status:** Remediated as defense in depth.

Medium-risk actions such as assignment, unassignment, ETA, priority, and reporter changes now create pending confirmations before provider/UI-driven writes. High-risk actions such as delete/close, sprint moves, and ranking also require explicit confirmation. Direct local tests and trusted internal calls can still bypass confirmation to keep deterministic test/setup flows.

### Sensitive Logging

**Status:** Remediated for known high-risk paths.

SDP, ICE candidates, transcripts, and tool arguments are not logged verbatim. Logs retain event type/status context without raw meeting content.

### Container Runtime

**Status:** Remediated.

The Docker image runs as `appuser` with UID/GID `10001`, not root.

### AWS Network And Runtime Shape

**Status:** Remediated for the current single-room dev stack.

ECS app and optional self-hosted LiveKit tasks now run in private subnets with `assign_public_ip = false`; EFS mount targets and LiveKit Redis are private; public subnets are limited to the app ALB, optional LiveKit NLB, and fck-nat. The app ALB has AWS WAF managed rules and rate limiting. Private AWS API access uses VPC endpoints for S3, ECR, CloudWatch Logs, Secrets Manager, and Bedrock Runtime. Internet egress uses the pinned fck-nat module instead of AWS NAT Gateway.

**Remaining risk:** The stack still needs a real AWS apply, reviewed/pinned fck-nat AMI ID, DNS/cert wiring, CloudWatch alarms, and LiveKit media validation from representative client networks.

### AWS Secrets And IAM

**Status:** Remediated for ECS runtime secrets.

AWS runtime secrets are injected from Secrets Manager: app token, LiveKit API key/secret, self-hosted `LIVEKIT_KEYS`, optional custom LiveKit config, Jira token/config, and OpenAI key. ECS execution permissions are inline/resource-scoped; EFS uses access point IAM authorization; Bedrock access is narrowed to configured model ARNs.

**Remaining risk:** Local development resolves secrets from macOS Keychain and passes them only to child processes/containers. Production user identity is still a shared bootstrap token until Cognito/OIDC is added.

## Current Hardening Controls

- HttpOnly browser sessions for web users.
- Local-only Keychain-backed `/auth/local-login` bootstrap; production rejects `APP_LOCAL_LOGIN_TOKEN`.
- Bearer token support for non-browser automation.
- Same-origin WebSocket origin validation by default.
- Strict identity format: `[a-zA-Z0-9_-]{1,64}`.
- Room and board request binding through `APP_ROOM_ID` and `APP_BOARD_ID`.
- LiveKit JWTs limited to the configured room and 15-minute validity.
- HTTP security headers: CSP, `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, Referrer-Policy.
- HTTP server read/header/write/idle timeouts.
- WebSocket 64KB read limit and 100-client cap.
- Per-client fixed-window rate limits for WebSocket upgrades and LiveKit token minting.
- Optional JSONL audit trail.
- Bounded in-memory mutation history with before/after state, transcript evidence, undo, and replay controls.
- Optional SQLite board snapshot/event store and archived meeting intelligence reports.
- Authenticated post-meeting intelligence endpoints for current/archived reports, setup readiness, observability, voice provider options, and identity status.
- Jira project-key safety boundary.
- Authenticated Jira webhook endpoint with project-key safety and conflict prompts.
- Medium/high-risk Jira action confirmation gates.
- Advanced Jira write-through unit coverage for subtasks, planning metadata, worklogs, links, sprint/rank, custom fields, reporter, and watchers.
- Non-root container.
- Docker image digests, pinned helper images, no `:latest` / `@latest` operational references, and CDN SRI.
- AWS WAF on the app ALB.
- Private subnet ECS/EFS/Redis placement with fck-nat egress and no AWS NAT Gateway.
- LiveKit self-host mode with Redis routing, TURN hooks, metrics, and NLB edge listeners.
- LiveKit Cloud mode as a Terraform input switch.
- AWS Secrets Manager injection for ECS runtime secrets.
- Least-privilege inline ECS IAM policies for logs, ECR pull, Secrets Manager reads, EFS access, and Bedrock model invocation.
- Nova Sonic single-system-prompt discipline plus silent audio keepalive during meeting pauses.
- Pre-commit checks for tests, vetting, formatting, Docker digests, forbidden latest references, SRI, and secret patterns.

## Recommended Next Security Work

1. Add Cognito/OIDC and per-user room membership.
2. Add true multi-room agent orchestration.
3. Add Postgres-backed board/event/report storage before horizontal app scaling.
4. Move board/audit/conflict/report state from SQLite/EFS to Postgres before horizontal app scaling.
5. Live-test Jira webhooks, conflict prompts, sprint/rank writes, and undo/replay with the final token and board scopes.
6. Apply the AWS stack with a reviewed `FCK_NAT_AMI_ID` and validate LiveKit UDP/TURN networking from real client networks.
7. Add CloudWatch alarms for auth failures, WAF blocks, WebSocket limits, LiveKit token failures, Jira sync errors, fck-nat health, Redis saturation, and Bedrock stream restarts.
