import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ArrowRight, Cpu, Network, Search, Sparkles } from 'lucide-react'
import { cn } from '@aspect/theme'
import type { AgentInfo, PoolAsset } from '../../api'
import AssetPool from './AssetPool'

/** Glanceable status of the current agent turn, in lieu of a transcript feed. */
export interface ReasoningSummary {
  focus: string
  nowTool: string
  nowTarget: string
  toolCount: number
  targetCount: number
  streaming: boolean
}

interface IntelRailProps {
  /** Summary mode (agent console): a status card instead of a transcript feed. */
  reasoningSummary?: ReasoningSummary
  agents: AgentInfo[]
  providerLabel: string
  scanning: boolean
  onCommand?: (text: string) => void
  onOpenNode: (agentID: string) => void
  /** Cortex reasoning feed; hide it where it adds no signal (e.g. the scan deck). */
  showReasoning?: boolean
  /** The command box duplicates the chat input; hide it on the agent console. */
  showCommand?: boolean
  /** Shared asset pool — the same panel rides here on both the scan deck and
   *  the agent console. Provide the assets + handlers to render it. */
  assets?: PoolAsset[]
  onAddAsset?: (raw: string) => void
  onRemoveAsset?: (id: string) => void
  onScanAsset?: (target: string) => void
  onDispatchAsset?: (target: string) => void
  hasAgents?: boolean
  /** Scan-in-progress flag for the pool's ⚡ buttons; falls back to `scanning`
   *  (on the agent console `scanning` means "agent thinking", not "scan running"). */
  scanBusy?: boolean
}

export default function IntelRail({ reasoningSummary, agents, providerLabel, scanning, onCommand, onOpenNode, showReasoning = true, showCommand = true, assets, onAddAsset, onRemoveAsset, onScanAsset, onDispatchAsset, hasAgents, scanBusy }: IntelRailProps) {
  const { t } = useTranslation('deck')

  return (
    <aside className="flex h-full min-h-0 w-full shrink-0 flex-col border-l border-border bg-card/70 backdrop-blur-md">
      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
        {/* Cortex reasoning */}
        {showReasoning && (
          <Panel icon={<Cpu className="h-3.5 w-3.5" />} title={t('cortexReasoning')} badge={providerLabel}>
            {reasoningSummary ? (
              <ReasoningSummaryView summary={reasoningSummary} scanning={scanning} />
            ) : (
              <p className="text-xs text-muted-foreground/70">{scanning ? t('awaitingInference') : t('noReasoning')}</p>
            )}
          </Panel>
        )}

        {/* Asset pool — shared target inventory (scan / agent / manual), one
            click from a local scan or an agent dispatch. Same panel on the
            scan deck and the agent console. */}
        {assets && onAddAsset && onRemoveAsset && onScanAsset && onDispatchAsset && (
          <AssetPool
            assets={assets}
            scanning={scanBusy ?? scanning}
            hasAgents={!!hasAgents}
            onAdd={onAddAsset}
            onRemove={onRemoveAsset}
            onScan={onScanAsset}
            onDispatch={onDispatchAsset}
          />
        )}

        {/* Agent nodes */}
        <NodesPanel agents={agents} onOpenNode={onOpenNode} last />
      </div>

      {showCommand && onCommand && <CommandBox onCommand={onCommand} />}
    </aside>
  )
}

const NODE_FILTER_THRESHOLD = 6

function nodeName(agent: AgentInfo): string {
  return agent.identity?.node_name || agent.name
}

