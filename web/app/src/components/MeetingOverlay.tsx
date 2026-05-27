// MeetingOverlay — D2.1 "Board / Meeting · Observatory Deck" (Paper NN-0).
//
// This is a full-viewport overlay that mounts above the regular board when
// a standup meeting is live. It is wired today from a prop (see App.tsx),
// because the WebSocket "meeting" envelope is not yet shaped to carry the
// full meeting payload (TODO marker in App.tsx). Once
// `BoardState.meeting` is extended, useBoardSocket should expose
// `state.activeMeeting: MeetingState | null` and App should pull from there.
//
// Visual reference: /tmp/paper-refs/D2.1-meeting.jsx (raw hex codes). All
// colors here use the Observatory palette tokens defined in
// web/app/tailwind.config.js (void/sky/atmos/edge/star/twilight/farstar/
// aurora/solar/magnetar/comet).
//
// Layout (top-down): top nav + agent-state bar (header), body row
// (4-column board + 320px chat side panel), floating bottom action bar.

import { useCallback, useEffect, useState, type FormEvent, type ReactNode } from "react"

// --- Types ------------------------------------------------------------------

export type AgentLiveState = "listening" | "speaking" | "tool_calling"

export interface MeetingParticipant {
  initials: string
  name: string
  speaking?: boolean
}

export interface MeetingLastMove {
  description: string
  agoSeconds: number
  undoToken: string
}

export interface MeetingAgent {
  profile: string
  state: AgentLiveState
  currentAction?: string
}

export interface MeetingCardAssignee {
  initials: string
  name: string
  kind?: "human" | "agent"
}

export interface MeetingCard {
  id: string
  issueType?: string
  title: string
  storyPoints?: number
  assignee?: MeetingCardAssignee
  dayLabel?: string
  // Meeting-specific decorations (mutually compatible flags):
  speakingNow?: { speaker: MeetingCardAssignee; via?: "Voice" | "Chat" }
  voiceQuote?: string
  movedByNova?: { agoSeconds: number }
  runProgress?: { current: number; total: number }
  askedYou?: { from: string; agoText: string; prompt?: string }
  faded?: boolean
  // Done-column adornment:
  commitHash?: string
  struckThrough?: boolean
}

export interface MeetingColumn {
  status: "Backlog" | "In Progress" | "Blocked" | "Done"
  count: number
  cards: MeetingCard[]
  rightLabel?: string // e.g. "today" on Done
}

export interface MeetingEvidence {
  // e.g. tool="move_ticket", detail="ABV2-076 → In Progress"
  tool: string
  detail: string
}

export interface MeetingChatMessage {
  id: string
  cardId?: string
  cardLabel?: string // e.g. "ABV2-114 LIVEKIT RETRY"
  sender: { initials: string; name: string; kind?: "human" | "agent" }
  body: string
  at: string // already-formatted timestamp e.g. "12:03"
  evidence?: MeetingEvidence
  // groupHead=true marks the first message of a thread, where we render the
  // card-label header. Consecutive messages in the same thread group should
  // pass groupHead=false (the left accent bar is rendered by the group).
  groupHead?: boolean
}

export interface MeetingState {
  meetingId: string
  startedAt: string
  elapsedMs: number
  agendaCurrent: number
  agendaTotal: number
  agendaDate: string // e.g. "Tuesday, May 26"
  agent: MeetingAgent
  lastMove?: MeetingLastMove
  costUsd: number
  participants: MeetingParticipant[]
  columns: MeetingColumn[]
  messages: MeetingChatMessage[]
  micOn?: boolean
  videoOn?: boolean
  chatMessageCount?: number
}

export interface MeetingOverlayProps {
  meeting: MeetingState
  onMicToggle?: () => void
  onVideoToggle?: () => void
  onConfirmBoard?: () => void
  onLeave?: () => void
  onSendMessage?: (body: string) => void
  onUndoLastMove?: (undoToken: string) => void
}

// --- Root component --------------------------------------------------------

