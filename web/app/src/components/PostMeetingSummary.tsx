// PostMeetingSummary renders artboard D2.6 from the Paper design
// (Post-meeting summary · Observatory Deck). This is a full-page view that
// is mounted from App.tsx when ?meeting=<id> is in the URL or when the
// post-meeting WebSocket event surfaces a report. The shape of MeetingReport
// here is the slimmed-down view the UI cares about — when /meetings/{id}
// lands we'll map the server's meetingIntelligenceReport into this shape in
// App.tsx rather than scattering the field rename through every renderer.

export interface DecisionRow {
  id: string
  title: string
  rationale: string
  timestamp: string // e.g. "9:04 AM"
}

export interface FollowUpRow {
  id: string
  title: string
  jiraId: string
  assignee: string // display name, used to render the avatar initials
}

export interface AgentWorkingRow {
  id: string // e.g. "swe-1"
  name: string // e.g. "Swe Agent 1"
  cardId: string // e.g. "ABV2-218"
  stepsDone: number
  stepsTotal: number
  etaMinutes: number
  cost: string // e.g. "$0.12"
}

export interface SyncTarget {
  id: string
  label: string // e.g. "Jira"
  detail: string // e.g. "7 cards updated"
  timestamp: string // e.g. "2s ago"
}

export interface MeetingReport {
  meetingId: string
  title: string // "Tuesday standup"
  endedAtLabel: string // "Standup ended · 9:08 AM"
  metadataLabel: string // "May 26 · 8m 14s · 3 participants + nova"
  breadcrumb: string // "agent-first v2 / Tuesday standup · summary"
  cardsMoved: number
  cardsMovedByNova: number
  cardsMovedByTeam: number
  runsKickedOff: number
  runsEtaMinutes: number
  questionsResolved: number
  questionAnswer: string // "per-tenant DB · answered by Scott"
  cost: string // "$0.43"
  timeSaved: string // "est. 1h 50m saved"
  decisions: DecisionRow[]
  followUps: FollowUpRow[]
  agentsWorking: AgentWorkingRow[]
  agentsFootnote: string
  syncTargets: SyncTarget[]
}

interface Props {
  report: MeetingReport
  onBackToBoard: () => void
}

export function PostMeetingSummary({ report, onBackToBoard }: Props): JSX.Element {
  const handleCopyRecap = (): void => {
    const text = buildRecapText(report)
    if (typeof navigator !== "undefined" && navigator.clipboard) {
      void navigator.clipboard.writeText(text)
    }
  }

  const handleOpenInJira = (): void => {
    // TODO: deep-link into Jira once the JQL filter for this meeting's
    // touched cards is plumbed through the meeting report payload.
  }

  return (
    <div className="flex min-h-full flex-col bg-void text-star antialiased [font-synthesis:none]">
      <TopNav
        breadcrumb={report.breadcrumb}
        onCopyRecap={handleCopyRecap}
        onOpenInJira={handleOpenInJira}
        onBackToBoard={onBackToBoard}
      />
      <HeaderBlock
        endedAtLabel={report.endedAtLabel}
        title={report.title}
        metadataLabel={report.metadataLabel}
      />
      <MetricStrip report={report} />
      <Body report={report} />
    </div>
  )
}

