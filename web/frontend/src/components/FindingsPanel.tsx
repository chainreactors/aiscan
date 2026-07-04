import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { AlertCircle, CheckCircle2, Crosshair, ExternalLink, Key, Radar, Shield } from 'lucide-react'
import type { ScanResult } from '../api'
import { buildFindings, findingTargetURL, PRIORITY_ORDER, type FindingItem, type FindingPriority } from '../lib/scan-result'
import { severityTone } from '../lib/tones'
import { cn } from '@aspect/theme'
import { MarkdownContent } from '@aspect/markdown'

interface FindingsPanelProps {
  result: ScanResult
}

type FilterValue = 'all' | FindingPriority | 'ai_verified'

export default function FindingsPanel({ result }: FindingsPanelProps) {
  const { t } = useTranslation('findings')
  const findings = useMemo(() => buildFindings(result), [result])
  const [filter, setFilter] = useState<FilterValue>('all')

  const filtered = useMemo(() => {
    if (filter === 'all') return findings
    if (filter === 'ai_verified') return findings.filter(f => f.source === 'verify' && f.status === 'confirmed')
    return findings.filter(f => f.priority === filter)
  }, [findings, filter])

  const grouped = useMemo(() => {
    const groups: Record<string, FindingItem[]> = {}
    for (const f of filtered) {
      ;(groups[f.priority] ||= []).push(f)
    }
    return groups
  }, [filtered])

  if (findings.length === 0) {
    return (
      <div className="py-12 text-center text-sm text-muted-foreground">
        <Shield className="mx-auto mb-3 h-8 w-8 opacity-40" />
        <p>{t('noFindings')}</p>
      </div>
    )
  }

  const aiCount = findings.filter(f => f.source === 'verify' && f.status === 'confirmed').length

  return (
    <div className="space-y-4 animate-fade-in">
      <div className="flex flex-wrap items-center gap-1.5">
        <FilterChip active={filter === 'all'} onClick={() => setFilter('all')}>
          {t('allCount', { count: findings.length })}
        </FilterChip>
        {PRIORITY_ORDER.map(p => {
          const count = findings.filter(f => f.priority === p).length
          if (count === 0) return null
          return (
            <FilterChip key={p} active={filter === p} onClick={() => setFilter(p)}>
              <span className={cn('inline-block h-2 w-2 rounded-full', severityTone[p].dot)} />
              {t(`severity_${p}`)} ({count})
            </FilterChip>
          )
        })}
        {aiCount > 0 && (
          <FilterChip active={filter === 'ai_verified'} onClick={() => setFilter('ai_verified')}>
            <CheckCircle2 className="h-3 w-3 text-success" />
            {t('aiVerifiedCount', { count: aiCount })}
          </FilterChip>
        )}
      </div>

      {PRIORITY_ORDER.map(priority => {
        const items = grouped[priority]
        if (!items || items.length === 0) return null
        const tone = severityTone[priority]
        return (
          <div key={priority} className={cn('rounded-lg border', tone.border)}>
            <div className={cn('mono-label flex items-center gap-2 border-b px-4 py-2.5 text-[11px]', tone.border, tone.bg, tone.text)}>
              <span className={cn('h-2.5 w-2.5 rounded-full', tone.dot)} />
              {t(`severity_${priority}`)} ({items.length})
            </div>
            <div className="divide-y divide-border/50">
              {items.map(item => (
                <FindingCard key={item.id} item={item} />
              ))}
            </div>
          </div>
        )
      })}
    </div>
  )
}