export function MeetingOverlay({
  meeting,
  onMicToggle,
  onVideoToggle,
  onConfirmBoard,
  onLeave,
  onSendMessage,
  onUndoLastMove,
}: MeetingOverlayProps): JSX.Element {
  // Esc shouldn't tear down the meeting — only the explicit Leave button
  // does, and even that goes through a confirm prompt. We trap Esc so the
  // overlay reads as modal.
  useEffect(() => {
    const onKey = (e: KeyboardEvent): void => {
      if (e.key === "Escape") e.stopPropagation()
    }
    window.addEventListener("keydown", onKey, true)
    return (): void => window.removeEventListener("keydown", onKey, true)
  }, [])

  const handleLeave = useCallback((): void => {
    // Browser confirm() is intentionally low-fidelity for now; the brief
    // calls for a confirm prompt and we don't want to land a custom modal
    // in this overlay until product validates the flow.
    if (typeof window !== "undefined") {
      const ok = window.confirm("Leave the standup? The meeting will continue without you.")
      if (!ok) return
    }
    onLeave?.()
  }, [onLeave])

  const elapsedLabel = formatElapsed(meeting.elapsedMs)
  const chatCount = meeting.chatMessageCount ?? meeting.messages.length

  return (
    <div
      className="fixed inset-0 z-50 flex flex-col bg-void/60 backdrop-blur-[1px]"
      role="dialog"
      aria-modal="true"
      aria-label="Standup meeting in progress"
      data-testid="meeting-overlay"
    >
      {/* The inner shell is a solid-bg pane so the 60% void backdrop only
          shows through at the screen edges on ultra-wide viewports. */}
      <div className="flex min-h-full flex-1 flex-col bg-void text-star">
        <MeetingHeaderBar
          meeting={meeting}
          elapsedLabel={elapsedLabel}
          onUndoLastMove={onUndoLastMove}
        />
        <div className="flex flex-1 min-h-0 flex-col lg:flex-row">
          <MeetingBoardPanel meeting={meeting} />
          <MeetingChatPanel
            messages={meeting.messages}
            chatCount={chatCount}
            onSendMessage={onSendMessage}
          />
        </div>
        <MeetingActionBar
          micOn={meeting.micOn ?? true}
          videoOn={meeting.videoOn ?? false}
          elapsedLabel={elapsedLabel}
          onMicToggle={onMicToggle}
          onVideoToggle={onVideoToggle}
          onConfirmBoard={onConfirmBoard}
          onLeave={handleLeave}
        />
      </div>
    </div>
  )
}

// --- Header bars -----------------------------------------------------------

interface HeaderProps {
  meeting: MeetingState
  elapsedLabel: string
  onUndoLastMove?: (undoToken: string) => void
}

function MeetingHeaderBar({ meeting, elapsedLabel, onUndoLastMove }: HeaderProps): JSX.Element {
  return (
    <header className="flex flex-col border-b border-edge bg-sky">
      {/* Top nav row */}
      <div className="flex items-center gap-6 border-b border-edge px-8 py-[18px]">
        <div className="flex items-center gap-2.5">
          <div className="flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-sm bg-aurora">
            <div className="h-2 w-2 rounded-[1px] bg-void" aria-hidden />
          </div>
          <span className="text-[17px] font-semibold leading-[22px] tracking-tight text-star">
            auto-bot
          </span>
        </div>
        <div className="flex grow basis-0 items-center gap-2 border-l border-edge pl-4">
          <span className="text-[13px] leading-4 text-twilight">Scott&apos;s team</span>
          <span className="text-[13px] leading-4 text-farstar">/</span>
          <span className="text-[13px] font-medium leading-4 text-star">agent-first v2</span>
          <span className="ml-2.5 inline-flex items-center gap-1.5 rounded-[11px] bg-aurora/10 px-2 py-[3px] pl-2 pr-2.5">
            <span className="h-1.5 w-1.5 shrink-0 rounded-[3px] bg-aurora" aria-hidden />
            <span className="text-[11px] font-medium uppercase leading-[14px] tracking-wide text-aurora">
              Standup live · {elapsedLabel}
            </span>
          </span>
        </div>
        <div className="flex items-center gap-3.5" aria-label="Participants">
          {meeting.participants.map((p) => (
            <ParticipantAvatar key={p.name} participant={p} />
          ))}
        </div>
      </div>

      {/* Agent state row */}
      <div className="flex flex-wrap items-center gap-4 px-8 py-[14px]">
        <div className="inline-flex items-center gap-2">
          <span className="inline-flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-sm border border-solar/40 bg-solar/15 font-mono text-[11px] font-semibold text-solar">
            {"</>"}
          </span>
          <span className="text-[11px] font-semibold uppercase tracking-widest text-farstar">Agent</span>
          <span className="text-[13px] font-medium text-star">{meeting.agent.profile}</span>
        </div>
        <span className="h-4 w-px shrink-0 bg-edge" aria-hidden />
        <div className="inline-flex items-center gap-2">
          <span className="text-[11px] font-semibold uppercase tracking-widest text-farstar">Now</span>
          <span className="text-[13px] text-star">{agentNowLabel(meeting.agent)}</span>
          {meeting.agent.state === "listening" || meeting.agent.state === "speaking" ? (
            <AudioBars tone="solar" />
          ) : null}
        </div>
        <span className="hidden grow lg:block" />
        {meeting.lastMove ? (
          <>
            <span className="hidden h-4 w-px shrink-0 bg-edge lg:block" aria-hidden />
            <div className="inline-flex items-center gap-2">
              <span className="text-[11px] font-semibold uppercase tracking-widest text-farstar">Last move</span>
              <span className="text-[13px] text-twilight">{meeting.lastMove.description}</span>
              <span className="font-mono text-[11px] text-farstar">{meeting.lastMove.agoSeconds}s</span>
              <button
                type="button"
                onClick={(): void => onUndoLastMove?.(meeting.lastMove!.undoToken)}
                className="rounded-md border border-edge bg-atmos px-2 py-0.5 text-[11px] font-medium text-comet hover:border-comet/60 hover:text-comet"
                data-testid="meeting-undo-last-move"
              >
                Undo
              </button>
            </div>
          </>
        ) : null}
        <span className="hidden h-4 w-px shrink-0 bg-edge lg:block" aria-hidden />
        <div className="inline-flex items-center gap-2">
          <span className="text-[11px] font-semibold uppercase tracking-widest text-farstar">Cost</span>
          <span className="font-mono text-[13px] text-star">${meeting.costUsd.toFixed(2)}</span>
        </div>
      </div>
    </header>
  )
}

