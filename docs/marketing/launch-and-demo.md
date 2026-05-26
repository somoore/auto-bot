# Launch Sequence + Demo Script

**Owner:** PM-2. Positioning lives with PM-1; this doc assumes it's final.

---

## 1. 90-second demo script

Open hot. Viewer sees the board within 2 seconds. Close on the dollar number.

| t | Voiceover | On screen |
|---|---|---|
| 0:00 | "It's 8:55 AM — the agent already drafted today's standup." | Pre-standup agenda overlay (D2.5): highlights, 2 blockers, 1 run awaiting review. |
| 0:08 | "Meeting starts; the agent listens." | Board (D2.1). LiveKit tiles top-right. Speaker lit. |
| 0:14 | "Name a card and it halos — the agent is on it." | Card #142 grows accent halo. Cursor never moves. |
| 0:22 | "Before it touches anything, the about-to-do bar counts down." | Bottom bar: "Move #142 to In Progress · 3 · 2 · 1". Anyone can say "wait." |
| 0:30 | "Standup ends. The agent creates three cards and assigns one to itself." | Three cards fly into Backlog. Card #145 gets an agent avatar. |
| 0:38 | "That run is already planning — no one clicked anything." | Drawer slides in. Run tab: `planning`, 7 steps, $0.02. |
| 0:46 | "Eight minutes in, it hits something ambiguous and stops to ask." | Question banner: "v2 endpoint or keep v1 for this tenant?" + suggested chips. |
| 0:56 | "A human answers from their phone over coffee." | Phone mockup. Tap "Use v2". |
| 1:02 | "Run resumes — checkpoint, checkpoint, evidence." | Run timeline fills. Cost: $0.31 → $0.58 → $0.79. |
| 1:14 | "PR opened. Card in awaiting-review. Evidence attached." | Card moves columns. PR chip. Diff preview. |
| 1:22 | "One standup. One agent shipped a PR. **$0.84 spent. 2h 15m saved.**" | Post-meeting summary (D2.6). Dollar number held on screen. |
| 1:28 | "Auto-bot. The kanban your agents live on." | Logo + URL. |

**Cut order if over:** drop 0:14 (halo) before 0:22 (about-to-do — the trust beat).

---

## 2. 30-second variant (HN / Twitter)

| t | Voiceover | On screen |
|---|---|---|
| 0:00 | "Standup over voice. The agent listens and updates the board." | Board with halo + about-to-do bar. |
| 0:08 | "Meeting ends; it creates follow-up cards and assigns one to itself." | Card with agent avatar. |
| 0:14 | "The run stops to ask; a human taps an answer from their phone." | Question banner → phone tap. |
| 0:22 | "PR opens. Card moves to review. Eighty-four cents." | Post-meeting summary, $0.84 highlighted. |
| 0:28 | "Auto-bot — the kanban your agents live on." | Logo. |

---

## 3. Hacker News submission

**Title (58 chars):** `Show HN: Auto-bot – a kanban your AI agents live on (MCP)`

**First comment (≤200 words):**

> Hi HN, Scott here, founder. Quick context and the obvious objection.
>
> Most "AI standup" tools are summarizers — they transcribe and post a digest. We took the other path: the kanban is canonical, every card can be assigned to a human *or* an agent, and agents communicate back on the card thread like a teammate. The board is also an MCP server (`cmd/mcpd`), so Claude Code, Cursor, and any agent SDK can read state, create cards, comment, and start durable Runs that pause to ask the team a question and resume on the answer.
>
> Obvious objection: "another agent kanban that hallucinates moves." Three guardrails. (1) Dry-run: every mutation lands in a pending queue with a before/after diff. (2) About-to-do bar: a 3-second countdown on every voice-driven mutation; anyone in the room can cancel. (3) Cost meter + audit trail per Run, per meeting, per tenant.
>
> Self-host today (Docker), MIT. Repo, demo video, architecture ADRs in the post. I'll be here all day — especially want feedback from people who tried agent-kanban patterns and bounced.

---

## 4. The Show HN angle

**Verifiable claim:**

> We made the kanban an MCP server, so Claude Code can assign itself a ticket, do the work, pause to ask the team a question on the card thread, and resume when answered.

Concrete:

