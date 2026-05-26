export function EmptyState(): JSX.Element {
  return (
    <div className="mx-auto mt-12 max-w-md rounded-xl border border-edge/60 bg-sky/40 p-8 text-center">
      <div aria-hidden className="mx-auto mb-4 inline-flex h-12 w-12 items-center justify-center rounded-full bg-gradient-to-br from-comet/30 via-aurora/20 to-solar/20 text-2xl">
        ✦
      </div>
      <h3 className="text-base font-semibold text-star">No cards yet</h3>
      <p className="mt-1 text-sm text-twilight">
        Say <span className="font-mono text-comet">&quot;create a card&quot;</span> in a standup or click + to add one.
      </p>
    </div>
  )
}
