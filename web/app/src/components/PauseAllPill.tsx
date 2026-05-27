import { useCallback, useState } from "react"
import type { TenantSettings } from "../types/board"

interface Props {
  settings?: TenantSettings
}

// PauseAllPill renders the tenant-wide kill switch. When agents are paused it
// glows Magnetar copper (#FF3D7F via the magnetar token); when off it is a
// quiet outline pill. Clicking flips POST /tenant/settings.
export function PauseAllPill({ settings }: Props): JSX.Element {
  const paused = Boolean(settings?.agents_paused)
  const dryRun = Boolean(settings?.dry_run_enabled)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | undefined>(undefined)

  const toggle = useCallback(async (field: "agents_paused" | "dry_run_enabled", next: boolean): Promise<void> => {
    setBusy(true)
    setError(undefined)
    try {
      const body: Record<string, unknown> = {
        agents_paused: settings?.agents_paused ?? false,
        dry_run_enabled: settings?.dry_run_enabled ?? false,
      }
      body[field] = next
      const res = await fetch("/tenant/settings", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      })
      if (!res.ok) {
        const text = await res.text()
        setError(`update failed: ${text || res.status}`)
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "request failed")
    } finally {
      setBusy(false)
    }
  }, [settings?.agents_paused, settings?.dry_run_enabled])

  return (
    <div className="flex flex-col items-end gap-1">
      <div className="flex items-center gap-2">
        <button
          type="button"
          disabled={busy}
          aria-pressed={dryRun}
          aria-label={dryRun ? "Dry-run is on; click to turn off" : "Dry-run is off; click to turn on"}
          onClick={(): void => { void toggle("dry_run_enabled", !dryRun) }}
          className={
            dryRun
              ? "inline-flex items-center gap-2 rounded-full border border-comet/40 bg-comet/15 px-3 py-1 text-xs font-medium text-comet hover:bg-comet/25 disabled:opacity-50"
              : "inline-flex items-center gap-2 rounded-full border border-edge bg-atmos px-3 py-1 text-xs text-twilight hover:text-star disabled:opacity-50"
          }
        >
          <span aria-hidden className={"h-1.5 w-1.5 rounded-full " + (dryRun ? "bg-comet animate-pulse" : "bg-farstar")} />
          {dryRun ? "Dry-run on" : "Dry-run off"}
        </button>
        <button
          type="button"
          disabled={busy}
          aria-pressed={paused}
          aria-label={paused ? "Agents are paused; click to resume" : "Agents are live; click to pause all agents"}
          onClick={(): void => { void toggle("agents_paused", !paused) }}
          className={
            paused
              ? "inline-flex items-center gap-2 rounded-full border border-magnetar/60 bg-magnetar/20 px-3 py-1 text-xs font-semibold text-magnetar shadow-[0_0_12px_rgba(255,61,127,0.45)] hover:bg-magnetar/30 disabled:opacity-50"
              : "inline-flex items-center gap-2 rounded-full border border-magnetar/30 bg-atmos px-3 py-1 text-xs font-medium text-magnetar hover:bg-magnetar/10 disabled:opacity-50"
          }
        >
          <span aria-hidden className={"h-1.5 w-1.5 rounded-full " + (paused ? "bg-magnetar animate-pulse" : "bg-magnetar/60")} />
          {paused ? "Agents paused" : "Pause all agents"}
        </button>
      </div>
      {error ? <p className="text-[10px] text-magnetar">{error}</p> : null}
    </div>
  )
}
