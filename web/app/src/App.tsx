import { useCallback, useEffect, useMemo, useState } from "react"
import { useBoardSocket } from "./lib/useBoardSocket"
import { BoardHeader } from "./components/BoardHeader"
import { BoardSubBar } from "./components/BoardSubBar"
import { BoardColumn } from "./components/BoardColumn"
import { CardDrawer } from "./components/CardDrawer"
import { DryRunQueue } from "./components/DryRunQueue"
import { AgendaOverlay, type Agenda } from "./components/AgendaOverlay"
import { EmptyState } from "./components/EmptyState"
import { SignInGate } from "./components/SignInGate"
import {
  PostMeetingSummary,
  type MeetingReport,
} from "./components/PostMeetingSummary"
import {
  MeetingOverlay,
  SAMPLE_MEETING,
  type MeetingState,
} from "./components/MeetingOverlay"
import {
  CARD_STATUSES,
  type AgentRunView,
  type Card,
  type CardStatus,
} from "./types/board"

const URL_PARAM_CARD = "card"
const URL_PARAM_AGENDA = "agenda"
const URL_PARAM_MEETING = "meeting"
// ?meeting=active mounts the live meeting overlay (D2.1). Any other
// non-empty value is treated as a post-meeting summary id (D2.6).
const MEETING_PARAM_ACTIVE = "active"

// TODO(standup): once GET /internal/standup/agenda lands (internal/standup
// Sprint 4) this should fetch live data — yesterday's runs, open blockers,
// reviews awaiting host, and a suggested speaker order. The shape mirrors
// internal/standup.Agenda so the swap is mechanical.
const SAMPLE_AGENDA: Agenda = {
  preparedAt: "2m ago",
  scheduledFor: "Tuesday, May 26 · 9:00 AM",
  estimate: "est. 8 minutes",
  participantSummary: "3 participants joining",
  highlights: [
    { kind: "shipped", title: "Tenant threading through auth + board + store", attribution: "SM · 14h" },
    { kind: "shipped", title: "Actor discriminated type — agents as first-class assignees", attribution: "swe-3 · 11h" },
    { kind: "run_done", title: "JiraSyncer projection refactor · 7/7 steps · evidence attached", attribution: "swe-1 · 2h" },
  ],
  blockers: [
    {
      cardId: "ABV2-088",
      question: "Per-tenant DB file vs shared with tenant_id partitioning?",
      ownerLabel: "swe-1 needs an answer · 3h left",
    },
  ],
  reviews: [
    {
      cardId: "ABV2-093",
      title: "JiraSyncer Projection refactor — replay test green",
      prNumber: "PR #142",
      reviewLabel: "ready for your review",
    },
  ],
  speakerOrder: [
    { participant: { id: "scott", name: "Scott", initials: "SM" }, action: "— answer ABV2-088, review ABV2-093", estimate: "~3 min" },
    { participant: { id: "aki", name: "Aki", initials: "AK" }, action: "— Linear webhook handoff, LiveKit retry kickoff", estimate: "~2 min" },
    { participant: { id: "jordan", name: "Jordan", initials: "JR" }, action: "— /readyz migration plan", estimate: "~2 min" },
  ],
}

