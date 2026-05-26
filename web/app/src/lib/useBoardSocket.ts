import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import type { AgentRunView, BoardState, RunQuestion } from "../types/board"

export type WsStatus = "connecting" | "open" | "closed"

export interface SessionState {
  ok: boolean
  loading: boolean
  participantIdentity?: string
  roomId?: string
  boardId?: string
  error?: string
}

export interface DispatchResult {
  ok: boolean
  status: number
  body: unknown
  error?: string
}

export interface BoardSocketState {
  status: WsStatus
  board?: BoardState
  lastAgentRun?: AgentRunView
  lastActionResult?: unknown
  openRunQuestions: RunQuestion[]
  // openRunQuestionsByCard is a last-write-wins lookup keyed by card_id, so
  // both the board-row banner (Card.tsx) and the drawer (CardDrawer.tsx)
  // can find the question for a given card in O(1).
  openRunQuestionsByCard: Map<string, RunQuestion>
  // agentRunsByCardId is the per-card view of the most recent agent_run
  // event we received. Driven by the inbound "agent_run" stream and seeded
  // from any agentRuns slice carried on the board snapshot.
  agentRunsByCardId: Map<string, AgentRunView>
  reconnectAttempt: number
  lastError?: string
  session: SessionState
}

interface OuterEnvelope { event: string; data: string }
interface InnerEnvelope { event: string; data: unknown }

const BACKOFF_MS = [1000, 2000, 4000, 8000, 16000, 30000]

function backoffDelay(attempt: number): number {
  return BACKOFF_MS[Math.min(attempt, BACKOFF_MS.length - 1)]
}

function wsURL(): string {
  const scheme = window.location.protocol === "https:" ? "wss" : "ws"
  return `${scheme}://${window.location.host}/websocket`
}

export interface BoardSocketAPI {
  state: BoardSocketState
  // send is the legacy WebSocket-frame sender. It returns false when the
  // socket is not open. Existing call sites (none today) keep working.
  send: (event: string, data: unknown) => boolean
  // dispatch posts a tool call through HTTP to /internal/tools/dispatch.
  // Returns a structured result with `ok` + error text instead of throwing
  // so call sites can render the error inline without try/catch boilerplate.
  dispatch: (tool: string, args: Record<string, unknown>) => Promise<DispatchResult>
}

