import { useMemo, useState } from "react"
import type { Card as CardModel, Comment } from "../types/board"

interface Props {
  card: CardModel
}

interface ThreadEntry {
  id: string
  body: string
  author: string
  createdAt?: string
  kind: "human" | "agent" | "system"
}

// CardThreadTab renders a mixed thread of human + agent messages with a
// composer at the bottom. The real backend endpoint
// (GET /api/cards/{id}/thread) lands in F1.2. Until then the thread is the
// card's existing `comments` slice plus a stub agent placeholder so the
// dividers + agent styling are exercisable.
export function CardThreadTab({ card }: Props): JSX.Element {
  const entries = useMemo<ThreadEntry[]>(() => mapEntries(card.comments), [card.comments])
  const grouped = useMemo(() => groupByDay(entries), [entries])
  const [draft, setDraft] = useState("")

  return (
    <section
      id="drawer-panel-thread"
      role="tabpanel"
      className="flex h-full flex-col"
    >
      <div className="flex-1 space-y-4 overflow-y-auto px-4 py-4">
        {grouped.length === 0 ? (
          <p className="text-sm text-twilight">
            No messages yet. Comments land in F1.2 — say hi below.
          </p>
        ) : (
          grouped.map((group) => (
            <div key={group.label}>
              <DateDivider label={group.label} />
              <ul className="mt-2 space-y-2">
                {group.entries.map((e) => (
                  <li key={e.id}>
                    <Message entry={e} />
                  </li>
                ))}
              </ul>
            </div>
          ))
        )}
      </div>
      <form
        onSubmit={(e): void => {
          e.preventDefault()
          // Send wiring lands in F1.2 (card.comment dispatch with thread
          // semantics). For this slice the composer just clears.
          setDraft("")
        }}
        className="border-t border-edge bg-sky/95 px-3 py-3"
      >
        <label htmlFor="thread-composer" className="sr-only">
          New message
        </label>
        <div className="flex items-end gap-2">
          <textarea
            id="thread-composer"
            value={draft}
            onChange={(e): void => setDraft(e.target.value)}
            rows={2}
            placeholder="Reply on this card…"
            className="flex-1 resize-none rounded-md border border-edge bg-atmos px-3 py-2 text-sm text-star placeholder:text-farstar focus:border-comet focus:outline-none"
          />
          <button
            type="submit"
            disabled={draft.trim().length === 0}
            className="rounded-md border border-edge bg-atmos px-3 py-2 text-xs font-semibold uppercase tracking-wider text-twilight hover:border-comet hover:text-star disabled:cursor-not-allowed disabled:opacity-50"
          >
            Send
          </button>
        </div>
      </form>
    </section>
  )
}

function mapEntries(comments: Comment[] | undefined): ThreadEntry[] {
  if (!comments) return []
  return comments
    .filter((c) => Boolean(c.body))
    .map((c, idx) => ({
      id: c.id || `c-${idx}`,
      body: c.body,
      author: c.author || "unknown",
      createdAt: c.createdAt,
      kind: looksLikeAgent(c.author) ? "agent" : "human",
    }))
}

function looksLikeAgent(author?: string): boolean {
  if (!author) return false
  const lower = author.toLowerCase()
  return lower.includes("agent") || lower.includes("nova") || lower.startsWith("bot:")
}

interface DayGroup {
  label: string
  entries: ThreadEntry[]
}

// groupByDay buckets entries into "today" / "yesterday" / dated groups,
// preserving order within each group.
function groupByDay(entries: ThreadEntry[]): DayGroup[] {
  const groups = new Map<string, ThreadEntry[]>()
  const order: string[] = []
  for (const e of entries) {
    const label = dayLabel(e.createdAt)
    if (!groups.has(label)) {
      groups.set(label, [])
      order.push(label)
    }
    groups.get(label)?.push(e)
  }
  return order.map((label) => ({ label, entries: groups.get(label) ?? [] }))
}

function dayLabel(iso?: string): string {
  if (!iso) return "Earlier"
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return "Earlier"
  const now = new Date()
  const start = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const target = new Date(date.getFullYear(), date.getMonth(), date.getDate())
  const diffDays = Math.round((start.getTime() - target.getTime()) / 86_400_000)
  if (diffDays <= 0) return "Today"
  if (diffDays === 1) return "Yesterday"
  return target.toLocaleDateString(undefined, { month: "short", day: "numeric" })
}

function DateDivider({ label }: { label: string }): JSX.Element {
  return (
    <div className="flex items-center gap-2 text-[10px] uppercase tracking-widest text-farstar">
      <span className="h-px flex-1 bg-edge" aria-hidden />
      <span>{label}</span>
      <span className="h-px flex-1 bg-edge" aria-hidden />
    </div>
  )
}

function Message({ entry }: { entry: ThreadEntry }): JSX.Element {
  const isAgent = entry.kind === "agent"
  return (
    <div
      className={
        isAgent
          ? "rounded-md border border-solar/30 bg-solar/5 px-3 py-2"
          : "rounded-md border border-edge/70 bg-atmos px-3 py-2"
      }
    >
      <div className="flex items-center justify-between gap-2 text-[10px] uppercase tracking-wider">
        <span className={isAgent ? "text-solar" : "text-comet"}>
          {entry.author}
        </span>
        {entry.createdAt ? (
          <time className="text-farstar" dateTime={entry.createdAt}>
            {formatTime(entry.createdAt)}
          </time>
        ) : null}
      </div>
      <p className="mt-1 whitespace-pre-wrap text-sm text-star">{entry.body}</p>
    </div>
  )
}

function formatTime(iso: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return ""
  return date.toLocaleTimeString(undefined, {
    hour: "numeric",
    minute: "2-digit",
  })
}
