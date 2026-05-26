import { useEffect, useState } from "react"
import type { RunQuestion } from "../types/board"

interface Props {
  question: RunQuestion
  // variant controls density. "card" matches the board-card inline banner;
  // "drawer" matches the larger Run-tab presentation in D1.3.
  variant?: "card" | "drawer"
  agentLabel?: string
}

// RunQuestionBanner is the solar-copper banner that surfaces a Nova question
// to the human. It is used in two places:
//   1. Inside Card.tsx for the board-row view (D2.1).
//   2. Inside CardRunTab.tsx for the drawer Run view (D1.3).
// The variant prop controls density; everything else is identical.
export function RunQuestionBanner({
  question,
  variant = "card",
  agentLabel,
}: Props): JSX.Element {
  const remaining = useTtlRemaining(question.asked_at, question.ttl_seconds)
  const isDrawer = variant === "drawer"

  return (
    <div
      role="status"
      aria-live="polite"
      className={
        isDrawer
          ? "rounded-md border border-solar/50 bg-solar/10 p-3 text-sm"
          : "mt-3 rounded-md border border-solar/40 bg-solar/10 p-2 text-xs"
      }
    >
      <div
        className={
          isDrawer
            ? "flex items-center justify-between gap-2 text-[11px] font-semibold uppercase tracking-wider text-solar"
            : "flex items-center justify-between gap-2 text-[10px] font-semibold uppercase tracking-wider text-solar"
        }
      >
        <span className="inline-flex items-center gap-1.5">
          <span aria-hidden>✦</span>
          {agentLabel || "Nova"} needs an answer
        </span>
        {remaining !== undefined ? (
          <span
            className={remaining <= 30 ? "font-mono text-magnetar" : "font-mono text-twilight"}
            title="time-to-live"
          >
            {formatRemaining(remaining)}
          </span>
        ) : null}
      </div>
      <p
        className={
          isDrawer
            ? "mt-1.5 text-star"
            : "mt-1 line-clamp-3 text-star"
        }
      >
        {question.prompt}
      </p>
      {isDrawer && question.reasoning ? (
        <details className="mt-2 text-xs text-twilight">
          <summary className="cursor-pointer text-[11px] uppercase tracking-wider text-farstar hover:text-twilight">
            Reasoning
          </summary>
          <p className="mt-1 whitespace-pre-wrap text-twilight">
            {question.reasoning}
          </p>
        </details>
      ) : null}
      {!isDrawer && question.suggestions && question.suggestions.length > 0 ? (
        <div className="mt-2 flex flex-wrap gap-1">
          {question.suggestions.slice(0, 3).map((suggestion) => (
            <span
              key={suggestion}
              className="rounded border border-solar/30 bg-void/40 px-1.5 py-0.5 text-[10px] text-star"
            >
              {suggestion}
            </span>
          ))}
        </div>
      ) : null}
    </div>
  )
}

// useTtlRemaining ticks down once per second from asked_at + ttl_seconds.
// Returns undefined when the question lacks timing info.
function useTtlRemaining(askedAt?: string, ttl?: number): number | undefined {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!askedAt || !ttl) return
    const id = window.setInterval(() => setNow(Date.now()), 1000)
    return (): void => window.clearInterval(id)
  }, [askedAt, ttl])
  if (!askedAt || !ttl) return undefined
  const started = new Date(askedAt).getTime()
  if (Number.isNaN(started)) return undefined
  const elapsed = Math.floor((now - started) / 1000)
  const remaining = ttl - elapsed
  return remaining > 0 ? remaining : 0
}

function formatRemaining(seconds: number): string {
  if (seconds <= 0) return "expired"
  const m = Math.floor(seconds / 60)
  const s = seconds % 60
  if (m === 0) return `${s}s`
  return `${m}m ${s.toString().padStart(2, "0")}s`
}
