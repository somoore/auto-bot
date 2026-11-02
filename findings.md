# Security findings — auto-bot

- **Repository:** `github.com/somoore/auto-bot` @ `baca6b4` (working tree modified, not committed)
- **Scan date:** 2026-11-02
- **Scope:** 43 non-test Go files (~27k LOC), 23 test files, Helm chart, Dockerfile, compose, CI workflows, both web pages.
- **Tools:** gosec, semgrep, trivy (image + config), govulncheck, gitleaks, hadolint, shellcheck, plus manual review.
- **Team roles:** PM (01), AppSec (02), Pentester (03), Devil's Advocate (04), QA (05).
- **Adjudication rule:** the Devil's Advocate wins on verdicts; the pentester wins on reachability.

## Findings

| ID | Severity | Status | Location | Issue | Fix | Verification |
|----|----------|--------|----------|-------|-----|--------------|
| SEC-01 (TEAM, DA-01) | HIGH | **FIXED** | `cmd/server/cf_access.go:146`, `meeting_access.go:495-505`, `:524-535` | Every Cloudflare-Access-authenticated user was treated as meeting host because `cfAccessContext` mints a sessionless context and `authorize`/`isHost` promoted any sessionless caller to host. | Added `requestAuthContext.SessionlessSSO` (set only by CF/ALB SSO), and made sessionless SSO host only when `identityEqual(identity, hostIdentity)`; injected admin bearer and `APP_AUTH_MODE=disabled` still host. | `TestSessionlessSSOParticipantIsNotMeetingHost`, `TestCFAccessContextMarksSessionlessSSO`; full suite green. |
| SEC-02 (TEAM, DA-08, P-01) | HIGH | **FIXED** | `meeting_access.go:448-480`, `:584-598`; `main.go:242-256` | Token-mode `POST /meeting/join` accepted a client-chosen identity equal to the host's; that string became the LiveKit identity/ASR speaker label and passed the voice host gate (case variants included). | `joinSessionLocked` now rejects, under `m.mu`, any participant identity that equals the host or is already in use (case-insensitive); host matching in `identityIsHostLocked` is now exact/case-sensitive. | `TestJoinRejectsHostAndDuplicateIdentity`; existing join/OIDC tests green. |
| SEC-03 (TEAM, DA-02) | HIGH | **FIXED** | `nova_sonic.go:659-705`, `:1079-1097`; `chat_messages.go:376-390` | Tool calls were authorized against `currentOrLastSpeakerLabel()`, which falls back to the last ASR speaker; a participant chat after a host utterance inherited host authority. | Tool authorization now uses `toolAuthorizationSpeakerLabel()`: typed chat uses the authenticated `authCtx.Identity` (`beginChatTurn`, cleared at turn boundaries), speech uses only active speakers with a 3 s ASR grace window, otherwise deny-safe empty. Transcript labelling still uses the old fallback. | `TestChatTurnAuthorizesAgainstAuthenticatedPrincipal`, `TestNovaSonicSpeakerLabelSeparation`; `go test -race ./cmd/server`. |
| SEC-04 | MEDIUM | **FIXED** | `cmd/server/board.go:2562-2594` | `asFloat64` accepted `"NaN"`/`"+Inf"`/`"-Inf"`; `NaN < 0` is false so story points and numeric custom fields could store a non-JSON-encodable float, poisoning every snapshot/broadcast. | `asFloat64` now rejects `math.IsNaN`/`math.IsInf` for every input type (covers `set_story_points`, `set_custom_field`, Jira story points). | `TestStoryPointsRejectNonFinite` (verifies board state still marshals). |
| SEC-05 (TEAM SEC-04) | MEDIUM | **MITIGATED** | `cmd/server/main.go:429-440`, `agent_runs.go:169-252`, `github_app.go:119-153` | `assign_ticket_to_agent` had no host gate while a meeting was active; it can mint a repo-scoped `pull_requests:write` token and post a model-generated PR review. | Added `assign_ticket_to_agent` to `kanbanToolRequiresHost`, so a non-host participant cannot dispatch the run (and its PR review write) during a meeting; existing medium-risk confirmation still applies. | `TestAssignTicketToAgentRequiresHostDuringMeeting`. Residual: no meeting → no host exists; see Residual risk. |
| SEC-06 (P-03) | LOW | **DEFERRED** | `alb_oidc.go:211-253`, `cf_access.go:144` | SSO identity is the lossy email local-part (`john.doe` and `john_doe` collide); truncation at 64 chars. | Not changed: deriving from `sub`/`oid` or rejecting collisions changes documented identity/roster semantics. Low practical impact (needs two allowlisted mailboxes that collide). | Reviewed only; tracked in residual risk. |
| SEC-07 (TEAM SEC-03, DA-03) | LOW→MEDIUM | **FIXED** | `meeting_intelligence.go:19-65`, `:227-255`, `:306-345`, `:401-440`; `main.go:389-397`; `nova_sonic.go:1085-1095` | Pending confirmations were board-global and the confirming actor was never compared to the requester; any client could confirm/cancel another actor's staged action by id. | `toolCallMeta`/`pendingConfirmation` now carry a server-derived `Requester` (never client-asserted) and `RequesterIsHost`; `confirmPendingAction`/`cancelPendingConfirmation` deny in-lock when the caller is neither the requester nor a host. | `TestConfirmationBoundToRequester`; existing `RestrictToParticipantConfirmable` and mixed-risk tests green. |
| SEC-08 (DA-05) | LOW | **ACCEPTED RISK** | `auth.go:474-522`, `web/index_livekit.html:4825` | Local browser bootstrap authenticates on `X-Auto-Bot-Local-Session: 1`; `APP_LOCAL_LOGIN_TOKEN` acts as a feature flag. | Not changed: requires `APP_ENV=local`, token mode, non-empty `APP_LOCAL_LOGIN_TOKEN`, localhost Host; the UI has no token to send in a header, so "fixing" it would break local dev. | Code review of guard; startup refuses the token outside local. |
| SEC-09 | LOW | **DEFERRED** | `meeting_intelligence.go:246`, `board.go:2366-2393` | Confirmation broadcast occurs while `board.mu` is held; a slow/non-reading client can stall board mutations. | Not changed: needs collect-then-send refactor; bounded by 10 s write deadline / 60 s pong timeout. | Reviewed only. |
| SEC-10 (SB-06, SB-07, DA-06) | LOW | **FIXED** | `deploy/helm/auto-bot/templates/deployment.yaml:25-38`, `values.yaml:96-104` | No container `securityContext` (root FS writable, caps retained, seccomp unset, privilege escalation allowed) and no CPU limit. | Added numeric `runAsUser/runAsGroup: 10001`, `runAsNonRoot`, `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem: true`, `capabilities.drop: [ALL]`, `seccompProfile: RuntimeDefault`; added a generous `cpu: "2"` limit. | `helm lint` + `helm template` (default and `awsRolesAnywhere.enabled=true`) render the fields. |
| SEC-11 (SB-01, SB-02, SB-04, SB-20) | LOW | **MITIGATED** | `go.mod` | Dependency advisories: go-jose (JWK/JWE DoS), x/net html, cel-go. | Bumped `golang.org/x/net` 0.60.0→0.62.0 and `github.com/google/cel-go` 0.29.0→0.30.0 (and transitive x/*). go-jose has no fixed release and is not symbol-reachable. | `govulncheck ./...`: 0 called vulnerabilities, 1 module-only (go-jose). |
| SEC-12 | LOW | **DEFERRED** | `board_store.go:61-90` | SQLite DB/WAL/SHM files inherit umask (typically 0644) inside a 0700 directory. | Not changed: parent dir is 0700 and the path is operator-controlled; chmod would need platform-specific handling. | Reviewed only. |
| SEC-13 (P-06) | LOW | **DEFERRED** | `auth.go:638-679`, `main.go:27` | `POST /auth/session` and `/jira/webhook` compare secrets with no rate limit; `tokenRateLimit` is a dead constant. | Not changed: add a fixed-window limiter + constant delay in a follow-up; default token is already rejected outside local. | Reviewed only. |
| SEC-14 (P-04) | INFO | **ACCEPTED RISK** | `voice_status.go:201-218` | `/voice/status` echoes scrubbed-but-raw AWS SDK errors to any participant. | Not changed: low value to the same audience that can already read board/Jira data; mapping error classes is cosmetic. | Reviewed only. |
| SEC-15 | INFO | **DEFERRED** | `alb_oidc.go:371-419`, `cf_access.go:266-343` | JWKS/key caches are unbounded maps keyed by attacker-influenced `kid`, and misses trigger outbound fetches. | Not changed: bounded by TLS ingress and allowlists; would need an LRU + negative cache. | Reviewed only. |
| SEC-16 | INFO | **ACCEPTED RISK** | `scripts/jira-*-config.sh` | `curl -u "$EMAIL:$TOKEN"` exposes the Jira token in argv on a multi-user host. | Not changed: local operator tooling; use `--netrc`/env in a follow-up. | Reviewed only. |
| SEC-17 | INFO | **DEFERRED** | `.github/workflows/ci.yml:50,55` | CI installs `staticcheck@latest` / `govulncheck@latest` (unpinned executable) in a job with the source tree and `GITHUB_TOKEN`. | Not changed: pinning requires version selection and a verified install; actions themselves are SHA-pinned (checked by script). | `scripts/check-github-actions-pinning.sh` passes. |
| P-02 | MEDIUM | **DEFERRED** | `main.go:298-347`, `meeting_access.go:272-274`, `nova_sonic.go:1325-1345` | WebSocket authorization happens only at upgrade; sockets are not closed/re-checked when a session is revoked or a meeting ends, and LiveKit grants stay valid ~15 min. | Not changed: needs per-client session binding + socket close/re-auth and LiveKit `RemoveParticipant`; larger lifecycle refactor than this release. | Reviewed only; tracked in residual risk. |
| P-05 | LOW | **DEFERRED** | `main.go:22,298-310` | `maxWSClients=100` is global, not per-identity; one participant can exhaust it. | Not changed: add a per-identity cap in a follow-up. | Reviewed only. |
| BUILD-01 | HIGH (build) | **FIXED** | `Dockerfile:16-21` | Stale Debian pins (`libssl3`/`openssl=3.0.22-1~deb12u1`) were superseded by `3.0.22-1~deb12u2`, so the runtime stage failed and the image build was red. | Updated both OpenSSL pins to `3.0.22-1~deb12u2` (current bookworm-security); other pins verified current. | `docker build --platform linux/amd64 .` succeeds. |
| SB-01 | HIGH | **ACCEPTED RISK** | `go.mod` (`go-jose/v3 v3.0.5`) | CVE-2026-116587 JWK RSA-param DoS. | No fixed release exists; go-jose is only linked through `livekit/protocol/auth` and `govulncheck` reports no symbol trace (app hand-rolls ALB/CF JWT verification). | `go mod why` / `govulncheck` symbol scan: no call path. |
| SB-02 | MEDIUM | **ACCEPTED RISK** | `go.mod` | CVE-2026-116585/6 JWE parse/decrypt DoS. | Same package/usage as SB-01; this server performs no JWE. | As SB-01. |
| SB-03 | UNKNOWN | **FALSE POSITIVE** | `golang.org/x/crypto` | GO-2026-5932 `openpgp`. | The `openpgp` package is not linked (`go list -deps`); govulncheck trace is module-only. | `go list -deps ./...` has no openpgp; govulncheck no symbol trace. |
| SB-04 | UNKNOWN | **FIXED** | `go.mod` (`golang.org/x/net`) | CVE-2026-78661 / CVE-2026-97045 in `x/net/html` (not imported). | Bumped to v0.62.0; `go mod tidy` clean. | `govulncheck ./...` no x/net finding. |
| SB-05 | HIGH | **FALSE POSITIVE** | `$GOCACHE/...` ×10 | gosec G115 integer conversions. | All 10 records are cgo-generated build-cache wrappers for `opus_decoder.go`/`opus_encoder.go`, not repo source; conversions are bounded RTP/frame sizes. | `gosec -exclude-generated ./...` → 0 issues. |
| SB-06 | HIGH | **FIXED** | `deploy/helm/auto-bot/templates/deployment.yaml` | KSV-0014/0118: writable root FS, default security context. | Same fix as SEC-10. | `helm template` shows hardened `securityContext`. |
| SB-07 | LOW/MEDIUM | **FIXED** | same | KSV-0001/0003/0004/0011/0030/0104/0106: caps, privilege escalation, seccomp, CPU limit. | Same fix as SEC-10. | `helm template` shows drop ALL, seccomp, CPU limit. |
| SB-08 | MEDIUM | **FALSE POSITIVE** | `Dockerfile:22-28` | KSV-0012 "runs as root". | Image creates uid/gid 10001 and ends `USER appuser`; chart does not override (now also asserts numeric uid). | Dockerfile inspection + rendered `runAsUser: 10001`. |
| SB-09 | LOW | **FALSE POSITIVE** | Helm/Dockerfile | KSV-0020/0021 UID/GID ≤ 10000. | Image and chart use 10001. | As above. |
| SB-10 | LOW | **FALSE POSITIVE** | chart | KSV-0110 default namespace. | Helm installs into the release namespace; no `namespace:` field by design. | Chart review. |
| SB-11 | MEDIUM | **FALSE POSITIVE** | `values.yaml:5` | KSV-0125 trusted registries. | Image is `ghcr.io/somoore/auto-bot`; registry trust is cluster policy, not a chart defect. | Chart review. |
| SB-12 | HIGH | **FALSE POSITIVE** | `cmd/server/jira.go:151` | semgrep dangerous exec. | `exec.CommandContext` argv comes from operator config, no shell, no request data; reaching it equals process control. | Code review; justified `#nosec G204`. |
| SB-13 | HIGH | **FALSE POSITIVE** | `main.go:668`, `docker-compose.yml:32` | semgrep insecure WebSocket. | `ws://` is local/dev or derived from plain-HTTP `APP_BASE_URL`; production derives `wss` from TLS/X-Forwarded-Proto. Compose is loopback + `APP_ENV=local`. | Code review. |
| SB-14 | MEDIUM | **FALSE POSITIVE** | `auth.go:589,603` | semgrep cookie missing Secure. | `Secure: secureCookies(r)` is unconditionally true outside `APP_ENV=local`. | Code review; justified `#nosec G124`. |
| SB-15 | MEDIUM | **FALSE POSITIVE** | `auth.go:708` | semgrep open redirect. | Sink requires `APP_ENV=local` + localhost + valid `APP_LOCAL_LOGIN_TOKEN`; sanitizer rejects absolute/`//`/host values. Not reachable in production (note: `/\evil.com` normalization is a local-only weakness). | Code review. |
| SB-16 | MEDIUM | **FALSE POSITIVE** | `main.go:311` | semgrep missing WS origin check. | `upgrader.CheckOrigin = makeOriginChecker(...)` is set in `main()`; default is same-host. Semgrep cannot see the assignment. | Code review (`main.go:82`). |
| SB-17 | MEDIUM | **FALSE POSITIVE** | `meeting_report_handlers.go:22` | semgrep direct ResponseWriter write. | Writes static `web/post_meeting.html` with explicit `text/html; charset=utf-8`; no interpolation. | Code review. |
| SB-18 | MEDIUM | **FALSE POSITIVE** | both web pages | semgrep missing SRI. | Flagged tag is the inline `data:` favicon; the only external script carries `integrity=`. | Code review. |
| SB-19 | LOW | **FALSE POSITIVE** | Dockerfile/scripts | hadolint DL3059/DL3066, shellcheck SC2155, DS-0026. | Style/portability only; k8s probes replace `HEALTHCHECK`; `USER appuser` resolves at build time. | Tool output review. |
| SB-20 | LOW | **FIXED** | `go.mod` (`cel-go`) | GO-2026-6094. | Bumped to v0.30.0; `protovalidate` build still green. | `govulncheck ./...` no cel-go finding. |

## Verification results

Run in `/Users/scottmoore/github/auto-bot`:

| Command | Result |
|---|---|
| `gofmt -l cmd/server` | no output |
| `go build ./...` | PASS |
| `go vet ./...` | PASS |
| `go test ./...` | PASS (`cmd/server`, `internal/core`) |
| `go test -race ./cmd/server` | PASS |
| `golangci-lint run` | 0 issues |
| `staticcheck ./...` | PASS |
| `gosec -exclude-generated ./...` | 0 issues (41 files) |
| `govulncheck ./...` | 0 called vulnerabilities; 1 module-level (go-jose) |
| `go mod verify` | all modules verified |
| `go mod tidy` | no further changes |
| `gitleaks detect --redact` | no leaks found |
| `scripts/check-import-boundaries.sh` | ok |
| `scripts/check-github-actions-pinning.sh` | ok |
| `helm lint deploy/helm/auto-bot` | 0 failed |
| `helm template ...` (default and Roles Anywhere) | PASS |
| `docker build --platform linux/amd64 .` | PASS |

## Residual risk

- **GitHub agent dispatch outside a meeting (SEC-05).** The host gate only applies while a meeting is active; with no meeting there is no host identity to bind to, so any authenticated user may still dispatch an agent. `GITHUB_ALLOWED_REPOS` is still permissive when unset (any installation-visible repo), though the token stays repo-scoped and PR comments remain behind `GITHUB_PR_COMMENTS_ENABLED`. Recommended follow-up: require a non-empty allowlist whenever the GitHub App is configured.
- **go-jose advisories (SB-01/SB-02).** No fixed release; not symbol-reachable today. Track `livekit/protocol` and bump when it does.
- **Identity projection collisions (SEC-06/P-03).** Sanitized email local-parts can collide/truncate; needs a `sub`/`oid`-derived identity and is a docs/behavior change.
- **WebSocket revocation (P-02).** Sockets are authorized at upgrade only; a removed participant keeps a live socket and a valid LiveKit grant until it expires. Requires per-client session binding, socket close/re-auth, and LiveKit `RemoveParticipant`.
- **Chat-turn concurrency (SEC-03).** The explicit chat principal is a single per-app field; two simultaneous chat authors can race, and the most recent author wins. In practice this can misattribute one tool call (usually to deny a host action), not grant host authority; a per-turn queue is the durable fix.
- **Receive-side standup.** The 3 s ASR grace window is deliberately short but not zero; a host-spoken tool call arriving just after `ActiveSpeakersChanged` clears is still authorized. This preserves legitimate host voice commands and is bounded to the host's own utterance.
- **Deferred low/INFO items:** SEC-09 (broadcast under lock), SEC-12 (DB file modes), SEC-13/P-06 (no auth rate limit), SEC-15 (unbounded JWKS cache), SEC-17 (unpinned CI tool installs), P-05 (global WS cap). None are reachable escalations without an already-authenticated participant.
- **Accepted risk:** SEC-08 (local-only session bootstrap), SEC-14 (voice status error text), SEC-16 (Jira token in local script argv), go-jose.
- No secrets were found, so no rotation is required. No git history was rewritten.
