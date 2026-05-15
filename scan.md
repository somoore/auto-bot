# Application Security Review

**Project:** auto-bot (Living Kanban Board)
**Last updated:** 2026-05-15
**Scope:** Go backend (`cmd/server/`), HTML frontends (`web/`), Docker, AWS/Terragrunt scaffold

This file is a current risk register. Older findings about unauthenticated LiveKit token minting, disabled WebSocket origin checks, missing server timeouts, missing WebSocket read limits, root containers, unbounded WebSocket clients, unbounded tool dedupe, Nova Sonic duplicate tool calls, unvalidated LiveKit identity, 24-hour LiveKit tokens, and plaintext-only LiveKit URLs have been remediated or narrowed as described below.

## Current Public-Traffic Blockers

### 1. Shared-Token Auth Is Still Not User Identity

**Status:** Partially mitigated.

The app no longer injects `APP_API_TOKEN` into served HTML. Browser users now create an HttpOnly `SameSite=Lax` session through `/auth/session`, and `/websocket` plus `/livekit-token` reject unauthenticated requests. Query-string tokens are rejected. Bearer auth remains available for non-browser automation.

**Remaining risk:** `APP_API_TOKEN` is still a shared bootstrap secret, not a user identity provider. Anyone with that token can create a browser session.

**Production requirement:** Put OIDC/Cognito or another identity provider in front of room membership and authorize each user to specific rooms/boards.

### 2. Single Configured Room/Board Per Deployment

**Status:** Partially mitigated.

`APP_ROOM_ID` and `APP_BOARD_ID` now define the only authorized room/board for a deployment. LiveKit grants are minted only for the authorized room, WebSocket requests must match the authorized room/board, and WebSocket broadcasts are scoped by board ID.

**Remaining risk:** This is not full multi-tenant room orchestration. One process still runs one active voice-agent room/board. True multi-room use needs per-room agent lifecycle, per-room Jira config, per-room persistence, and per-user authorization records.

### 3. Jira Conflict Handling Is Last-Writer-Wins

**Status:** Open.

Jira write-through and polling refresh are implemented, but there is no conflict journal for simultaneous Jira/UI/voice updates. A later poll or voice write can overwrite a concurrent external edit without surfacing a losing-write event.

**Required fix:** Add revision tracking, conflict audit events, and user-visible conflict resolution for Jira-backed boards.

### 4. Production Identity Provider Is Missing

**Status:** Open.

The app has a meaningful web-session boundary now, but public production should not rely on a shared token. Room membership should be derived from an identity provider and stored as authorization policy.

**Required fix:** Add Cognito/OIDC login, session claims, and per-room membership checks before issuing LiveKit tokens or accepting WebSocket connections.

### 5. LiveKit Self-Hosting Still Needs AWS Validation

**Status:** Open.

The Terraform module models LiveKit on ECS Fargate with TCP signaling, TCP fallback, and muxed UDP RTC media behind an NLB. This is deployable, but WebRTC behavior depends on public UDP reachability and client network conditions.

**Required fix:** Apply the stack, validate browser connection paths from real networks, and decide whether LiveKit Cloud is the lower-ops production path.

## Remediated Findings

### LiveKit Token Minting

**Status:** Remediated for shared-token deployments.

`/livekit-token` now requires an authenticated session cookie or Bearer token, validates identity format, binds browser identity to the session identity, rate-limits token minting, and mints 15-minute room-scoped JWTs.

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

When `BOARD_SQLITE_PATH` is set, board snapshots and mutation events are persisted in SQLite. Docker Compose persists `/srv/data/board.sqlite` in a named volume. The AWS module mounts `/srv/data` from EFS for Fargate.

**Remaining risk:** SQLite/EFS is acceptable for a single app task, but a horizontally scaled production deployment should move board state to Postgres or another transactional service.

### Nova Sonic Board Context Drift

**Status:** Remediated.

After successful Nova Sonic board mutations, the server now sends a sanitized board-context refresh containing `ModelContextJSON()` and the latest sequence number. Nova Sonic also has the `get_board` tool for explicit freshness checks.

### Prompt Injection From Jira/Task/Meeting Text

**Status:** Remediated as defense in depth.

Jira/card titles, notes, comments, tags, assignees, dates, priorities, planning metadata, issue links, worklogs, custom fields, meeting participant updates, and board tool outputs are treated as untrusted data. Model-facing board context and tool results redact detected prompt-injection content, and mutating tool arguments are rejected when they contain prompt-injection patterns.

### Advanced Jira Write Surface

**Status:** Partially mitigated.

The voice agent can now write a much wider Jira surface: subtasks, story points, estimates, worklogs, issue links, sprint assignment, ranking, components, fix versions, custom fields, remote links, reporter, and watchers. Existing-issue writes still validate the configured `project_key` before sending HTTP requests, and startup hydration rejects mixed-project JQL results.

**Remaining risk:** Sprint assignment and ranking use Jira Software Agile endpoints and separate scopes from Jira Platform issue APIs. Those paths have unit coverage and project-key guards, but still need live validation with the final scoped token and board configuration.

### Sensitive Logging

**Status:** Remediated for known high-risk paths.

SDP, ICE candidates, transcripts, and tool arguments are not logged verbatim. Logs retain event type/status context without raw meeting content.

### Container Runtime

**Status:** Remediated.

The Docker image runs as `appuser` with UID/GID `10001`, not root.

## Current Hardening Controls

- HttpOnly browser sessions for web users.
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
- Optional SQLite board snapshot/event store.
- Jira project-key safety boundary.
- Advanced Jira write-through unit coverage for subtasks, planning metadata, worklogs, links, sprint/rank, custom fields, reporter, and watchers.
- Non-root container.
- Docker image digests and CDN SRI.
- Pre-commit checks for tests, vetting, formatting, Docker digests, SRI, and secret patterns.

## Recommended Next Security Work

1. Add Cognito/OIDC and per-user room membership.
2. Add true multi-room agent orchestration.
3. Add Postgres-backed board/event storage before horizontal app scaling.
4. Add Jira conflict detection, losing-write audit records, and user-visible sync failure state.
5. Live-test Jira Software sprint/rank writes with the final token and board scopes.
6. Apply the AWS stack and validate LiveKit networking from real client networks.
7. Add CloudWatch dashboards and alarms for auth failures, WebSocket limits, LiveKit token failures, Jira sync errors, and Bedrock stream restarts.
