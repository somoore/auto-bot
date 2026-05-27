// Package intake owns the async-standup intake surface.
//
// Daria's first-week walkthrough (docs/persona-feedback/daria-first-week.md)
// identifies the voice-only assumption as the single biggest blocker for
// distributed-team adoption. internal/intake gives async-first teams a
// written intake path that produces the same agent-driven outputs as the
// voice standup: structured per-participant yesterday/today/blockers plus
// references to existing cards.
//
// The package is intentionally provider-neutral. It owns:
//
//   - The Intake struct (the canonical async-standup record) and the
//     BlockerItem child type.
//   - A heuristic Parser that normalizes free-form text into a structured
//     Intake. cmd/server may swap in a Bedrock-backed parser later, but the
//     default heuristic keeps internal/intake usable in tests and tools.
//   - A Slack signature verifier (HMAC-SHA256, constant-time compare,
//     5-minute replay window) so callers can mount the Slack webhook
//     adapter without reaching for an extra dependency.
//   - An in-memory Store that indexes recent intakes per tenant + board.
//     Persistence is intentionally out of scope; the board snapshot is the
//     long-lived artifact and intake records exist only long enough to
//     fold into the next standup or agent run.
//
// Import boundary: internal/intake imports internal/core and internal/board
// only. It must not pull in cmd/server (the application entrypoint) or any
// provider SDK. The import boundary is enforced by
// scripts/check-import-boundaries.sh.
package intake
