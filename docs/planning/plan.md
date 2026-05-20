# Living Kanban Board — Project Plan

**Moore.cloud**
*Voice-agent-first. Agent-executed. Human-as-fallback.*

---

## Vision

A Kanban board where standup happens by voice, the board syncs bidirectionally with Jira, and tasks are executed by autonomous agents — not humans. Humans pick up the slack when an agent lacks the tooling, data, or integration to complete the work. Every time that happens, it's a signal for the next automation to build.

The board generates its own roadmap.

---

## Implementation Checkpoint — 2026-05-15

The current repo has moved beyond the original Phase 2/3 scaffold in several areas:

- Jira sync now includes project-key safety, startup hydration, polling, real Blocked workflow support, blocked flag fallback, assignment, comments, ETA, priority, labels, subtasks, story points, estimates, worklogs, issue links, sprint assignment, ranking, components, fix versions, custom fields, remote links, reporter, watchers, metadata discovery, and transition option discovery.
- The voice agent now has structured scrum-master meeting tools for starting meetings, registering participants, recording updates, tracking blockers/risks/action items, choosing the next speaker, summarizing, and ending meetings.
- Public-traffic hardening now includes HttpOnly session auth, authenticated WebSockets, local-only Keychain login bootstrap for development, room/board binding, rate limits, production LiveKit credential checks, prompt-injection defenses, audit logging, and optional SQLite event history.
- Nova Sonic stream handling now avoids duplicate Bedrock `SYSTEM` content after board mutations and keeps the audio input stream alive through quiet-room pauses.
- AWS infrastructure now exists in Terraform/Terragrunt for ECS Fargate app + LiveKit, ALB/NLB, ECR, CloudWatch, Secrets Manager, EFS persistence, Bedrock task-role permissions, S3 remote state, and DynamoDB locking in `us-east-1`.

Remaining plan gaps are tracked in `progress.md`; the largest are live Jira sprint/rank validation, Jira conflict handling, OIDC/Cognito, true multi-room orchestration, AWS deployment validation, and Phase 4 agent execution.

---

## Prove the Product

This plan isn't just "build the product" — it's "prove the product." The wedge is 4 hours of meetings per week. That number gets tracked across phases. If it doesn't move in our own team's data, the product doesn't work and we'll know sooner rather than later.

| Phase | Meeting Hours/Week | What Changed |
|-------|-------------------|--------------|
| Baseline (pre-Phase 1) | 4.0 | Manual standups, status recitation |
| After Phase 1 | 4.0 | No change expected — this is instrumentation |
| After Phase 2 | ~3.5 | Board prep eliminated, less "let me pull up Jira" |
| After Phase 3 | ~3.0 | Voice agent handles board ops, standup is faster |
| After Phase 4 | ~1.5 | Agent summary replaces status recitation, humans only triage |
| After Phase 5 | ~1.5 | No change — deployment doesn't affect meeting time |

Measure weekly. If a phase doesn't move the number, diagnose before proceeding.

---

## Phase 1 — OpenAI Realtime Baseline

**Goal:** Get the upstream `openai-realtime-meeting-assistant` demo running, validate the voice → tool-call → board-update loop, and build a failure inventory that becomes the test suite for everything after.

**Tasks:**

- Clone and run the demo locally (Go 1.24+, Opus, pkg-config)
- Configure `OPENAI_API_KEY` and confirm Realtime peer connects with `gpt-realtime-2`
- Test all six tool calls: `create_ticket`, `move_ticket`, `add_tags`, `update_ticket`, `delete_ticket`, `do_nothing`
- Validate multi-participant audio mixing (join from two browser tabs, confirm mixed audio reaches the model)
- Document echo, VAD sensitivity, and tool-call hallucination behavior
- Fork the repo into `moore-cloud/living-kanban-board`
- **Failure inventory (key deliverable):** A short doc listing every false positive, false negative, ambiguous command, and edge case hit during testing. Examples: "said 'I'm done with that' and the model created a ticket instead of moving one to Done," "background cough triggered a create_ticket." This doc becomes the test suite for Phase 2 and the prompt-engineering target for Phase 3. Without it, Phase 1 is a checkbox exercise that teaches nothing

**Exit Criteria:** A working local demo where voice commands reliably create, move, and update cards. Failure inventory doc delivered with categorized edge cases. Baseline meeting hours measured.