function TopNav({
  breadcrumb,
  onCopyRecap,
  onOpenInJira,
  onBackToBoard,
}: {
  breadcrumb: string
  onCopyRecap: () => void
  onOpenInJira: () => void
  onBackToBoard: () => void
}): JSX.Element {
  const [scope, trail] = splitBreadcrumb(breadcrumb)
  return (
    <div className="flex items-center gap-6 border-b border-edge bg-sky px-8 py-[18px]">
      <div className="flex shrink-0 items-center gap-2.5">
        <span aria-hidden className="flex h-[22px] w-[22px] items-center justify-center rounded-[4px] bg-aurora">
          <span className="block h-2 w-2 rounded-[1px] bg-void" />
        </span>
        <span className="font-['Inter_Tight',system-ui,sans-serif] text-[17px] font-semibold leading-[22px] tracking-[-0.01em] text-star">
          auto-bot
        </span>
      </div>
      <nav
        aria-label="breadcrumb"
        className="flex grow basis-0 items-center gap-2 border-l border-edge pl-4"
      >
        <span className="font-['Inter_Tight',system-ui,sans-serif] text-[13px] leading-4 text-twilight">
          {scope}
        </span>
        <span aria-hidden className="font-['Inter_Tight',system-ui,sans-serif] text-[13px] leading-4 text-farstar">
          /
        </span>
        <span className="font-['Inter_Tight',system-ui,sans-serif] text-[13px] font-medium leading-4 text-star">
          {trail}
        </span>
      </nav>
      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={onCopyRecap}
          data-testid="post-meeting-copy-recap"
          className="rounded-md border border-edge bg-atmos px-3 py-1.5 font-['Inter_Tight',system-ui,sans-serif] text-xs font-medium leading-4 text-star transition hover:border-twilight/40 focus:outline-none focus:ring-2 focus:ring-aurora/40"
        >
          Copy recap
        </button>
        <button
          type="button"
          onClick={onOpenInJira}
          data-testid="post-meeting-open-jira"
          className="rounded-md border border-edge bg-atmos px-3 py-1.5 font-['Inter_Tight',system-ui,sans-serif] text-xs font-medium leading-4 text-star transition hover:border-twilight/40 focus:outline-none focus:ring-2 focus:ring-aurora/40"
        >
          Open in Jira
        </button>
        <button
          type="button"
          onClick={onBackToBoard}
          data-testid="post-meeting-back-to-board"
          className="rounded-md bg-aurora px-3.5 py-1.5 font-['Inter_Tight',system-ui,sans-serif] text-xs font-semibold leading-4 text-void transition hover:bg-aurora/90 focus:outline-none focus:ring-2 focus:ring-aurora/50"
        >
          Back to board
        </button>
      </div>
    </div>
  )
}

function HeaderBlock({
  endedAtLabel,
  title,
  metadataLabel,
}: {
  endedAtLabel: string
  title: string
  metadataLabel: string
}): JSX.Element {
  return (
    <div className="flex flex-col gap-[18px] px-12 pb-7 pt-8">
      <span className="inline-flex w-fit items-center gap-2 rounded-full border border-aurora/40 bg-aurora/10 px-3 py-1 font-['Inter_Tight',system-ui,sans-serif] text-[11px] font-medium uppercase leading-4 tracking-widest text-aurora">
        <span aria-hidden className="h-1.5 w-1.5 rounded-full bg-aurora" />
        {endedAtLabel}
      </span>
      <h1
        className="font-['Inter_Tight',system-ui,sans-serif] text-[38px] font-bold leading-tight tracking-[-0.025em] text-star"
        data-testid="post-meeting-title"
      >
        {title}
      </h1>
      <p className="font-['JetBrains_Mono',ui-monospace,monospace] text-[12px] leading-4 text-twilight">
        {metadataLabel}
      </p>
    </div>
  )
}

function MetricStrip({ report }: { report: MeetingReport }): JSX.Element {
  return (
    <div className="grid grid-cols-1 gap-4 px-12 pb-8 sm:grid-cols-2 lg:grid-cols-4">
      <MetricTile
        label="Cards moved"
        value={String(report.cardsMoved)}
        subtitle={`${report.cardsMovedByNova} by nova · ${report.cardsMovedByTeam} by team`}
        subtitleClassName="text-aurora"
      />
      <MetricTile
        label="Agent runs kicked off"
        value={String(report.runsKickedOff)}
        valueClassName="text-solar"
        subtitle={`both running now · est. ${report.runsEtaMinutes} min`}
        subtitleClassName="text-solar/90"
        tileClassName="border-solar/60 ring-1 ring-solar/30"
      />
      <MetricTile
        label="Questions resolved"
        value={String(report.questionsResolved)}
        subtitle={report.questionAnswer}
      />
      <MetricTile
        label="Meeting cost"
        value={report.cost}
        valueClassName="font-['JetBrains_Mono',ui-monospace,monospace]"
        subtitle={report.timeSaved}
      />
    </div>
  )
}

