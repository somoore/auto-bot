import { useEffect } from "react"

// Agenda mirrors the shape of internal/standup.Agenda (Sprint 4 will land
// the Go struct + /internal/standup/agenda endpoint). Until then this
// type is the canonical UI contract; the backend will adopt it.
export interface AgendaParticipant {
  id: string
  name: string
  initials?: string
  kind?: "human" | "agent"
}

export interface AgendaHighlight {
  // kind drives the dot color and label. "shipped" = aurora, "run_done" =
  // solar. Anything else falls back to neutral.
  kind: "shipped" | "run_done" | string
  title: string
  attribution?: string // e.g. "SM · 14h"
}

export interface AgendaBlocker {
  cardId: string
  question: string
  ownerLabel?: string // e.g. "swe-1 needs an answer · 3h left"
}

export interface AgendaReview {
  cardId: string
  title: string
  prNumber?: string // e.g. "PR #142"
  reviewLabel?: string // e.g. "ready for your review"
}

export interface AgendaSpeaker {
  participant: AgendaParticipant
  action?: string // "— answer ABV2-088, review ABV2-093"
  estimate?: string // "~3 min"
}

export interface Agenda {
  scheduledFor?: string // "Tuesday, May 26 · 9:00 AM"
  estimate?: string // "est. 8 minutes"
  participantSummary?: string // "3 participants joining"
  preparedAt?: string // "2m ago"
  highlights?: AgendaHighlight[]
  blockers?: AgendaBlocker[]
  reviews?: AgendaReview[]
  speakerOrder?: AgendaSpeaker[]
}

interface Props {
  agenda: Agenda
  onStart: () => void
  onSkip: () => void
  onClose: () => void
}

export function AgendaOverlay({
  agenda,
  onStart,
  onSkip,
  onClose,
}: Props): JSX.Element {
  // ESC closes without firing onStart, matching CardDrawer behavior.
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

  const highlights = agenda.highlights ?? []
  const blockers = agenda.blockers ?? []
  const reviews = agenda.reviews ?? []
  const speakers = agenda.speakerOrder ?? []
  const awaitingCount = blockers.length + reviews.length

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center"
      role="dialog"
      aria-modal="true"
      aria-label="Standup agenda"
    >
      <button
        type="button"
        aria-label="Close agenda"
        onClick={onClose}
        className="absolute inset-0 cursor-default bg-void/60 backdrop-blur-sm"
      />
      <section
        data-testid="agenda-overlay"
        className="relative flex max-h-[92vh] w-[720px] flex-col overflow-y-auto rounded-xl border border-edge bg-sky antialiased shadow-2xl"
      >
        <Header
          preparedAt={agenda.preparedAt ?? "just now"}
          scheduledFor={agenda.scheduledFor}
          estimate={agenda.estimate}
          participantSummary={agenda.participantSummary}
          onClose={onClose}
        />
        <div className="flex flex-col gap-6 px-8 py-6">
          <SinceYesterday highlights={highlights} />
          <AwaitingYou
            blockers={blockers}
            reviews={reviews}
            count={awaitingCount}
          />
          <SpeakerOrder speakers={speakers} />
        </div>
        <Footer onSkip={onSkip} onStart={onStart} onClose={onClose} />
      </section>
    </div>
  )
}

function Header({
  preparedAt,
  scheduledFor,
  estimate,
  participantSummary,
  onClose,
}: {
  preparedAt: string
  scheduledFor?: string
  estimate?: string
  participantSummary?: string
  onClose: () => void
}): JSX.Element {
  const meta = [scheduledFor, estimate, participantSummary].filter(
    (s): s is string => Boolean(s && s.length > 0),
  )
  return (
    <header className="flex flex-col gap-3 border-b border-edge px-8 pb-5 pt-7">
      <div className="flex items-center gap-3">
        <span className="inline-flex items-center gap-2 rounded-[11px] border border-solar/35 bg-solar/10 py-1 pl-1.5 pr-2.5">
          <span
            aria-hidden
            className="inline-flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded-[7px] bg-solar"
          >
            <span className="block h-1.5 w-[3px] bg-void" />
          </span>
          <span className="font-medium tracking-[0.06em] text-[11px] leading-3.5 text-solar">
            nova prepared this · {preparedAt}
          </span>
        </span>
        <span className="grow" />
        <button
          type="button"
          aria-label="Close agenda"
          onClick={onClose}
          className="inline-flex h-5.5 w-5.5 items-center justify-center text-twilight hover:text-star"
        >
          <span aria-hidden className="text-base leading-none">×</span>
        </button>
      </div>
      <h2 className="text-[28px] font-bold leading-8 tracking-[-0.02em] text-star">
        Standup agenda
      </h2>
      {meta.length > 0 ? (
        <div className="flex flex-wrap items-center gap-2.5">
          {meta.map((label, i) => (
            <span key={label} className="flex items-center gap-2.5">
              {i > 0 ? (
                <span
                  aria-hidden
                  className="inline-block h-[3px] w-[3px] rounded-[1.5px] bg-farstar"
                />
              ) : null}
              <span className="text-[13px] leading-4 text-twilight">
                {label}
              </span>
            </span>
          ))}
        </div>
      ) : null}
    </header>
  )
}

