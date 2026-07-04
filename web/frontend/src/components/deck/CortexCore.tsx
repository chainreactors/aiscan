import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { DeckState } from '../../lib/deck'

interface CortexCoreProps {
  progress: number // 0..1
  state: DeckState
  modelName: string
  verifiedCount: number
}

const STATE_KEY: Record<DeckState, string> = {
  idle: 'stateIdle',
  queued: 'stateQueued',
  scanning: 'stateScanning',
  complete: 'stateComplete',
  failed: 'stateFailed',
  canceled: 'stateCanceled',
}

const C = 490 // circumference of r=78

/**
 * The cortex nucleus — the hero signature of the CORTEX deck. A progress arc
 * wraps two counter-rotating structural rings around a luminous core; the core
 * fires a one-shot pulse each time a finding crosses into AI-verified.
 */
export default function CortexCore({ progress, state, modelName, verifiedCount }: CortexCoreProps) {
  const { t } = useTranslation('deck')
  const [pulseKey, setPulseKey] = useState(0)
  const prevVerified = useRef(verifiedCount)
  const live = state === 'scanning' || state === 'queued'

  useEffect(() => {
    if (verifiedCount > prevVerified.current) setPulseKey((k) => k + 1)
    prevVerified.current = verifiedCount
  }, [verifiedCount])

  const pct = Math.round(progress * 100)
  const offset = C - C * Math.max(0, Math.min(1, progress))

  return (
    <div className="relative grid h-[240px] w-[280px] place-items-center">
      <svg className="absolute inset-0 h-full w-full" viewBox="0 0 280 240" aria-hidden="true">
        <defs>
          <linearGradient id="cortexArc" x1="0" y1="0" x2="1" y2="1">
            <stop offset="0" stopColor="hsl(var(--ai))" />
            <stop offset="1" stopColor="hsl(var(--primary))" />
          </linearGradient>
          <radialGradient id="cortexGlow" cx="50%" cy="50%" r="50%">
            <stop offset="0" stopColor="hsl(var(--primary))" stopOpacity="0.42" />
            <stop offset="70%" stopColor="hsl(var(--ai))" stopOpacity="0.05" />
            <stop offset="100%" stopColor="hsl(var(--ai))" stopOpacity="0" />
          </radialGradient>
        </defs>

        <circle cx="140" cy="120" r="92" fill="url(#cortexGlow)" />
        {/* progress track + arc */}
        <circle cx="140" cy="120" r="78" fill="none" stroke="hsl(var(--border))" strokeWidth="6" />
        <circle
          cx="140"
          cy="120"
          r="78"
          fill="none"
          stroke="url(#cortexArc)"
          strokeWidth="6"
          strokeLinecap="round"
          strokeDasharray={C}
          strokeDashoffset={offset}
          transform="rotate(-90 140 120)"
          style={{ transition: 'stroke-dashoffset 1.1s cubic-bezier(0.16,1,0.3,1)' }}
        />
        {/* counter-rotating structural rings */}
        <g className={live ? 'core-spin' : undefined} style={{ transformBox: 'fill-box' } as React.CSSProperties}>
          <circle cx="140" cy="120" r="64" fill="none" stroke="hsl(var(--border))" strokeWidth="1" strokeDasharray="2 8" />
        </g>
        <g className={live ? 'core-spin-rev' : undefined} style={{ transformBox: 'fill-box' } as React.CSSProperties}>
          <circle cx="140" cy="120" r="52" fill="none" stroke="hsl(var(--primary) / 0.35)" strokeWidth="1.2" strokeDasharray="40 14 6 14" />
        </g>
        {/* nucleus */}
        <circle cx="140" cy="120" r="42" fill="url(#cortexArc)" opacity="0.18" />
        <circle cx="140" cy="120" r="30" fill="url(#cortexArc)" opacity="0.36" className={live ? 'breathe' : undefined} style={{ transformOrigin: '140px 120px' }} />
      </svg>

      {pulseKey > 0 && (
        <span
          key={pulseKey}
          className="core-pulse pointer-events-none absolute left-1/2 top-1/2 z-[1] h-[120px] w-[120px] rounded-full border-2"
          style={{ borderColor: 'hsl(var(--ai))', transform: 'translate(-50%,-50%)' }}
        />
      )}

      <div className="absolute inset-0 z-[2] flex flex-col items-center justify-center text-center">
        <div className="font-display text-[46px] font-semibold leading-none tracking-[-0.04em] text-foreground">
          {pct}
          <span className="text-[18px] text-foreground/70">%</span>
        </div>
        <div className="mt-1.5 flex items-center gap-2 font-mono text-[10px] font-bold uppercase tracking-[0.22em] text-primary">
          {live && <span className="breathe inline-block h-1.5 w-1.5 rounded-full bg-primary shadow-[0_0_10px_hsl(var(--primary))]" />}
          {t(STATE_KEY[state])}
        </div>
        <div className="absolute inset-x-0 bottom-1 font-mono text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground/70">
          CORTEX · {modelName}
        </div>
      </div>
    </div>
  )
}
