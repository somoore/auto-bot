import { useState } from "react"
import type {
  AgentRunView,
  Card as CardModel,
  PlanStep,
  RunQuestion,
} from "../types/board"
import { RunQuestionBanner } from "./RunQuestionBanner"
import { SuggestedAnswerChip } from "./SuggestedAnswerChip"
import type { DispatchResult } from "../lib/useBoardSocket"

// DispatchFn is the narrow façade CardRunTab and CardDrawer take from
// useBoardSocket.dispatch(). Defined here so both files can import it
// without a circular dependency.
export type DispatchFn = (
  tool: string,
  args: Record<string, unknown>,
) => Promise<DispatchResult>

interface Props {
  card: CardModel
  question?: RunQuestion
  run?: AgentRunView
  dispatch: DispatchFn
  currentUserId?: string
}

// CardRunTab is the D1.3 "Run waiting" pane. It renders the question banner,
// up to three suggested-answer chips, a free-text composer, the plan step
// list with the current step highlighted, and Cancel / Take-over / Retry
// controls. All mutations go through `dispatch` which posts to
// /internal/tools/dispatch.
//
// Server-side support: the run.* and agent.* tools listed here are not yet
// implemented by /internal/tools/dispatch (which currently only switches on
// card.create|update|comment). The UI sends them and surfaces the resulting
// error inline. F1.2 will wire the server tools.
export function CardRunTab({
  card,
  question,
  run,
  dispatch,
  currentUserId,
}: Props): JSX.Element {
  const [free, setFree] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | undefined>(undefined)

  const answer = async (text: string): Promise<void> => {
    if (!question) return
    setBusy(true)
    setError(undefined)
    const res = await dispatch("run.answer_question", {
      question_id: question.id,
      answer: text,
      answered_by: currentUserId || "ui",
      answered_via: "ui",
    })
    setBusy(false)
    if (!res.ok) setError(res.error || `dispatch failed (${res.status})`)
    else setFree("")
  }

  const takeOver = async (): Promise<void> => {
    if (!run) return
    setBusy(true)
    setError(undefined)
    const res = await dispatch("agent.take_over_run", {
      run_id: run.run_id,
      taken_over_by: currentUserId || "ui",
    })
    setBusy(false)
    if (!res.ok) setError(res.error || `dispatch failed (${res.status})`)
  }

  const cancel = async (): Promise<void> => {
    if (!run) return
    const reason = window.prompt("Cancel this run — reason?", "Cancelled by human")
    if (reason === null) return
    setBusy(true)
    setError(undefined)
    const res = await dispatch("agent.cancel_run", {
      run_id: run.run_id,
      reason,
      cancelled_by: currentUserId || "ui",
    })
    setBusy(false)
    if (!res.ok) setError(res.error || `dispatch failed (${res.status})`)
  }

  const retry = async (stepIndex: number): Promise<void> => {
    if (!run) return
    setBusy(true)
    setError(undefined)
    const res = await dispatch("agent.retry_run", {
      run_id: run.run_id,
      start_step: stepIndex,
    })
    setBusy(false)
    if (!res.ok) setError(res.error || `dispatch failed (${res.status})`)
  }

  return (
    <section
      id="drawer-panel-run"
      role="tabpanel"
      className="space-y-4 px-4 py-4"
    >
      {question ? (
        <div className="space-y-3">
          <RunQuestionBanner
            question={question}
            variant="drawer"
            agentLabel={run?.agent_profile || run?.specialist}
          />
          <SuggestedAnswers
            question={question}
            disabled={busy}
            onPick={(text): void => {
              void answer(text)
            }}
          />
          <form
            onSubmit={(e): void => {
              e.preventDefault()
              const text = free.trim()
              if (text.length === 0) return
              void answer(text)
            }}
            className="space-y-2"
          >
            <label
              htmlFor="run-free-text"
              className="text-[11px] font-semibold uppercase tracking-wider text-farstar"
            >
              Or type your own
            </label>
            <textarea
              id="run-free-text"
              value={free}
              onChange={(e): void => setFree(e.target.value)}
              rows={2}
              placeholder="Give Nova a different answer…"
              className="w-full resize-none rounded-md border border-edge bg-atmos px-3 py-2 text-sm text-star placeholder:text-farstar focus:border-comet focus:outline-none"
            />
            <button
              type="submit"
              disabled={busy || free.trim().length === 0}
              className="inline-flex items-center gap-2 rounded-md border border-solar bg-solar/15 px-3 py-2 text-xs font-semibold uppercase tracking-wider text-star hover:bg-solar/25 disabled:cursor-not-allowed disabled:opacity-50"
            >
              Send answer
            </button>
          </form>
        </div>
      ) : (
        <p className="text-sm text-twilight">
          No open question right now. Nova will surface anything she gets stuck
          on here.
        </p>
      )}

      {run ? (
        <div className="space-y-3">
          <RunHeader run={run} />
          {run.plan && run.plan.length > 0 ? (
            <PlanList plan={run.plan} onRetry={(idx): void => void retry(idx)} disabled={busy} />
          ) : null}
          <div className="flex flex-wrap gap-2">
            <button
              type="button"
              onClick={(): void => void takeOver()}
              disabled={busy}
              className="rounded-md border border-comet bg-comet/15 px-3 py-2 text-xs font-semibold uppercase tracking-wider text-star hover:bg-comet/25 disabled:cursor-not-allowed disabled:opacity-50"
            >
              Take over
            </button>
            <button
              type="button"
              onClick={(): void => void cancel()}
              disabled={busy}
              className="rounded-md border border-magnetar bg-magnetar/15 px-3 py-2 text-xs font-semibold uppercase tracking-wider text-star hover:bg-magnetar/25 disabled:cursor-not-allowed disabled:opacity-50"
            >
              Cancel run
            </button>
          </div>
        </div>
      ) : null}

      {error ? (
        <p
          role="alert"
          className="rounded-md border border-magnetar/50 bg-magnetar/10 px-3 py-2 text-xs text-magnetar"
        >
          {error}
        </p>
      ) : null}

      <p className="text-[10px] text-farstar">
        Card {card.id} · {card.status}
      </p>
    </section>
  )
}

