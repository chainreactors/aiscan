import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { AlertTriangle, Boxes, KeyRound, LayoutGrid, PanelRight, ShieldCheck, Sparkles, X } from 'lucide-react'
import { cn } from '@aspect/theme'
import type { AgentInfo, PoolAsset, ScanJob, ScanOptions, ScanResult } from '../../api'
import { buildResultModel, buildFindings } from '../../lib/scan-result'
import {
  PIPELINE_PHASES,
  deckProgress,
  deckState,
  findingAI,
  formatElapsed,
  inferPhaseIndex,
  splitLog,
} from '../../lib/deck'
import ScanForm from '../ScanForm'
import CortexCore from './CortexCore'
import MetricStrip, { type Metric } from './MetricStrip'
import FindingsStream from './FindingsStream'
import DeckAssetTree from './DeckAssetTree'
import LootLog from './LootLog'
import IntelRail from './IntelRail'

/** Canonical pipeline phase (lib/deck) → deck i18n key. */
const PHASE_KEY: Record<string, string> = {
  Discovery: 'phaseDiscovery',
  'Web probe': 'phaseWebProbe',
  'Weak-cred': 'phaseWeakCred',
  'POC detect': 'phasePocDetect',
  'AI verify': 'phaseAiVerify',
}

interface OperationDeckProps {
  scan: ScanJob | null
  result: ScanResult | null
  lines: string[]
  scanning: boolean
  error: string
  analysisAvailable: boolean
  agents: AgentInfo[]
  llmModel?: string
  llmProvider?: string
  now: number
  onSubmit: (target: string, mode: string, options: ScanOptions) => void
  onClearError: () => void
  onCommandCortex: (text: string) => void
  onOpenNode: (agentID: string) => void
  assets: PoolAsset[]
  onAddAsset: (raw: string) => void
  onRemoveAsset: (id: string) => void
  onDispatchAgent: (target: string) => void
}