// TODO(meeting): replace with fetch of /meetings?meeting_id=<id>
// (cmd/server/meeting_report_handlers.go) and map meetingIntelligenceReport
// into MeetingReport. Sample exists so the route is exercisable until the
// post-meeting WS event lands.
function sampleMeetingReport(meetingId: string): MeetingReport {
  return {
    meetingId,
    title: "Tuesday standup",
    endedAtLabel: "Standup ended · 9:08 AM",
    metadataLabel: "May 26 · 8m 14s · 3 participants + nova",
    breadcrumb: "agent-first v2 / Tuesday standup · summary",
    cardsMoved: 7,
    cardsMovedByNova: 3,
    cardsMovedByTeam: 4,
    runsKickedOff: 2,
    runsEtaMinutes: 24,
    questionsResolved: 1,
    questionAnswer: "per-tenant DB · answered by Scott",
    cost: "$0.43",
    timeSaved: "est. 1h 50m saved",
    decisions: [
      { id: "d1", title: "Ship per-tenant DB behind a feature flag", rationale: "Scott confirmed the migration path; nova will draft the flag plan.", timestamp: "9:04 AM" },
      { id: "d2", title: "Defer the SSO refactor to next sprint", rationale: "Scope risk too high to land alongside the DB cutover.", timestamp: "9:06 AM" },
    ],
    followUps: [
      { id: "f1", title: "Draft DB migration runbook", jiraId: "ABV2-218", assignee: "Daria" },
      { id: "f2", title: "Schedule SSO scoping session", jiraId: "ABV2-219", assignee: "Scott Moore" },
      { id: "f3", title: "Capture rollback criteria", jiraId: "ABV2-220", assignee: "Priya" },
    ],
    agentsWorking: [
      { id: "swe-1", name: "swe-1", cardId: "ABV2-218", stepsDone: 2, stepsTotal: 6, etaMinutes: 12, cost: "$0.12" },
      { id: "swe-3", name: "swe-3", cardId: "ABV2-221", stepsDone: 1, stepsTotal: 4, etaMinutes: 18, cost: "$0.08" },
    ],
    agentsFootnote: "You'll be notified when either needs your input or completes.",
    syncTargets: [
      { id: "s1", label: "Jira", detail: "7 cards updated", timestamp: "2s ago" },
      { id: "s2", label: "Slack recap", detail: "#standup-daily posted", timestamp: "just now" },
    ],
  }
}

