import { useEffect, useMemo, useState } from "react"
import type { AgentRunView, Card as CardModel, RunQuestion } from "../types/board"
import { CardThreadTab } from "./CardThreadTab"
import { CardRunTab, type DispatchFn } from "./CardRunTab"

export type DrawerTab = "thread" | "run" | "history"

interface Props {
  card: CardModel
  question?: RunQuestion
  run?: AgentRunView
  onClose: () => void
  // dispatch is the POST /internal/tools/dispatch helper exposed by
  // useBoardSocket; passed through so the Run tab can answer/take-over/cancel.
  dispatch: DispatchFn
  // currentUserId is used as `answered_by` when the user submits answers
  // and for "Take over" attribution.
  currentUserId?: string
}

export function CardDrawer({
  card,
  question,
  run,
  onClose,
  dispatch,
  currentUserId,
}: Props): JSX.Element {
  const hasRun = Boolean(run) || Boolean(question)
  const [tab, setTab] = useState<DrawerTab>(hasRun ? "run" : "thread")

  // ESC closes the drawer. Bound at document level so the drawer can be
  // dismissed regardless of which element has focus.
  useEffect(() => {
    const onKey = (e: KeyboardEvent): void => {
      if (e.key === "Escape") {
        e.stopPropagation()
        onClose()
      }
    }
    document.addEventListener("keydown", onKey)
    return (): void => document.removeEventListener("keydown", onKey)
  }, [onClose])

  // If the run becomes available after open, switch to Run tab once.
  const initialHadRun = useMemo(() => hasRun, []) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (!initialHadRun && hasRun) setTab("run")
  }, [hasRun, initialHadRun])

  return (
    <div
      className="fixed inset-0 z-40"
      role="dialog"
      aria-modal="true"
      aria-label={`Card ${card.id}: ${card.title}`}
    >
      <button
        type="button"
        aria-label="Close drawer"
        onClick={onClose}
        className="absolute inset-0 cursor-default bg-void/60 backdrop-blur-sm"
      />
      <aside
        className="absolute inset-y-0 right-0 flex w-full flex-col border-l border-edge bg-sky shadow-2xl sm:w-[480px]"
        data-testid="card-drawer"
      >
        <DrawerHeader card={card} onClose={onClose} />
        <Tabs tab={tab} setTab={setTab} hasRun={hasRun} />
        <div className="flex-1 overflow-y-auto">
          {tab === "thread" ? <CardThreadTab card={card} /> : null}
          {tab === "run" ? (
            <CardRunTab
              card={card}
              question={question}
              run={run}
              dispatch={dispatch}
              currentUserId={currentUserId}
            />
          ) : null}
          {tab === "history" ? <HistoryTab card={card} /> : null}
        </div>
      </aside>
    </div>
  )
}

function DrawerHeader({
  card,
  onClose,
}: {
  card: CardModel
  onClose: () => void
}): JSX.Element {
  const assignee = card.assignee
  const display = assignee?.displayName || assignee?.id
  return (
    <header className="flex items-start gap-3 border-b border-edge px-4 py-3">
      <button
        type="button"
        onClick={onClose}
        aria-label="Back to board"
        className="mt-0.5 inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-md border border-edge text-twilight hover:border-edge/80 hover:text-star"
      >
        <span aria-hidden>‹</span>
      </button>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2 text-[10px] uppercase tracking-wider text-farstar">
          <span>{card.issueType?.toUpperCase() || "TASK"}</span>
          <span className="font-mono text-twilight">{card.id}</span>
          {card.priority ? (
            <span className="rounded bg-edge px-1.5 py-0.5 text-[9px] text-twilight">
              {card.priority}
            </span>
          ) : null}
        </div>
        <h2 className="mt-0.5 break-words text-base font-medium leading-snug text-star">
          {card.title}
        </h2>
        {display ? (
          <div className="mt-1.5 inline-flex items-center gap-1.5 rounded-full border border-edge/80 bg-atmos px-2 py-0.5 text-[11px] text-twilight">
            <span
              className={
                assignee?.kind === "agent"
                  ? "h-1.5 w-1.5 rounded-full bg-solar"
                  : "h-1.5 w-1.5 rounded-full bg-comet"
              }
              aria-hidden
            />
            {display}
          </div>
        ) : null}
      </div>
    </header>
  )
}

function Tabs({
  tab,
  setTab,
  hasRun,
}: {
  tab: DrawerTab
  setTab: (t: DrawerTab) => void
  hasRun: boolean
}): JSX.Element {
  const tabs: { id: DrawerTab; label: string; disabled?: boolean }[] = [
    { id: "thread", label: "Thread" },
    { id: "run", label: "Run", disabled: !hasRun },
    { id: "history", label: "History" },
  ]
  return (
    <nav
      role="tablist"
      aria-label="Card sections"
      className="flex gap-1 border-b border-edge px-3 pt-1"
    >
      {tabs.map((t) => {
        const active = tab === t.id
        return (
          <button
            key={t.id}
            type="button"
            role="tab"
            aria-selected={active}
            aria-controls={`drawer-panel-${t.id}`}
            disabled={t.disabled}
            onClick={(): void => setTab(t.id)}
            className={
              active
                ? "border-b-2 border-aurora px-3 py-2 text-xs font-semibold uppercase tracking-wider text-star"
                : t.disabled
                  ? "px-3 py-2 text-xs font-semibold uppercase tracking-wider text-farstar/40"
                  : "px-3 py-2 text-xs font-semibold uppercase tracking-wider text-twilight hover:text-star"
            }
          >
            {t.label}
          </button>
        )
      })}
    </nav>
  )
}

function HistoryTab({ card }: { card: CardModel }): JSX.Element {
  // F1.2 backend will land a real activity log endpoint
  // (GET /api/cards/{id}/history). For this slice we list checkpoints from
  // any agent run that ran against this card if present in the snapshot,
  // and fall back to a placeholder.
  const linkCount = card.issueLinks?.length ?? 0
  return (
    <section
      id="drawer-panel-history"
      role="tabpanel"
      className="space-y-3 px-4 py-4 text-sm text-twilight"
    >
      <p className="text-xs uppercase tracking-wider text-farstar">History</p>
      <p>
        Activity log lands in F1.2. For now this tab shows linked issues and
        the card&apos;s sprint.
      </p>
      <ul className="space-y-1 text-xs">
        {card.sprint?.name ? <li>Sprint: {card.sprint.name}</li> : null}
        <li>Linked issues: {linkCount}</li>
        {card.reporter?.displayName ? (
          <li>Reporter: {card.reporter.displayName}</li>
        ) : null}
      </ul>
    </section>
  )
}
