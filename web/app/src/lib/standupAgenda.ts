// standupAgenda hooks the React AgendaOverlay to /internal/standup/agenda.
// The endpoint returns internal/standup.Agenda (Go); this file translates
// that shape into the existing UI-side Agenda interface in
// components/AgendaOverlay.tsx so the visual layer stays decoupled from
// wire-format drift.

import { useEffect, useState } from "react"
import type {
  Agenda,
  AgendaBlocker,
  AgendaHighlight,
  AgendaReview,
  AgendaSpeaker,
} from "../components/AgendaOverlay"

// StandupAgendaWire mirrors internal/standup.Agenda JSON tags exactly. Keep
// this in lockstep with internal/standup/agenda.go.
export interface StandupAgendaWire {
  tenant_id: string
  board_id: string
  generated_at: string
  window: string
  highlights?: Array<{
    card_id: string
    title: string
    status: string
    assignee?: string
    note?: string
  }>
  blockers?: Array<{
    card_id: string
    title: string
    reason?: string
    assignee?: string
    updated_at?: string
  }>
  runs_awaiting_review?: Array<{
    run_id: string
    card_id: string
    jira_issue_key?: string
    status: string
    agent_profile?: string
    summary?: string
    updated_at?: string
  }>
  open_questions?: Array<{
    question_id: string
    run_id: string
    card_id: string
    prompt: string
    asked_at: string
  }>
  proposed_speaker_order?: string[]
  summary?: string
}

// translateAgenda maps the Go-side wire shape into the UI Agenda. The
// Go Status field is "Done" | "In Progress" (board.StatusDone /
// board.StatusInProgress). Map to the visual kinds the drawer already
// understands ("shipped" | "run_done"). open_questions become blockers
// because each is a card waiting on a human answer.
// runs_awaiting_review surface as reviews so the host has a clear "ack me"
// list.
export function translateAgenda(wire: StandupAgendaWire): Agenda {
  const highlights: AgendaHighlight[] = (wire.highlights ?? []).map((h) => ({
    kind: /^done$/i.test(h.status) ? "shipped" : "run_done",
    title: h.title,
    attribution: h.assignee
      ? `${h.assignee}${h.note ? ` · ${h.note}` : ""}`
      : h.note,
  }))

  const blockers: AgendaBlocker[] = [
    ...(wire.blockers ?? []).map(
      (b): AgendaBlocker => ({
        cardId: b.card_id,
        question: b.title + (b.reason ? ` — ${b.reason}` : ""),
        ownerLabel: b.assignee ? `${b.assignee} needs an answer` : undefined,
      }),
    ),
    ...(wire.open_questions ?? []).map(
      (q): AgendaBlocker => ({
        cardId: q.card_id,
        question: q.prompt,
        ownerLabel: `run ${q.run_id} waiting on human`,
      }),
    ),
  ]

  const reviews: AgendaReview[] = (wire.runs_awaiting_review ?? []).map(
    (r): AgendaReview => ({
      cardId: r.card_id,
      title: r.summary || `Run ${r.run_id} awaiting review`,
      prNumber: r.jira_issue_key,
      reviewLabel: `${r.status} · ${r.agent_profile ?? "agent"}`,
    }),
  )

  const speakerOrder: AgendaSpeaker[] = (wire.proposed_speaker_order ?? []).map(
    (name): AgendaSpeaker => ({
      participant: { id: name, name },
    }),
  )

  return {
    preparedAt: wire.generated_at,
    scheduledFor: undefined,
    estimate: wire.window ? `lookback ${wire.window}` : undefined,
    participantSummary: wire.summary,
    highlights,
    blockers,
    reviews,
    speakerOrder,
  }
}

export interface UseAgendaResult {
  agenda?: Agenda
  loading: boolean
  error?: string
}

// useStandupAgenda fetches /internal/standup/agenda when `enabled` is true
// (typically when the overlay is opened). The hook returns `agenda` only
// after the fetch resolves so the caller can fall back to sample data on
// 404/error without a render flash while loading.
export function useStandupAgenda(enabled: boolean): UseAgendaResult {
  const [agenda, setAgenda] = useState<Agenda | undefined>(undefined)
  const [loading, setLoading] = useState<boolean>(false)
  const [error, setError] = useState<string | undefined>(undefined)

  useEffect(() => {
    if (!enabled) return
    let cancelled = false
    setLoading(true)
    setError(undefined)
    void (async (): Promise<void> => {
      try {
        const res = await fetch("/internal/standup/agenda", {
          method: "GET",
          credentials: "include",
        })
        if (!res.ok) {
          if (!cancelled) {
            setError(`HTTP ${res.status}`)
            setLoading(false)
          }
          return
        }
        const wire = (await res.json()) as StandupAgendaWire
        if (cancelled) return
        setAgenda(translateAgenda(wire))
        setLoading(false)
      } catch (err) {
        if (cancelled) return
        setError(err instanceof Error ? err.message : "network error")
        setLoading(false)
      }
    })()
    return (): void => {
      cancelled = true
    }
  }, [enabled])

  return { agenda, loading, error }
}
