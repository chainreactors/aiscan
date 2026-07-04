import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Bot, Download, Plus, Target, X, Zap } from 'lucide-react'
import { cn } from '@aspect/theme'
import type { PoolAsset } from '../../api'
import { exportAssetsCSV } from '../../lib/asset-export'

interface AssetPoolProps {
  assets: PoolAsset[]
  scanning: boolean
  hasAgents: boolean
  onAdd: (raw: string) => void
  onRemove: (id: string) => void
  onScan: (target: string) => void
  onDispatch: (target: string) => void
}

/**
 * The asset pool — the recon deck's shared target inventory, and the single
 * panel used for it everywhere. Assets flow in from three sources (scan
 * results, agent recon, manual entry) and every row is one click from a local
 * scan (⚡) or an agent dispatch (🤖). It rides in the right-hand IntelRail on
 * both the scan deck and the agent console, so those two views share one panel
 * instead of the old split (main-column pool + rail "discovered" list).
 * See [[aiscan-web-redesign-direction]] · [[aiscan-asset-pool]].
 */
export default function AssetPool({ assets, scanning, hasAgents, onAdd, onRemove, onScan, onDispatch }: AssetPoolProps) {
  const { t } = useTranslation('deck')
  const [draft, setDraft] = useState('')

  const submit = () => {
    const v = draft.trim()
    if (!v) return
    onAdd(v)
    setDraft('')
  }

  return (
    <div className="p-5 border-b border-border">
      <h3 className="mb-4 flex items-center gap-2 font-display text-xs font-semibold uppercase tracking-[0.08em] text-muted-foreground">
        <Target className="h-3.5 w-3.5" />
        {t('assetPool')}
        <span className="ml-auto font-mono text-[10px] font-semibold tracking-[0.06em] text-ai">{assets.length}</span>
        {assets.length > 0 && (
          <button
            type="button"
            onClick={() => exportAssetsCSV(assets)}
            title={t('assetExport')}
            aria-label={t('assetExport')}
            className="grid h-5 w-5 place-items-center rounded border border-transparent text-muted-foreground/50 transition-colors hover:border-border hover:bg-secondary/60 hover:text-foreground"
          >
            <Download className="h-3 w-3" />
          </button>
        )}
      </h3>

      {/* manual add — a bare host / ip / url, or several separated by space/comma */}
      <div className="mb-3 flex items-center gap-2 rounded-lg border border-input bg-card py-1.5 pl-2.5 pr-1.5 transition focus-within:border-ai/50 focus-within:ring-1 focus-within:ring-ai/20">
        <Plus className="h-3.5 w-3.5 shrink-0 text-muted-foreground/70" />
        <input
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            // Guard against an IME composition Enter (confirming a pinyin
            // candidate) firing a submit — the primary locale is Chinese.
            if (e.key === 'Enter' && !e.nativeEvent.isComposing) {
              e.preventDefault()
              submit()
            }
          }}
          placeholder={t('assetAddPlaceholder')}
          aria-label={t('assetAddPlaceholder')}
          className="min-w-0 flex-1 bg-transparent font-mono text-[12px] text-foreground outline-none placeholder:font-sans placeholder:text-muted-foreground/55"
        />
        <button
          type="button"
          onClick={submit}
          disabled={!draft.trim()}
          className="shrink-0 rounded-md border border-border bg-secondary/50 px-2 py-1 font-mono text-[10.5px] font-semibold text-foreground transition-colors hover:bg-secondary disabled:cursor-not-allowed disabled:opacity-40"
        >
          {t('assetAdd')}
        </button>
      </div>

      {assets.length === 0 ? (
        <p className="py-6 text-center text-xs text-muted-foreground/70">{t('assetPoolEmpty')}</p>
      ) : (
        // Bounded scroll so a large pool (dozens of assets) can't crowd the rest
        // of the rail out of reach; the pool holds everything, this just clips.
        <div className="flex max-h-[18rem] flex-col gap-1 overflow-y-auto pr-1">
          {assets.map((a) => (
            <AssetRow
              key={a.id}
              asset={a}
              scanning={scanning}
              hasAgents={hasAgents}
              onRemove={onRemove}
              onScan={onScan}
              onDispatch={onDispatch}
            />
          ))}
        </div>
      )}
    </div>
  )
}