function App(): JSX.Element {
  const { state, dispatch } = useBoardSocket()
  // Hooks run on every render — keep them above any early return.
  const cards = state.board?.cards ?? []
  const cardsByStatus = useMemoBucketByStatus(cards)
  const questionsByCard = state.openRunQuestionsByCard
  const runsByCard = state.agentRunsByCardId
  const activeRun = state.lastAgentRun
  const agentActiveColumn = activeRunColumn(activeRun, cards)

  // selectedCardId drives the CardDrawer mount. It is initialized from
  // ?card=... so the drawer state is shareable, and kept in sync with the
  // URL on open/close. popstate (browser back) closes the drawer.
  const [selectedCardId, setSelectedCardId] = useState<string | null>(() =>
    readCardParam(),
  )
  const [agendaOpen, setAgendaOpen] = useState<boolean>(() => readAgendaParam())
  const [meetingId, setMeetingId] = useState<string | null>(() => {
    const v = readMeetingParam()
    // Live-meeting marker is handled by activeMeeting below; only treat
    // other non-empty values as a post-meeting summary id.
    return v && v !== MEETING_PARAM_ACTIVE ? v : null
  })

  const openAgenda = useCallback((): void => {
    setAgendaOpen(true)
    pushAgendaParam()
  }, [])
  const closeAgenda = useCallback((): void => {
    setAgendaOpen(false)
    clearAgendaParam()
  }, [])
  const startAgenda = useCallback((): void => {
    // TODO(standup): wire to the standup meeting kickoff (LiveKit room +
    // scrum-master agent). For F-D2.5 the CTA just dismisses the overlay.
    setAgendaOpen(false)
    clearAgendaParam()
  }, [])
  const closeMeeting = useCallback((): void => {
    setMeetingId(null)
    clearMeetingParam()
  }, [])

  // activeMeeting drives the MeetingOverlay mount when ?meeting=active.
  // Any other ?meeting=<id> value mounts the PostMeetingSummary (handled
  // by meetingId state above). Once useBoardSocket carries a real
  // MeetingState, this derivation should swap to state.board.meeting.
  // TODO(meeting): plumb real meeting payload from useBoardSocket.
  const [activeMeeting, setActiveMeeting] = useState<MeetingState | null>(() =>
    readMeetingParam() === MEETING_PARAM_ACTIVE ? SAMPLE_MEETING : null,
  )

  const openCard = useCallback((cardId: string): void => {
    setSelectedCardId(cardId)
    pushCardParam(cardId)
  }, [])

  const closeCard = useCallback((): void => {
    setSelectedCardId(null)
    clearCardParam()
  }, [])

  useEffect(() => {
    const onPop = (): void => {
      setSelectedCardId(readCardParam())
      setAgendaOpen(readAgendaParam())
      const m = readMeetingParam()
      setActiveMeeting(m === MEETING_PARAM_ACTIVE ? SAMPLE_MEETING : null)
      setMeetingId(m && m !== MEETING_PARAM_ACTIVE ? m : null)
    }
    window.addEventListener("popstate", onPop)
    return (): void => window.removeEventListener("popstate", onPop)
  }, [])

  // If the selected card disappears from the board (server cleanup), close
  // the drawer instead of rendering against a stale snapshot.
  const selectedCard = useMemo<Card | undefined>(() => {
    if (!selectedCardId) return undefined
    return cards.find((c) => c.id === selectedCardId)
  }, [cards, selectedCardId])
  useEffect(() => {
    if (selectedCardId && cards.length > 0 && !selectedCard) {
      closeCard()
    }
  }, [cards.length, closeCard, selectedCard, selectedCardId])

  // ?meeting=<id> mounts the post-meeting summary as a full-page view ahead
  // of the loading/auth gates so a deep-linked summary is rendered without
  // waiting on the board WebSocket handshake.
  if (meetingId) {
    return (
      <PostMeetingSummary
        report={sampleMeetingReport(meetingId)}
        onBackToBoard={closeMeeting}
      />
    )
  }

  if (state.session.loading) {
    return (
      <div className="flex min-h-full items-center justify-center text-twilight">
        <div className="inline-flex items-center gap-3 rounded-full border border-edge bg-atmos px-4 py-2 text-sm">
          <span className="h-2 w-2 animate-pulse rounded-full bg-comet" aria-hidden />
          Connecting to Observatory…
        </div>
      </div>
    )
  }

  if (!state.session.ok) {
    return <SignInGate error={state.session.error} />
  }

  return (
    <div className="flex min-h-full flex-col">
      <BoardHeader
        status={state.status}
        session={state.session}
        reconnectAttempt={state.reconnectAttempt}
        onStartStandup={openAgenda}
      />
      <BoardSubBar
        agentActive={isAgentActive(activeRun?.status)}
        agentLabel={activeRun?.agent_profile || activeRun?.specialist}
        cardCount={cards.length}
        tenantSettings={state.tenantSettings}
      />
      <DryRunQueue pending={state.pendingActions} />
      <main className="flex-1 overflow-x-auto px-4 py-5 sm:px-6">
        {cards.length === 0 ? (
          <>
            <ColumnShell />
            <EmptyState />
          </>
        ) : (
          <div className="mx-auto flex max-w-[1400px] gap-4">
            {CARD_STATUSES.map((status) => (
              <BoardColumn
                key={status}
                status={status}
                cards={cardsByStatus.get(status) ?? []}
                questionsByCard={questionsByCard}
                agentActive={agentActiveColumn === status}
                onOpenCard={openCard}
              />
            ))}
          </div>
        )}
        {state.lastError ? (
          <p className="mx-auto mt-4 max-w-[1400px] text-[10px] text-magnetar">{state.lastError}</p>
        ) : null}
      </main>
      {selectedCard ? (
        <CardDrawer
          card={selectedCard}
          question={questionsByCard.get(selectedCard.id)}
          run={runsByCard.get(selectedCard.id) ?? activeRunForCard(activeRun, selectedCard.id)}
          dispatch={dispatch}
          currentUserId={state.session.participantIdentity}
          onClose={closeCard}
        />
      ) : null}
      {agendaOpen ? (
        <AgendaOverlay
          agenda={SAMPLE_AGENDA}
          onStart={startAgenda}
          onSkip={closeAgenda}
          onClose={closeAgenda}
        />
      ) : null}
      {activeMeeting ? (
        <MeetingOverlay
          meeting={activeMeeting}
          onLeave={(): void => {
            setActiveMeeting(null)
            clearMeetingParam()
          }}
          onMicToggle={(): void => undefined}
          onVideoToggle={(): void => undefined}
          onConfirmBoard={(): void => undefined}
          onSendMessage={(): void => undefined}
          onUndoLastMove={(): void => undefined}
        />
      ) : null}
    </div>
  )
}