function SectionHeader({
  label,
  subscript,
}: {
  label: string
  subscript?: string
}): JSX.Element {
  return (
    <div className="flex items-baseline gap-2.5">
      <h3 className="font-semibold uppercase tracking-[0.12em] text-[11px] leading-3.5 text-star">
        {label}
      </h3>
      {subscript ? (
        <span className="font-['JetBrains_Mono',system-ui,sans-serif] text-[10px] leading-3 text-farstar">
          {subscript}
        </span>
      ) : null}
    </div>
  )
}

function SinceYesterday({
  highlights,
}: {
  highlights: AgendaHighlight[]
}): JSX.Element {
  return (
    <section className="flex flex-col gap-3.5">
      <SectionHeader
        label="Since yesterday"
        subscript={`${highlights.length} event${highlights.length === 1 ? "" : "s"}`}
      />
      <ul className="flex flex-col gap-2">
        {highlights.length === 0 ? (
          <li className="rounded-md border border-edge bg-atmos px-3.5 py-2.5 text-[13px] leading-4 text-twilight">
            No events since yesterday.
          </li>
        ) : (
          highlights.map((h, idx) => <HighlightRow key={idx} highlight={h} />)
        )}
      </ul>
    </section>
  )
}

function HighlightRow({ highlight }: { highlight: AgendaHighlight }): JSX.Element {
  const tone = highlightTone(highlight.kind)
  return (
    <li className="flex items-center gap-2.5 rounded-md border border-edge bg-atmos px-3.5 py-2.5">
      <span
        aria-hidden
        className={`size-1.5 shrink-0 rounded-[3px] ${tone.dot}`}
      />
      <span
        className={`font-['JetBrains_Mono',system-ui,sans-serif] font-medium text-[10px] leading-3 ${tone.label}`}
      >
        {tone.text}
      </span>
      <span className="grow basis-0 text-[13px] leading-4 text-star">
        {highlight.title}
      </span>
      {highlight.attribution ? (
        <span className="font-['JetBrains_Mono',system-ui,sans-serif] text-[10px] leading-3 text-twilight">
          {highlight.attribution}
        </span>
      ) : null}
    </li>
  )
}

function highlightTone(kind: string): {
  dot: string
  label: string
  text: string
} {
  switch (kind) {
    case "shipped":
      return { dot: "bg-aurora", label: "text-aurora", text: "SHIPPED" }
    case "run_done":
      return { dot: "bg-solar", label: "text-solar", text: "RUN DONE" }
    default:
      return {
        dot: "bg-twilight",
        label: "text-twilight",
        text: kind.toUpperCase(),
      }
  }
}

function AwaitingYou({
  blockers,
  reviews,
  count,
}: {
  blockers: AgendaBlocker[]
  reviews: AgendaReview[]
  count: number
}): JSX.Element {
  return (
    <section className="flex flex-col gap-3.5">
      <SectionHeader
        label="Awaiting you"
        subscript={`${count} item${count === 1 ? "" : "s"}`}
      />
      <ul className="flex flex-col gap-2">
        {count === 0 ? (
          <li className="rounded-md border border-edge bg-atmos px-3.5 py-3 text-[13px] leading-4 text-twilight">
            Nothing waiting on you. Nice.
          </li>
        ) : (
          <>
            {blockers.map((b) => (
              <BlockerCard key={b.cardId} blocker={b} />
            ))}
            {reviews.map((r) => (
              <ReviewCard key={r.cardId} review={r} />
            ))}
          </>
        )}
      </ul>
    </section>
  )
}

function BlockerCard({ blocker }: { blocker: AgendaBlocker }): JSX.Element {
  const label = blocker.ownerLabel
    ? `${blocker.cardId} · ${blocker.ownerLabel}`
    : blocker.cardId
  return (
    <li className="flex flex-col gap-1.5 rounded-md border border-solar/30 bg-solar/5 px-3.5 py-3">
      <div className="flex items-center gap-2">
        <span
          aria-hidden
          className="inline-flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded-[7px] bg-solar"
        >
          <span className="font-['JetBrains_Mono',system-ui,sans-serif] text-[8px] font-bold leading-[10px] text-void">
            ?
          </span>
        </span>
        <span className="font-['JetBrains_Mono',system-ui,sans-serif] font-medium tracking-[0.05em] text-[10px] leading-3 text-solar">
          {label}
        </span>
        <span className="grow" />
      </div>
      <p className="text-[13px] font-medium leading-[18px] text-star">
        {blocker.question}
      </p>
    </li>
  )
}

