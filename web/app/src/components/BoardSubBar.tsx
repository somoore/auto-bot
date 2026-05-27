import { useMemo } from "react"
import { PauseAllPill } from "./PauseAllPill"
import type { TenantSettings } from "../types/board"

interface Props {
  agentActive: boolean
  agentLabel?: string
  cardCount: number
  tenantSettings?: TenantSettings
}

export function BoardSubBar({ agentActive, agentLabel, cardCount, tenantSettings }: Props): JSX.Element {
  const days = useMemo(() => buildDayStrip(), [])
  return (
    <div className="border-b border-edge/60 bg-void">
      <div className="mx-auto flex max-w-[1400px] flex-wrap items-center gap-3 px-6 py-3">
        <DayStrip days={days} />
        <div className="hidden h-5 w-px bg-edge md:block" aria-hidden />
        <FilterPills count={cardCount} />
        <div className="ml-auto" />
        <PauseAllPill settings={tenantSettings} />
        <AgentPill active={agentActive} label={agentLabel} />
      </div>
    </div>
  )
}

interface Day { iso: string; label: string; weekday: string; isToday: boolean }

function buildDayStrip(): Day[] {
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  const days: Day[] = []
  for (let offset = -3; offset <= 3; offset++) {
    const d = new Date(today)
    d.setDate(today.getDate() + offset)
    days.push({
      iso: d.toISOString().slice(0, 10),
      label: String(d.getDate()),
      weekday: d.toLocaleDateString(undefined, { weekday: "short" }),
      isToday: offset === 0,
    })
  }
  return days
}

function DayStrip({ days }: { days: Day[] }): JSX.Element {
  return (
    <div className="flex items-center gap-1" role="tablist" aria-label="Day">
      {days.map((day) => (
        <button
          key={day.iso}
          role="tab"
          aria-selected={day.isToday}
          className={
            day.isToday
              ? "flex h-9 w-12 flex-col items-center justify-center rounded-md bg-edge text-star shadow-inner"
              : "flex h-9 w-12 flex-col items-center justify-center rounded-md text-farstar hover:bg-atmos hover:text-twilight"
          }
        >
          <span className="text-[10px] uppercase tracking-widest">{day.weekday}</span>
          <span className="text-sm font-semibold">{day.label}</span>
        </button>
      ))}
    </div>
  )
}

function FilterPills({ count }: { count: number }): JSX.Element {
  return (
    <div className="flex flex-wrap items-center gap-2 text-xs">
      <Pill active>All</Pill>
      <Pill>Mine</Pill>
      <Pill>Agents</Pill>
      <Pill>Blocked</Pill>
      <span className="text-xs text-farstar">{count} cards</span>
    </div>
  )
}

function Pill({ children, active = false }: { children: React.ReactNode; active?: boolean }): JSX.Element {
  const cls = active
    ? "rounded-full border border-comet/40 bg-comet/10 px-3 py-1 text-comet"
    : "rounded-full border border-edge bg-atmos px-3 py-1 text-twilight hover:text-star"
  return <button type="button" className={cls}>{children}</button>
}

function AgentPill({ active, label }: { active: boolean; label?: string }): JSX.Element {
  if (active) {
    return (
      <span className="inline-flex items-center gap-2 rounded-full border border-solar/40 bg-solar/10 px-3 py-1 text-xs font-medium text-solar">
        <span aria-hidden className="h-1.5 w-1.5 animate-pulse rounded-full bg-solar" />
        Agent active{label ? ` · ${label}` : ""}
      </span>
    )
  }
  return (
    <span className="inline-flex items-center gap-2 rounded-full border border-edge bg-atmos px-3 py-1 text-xs text-twilight">
      <span aria-hidden className="h-1.5 w-1.5 rounded-full bg-farstar" />
      Agents idle
    </span>
  )
}