export default function OperationDeck({
  scan,
  result,
  lines,
  scanning,
  error,
  analysisAvailable,
  agents,
  llmModel,
  llmProvider,
  now,
  onSubmit,
  onClearError,
  onCommandCortex,
  onOpenNode,
  assets,
  onAddAsset,
  onRemoveAsset,
  onDispatchAgent,
}: OperationDeckProps) {
  const { t } = useTranslation('deck')
  const modeLabel = (mode?: string) =>
    mode === 'quick' ? t('modeQuick') : mode === 'full' ? t('modeFull') : mode || t('standby')

  const findingsRef = useRef<HTMLDivElement>(null)
  const assetsRef = useRef<HTMLDivElement>(null)
  const lootRef = useRef<HTMLDivElement>(null)
  const heroRef = useRef<HTMLDivElement>(null)
  const mainRef = useRef<HTMLElement>(null)
  const navLockRef = useRef(0)
  const [activeNav, setActiveNav] = useState('op')
  // Below xl the intel rail is off-canvas; this opens it as a right-side drawer
  // so the asset pool + agent nodes stay reachable on tablet/phone widths.
  const [intelOpen, setIntelOpen] = useState(false)

  const model = llmModel || agents.find((a) => a.identity?.model)?.identity?.model || 'cortex'
  const providerLabel = (llmProvider || model).toUpperCase()
  const state = deckState(scan, scanning)
  const elapsed = formatElapsed(scan, result, now)
  // The scan log is walked with up to 5 regexes per line; without memoization
  // both inferPhaseIndex and deckProgress (which itself re-derives the phase)
  // re-scan the whole log on every render — and this deck re-renders every
  // second from the App elapsed clock. Compute the phase once per new line and
  // hand it to deckProgress so the log is scanned at most once per update.
  const phaseIndex = useMemo(() => inferPhaseIndex(lines), [lines])
  const progress = useMemo(
    () => deckProgress(scan, lines, scanning, phaseIndex),
    [scan, lines, scanning, phaseIndex],
  )

  const model3 = useMemo(() => (result ? buildResultModel(result) : null), [result])
  const findings = useMemo(() => (result ? buildFindings(result) : []), [result])
  const verifiedCount = useMemo(() => findings.filter((f) => findingAI(f) === 'verified').length, [findings])
  const { scanner } = useMemo(() => splitLog(lines), [lines])
  const loots = useMemo(() => (result?.loots || []).filter((l) => (l.kind || '').toLowerCase() !== 'fingerprint'), [result])

  const m = model3?.metrics
  const metrics: Metric[] = [
    { k: t('metricHosts'), v: m?.hosts ?? 0 },
    { k: t('metricServices'), v: m?.services ?? 0 },
    { k: t('metricWeb'), v: m?.web ?? 0 },
    { k: t('metricFingerprints'), v: m?.fingers ?? 0 },
    { k: t('metricLoots'), v: loots.length },
    { k: t('metricRequests'), v: result?.summary?.requests ?? 0, comma: true },
    { k: t('metricErrors'), v: m?.errors ?? 0, err: true },
    { k: t('metricDuration'), v: elapsed },
  ]

  const targets = (scan?.target || '').split(/[,\s]+/).filter(Boolean)
  const navItems = [
    { id: 'op', label: t('operation'), icon: <LayoutGrid className="h-[19px] w-[19px]" />, ref: heroRef, count: null },
    { id: 'findings', label: t('findings'), icon: <ShieldCheck className="h-[19px] w-[19px]" />, ref: findingsRef, count: findings.length },
    { id: 'assets', label: t('assets'), icon: <Boxes className="h-[19px] w-[19px]" />, ref: assetsRef, count: model3?.hosts?.length ?? 0 },
    { id: 'loot', label: t('loot'), icon: <KeyRound className="h-[19px] w-[19px]" />, ref: lootRef, count: loots.length },
  ]

  const scrollTo = (id: string, ref: React.RefObject<HTMLDivElement>) => {
    navLockRef.current = Date.now() + 1200
    setActiveNav(id)
    ref.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }

  // Scroll-spy: the rail highlight tracks whichever section sits at the top of
  // the main column as you scroll, not just the last icon clicked. A click sets
  // the highlight and briefly locks it (released early by a real wheel/touch) so
  // the smooth scroll doesn't drag the highlight through the sections it passes.
  useEffect(() => {
    const root = mainRef.current
    if (!root) return
    const sections = [
      { id: 'op', el: heroRef.current },
      { id: 'findings', el: findingsRef.current },
      { id: 'assets', el: assetsRef.current },
      { id: 'loot', el: lootRef.current },
    ].filter((s): s is { id: string; el: HTMLDivElement } => !!s.el)
    if (sections.length === 0) return

    const pick = () => {
      if (Date.now() < navLockRef.current) return
      // Active = the last section whose top has scrolled above the detection
      // line near the top of the viewport (standard scroll-spy). On a short
      // result page the trailing asset/loot row can't scroll that high, so once
      // the scroll bottoms out we pin the last section.
      if (root.scrollTop + root.clientHeight >= root.scrollHeight - 4) {
        setActiveNav(sections[sections.length - 1].id)
        return
      }
      const rootTop = root.getBoundingClientRect().top
      const line = root.clientHeight * 0.28
      let current = sections[0].id
      let bestTop = -Infinity
      for (const s of sections) {
        // ties (assets/loot share a row on wide screens) keep the earlier one.
        const top = s.el.getBoundingClientRect().top - rootTop
        if (top <= line && top > bestTop) {
          bestTop = top
          current = s.id
        }
      }
      setActiveNav(current)
    }

    let raf = 0
    const onScroll = () => {
      cancelAnimationFrame(raf)
      raf = requestAnimationFrame(pick)
    }
    const release = () => {
      navLockRef.current = 0
    }

    const observer = new IntersectionObserver(pick, { root, rootMargin: '0px 0px -72% 0px', threshold: [0, 1] })
    sections.forEach((s) => observer.observe(s.el))
    root.addEventListener('scroll', onScroll, { passive: true })
    root.addEventListener('wheel', release, { passive: true })
    root.addEventListener('touchmove', release, { passive: true })
    pick()
    return () => {
      cancelAnimationFrame(raf)
      observer.disconnect()
      root.removeEventListener('scroll', onScroll)
      root.removeEventListener('wheel', release)
      root.removeEventListener('touchmove', release)
    }
  }, [])

  const intelRail = (
    <IntelRail
      agents={agents}
      providerLabel={providerLabel}
      scanning={scanning}
      onCommand={onCommandCortex}
      onOpenNode={onOpenNode}
      showReasoning={false}
      assets={assets}
      onAddAsset={onAddAsset}
      onRemoveAsset={onRemoveAsset}
      onScanAsset={(target) => onSubmit(target, 'quick', { verify: false, sniper: false, deep: false })}
      onDispatchAsset={onDispatchAgent}
      hasAgents={agents.length > 0}
    />
  )

  return (
    // Responsive shell: main-only on phones, + rail at lg, + intel column at xl.
    // The nav rail and intel column drop out of the grid below their breakpoints
    // (display:none) so the fixed 64px+348px chrome can't overflow a narrow screen.
    <div className="grid h-full min-h-0 w-full grid-cols-1 lg:grid-cols-[64px_1fr] xl:grid-cols-[64px_1fr_348px]">
      {/* RAIL — hidden below lg; four section-jumps aren't worth a column on a phone. */}
      <nav className="hidden flex-col items-center gap-2 border-r border-border bg-card/70 py-5 lg:flex">
        {navItems.map((item) => (
          <button
            key={item.id}
            title={item.label}
            aria-label={item.count != null && item.count > 0 ? `${item.label} · ${item.count}` : item.label}
            onClick={() => scrollTo(item.id, item.ref)}
            className={cn(
              'relative grid h-[42px] w-[42px] place-items-center rounded-xl transition-colors',
              activeNav === item.id ? 'bg-secondary/60 text-foreground' : 'text-muted-foreground/60 hover:bg-secondary/40 hover:text-foreground',
            )}
          >
            {activeNav === item.id && (
              <span className="absolute -left-[13px] top-1/2 h-5 w-[3px] -translate-y-1/2 rounded bg-gradient-to-b from-ai to-primary shadow-glow-sm" />
            )}
            {item.icon}
            {/* Only badge a non-zero count — a "0" pill on every icon reads as a
                row of unread-notification dots on an empty deck, and its count
                would otherwise become the button's accessible name. */}
            {item.count != null && item.count > 0 && (
              <span
                aria-hidden="true"
                className="absolute -right-1 -top-1 grid h-[16px] min-w-[16px] place-items-center rounded-full border border-ai/40 bg-card px-1 font-mono text-[9px] font-bold tabular-nums text-foreground"
              >
                {item.count > 99 ? '99+' : item.count}
              </span>
            )}
          </button>
        ))}
      </nav>

      {/* MAIN */}
      <main ref={mainRef} className="min-w-0 overflow-y-auto px-4 py-6 sm:px-8">
        {/* scanbar */}
        <div className="mb-6 rounded-xl border border-border bg-card/70 px-4 py-2.5 shadow-soft backdrop-blur">
          <ScanForm onSubmit={onSubmit} disabled={scanning} analysisAvailable={analysisAvailable} />
        </div>

        {error && (
          <div role="alert" className="mb-6 flex items-start gap-2 rounded-xl border border-destructive/30 bg-destructive/10 px-4 py-3 text-sm text-destructive">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
            <span className="min-w-0 flex-1 break-words">{error}</span>
            <button type="button" aria-label={t('dismiss')} onClick={onClearError} className="rounded p-0.5 text-destructive/70 hover:bg-destructive/10 hover:text-destructive">
              <X className="h-4 w-4" />
            </button>
          </div>
        )}

        {/* HERO */}
        <section
          ref={heroRef}
          className="relative mb-6 grid items-center gap-8 overflow-hidden rounded-[22px] border border-border bg-card/70 px-8 py-6 shadow-lifted backdrop-blur lg:grid-cols-[280px_1fr]"
          style={{ background: 'radial-gradient(420px 280px at 14% 50%, hsl(var(--primary) / 0.10), transparent 70%), hsl(var(--card) / 0.7)' }}
        >
          <div className="justify-self-center">
            <CortexCore progress={progress} state={state} modelName={model} verifiedCount={verifiedCount} />
          </div>
          <div className="flex min-w-0 flex-col gap-4">
            <div>
              <div className="flex items-center gap-2 font-mono text-[10.5px] font-bold uppercase tracking-[0.2em] text-muted-foreground">
                {t('operation')}
                <span className="text-muted-foreground/50">·</span>
                {t('pipelineWith', { mode: modeLabel(scan?.mode) })}
                {scan?.verify && (
                  <>
                    <span className="text-muted-foreground/50">·</span>
                    <span className="text-gradient-brand">{t('aiVerify')}</span>
                  </>
                )}
              </div>
              <h1 className="mt-2 font-display text-[36px] font-bold leading-[1.04] tracking-[-0.02em] text-foreground">{scan?.target || t('standbyTitle')}</h1>
              <div className="mt-2 flex items-center gap-2 font-display text-[15px] font-medium text-muted-foreground">
                <span className={cn('h-1.5 w-1.5 rounded-full', scanning ? 'breathe bg-primary shadow-[0_0_9px_hsl(var(--primary))]' : 'bg-muted-foreground/50')} />
                {scanning ? t('heroLive') : state === 'complete' ? t('heroComplete') : state === 'failed' ? t('heroFailed') : t('heroAwaiting')}
              </div>
              {targets.length > 0 && (
                <div className="mt-2.5 flex flex-wrap gap-2 font-mono text-xs text-muted-foreground">
                  {targets.slice(0, 4).map((t) => (
                    <b key={t} className="rounded-md border border-border bg-secondary/50 px-2 py-0.5 font-medium text-foreground">{t}</b>
                  ))}
                </div>
              )}
            </div>

            <div className="flex flex-wrap gap-2">
              {scan?.mode && <Chip on>{t('pipelineWith', { mode: modeLabel(scan.mode) })}</Chip>}
              {scan?.verify && <Chip ai icon={<Sparkles className="h-3 w-3" />}>{t('aiVerify')}</Chip>}
              {scan?.sniper && <Chip ai>{t('chipSniper')}</Chip>}
              {scan?.deep && <Chip ai>{t('chipDeep')}</Chip>}
              <Chip>{elapsed}</Chip>
            </div>

            {/* timeline */}
            <div className="flex items-center gap-2">
              {PIPELINE_PHASES.map((phase, i) => {
                const done = i < phaseIndex || state === 'complete'
                const active = scanning && i === phaseIndex
                return (
                  <div key={phase} className="flex flex-1 items-center gap-2">
                    <span className={cn('flex items-center gap-1.5 whitespace-nowrap font-mono text-[10px] font-semibold uppercase tracking-[0.04em]', active ? 'text-primary' : done ? 'text-muted-foreground' : 'text-muted-foreground/70')}>
                      <span className={cn('h-1.5 w-1.5 rounded-full', active ? 'breathe bg-primary shadow-[0_0_8px_hsl(var(--primary))]' : done ? 'bg-success' : 'bg-border')} />
                      {t(PHASE_KEY[phase])}
                    </span>
                    {i < PIPELINE_PHASES.length - 1 && <span className="h-px flex-1 bg-border" />}
                  </div>
                )
              })}
            </div>
          </div>
        </section>

        {/* The all-zero ledger carries no signal on the standby deck — show it
            only once a scan is queued/running/done so the landing view isn't a
            wall of zeros. */}
        {state !== 'idle' && <MetricStrip metrics={metrics} />}

        <div ref={findingsRef}>
          <FindingsStream
            findings={findings}
            triaging={scanning && analysisAvailable}
            state={state}
            onRescanFull={scan?.target ? () => onSubmit(scan.target, 'full', { verify: true, sniper: false, deep: false }) : undefined}
          />
        </div>

        <div className="grid gap-6 xl:grid-cols-2">
          <div ref={assetsRef}>
            {/* Key on the scan id so switching scans remounts the tree with a
                fresh default-open state; within one scan it stays mounted and
                preserves the operator's manual expand/collapse as hosts stream. */}
            <DeckAssetTree key={scan?.id} hosts={model3?.hosts || []} />
          </div>
          <div ref={lootRef}>
            <LootLog loots={loots} scannerLines={scanner} />
          </div>
        </div>
      </main>

      {/* INTEL — in-grid third column at xl+; an off-canvas drawer below xl. */}
      <div className="hidden min-h-0 xl:block xl:h-full">{intelRail}</div>

      {/* Floating toggle + drawer for the intel rail below xl, so the asset pool
          and agent nodes stay reachable on tablet / phone widths. */}
      <button
        type="button"
        onClick={() => setIntelOpen(true)}
        aria-label={t('openIntel')}
        title={t('openIntel')}
        className="fixed bottom-4 right-4 z-30 grid h-11 w-11 place-items-center rounded-full border border-border bg-card text-foreground shadow-lifted transition-colors hover:bg-secondary xl:hidden"
      >
        <PanelRight className="h-5 w-5" />
      </button>
      {intelOpen && (
        <div className="fixed inset-0 z-40 xl:hidden">
          <div className="absolute inset-0 bg-black/50 backdrop-blur-sm animate-in fade-in duration-200" onClick={() => setIntelOpen(false)} />
          <div className="absolute inset-y-0 right-0 flex w-[min(348px,88vw)] flex-col bg-card shadow-elevated animate-in slide-in-from-right-4 duration-200">
            <div className="flex h-11 shrink-0 items-center justify-between border-b border-border px-3">
              <span className="font-display text-xs font-semibold uppercase tracking-[0.08em] text-muted-foreground">{t('openIntel')}</span>
              <button type="button" onClick={() => setIntelOpen(false)} aria-label={t('dismiss')} className="rounded-md p-1 text-muted-foreground hover:bg-accent hover:text-foreground">
                <X className="h-4 w-4" />
              </button>
            </div>
            <div className="min-h-0 flex-1">{intelRail}</div>
          </div>
        </div>
      )}
    </div>
  )
}

function Chip({ children, on, ai, icon }: { children: ReactNode; on?: boolean; ai?: boolean; icon?: ReactNode }) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 font-mono text-[10.5px] font-semibold uppercase tracking-[0.04em]',
        ai
          ? 'border border-transparent bg-gradient-to-r from-ai/85 to-primary/85 text-ai-foreground'
          : on
            ? 'border border-border bg-secondary/50 text-foreground'
            : 'border border-border bg-secondary/40 text-muted-foreground',
      )}
    >
      {icon}
      {children}
    </span>
  )
}