function ReviewCard({ review }: { review: AgendaReview }): JSX.Element {
  const left = review.reviewLabel
    ? `${review.cardId} · ${review.reviewLabel}`
    : review.cardId
  return (
    <li className="flex flex-col gap-1.5 rounded-md border border-edge bg-atmos px-3.5 py-3">
      <div className="flex items-center gap-2">
        <span className="font-['JetBrains_Mono',system-ui,sans-serif] font-medium tracking-[0.05em] text-[10px] leading-3 text-aurora">
          {left}
        </span>
        <span className="grow" />
        {review.prNumber ? (
          <span className="font-['JetBrains_Mono',system-ui,sans-serif] text-[10px] leading-3 text-twilight">
            {review.prNumber}
          </span>
        ) : null}
      </div>
      <p className="text-[13px] font-medium leading-[18px] text-star">
        {review.title}
      </p>
    </li>
  )
}

function SpeakerOrder({
  speakers,
}: {
  speakers: AgendaSpeaker[]
}): JSX.Element {
  return (
    <section className="flex flex-col gap-3.5">
      <SectionHeader
        label="Suggested speaker order"
        subscript="based on Awaiting you"
      />
      <ol className="flex flex-col gap-2">
        {speakers.length === 0 ? (
          <li className="rounded-md border border-edge bg-atmos px-3.5 py-3 text-[13px] leading-4 text-twilight">
            No participants assigned yet.
          </li>
        ) : (
          speakers.map((s, i) => (
            <SpeakerRow key={s.participant.id} index={i + 1} speaker={s} />
          ))
        )}
      </ol>
    </section>
  )
}

function SpeakerRow({
  index,
  speaker,
}: {
  index: number
  speaker: AgendaSpeaker
}): JSX.Element {
  const lead = index === 1
  return (
    <li className="flex items-center gap-3 rounded-md border border-edge bg-atmos px-3.5 py-3">
      <span
        aria-hidden
        className={
          lead
            ? "inline-flex h-5.5 w-5.5 shrink-0 items-center justify-center rounded-[11px] bg-solar"
            : "inline-flex h-5.5 w-5.5 shrink-0 items-center justify-center rounded-[11px] border border-edge bg-sky"
        }
      >
        <span
          className={
            lead
              ? "font-['JetBrains_Mono',system-ui,sans-serif] text-[10px] font-bold leading-3 text-void"
              : "font-['JetBrains_Mono',system-ui,sans-serif] text-[10px] font-semibold leading-3 text-twilight"
          }
        >
          {index}
        </span>
      </span>
      <div className="flex min-w-0 grow basis-0 items-center gap-2">
        <Avatar participant={speaker.participant} />
        <span className="truncate text-[13px] font-medium leading-4 text-star">
          {speaker.participant.name}
        </span>
        {speaker.action ? (
          <span className="truncate text-xs leading-4 text-twilight">
            {speaker.action}
          </span>
        ) : null}
      </div>
      {speaker.estimate ? (
        <span className="font-['JetBrains_Mono',system-ui,sans-serif] text-[10px] leading-3 text-twilight">
          {speaker.estimate}
        </span>
      ) : null}
    </li>
  )
}

function Avatar({
  participant,
}: {
  participant: AgendaParticipant
}): JSX.Element {
  const initials = participant.initials ?? deriveInitials(participant.name)
  return (
    <span
      aria-hidden
      className="inline-flex h-5.5 w-5.5 shrink-0 items-center justify-center rounded-[11px] border border-edge bg-sky"
    >
      <span className="text-[10px] font-semibold leading-3 text-star">
        {initials}
      </span>
    </span>
  )
}

function deriveInitials(name: string): string {
  const parts = name.split(/[\s\-_.@]+/).filter(Boolean)
  if (parts.length === 0) return "?"
  return parts
    .slice(0, 2)
    .map((p) => p[0]?.toUpperCase() ?? "")
    .join("")
}

function Footer({
  onSkip,
  onStart,
  onClose,
}: {
  onSkip: () => void
  onStart: () => void
  onClose: () => void
}): JSX.Element {
  return (
    <footer className="flex items-center gap-3 border-t border-edge px-8 pb-6 pt-[18px]">
      <p className="text-[11px] leading-3.5 text-twilight">
        nova will follow this — you can interrupt anytime
      </p>
      <span className="grow" />
      <button
        type="button"
        onClick={(): void => {
          onSkip()
          onClose()
        }}
        className="rounded px-3.5 py-2 text-xs font-medium text-twilight hover:text-star focus:outline-none focus:ring-2 focus:ring-edge"
      >
        Skip agenda
      </button>
      <button
        type="button"
        onClick={onStart}
        className="inline-flex items-center gap-2 rounded-[7px] bg-aurora px-[18px] py-2.5 text-[13px] font-semibold leading-4 tracking-[-0.005em] text-void transition hover:bg-aurora/90 focus:outline-none focus:ring-2 focus:ring-aurora/60"
      >
        Start standup
        <ArrowGlyph />
      </button>
    </footer>
  )
}

function ArrowGlyph(): JSX.Element {
  // Diagonal arrow head: bottom + right border of a small square, rotated.
  return (
    <span
      aria-hidden
      className="-mt-0.5 inline-block size-2 shrink-0 border-b-[1.5px] border-r-[1.5px] border-void"
      style={{ transform: "rotate(-45deg)" }}
    />
  )
}
