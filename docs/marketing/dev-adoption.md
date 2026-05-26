# PM-003: Developer Marketing for the MCP Surface

**Audience:** the engineer who installed 50 dev tools this year and was disappointed by 47. We aren't asking them to convince their VP. We're asking them to paste eight lines into `~/.claude/mcp.json` on a Wednesday afternoon.

**Differentiation:** PM-1 sells the buyer the *board*. PM-2 sells the launch the *story*. PM-3 sells the developer a *tool that makes today's PR easier* — the board sneaks in behind it.

---

## 1. The wedge

Engineers don't need another kanban. They need their coding agent to stop forgetting what it was doing.

**Wedge:** *"My Claude Code agent finally has a place to write down what it's doing that survives the next `/clear`."*

A Claude Code session that runs two hours holds its plan in context. Hit compaction, lose half. Open a second terminal, lose all of it. Cursor and Claude Code can't see each other's work. Engineers end up copy-pasting `TODO.md` between windows.

Auto-bot's MCP surface gives the agent five durable, audited tools — `board.list_cards`, `board.get_card`, `card.create`, `card.update`, `card.comment` — backed by SQLite and broadcast over WebSocket. The agent reads its own plan back after a restart. Two agents on one machine share state through `card.comment`. The engineer never has to open the kanban UI on day one — the wedge is durable agent memory, not the board. Team adoption is the second-order effect.

---

## 2. Five-minute integration guide

**Prerequisites:** `auto-bot` running locally (`scripts/local-up.sh`) or a hosted instance + MCP token.

### Claude Code (`~/.claude/mcp.json`)

```json
{
  "mcpServers": {
    "auto-bot": {
      "command": "/usr/local/bin/mcpd",
      "args": ["--transport", "stdio"],
      "env": {
        "AUTO_BOT_BASE_URL": "http://localhost:3001",
        "AUTO_BOT_MCP_TOKEN": "abk_live_<paste-from-/admin/mcp-tokens>",
        "AUTO_BOT_TENANT_ID": "default"
      }
    }
  }
}
```

### Cursor (`~/.cursor/mcp.json`)

Same shape; Cursor honors the standard `mcpServers` block.

```json
{
  "mcpServers": {
    "auto-bot": {
      "command": "/usr/local/bin/mcpd",
      "args": ["--transport", "stdio"],
      "env": {
        "AUTO_BOT_BASE_URL": "http://localhost:3001",
        "AUTO_BOT_MCP_TOKEN": "abk_live_..."
      }
    }
  }
}
```

### First call: `board.list_cards`

```
> What's on my board?

[claude calls auto-bot::board.list_cards filter={"assignee":"me"}]

{
  "cards": [
    {
      "id": "card_01HNQ...",
      "title": "Wire MCP token rotation into /admin",
      "status": "in-progress",
      "assignee": {"kind":"human","display_name":"scott"},
      "updated_at": "2026-05-26T14:02:11Z",
      "run": null
    },
    {
      "id": "card_01HNR...",
      "title": "Replay test for JiraProjection",
      "status": "todo",
      "assignee": {"kind":"agent","display_name":"pr-reviewer"},
      "run": {"status":"waiting_human","question":"EMAL-11 or EMAL-12?"}
    }
  ],
  "sequence": 4421
}
```

Three cards, one waiting on you. Under five minutes including the restart.

---

## 3. The "fall in love" moment

Three candidates:

| Candidate | Why tempting | Why it loses |
|---|---|---|
| Auto-move card to Done on `git push` | Satisfying | Requires git hook + repo config + status-name mapping. Not 5-minute. |
| `/cards` slash command in Claude Code | Useful | Discovery only; doesn't change agent behavior. |
| **Agent writes checkpoints on its own card; you reload the session and it picks up exactly where it left off** | Solves the actual pain | Wins. |

**The moment:** the engineer kicks off a long refactor with Claude Code. The session hits context limit and compacts away the plan. They type `/clear`, then `pick up where you left off on the auth refactor`. The agent calls `board.get_card` on its own card, reads back the checkpoint trail it wrote thirty minutes ago, and resumes — same plan, same step, same next action.

This wins because (a) it solves the most common Claude Code complaint of 2026 — "it forgot what we were doing" — without asking the engineer to learn the board; (b) the board UI is incidental; the agent uses it as scratch memory; (c) it compounds: by week two the board holds thirty cards of agent memory and is irreplaceable.

We do *not* lead with "AI scrum master." Lead with "your agent stops forgetting."

---

## 4. Distribution channels (priority order)