function NodesPanel({ agents, onOpenNode, last }: { agents: AgentInfo[]; onOpenNode: (agentID: string) => void; last?: boolean }) {
  const { t } = useTranslation('deck')
  const [query, setQuery] = useState('')

  const spaces = useMemo(() => {
    const set = new Set<string>()
    for (const a of agents) set.add(a.identity?.space || 'default')
    return set
  }, [agents])
  const multiSpace = spaces.size > 1
  const space = agents.find((a) => a.identity?.space)?.identity?.space || 'default'
  const activeNodes = agents.filter((a) => a.busy).length
  const many = agents.length > NODE_FILTER_THRESHOLD

  // Busy nodes first, then by running tools desc, then name — so the nodes
  // actually doing work stay visible at the top without scrolling.
  const sorted = useMemo(
    () =>
      [...agents].sort((a, b) => {
        if (a.busy !== b.busy) return a.busy ? -1 : 1
        const ra = a.stats?.running_tools ?? 0
        const rb = b.stats?.running_tools ?? 0
        if (ra !== rb) return rb - ra
        return nodeName(a).localeCompare(nodeName(b))
      }),
    [agents],
  )
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return sorted
    return sorted.filter((a) =>
      `${nodeName(a)} ${a.identity?.model || ''} ${a.identity?.provider || ''} ${a.identity?.space || ''}`
        .toLowerCase()
        .includes(q),
    )
  }, [sorted, query])

  const badge = agents.length
    ? activeNodes
      ? t('nodesSummary', { active: activeNodes, total: agents.length })
      : t('nodesOnline', { count: agents.length })
    : undefined

  return (
    <Panel icon={<Network className="h-3.5 w-3.5" />} title={t('ioaNodes')} badge={badge} last={last}>
      <div className="mb-3 font-mono text-[11px] text-muted-foreground">
        {multiSpace ? (
          t('spacesCount', { count: spaces.size })
        ) : (
          <>
            {t('space')} <b className="text-foreground">{space}</b>
          </>
        )}
      </div>

      {many && (
        <div className="relative mb-2">
          <Search className="pointer-events-none absolute left-2 top-1/2 h-3 w-3 -translate-y-1/2 text-muted-foreground/60" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t('filterNodes')}
            aria-label={t('filterNodes')}
            className="w-full rounded-md border border-input bg-card py-1.5 pl-7 pr-2 text-[11px] text-foreground outline-none placeholder:text-muted-foreground/50 focus:border-ai/50 focus:ring-1 focus:ring-ai/20"
          />
        </div>
      )}

      {agents.length === 0 ? (
        <p className="text-xs text-muted-foreground/70">{t('noNodes')}</p>
      ) : filtered.length === 0 ? (
        <p className="text-xs text-muted-foreground/70">{t('noMatch')}</p>
      ) : (
        <div className={cn('flex flex-col gap-2', many && 'max-h-[19rem] gap-1.5 overflow-y-auto pr-1')}>
          {filtered.map((a) => (
            <NodeRow key={a.id} agent={a} onOpen={onOpenNode} compact={many} showSpace={multiSpace} />
          ))}
        </div>
      )}
    </Panel>
  )
}

function NodeRow({
  agent,
  onOpen,
  compact,
  showSpace,
}: {
  agent: AgentInfo
  onOpen: (agentID: string) => void
  compact?: boolean
  showSpace?: boolean
}) {
  const { t } = useTranslation('deck')
  const name = nodeName(agent)
  const model = agent.identity?.model || agent.identity?.provider || '—'
  const tokens = agent.stats?.total_tokens ?? 0
  const running = agent.stats?.running_tools ?? 0
  const space = agent.identity?.space || 'default'

  return (
    <button
      type="button"
      onClick={() => onOpen(agent.id)}
      title={t('openConsole')}
      aria-label={`${name} — ${t('openConsole')}`}
      className={cn(
        'flex w-full items-center gap-3 rounded-lg border border-border bg-secondary/40 text-left transition-colors hover:border-ai/40 hover:bg-secondary/70 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ai/40',
        compact ? 'p-2' : 'p-3',
      )}
    >
      <span
        className={cn(
          'h-2 w-2 shrink-0 rounded-full',
          agent.busy ? 'breathe bg-primary shadow-[0_0_9px_hsl(var(--primary))]' : 'bg-muted-foreground/50',
        )}
      />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1.5">
          <span className="truncate font-mono text-xs font-semibold text-foreground">{name}</span>
          {showSpace && (
            <span className="shrink-0 rounded bg-muted px-1 py-px font-mono text-[9px] text-muted-foreground">
              {space}
            </span>
          )}
        </div>
        <div className="mt-px flex items-center gap-2 text-[10.5px] text-muted-foreground">
          <span className="font-mono text-ai">{model}</span>
          <span>{agent.busy ? t('busy') : t('idle')}</span>
          {compact && running > 0 && <span className="text-foreground">▶{running}</span>}
        </div>
        {!compact && (
          <span
            className={cn(
              'mt-1 inline-block rounded border border-border bg-card px-1.5 py-px font-mono text-[10px]',
              running > 0 ? 'text-foreground' : 'text-muted-foreground/60',
            )}
          >
            {running > 0 ? t('runningTools', { count: running }) : t('awaitingTask')}
          </span>
        )}
      </div>
      <div className="shrink-0 text-right font-mono text-[10.5px] text-muted-foreground/70">
        {formatTokens(tokens)}
        <br />
        <span className="opacity-60">{t('tokens')}</span>
      </div>
    </button>
  )
}