function FindingCard({ item }: { item: FindingItem }) {
  const { t } = useTranslation('findings')
  const [expanded, setExpanded] = useState(false)

  return (
    <div className="p-3 text-xs">
      <div className="flex items-start gap-2">
        <FindingKindIcon kind={item.kind} />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium text-foreground break-words">{item.title}</span>
            <span className="rounded bg-secondary px-1.5 py-0.5 text-[10px] text-muted-foreground">{item.kind}</span>
            <FindingSourceBadge source={item.source} status={item.status} />
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-2 text-[11px] text-muted-foreground">
            <FindingTarget target={item.target} />
            {item.tags.slice(0, 5).map(tag => (
              <span key={tag} className="rounded bg-secondary/80 px-1.5 py-0.5 text-[10px]">{tag}</span>
            ))}
            {item.tags.length > 5 && (
              <span className="text-[10px]">+{item.tags.length - 5}</span>
            )}
          </div>
        </div>
      </div>

      {item.detail && (
        <div className="mt-2">
          {!expanded ? (
            <button
              type="button"
              className="text-[11px] text-ai hover:underline"
              onClick={() => setExpanded(true)}
            >
              {t('showAIAnalysis')}
            </button>
          ) : (
            <div className="mt-1 rounded-md border-l-4 border-l-ai bg-ai/5 p-3">
              <div className="mb-1.5 flex items-center justify-between">
                <span className="mono-label text-ai">
                  {item.source === 'verify' ? t('aiVerification') : item.source === 'sniper' ? t('cveIntelligence') : t('analysis')}
                </span>
                <button
                  type="button"
                  className="text-[10px] text-muted-foreground hover:text-foreground"
                  onClick={() => setExpanded(false)}
                >
                  {t('hide')}
                </button>
              </div>
              <div className="max-h-72 overflow-auto text-muted-foreground">
                <MarkdownContent content={item.detail} compact muted />
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function FindingTarget({ target }: { target: string }) {
  const { t } = useTranslation('findings')
  const href = findingTargetURL(target)
  if (!href) {
    return <span className="break-all font-mono">{target}</span>
  }
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer noopener"
      title={target}
      aria-label={t('openTargetUrl', { url: target })}
      className="inline-flex items-center gap-1 break-all font-mono text-primary/90 underline-offset-2 hover:text-primary hover:underline"
    >
      {target}
      <ExternalLink className="h-3 w-3 shrink-0 opacity-60" />
    </a>
  )
}

function FindingKindIcon({ kind }: { kind: FindingItem['kind'] }) {
  switch (kind) {
    case 'vuln': return <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-destructive" />
    case 'weakpass': return <Key className="mt-0.5 h-3.5 w-3.5 shrink-0 text-caution" />
    case 'fingerprint': return <Shield className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
    default: return <Shield className="mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground" />
  }
}

function FindingSourceBadge({ source, status }: { source?: string; status?: string }) {
  const { t } = useTranslation('findings')
  if (source === 'verify' && status === 'confirmed') {
    return (
      <span className="inline-flex items-center gap-1 rounded bg-success/12 px-1.5 py-0.5 text-[10px] font-medium text-success">
        <CheckCircle2 className="h-3 w-3" />{t('aiVerified')}
      </span>
    )
  }
  if (source === 'verify' && status === 'not_confirmed') {
    return <span className="rounded bg-secondary px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">{t('notConfirmed')}</span>
  }
  if (source === 'verify' && status === 'inconclusive') {
    return <span className="rounded bg-warning/12 px-1.5 py-0.5 text-[10px] font-medium text-warning">{t('inconclusive')}</span>
  }
  if (source === 'sniper') {
    return (
      <span className="inline-flex items-center gap-1 rounded bg-destructive/12 px-1.5 py-0.5 text-[10px] font-medium text-destructive">
        <Crosshair className="h-3 w-3" />{t('cveIntel')}
      </span>
    )
  }
  if (source === 'deep') {
    return (
      <span className="inline-flex items-center gap-1 rounded bg-warning/12 px-1.5 py-0.5 text-[10px] font-medium text-warning">
        <Radar className="h-3 w-3" />{t('deepTest')}
      </span>
    )
  }
  return null
}

function FilterChip({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-[11px] font-medium transition-colors',
        active
          ? 'border-primary/40 bg-primary/15 text-primary'
          : 'border-border bg-background text-muted-foreground hover:border-primary/30 hover:text-foreground',
      )}
    >
      {children}
    </button>
  )
}