- **Reproducible:** `cmd/mcpd` is a binary. JSON-RPC tools: `board.list_cards`, `card.update`, `run.ask_human`, `run.checkpoint`. Point Claude Code at it in 5 minutes.
- **Falsifiable:** the ask-the-human → resume loop is one user-visible artifact (question banner) backed by `run_questions` SQLite table + state machine. Either it round-trips or it doesn't.

Avoid: "AI-powered standups," "intelligent kanban," "next-gen collaboration." Those die on HN.

---

## 5. Day-by-day launch arc (28 days)

**Day -14 — soft launch.** Personal email to 12 people: 4 eng managers we've shadowed, 3 Claude Code power users (Anthropic Discord), 3 founders running 5-15 person teams, 2 MCP server authors. Ask: "spin up Docker, run one standup, tell me where it broke." Goal: 3 teams using daily by Day 0.

**Day -7 — content seeding.** Three blog posts. (a) "Why the kanban should be canonical, not Jira" — ADR-0002 with diagrams. (b) "We made our kanban an MCP server" — code samples, Claude Code screenshots. (c) "Ask-the-human: the missing primitive in agent workflows." Outreach to 5 newsletters (Pragmatic Engineer, Bytes, Latent Space, TLDR, Refactoring), 8 dev-tool podcasters, 3 journalists.

**Day 0 — launch order.** 06:30 PT Show HN, founder online 8h. 07:00 Twitter thread + 30s video pinned. 07:30 LinkedIn from founder (eng-manager angle). 09:00 email soft-launch participants — no upvote asks, HN bans that. 10:00 Anthropic `#mcp-servers`, Cursor Discord, r/programming (only if HN is going well). CTA everywhere: "`docker compose up` → 5 min to your first agent-run."

**Day +1 — response plan.** Founder Q&A 06:00–22:00 PT. Eng-on-call for GitHub issues. PM-2 triages. Pre-written responses to the 5 inevitable critiques: (1) "just X with a wrapper" → diff vs. nearest neighbor; (2) "can't trust agents on my board" → dry-run + about-to-do gif; (3) "MCP is hype" → JSON-RPC tool list; (4) "voice is gimmicky" → board works without voice; (5) "what about Jira" → outbound projection ADR. Acknowledge, link, move on.

**Day +7 — metrics that matter.** Care: Docker pulls (2k); `cmd/mcpd` initialize from non-localhost (200); ≥1 RunQuestion answered in the wild; inbound from teams of 5+. Ignore: HN upvotes after day 1, Twitter likes, GitHub stars over 1k.

**Day +14 — ship next.** Whatever the launch surfaced as missing. Not a roadmap post — one thing the loudest 20 commenters asked for.

**Day +30 — did it work?** (1) Weekly active tenants ≥ 25. (2) Median Runs/tenant/week ≥ 5. (3) ≥ 1 inbound from a team of 50+. Two of three = win. One = revisit positioning with PM-1.

---

## 6. Three kill criteria (pull the launch if any fire)

1. **Trust regression in soft launch:** any Day-14 team reports an unauthorized board mutation that bypassed dry-run / about-to-do bar. Kills the "agents you can trust" claim. Pull, fix, re-test 14 days.
2. **Cost meter off by >25%** vs. AWS bill in 7-day soft-launch window. "$0.84" is the demo close — if it's wrong, the demo is a lie. Pull, reconcile, re-shoot.
3. **MCP smoke fails T-3 days.** `scripts/validate-golden-demo.sh` + Claude Code MCP smoke (read card, write comment, start run, answer question) all green on a clean clone the Friday before launch. Any red = postpone one week.

---

## 7. Post-mortem template (Day +30)

Sections fixed in advance so we don't rationalize after the fact.

- **Headline numbers.** Pass/fail vs. Day +30 targets.
- **Funnel.** Impressions → visits → Docker pulls → first `mcpd` start → first RunQuestion answered. Biggest leak?
- **Loudest 20 commenters.** Bucket: positioning, trust, pricing, feature gap, wrong-shape. Which dominated?
- **One quote** for the landing page. Real customer, real handle, with permission.
- **Three surprises** (one sentence each).
- **Three changes if we relaunched tomorrow.**
- **Decision.** Continue / pivot wedge / hold. Signed by founder + PM-1 + PM-2.

---

**Demo opening line, verbatim:** "It's 8:55 AM — the agent already drafted today's standup."
