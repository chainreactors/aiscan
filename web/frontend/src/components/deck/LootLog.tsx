import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { KeyRound } from 'lucide-react'
import { cn } from '@aspect/theme'
import type { Loot } from '../../api'
import { logLineTone } from '../../lib/tones'
import { SectionHead } from './FindingsStream'
import DeckEmpty from './DeckEmpty'

interface LootLogProps {
  loots: Loot[]
  scannerLines: string[]
}

export default function LootLog({ loots, scannerLines }: LootLogProps) {
  const { t } = useTranslation('deck')
  const logRef = useRef<HTMLDivElement>(null)
  const tail = scannerLines.slice(-9)

  useEffect(() => {
    if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight
  }, [scannerLines.length])

  return (
    <section className="min-w-0">
      <SectionHead title={t('loot')} count={loots.length ? t('secretsCount', { count: loots.length }) : t('none')} />
      {loots.length === 0 ? (
        <div className="mb-5">
          <DeckEmpty glyph={<KeyRound className="h-6 w-6" />} title={t('noLoot')} />
        </div>
      ) : (
        <div className="mb-5 flex flex-col gap-2">
          {loots.slice(0, 6).map((loot, i) => (
            <div key={`${loot.target}-${i}`} className="flex items-center gap-3 rounded-lg border border-border bg-card/80 px-4 py-3 shadow-soft">
              <span className="grid h-[30px] w-[30px] shrink-0 place-items-center rounded-lg bg-destructive/12 text-destructive">
                <KeyRound className="h-[15px] w-[15px]" />
              </span>
              <div className="min-w-0">
                <div className="truncate text-[12.5px] font-semibold text-foreground">{loot.description || loot.kind || t('secretFallback')}</div>
                <div className="truncate font-mono text-[11px] text-muted-foreground">{loot.target}</div>
              </div>
              <span className="ml-auto shrink-0 font-mono text-[10px] uppercase tracking-[0.1em] text-ai">{loot.kind || t('lootFallback')}</span>
            </div>
          ))}
        </div>
      )}

      <SectionHead title={t('scannerLog')} count={scannerLines.length ? t('live') : t('idle')} />
      {/* Screen readers: the visible log is a sliding window keyed by index, so a
          live region ON it would re-read every row each time the window shifts.
          Instead announce only the newest line through this off-screen polite
          region; the visible log stays a plain (browsable, non-live) element. */}
      <div className="sr-only" role="status" aria-live="polite">
        {scannerLines.length > 0 ? scannerLines[scannerLines.length - 1] : ''}
      </div>
      <div
        ref={logRef}
        className="max-h-[220px] overflow-y-auto rounded-lg border border-border bg-secondary/30 p-3 font-mono text-[11.5px] leading-[1.85]"
      >
        {tail.length === 0 ? (
          <div className="text-muted-foreground/60">{t('awaitingScanner')}</div>
        ) : (
          tail.map((line, i) => (
            <div key={i} className={cn('overflow-hidden text-ellipsis whitespace-nowrap', logLineTone(line))}>
              {line}
            </div>
          ))
        )}
      </div>
    </section>
  )
}