function ParticipantAvatar({ participant }: { participant: MeetingParticipant }): JSX.Element {
  return (
    <div className="flex items-center gap-2">
      <div
        className={
          participant.speaking
            ? "flex h-7 w-7 shrink-0 items-center justify-center rounded-full border-2 border-twilight bg-atmos text-[11px] font-semibold text-star"
            : "flex h-7 w-7 shrink-0 items-center justify-center rounded-full border border-edge bg-atmos text-[11px] font-semibold text-star"
        }
        title={participant.name}
      >
        {participant.initials}
      </div>
      {participant.speaking ? <AudioBars tone="star" /> : null}
    </div>
  )
}

function agentNowLabel(agent: MeetingAgent): string {
  const verb =
    agent.state === "listening" ? "Listening"
      : agent.state === "speaking" ? "Speaking"
        : "Calling tools"
  if (agent.currentAction) return `${verb} · ${agent.currentAction}`
  return verb
}

// --- Audio bars (small reusable indicator) ---------------------------------

interface AudioBarsProps { tone: "solar" | "aurora" | "star" | "twilight" }
function AudioBars({ tone }: AudioBarsProps): JSX.Element {
  const a =
    tone === "solar" ? "bg-solar"
      : tone === "aurora" ? "bg-aurora"
        : tone === "star" ? "bg-star"
          : "bg-twilight"
  const b = "bg-twilight"
  // Sizes mirror the Paper artboard pattern: short/tall/mid/tallest/shortest
  return (
    <span className="inline-flex h-3.5 items-end gap-0.5" aria-hidden>
      <span className={`h-1.5 w-0.5 rounded-[1px] ${b}`} />
      <span className={`h-3 w-0.5 rounded-[1px] ${a}`} />
      <span className={`h-2 w-0.5 rounded-[1px] ${b}`} />
      <span className={`h-3.5 w-0.5 rounded-[1px] ${a}`} />
      <span className={`h-1 w-0.5 rounded-[1px] ${b}`} />
    </span>
  )
}

// --- Board panel (left side) -----------------------------------------------

