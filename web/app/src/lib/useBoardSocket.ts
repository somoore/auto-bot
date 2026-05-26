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

export interface BoardSocketState {
  status: WsStatus
  board?: BoardState
  lastAgentRun?: AgentRunView
  lastActionResult?: unknown
  openRunQuestions: RunQuestion[]
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
  send: (event: string, data: unknown) => boolean
}

export function useBoardSocket(): BoardSocketAPI {
  const [status, setStatus] = useState<WsStatus>("connecting")
  const [board, setBoard] = useState<BoardState | undefined>(undefined)
  const [openRunQuestions, setOpenRunQuestions] = useState<RunQuestion[]>([])
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
        setLastAgentRun(inner.data as AgentRunView)
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

  const state = useMemo<BoardSocketState>(() => ({
    status, board, openRunQuestions, lastAgentRun, lastActionResult,
    reconnectAttempt, lastError, session,
  }), [status, board, openRunQuestions, lastAgentRun, lastActionResult, reconnectAttempt, lastError, session])

  return { state, send }
}