function SuggestedAnswers({
  question,
  disabled,
  onPick,
}: {
  question: RunQuestion
  disabled: boolean
  onPick: (text: string) => void
}): JSX.Element | null {
  const suggestions = (question.suggestions || []).slice(0, 3)
  if (suggestions.length === 0) return null
  return (
    <ul className="space-y-2" data-testid="suggested-answers">
      {suggestions.map((s, i) => (
        <li key={s}>
          <SuggestedAnswerChip
            index={i + 1}
            label={s}
            recommended={i === 0}
            showEnterHint={i === 0}
            disabled={disabled}
            onClick={(): void => onPick(s)}
          />
        </li>
      ))}
    </ul>
  )
}

function RunHeader({ run }: { run: AgentRunView }): JSX.Element {
  return (
    <div className="flex items-center justify-between rounded-md border border-edge bg-atmos px-3 py-2">
      <div>
        <p className="text-[11px] uppercase tracking-wider text-farstar">
          {run.agent_profile || run.specialist || "agent"}
        </p>
        <p className="text-sm text-star">{run.current_step || run.status}</p>
      </div>
      <span
        className={
          run.status === "needs_input" || run.status === "waiting_on_human"
            ? "rounded-full border border-solar/40 bg-solar/15 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-solar"
            : "rounded-full border border-edge px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-twilight"
        }
      >
        {run.status.replace(/_/g, " ")}
      </span>
    </div>
  )
}

function PlanList({
  plan,
  onRetry,
  disabled,
}: {
  plan: PlanStep[]
  onRetry: (stepIndex: number) => void
  disabled: boolean
}): JSX.Element {
  return (
    <ol className="space-y-1" data-testid="plan-list">
      {plan.map((step) => (
        <li
          key={step.index}
          className={
            step.status === "running"
              ? "flex items-center justify-between gap-2 rounded-md border border-solar/40 bg-solar/10 px-3 py-2 text-sm text-star"
              : step.status === "done"
                ? "flex items-center justify-between gap-2 rounded-md border border-edge/60 bg-sky/60 px-3 py-2 text-sm text-twilight"
                : "flex items-center justify-between gap-2 rounded-md border border-edge/40 bg-atmos/40 px-3 py-2 text-sm text-twilight"
          }
        >
          <div className="min-w-0">
            <p className="text-[10px] font-mono uppercase tracking-wider text-farstar">
              step {step.index + 1} · {step.status}
            </p>
            <p className="truncate">{step.title}</p>
          </div>
          {step.status === "done" ? (
            <button
              type="button"
              onClick={(): void => onRetry(step.index)}
              disabled={disabled}
              className="shrink-0 rounded border border-edge px-2 py-1 text-[10px] font-semibold uppercase tracking-wider text-twilight hover:border-comet hover:text-star disabled:cursor-not-allowed disabled:opacity-50"
            >
              Retry from here
            </button>
          ) : null}
        </li>
      ))}
    </ol>
  )
}