---

## Phase 2 — Jira Sync (Single Workflow)

**Goal:** Wire the board to a single, hand-picked Jira project — ours. Get bidirectional sync working against a real workflow before generalizing.

**Design Principle:** Scope ruthlessly. Real Jira workflows have 6-15 statuses, conditional transitions, required fields, and resolution screens. This is where every Jira integration dies. Pick one workflow, hardcode the mapping, prove it works. The config layer comes in Phase 2.5.

**Tasks:**

- Define the Jira ↔ Kanban mapping for our specific project:
  - Issue key → `card.ID`
  - Map our project's Jira statuses to the four Kanban columns (Backlog / In Progress / Blocked / Done) — this will be a many-to-one mapping
  - Summary → `card.Title`
  - Description → `card.Notes`
  - Labels → `card.Tags`
- Hardcode the Jira transition IDs for our workflow (look up via `GET /rest/api/3/issue/{key}/transitions`)
- Build a Jira REST API client (v3) in Go — authenticate via API token initially
- **Secrets management:** Move Jira API token and OpenAI API key into AWS Secrets Manager (or 1Password CLI for local dev). No credentials in env vars or code from this point forward. Add TLS via mkcert for local dev
- **Inbound sync (Jira → Board):** On agent startup, pull issues from a JQL filter and hydrate the card slice. Use Jira webhooks for ongoing updates (fall back to polling if webhook setup is blocked)
- **Outbound sync (Board → Jira):** When a tool call mutates the board, write the change back:
  - `create_ticket` → create Jira issue
  - `move_ticket` → call the hardcoded transition for our workflow
  - `update_ticket` → update summary/description
  - `add_tags` → append labels
  - `delete_ticket` → transition to cancelled/closed (configurable)
- Conflict handling: last-write-wins for v1, but **log the losing write** with both timestamps and payloads — that log is a dataset for "should we have prompted the user?" and is needed to reconstruct disputes
- **`get_board` tool with freshness contract:**
  - Returns board state with a `timestamp` and `sequence_number`
  - If the board changes during a model's turn (someone else updates Jira, another participant's voice command lands), the orchestrator injects a "board changed since your last read" event so the model knows its data is stale
  - This matters more than it sounds — it's the difference between an agent that handles concurrent edits gracefully and one that overwrites Sarah's update because it didn't know it happened
- Replace the hardcoded `initialKanbanBoardCards` with Jira-sourced data
- Replay the Phase 1 failure inventory as a regression test against the Jira-connected board

**Exit Criteria:** Voice commands update both the Kanban board and our real Jira board. Changes in Jira appear on the board within seconds. Secrets are out of env vars. Losing writes are logged. `get_board` returns timestamped, sequenced state. Phase 1 failure inventory items retested.

---

## Phase 2.5 — Jira Workflow Config Layer

**Goal:** Make the Jira sync work with other teams' workflows without code changes. Know exactly which cases the config layer doesn't cover before someone hits them in production.

**Tasks:**

- Build a configuration schema that maps arbitrary Jira statuses to the four Kanban columns
- Support configurable transition IDs per status change (since Jira transition IDs are project-specific)
- Handle required fields on transitions (some Jira workflows require resolution, comment, or custom fields to transition)
- Handle conditional transitions (some transitions are only available from certain statuses)
- Config file format: YAML or JSON, loaded at startup, validated before the agent connects
- **Validation targets (three workflows, not two):**
  - Our own workflow (already working from Phase 2)
  - A real outside team's workflow — not a colleague's side project, an actual team with their own Jira conventions
  - A deliberately adversarial workflow: one with a required custom field on transition, or a required resolution screen, or a conditional transition that's only available from certain statuses. The point isn't to handle every case; it's to document which cases the config layer doesn't cover
- Deliver a "known limitations" doc: what workflow patterns will break the config layer, so we can tell users up front instead of letting them discover it

**Exit Criteria:** A new Jira project can be onboarded by editing a config file. Three workflows validated. Known limitations documented with specific examples of what won't work.

---

## Phase 3 — Nova Sonic 2 via LiveKit (Parallel Path)