function CommandBox({ onCommand }: { onCommand: (text: string) => void }) {
  const { t } = useTranslation('deck')
  const [value, setValue] = useState('')
  const submit = () => {
    const v = value.trim()
    if (!v) return
    onCommand(v)
    setValue('')
  }
  return (
    <div className="border-t border-border bg-gradient-to-b from-transparent to-ai/[0.05] p-4">
      <div className="flex items-center gap-2 rounded-xl border border-input bg-card py-1.5 pl-3 pr-1.5 transition focus-within:border-ai focus-within:ring-2 focus-within:ring-ai/15">
        <Sparkles className="h-4 w-4 shrink-0 text-ai" />
        <input
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => {
            // Don't submit on the Enter that confirms an IME candidate (zh).
            if (e.key === 'Enter' && !e.nativeEvent.isComposing) submit()
          }}
          placeholder={t('commandPlaceholder')}
          aria-label={t('commandAria')}
          className="min-w-0 flex-1 bg-transparent text-[13px] text-foreground outline-none placeholder:text-muted-foreground/60"
        />
        <button
          type="button"
          onClick={submit}
          aria-label={t('send')}
          className="grid h-7 w-7 shrink-0 place-items-center rounded-lg bg-gradient-to-r from-ai to-primary text-ai-foreground"
        >
          <ArrowRight className="h-3.5 w-3.5" />
        </button>
      </div>
      <div className="mt-2 flex items-center gap-1.5 text-[10px] text-muted-foreground/70">
        <kbd className="rounded border border-input px-1.5 py-px font-mono text-[9.5px] text-muted-foreground">↵</kbd>
        {t('commandHint')}
      </div>
    </div>
  )
}

function ReasoningSummaryView({ summary, scanning }: { summary: ReasoningSummary; scanning: boolean }) {
  const { t } = useTranslation('deck')
  const { focus, nowTool, nowTarget, toolCount, targetCount } = summary

  if (!focus && !nowTool && toolCount === 0) {
    return <p className="text-xs text-muted-foreground/70">{scanning ? t('awaitingInference') : t('noReasoning')}</p>
  }

  return (
    <div className="flex flex-col gap-3.5">
      {focus && (
        <div>
          <div className="mb-1.5 font-mono text-[10px] uppercase tracking-[0.08em] text-muted-foreground">{t('summaryFocus')}</div>
          <p className="rounded-lg border border-ai/20 bg-gradient-to-r from-ai/10 to-ai/[0.04] px-3 py-2 text-[12.5px] leading-relaxed text-foreground">
            {focus}
            {scanning && <span className="blink ml-0.5 text-ai">▍</span>}
          </p>
        </div>
      )}

      {nowTool && (
        <div className="flex items-center gap-2 text-[12.5px]">
          <span
            className={cn(
              'h-1.5 w-1.5 shrink-0 rounded-full',
              scanning ? 'breathe bg-primary shadow-[0_0_8px_hsl(var(--primary))]' : 'bg-muted-foreground/50',
            )}
          />
          <span className="shrink-0 font-mono text-[10px] uppercase tracking-[0.06em] text-muted-foreground">{t('summaryNow')}</span>
          <span className="font-mono text-ai">{nowTool}</span>
          {nowTarget && <span className="truncate text-muted-foreground">— {nowTarget}</span>}
        </div>
      )}

      <div className="border-t border-border pt-2.5 font-mono text-[11px] text-muted-foreground">
        {t('summaryStats', { tools: toolCount, targets: targetCount })}
      </div>
    </div>
  )
}

function Panel({ icon, title, badge, last, children }: { icon: React.ReactNode; title: string; badge?: string; last?: boolean; children: React.ReactNode }) {
  return (
    <div className={cn('p-5', !last && 'border-b border-border')}>
      <h3 className="mb-4 flex items-center gap-2 font-display text-xs font-semibold uppercase tracking-[0.08em] text-muted-foreground">
        {icon}
        {title}
        {badge && <span className="ml-auto font-mono text-[10px] font-semibold tracking-[0.06em] text-ai">{badge}</span>}
      </h3>
      {children}
    </div>
  )
}

function formatTokens(n: number): string {
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`
  return String(n)
}
