# Daria — First Week with auto-bot

**Persona:** Daria, senior EM, 12-person infra team. Linear-native, async-first, burned by three prior "AI for engineering" tools. Will give auto-bot one standup, one bug investigation, one PR-review cycle before deciding.

**Reviewed:** Build plan `i-would-like-you-enchanted-firefly.md` (D1.1–D1.6, D2.1, D2.5, D2.6, D2.2); README; recent commits through `9aa953d`.

---

## Day 1 — Onboarding

Daria opens `http://localhost:3001`. First impression: the Observatory Deck board (D2.1) looks editorial — not the cyan-on-black "AI dashboard" template she's bored of. Good. But within 30 seconds she does **not** understand the value prop. The columns look like Linear-with-a-podcast. She sees Sprint 0 chips, agent avatars on two cards, a "Pause all agents" pill (desaturated per D-fix.1). There is no landing copy explaining *why agents are on cards*.

The phrase **"Run"** appears in the card drawer's tab strip without context — she assumes it's CI. **"RunQuestion"** never appears in UI text but the architecture doc uses it. Both terms are insider language leaked from the schema.

Time-to-first-useful-action is dominated by Keychain. `scripts/local-up.sh` works — but the README assumes she has `assume`, an AWS profile literally named `test_AccountA/AdministratorAccess`, a Jira token, and a willingness to bind eight Keychain entries before the app loads. For an evaluator, this is the moment she closes the tab. *She gives it a second chance only because a colleague vouched.*

**Day 1 verdict:** value prop unclear, setup is hostile. She would not have made it past Day 1 without a vouch.

---

## Day 2 — First standup

Four engineers join the LiveKit room. Daria stays muted; she watches. The pre-standup agenda overlay (D2.5) appears — highlights, blockers, runs awaiting review, suggested speaker order. **This is the first moment auto-bot earns trust.** It looks like a meeting prep doc, not a chatbot.

What goes well:
- Cards move as people speak. "I shipped the IPv6 thing" → card slides to Done with a transcript-evidence chip. Her team can *see* the agent's reasoning.
- Medium-risk mutations create pending confirmations rather than firing. An "assign to Priya" sits in queue for 6 seconds until the host nods.
- The Slack-ready recap matches Granola's output but with citations back to transcript moments.

What feels weird:
- The agent speaks. Her team is async-first; a voice answering back during standup feels theatrical. She wants a **silent mode** where the agent only writes to board and chat.
- The Nova Sonic voice is named "matthew" in env vars but unnamed in UI. Two engineers ask "who is that?" — small thing, but the answer "it's the bot" undermines the calm Observatory aesthetic.
- The "Agent controls" pill tooltip says "cancel, take over, retry" — three verbs with no explanation of what gets cancelled. She doesn't know the difference between cancelling a Run and pausing all agents.

**Destructive-action comfort:** moderate. The confirmation queue is real and visible. But "Pause all agents" is now buried in a menu (D-fix.1 desaturated it). Daria *wants it more visible*, not less — that's the button she'd hit at 3am if Jira started thrashing. The design moved the wrong direction for the gatekeeper persona.

---

## Day 3 — Async work

Two cards get assigned to agents between meetings: a flaky-test investigation and a PR-review pass. Card halos pulse (D1.1 running-Run treatment). The Run tab shows a state-machine timeline with checkpoints.

**Before** approving the assignment she asks: "What can this agent actually touch?" The answer is scattered across `GITHUB_ALLOWED_REPOS`, `GITHUB_PR_COMMENTS_ENABLED`, Jira project_key constraints, and confirmation gates. No single page summarizes *this agent's blast radius for this card*. She wants a one-liner in the Run header: "Can read repo X, can comment on PRs, cannot merge, cannot move Jira outside EMAL."

The Plan (collapsed per D-fix.1: "7 steps · step 3 in progress · show plan ↓") reads like an LLM thought trace, not a commitment. She wants it stamped with "agent intends to" not "agent will" — wording matters at this trust stage.

**After:** Audit replay. Transcript evidence + before/after + Jira confirmation. **This is the single most credible artifact in the product** — what she'd demo to her VP.

**Cost meter ($0.43/meeting):** reassures. $0.43 is below her coffee. But she immediately asks the honest follow-up: "what's the p99?" The per-run $0.84 is fine; what scares her is an unbounded review on a 4,000-line PR. She wants a **per-run cost ceiling visible on the card before approval**, not in env var `AGENT_COST_BUDGET_CENTS=250`.

---

## Day 4 — A Run pauses for her input (D1.3)

The killer state: "swe-1 is asking." Question: *"The failing test references a deprecated mock for AuthService. Should I (a) update the mock, (b) skip with a TODO, or (c) wait for human review of the underlying API change?"*

The three suggested chips per D-fix.1:
- **[Recommended] Update the mock** — solid weight
- Skip with TODO — secondary
- Wait for human review — dashed border

Her honest reaction: the chips are useful *because* the agent committed to a recommendation. Equal-weight chips would feel patronizing — "you tell me." The visual hierarchy does the work. **Good design.**

What she'd still change:
- She wants a fourth chip: **"delegate question to @priya."** The question is to *her*, the EM, who doesn't own the AuthService refactor.
- "Resume run" reads like restarting CI. Should say "Send answer" — the resume is the agent's problem, not hers.

---

## Day 5 — Review

Daria does not roll out to the full team. She rolls out to **3 of 12** engineers — the Cursor/Claude Code daily users. Quiet pilot.

**Why not full rollout:**
1. Voice-meeting wedge is wrong for her async team. Half her engineers are in Lisbon and Bangalore; standup is written.
2. Jira is canonical at the company level. ADR 0002 (kanban-canonical) is correct strategically but means a *second source of truth* during projection rollout. Risky.
3. Setup. Eight Keychain entries. She cannot ask 12 engineers to do that.

**Single biggest blocker:** **No async surface.** The whole product assumes a voice meeting is the entry point. Until there's a "drop a written standup in chat → cards update → agents fire" path that matches the voice path, this is a tool for synchronous teams and her org is not one.

---

## Three highest-impact UX changes

1. **Async-first standup mode.** A written-standup intake (form, Slack thread import, or chat in the Observatory Deck) producing the same agenda/cards/run-assignment outputs as the voice path. Without this, half the addressable market is excluded. Bigger than any UI polish.

2. **Per-card agent-capability summary.** A one-line "blast radius" stamp on every Run header: scopes, repo allowlist, cost ceiling, what requires human confirmation. Today this is spread across env vars, project keys, and confirmation-gate code. Surfacing it converts "I assume this is safe" into "I can see this is safe" — the entire job at the gatekeeper stage.

3. **Plain-language onboarding without Keychain on first run.** A guest-mode landing at `/` that explains what auto-bot does, what a Run is, what an agent assignee is — with a sandbox board pre-seeded so an evaluator can click *before* configuring AWS, Jira, GitHub, LiveKit, and eight Keychain entries.

Daria's last comment, off the record: *"The audit replay is the best thing in here. If you led with that screenshot instead of the kanban board, I would have given you a meeting."*
