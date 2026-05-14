# Application Security Review

**Project:** auto-bot (Living Kanban Board)  
**Date:** 2026-05-14  
**Scope:** Full codebase — Go backend (`cmd/server/`), HTML frontends (`web/`), Docker, scripts

---

## CRITICAL

### 1. Unauthenticated LiveKit JWT Minting

**Files:** `cmd/server/main.go:155-169`, `cmd/server/nova_sonic.go:636-647`

The `/livekit-token` endpoint issues LiveKit JWTs with full `RoomJoin` grants to anyone who hits the URL. No authentication, no session, no CSRF protection. Any caller can join the meeting room as any identity. Tokens are valid for 24 hours.

**Impact:** Complete room takeover — an attacker can join, listen to audio, see the board, and issue voice commands as any participant.

**Fix:**
- Require authenticated session (cookie, API key, OIDC) before issuing tokens
- Validate and restrict `identity` parameter server-side
- Shorten TTL to minutes, implement token refresh
- Add rate limiting to the endpoint

### 2. Default LiveKit Credentials Hardcoded

**Files:** `cmd/server/nova_sonic.go:82-84`

`LIVEKIT_API_KEY` defaults to `devkey` and `LIVEKIT_API_SECRET` defaults to `secret`. If deployed without overriding, anyone with these known credentials can mint their own tokens or connect directly to the LiveKit server.

**Impact:** Full room access bypass — attacker mints their own tokens without needing the app server at all.

**Fix:**
- Remove default values; fail fast if unset in production
- Load from a secret manager (AWS Secrets Manager, Vault)
- Rotate keys on every deployment

---

## HIGH

### 3. DOM XSS via Transcription Speaker Name

**File:** `web/index_livekit.html:~1257`

`addTranscriptEntry` sets `speaker.innerHTML` to a template string containing `data.speaker` from WebSocket messages. The speaker name is not escaped. If the backend or a malicious peer injects HTML (e.g., `<img onerror=alert(1)>`), it executes in every connected browser.

**Impact:** Arbitrary JavaScript execution in all connected clients — session hijacking, data exfiltration, board manipulation.

**Fix:**
- Use `textContent` instead of `innerHTML` for speaker name
- Build DOM nodes with `createElement` / `appendChild`
- Never concatenate untrusted strings into `innerHTML`

### 4. WebSocket Origin Validation Disabled

**File:** `cmd/server/main.go:22-24`

`CheckOrigin: func(r *http.Request) bool { return true }` accepts WebSocket connections from any origin. A malicious page on any domain can open a WebSocket to the app in the victim's browser, receiving all kanban events and transcription data.

**Impact:** Cross-site WebSocket hijacking — attacker's page silently connects to the victim's active session.

**Fix:**
- Implement strict origin allowlist matching deployment URLs
- Validate `Origin` header against configured allowed origins
- Consider ticket-based WebSocket URLs with short-lived tokens

### 5. Host Header Injection into Served HTML

**File:** `cmd/server/main.go:131-134`

`Execute(w, "ws://"+r.Host+"/websocket")` embeds the client-supplied `Host` header directly into the HTML template. An attacker controlling the `Host` header can redirect WebSocket connections to an attacker-controlled server.

**Impact:** WebSocket connection hijacking via cache poisoning or direct request manipulation.

**Fix:**
- Use a configurable base URL from environment variables
- Validate `Host` header against an allowlist
- Never reflect request headers into responses without sanitization

### 6. Concurrency Bug — Pointer to Slice Element After Mutex Release

**File:** `cmd/server/board.go:135-160`

The `show_ticket` handler takes `found := &board.cards[i]` under the lock, then releases the mutex before using the pointer. If another goroutine appends to `cards` (causing slice reallocation), the pointer becomes invalid — leading to crashes or data corruption.

**Impact:** Data corruption, panics, potential memory safety issues under concurrent load.

**Fix:**
- Copy the card by value while holding the lock
- Or hold the lock until the response is fully built
- Never retain pointers into a slice after releasing a mutex that guards the slice

### 7. Sensitive Data Logged (SDP, ICE, Transcripts, Tool Args)

**Files:** `cmd/server/main.go:315,449,527,542-555`, `cmd/server/nova_sonic.go:457-469,488`

Full WebSocket payloads, SDP offers/answers, ICE candidates, user transcripts, and tool call arguments are logged. SDP/ICE data enables session hijacking; transcripts contain PII; tool arguments reveal business data.

**Impact:** Information disclosure via log access — credentials, PII, session data exposed.

