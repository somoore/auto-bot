interface Props {
  index: number
  label: string
  recommended?: boolean
  // showEnterHint shows "↵ enter" — used for the first (default) chip so the
  // user knows pressing return submits it.
  showEnterHint?: boolean
  disabled?: boolean
  onClick: () => void
}

// SuggestedAnswerChip renders a numbered suggestion the user can pick to
// answer the current run question. The number doubles as a keyboard hint;
// callers wire up 1/2/3 hotkeys in CardRunTab.
export function SuggestedAnswerChip({
  index,
  label,
  recommended,
  showEnterHint,
  disabled,
  onClick,
}: Props): JSX.Element {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-label={recommended ? `${label} (recommended)` : label}
      className={
        recommended
          ? "group flex w-full items-center gap-2 rounded-md border border-solar bg-solar/15 px-3 py-2 text-left text-sm text-star hover:bg-solar/25 disabled:cursor-not-allowed disabled:opacity-50"
          : "group flex w-full items-center gap-2 rounded-md border border-edge bg-atmos px-3 py-2 text-left text-sm text-star hover:border-solar/50 disabled:cursor-not-allowed disabled:opacity-50"
      }
    >
      <span
        aria-hidden
        className={
          recommended
            ? "inline-flex h-5 w-5 shrink-0 items-center justify-center rounded border border-solar bg-solar/30 text-[10px] font-mono text-star"
            : "inline-flex h-5 w-5 shrink-0 items-center justify-center rounded border border-edge bg-sky text-[10px] font-mono text-twilight"
        }
      >
        {index}
      </span>
      <span className="flex-1">{label}</span>
      {recommended ? (
        <span className="rounded bg-solar/30 px-1.5 py-0.5 text-[9px] font-semibold uppercase tracking-wider text-star">
          recommended
        </span>
      ) : null}
      {showEnterHint ? (
        <span className="hidden font-mono text-[10px] text-twilight sm:inline">
          ↵ enter
        </span>
      ) : null}
    </button>
  )
}
