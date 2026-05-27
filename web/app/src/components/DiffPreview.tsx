import { useMemo } from "react"
import type { Card, PendingActionDiff } from "../types/board"

interface Props {
  diff: PendingActionDiff
}

// DiffPreview renders the before/after Card snapshots side-by-side with the
// changed/created/removed sets highlighted. It is intentionally compact so it
// can drop into the DryRunQueue row without overflowing the column width.
export function DiffPreview({ diff }: Props): JSX.Element {
  const beforeIndex = useMemo(() => indexCardsById(diff.before), [diff.before])
  const afterIndex = useMemo(() => indexCardsById(diff.after), [diff.after])
  const allIds = useMemo(() => {
    const set = new Set<string>()
    for (const c of diff.before) set.add(c.id)
    for (const c of diff.after) set.add(c.id)
    return Array.from(set).sort()
  }, [diff.before, diff.after])

  if (diff.error) {
    return (
      <div className="rounded-md border border-magnetar/40 bg-magnetar/10 px-3 py-2 text-xs text-magnetar">
        Diff could not be computed: {diff.error}
      </div>
    )
  }
  if (allIds.length === 0) {
    return (
      <div className="rounded-md border border-edge/40 bg-atmos/40 px-3 py-2 text-xs text-farstar">
        This action would not change any cards.
      </div>
    )
  }

  const created = new Set(diff.created_card_ids ?? [])
  const changed = new Set(diff.changed_card_ids ?? [])
  const removed = new Set(diff.removed_card_ids ?? [])

  return (
    <div className="rounded-md border border-edge/40 bg-void/60 p-2 text-[11px]">
      <Legend created={created.size} changed={changed.size} removed={removed.size} />
      <div className="mt-2 grid grid-cols-1 gap-2 sm:grid-cols-2">
        <Column heading="Before" badge="bg-edge text-twilight">
          {allIds.map((id) => {
            const card = beforeIndex.get(id)
            if (!card) return null
            const tag = removed.has(id) ? "removed" : changed.has(id) ? "changed" : "unchanged"
            return <CardRow key={"b-" + id} card={card} tag={tag} />
          })}
        </Column>
        <Column heading="After" badge="bg-comet/20 text-comet">
          {allIds.map((id) => {
            const card = afterIndex.get(id)
            if (!card) return null
            const tag = created.has(id) ? "created" : changed.has(id) ? "changed" : "unchanged"
            return <CardRow key={"a-" + id} card={card} tag={tag} />
          })}
        </Column>
      </div>
    </div>
  )
}

interface ColumnProps {
  heading: string
  badge: string
  children: React.ReactNode
}

function Column({ heading, badge, children }: ColumnProps): JSX.Element {
  return (
    <div className="rounded border border-edge/40 bg-atmos/40 p-2">
      <div className={"mb-2 inline-block rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-widest " + badge}>
        {heading}
      </div>
      <div className="flex flex-col gap-1">{children}</div>
    </div>
  )
}

type RowTag = "created" | "changed" | "removed" | "unchanged"

function CardRow({ card, tag }: { card: Card; tag: RowTag }): JSX.Element {
  const cls =
    tag === "created"
      ? "border-solar/40 bg-solar/10"
      : tag === "changed"
        ? "border-comet/40 bg-comet/10"
        : tag === "removed"
          ? "border-magnetar/40 bg-magnetar/10"
          : "border-edge/40 bg-void/40"
  return (
    <div className={"flex items-start gap-2 rounded border px-2 py-1 " + cls}>
      <span className="flex-shrink-0 rounded bg-edge px-1 text-[10px] uppercase tracking-widest text-twilight">{card.status}</span>
      <div className="min-w-0 flex-1">
        <div className="truncate text-xs font-medium text-star">{card.title || card.id}</div>
        {card.assignee?.displayName ? (
          <div className="truncate text-[10px] text-farstar">{card.assignee.displayName}</div>
        ) : null}
      </div>
      <span className="flex-shrink-0 text-[10px] uppercase text-farstar">{tag}</span>
    </div>
  )
}

function Legend({ created, changed, removed }: { created: number; changed: number; removed: number }): JSX.Element {
  return (
    <div className="flex flex-wrap items-center gap-3 text-[10px] text-farstar">
      <LegendItem label={`${created} created`} color="bg-solar" />
      <LegendItem label={`${changed} changed`} color="bg-comet" />
      <LegendItem label={`${removed} removed`} color="bg-magnetar" />
    </div>
  )
}

function LegendItem({ label, color }: { label: string; color: string }): JSX.Element {
  return (
    <span className="inline-flex items-center gap-1">
      <span className={"inline-block h-1.5 w-1.5 rounded-full " + color} aria-hidden />
      {label}
    </span>
  )
}

function indexCardsById(cards: Card[]): Map<string, Card> {
  const m = new Map<string, Card>()
  for (const c of cards) m.set(c.id, c)
  return m
}
