import { useEffect, useRef, useState } from 'react'
import { cn } from '@aspect/theme'

export interface Metric {
  k: string
  v: number | string
  comma?: boolean
  err?: boolean
}

/** The 8-cell metric ledger beneath the hero. Numeric cells count up on change. */
export default function MetricStrip({ metrics }: { metrics: Metric[] }) {
  return (
    <div className="mb-6 grid grid-cols-2 gap-px overflow-hidden rounded-xl border border-border bg-border sm:grid-cols-4 xl:grid-cols-8">
      {metrics.map((m) => (
        <div key={m.k} className="flex flex-col gap-1 bg-card px-4 py-3.5">
          <span className={cn('font-display text-[25px] font-semibold leading-none tracking-[-0.03em]', m.err && Number(m.v) > 0 ? 'text-caution' : 'text-foreground')}>
            {typeof m.v === 'number' ? <CountUp value={m.v} comma={m.comma} /> : m.v}
          </span>
          <span className="font-mono text-[10px] font-semibold uppercase tracking-[0.1em] text-muted-foreground/70">{m.k}</span>
        </div>
      ))}
    </div>
  )
}

function CountUp({ value, comma }: { value: number; comma?: boolean }) {
  const [display, setDisplay] = useState(value)
  // Mirrors the value currently painted on screen. Reading this (not a "start of
  // the last animation" ref) as the origin means an update that lands mid-count
  // continues smoothly from where the number is, instead of snapping back to the
  // interrupted animation's origin and re-climbing (a visible backward jump).
  const displayRef = useRef(value)
  const raf = useRef<number | null>(null)

  useEffect(() => {
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
      setDisplay(value)
      displayRef.current = value
      return
    }
    const start = performance.now()
    const a = displayRef.current
    const b = value
    const dur = 900
    const step = (ts: number) => {
      const p = Math.min((ts - start) / dur, 1)
      const eased = 1 - Math.pow(1 - p, 3)
      const cur = Math.round(a + (b - a) * eased)
      displayRef.current = cur
      setDisplay(cur)
      if (p < 1) raf.current = requestAnimationFrame(step)
      else displayRef.current = b
    }
    raf.current = requestAnimationFrame(step)
    return () => {
      if (raf.current) cancelAnimationFrame(raf.current)
    }
  }, [value])

  return <>{comma ? display.toLocaleString() : display}</>
}
