# Erratum: Commit Title Swaps from the 2026-05-26 Racing-Tree Incident

On 2026-05-26 a wave of concurrent agents wrote to the same git index. The work itself is durable on `agent-first-v2-sprint-0`; only the commit-message metadata is wrong. Rather than rewrite history on an eleven-commit chain — which would break every existing reference and SHA — this document maps each affected commit to the deliverable it actually contains.

## Mapping

| Commit  | Title as recorded                                          | Actual content                                                                                                  |
|---------|------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------|
| fa26c39 | "Add PM-001: positioning + landing copy"                   | PM-2 + PM-3 deliverables: `docs/marketing/dev-adoption.md` (+177) and `docs/marketing/launch-and-demo.md` (+113). |
| 82d1d59 | "Add new-contributor onboarding guide"                     | PM-1 deliverable: `docs/marketing/positioning.md` (+92). Also deletes the two files fa26c39 added (race artifact). |
| 1c85bbb | "Add PM-002: launch sequence + demo script"                | Scribe-3 deliverable: `docs/onboarding/new-contributor.md` (+284). Plus a 2-line header touch to `docs/security/silent-failure-scan-001.md`. |
| 4793d1f | "Add API reference: HTTP + MCP tools"                      | SWE-2 F1.0 deliverable: 988 LOC of React under `web/app/src/` (App.tsx, components/*, lib/useBoardSocket.ts, types/board.ts, tailwind config). |
| d103479 | "Add SecArch-002: agent permission + trust ceremony review" | Scribe-2 API ref (`docs/api/mcp-tools.md` +648, `docs/api/openapi.yaml` +322) *and* SA-2 review (`docs/security/sec-arch-review-002.md` +258). Two deliverables in one commit. |
| 97d24ae | "Add SecArch-002: agent permission + trust ceremony review" | Not a duplicate, despite the identical title. A 2-line edit to `docs/security/silent-failure-scan-001.md` adding a "Commit: see git log for 'Add SecEng-001'" trailer to the deliverable header. |

## Lost-and-recovered artifacts

The cleanup commit that lands alongside this erratum recovers:

- `docs/marketing/launch-and-demo.md` — created in fa26c39, deleted by the racing 82d1d59, recovered from git (`git show fa26c39:…`).
- `docs/adrs/0002-canonical-board-with-external-projections.md` — Scribe-1 deliverable, never committed during the race; reconstructed from the plan.
- `docs/adrs/0003-mcp-server-as-universal-external-surface.md` — same.
- `docs/adrs/0004-multi-tenant-model.md` — same.
- `docs/critiques/sprint-1-wont-ship.md` — DA-1 deliverable lost in the race; placeholder stub committed, to be re-derived at Sprint 2 exit.

## Why no rebase

Rewriting six commit messages on a shared branch already published to other worktrees would change every SHA downstream, break links in `docs/security/silent-failure-scan-001.md` ("Scope: commits `0f5bfaf..9aa953d`"), and force every reviewer who has fetched the branch to reset. The metadata cost of this erratum is one markdown file. The metadata cost of a rebase is every existing reference to the affected SHAs. We took the cheaper choice.
