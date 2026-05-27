import { useState } from "react"

interface Props { error?: string }

// SignInGate is the fallback when /auth/session refuses to mint a local
// browser session. In practice the GET endpoint mints one for localhost
// requests, so this UI is rarely seen. When it IS seen, we send the user
// to root, where the legacy host-token sign-in flow lives. (See
// cmd/server/auth.go localLoginHandler — GET-only with query-string token.)
export function SignInGate({ error }: Props): JSX.Element {
  const [retrying, setRetrying] = useState(false)
  const retry = async (): Promise<void> => {
    setRetrying(true)
    try {
      const res = await fetch("/auth/session", { method: "GET", credentials: "include" })
      if (res.ok) { window.location.reload(); return }
    } catch { /* fall through */ }
    setRetrying(false)
  }
  return (
    <main className="flex min-h-full items-center justify-center px-6 py-12">
      <div className="w-full max-w-md rounded-xl border border-edge/60 bg-sky/60 p-6 shadow-xl">
        <h2 className="text-lg font-semibold text-star">Sign in to Observatory</h2>
        <p className="mt-2 text-sm text-twilight">
          Your browser session could not be established. The server may not be configured to
          mint local browser sessions, or the existing session expired.
        </p>
        {error ? <p className="mt-3 text-xs text-magnetar">{error}</p> : null}
        <div className="mt-5 flex flex-col gap-3">
          <button
            type="button"
            onClick={() => void retry()}
            disabled={retrying}
            className="inline-flex w-full items-center justify-center rounded-md bg-aurora px-3 py-2 text-sm font-semibold text-void shadow-sm transition hover:bg-aurora/90 focus:outline-none focus:ring-2 focus:ring-aurora/50 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {retrying ? "Retrying…" : "Retry sign-in"}
          </button>
          <a href="/" className="inline-flex w-full items-center justify-center rounded-md border border-edge bg-atmos px-3 py-2 text-sm font-medium text-twilight hover:text-star">
            Open the legacy room
          </a>
        </div>
      </div>
    </main>
  )
}
