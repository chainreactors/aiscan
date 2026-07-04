import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, ScanLine, Sparkles, Trash2 } from 'lucide-react'
import { cn } from '@aspect/theme'
import BrandLogo from '../brand/BrandLogo'
import type { ScanJob } from '../../api'

export type DeckView = 'scan' | 'chat'

interface DeckTopBarProps {
  view: DeckView
  onSwitchView: (view: DeckView) => void
  model: string
  analysisAvailable: boolean
  /** Scan-side breadcrumb + live status. Ignored on the agent view. */
  scan?: ScanJob | null
  scans?: ScanJob[]
  onSelectScan?: (scan: ScanJob) => void
  onDeleteScan?: (scan: ScanJob) => void
  scanning?: boolean
  elapsed?: string
  /** Agent-side breadcrumb label (active session / agent name). */
  chatCrumb?: string
  /** View-specific right-hand control cluster. */
  actions?: ReactNode
}

/**
 * The persistent operation-deck top bar. It is rendered once, above the body,
 * so flipping SCAN ⇄ AGENT swaps only the content beneath it — the brand, the
 * SCAN/AGENT switch and the global controls never move. See [[aiscan-web-redesign-direction]].
 */
export default function DeckTopBar({
  view,
  onSwitchView,
  model,
  analysisAvailable,
  scan = null,
  scans = [],
  onSelectScan,
  onDeleteScan,
  scanning = false,
  elapsed = '00:00',
  chatCrumb,
  actions,
}: DeckTopBarProps) {
  const { t } = useTranslation('deck')
  return (
    <header className="z-30 flex h-[60px] shrink-0 items-center gap-5 border-b border-border bg-card/80 px-6 backdrop-blur-md">
      <div className="flex shrink-0 items-center gap-3">
        <BrandLogo size={30} className="drop-shadow-[0_0_10px_rgba(255,77,77,0.45)]" />
        <div className="font-display text-[19px] font-semibold leading-none tracking-[-0.02em]">
          ai<b className="text-gradient-brand">scan</b>
        </div>
      </div>

      {view === 'scan' ? (
        <HistoryCrumb scan={scan} scans={scans} onSelectScan={onSelectScan} onDeleteScan={onDeleteScan} />
      ) : (
        <ChatCrumb label={chatCrumb} />
      )}

      <div className="flex-1" />

      <ViewSwitch view={view} onSwitch={onSwitchView} />

      {/* No reserved width: the readout is only as wide as its label, so there's
          no permanent empty slack beside it. The switch nudges left when the
          label grows from "READY" to "SCANNING · 00:00"; the model pill and
          controls stay anchored on the right. */}
      <div className="hidden shrink-0 items-center gap-2 font-mono text-[11.5px] text-muted-foreground md:flex">
        {view === 'scan' ? (
          <>
            <span className={cn('h-1.5 w-1.5 rounded-full', scanning ? 'breathe bg-primary shadow-[0_0_8px_hsl(var(--primary))]' : 'bg-success shadow-[0_0_8px_hsl(var(--success))]')} />
            {scanning ? `${t('statusScanning')} · ${elapsed}` : t('statusReady')}
          </>
        ) : (
          <>
            <span className="h-1.5 w-1.5 rounded-full bg-success shadow-[0_0_8px_hsl(var(--success))]" />
            {t('statusReady')}
          </>
        )}
      </div>

      <div className="hidden shrink-0 items-center gap-2 rounded-full border border-border bg-secondary/50 px-3 py-1.5 font-mono text-[11.5px] lg:flex">
        <span className="h-2 w-2 rounded-sm bg-gradient-to-br from-ai to-primary" />
        <span className="text-foreground">{model}</span>
        <span role="img" aria-label={analysisAvailable ? t('llmReady') : t('llmOffline')} title={analysisAvailable ? t('llmReady') : t('llmOffline')} className={analysisAvailable ? 'text-success' : 'text-caution'}>●</span>
      </div>

      <div className="flex min-w-0 shrink-0 items-center gap-1">{actions}</div>
    </header>
  )
}