function MeetingBoardPanel({ meeting }: { meeting: MeetingState }): JSX.Element {
  const progress = Array.from({ length: meeting.agendaTotal }, (_, i) => {
    if (i < meeting.agendaCurrent - 1) return "aurora" as const
    if (i === meeting.agendaCurrent - 1) return "solar" as const
    return "edge" as const
  })
  const elapsedLabel = formatElapsed(meeting.elapsedMs)

  return (
    <div className="flex flex-1 min-w-0 flex-col gap-5 overflow-y-auto pl-8 pr-7 pt-[18px] pb-6">
      <div className="flex flex-col gap-3">
        <div className="flex flex-wrap items-baseline gap-3">
          <h1 className="text-[22px] font-semibold leading-7 text-star">{meeting.agendaDate}</h1>
          <span className="text-[11px] font-semibold uppercase tracking-widest text-farstar">
            {meeting.agendaCurrent} of {meeting.agendaTotal} agenda items · {elapsedLabel} spent
          </span>
        </div>
        <div className="flex items-center gap-1.5">
          {progress.map((tone, i) => (
            <span
              key={i}
              className={
                tone === "aurora"
                  ? "h-1 grow rounded-full bg-aurora"
                  : tone === "solar"
                    ? "h-1 grow rounded-full bg-solar"
                    : "h-1 grow rounded-full bg-edge"
              }
            />
          ))}
        </div>
      </div>

      <div className="flex min-h-0 flex-1 gap-4 overflow-x-auto pb-1">
        {meeting.columns.map((col) => (
          <MeetingColumnView key={col.status} column={col} />
        ))}
      </div>
    </div>
  )
}

function MeetingColumnView({ column }: { column: MeetingColumn }): JSX.Element {
  return (
    <section className="flex min-w-[260px] flex-1 flex-col rounded-xl border border-edge/60 bg-sky/60">
      <header className="flex items-center justify-between border-b border-edge/60 px-4 py-3">
        <h2 className="text-xs font-semibold uppercase tracking-widest text-twilight">
          {column.status}
        </h2>
        <div className="flex items-center gap-2">
          {column.rightLabel ? (
            <span className="text-[10px] font-semibold uppercase tracking-widest text-aurora">
              {column.rightLabel}
            </span>
          ) : null}
          <span className="rounded-full bg-edge px-2 py-0.5 text-[10px] font-mono text-twilight">
            {column.count.toString().padStart(2, "0")}
          </span>
        </div>
      </header>
      <div className="flex flex-1 flex-col gap-2 overflow-y-auto px-3 py-3">
        {column.cards.length === 0 ? (
          <div className="flex flex-1 items-center justify-center text-xs text-farstar">empty</div>
        ) : (
          column.cards.map((card) => <MeetingCardView key={card.id} card={card} />)
        )}
      </div>
    </section>
  )
}

// --- Card variants ---------------------------------------------------------

export function MeetingCardView({ card }: { card: MeetingCard }): JSX.Element {
  const baseRing =
    card.speakingNow || card.askedYou
      ? "border-solar/70 shadow-[0_0_0_3px_rgba(255,140,66,0.18)]"
      : "border-edge/80"
  const opacity = card.faded ? "opacity-60" : ""
  return (
    <article
      className={`relative rounded-lg border ${baseRing} bg-atmos px-3 py-3 ${opacity}`}
      data-card-id={card.id}
      data-testid={`meeting-card-${card.id}`}
    >
      {card.speakingNow ? (
        <div className="mb-2 flex items-center gap-2">
          <span className="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-solar/15 text-[10px] font-semibold text-solar">
            {card.speakingNow.speaker.initials}
          </span>
          <span className="text-[11px] font-semibold uppercase tracking-widest text-solar">
            {card.speakingNow.speaker.name} · Speaking now
          </span>
          <AudioBars tone="solar" />
        </div>
      ) : null}

      {card.movedByNova ? (
        <span className="mb-2 inline-flex items-center gap-1 self-start rounded-full border border-solar/40 bg-solar/10 px-2 py-0.5 text-[10px] font-medium text-solar">
          <span aria-hidden>✦</span>
          moved by nova · {card.movedByNova.agoSeconds}s ago
        </span>
      ) : null}

      {card.askedYou ? (
        <div className="mb-2 -mx-3 -mt-3 flex items-center gap-2 rounded-t-lg border-b border-solar/30 bg-solar/10 px-3 py-1.5">
          <span className="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-solar text-[10px] font-bold text-void">?</span>
          <span className="text-[11px] font-medium text-solar">
            {card.askedYou.from} asked you · {card.askedYou.agoText}
          </span>
        </div>
      ) : null}

      <div className="flex items-center gap-2 text-[10px] uppercase tracking-wider text-farstar">
        <span className={card.struckThrough ? "font-mono text-twilight line-through" : "font-mono text-twilight"}>
          {card.id}
        </span>
        {card.issueType ? <span>· {card.issueType}</span> : null}
        {typeof card.storyPoints === "number" ? <span>· {card.storyPoints} pts</span> : null}
      </div>

      <h3 className="mt-1 text-sm font-medium text-star">{card.title}</h3>

      {card.voiceQuote ? (
        <blockquote className="mt-2 rounded-md border-l-2 border-solar bg-void/70 px-3 py-2 text-[12px] italic text-twilight">
          “{card.voiceQuote}”
        </blockquote>
      ) : null}

      {card.askedYou?.prompt ? (
        <p className="mt-2 text-[12px] text-twilight">{card.askedYou.prompt}</p>
      ) : null}

      <div className="mt-3 flex flex-wrap items-center gap-2 text-[10px] text-farstar">
        {card.assignee ? (
          <span className="inline-flex items-center gap-1 rounded-full border border-edge bg-sky px-2 py-0.5 text-[10px] text-twilight">
            <span
              className={
                card.assignee.kind === "agent"
                  ? "inline-flex h-4 w-4 items-center justify-center rounded-full bg-solar/20 text-[9px] font-semibold text-solar"
                  : "inline-flex h-4 w-4 items-center justify-center rounded-full bg-comet/20 text-[9px] font-semibold text-comet"
              }
            >
              {card.assignee.initials}
            </span>
            {card.assignee.name}
          </span>
        ) : null}
        {card.runProgress ? (
          <span className="inline-flex items-center gap-1 rounded-full border border-solar/40 bg-solar/10 px-2 py-0.5 text-[10px] font-mono text-solar">
            RUN · {card.runProgress.current}/{card.runProgress.total}
          </span>
        ) : null}
        {card.commitHash ? (
          <span className="font-mono text-[10px] text-aurora">{card.commitHash}</span>
        ) : null}
        {card.dayLabel ? <span>{card.dayLabel}</span> : null}
      </div>
    </article>
  )
}