**Fix:**
- Log only event types and correlation IDs
- Redact SDP, ICE candidates, and transcript content
- Gate verbose logging behind a debug flag
- Never log tool call arguments in production

### 8. No HTTP Server Timeouts

**File:** `cmd/server/main.go:149-152`

`http.ListenAndServe` uses Go's default `http.Server` with no `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, or `IdleTimeout`. Slow clients can hold connections indefinitely (slowloris DoS).

**Impact:** Resource exhaustion — a small number of slow connections can starve the server.

**Fix:**
```go
srv := &http.Server{
    Addr:              addr,
    ReadHeaderTimeout: 5 * time.Second,
    ReadTimeout:       15 * time.Second,
    WriteTimeout:      30 * time.Second,
    IdleTimeout:       60 * time.Second,
    MaxHeaderBytes:    1 << 16,
    Handler:           mux,
}
```

### 9. Unbounded WebSocket Message Size

**Files:** `cmd/server/main.go:363-365,519-525`

No `SetReadLimit` is called on WebSocket connections. Gorilla WebSocket's default allows unbounded frame sizes — a single message can exhaust server memory.

**Impact:** Memory exhaustion DoS with a single malicious WebSocket message.

**Fix:**
- Call `conn.SetReadLimit(maxBytes)` after upgrade (e.g., 64KB for signaling JSON)
- Close connections that exceed the limit

### 10. Unbounded WebSocket Client Registry

**File:** `cmd/server/board.go:534-548,566-588`

Every `/websocket` connection appends to `wsClients` with no cap. Each broadcast fans out to all clients. An attacker can open thousands of connections to amplify CPU and memory usage.

**Impact:** DoS via connection flooding — broadcast amplification.

**Fix:**
- Cap maximum concurrent WebSocket connections (e.g., 50)
- Rate-limit connection upgrades per IP
- Implement idle timeouts
- Authenticate before upgrade

---

## MEDIUM

### 11. No Content Security Policy

**Files:** `web/index.html`, `web/index_livekit.html`, `cmd/server/main.go`

