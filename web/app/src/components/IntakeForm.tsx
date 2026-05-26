import { useState, type FormEvent } from "react"

// IntakeForm is the async-standup intake surface that backstops the voice
// path. Daria's first-week walkthrough (docs/persona-feedback/
// daria-first-week.md) flags voice-only as the #1 missing piece for
// distributed teams; this form is what those teams use instead.
//
// On submit the form POSTs to /intake/standup with Source=form, then
// renders the server's confirmation: the persisted intake plus any
// cards the server created from blockers and any comments posted to
// mentioned cards. Cards-from-blockers and comments-on-mentions are
// wired in cmd/server/intake_followups.go.

interface CreatedCardSummary {
  id: string
  title: string
  status: string
}

interface PostedCommentSummary {
  card_id: string
  body: string
  author?: string
}

interface IntakeResponse {
  ok: boolean
  intake: {
    submitter: string
    today?: string
    yesterday?: string
    source?: string
    submitted_at?: string
  }
  created?: CreatedCardSummary[]
  comments?: PostedCommentSummary[]
}

interface Props {
  /** Override the POST URL — primarily for tests. */
  endpoint?: string
  /** Bearer token if the SPA isn't relying on cookie auth. */
  bearerToken?: string
}

const TEXTAREA_BASE =
  "w-full rounded-md border border-edge bg-atmos px-3 py-2 text-sm text-star placeholder:text-farstar/70 focus:outline-none focus:ring-2 focus:ring-aurora/40 disabled:cursor-not-allowed disabled:opacity-60"