// --- Chat side panel -------------------------------------------------------

interface ChatProps {
  messages: MeetingChatMessage[]
  chatCount: number
  onSendMessage?: (body: string) => void
}

function MeetingChatPanel({ messages, chatCount, onSendMessage }: ChatProps): JSX.Element {
  const [draft, setDraft] = useState("")
  const onSubmit = (e: FormEvent<HTMLFormElement>): void => {
    e.preventDefault()
    const body = draft.trim()
    if (!body) return
    onSendMessage?.(body)
    setDraft("")
  }

  // Group consecutive messages by thread (cardId or general). Each group
  // gets one left accent bar + one header.
  const groups = groupMessages(messages)

  return (
    <aside className="flex w-full shrink-0 flex-col border-l border-edge bg-sky lg:w-[320px]">
      <header className="flex items-center justify-between border-b border-edge px-5 py-3.5">
        <div className="flex items-baseline gap-2">
          <h2 className="text-[13px] font-semibold text-star">Chat</h2>
          <span className="font-mono text-[11px] text-twilight">{chatCount}</span>
        </div>
        <span className="inline-flex items-center gap-1 rounded-full border border-edge bg-atmos px-2 py-0.5 text-[10px] font-medium uppercase tracking-widest text-twilight">
          EN auto
        </span>
      </header>
      <div className="flex items-center gap-4 border-b border-edge px-5 py-2 text-[10px] text-farstar">
        <span className="inline-flex items-center gap-1.5">
          <span className="inline-block h-2 w-0.5 rounded-[1px] bg-solar" aria-hidden />
          attached to card
        </span>
        <span className="inline-flex items-center gap-1.5">
          <span className="inline-block h-2 w-0.5 rounded-[1px] bg-farstar" aria-hidden />
          general
        </span>
      </div>
      <div className="flex flex-1 flex-col gap-3.5 overflow-y-auto px-5 pt-2 pb-6">
        {groups.map((g, gi) => (
          <ChatGroup key={`${g.kind}-${g.cardId ?? "general"}-${gi}`} group={g} />
        ))}
      </div>
      <form
        onSubmit={onSubmit}
        className="flex flex-col gap-2 border-t border-edge px-5 py-3"
        data-testid="meeting-chat-form"
      >
        <div className="flex items-center justify-between text-[10px] font-semibold uppercase tracking-widest text-farstar">
          <span>Speak or type</span>
          <span className="font-mono text-[10px] text-twilight">⌘↵</span>
        </div>
        <div className="flex items-center gap-2">
          <input
            type="text"
            value={draft}
            onChange={(e): void => setDraft(e.target.value)}
            placeholder="Ask the team, or @nova for the agent"
            className="flex-1 rounded-md border border-edge bg-atmos px-3 py-2 text-[12px] text-star placeholder:text-twilight focus:border-comet focus:outline-none"
            data-testid="meeting-chat-input"
            onKeyDown={(e): void => {
              if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
                e.preventDefault()
                if (draft.trim()) {
                  onSendMessage?.(draft.trim())
                  setDraft("")
                }
              }
            }}
          />
          <button
            type="submit"
            disabled={!draft.trim()}
            className="rounded-md bg-aurora px-3 py-2 text-[12px] font-semibold text-void disabled:opacity-40"
            data-testid="meeting-chat-send"
          >
            Send
          </button>
        </div>
      </form>
    </aside>
  )
}

