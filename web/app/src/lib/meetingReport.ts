// meetingReport hooks PostMeetingSummary to GET /meetings/{id}. The server
// returns the heavyweight meetingIntelligenceReport
// (cmd/server/meeting_reports.go); this file selects the subset the D2.6
// view actually needs and translates it into the UI's MeetingReport shape.

import { useEffect, useState } from "react"
import type {
  AgentWorkingRow,
  DecisionRow,
  FollowUpRow,
  MeetingReport,
  SyncTarget,
} from "../components/PostMeetingSummary"

// MeetingReportWire mirrors the JSON tags on
// cmd/server/meeting_reports.go::meetingIntelligenceReport. Only the
// fields the D2.6 view consumes are typed; unused fields are intentionally
// left untyped so the wire schema can evolve.
export interface MeetingReportWire {
  meeting_id: string
  meeting_type?: string
  generated_at: string
  started_at?: string
  ended_at?: string
  summary?: string
  decisions?: string[]
  action_items?: string[]
  participants?: Array<{ identity: string; role?: string }>
  follow_ups?: Array<{
    id?: string
    title?: string
    description?: string
    owner?: string
    assignee?: string
    card_id?: string
    jira_issue_key?: string
  }>
  agent_runs?: Array<{
    run_id: string
    card_id: string
    status?: string
    agent_profile?: string
    summary?: string
    plan?: Array<{ status?: string }>
    cost_cents?: number
  }>
  jira_changes?: Array<{
    card_id?: string
    tool_name?: string
    source?: string
    occurred_at?: string
  }>
  product_proof?: {
    actual_meeting_minutes?: number
    estimated_net_minutes_saved?: number
  }
}

function formatTime(rfc: string | undefined): string {
  if (!rfc) return ""
  const date = new Date(rfc)
  if (Number.isNaN(date.getTime())) return ""
  return date.toLocaleTimeString(undefined, {
    hour: "numeric",
    minute: "2-digit",
  })
}

function formatDate(rfc: string | undefined): string {
  if (!rfc) return ""
  const date = new Date(rfc)
  if (Number.isNaN(date.getTime())) return ""
  return date.toLocaleDateString(undefined, { month: "short", day: "numeric" })
}

function formatDurationMinutes(minutes: number | undefined): string {
  if (!minutes || minutes <= 0) return ""
  const m = Math.round(minutes)
  if (m < 60) return `${m}m`
  const hours = Math.floor(m / 60)
  const rem = m % 60
  return rem === 0 ? `${hours}h` : `${hours}h ${rem}m`
}

