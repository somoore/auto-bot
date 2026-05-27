import type { SessionState, WsStatus } from "../lib/useBoardSocket"
import { ConnectionPill } from "./ConnectionPill"

interface Props {
  status: WsStatus
  session: SessionState
  reconnectAttempt: number
  agentActive: boolean
  agentLabel?: string
  onStartStandup?: () => void
}

export function BoardHeader({
  status,
  session,
  reconnectAttempt,
  agentActive,
  agentLabel,
  onStartStandup,
}: Props): JSX.Element {
  const presenceLabel = session.participantIdentity ?? "you"
  return (
    <header className="border-b border-edge/60 bg-sky/80 backdrop-blur">
      <div className="mx-auto flex max-w-[1400px] flex-col gap-3 px-6 py-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-3">
          <BrandMark />
          <nav aria-label="breadcrumb" className="hidden items-center gap-2 text-sm text-twilight sm:flex">
            <span className="text-farstar">Auto-Bot</span>
            <span aria-hidden className="text-farstar">/</span>
            <span className="font-medium text-star">Board</span>
            {session.boardId ? (
              <>
                <span aria-hidden className="text-farstar">/</span>
                <span className="font-mono text-xs text-twilight">{session.boardId}</span>
              </>
            ) : null}
          </nav>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <Presence label={presenceLabel} />
          <AgentPill active={agentActive} label={agentLabel} />
          <ConnectionPill status={status} attempt={reconnectAttempt} />
          <button
            type="button"
            onClick={onStartStandup}
            className="inline-flex items-center gap-2 rounded-md bg-aurora px-3 py-1.5 text-sm font-semibold text-void shadow-sm transition hover:bg-aurora/90 focus:outline-none focus:ring-2 focus:ring-aurora/50"
          >
            <span aria-hidden>▶</span>
            Start standup
          </button>
        </div>
      </div>
    </header>
  )
}

function BrandMark(): JSX.Element {
  return (
    <div className="flex items-center gap-2">
      <span aria-hidden className="inline-flex h-7 w-7 items-center justify-center rounded-md bg-gradient-to-br from-comet via-aurora to-solar text-void">
        <svg viewBox="0 0 16 16" fill="none" className="h-4 w-4" xmlns="http://www.w3.org/2000/svg">
          <circle cx="8" cy="8" r="2.5" fill="currentColor" />
          <circle cx="8" cy="8" r="6" stroke="currentColor" strokeWidth="1.25" />
        </svg>
      </span>
      <span className="text-base font-semibold tracking-tight text-star">Observatory</span>
    </div>
  )
}

function AgentPill({ active, label }: { active: boolean; label?: string }): JSX.Element {
  if (active) {
    return (
      <span className="inline-flex items-center gap-2 rounded-full border border-edge bg-atmos px-3 py-1 text-xs text-star">
        <span aria-hidden className="h-1.5 w-1.5 animate-pulse rounded-full bg-aurora" />
        Agent active{label ? <span className="font-mono text-twilight"> · {label}</span> : null}
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

function Presence({ label }: { label: string }): JSX.Element {
  const initials = label.split(/[\s\-_.@]+/).filter(Boolean).slice(0, 2)
    .map((part) => part[0]?.toUpperCase() ?? "").join("") || "?"
  return (
    <span className="inline-flex h-7 items-center gap-2 rounded-full border border-edge bg-atmos pl-1 pr-3 text-xs text-twilight" title={label}>
      <span aria-hidden className="inline-flex h-5 w-5 items-center justify-center rounded-full bg-comet/20 text-[10px] font-semibold text-comet">
        {initials}
      </span>
      <span className="hidden font-medium text-star sm:inline">{label}</span>
    </span>
  )
}