interface ChatGroupRecord {
  kind: "card" | "general"
  cardId?: string
  cardLabel?: string
  messages: MeetingChatMessage[]
}

function groupMessages(messages: MeetingChatMessage[]): ChatGroupRecord[] {
  const out: ChatGroupRecord[] = []
  for (const m of messages) {
    const last = out[out.length - 1]
    const kind: "card" | "general" = m.cardId ? "card" : "general"
    if (last && last.kind === kind && last.cardId === m.cardId) {
      last.messages.push(m)
    } else {
      out.push({ kind, cardId: m.cardId, cardLabel: m.cardLabel, messages: [m] })
    }
  }
  return out
}

function ChatGroup({ group }: { group: ChatGroupRecord }): JSX.Element {
  const accent = group.kind === "card" ? "bg-solar" : "bg-farstar"
  const headerTone =
    group.kind === "card" ? "font-mono text-[10px] uppercase tracking-widest text-solar"
      : "font-mono text-[10px] uppercase tracking-widest text-farstar"
  const first = group.messages[0]
  const headerLabel =
    group.kind === "card" ? (group.cardLabel ?? group.cardId ?? "CARD")
      : "GENERAL"
  return (
    <section className="flex gap-3">
      <span className={`mt-0.5 w-0.5 shrink-0 rounded-[1px] ${accent}`} aria-hidden />
      <div className="flex min-w-0 flex-1 flex-col gap-1.5">
        <div className="flex items-baseline justify-between gap-2">
          <span className={headerTone}>{headerLabel}</span>
          <span className="font-mono text-[10px] text-twilight">{first?.at}</span>
        </div>
        {group.messages.map((m) => (
          <ChatMessageRow key={m.id} message={m} />
        ))}
      </div>
    </section>
  )
}

function ChatMessageRow({ message }: { message: MeetingChatMessage }): JSX.Element {
  return (
    <div className="flex flex-col gap-1">
      <div className="flex items-start gap-2">
        <span
          className={
            message.sender.kind === "agent"
              ? "inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-solar/20 text-[10px] font-semibold text-solar"
              : "inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-comet/20 text-[10px] font-semibold text-comet"
          }
          title={message.sender.name}
        >
          {message.sender.initials}
        </span>
        <p className="text-[12px] leading-snug text-star">{message.body}</p>
      </div>
      {message.evidence ? <EvidenceCard evidence={message.evidence} /> : null}
    </div>
  )
}

function EvidenceCard({ evidence }: { evidence: MeetingEvidence }): JSX.Element {
  return (
    <div className="ml-7 mt-1 inline-flex items-center gap-2 rounded-md border border-solar/30 bg-solar/10 px-2 py-1.5">
      <span className="font-mono text-[10px] font-semibold text-solar">{"</>"}</span>
      <span className="font-mono text-[11px] text-star">
        <span className="text-solar">{evidence.tool}</span> {evidence.detail}
      </span>
    </div>
  )
}

// --- Floating action bar ---------------------------------------------------

interface ActionBarProps {
  micOn: boolean
  videoOn: boolean
  elapsedLabel: string
  onMicToggle?: () => void
  onVideoToggle?: () => void
  onConfirmBoard?: () => void
  onLeave?: () => void
}

