import { useTranslation } from 'react-i18next'
import { Check, ExternalLink, Loader2, Shield, ShieldCheck } from 'lucide-react'
import { cn } from '@aspect/theme'
import { findingTargetURL, type FindingItem } from '../../lib/scan-result'
import { severityTone } from '../../lib/tones'
import { findingAI } from '../../lib/deck'
import type { DeckState } from '../../lib/deck'
import DeckEmpty from './DeckEmpty'

interface FindingsStreamProps {
  findings: FindingItem[]
  triaging: boolean
  /** Deck state, so the empty body can distinguish idle / scanning / clean-complete. */
  state?: DeckState
  /** Offer a "re-scan in full mode" action when a scan finished with no findings. */
  onRescanFull?: () => void
}

export default function FindingsStream({ findings, triaging, state = 'idle', onRescanFull }: FindingsStreamProps) {
  const { t } = useTranslation('deck')
  return (
    <section className="mb-6">
      <SectionHead title={t('findings')} count={t('activeCount', { count: findings.length })}>
        {triaging && (
          <span className="ml-auto inline-flex items-center gap-2 whitespace-nowrap font-mono text-[10px] font-medium uppercase tracking-[0.14em] text-ai">
            <span className="relative flex h-1.5 w-1.5">
              <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-ai/70" />
              <span className="relative inline-flex h-1.5 w-1.5 rounded-full bg-ai" />
            </span>
            {t('liveTriaging')}
          </span>
        )}
      </SectionHead>

      {findings.length === 0 ? (
        <EmptyFindings state={state} triaging={triaging} onRescanFull={onRescanFull} />
      ) : (
        <div className="flex flex-col divide-y divide-border/50 overflow-hidden rounded-xl border border-border/60 bg-card/30">
          {findings.map((f, i) => (
            <FindingCard key={f.id} finding={f} index={i} />
          ))}
        </div>
      )}
    </section>
  )
}

/**
 * The empty findings body, told apart by deck state so a finished-clean scan
 * reads as a resolved success (and offers a deeper re-scan) rather than reusing
 * the flat "no findings" placeholder that idle and mid-scan also show.
 */
function EmptyFindings({ state, triaging, onRescanFull }: { state: DeckState; triaging: boolean; onRescanFull?: () => void }) {
  const { t } = useTranslation('deck')

  // Mid-scan, before anything streams in — read as working, not empty.
  if (triaging || state === 'scanning' || state === 'queued') {
    return (
      <div className="flex items-center justify-center gap-2.5 rounded-xl border border-border bg-card/40 py-8 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 shrink-0 animate-spin text-ai" />
        {t('findingsTriaging')}
      </div>
    )
  }

  // A completed scan that surfaced nothing — a positive "clean" result, plus a
  // one-click path to dig deeper instead of a dead end.
  if (state === 'complete') {
    return (
      <DeckEmpty
        tone="success"
        glyph={<ShieldCheck className="h-6 w-6" />}
        title={t('findingsClean')}
        action={
          onRescanFull && (
            <button
              type="button"
              onClick={onRescanFull}
              className="rounded-md border border-border bg-secondary/60 px-3 py-1.5 font-mono text-[11px] font-semibold text-foreground transition-colors hover:bg-secondary"
            >
              {t('rescanFull')}
            </button>
          )
        }
      />
    )
  }

  // True idle (or failed/canceled) — a standby instrument that points back at
  // the scanbar rather than a hollow box.
  return (
    <DeckEmpty
      glyph={<Shield className="h-6 w-6" />}
      title={state === 'idle' ? t('findingsIdleHint') : t('noFindings')}
    />
  )
}