export function useBoardSocket(): BoardSocketAPI {
  const [status, setStatus] = useState<WsStatus>("connecting")
  const [board, setBoard] = useState<BoardState | undefined>(undefined)
  const [openRunQuestions, setOpenRunQuestions] = useState<RunQuestion[]>([])
  const [agentRunsByCard, setAgentRunsByCard] = useState<Map<string, AgentRunView>>(() => new Map())
  const [lastAgentRun, setLastAgentRun] = useState<AgentRunView | undefined>(undefined)
  const [lastActionResult, setLastActionResult] = useState<unknown>(undefined)
  const [reconnectAttempt, setReconnectAttempt] = useState(0)
  const [lastError, setLastError] = useState<string | undefined>(undefined)
  const [session, setSession] = useState<SessionState>({ ok: false, loading: true })

  const socketRef = useRef<WebSocket | null>(null)
  const attemptRef = useRef(0)
  const retryTimerRef = useRef<number | undefined>(undefined)
  const closedByCallerRef = useRef(false)

  useEffect(() => {
    let cancelled = false
    const probe = async (): Promise<void> => {
      try {
        const res = await fetch("/auth/session", { method: "GET", credentials: "include" })
        if (cancelled) return
        if (!res.ok) {
          setSession({ ok: false, loading: false, error: "auth " + res.status })
          return
        }
        const body = (await res.json()) as {
          ok?: boolean
          participant_identity?: string
          room_id?: string
          board_id?: string
        }
        setSession({
          ok: Boolean(body.ok),
          loading: false,
          participantIdentity: body.participant_identity,
          roomId: body.room_id,
          boardId: body.board_id,
        })
      } catch (err) {
        if (cancelled) return
        setSession({
          ok: false,
          loading: false,
          error: err instanceof Error ? err.message : "auth probe failed",
        })
      }
    }
    void probe()
    return (): void => { cancelled = true }
  }, [])

  const handleInnerEvent = useCallback((inner: InnerEnvelope) => {
    switch (inner.event) {
      case "board": {
        const next = inner.data as BoardState
        setBoard(next)
        if (Array.isArray(next.open_run_questions)) {
          setOpenRunQuestions(next.open_run_questions)
        }
        if (Array.isArray(next.agentRuns)) {
          setAgentRunsByCard(() => {
            const map = new Map<string, AgentRunView>()
            for (const run of next.agentRuns ?? []) {
              if (run.card_id) map.set(run.card_id, run)
            }
            return map
          })
        }
        break
      }
      case "run_question_asked": {
        const q = inner.data as RunQuestion
        setOpenRunQuestions((prev) => {
          const filtered = prev.filter((p) => p.id !== q.id)
          return [...filtered, q]
        })
        break
      }
      case "run_question_answered":
      case "run_question_expired": {
        const q = inner.data as RunQuestion
        setOpenRunQuestions((prev) => prev.filter((p) => p.id !== q.id))
        break
      }
      case "agent_run": {
        const run = inner.data as AgentRunView
        setLastAgentRun(run)
        if (run.card_id) {
          setAgentRunsByCard((prev) => {
            const next = new Map(prev)
            next.set(run.card_id, run)
            return next
          })
        }
        break
      }
      case "action_result": {
        setLastActionResult(inner.data)
        break
      }
      case "status":
        break
      default:
        break
    }
  }, [])

  const handleMessage = useCallback((raw: string) => {
    let outer: OuterEnvelope
    try {
      outer = JSON.parse(raw) as OuterEnvelope
    } catch (err) {
      setLastError("outer parse: " + (err instanceof Error ? err.message : "?"))
      return
    }
    // Server wraps every board event in an outer "kanban" envelope whose
    // Data is a JSON-encoded {event, data} string. See cmd/server/board.go
    // sendKanbanEvent / defaultBroadcastSink.
    if (outer.event !== "kanban") return
    let inner: InnerEnvelope
    try {
      inner = JSON.parse(outer.data) as InnerEnvelope
    } catch (err) {
      setLastError("inner parse: " + (err instanceof Error ? err.message : "?"))
      return
    }
    handleInnerEvent(inner)
  }, [handleInnerEvent])

  const connect = useCallback(() => {
    if (closedByCallerRef.current) return
    setStatus("connecting")
    let ws: WebSocket
    try {
      ws = new WebSocket(wsURL())
    } catch (err) {
      setLastError(err instanceof Error ? err.message : "ws ctor failed")
      setStatus("closed")
      scheduleReconnect()
      return
    }
    socketRef.current = ws
    ws.onopen = (): void => {
      attemptRef.current = 0
      setReconnectAttempt(0)
      setStatus("open")
      setLastError(undefined)
    }
    ws.onmessage = (ev): void => {
      if (typeof ev.data === "string") handleMessage(ev.data)
    }
    ws.onerror = (): void => { setLastError("socket error") }
    ws.onclose = (ev): void => {
      socketRef.current = null
      setStatus("closed")
      if (closedByCallerRef.current) return
      if (ev.code === 1008 || ev.code === 4401) {
        setSession((prev) => ({ ...prev, ok: false, loading: false }))
      }
      scheduleReconnect()
    }
    function scheduleReconnect(): void {
      const next = attemptRef.current + 1
      attemptRef.current = next
      setReconnectAttempt(next)
      const delay = backoffDelay(next - 1)
      window.clearTimeout(retryTimerRef.current)
      retryTimerRef.current = window.setTimeout(() => { connect() }, delay)
    }
  }, [handleMessage])

  useEffect(() => {
    if (session.loading) return
    if (!session.ok) { setStatus("closed"); return }
    closedByCallerRef.current = false
    connect()
    return (): void => {
      closedByCallerRef.current = true
      window.clearTimeout(retryTimerRef.current)
      const sock = socketRef.current
      socketRef.current = null
      if (sock && sock.readyState <= WebSocket.OPEN) sock.close()
    }
  }, [connect, session.loading, session.ok])

  const send = useCallback((event: string, data: unknown): boolean => {
    const sock = socketRef.current
    if (!sock || sock.readyState !== WebSocket.OPEN) return false
    const payload = JSON.stringify({ event, data: JSON.stringify(data) })
    sock.send(payload)
    return true
  }, [])

  // dispatch posts a tool call to /internal/tools/dispatch. Auth flows
  // through the existing session cookie (credentials: include).
  const dispatch = useCallback(
    async (tool: string, args: Record<string, unknown>): Promise<DispatchResult> => {
      try {
        const res = await fetch("/internal/tools/dispatch", {
          method: "POST",
          credentials: "include",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ tool, args, dispatcher: "ui" }),
        })
        let body: unknown
        try {
          body = await res.json()
        } catch {
          body = undefined
        }
        if (!res.ok) {
          const err =
            (body && typeof body === "object" && "error" in body
              ? String((body as { error?: unknown }).error)
              : undefined) || `HTTP ${res.status}`
          return { ok: false, status: res.status, body, error: err }
        }
        return { ok: true, status: res.status, body }
      } catch (err) {
        return {
          ok: false,
          status: 0,
          body: undefined,
          error: err instanceof Error ? err.message : "network error",
        }
      }
    },
    [],
  )

  // openRunQuestionsByCard derives a card_id → question lookup so the
  // drawer and the board-row banner can both find a question in O(1).
  // Last-write-wins: later entries in the openRunQuestions array overwrite
  // earlier ones, which matches the server's expectation that only one
  // question per card is open at a time.
  const openRunQuestionsByCard = useMemo<Map<string, RunQuestion>>(() => {
    const map = new Map<string, RunQuestion>()
    for (const q of openRunQuestions) {
      if (q.status !== "open") continue
      map.set(q.card_id, q)
    }
    return map
  }, [openRunQuestions])

  const state = useMemo<BoardSocketState>(
    () => ({
      status,
      board,
      openRunQuestions,
      openRunQuestionsByCard,
      agentRunsByCardId: agentRunsByCard,
      lastAgentRun,
      lastActionResult,
      reconnectAttempt,
      lastError,
      session,
    }),
    [
      status,
      board,
      openRunQuestions,
      openRunQuestionsByCard,
      agentRunsByCard,
      lastAgentRun,
      lastActionResult,
      reconnectAttempt,
      lastError,
      session,
    ],
  )

  return { state, send, dispatch }
}
