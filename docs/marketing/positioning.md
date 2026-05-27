# auto-bot — Positioning & Landing Copy

**Author:** PM-001
**Date:** 2026-05-26
**Status:** Draft for launch
**Design system:** Observatory Deck (dark) — void `#0B0D14`, sky `#13161F`, atmos `#1B1F2B`, star ivory `#F0E9D6`, aurora teal `#3CDFB1`, solar orange `#FF8C42`, magnetar pink `#FF3D7F`

---

## 1. Positioning statement

**For engineering teams running Jira, auto-bot is the agent-native work surface where the kanban is the canonical place humans and agents are assigned work and communicate back through it — unlike Jira, which treats agents as outside tools poking at a board they don't actually live on.**

**Category lock: "agent-native work surface."** Not "Kanban" — undersells the Run object, MCP surface, and projections; competes head-on with Linear's polish and Jira's enterprise footprint where we lose. Not "AI meeting tool" — Otter/Fireflies own that and the kanban is the wedge, not the product. Not "coding agent surface" — Cursor owns the IDE; the IDE is one MCP client of ours, not the surface. "Agent platform" is too abstract; "work surface" is concrete enough to picture and new enough that no incumbent owns it. The defensible insight: **work assignment is the right primitive for agent coordination**, and no current Kanban treats agents as first-class assignees with durable Runs, ask-the-human threads, and projections to Jira/Linear/GH Issues.

---

## 2. Three taglines

1. **Understated (target: platform engineers).** *The kanban your agents can read and write.*
2. **Mid (target: engineering managers).** *Standup ends. Work starts itself.*
3. **Bold (target: founders).** *Stop assigning work to humans first.*

**Test:** EMs respond to #2 — visibility on what the team is doing without nagging is the daily pain. Platform engineers click #1 — they recognize "MCP" without it being said, and "read and write" promises a real API not a chatbot wrapper. Founders read #3 and either lean in or recoil; that's the right filter for an early-adopter persona. Skip #3 outside founder channels.

---

## 3. Hero copy (above-the-fold)

**Headline (6 words):**
*The kanban where agents do work.*

**Subhead (21 words):**
Voice meetings get the board onto your team. Agents pick up cards, ask you questions, ship evidence. Jira stays in sync.

**Primary CTA:** *Run a standup* (links to local-up.sh quickstart, not a signup wall)
**Secondary CTA:** *Read the MCP spec* (links to `docs/adrs/0003-mcp-server-as-universal-external-surface.md`)

**Hero aesthetic notes:** void background, single thin aurora-teal hairline under the headline, ivory body. No hero illustration — instead, a live-looking board snapshot with one card halo'd in solar orange and a `Run · planning` chip in atmos. No gradients. No glow. No "AI" badge.

---

## 4. Audience value props (60 words each)

### Engineering managers — time saved + visibility

You spend the morning in standup and the rest of the day chasing status. auto-bot runs the standup hands-free, creates the cards, and assigns them — to humans or agents. Every card shows who has it, what the agent is doing right now, what it's waiting on you for, and what it cost. Less coordination, more shipping. Same Jira.

### Platform engineers — MCP surface, agents-as-API

The board is a Model Context Protocol server. Claude Code, Cursor, Claude Agent SDK, and anything else that speaks MCP can list cards, create them, post comments, start Runs, write checkpoints, and ask humans questions — through one audited surface with scoped per-tenant tokens. Every call goes through the same audit log as voice and HTTP. No webhook spaghetti.

### Founders — cost per meeting + automation ratio

Every meeting shows what it cost — tokens, audio seconds, LiveKit minutes — and how many admin minutes were saved versus the baseline. Every Run shows estimated cost before it spends. The automation ratio (agent-handled work over total work) is a number on your dashboard, not a deck slide. You'll see your engineering org's leverage curve in real numbers.

---

## 5. Competitive frame

