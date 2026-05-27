# Why agent-first v2 is the wrong shape entirely

Devil's Advocate #2, Sprint 1 exit. Architecture, not implementation. The thesis under attack: that the world needs an agent-first voice-Kanban with mixed human/agent assignees, a durable Run object, an MCP surface, and a closed-loop standup. The shape is wrong end-to-end. Burn most of `internal/board` and ship the one piece with leverage.

## 1. The premise is broken

"The kanban becomes the canonical place where work is assigned" is a 2014 product instinct in a 2026 market. Engineers do not want a third board. They are actively rebelling against the two they have. Linear won taste; Jira won enterprise; nothing else gets installed. Realistic adoption for Yet Another Kanban — even with voice on top — is a flat line through a friendly-team graveyard. The wedge ("voice meetings get the board onto a team") assumes a team will run standup on your tool. They will not. They will run it on Zoom and tolerate a Slack bot summary. The plan budgets a "customer tester" role *after* commit `bcb5008` — architecture chosen before a single user said yes. Wrong order of operations.

And who in 2026 wants *more* standup? Async-first won. Loom won. The product is fighting a receding tide.

## 2. The agent-as-participant abstraction is wrong

`Actor{Kind: human|agent}` (commit `043ca73`) is the load-bearing v2 abstraction and it is theater. An agent is not a teammate; it is a tool you wield. Treating swe-1 as an Actor with an avatar and card-thread voice adds zero leverage and infinite UX surface: every card needs presence semantics for non-humans, every WS payload carries an agent role, every Jira projection silently drops agent assignees (the README admits it — "Jira has no notion of agent assignees"). You pay a tax to model something the rest of the world does not believe in.

The value of "agent posts on card thread" versus "agent opens a PR with evidence and you review it" is negative. PR flow is universal, async, reviewable, revertible, and integrates with every code-review tool on earth. Card-thread flow is bespoke, requires the user inside your UI, and reinvents notification routing. Sprint 1's largest commit (`bcb5008`, +1361/−89) built a `RunCoordinator` to coordinate a fiction.

## 3. MCP is a footgun

MCP in 2026 is still a moving spec; every client (Claude Code, Cursor, Agent SDK, ChatGPT desktop) implements a different subset, none agreeing on auth, streaming, or error shape; tool-call latency stacks badly; the security model for "agents mutate the board remotely" is a disaster waiting. Per-tenant tokens with "scoped capabilities" is the same model that produced years of OAuth-scope CVEs. Realistic abuse: a compromised laptop's Claude Code session silently drains a board, reassigns work to a hostile agent identity, comments fabricated evidence on cards, and the audit log faithfully records every step — exactly the way a real attacker wants it logged. Audit is not defense.

Worse: if the value prop is "any LLM drives the board," then *the board itself is the wrong product*. See §6.

## 4. "Closed-loop standup" is a feature, not a product

Asana has standup bots. Linear has Cycles + automations. Notion has AI summaries. GitHub has Copilot Workspace and Issues-with-agents. Every Sprint 4 deliverable — agenda builder, post-meeting card creation, agent kickoff on close — is two sprints of work for an incumbent with distribution. The supposed moat is "voice + canonical board + MCP." Voice is a commodity. Canonical board is the thing nobody wants you to be. MCP is a protocol, not a product. There is no defensible wedge.

## 5. The trust ceremony is theater

Sprint 4's dry-run + diff preview + undo + pause-all-agents sounds responsible. It is a confession. If agents are doing things destructive enough to need a queue, a diff view, and a panic button, the correct response is *do not let agents do those things*. Gate destructive mutations behind explicit human approval at the point of intent — same as a deploy. The trust-ceremony pattern says "we built dangerous automation and bolted on circuit breakers." Users will read it the same way. The pause-all-agents button is the tell: it exists because someone expects to need it.

## 6. The pivot

Ship an MCP-only product. No board UI. No voice. No `internal/board`, no React app, no `cmd/server`. Just `cmd/mcpd` — a kanban-shaped MCP tool surface any LLM client can drive, backed by adapters to the boards teams already use: Linear, Jira, GitHub Issues, Asana. The product is the *protocol adapter and the agent-ergonomics layer* — durable Runs, ask-the-human, evidence chains, cost — exposed as MCP tools that *project onto someone else's board*. The Run object becomes valuable precisely because no incumbent has it. Voice becomes an optional Zoom plugin, not a wedge. Multi-tenant is trivial — there is no UI to gate. The thesis inverts: Linear/Jira stay canonical for the customer; the Run substrate is canonical for the agent. Much smaller surface, much sharper wedge, installed by an EM in fifteen minutes instead of migrated to over a quarter.

The current shape is a CRM that wants to be Salesforce. The right shape is a Stripe for agent work — one API, lives behind everyone else's UI, charges per Run.
