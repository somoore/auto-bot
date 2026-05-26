import { useMemo } from "react"
import { useBoardSocket } from "./lib/useBoardSocket"
import { BoardHeader } from "./components/BoardHeader"
import { BoardSubBar } from "./components/BoardSubBar"
import { BoardColumn } from "./components/BoardColumn"
import { EmptyState } from "./components/EmptyState"
import { SignInGate } from "./components/SignInGate"
import {
  CARD_STATUSES,
  type Card,
  type CardStatus,
  type RunQuestion,
} from "./types/board"

function App(): JSX.Element {
  const { state } = useBoardSocket()
  // Hooks run on every render — keep them above any early return.
  const cards = state.board?.cards ?? []
  const cardsByStatus = useMemoBucketByStatus(cards)
  const questionsByCard = useMemoQuestionsByCard(state.openRunQuestions)
  const activeRun = state.lastAgentRun
  const agentActiveColumn = activeRunColumn(activeRun, cards)

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
      />
      <BoardSubBar
        agentActive={isAgentActive(activeRun?.status)}
        agentLabel={activeRun?.agent_profile || activeRun?.specialist}
        cardCount={cards.length}
      />
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
              />
            ))}
          </div>
        )}
        {state.lastError ? (
          <p className="mx-auto mt-4 max-w-[1400px] text-[10px] text-magnetar">{state.lastError}</p>
        ) : null}
      </main>
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

function useMemoQuestionsByCard(questions: RunQuestion[]): Map<string, RunQuestion> {
  return useMemo(() => {
    const map = new Map<string, RunQuestion>()
    for (const q of questions) {
      if (q.status !== "open") continue
      map.set(q.card_id, q)
    }
    return map
  }, [questions])
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

export default App
