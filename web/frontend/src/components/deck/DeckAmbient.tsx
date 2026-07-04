import { cn } from '@aspect/theme'

interface DeckAmbientProps {
  /** Lifts the field a touch brighter while a scan is live. */
  scanning?: boolean
}

/**
 * The living backdrop behind the entire operation deck: a slow cortex aurora
 * (sky-blue + signal-blue) drifting over a faint sensor grid that pans beneath
 * it. Pure CSS transforms — cheap to composite — pinned at z-index -10 below all
 * content so it reads through the translucent cards and the gaps between them.
 * Honors prefers-reduced-motion. See [[aiscan-web-redesign-direction]].
 */
export default function DeckAmbient({ scanning = false }: DeckAmbientProps) {
  return (
    <div className={cn('deck-ambient', scanning && 'is-scanning')} aria-hidden="true">
      <span className="deck-ambient-blob deck-ambient-blob-a" />
      <span className="deck-ambient-blob deck-ambient-blob-b" />
      <span className="deck-ambient-blob deck-ambient-blob-c" />
    </div>
  )
}