No CSP headers or meta tags. Large inline `<script>` and `<style>` blocks. Without CSP, any XSS vulnerability (like #3) has no defense-in-depth mitigation.

**Impact:** XSS attacks have maximum impact — no browser-level restrictions on script execution.

**Fix:**
- Add CSP header via middleware: `Content-Security-Policy: default-src 'self'; script-src 'self' https://cdn.jsdelivr.net; style-src 'self' 'unsafe-inline'`
- Migrate inline scripts to external files with nonces for strictest policy
- Add `frame-ancestors 'none'` to prevent clickjacking

### 12. No Clickjacking Protection

**Files:** `cmd/server/main.go`, `web/*.html`

No `X-Frame-Options` or `frame-ancestors` CSP directive. The app can be embedded in an invisible iframe to trick users into clicking "Join Room", toggling mic/camera, or confirming board changes.

**Impact:** UI redress attacks — user unknowingly performs actions in the embedded app.

**Fix:**
- Set `X-Frame-Options: DENY` header
- Add `Content-Security-Policy: frame-ancestors 'none'`

### 13. Docker Container Runs as Root

**File:** `Dockerfile`

No `USER` directive. The Go binary runs as root inside the container. If the process is compromised, the attacker has root access within the container.

**Impact:** Increased blast radius of any container escape or process compromise.

**Fix:**
```dockerfile
RUN groupadd -r appuser && useradd -r -g appuser appuser
RUN chown -R appuser:appuser /srv
USER appuser
CMD ["/srv/app"]
```

### 14. `handledCalls` Map Grows Without Bound

**File:** `cmd/server/board.go:95-104`

Every unique tool `call_id` is stored forever for deduplication. Over long-running sessions, this map grows unbounded.

**Impact:** Slow memory leak — eventually OOM under sustained use.

**Fix:**
- Use a bounded LRU cache or TTL-based eviction
- Cap map size and evict oldest entries

### 15. Nova Sonic Tool Calls Lack Deduplication

**File:** `cmd/server/nova_sonic.go:481-503`

The OpenAI path deduplicates via `MarkCallHandled(CallID)`. The Nova Sonic path calls `ApplyToolCall` directly with no duplicate suppression on `toolUseId`. Repeated `toolUse` deliveries can apply mutations multiple times.

**Impact:** Duplicate board operations — cards moved/created/deleted multiple times.

**Fix:**
- Track handled `toolUseId` with the same pattern as the OpenAI path
- Add TTL to prevent unbounded growth

### 16. Identity Parameter Not Validated

**File:** `cmd/server/main.go:155-159`

The `identity` query parameter on `/livekit-token` accepts arbitrary strings with no length, format, or charset validation. Extreme values could create oversized JWTs, confuse logging, or exploit downstream systems that trust the identity format.

**Impact:** Log injection, UI confusion, potential JWT parsing issues.

**Fix:**
- Validate: alphanumeric + hyphens/underscores, max 64 characters
- Generate server-side identities for anonymous flows

### 17. Kanban Tool Inputs Lack Size Bounds

**File:** `cmd/server/board.go:107-167,350-384,416-439`

Card `title`, `notes`, and `tags` fields accept unbounded strings and array sizes. Large values inflate WebSocket broadcasts, memory, and logs.

**Impact:** Memory amplification — a single tool call with a megabyte title broadcasts to all clients.

**Fix:**
- Enforce max lengths (e.g., title: 200 chars, notes: 2000 chars, tags: 20 items × 50 chars)
- Reject oversized inputs before processing

### 18. Internal Errors Leaked to Clients

**Files:** `cmd/server/main.go:161-164`, `cmd/server/kanban.go:160-163`

`http.Error(w, err.Error(), ...)` and `broadcastKanbanEvent(..., err.Error())` expose internal Go error messages to browser clients, potentially revealing implementation details.

**Impact:** Information disclosure — error messages may reveal file paths, library versions, or internal state.

**Fix:**
- Return generic error messages to clients
- Log full errors server-side only

---

## LOW

### 19. LiveKit URL Hardcoded as Plaintext `ws://`

**File:** `web/index_livekit.html:~1054`

`const livekitURL = \`ws://${window.location.hostname}:7880\`` always uses cleartext WebSocket. If served over HTTPS, browsers block mixed content. Without TLS, audio/video streams are unencrypted on the wire.

**Impact:** MITM on audio/video streams; broken functionality when served over HTTPS.

**Fix:**
- Derive scheme from page protocol: `${location.protocol === 'https:' ? 'wss' : 'ws'}://...`
- Make LiveKit URL configurable via server-rendered template variable

### 20. Long-Lived LiveKit Tokens (24h TTL)

**File:** `cmd/server/nova_sonic.go:645`

Tokens are valid for 24 hours. A stolen token grants full room access for the entire duration.

**Impact:** Extended window for token theft exploitation.

**Fix:**
- Reduce TTL to 5-15 minutes
- Implement token refresh mechanism

### 21. WebSocket JSON Parsing Without Error Handling

**Files:** `web/index.html:770,779`, `web/index_livekit.html:1156-1157,1232`

`JSON.parse(event.data)` runs without `try/catch`. Malformed messages crash the handler, disrupting the client session.

**Impact:** Client-side availability issues from malformed messages.

**Fix:**
- Wrap in try/catch
- Validate message schema before processing
- Ignore invalid messages gracefully

### 22. Transcripts and Tool Data Broadcast to All Clients

**File:** `cmd/server/nova_sonic.go:457-469`

All transcription text and tool call results are broadcast to every connected WebSocket client. In multi-tenant scenarios, this leaks cross-session data.

**Impact:** Information disclosure in multi-session deployments.

**Fix:**
- Scope broadcasts to the originating room/session
- Gate transcript distribution by consent

---

## INFO

### 23. No SQL, Command Injection, or Path Traversal Vectors

No database queries, `exec` calls, or file path operations based on user input were found. JSON encoding for API responses reduces injection surface.

### 24. External Script SRI Correctly Applied

`web/index_livekit.html` loads `livekit-client@2.19.0` from jsDelivr with `integrity="sha384-..."` and `crossorigin="anonymous"`. `web/index.html` has no external scripts. Supply chain for CDN dependencies is properly secured.

### 25. Go Module Checksums Verified

`go.sum` contains 264 lines of `h1:` checksums. `go mod verify` passes. All modules are pinned to exact versions. No known vulnerable patterns in dependency usage.

### 26. Audio Mixer Has Backpressure

`cmd/server/audio_mixer.go:68-72` and `cmd/server/nova_sonic_mixer.go:90-94` use bounded channels with drop-on-full semantics, providing some DoS resilience on the audio path.

---

## Remediation Priority

| Priority | Items | Effort |
|----------|-------|--------|
| **Immediate** | #1, #2 (auth + secrets) | Medium |
| **This sprint** | #3, #4, #5, #6 (XSS, CSRF, race condition) | Medium |
| **Next sprint** | #7-#10 (DoS hardening) | Medium |
| **Backlog** | #11-#18 (defense in depth) | Low-Medium |
| **Track** | #19-#22 (low risk) | Low |