// translateMeetingReport maps the heavyweight wire shape into the slim
// MeetingReport the UI renders. Missing fields fall back to empty arrays
// or sensible defaults — the view tolerates "no data" cells.
export function translateMeetingReport(wire: MeetingReportWire): MeetingReport {
  const endedTime = formatTime(wire.ended_at)
  const endedDate = formatDate(wire.ended_at)
  const participants = wire.participants ?? []
  const cardsTouched = new Set<string>()
  for (const change of wire.jira_changes ?? []) {
    if (change.card_id) cardsTouched.add(change.card_id)
  }
  const agentRuns = wire.agent_runs ?? []
  for (const run of agentRuns) {
    if (run.card_id) cardsTouched.add(run.card_id)
  }
  const cardsMovedByNova = (wire.jira_changes ?? []).filter(
    (c) => c.source === "agent" || c.source === "voice" || c.source === "nova",
  ).length
  const cardsMoved = cardsTouched.size
  const cardsMovedByTeam = Math.max(0, cardsMoved - cardsMovedByNova)

  const meetingMinutes = wire.product_proof?.actual_meeting_minutes ?? 0
  const netMinutes = wire.product_proof?.estimated_net_minutes_saved ?? 0

  const decisions: DecisionRow[] = (wire.decisions ?? []).map((d, i) => ({
    id: `d-${i}`,
    title: d,
    rationale: "",
    timestamp: endedTime,
  }))

  const followUps: FollowUpRow[] = (wire.follow_ups ?? []).map((f, i) => ({
    id: f.id ?? `f-${i}`,
    title: f.title ?? f.description ?? `Follow-up ${i + 1}`,
    jiraId: f.jira_issue_key ?? f.card_id ?? "",
    assignee: f.assignee ?? f.owner ?? "",
  }))

  // Open agent runs (not completed) drive the "Agents working now" panel.
  const activeStatuses = new Set([
    "queued",
    "classifying",
    "fetching_context",
    "reviewing",
    "publishing",
    "retrying",
    "waiting_on_human",
    "needs_input",
  ])
  const agentsWorking: AgentWorkingRow[] = agentRuns
    .filter((r) => activeStatuses.has(r.status ?? ""))
    .map((r) => {
      const total = r.plan?.length ?? 0
      const done = (r.plan ?? []).filter((p) => p.status === "done").length
      const costDollars = ((r.cost_cents ?? 0) / 100).toFixed(2)
      return {
        id: r.run_id,
        name: r.agent_profile ?? r.run_id,
        cardId: r.card_id,
        stepsDone: done,
        stepsTotal: total,
        etaMinutes: 0,
        cost: `$${costDollars}`,
      }
    })

  const runsKickedOff = agentRuns.length
  const runsEtaMinutes = agentsWorking.reduce((acc, a) => acc + a.etaMinutes, 0)

  const meetingType = wire.meeting_type ?? "meeting"
  const title = `${meetingType.charAt(0).toUpperCase()}${meetingType.slice(1)}`
  const endedAtLabel = endedTime ? `Ended · ${endedTime}` : "Meeting ended"
  const durationLabel = formatDurationMinutes(meetingMinutes)
  const metaParts = [endedDate, durationLabel, `${participants.length} participants`].filter(
    (s) => s,
  )
  const metadataLabel = metaParts.join(" · ")

  const syncTargets: SyncTarget[] = []
  if ((wire.jira_changes ?? []).length > 0) {
    syncTargets.push({
      id: "jira",
      label: "Jira",
      detail: `${wire.jira_changes!.length} cards updated`,
      timestamp: endedTime,
    })
  }

  return {
    meetingId: wire.meeting_id,
    title,
    endedAtLabel,
    metadataLabel,
    breadcrumb: `agent-first v2 / ${title} · summary`,
    cardsMoved,
    cardsMovedByNova,
    cardsMovedByTeam,
    runsKickedOff,
    runsEtaMinutes,
    questionsResolved: 0,
    questionAnswer: wire.summary ?? "",
    cost: "—",
    timeSaved: netMinutes > 0 ? `est. ${formatDurationMinutes(netMinutes)} saved` : "",
    decisions,
    followUps,
    agentsWorking,
    agentsFootnote:
      agentsWorking.length > 0
        ? "You'll be notified when either needs your input or completes."
        : "No agents are currently running.",
    syncTargets,
  }
}

export interface UseMeetingReportResult {
  report?: MeetingReport
  loading: boolean
  // notFound distinguishes "fetched, server said 404" from "still loading"
  // so the caller can switch in the sample fallback only after the server
  // has weighed in.
  notFound: boolean
  error?: string
}

// useMeetingReport fetches /meetings/{meetingId} when `meetingId` is set.
// On 404 the hook sets notFound=true so the caller can render the sample
// fallback without a render flash. Network/decode errors are surfaced via
// `error` and treated the same as 404 by the caller.
export function useMeetingReport(
  meetingId: string | null,
): UseMeetingReportResult {
  const [report, setReport] = useState<MeetingReport | undefined>(undefined)
  const [loading, setLoading] = useState<boolean>(false)
  const [notFound, setNotFound] = useState<boolean>(false)
  const [error, setError] = useState<string | undefined>(undefined)

  useEffect(() => {
    setReport(undefined)
    setNotFound(false)
    setError(undefined)
    if (!meetingId) {
      setLoading(false)
      return
    }
    let cancelled = false
    setLoading(true)
    void (async (): Promise<void> => {
      try {
        const res = await fetch(`/meetings/${encodeURIComponent(meetingId)}`, {
          method: "GET",
          credentials: "include",
        })
        if (cancelled) return
        if (res.status === 404) {
          setNotFound(true)
          setLoading(false)
          return
        }
        if (!res.ok) {
          setError(`HTTP ${res.status}`)
          setLoading(false)
          return
        }
        const wire = (await res.json()) as MeetingReportWire
        if (cancelled) return
        setReport(translateMeetingReport(wire))
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
  }, [meetingId])

  return { report, loading, notFound, error }
}