function ColumnShell(): JSX.Element {
  return (
    <div className="mx-auto flex max-w-[1400px] gap-4 opacity-50">
      {CARD_STATUSES.map((status) => (
        <section key={status} className="flex min-h-[40vh] min-w-[280px] flex-1 flex-col rounded-xl border border-edge/40 bg-sky/40">
          <header className="border-b border-edge/40 px-4 py-3">
            <h2 className="text-xs font-semibold uppercase tracking-widest text-farstar">{status}</h2>
          </header>
          <div className="flex-1" />
        </section>
      ))}
    </div>
  )
}

function useMemoBucketByStatus(cards: Card[]): Map<CardStatus, Card[]> {
  return useMemo(() => {
    const buckets = new Map<CardStatus, Card[]>()
    for (const status of CARD_STATUSES) buckets.set(status, [])
    for (const card of cards) {
      const bucket = buckets.get(card.status)
      if (bucket) bucket.push(card)
      else buckets.get("Backlog")?.push(card)
    }
    return buckets
  }, [cards])
}

function isAgentActive(status?: string): boolean {
  if (!status) return false
  switch (status) {
    case "queued":
    case "classifying":
    case "fetching_context":
    case "reviewing":
    case "publishing":
    case "retrying":
    case "waiting_on_human":
      return true
    default:
      return false
  }
}

function activeRunColumn(run: { card_id?: string } | undefined, cards: Card[]): CardStatus | undefined {
  if (!run?.card_id) return undefined
  return cards.find((c) => c.id === run.card_id)?.status
}

function activeRunForCard(
  run: AgentRunView | undefined,
  cardId: string,
): AgentRunView | undefined {
  if (!run || run.card_id !== cardId) return undefined
  return run
}

function readCardParam(): string | null {
  if (typeof window === "undefined") return null
  const params = new URLSearchParams(window.location.search)
  const v = params.get(URL_PARAM_CARD)
  return v && v.length > 0 ? v : null
}

function pushCardParam(cardId: string): void {
  if (typeof window === "undefined") return
  const params = new URLSearchParams(window.location.search)
  params.set(URL_PARAM_CARD, cardId)
  const next = `${window.location.pathname}?${params.toString()}`
  window.history.pushState({ cardId }, "", next)
}

function readMeetingParam(): string | null {
  if (typeof window === "undefined") return null
  const params = new URLSearchParams(window.location.search)
  const v = params.get(URL_PARAM_MEETING)
  return v && v.length > 0 ? v : null
}

function clearMeetingParam(): void {
  if (typeof window === "undefined") return
  const params = new URLSearchParams(window.location.search)
  if (!params.has(URL_PARAM_MEETING)) return
  params.delete(URL_PARAM_MEETING)
  const query = params.toString()
  const next = query ? `${window.location.pathname}?${query}` : window.location.pathname
  window.history.pushState({}, "", next)
}

function clearCardParam(): void {
  if (typeof window === "undefined") return
  const params = new URLSearchParams(window.location.search)
  if (!params.has(URL_PARAM_CARD)) return
  params.delete(URL_PARAM_CARD)
  const query = params.toString()
  const next = query ? `${window.location.pathname}?${query}` : window.location.pathname
  window.history.pushState({}, "", next)
}

function readAgendaParam(): boolean {
  if (typeof window === "undefined") return false
  const params = new URLSearchParams(window.location.search)
  return params.get(URL_PARAM_AGENDA) === "open"
}

function pushAgendaParam(): void {
  if (typeof window === "undefined") return
  const params = new URLSearchParams(window.location.search)
  if (params.get(URL_PARAM_AGENDA) === "open") return
  params.set(URL_PARAM_AGENDA, "open")
  const next = `${window.location.pathname}?${params.toString()}`
  window.history.pushState({ agenda: "open" }, "", next)
}

function clearAgendaParam(): void {
  if (typeof window === "undefined") return
  const params = new URLSearchParams(window.location.search)
  if (!params.has(URL_PARAM_AGENDA)) return
  params.delete(URL_PARAM_AGENDA)
  const query = params.toString()
  const next = query ? `${window.location.pathname}?${query}` : window.location.pathname
  window.history.pushState({}, "", next)
}

export default App