function MeetingActionBar({
  micOn,
  videoOn,
  elapsedLabel,
  onMicToggle,
  onVideoToggle,
  onConfirmBoard,
  onLeave,
}: ActionBarProps): JSX.Element {
  return (
    <div className="flex justify-center px-6 pt-[18px] pb-6">
      <div className="flex items-center gap-3 rounded-[28px] border border-edge bg-atmos px-4 py-2 shadow-[0_12px_28px_rgba(0,0,0,0.5)]">
        <ActionButton
          testId="meeting-action-mic"
          onClick={onMicToggle}
          tone={micOn ? "aurora" : "neutral"}
          icon={<MicIcon on={micOn} />}
          label={micOn ? "Mic on" : "Mic off"}
        />
        <ActionButton
          testId="meeting-action-video"
          onClick={onVideoToggle}
          tone={videoOn ? "neutral" : "muted"}
          icon={<VideoIcon on={videoOn} />}
          label={videoOn ? "Video on" : "Video off"}
        />
        <span className="h-6 w-px shrink-0 bg-edge" aria-hidden />
        <div className="flex items-center gap-2 px-2">
          <span className="font-mono text-[13px] text-star">{elapsedLabel}</span>
          <AudioBars tone="aurora" />
        </div>
        <button
          type="button"
          onClick={onConfirmBoard}
          data-testid="meeting-action-confirm"
          className="rounded-full bg-aurora px-4 py-1.5 text-[12px] font-semibold text-void hover:brightness-95"
        >
          Confirm board
        </button>
        <button
          type="button"
          onClick={onLeave}
          data-testid="meeting-action-leave"
          className="rounded-full px-3 py-1.5 text-[12px] font-semibold text-magnetar hover:bg-magnetar/10"
        >
          Leave
        </button>
      </div>
    </div>
  )
}

interface ActionButtonProps {
  testId: string
  onClick?: () => void
  tone: "aurora" | "neutral" | "muted"
  icon: ReactNode
  label: string
}

function ActionButton({ testId, onClick, tone, icon, label }: ActionButtonProps): JSX.Element {
  const cls =
    tone === "aurora"
      ? "bg-aurora/25 text-aurora hover:bg-aurora/35"
      : tone === "muted"
        ? "bg-atmos text-farstar hover:bg-edge/60"
        : "bg-atmos text-twilight hover:bg-edge/60"
  return (
    <button
      type="button"
      onClick={onClick}
      data-testid={testId}
      className={`inline-flex items-center gap-2 rounded-full px-3 py-1.5 text-[12px] font-medium ${cls}`}
    >
      <span className="inline-flex h-5 w-5 items-center justify-center" aria-hidden>
        {icon}
      </span>
      <span>{label}</span>
    </button>
  )
}

function MicIcon({ on }: { on: boolean }): JSX.Element {
  // Pure CSS / SVG so we don't pull a new dep.
  return (
    <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden>
      <rect x="6" y="2" width="4" height="8" rx="2" fill="currentColor" />
      <path
        d="M4 8a4 4 0 0 0 8 0M8 12v2"
        stroke="currentColor"
        strokeWidth="1.4"
        fill="none"
        strokeLinecap="round"
      />
      {on ? null : (
        <line x1="3" y1="13" x2="13" y2="3" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
      )}
    </svg>
  )
}

function VideoIcon({ on }: { on: boolean }): JSX.Element {
  return (
    <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden>
      <rect x="1.5" y="4" width="9" height="8" rx="1.5" stroke="currentColor" strokeWidth="1.4" fill="none" />
      <path d="M10.5 7l3.5-1.8v5.6L10.5 9z" stroke="currentColor" strokeWidth="1.4" fill="none" strokeLinejoin="round" />
      {on ? null : (
        <line x1="2" y1="14" x2="14" y2="2" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
      )}
    </svg>
  )
}

// --- Helpers ---------------------------------------------------------------

export function formatElapsed(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000))
  const mm = Math.floor(total / 60)
  const ss = total % 60
  return `${mm.toString().padStart(2, "0")}:${ss.toString().padStart(2, "0")}`
}

// --- Sample (dev wiring) ---------------------------------------------------