**Goal:** Add AWS Nova Sonic 2 + LiveKit as a second, independent voice agent path. The existing OpenAI Realtime 2 + Pion codebase stays fully intact and functional. A startup flag selects which path runs — they never run simultaneously, and neither path depends on the other.

**Design Principle:** Don't abstract, don't refactor. These are two separate runtime paths that share only the Kanban board state and the Jira sync layer. The OpenAI path uses Pion WebRTC, the hand-rolled audio mixer, and the OpenAI Realtime data channel — exactly as the upstream demo works today. The Nova Sonic path uses LiveKit for WebRTC/SFU/audio and the upstream Nova Sonic 2 MCP integration. Keeping them independent means neither can break the other. This is the most senior decision in the plan — maintain this discipline.

**Tasks:**

- **Provider selection:**
  - Startup flag or environment variable: `VOICE_PROVIDER=openai` (default) or `VOICE_PROVIDER=nova-sonic`
  - When `openai`: existing `main.go` + `audio_mixer.go` + `kanban.go` Realtime peer code runs unchanged. LiveKit is not started, not imported, not involved
  - When `nova-sonic`: LiveKit agent path runs. Pion SFU, audio mixer, and OpenAI Realtime peer code are not started
  - Both paths call into the same Kanban board state and Jira sync layer — that's the only shared surface
- **Preserve the OpenAI Realtime path:**
  - Do not modify `main.go`, `audio_mixer.go`, `opus_encoder.go`, `opus_decoder.go`, or the Pion WebRTC signaling code
  - Do not refactor `kanbanBoardApp` to accommodate a provider abstraction — instead, extract the board state and Jira sync into a shared package that both paths import
  - The OpenAI Realtime peer in `kanban.go` (`JoinConferenceRoom`, `connectRealtimePeer`, `handleRealtimeEvent`) stays exactly as-is
- **Build the Nova Sonic + LiveKit path (new code):**
  - LiveKit Cloud for the demo — per-minute cost on a standup is rounding error compared to OpenAI Realtime + agent inference. Self-host when a customer is paying for it
  - LiveKit Agent joins the room as a server-side participant via LiveKit Agents SDK
  - Nova Sonic 2 connection via the merged upstream LiveKit MCP integration
  - Agent receives mixed room audio from LiveKit, streams to Nova Sonic, handles tool-call responses
  - Board state broadcast to participants via LiveKit data channels
  - The LiveKit path has its own entry point — no code sharing with the Pion signaling path
- **Nova Sonic 2 specifics:**
  - Handle 8-minute session renewal seamlessly — on renewal, agent calls `get_board` tool to pull current state with sequence number (no stale data)
  - Tune session instructions for Nova Sonic's model behavior — replay the Phase 1 failure inventory to calibrate prompt engineering
  - Validate VAD behavior — ensure filler speech triggers `do_nothing`, not false tool calls
- **Testing:**
  - Verify OpenAI Realtime path still works identically after Phase 3 changes (regression test)
  - Test Nova Sonic path end-to-end: voice → tool call → board update → Jira sync
  - A/B compare tool-call accuracy, latency, and ambiguous-command handling between providers
  - Replay Phase 1 failure inventory against both providers and compare results
  - Verify multi-participant audio works cleanly through LiveKit → Nova Sonic

**Exit Criteria:** `VOICE_PROVIDER=openai` runs the original codebase with zero regressions. `VOICE_PROVIDER=nova-sonic` runs the LiveKit + Nova Sonic 2 path. Switching is a config change, not a code change. Session renewal on the Nova Sonic path is invisible to participants. Phase 1 failure inventory tested on both providers.

---

## Phase 4 — Agent-First Task Execution

**Goal:** When a task lands on the board, an agent picks it up and does the work. Humans are the escalation path, not the default.

**Current implementation slice:** Voice can now call `assign_ticket_to_agent` on a Jira-backed card. That creates a durable `agent_run`, stores checkpoints in SQLite, shows the run in the live Meeting Settings drawer and post-meeting intelligence page, uses AWS Bedrock Claude Haiku through the US inference profile for PM classification, routes normal code-review requests to AWS Bedrock Claude Sonnet 4.6 through the US inference profile, reads PR diffs through a short-lived least-privilege GitHub App installation token, writes findings back to the Jira ticket, and optionally posts a PR review comment when `GITHUB_PR_COMMENTS_ENABLED=true`. There is no direct Anthropic API path; Claude usage is through Bedrock IAM only, and runs fail visibly instead of continuing if the Bedrock client is unavailable. Live smoke tests passed against Jira issues `EMAL-22` and `EMAL-23` and GitHub PR `somoore/auto-bot#1`; `EMAL-23` used the cost-balanced Haiku + Sonnet 4.6 model pairing.

