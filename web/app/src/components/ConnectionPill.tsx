import type { WsStatus } from "../lib/useBoardSocket"

interface Props { status: WsStatus; attempt: number }

export function ConnectionPill({ status, attempt }: Props): JSX.Element {
  if (status === "open") {
    return (
      <span className="inline-flex items-center gap-2 rounded-full border border-aurora/30 bg-aurora/10 px-3 py-1 text-xs font-medium text-aurora">
        <span aria-hidden className="h-1.5 w-1.5 rounded-full bg-aurora shadow-[0_0_8px_rgba(60,223,177,0.7)]" />
        Live
      </span>
    )
  }
  if (status === "connecting") {
    return (
      <span className="inline-flex items-center gap-2 rounded-full border border-edge bg-atmos px-3 py-1 text-xs font-medium text-twilight">
        <span aria-hidden className="h-1.5 w-1.5 animate-pulse rounded-full bg-twilight" />
        Connecting…
      </span>
    )
  }
  return (
    <span className="inline-flex items-center gap-2 rounded-full border border-magnetar/40 bg-magnetar/10 px-3 py-1 text-xs font-medium text-magnetar">
      <span aria-hidden className="h-1.5 w-1.5 animate-pulse rounded-full bg-magnetar" />
      Reconnecting{attempt > 0 ? ` (#${attempt})` : "…"}
    </span>
  )
}