// SAMPLE_MEETING is used by App.tsx when the URL has ?meeting=active. It is
// intentionally exported (not behind an env flag) so a Vitest unit test can
// snapshot the rendered overlay without crafting fixtures from scratch.
export const SAMPLE_MEETING: MeetingState = {
  meetingId: "sample-meeting-1",
  startedAt: "2026-05-26T19:00:00Z",
  elapsedMs: 4 * 60 * 1000 + 23 * 1000,
  agendaCurrent: 3,
  agendaTotal: 5,
  agendaDate: "Tuesday, May 26",
  costUsd: 0.18,
  agent: {
    profile: "nova",
    state: "listening",
    currentAction: "Scott is updating ABV2-114",
  },
  lastMove: {
    description: "moved ABV2-076 → In Progress",
    agoSeconds: 14,
    undoToken: "undo-abv2-076-inprogress",
  },
  participants: [
    { initials: "SM", name: "Scott Moore", speaking: true },
    { initials: "AK", name: "Aki" },
    { initials: "JR", name: "Jordan" },
  ],
  columns: [
    {
      status: "Backlog",
      count: 3,
      cards: [
        {
          id: "ABV2-114",
          issueType: "Voice",
          title: "LiveKit retry on transient hang-up",
          storyPoints: 3,
          assignee: { initials: "AK", name: "Aki", kind: "human" },
          speakingNow: { speaker: { initials: "SM", name: "Scott" } },
          voiceQuote: "let's start on the LiveKit retry one — Aki and I scoped it yesterday",
          dayLabel: "Today",
        },
        {
          id: "ABV2-121",
          issueType: "Task",
          title: "Document the meeting overlay tokens",
          storyPoints: 2,
          assignee: { initials: "JR", name: "Jordan", kind: "human" },
          faded: true,
        },
        {
          id: "ABV2-122",
          issueType: "Task",
          title: "Hook board sub-bar to tenant pause state",
          storyPoints: 1,
          assignee: { initials: "SM", name: "Scott", kind: "human" },
          faded: true,
        },
      ],
    },
    {
      status: "In Progress",
      count: 2,
      cards: [
        {
          id: "ABV2-076",
          issueType: "Bug",
          title: "Linear webhook backfill misses retries",
          storyPoints: 5,
          assignee: { initials: "NV", name: "nova", kind: "agent" },
          movedByNova: { agoSeconds: 14 },
        },
        {
          id: "ABV2-091",
          issueType: "Task",
          title: "Sweep agent dashboards for kpi drift",
          storyPoints: 2,
          assignee: { initials: "NV", name: "nova", kind: "agent" },
          runProgress: { current: 3, total: 7 },
          faded: true,
        },
      ],
    },
    {
      status: "Blocked",
      count: 1,
      cards: [
        {
          id: "ABV2-088",
          issueType: "Spike",
          title: "Per-tenant DB shape for replay events",
          storyPoints: 3,
          assignee: { initials: "SW", name: "swe-1", kind: "agent" },
          askedYou: {
            from: "swe-1",
            agoText: "3m",
            prompt: "Should replay events live on the same schema as audit, or a sidecar?",
          },
        },
      ],
    },
    {
      status: "Done",
      count: 4,
      rightLabel: "today",
      cards: [
        {
          id: "ABV2-064",
          issueType: "Task",
          title: "Switch Nova Sonic output to streaming",
          assignee: { initials: "NV", name: "nova", kind: "agent" },
          commitHash: "a1f93c2",
          struckThrough: true,
          faded: true,
        },
        {
          id: "ABV2-070",
          issueType: "Bug",
          title: "Voice host auth on cold reconnects",
          assignee: { initials: "SM", name: "Scott", kind: "human" },
          commitHash: "5d8e1b7",
          struckThrough: true,
          faded: true,
        },
      ],
    },
  ],
  messages: [
    {
      id: "m1",
      cardId: "ABV2-114",
      cardLabel: "ABV2-114 LIVEKIT RETRY",
      sender: { initials: "SM", name: "Scott", kind: "human" },
      body: "Calling this one first — I think the retry interval is the smallest fix.",
      at: "12:03",
    },
    {
      id: "m2",
      cardId: "ABV2-114",
      cardLabel: "ABV2-114 LIVEKIT RETRY",
      sender: { initials: "AK", name: "Aki", kind: "human" },
      body: "Agreed. I'll pair after standup if you want.",
      at: "12:03",
    },
    {
      id: "m3",
      sender: { initials: "JR", name: "Jordan", kind: "human" },
      body: "Heads-up: I'm out tomorrow, ping me on Slack for any blockers.",
      at: "12:02",
    },
    {
      id: "m4",
      cardId: "ABV2-076",
      cardLabel: "ABV2-076 LINEAR WEBHOOK",
      sender: { initials: "SM", name: "Scott", kind: "human" },
      body: "Moving the webhook backfill to In Progress — nova will start poking at it.",
      at: "12:01",
      evidence: { tool: "move_ticket", detail: "ABV2-076 → In Progress" },
    },
    {
      id: "m5",
      cardId: "ABV2-088",
      cardLabel: "ABV2-088 PER-TENANT DB",
      sender: { initials: "NV", name: "nova", kind: "agent" },
      body: "I asked the spec question on ABV2-088 — see the card.",
      at: "11:58",
      evidence: { tool: "ask_question", detail: "ABV2-088 sidecar vs shared schema" },
    },
  ],
  micOn: true,
  videoOn: false,
  chatMessageCount: 18,
}