function MetricTile({
  label,
  value,
  subtitle,
  valueClassName = "",
  subtitleClassName = "text-twilight",
  tileClassName = "",
}: {
  label: string
  value: string
  subtitle: string
  valueClassName?: string
  subtitleClassName?: string
  tileClassName?: string
}): JSX.Element {
  return (
    <div
      className={`flex flex-col gap-1.5 rounded-lg border border-edge bg-sky px-5 py-[18px] ${tileClassName}`}
    >
      <span className="font-['Inter_Tight',system-ui,sans-serif] text-[10px] font-semibold uppercase tracking-widest text-farstar">
        {label}
      </span>
      <span
        className={`font-['Inter_Tight',system-ui,sans-serif] text-[28px] font-semibold leading-none tracking-tight text-star ${valueClassName}`}
      >
        {value}
      </span>
      <span className={`text-[11px] leading-4 ${subtitleClassName}`}>{subtitle}</span>
    </div>
  )
}

function Body({ report }: { report: MeetingReport }): JSX.Element {
  return (
    <div className="grid grid-cols-1 gap-6 px-12 pb-10 lg:grid-cols-3">
      <div className="flex flex-col gap-6 lg:col-span-2">
        <DecisionsSection decisions={report.decisions} />
        <FollowUpsSection followUps={report.followUps} />
      </div>
      <div className="flex flex-col gap-6">
        <AgentsWorkingPanel
          agents={report.agentsWorking}
          footnote={report.agentsFootnote}
        />
        <SyncedToSection targets={report.syncTargets} />
      </div>
    </div>
  )
}

function SectionHeader({ children }: { children: string }): JSX.Element {
  return (
    <h2 className="font-['Inter_Tight',system-ui,sans-serif] text-[11px] font-semibold uppercase tracking-widest text-farstar">
      {children}
    </h2>
  )
}

function DecisionsSection({ decisions }: { decisions: DecisionRow[] }): JSX.Element {
  return (
    <section className="flex flex-col gap-3" data-testid="post-meeting-decisions">
      <SectionHeader>Decisions</SectionHeader>
      <ul className="flex flex-col gap-2">
        {decisions.map((d) => (
          <li
            key={d.id}
            className="flex items-start gap-3 rounded-lg border border-edge bg-sky px-4 py-[14px]"
          >
            <span aria-hidden className="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full bg-aurora" />
            <div className="flex grow flex-col gap-1">
              <span className="font-['Inter_Tight',system-ui,sans-serif] text-[13px] font-medium leading-5 text-star">
                {d.title}
              </span>
              <span className="font-['Inter_Tight',system-ui,sans-serif] text-[12px] leading-[18px] text-twilight">
                {d.rationale}
              </span>
            </div>
            <span className="shrink-0 font-['JetBrains_Mono',ui-monospace,monospace] text-[11px] leading-4 text-farstar">
              {d.timestamp}
            </span>
          </li>
        ))}
      </ul>
    </section>
  )
}

function FollowUpsSection({ followUps }: { followUps: FollowUpRow[] }): JSX.Element {
  return (
    <section className="flex flex-col gap-3" data-testid="post-meeting-follow-ups">
      <SectionHeader>Follow-ups</SectionHeader>
      <ul className="flex flex-col gap-2">
        {followUps.map((f) => (
          <li
            key={f.id}
            className="flex items-center gap-3 rounded-lg border border-edge bg-sky px-4 py-[14px]"
          >
            <AssigneeAvatar name={f.assignee} />
            <span className="grow font-['Inter_Tight',system-ui,sans-serif] text-[13px] font-medium leading-5 text-star">
              {f.title}
            </span>
            <span className="shrink-0 font-['JetBrains_Mono',ui-monospace,monospace] text-[11px] leading-4 text-twilight">
              {f.jiraId}
            </span>
          </li>
        ))}
      </ul>
    </section>
  )
}

function AssigneeAvatar({ name }: { name: string }): JSX.Element {
  const initials =
    name
      .split(/[\s\-_.@]+/)
      .filter(Boolean)
      .slice(0, 2)
      .map((p) => p[0]?.toUpperCase() ?? "")
      .join("") || "?"
  return (
    <span
      title={name}
      aria-label={name}
      className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-comet/20 text-[10px] font-semibold text-comet"
    >
      {initials}
    </span>
  )
}

