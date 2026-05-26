# Sprint 1 — Why this won't ship

Author: Devil's Advocate #1 (DA-1).
Date: 2026-05-26.
Status: Placeholder. The original critique was authored during Sprint 1 exit but was lost in the parallel-agent commit race on 2026-05-26 (see `docs/erratum-commit-title-swaps.md`). This stub records the slot so the audit trail is intact; the next DA-1 pass at Sprint 2 exit should re-derive the critique against the Sprint 1 code that actually landed.

## The frame

"Won't ship" means: the work is real, the tests pass, the demo runs — and yet it fails to reach a paying user, an external contributor, or production within the next two sprints. Not "is broken." "Is stranded."

## Known Sprint 1 risk surfaces (to be expanded by the re-run)

The following were flagged during Sprint 1 execution and remain the natural targets for the recovered critique:

- **RunCoordinator interface ergonomics.** `AskHuman` blocks at the call site; if the calling agent crashes mid-question, the resume path has no test yet.
- **Agent identity at the SQLite layer.** `kanbanActor` discriminated type is correct in Go, but the storage layer flattens to two nullable columns; queries that forget to filter on `kind` will silently return mixed rows.
- **WS broadcast surface.** Adding `RunQuestion` + `Plan` to the board-state payload increases the per-tick wire size; no measurement of impact on the existing meeting clients.
- **Documentation lag.** ADRs 0002–0004 were drafted but not committed; the canonical board flip ships in Sprint 3 without an accepted ADR backing it.

## Action

The Sprint 2 DA-1 pass owns reconstruction. Until then, this stub stands so reviewers see the gap rather than assuming the critique was never assigned.