- **Jira.** We're not them because Jira treats agents as outside tools poking at a board, not as assignees living on it; auto-bot makes Jira an outbound projection so durable Run state, ask-the-human threads, and evidence can exist somewhere Jira's schema can't hold them.
- **Linear.** We're not them because Linear is the best ticket tracker for humans and stops there; we ship voice standup, an MCP surface, and a Run object that Linear deliberately won't add because it would dilute their craft positioning.
- **Asana.** We're not them because Asana sells to the company and ignores engineering practice; auto-bot ships voice standup, Jira sync, MCP, and a code-review agent — Asana ships none of those and won't.
- **Atlassian Rovo.** We're not them because Rovo is bolted onto Jira's existing model; auto-bot inverts the model so the kanban is canonical and Jira is a projection, which is the only shape that lets durable Run state and ask-the-human threads exist.
- **Cursor for teams.** We're not them because Cursor lives in the IDE and stops at the file — Cursor is one MCP client of auto-bot, not a substitute; the work doesn't start or end in the editor and shouldn't be coordinated from inside it.

---

## 6. The "smell test" — three things we never say

1. **"AI-powered."** It signals "we slapped GPT on a CRUD app" to anyone technical. The actual content — Bedrock Claude, Nova Sonic, MCP, Run objects — is more specific and more interesting. Say what's running, not "AI."
2. **"Revolutionizes" / "reimagines" / "transforms."** Engineering audiences read those as marketing tics and stop reading. The product is interesting; the verb should be the boring one (`auto-bot updates Jira`, `the agent asks for input`), not the breathless one.
3. **"10x productivity" / "supercharge your team."** Engineers know productivity isn't a scalar and won't buy from anyone who acts like it is. Replace with the actual measured thing: minutes-of-admin avoided per meeting, automation ratio, cost per Run. The dashboard already computes these.

Bonus banned phrases: "next-generation," "intelligent automation," "human-in-the-loop" (we say "ask-the-human" because that's what the table is called), "seamless," "unleash."

---

## 7. Launch sequence — 4-week arc

| Day | What | Where | Who |
|---|---|---|---|
| **−14** | Build-in-public thread: "Why the kanban should be canonical, not Jira" with a 60-second voice-to-card screencap. Link to ADR 0002. | X, LinkedIn, scott@moore.cloud blog | Scott + PM-1 |
| **−7** | MCP teaser post: "Your kanban is an MCP server now." Includes a Claude Code session reading the board, posting a comment, starting a Run. Link to ADR 0003. | X, Cursor Discord, Claude Code Discord, r/ClaudeAI | PM-3 (developer marketing) |
| **0** | Show HN: "auto-bot — voice standup that assigns work to agents (Go, MCP, Bedrock)." Open source, MIT, golden-demo gif, the $0.84 demo cost number from the Sprint 5 nirvana scenario. autobot.dev goes live. | Hacker News, Lobsters, Go subreddit, Show HN, /r/devops | Scott posts, PM-2 amplifies |
| **+7** | Demo video: 3-minute Loom of the Sprint 5 morning-standup loop. Cards created, agent picks one up, asks a question, ships, Jira stays in sync. Real audio, real cost number. | YouTube, autobot.dev hero secondary, X thread, LinkedIn | PM-2 |
| **+14** | Technical deep-dive: "How we made the kanban canonical and Jira a projection" — projection contract, conflict resolution, replay test. Cross-post on the Atlassian developer subreddit and r/programming. | Engineering blog, HN repost-from-blog, /r/programming | PM-3 + Scribe-2 |
| **+30** | First customer case study: an engineering team's automation ratio over their first 30 days. Real numbers; permission to publish secured at install. | autobot.dev /customers, X, LinkedIn, EM newsletters (Gergely, Charity Majors) | PM-1 + Scott |

**Cadence rule:** every post links back to either the live demo, the MCP spec, or a real number. No post links to a feature page that doesn't exist yet. No post says "AI." If we can't show it running, we don't post it.

---