function FindingCard({ finding, index }: { finding: FindingItem; index: number }) {
  const { t } = useTranslation('deck')
  const { t: tf } = useTranslation('findings')
  const tone = severityTone[finding.priority]
  const ai = findingAI(finding)
  const verified = ai === 'verified'
  // When the target is a navigable http(s) URL the whole row becomes a link
  // that opens it in a new tab. Non-web targets (bare host:port) stay inert.
  const href = findingTargetURL(finding.target)

  const className = cn(
    'card-in group relative flex items-stretch gap-3.5 px-4 py-3 transition-colors duration-200',
    href && 'cursor-pointer focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-ai/40',
    verified ? 'bg-ai/[0.04] hover:bg-ai/[0.07]' : 'hover:bg-foreground/[0.025]',
  )
  const style = { animationDelay: `${0.04 + index * 0.045}s` }

  const body = (
    <>
      {/* Severity rail — a single quiet tick, color carries the weight. */}
      <span className={cn('mt-0.5 h-[1.7rem] w-[3px] shrink-0 rounded-full', tone.dot)} />

      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <div className="flex items-baseline gap-2.5">
          <span className={cn('shrink-0 font-mono text-[10px] font-semibold uppercase tracking-[0.14em]', tone.text)}>
            {tf(`severity_${finding.priority}`)}
          </span>
          <span className="min-w-0 flex-1 truncate text-[13px] font-medium tracking-[-0.005em] text-foreground">{finding.title}</span>
        </div>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-muted-foreground">
          {finding.target && (
            <span className={cn('inline-flex min-w-0 items-center gap-1 truncate font-mono text-foreground/70', href && 'underline-offset-2 group-hover:text-primary group-hover:underline')}>
              <span className="truncate">{finding.target}</span>
              {href && <ExternalLink className="h-3 w-3 shrink-0 opacity-40 transition-opacity group-hover:opacity-90" />}
            </span>
          )}
          {finding.source && (
            <span className="inline-flex items-center gap-1.5 text-muted-foreground/70">
              {t('source')}
              <span className="font-mono text-[10.5px] text-foreground/60">{finding.source}</span>
            </span>
          )}
          {finding.status && finding.source && <span className="text-muted-foreground/60">{finding.status}</span>}
        </div>
      </div>

      {ai && <FindingProvenance ai={ai} verified={verified} label={verified ? t('aiVerified') : ai === 'sniper' ? t('aiSniper') : t('aiDeep')} />}
    </>
  )

  if (href) {
    return (
      <a
        href={href}
        target="_blank"
        rel="noreferrer noopener"
        className={className}
        style={style}
        title={finding.target}
        aria-label={t('openTargetUrl', { url: finding.target })}
      >
        {body}
      </a>
    )
  }

  return (
    <article className={className} style={style}>
      {body}
    </article>
  )
}

/* AI provenance — quiet violet marker, no glow/sparkle. Verified earns a
   reticle-ringed check (the deck's targeting signature, at rest); sniper/deep
   are a hairline-bracketed label. */
function FindingProvenance({ ai, verified, label }: { ai: 'verified' | 'sniper' | 'deep'; verified: boolean; label: string }) {
  return (
    <span className="flex shrink-0 items-center gap-1.5 self-center font-mono text-[10px] font-medium uppercase tracking-[0.1em] text-ai/90" title={label}>
      {verified ? (
        <span className="inline-flex h-[18px] w-[18px] items-center justify-center rounded-full border border-ai/40 text-ai">
          <Check className="h-2.5 w-2.5" strokeWidth={3} />
        </span>
      ) : (
        <span className="h-1.5 w-1.5 rounded-full bg-ai/60" aria-hidden />
      )}
      <span className="hidden sm:inline">{label}</span>
    </span>
  )
}

export function SectionHead({ title, count, children }: { title: string; count?: string; children?: React.ReactNode }) {
  return (
    <div className="mb-4 flex items-center gap-3">
      <h2 className="font-display text-sm font-semibold tracking-[0.01em] text-foreground">{title}</h2>
      {count && (
        <span className="rounded-full border border-border bg-secondary/50 px-2.5 py-0.5 font-mono text-[11px] text-muted-foreground">{count}</span>
      )}
      {children}
    </div>
  )
}
