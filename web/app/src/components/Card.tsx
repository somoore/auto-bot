import type { Card as CardModel, RunQuestion } from "../types/board"
import { RunQuestionBanner } from "./RunQuestionBanner"

interface Props {
  card: CardModel
  question?: RunQuestion
  // The backend does not yet emit a "moved by nova" signal. The App
  // derives this from `assignee.kind === 'agent'` as a proxy until
  // F1.1/F1.2 surfaces a real checkpoints last-toucher field.
  agentMoved?: boolean
  // onOpen wires the drawer mount in App.tsx. The card surface becomes a
  // button so it's keyboard-reachable.
  onOpen?: (cardId: string) => void
}

export function Card({ card, question, agentMoved, onOpen }: Props): JSX.Element {
  const assignee = card.assignee
  const isAgentAssignee = assignee?.kind === "agent"
  const clickable = Boolean(onOpen)
  return (
    <article
      className={
        clickable
          ? "group relative cursor-pointer rounded-lg border border-edge/80 bg-atmos px-3 py-3 text-left shadow-sm transition hover:border-edge hover:bg-atmos/80 focus-within:border-comet"
          : "group relative rounded-lg border border-edge/80 bg-atmos px-3 py-3 shadow-sm transition hover:border-edge hover:bg-atmos/80"
      }
      data-card-id={card.id}
      onClick={clickable ? (): void => onOpen?.(card.id) : undefined}
      onKeyDown={
        clickable
          ? (e): void => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault()
                onOpen?.(card.id)
              }
            }
          : undefined
      }
      role={clickable ? "button" : undefined}
      tabIndex={clickable ? 0 : undefined}
      aria-label={clickable ? `Open card ${card.id}: ${card.title}` : undefined}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <div className="flex items-center gap-2 text-[10px] uppercase tracking-wider text-farstar">
            <span>{card.issueType?.toUpperCase() || "TASK"}</span>
            <span className="font-mono text-twilight">{card.id}</span>
            {card.priority ? (
              <span className="rounded bg-edge px-1.5 py-0.5 text-[9px] text-twilight">{card.priority}</span>
            ) : null}
          </div>
          <h3 className="line-clamp-2 text-sm font-medium text-star">{card.title}</h3>
        </div>
        <AssigneeBadge assignee={assignee} />
      </div>

      {card.notes ? <p className="mt-2 line-clamp-2 text-xs text-twilight">{card.notes}</p> : null}

      {card.tags && card.tags.length > 0 ? (
        <div className="mt-2 flex flex-wrap gap-1">
          {card.tags.slice(0, 5).map((tag) => (
            <span key={tag} className="rounded-sm border border-edge/60 bg-sky px-1.5 py-0.5 text-[10px] text-twilight">{tag}</span>
          ))}
        </div>
      ) : null}

      {(agentMoved || isAgentAssignee) && !question ? (
        <span className="mt-2 inline-flex items-center gap-1 rounded-full border border-solar/40 bg-solar/10 px-2 py-0.5 text-[10px] font-medium text-solar">
          <span aria-hidden>✦</span>
          moved by nova
        </span>
      ) : null}

      {question ? <RunQuestionBanner question={question} variant="card" /> : null}

      <CardFooter card={card} />
    </article>
  )
}

function AssigneeBadge({ assignee }: { assignee?: CardModel["assignee"] }): JSX.Element | null {
  if (!assignee) return null
  const display = assignee.displayName || assignee.id || "?"
  const initials = display.split(/[\s\-_.@]+/).filter(Boolean).slice(0, 2)
    .map((part) => part[0]?.toUpperCase() ?? "").join("") || "?"
  if (assignee.kind === "agent") {
    return (
      <span title={`Agent · ${display}`} className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-full border border-solar/40 bg-solar/15 text-[10px] font-semibold text-solar">
        {initials}
      </span>
    )
  }
  return (
    <span title={display} className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-full border border-comet/30 bg-comet/15 text-[10px] font-semibold text-comet">
      {initials}
    </span>
  )
}

function CardFooter({ card }: { card: CardModel }): JSX.Element | null {
  const bits: string[] = []
  if (typeof card.storyPoints === "number") bits.push(`${card.storyPoints} pt`)
  if (card.dueDate) bits.push(`Due ${card.dueDate.slice(0, 10)}`)
  if (card.sprint?.name) bits.push(card.sprint.name)
  if (bits.length === 0) return null
  return (
    <div className="mt-3 flex flex-wrap items-center gap-x-2 gap-y-1 text-[10px] text-farstar">
      {bits.map((bit) => <span key={bit}>{bit}</span>)}
    </div>
  )
}