function ViewSwitch({ view, onSwitch }: { view: DeckView; onSwitch: (view: DeckView) => void }) {
  const { t } = useTranslation('deck')
  return (
    <div className="flex shrink-0 rounded-full border border-border bg-secondary/50 p-[3px]">
      <SwitchButton active={view === 'scan'} onClick={() => onSwitch('scan')} icon={<ScanLine className="h-3.5 w-3.5" />}>
        {t('btnScan')}
      </SwitchButton>
      <SwitchButton active={view === 'chat'} onClick={() => onSwitch('chat')} icon={<Sparkles className="h-3.5 w-3.5" />}>
        {t('btnAgent')}
      </SwitchButton>
    </div>
  )
}

function SwitchButton({
  active,
  onClick,
  icon,
  children,
}: {
  active: boolean
  onClick: () => void
  icon: ReactNode
  children: ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={cn(
        'flex items-center gap-1.5 rounded-full px-3.5 py-1.5 text-[11.5px] font-semibold tracking-[0.04em] transition-colors',
        active
          ? 'bg-gradient-to-r from-ai to-primary text-ai-foreground shadow-glow-sm'
          : 'text-muted-foreground hover:text-foreground',
      )}
    >
      {icon}
      {children}
    </button>
  )
}

function ChatCrumb({ label }: { label?: string }) {
  const { t } = useTranslation('deck')
  return (
    <div className="ml-2 hidden min-w-0 items-center gap-1.5 px-1.5 py-1 font-mono text-xs lg:flex">
      <span className="text-muted-foreground/60">/</span>
      <span className="text-muted-foreground">{t('crumbAgents')}</span>
      <span className="text-muted-foreground/60">/</span>
      <span className="max-w-[220px] truncate font-semibold text-foreground">{label || t('chatConsole')}</span>
    </div>
  )
}

function HistoryCrumb({
  scan,
  scans = [],
  onSelectScan,
  onDeleteScan,
}: {
  scan: ScanJob | null
  scans?: ScanJob[]
  onSelectScan?: (s: ScanJob) => void
  onDeleteScan?: (s: ScanJob) => void
}) {
  const { t } = useTranslation('deck')
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onDoc = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [open])

  const statusDot = (s: string) =>
    s === 'running' || s === 'queued' ? 'bg-primary' : s === 'completed' ? 'bg-success' : s === 'failed' ? 'bg-destructive' : 'bg-muted-foreground/50'

  return (
    <div ref={ref} className="relative ml-2 hidden min-w-0 lg:block">
      <button
        onClick={() => setOpen((v) => !v)}
        className="flex min-w-0 items-center gap-1.5 rounded-md px-1.5 py-1 font-mono text-xs transition-colors hover:bg-secondary/50"
      >
        <span className="text-muted-foreground/60">/</span>
        <span className="text-muted-foreground">{t('crumbOperations')}</span>
        <span className="text-muted-foreground/60">/</span>
        <span className="max-w-[200px] truncate font-semibold text-foreground">{scan?.target || t('standby')}</span>
        <ChevronDown className={cn('h-3.5 w-3.5 text-muted-foreground/60 transition-transform', open && 'rotate-180')} />
      </button>
      {open && (
        <div className="absolute left-0 top-[calc(100%+6px)] z-50 max-h-[60vh] w-[320px] overflow-y-auto rounded-xl border border-border bg-popover p-1.5 shadow-elevated">
          {scans.length === 0 ? (
            <div className="px-3 py-4 text-center text-xs text-muted-foreground">{t('noScansYet')}</div>
          ) : (
            scans.map((s) => (
              <div
                key={s.id}
                className={cn(
                  'group flex items-center rounded-lg pr-1 transition-colors hover:bg-secondary/50',
                  s.id === scan?.id && 'bg-secondary/60',
                )}
              >
                <button
                  onClick={() => {
                    onSelectScan?.(s)
                    setOpen(false)
                  }}
                  className="flex min-w-0 flex-1 items-center gap-2 rounded-lg px-2.5 py-2 text-left"
                >
                  <span className={cn('h-2 w-2 shrink-0 rounded-full', statusDot(s.status))} />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-mono text-xs font-medium text-foreground">{s.target}</span>
                    <span className="block truncate font-mono text-[10px] text-muted-foreground">{s.mode} · {s.status}</span>
                  </span>
                </button>
                {onDeleteScan && (
                  <button
                    type="button"
                    onClick={(e) => {
                      e.stopPropagation()
                      onDeleteScan(s)
                    }}
                    aria-label={t('deleteScan')}
                    title={t('deleteScan')}
                    className="invisible shrink-0 rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive group-hover:visible"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                )}
              </div>
            ))
          )}
        </div>
      )}
    </div>
  )
}

