import type { Card as CardModel, CardStatus, RunQuestion } from "../types/board"
import { Card } from "./Card"

interface Props {
  status: CardStatus
  cards: CardModel[]
  questionsByCard: Map<string, RunQuestion>
  agentActive: boolean
  onOpenCard?: (cardId: string) => void
}

export function BoardColumn({ status, cards, questionsByCard, agentActive, onOpenCard }: Props): JSX.Element {
  return (
    <section className="flex min-h-[60vh] min-w-[280px] flex-1 flex-col rounded-xl border border-edge/60 bg-sky/60">
      <header className="flex items-center justify-between border-b border-edge/60 px-4 py-3">
        <h2 className={
          agentActive
            ? "text-xs font-semibold uppercase tracking-widest text-solar"
            : "text-xs font-semibold uppercase tracking-widest text-twilight"
        }>
          {status}
        </h2>
        <span className="rounded-full bg-edge px-2 py-0.5 text-[10px] font-mono text-twilight">{cards.length}</span>
      </header>
      <div className="flex flex-1 flex-col gap-2 overflow-y-auto px-3 py-3">
        {cards.length === 0 ? (
          <div className="flex flex-1 items-center justify-center text-xs text-farstar">empty</div>
        ) : (
          cards.map((card) => (
            <Card
              key={card.id}
              card={card}
              question={questionsByCard.get(card.id)}
              agentMoved={card.assignee?.kind === "agent"}
              onOpen={onOpenCard}
            />
          ))
        )}
      </div>
    </section>
  )
}