| # | Channel | Pitch | Realistic conversion | Cost | Owner |
|---|---|---|---|---|---|
| 1 | **Anthropic MCP directory** | "Durable memory for coding agents, kanban behind it." | 500-2000 installs first 30 days if listing + screenshot are sharp; ~5% become WAU. | Eng time. | PM-3 + Scribe-3 |
| 2 | **r/ClaudeAI + r/cursor Show post** | "I built an MCP server so my Claude Code agent stops forgetting between sessions. OSS." Lead with the gif. | Top post: 50-100 installs. | Free. | Founder. No astroturf. |
| 3 | **Show HN** | `Show HN: Durable memory for Claude Code/Cursor agents via MCP`. Tuesday morning. | Front page: 1000-3000. Miss: 50. High variance. | Free; one-shot. | Founder, gif pre-rendered. |
| 4 | **AI-eng YouTube** | Cole Medin, Sam Witteveen, AI Jason, Dave Ebbelaar; long shot Fireship. Pitch: 30-sec clip of Claude Code resuming a refactor after `/clear`. | One Medin/Witteveen feature = 1000-3000. | 2h per creator; 1 in 5 bites. | PM-3 outreach; founder records. |
| 5 | **Twitter/X threads** | 3-tweet thread + 30-sec gif. Tag @AnthropicAI, @cursor_ai. Quote-RT agents-forgetting complaints. | 50-200 per thread at 100K impressions. | Free. | Founder, weekly not daily. |
| 6 | **2026 conferences** | AI Engineer Summit (Oct NYC; CFP open through July), MCP Dev Conf (rumored Q3), KubeCon NA AI day. Skip re:Invent (enterprise), QCon (wrong density). | Talk = 100-300 week-of + durable backlink. | $0-2K travel; 40h per talk. | Founder + Scribe-2. |

**Skip:** Google Ads (uBlock), LinkedIn (wrong density), Product Hunt (PM-2's job; PH is buyers not builders).

---

## 5. Friction killers

Engineer saw the tweet at 2:47 PM. Need them running `board.list_cards` by 2:52 PM. Top five blockers:

1. **"I have to spin up Docker, LiveKit, and AWS Bedrock just to try this?"** Ship `mcpd` as a standalone binary against embedded SQLite — no LiveKit, no Bedrock, no voice. `brew install auto-bot/mcpd && mcpd --local` is self-contained. Voice stack opt-in later.

2. **"What's an MCP token and where do I get one?"** `mcpd --local` mints and prints a token on first run. No `/admin` page in solo mode.

3. **"I don't want to expose port 3001 or set up auth."** Solo mode is stdio-only. No HTTP server runs. Token is a sanity check, not a security boundary.

4. **"The README talks about Jira, Nova Sonic, Bedrock, LiveKit — this isn't for me."** Ship `docs/mcp-quickstart.md` that is *only* the MCP path: install, configure Claude Code, first call. No voice, no Jira, no AWS. The full README stays as the team product.

5. **"Tools don't auto-discover; I have to read docs to know what to ask."** Tool descriptions inside the MCP server itself written so Claude Code reads them aloud. `card.create` description: "Create a card for work in progress. Use when the user asks you to remember something across sessions." The agent figures out when to call without the human reading docs.

---

## 6. The anti-pattern of dev marketing

Three things teams do that look like dev marketing and actually repel engineers. Do not do these.

1. **Email-gating the download.** "Enter your work email to try the MCP server." Engineers close the tab. The install link must be `brew install` or `curl | sh` (with a verifiable checksum), full stop. Sales-team interception of curious devs has killed more bottom-up adoption than any competitor.

2. **Marketing-voiced demo videos.** ("Are *you* tired of context-switching between your AI agents?") The demo must be the founder or an engineer, screen-recorded, real terminal, real mistakes left in. A glossy 2-minute video signals "budget and no users." A 45-second Loom with a typo signals "shipped this last night."

3. **Comparison charts vs. Jira / Linear / Trello with green checkmarks.** Engineers know the format means nothing. Worse, it positions auto-bot as a kanban-replacement when the wedge is *agent memory*. Comparison charts come out after PMF, never before — and even then only against direct MCP-surface competitors (which don't really exist yet; leave the box empty rather than invent one).

---

## 7. Metrics that matter (and ones that lie)

| Metric | Truth | Lie |
|---|---|---|
| **GitHub stars** | Useful only for the Show HN headline. | 10K stars can hide zero WAU. |
| **Total installs** | Sets funnel ceiling. | Counts the curious; `brew install` is not commitment. |
| **Weekly active MCP servers** (server pings `/heartbeat` once per active week) | Real number. | Idle Docker containers — require a real tool call in the week. |
| **First-week retention** (Mon installer → Fri tool call) | Most predictive single number. | Doesn't lie at n > 50. |
| **Median tool calls per WAU** | In-the-loop vs. kicking tires. | Skewed by power users — median, not mean. |
| **Cards created per engineer per week** | Tells us if the agent uses it as memory or the engineer manually nudges it. | Meaningful only from week 2. |

**PMF ratio at the dev layer:** **first-week retention ≥ 40%** of installs, sustained over a rolling 30 days, with median ≥ 5 tool calls per WAU.

- Below 25%: no PMF; the fall-in-love moment isn't landing — change the wedge before spending on distribution.
- 25-40%: PMF-adjacent; iterate on friction killers.
- Above 40%: pour fuel on channels 1-3.

What we will *not* report internally as success: stars, Twitter impressions, total installs, MCP-directory rank. Those are inputs. Retention is the output.

---

**Bottom line:** the MCP surface is not a feature of the kanban. The kanban is a side effect of the MCP surface. Ship the binary, ship the 3-section quickstart, post the gif, measure first-week retention. Everything else is downstream.
