import { useCallback, useMemo, useState } from "react"
import { DiffPreview } from "./DiffPreview"
import type { PendingActionEnvelope } from "../types/board"

interface Props {
  pending: PendingActionEnvelope[]
}

// DryRunQueue renders the staged-action queue with approve / reject controls
// and an embedded DiffPreview for each entry. The component is purely
// presentational on top of the WS state; calls to /tenant/pending_actions/
// {id}/{approve|reject} drive the lifecycle.
export function DryRunQueue({ pending }: Props): JSX.Element | null {
  if (pending.length === 0) return null
  return (
    <section aria-labelledby="dry-run-queue-heading" className="border-b border-edge/60 bg-atmos/40">
      <div className="mx-auto max-w-[1400px] px-4 py-4 sm:px-6">
        <header className="mb-3 flex items-baseline justify-between">
          <h2 id="dry-run-queue-heading" className="text-xs font-semibold uppercase tracking-widest text-farstar">
            Dry-run queue
          </h2>
          <span className="text-xs text-twilight">{pending.length} pending</span>
        </header>
        <div className="flex flex-col gap-3">
          {pending.map((env) => (
            <DryRunRow key={env.action.action_id} envelope={env} />
          ))}
        </div>
      </div>
    </section>
  )
}

function DryRunRow({ envelope }: { envelope: PendingActionEnvelope }): JSX.Element {
  const [busy, setBusy] = useState<"approve" | "reject" | undefined>(undefined)
  const [error, setError] = useState<string | undefined>(undefined)
  const [expanded, setExpanded] = useState(false)

  const decide = useCallback(async (verb: "approve" | "reject"): Promise<void> => {
    setBusy(verb)
    setError(undefined)
    try {
      const res = await fetch(`/tenant/pending_actions/${envelope.action.action_id}/${verb}`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ note: "" }),
      })
      if (!res.ok) {
        const text = await res.text()
        setError(`${verb} failed: ${text || res.status}`)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "request failed")
    } finally {
      setBusy(undefined)
    }
  }, [envelope.action.action_id])

  const argSummary = useMemo(() => summarizeArgs(envelope.action.args), [envelope.action.args])

  return (
    <article className="rounded-lg border border-edge bg-void/80 p-3">
      <header className="flex flex-wrap items-baseline justify-between gap-2">
        <div className="flex items-baseline gap-2">
          <span className="rounded-full bg-comet/20 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-widest text-comet">
            {envelope.action.tool}
          </span>
          <span className="text-xs text-twilight">{argSummary}</span>
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            className="rounded-full border border-edge bg-atmos px-3 py-1 text-xs text-twilight hover:text-star"
            onClick={(): void => setExpanded((v) => !v)}
          >
            {expanded ? "Hide diff" : "Show diff"}
          </button>
          <button
            type="button"
            disabled={busy !== undefined}
            className="rounded-full border border-solar/40 bg-solar/10 px-3 py-1 text-xs font-medium text-solar hover:bg-solar/20 disabled:opacity-50"
            onClick={(): void => { void decide("approve") }}
          >
            {busy === "approve" ? "Approving…" : "Approve"}
          </button>
          <button
            type="button"
            disabled={busy !== undefined}
            className="rounded-full border border-magnetar/40 bg-magnetar/10 px-3 py-1 text-xs font-medium text-magnetar hover:bg-magnetar/20 disabled:opacity-50"
            onClick={(): void => { void decide("reject") }}
          >
            {busy === "reject" ? "Rejecting…" : "Reject"}
          </button>
        </div>
      </header>
      {error ? <p className="mt-2 text-[10px] text-magnetar">{error}</p> : null}
      {expanded ? (
        <div className="mt-3">
          <DiffPreview diff={envelope.diff} />
        </div>
      ) : null}
    </article>
  )
}

function summarizeArgs(args?: Record<string, unknown>): string {
  if (!args) return ""
  const title = args["title"]
  const cardID = args["card_id"]
  const target = args["target_status"] || args["status"]
  if (typeof title === "string" && title) return `“${title}”`
  if (typeof cardID === "string" && cardID) {
    if (typeof target === "string" && target) return `${cardID} → ${target}`
    return cardID
  }
  return ""
}
