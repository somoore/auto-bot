import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'

type WsState = 'connecting' | 'open' | 'closed'

interface HealthResponse {
  status?: string
  [key: string]: unknown
}

async function fetchHealth(): Promise<HealthResponse> {
  const res = await fetch('/healthz')
  if (!res.ok) {
    throw new Error(`healthz HTTP ${res.status}`)
  }
  const contentType = res.headers.get('content-type') ?? ''
  if (contentType.includes('application/json')) {
    return (await res.json()) as HealthResponse
  }
  const text = (await res.text()).trim()
  return { status: text || 'ok' }
}

function useWebSocketStatus(): WsState {
  const [state, setState] = useState<WsState>('connecting')

  useEffect(() => {
    const scheme = window.location.protocol === 'https:' ? 'wss' : 'ws'
    const url = `${scheme}://${window.location.host}/websocket`
    let ws: WebSocket | null = null
    try {
      ws = new WebSocket(url)
    } catch {
      setState('closed')
      return
    }
    ws.onopen = () => setState('open')
    ws.onclose = () => setState('closed')
    ws.onerror = () => setState('closed')
    return () => {
      ws?.close()
    }
  }, [])

  return state
}

const COLUMNS = ['Backlog', 'In Progress', 'Done'] as const

function App() {
  const wsState = useWebSocketStatus()
  const health = useQuery({
    queryKey: ['healthz'],
    queryFn: fetchHealth,
    refetchInterval: 15_000,
  })

  const serverStatus = health.isPending
    ? 'checking…'
    : health.isError
      ? 'unreachable'
      : (health.data?.status ?? 'ok')

  return (
    <div className="min-h-screen bg-slate-50 text-slate-900">
      <header className="border-b border-slate-200 bg-white">
        <div className="mx-auto flex max-w-6xl flex-col gap-2 px-6 py-4 sm:flex-row sm:items-center sm:justify-between">
          <h1 className="text-xl font-semibold tracking-tight">
            Auto-Bot v2 — Board (skeleton)
          </h1>
          <div className="flex flex-wrap gap-4 text-sm">
            <span className="inline-flex items-center gap-2">
              <span
                aria-hidden
                className={`h-2 w-2 rounded-full ${
                  health.isError
                    ? 'bg-red-500'
                    : health.isPending
                      ? 'bg-amber-500'
                      : 'bg-emerald-500'
                }`}
              />
              <span className="text-slate-600">
                Server: <span className="font-medium">{serverStatus}</span>
              </span>
            </span>
            <span className="inline-flex items-center gap-2">
              <span
                aria-hidden
                className={`h-2 w-2 rounded-full ${
                  wsState === 'open'
                    ? 'bg-emerald-500'
                    : wsState === 'connecting'
                      ? 'bg-amber-500'
                      : 'bg-red-500'
                }`}
              />
              <span className="text-slate-600">
                WebSocket: <span className="font-medium">{wsState}</span>
              </span>
            </span>
          </div>
        </div>
      </header>

      <main className="mx-auto max-w-6xl px-6 py-8">
        <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
          {COLUMNS.map((name) => (
            <section
              key={name}
              className="flex min-h-[60vh] flex-col rounded-lg border border-slate-200 bg-white shadow-sm"
            >
              <header className="border-b border-slate-200 px-4 py-3">
                <h2 className="text-sm font-semibold uppercase tracking-wide text-slate-500">
                  {name}
                </h2>
              </header>
              <div className="flex-1 px-4 py-6 text-sm text-slate-400">
                No items yet.
              </div>
            </section>
          ))}
        </div>
      </main>
    </div>
  )
}

export default App