function AgentsWorkingPanel({
  agents,
  footnote,
}: {
  agents: AgentWorkingRow[]
  footnote: string
}): JSX.Element {
  return (
    <section
      className="flex flex-col gap-3"
      data-testid="post-meeting-agents-working"
    >
      <SectionHeader>Agents working now</SectionHeader>
      <div className="flex flex-col rounded-lg border border-solar/40 bg-solar/10">
        {agents.map((a, idx) => (
          <div
            key={a.id}
            className={`flex items-center gap-3 px-4 py-3 ${
              idx > 0 ? "border-t border-solar/20" : ""
            }`}
          >
            <span
              aria-hidden
              className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-solar/30 font-['JetBrains_Mono',ui-monospace,monospace] text-[10px] font-semibold text-solar"
            >
              {a.id}
            </span>
            <div className="flex grow flex-col gap-0.5">
              <span className="font-['Inter_Tight',system-ui,sans-serif] text-[13px] font-medium leading-5 text-star">
                {a.name}
              </span>
              <span className="font-['JetBrains_Mono',ui-monospace,monospace] text-[11px] leading-4 text-solar/90">
                {a.cardId} · {a.stepsDone}/{a.stepsTotal} steps · est. {a.etaMinutes} min
              </span>
            </div>
            <span className="shrink-0 font-['JetBrains_Mono',ui-monospace,monospace] text-[11px] leading-4 text-star">
              {a.cost}
            </span>
          </div>
        ))}
        <div className="border-t border-solar/20 px-4 py-3">
          <p className="rounded-md bg-atmos px-3 py-2 font-['Inter_Tight',system-ui,sans-serif] text-[11px] leading-[18px] text-twilight">
            {footnote}
          </p>
        </div>
      </div>
    </section>
  )
}

function SyncedToSection({ targets }: { targets: SyncTarget[] }): JSX.Element {
  return (
    <section className="flex flex-col gap-3" data-testid="post-meeting-sync-targets">
      <SectionHeader>Sync'd to</SectionHeader>
      <ul className="flex flex-col gap-2">
        {targets.map((t) => (
          <li
            key={t.id}
            className="flex items-center gap-3 rounded-md border border-edge bg-sky px-3 py-2"
          >
            <span aria-hidden className="h-1.5 w-1.5 shrink-0 rounded-full bg-aurora" />
            <span className="grow font-['Inter_Tight',system-ui,sans-serif] text-[12px] leading-4 text-star">
              <span className="font-medium">{t.label}</span>
              <span className="text-twilight"> · {t.detail}</span>
            </span>
            <span className="shrink-0 font-['JetBrains_Mono',ui-monospace,monospace] text-[10px] leading-4 text-farstar">
              {t.timestamp}
            </span>
          </li>
        ))}
      </ul>
    </section>
  )
}

function splitBreadcrumb(crumb: string): [string, string] {
  const idx = crumb.indexOf("/")
  if (idx === -1) return ["", crumb.trim()]
  return [crumb.slice(0, idx).trim(), crumb.slice(idx + 1).trim()]
}

export function buildRecapText(report: MeetingReport): string {
  const lines: string[] = []
  lines.push(`${report.title} — ${report.endedAtLabel}`)
  lines.push(report.metadataLabel)
  lines.push("")
  lines.push(
    `Metrics: ${report.cardsMoved} cards moved · ${report.runsKickedOff} agent runs · ${report.questionsResolved} questions resolved · cost ${report.cost} (${report.timeSaved})`,
  )
  if (report.decisions.length > 0) {
    lines.push("")
    lines.push("Decisions:")
    for (const d of report.decisions) {
      lines.push(`  • ${d.title} — ${d.rationale} (${d.timestamp})`)
    }
  }
  if (report.followUps.length > 0) {
    lines.push("")
    lines.push("Follow-ups:")
    for (const f of report.followUps) {
      lines.push(`  • ${f.jiraId} — ${f.title} (${f.assignee})`)
    }
  }
  if (report.agentsWorking.length > 0) {
    lines.push("")
    lines.push("Agents working now:")
    for (const a of report.agentsWorking) {
      lines.push(
        `  • ${a.name} (${a.id}) on ${a.cardId} — ${a.stepsDone}/${a.stepsTotal} steps · est. ${a.etaMinutes} min · ${a.cost}`,
      )
    }
  }
  if (report.syncTargets.length > 0) {
    lines.push("")
    lines.push("Sync'd to:")
    for (const t of report.syncTargets) {
      lines.push(`  • ${t.label}: ${t.detail} (${t.timestamp})`)
    }
  }
  return lines.join("\n")
}