// Source is shown as a small colored dot (not a repeated text pill): when the
// pool is homogeneous, a column of identical "AGENT" words is pure noise. The
// full source name rides in the dot's title/aria-label.
const SOURCE_DOT: Record<string, string> = {
  agent: 'bg-ai',
  scan: 'bg-primary',
  manual: 'bg-muted-foreground/50',
}

function AssetRow({
  asset,
  scanning,
  hasAgents,
  onRemove,
  onScan,
  onDispatch,
}: {
  asset: PoolAsset
  scanning: boolean
  hasAgents: boolean
  onRemove: (id: string) => void
  onScan: (target: string) => void
  onDispatch: (target: string) => void
}) {
  const { t } = useTranslation('deck')
  const source = asset.source || 'manual'
  const dot = SOURCE_DOT[source] || SOURCE_DOT.manual
  const sourceLabel = source === 'agent' ? t('srcAgent') : source === 'scan' ? t('srcScan') : t('srcManual')

  return (
    <div className="group relative flex items-center gap-2 rounded-md border border-transparent px-1.5 py-1 transition-colors hover:border-border hover:bg-secondary/40">
      <span className={cn('h-1.5 w-1.5 shrink-0 rounded-full', dot)} title={sourceLabel} aria-label={sourceLabel} />
      <button
        type="button"
        onClick={() => onScan(asset.target)}
        disabled={scanning}
        title={t('assetScan')}
        className="min-w-0 flex-1 truncate text-left font-mono text-[11.5px] text-foreground transition-colors hover:text-primary disabled:cursor-not-allowed"
      >
        {asset.target}
      </button>
      {/* Actions ride in on hover/focus so the target keeps the full row width
          even in a narrow rail; the row itself already scans on click. */}
      <div className="absolute inset-y-0 right-0 hidden items-center gap-0.5 rounded-r-md bg-gradient-to-l from-card via-card to-transparent pl-6 pr-1 group-hover:flex group-focus-within:flex">
        <IconBtn label={t('assetScan')} tone="primary" disabled={scanning} onClick={() => onScan(asset.target)}>
          <Zap className="h-3 w-3" />
        </IconBtn>
        <IconBtn label={hasAgents ? t('assetDispatch') : t('assetDispatchNoAgent')} tone="ai" disabled={!hasAgents} onClick={() => onDispatch(asset.target)}>
          <Bot className="h-3 w-3" />
        </IconBtn>
        <IconBtn label={t('assetRemove')} tone="muted" onClick={() => onRemove(asset.id)}>
          <X className="h-3 w-3" />
        </IconBtn>
      </div>
    </div>
  )
}

function IconBtn({
  children,
  label,
  tone,
  disabled,
  onClick,
}: {
  children: ReactNode
  label: string
  tone: 'primary' | 'ai' | 'muted'
  disabled?: boolean
  onClick: () => void
}) {
  const toneClass =
    tone === 'primary'
      ? 'text-muted-foreground/60 hover:border-primary/40 hover:bg-primary/10 hover:text-primary'
      : tone === 'ai'
        ? 'text-muted-foreground/60 hover:border-ai/40 hover:bg-ai/10 hover:text-ai'
        : 'text-muted-foreground/50 hover:border-destructive/40 hover:bg-destructive/10 hover:text-destructive'
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      title={label}
      aria-label={label}
      className={cn(
        'grid h-6 w-6 place-items-center rounded-md border border-transparent transition-colors disabled:cursor-not-allowed disabled:opacity-30',
        toneClass,
      )}
    >
      {children}
    </button>
  )
}