export function IntakeForm({ endpoint = "/intake/standup", bearerToken }: Props): JSX.Element {
  const [yesterday, setYesterday] = useState("")
  const [today, setToday] = useState("")
  const [blockers, setBlockers] = useState("")
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [confirmation, setConfirmation] = useState<IntakeResponse | null>(null)

  const submit = async (event: FormEvent<HTMLFormElement>): Promise<void> => {
    event.preventDefault()
    if (submitting) return
    setError(null)
    setConfirmation(null)

    const today_ = today.trim()
    const yesterday_ = yesterday.trim()
    const blockerLines = blockers
      .split("\n")
      .map((line) => line.trim())
      .filter((line) => line.length > 0)
      .map((text) => ({ text }))

    if (today_ === "" && yesterday_ === "" && blockerLines.length === 0) {
      setError("Add at least a yesterday, today, or blocker entry.")
      return
    }

    const body = {
      yesterday: yesterday_,
      today: today_,
      blockers: blockerLines,
      source: "form",
    }
    setSubmitting(true)
    try {
      const headers: Record<string, string> = { "Content-Type": "application/json" }
      if (bearerToken) headers["Authorization"] = `Bearer ${bearerToken}`
      const res = await fetch(endpoint, {
        method: "POST",
        credentials: "include",
        headers,
        body: JSON.stringify(body),
      })
      if (!res.ok) {
        const errBody = await safeReadError(res)
        setError(errBody)
        return
      }
      const payload = (await res.json()) as IntakeResponse
      setConfirmation(payload)
      // Clear blockers on success so an EM can file the next person's
      // standup without re-typing the boilerplate. Yesterday/Today
      // intentionally stay so a backlog of standups can chain.
      setBlockers("")
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <section
      className="mx-auto w-full max-w-2xl rounded-xl border border-edge/60 bg-sky/60 p-5 shadow-xl"
      aria-labelledby="intake-form-heading"
    >
      <header className="mb-4">
        <h2 id="intake-form-heading" className="text-base font-semibold text-star">
          Async standup
        </h2>
        <p className="mt-1 text-xs text-twilight">
          Drop a written standup. The agent loop produces the same cards, comments, and run
          assignments as a voice meeting.
        </p>
      </header>
      <form onSubmit={(e) => void submit(e)} className="flex flex-col gap-4" noValidate>
        <label className="flex flex-col gap-1.5 text-xs font-medium text-twilight">
          Yesterday
          <textarea
            className={TEXTAREA_BASE}
            rows={3}
            placeholder="What shipped, what landed in review, what closed."
            value={yesterday}
            onChange={(e) => setYesterday(e.target.value)}
            disabled={submitting}
            data-testid="intake-yesterday"
          />
        </label>
        <label className="flex flex-col gap-1.5 text-xs font-medium text-twilight">
          Today
          <textarea
            className={TEXTAREA_BASE}
            rows={3}
            placeholder="What you're picking up. Reference cards by id (card-001, PROJ-42) when it helps."
            value={today}
            onChange={(e) => setToday(e.target.value)}
            disabled={submitting}
            data-testid="intake-today"
          />
        </label>
        <label className="flex flex-col gap-1.5 text-xs font-medium text-twilight">
          Blockers <span className="font-normal text-farstar">(one per line — each unblocked one becomes a Blocked card)</span>
          <textarea
            className={TEXTAREA_BASE}
            rows={3}
            placeholder="Need Linear creds&#10;Waiting on review of card-007"
            value={blockers}
            onChange={(e) => setBlockers(e.target.value)}
            disabled={submitting}
            data-testid="intake-blockers"
          />
        </label>
        <div className="flex items-center justify-end gap-3">
          {error ? (
            <span className="text-xs text-magnetar" data-testid="intake-error">
              {error}
            </span>
          ) : null}
          <button
            type="submit"
            disabled={submitting}
            data-testid="intake-submit"
            className="inline-flex items-center justify-center rounded-md bg-aurora px-4 py-2 text-sm font-semibold text-void shadow-sm transition hover:bg-aurora/90 focus:outline-none focus:ring-2 focus:ring-aurora/50 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {submitting ? "Submitting…" : "Submit standup"}
          </button>
        </div>
      </form>
      {confirmation ? <IntakeConfirmation result={confirmation} /> : null}
    </section>
  )
}

function IntakeConfirmation({ result }: { result: IntakeResponse }): JSX.Element {
  const created = result.created ?? []
  const comments = result.comments ?? []
  return (
    <div
      className="mt-5 rounded-md border border-aurora/30 bg-aurora/5 p-4 text-sm text-star"
      data-testid="intake-confirmation"
    >
      <p className="font-medium text-aurora">Intake recorded.</p>
      <p className="mt-1 text-xs text-twilight">
        Submitted by <span className="text-star">{result.intake.submitter}</span>
        {result.intake.submitted_at ? <> at <span className="text-star">{result.intake.submitted_at}</span></> : null}.
      </p>
      {created.length > 0 ? (
        <div className="mt-3">
          <p className="text-xs font-semibold uppercase tracking-wide text-aurora">
            Cards created
          </p>
          <ul className="mt-1 space-y-1 text-xs text-twilight" data-testid="intake-created-list">
            {created.map((card) => (
              <li key={card.id}>
                <span className="text-star">{card.title}</span>
                <span className="ml-2 text-farstar">({card.status})</span>
              </li>
            ))}
          </ul>
        </div>
      ) : null}
      {comments.length > 0 ? (
        <div className="mt-3">
          <p className="text-xs font-semibold uppercase tracking-wide text-aurora">
            Comments posted
          </p>
          <ul className="mt-1 space-y-1 text-xs text-twilight" data-testid="intake-comments-list">
            {comments.map((comment, index) => (
              <li key={`${comment.card_id}-${index}`}>
                <span className="text-star">{comment.card_id}</span>
              </li>
            ))}
          </ul>
        </div>
      ) : null}
    </div>
  )
}

async function safeReadError(res: Response): Promise<string> {
  try {
    const data = (await res.json()) as { error?: string }
    if (data?.error) return data.error
  } catch {
    // ignore — fall through to generic message
  }
  return `Submission failed (${res.status})`
}

export default IntakeForm
