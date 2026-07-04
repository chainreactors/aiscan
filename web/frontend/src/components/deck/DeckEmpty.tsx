import type { CSSProperties, ReactNode } from 'react'
import { cn } from '@aspect/theme'

/**
 * The shared standby placeholder for an idle deck section. Rather than a hollow
 * bordered box (or, when a section has no rows, a box that collapses to a bare
 * hairline), it reuses the deck signature — a reticle-cornered tactical-grid
 * tile cradling the section glyph — so an empty section reads as an instrument
 * at rest, not as missing UI. Static by default (no scanline) so the landing
 * stays calm; pass `live` for the one section actively listening.
 */
export default function DeckEmpty({
  glyph,
  title,
  hint,
  tone = 'muted',
  live = false,
  action,
  className,
}: {
  glyph: ReactNode
  title: string
  hint?: ReactNode
  /** success = a resolved/clean result (green tile); muted = plain standby. */
  tone?: 'muted' | 'success'
  live?: boolean
  action?: ReactNode
  className?: string
}) {
  const success = tone === 'success'
  return (
    <div
      className={cn(
        // Recessed well: a faint sunken surface + inset hairline so an empty
        // section reads as a groove beneath the lifted hero, not a hollow
        // outline floating on the field. Pairs with the deck's elevation model.
        'flex flex-col items-center gap-3 rounded-xl border px-6 py-8 text-center',
        success
          ? 'border-success/25 bg-success/[0.05]'
          : 'border-border/70 bg-foreground/[0.015] shadow-[inset_0_1px_3px_hsl(var(--foreground)/0.05)]',
        className,
      )}
    >
      <div
        className={cn(
          'reticle relative grid h-14 w-14 place-items-center overflow-hidden rounded-xl border bg-card/40',
          success ? 'border-success/30' : 'border-border/60',
        )}
        style={{ '--reticle-size': '10px', '--reticle-color': success ? 'hsl(var(--success) / 0.55)' : undefined } as CSSProperties}
      >
        <div className="absolute inset-0 grid-tactical opacity-60" />
        {live && <div className="scanline absolute inset-x-0 inset-y-0" />}
        <span className={cn('relative', success ? 'text-success' : 'text-muted-foreground/70')}>{glyph}</span>
      </div>
      <div className="flex flex-col gap-1">
        <span className="text-sm font-medium text-foreground">{title}</span>
        {hint && <span className="max-w-xs text-xs leading-relaxed text-muted-foreground">{hint}</span>}
      </div>
      {action}
    </div>
  )
}