**Headline Deliverable:** The 60-second agent standup summary. At meeting start, the voice agent delivers an overnight activity report — completed PRs, blocked items, escalations awaiting human input. Humans only discuss what's flagged. Standup becomes triage, not status recitation.

**Success criterion:** Three engineers on the team prefer the agent's morning summary to the human-run standup, blind-rated. That's a real, measurable, defendable claim — and it's the demo for the landing page.

**Tasks:**

- **Pre-classifier filter:**
  - Before spending even a haiku call, check if the ticket has enough metadata to classify. Tickets with only `{title: "fix it"}` and no labels, no description, no context go straight to `needs-human` without burning a classifier call
  - This is a simple rule-based filter, not ML: minimum title length, at least one label or a description with >N words
- **Classifier cold-start (don't spend agent money before you have data):**
  - For the first N tickets (suggest 50), run the haiku-class classifier but don't dispatch automatically. Instead, post the classifier's proposed agent-type and confidence to the Jira ticket as a comment. A human clicks "looks right" or corrects it
  - This gives labeled data — agent-type, classifier confidence, human-confirmed correct — without spending agent money on bad dispatches
  - Once you have a few dozen labeled rows, you have an actual threshold. Set it empirically, not by guessing
- **Classifier (post cold-start):**
  - AWS Bedrock Haiku-class model classifies work type from ticket metadata (labels, title, description)
  - **Confidence threshold** set from cold-start data: below threshold, skip the guess and route straight to `needs-human`
  - **Cost cap per ticket, not per agent run.** A bad classifier can dispatch the same ticket three times in an hour. Track total spend per issue key and hard-stop at the cap
- **Agent dispatch (above threshold):**
  - `code review` → **Code Review Agent** (Bedrock Sonnet by default, Opus for escalation): read pull request diff through GitHub App read-only access, post findings to Jira, optionally post a PR review comment
  - `bug` / `fix` → **Code Agent** (future): pull repo in sandbox, reproduce, propose diff, open PR only after explicit trust gates
  - `research` / `investigate` → **Research Agent**: deep research, write findings back to ticket notes
  - `security` / `audit` → **Security Agent**: run ffsec toolchain, scan infrastructure, report findings
  - `documentation` / `docs` → **Docs Agent**: generate or update documentation
  - Below confidence threshold or no matching profile → **Escalate to human**
- **Code Agent: proposed_changes mode:**
  - Before auto-opening PRs, Code Agent posts the diff as a Jira comment and waits for human approval
  - This is a trust calibration gate — remove it once the team has confidence in the agent's output quality
  - Saves you from premature trust: a bad PR is worse than no PR because it creates review burden
- **Agent execution environment:**
  - Sandboxed execution per agent run (container-per-task or isolated workspace)
  - Each agent gets scoped credentials: GitHub App installation tokens scoped to the requested repo, Jira write-back through the server-side broker, relevant MCP servers
  - Timeout and cost budget per task — kill runaway agents
  - **Transactional checkpoints:** every agent reports progress at defined checkpoints (e.g., Code Agent: "cloned repo," "created branch," "committed fix," "pushed"). This standardizes partial work before you need it for `take_over`
  - Execution log attached to the Jira ticket as a comment
- **Human escalation with typed tags:**
  - When an agent can't complete, it moves the ticket to Blocked and adds a specific escalation tag:
    - `needs-tooling` — agent lacks an MCP server, connector, or integration to proceed. **This is the automation backlog.** These tags generate the roadmap for what to build next
    - `needs-decision` — agent hit an ambiguous requirement, a tradeoff, or a judgment call that requires human input
    - `needs-human` — general fallback (access issues, confidence too low, etc.)
  - Agent writes a structured escalation note: what was attempted, what's missing, what a human needs to decide
  - Track escalation reasons by type — `needs-tooling` count is a product metric
- **Manual override:**
  - `take_over` tool: human says "I'll take this." Running agent stops at the last transactional checkpoint. Everything committed up to that checkpoint is preserved with a `partial-agent-work` tag. Anything mid-flight (uncommitted edit, in-progress API call) is discarded. The unit of partial work is the checkpoint, not "whatever state the process was in"
  - `retry_with` tool: human says "redo the ICE restart fix but use the staging TURN server." Agent re-executes with the additional constraint injected. Costs against the same ticket's cost cap
  - Without these, the only escape from an agent going sideways is killing the container
- **Agent standup summary:**
  - Voice agent delivers a 60-second summary at meeting start: overnight agent activity, completed PRs/reports, blocked items, escalations awaiting human input
  - Humans only discuss what's flagged — standup is triage, not status recitation
  - This is a deliverable (the agent speaks), not a vibe shift (asking humans to change how they talk)
- **Audit trail:**
  - Log every tool call with the audio segment timestamp and the model's reasoning (if available from the provider). When a user says "I never said that," you need the receipt
  - Log the classifier's confidence score and chosen agent type for every dispatch decision
  - Log every checkpoint for every agent run
- **Metrics:**
  - Agent completion rate by task type
  - Classifier accuracy (track when humans override agent classification during cold-start and after)
  - Average time from ticket creation to agent PR/report
  - Escalation rate by type (`needs-tooling` vs `needs-decision` vs `needs-human`)
  - Cost per agent-completed task vs estimated human cost
  - Cost per ticket (cumulative across retries)
  - **Standup satisfaction:** blind-rated preference for agent summary vs human-run standup

**Exit Criteria:** Tasks created via voice standup are automatically picked up by agents above the confidence threshold (set empirically from cold-start data). Code Agent uses proposed_changes mode pending trust calibration. Agents complete work, update Jira, and escalate with typed tags when they can't proceed. Humans can take over (at checkpoint boundaries) or retry with constraints. Standup opens with an agent-delivered summary. Three engineers prefer it to the old format. `needs-tooling` escalations generate a visible automation backlog. Meeting hours tracked and compared to baseline.

---

## Phase 5 — Auth, Hardening & AWS Deployment

**Goal:** Productionize. Everything before this can demo over ngrok. Deploy when there's something worth hosting.

**Tasks:**

- **Authentication:**
  - Add OAuth 2.0 / OIDC for room access (consider AWS Cognito or a lightweight OIDC provider)
  - Room-level access control — map authenticated users to allowed Jira projects/boards
- **Hardening:**
  - TLS termination (ALB or CloudFront) — real certs replacing the mkcert setup from Phase 2
  - WebSocket upgrade validation with origin checks
  - Rate limiting on the signaling endpoint
  - Input validation on all tool-call arguments before Jira write-back
  - Audit logging — every board mutation logged with timestamp, user, source (voice/jira-webhook), and audio segment reference
- **AWS Deployment:**
  - Containerize the Go application (Docker)
  - Deploy on ECS Fargate (preferred for ops simplicity)
  - ALB with WebSocket support for signaling
  - CloudWatch for logs and metrics
  - Infrastructure as code (CDK or Terraform)
  - Consider TURN server deployment (Coturn on EC2) for NAT traversal in corporate networks — only needed for the OpenAI Realtime path; LiveKit Cloud handles this for the Nova Sonic path
- **Observability:**
  - Structured logging for all Realtime API events, tool calls, Jira sync operations, and agent dispatch decisions
  - Metrics dashboard: tool-call latency, Jira sync lag, WebRTC connection success rate, classifier confidence distribution, agent cost per task/ticket
  - Automation backlog dashboard: `needs-tooling` items ranked by frequency
  - Meeting hours trend line from baseline through current

**Exit Criteria:** Deployed to AWS, accessible over HTTPS, authenticated users only, secrets managed properly, full audit trail, observability in place. Meeting hours reduction validated and visible on a dashboard.

---

## Architecture

```
                    VOICE_PROVIDER=openai              VOICE_PROVIDER=nova-sonic
                    (existing codebase)                (new parallel path)

                ┌─────────────────────┐           ┌──────────────────────────┐
                │   Pion WebRTC SFU   │           │   LiveKit Cloud (SFU)    │
                │   + Audio Mixer     │           │   + Audio Routing        │
                │   + WS Signaling    │           │   + LiveKit Agents SDK   │
                └─────────┬───────────┘           └────────────┬─────────────┘
                          │                                    │
                          ▼                                    ▼
                ┌─────────────────────┐           ┌──────────────────────────┐
                │  OpenAI Realtime 2  │           │  AWS Nova Sonic 2        │
                │  (WebRTC peer +     │           │  (LiveKit MCP plugin,    │
                │   data channel)     │           │   8-min session renewal) │
                └─────────┬───────────┘           └────────────┬─────────────┘
                          │                                    │
                          │         Tool Calls                 │
                          └──────────────┬─────────────────────┘
                                         │
                                         ▼
                              ┌─────────────────────┐
                              │   KANBAN BOARD STATE │ ◄── shared
                              │   + Jira Sync Layer  │ ◄── shared
                              │   + get_board (seq#) │ ◄── shared
                              └──────────┬──────────┘
                                         │
                                   Board Changes
                                   (Jira webhooks)
                                         │
                                         ▼
                              ┌─────────────────────┐
                              │  PRE-CLASSIFIER      │
                              │  (rule-based filter)  │
                              │  enough metadata?     │
                              └──────────┬──────────┘
                                    yes  │  no → needs-human
                                         │
                                         ▼
                              ┌─────────────────────┐
                              │  CLASSIFIER (haiku)  │
                              │  confidence threshold │
                              │  (set from cold-start │
                              │   labeled data)       │
                              └──────────┬──────────┘
                                         │
                          ┌──────────────┼──────────────┐
                          │     above    │    below      │
                          │   threshold  │  threshold    │
                          ▼              │               ▼
               ┌─────────────────┐       │    ┌──────────────────┐
               │   ORCHESTRATOR  │       │    │  needs-human     │
               │  Route by type  │       │    │  (skip the guess)│
               │  Cost cap/ticket│       │    └──────────────────┘
               └──┬───┬───┬─────┘       │
                  │   │   │              │
         ┌────────┘   │   └────────┐     │
         ▼            ▼            ▼     │
     ┌───────┐  ┌────────┐  ┌───────┐   │
     │ Code  │  │Research│  │  Sec  │   │
     │ Agent │  │ Agent  │  │ Agent │   │
     │       │  │        │  │       │   │
     │propose│  └───┬────┘  └───┬───┘   │
     │changes│      │           │       │
     └───┬───┘      │           │       │
         │   checkpoints        │       │
         └──────────┴───────────┘       │
                    │                    │
         ┌──────────┴──────────┐         │
         │  On failure, tag:   │         │
         │  needs-tooling  ◄───┼─── automation backlog
         │  needs-decision     │         │
         │  needs-human        │◄────────┘
         └─────────────────────┘
                    │
               Results + escalations
                 back to Jira

    Voice tools: take_over (at checkpoint), retry_with
    Standup: 60-sec agent summary → human triage only
    Metric: meeting hours/week vs baseline
```

---

## Open Questions

- **Jira workflow mapping:** Phase 2 is hardcoded to our workflow. Phase 2.5 validates three workflows including a deliberately adversarial one. How many validated workflows before we call it general-purpose?
- **Agent identity in Jira:** Do agents comment as a service account, or do we create per-agent Jira users for traceability?
- **LiveKit Cloud → self-hosted:** Cloud for demo, self-host when a customer is paying. What's the trigger for the switch?
- **Multi-board support:** Phase 2 assumes a single Jira board. When do we support multiple boards/projects per deployment?
- **Classifier cold-start duration:** 50 tickets before auto-dispatch? More? Depends on how quickly we get confident labels. Could be weeks for a small team
- **proposed_changes graduation:** When does Code Agent stop posting diffs for approval and start opening PRs directly? Needs a success-rate threshold from the approval data
- **`take_over` mid-checkpoint:** What if an agent is mid-API-call (e.g., halfway through a Jira bulk update) when `take_over` fires? Need a cancellation contract for each agent type
- **`get_board` staleness window:** How long is an acceptable window between a `get_board` read and acting on the data? 5 seconds? 30 seconds? Does the model get an interrupt, or does it re-read on every tool call?
- **Meeting hours measurement:** Self-reported or automated? Calendar integration to track actual meeting duration, or weekly survey? The former is more reliable but harder to set up
