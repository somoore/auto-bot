# Redesign Port Plan — `web/index_livekit.html`

**Strategy:** Restyle in place. Preserve the ~3,975 lines of server-wired JS and all
`getElementById` contracts. Rebuild markup/CSS, add a lobby, convert the settings drawer
into an on-demand Host tools panel. No framework, no build step.

**Server changes:** none required. Favicon is an inline `data:` URI (CSP `img-src data:`
already allows it). Fonts are self-hosted inline `@font-face` (no CSP `font-src` needed).
The only injected template var is `{{.WS}}` — leave it intact.

## Constraints / facts (verified against current code)
- File: `web/index_livekit.html`, 6,095 lines (~1,738 CSS, ~3,975 JS, 106 `getElementById`).
- No favicon route, no static handler — inline favicon only.
- CSP: `script-src` allows `cdn.jsdelivr.net`; `style-src 'unsafe-inline'`; **no `font-src`** → self-host fonts.
- Inter is named but never loaded today (system fallback). Geist Mono not present.
- Existing IDs the JS depends on (must keep): `videoStrip`, `board`, `transcriptionScroll`,
  `meetingConfigToggle`, `meetingAccessDrawer`, `meetingAccessScrim`, `roleHost`,
  `roleParticipant`, `identityInput`, `meetingTypeSelect`, `voiceModelSelect`, `joinModeFull`,
  `joinModeChat`, `createMeeting`, `joinCodeInput`, `join`, `confirmBoard`, agent-workspace IDs, etc.

## Verify loop (critical — avoids editing-with-no-effect)
The published GHCR image bakes the OLD html at `/srv/web/`. To see edits, run with the local
web dir mounted:
```
docker run -d --name auto-bot-dev -p 127.0.0.1:3001:3000 \
  -e APP_ENV=local -e APP_AUTH_MODE=token -e APP_API_TOKEN=dev \
  -e BOARD_SQLITE_PATH=/tmp/board.sqlite \
  -v $(pwd)/web:/srv/web \
  ghcr.io/somoore/auto-bot:0.0.3-prealpha
```
**CRITICAL:** the server reads the template into memory once at boot (`os.ReadFile` +
`template.Must` in `main.go`). A browser reload alone shows stale HTML — after every edit run
`docker restart auto-bot-dev` (≈3s), THEN reload `http://127.0.0.1:3001/` and screenshot.

## Progress (verified in browser, zero console errors)
- [x] Step 1 — head: title "Auto Bot", inline favicon, brand tokens + mono stack
- [x] Step 2 — lobby: built, styled, wired to existing JS (Start/Join/Chat-only), safe error banner
- [x] Step 3 — maritime stage: ink background, tide accents, board/topbar/controls retheme
- [x] Step 4 — drawer → Host tools: button rebranded, panel restyled light, verbose sections hidden
- [ ] Host tools fine polish (condense Control Center 8 lists → 4-card layout)
- [ ] Step 5 — states + mobile + chat sheet + a11y
- [ ] Step 6 — post-meeting page
- Hidden features pending user veto: Theater of Work, Multi-Person Proof, Agent Confidence, Executive recap

## Order (low coupling → high coupling; verify each before next)

### Step 1 — `<head>` (zero JS risk)
- Title → `Auto Bot`.
- Inline favicon (`data:` SVG: navy terminal chip + blue cursor).
- Inline `@font-face` for Geist Mono (logo) + Inter (UI), base64 woff2.
- Keep viewport meta.

### Step 2 — Lobby (additive, new screen)
- New first-screen lobby: `auto_bot█` mark, hero, action card (Start / code→Join),
  identity, Jira/Linear chips, humans+AI background scene.
- Wire **Start a meeting** → existing host setup (`createMeeting` flow / `/meeting/setup`).
- Wire **Join** → existing `joinCodeInput` + `/meeting/join`.
- Wire **Chat only** → existing `joinModeChat`.
- Pull identity from existing SSO/session status call.
- Lobby shows until joined, then reveals the meeting view (existing app shell).

### Step 3 — Meeting stage restyle (medium)
- Apply maritime palette + new tile/board/transcript styling to existing
  `videoStrip` / `board` / `transcriptionScroll` (keep IDs).
- Filmstrip default; board is the tall hero. Brand in header.
- Control bar restyle; add collapse-video affordance.

### Step 4 — Drawer → Host tools panel (highest coupling; do last)
- Replace `meetingConfigToggle` + `meetingAccessDrawer` with the on-demand Host tools sheet.
- Keep the operator-panel IDs the JS writes to (pending confirmations, agent runs, voice
  health, etc.) — re-home them inside the new panel, don't delete.
- Scrim reuse (`meetingAccessScrim`).

### Step 5 — States + mobile polish
- Connecting / voice-failed / empty-board / you're-first states.
- Responsive: lobby + pre-join + in-meeting + chat bottom sheet at mobile width.
- Accessibility contrast pass (slate muted text).

### Step 6 — Post-meeting
- Restyle `web/post_meeting.html` to the recap design (separate, lower risk).

## Out of scope
- No server/Go changes. No framework. No new endpoints.
- Don't touch the WebSocket event names or tool-call payloads.
