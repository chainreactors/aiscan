import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { Circle, Monitor, RefreshCw, Search, X } from 'lucide-react'
import { listAgents } from '../api'
import type { AgentInfo } from '../api'
// Lazy — same @xterm chunk App splits; a static import here would pull it back
// into the first-paint bundle.
const AgentTerminal = lazy(() => import('./terminal'))
import { cn } from '@aspect/theme'
import { Spinner } from '@aspect/ui'
import { usePolling } from '../hooks/usePolling'
import { useDialogA11y } from '../hooks/useDialogA11y'

interface AgentPanelProps {
  open: boolean
  /** When opened from a deck node click, focus this agent's console. */
  focusAgentID?: string
  onClose: () => void
}

export default function AgentPanel({ open, focusAgentID, onClose }: AgentPanelProps) {
  const { t } = useTranslation('agent')
  const { agents, error, loading, refresh, selected, selectedID, setSelectedID } = useAgentDirectory(open, focusAgentID)
  const showAgentList = agents.length > 1

  // Esc-to-close + focus trap/restore (parity with the Radix-backed ConfirmDialog).
  const panelRef = useRef<HTMLDivElement>(null)
  useDialogA11y(open, onClose, panelRef)

  if (!open) return null

  return (
    <div onClick={onClose} className="fixed inset-0 z-50 flex justify-end bg-black/50 backdrop-blur-md animate-in fade-in duration-200">
      <div ref={panelRef} tabIndex={-1} onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true" aria-labelledby="agent-panel-title" className="flex h-full w-full max-w-7xl flex-col border-l border-border/70 bg-card shadow-elevated animate-in slide-in-from-right-4 duration-200 focus:outline-none">
        <div className="flex h-12 shrink-0 items-center justify-between border-b border-border/60 px-4">
          <div className="flex min-w-0 items-center gap-3">
            <Monitor className="h-4 w-4 shrink-0 text-primary" />
            <div className="min-w-0">
              <div className="flex min-w-0 items-center gap-2">
                <span id="agent-panel-title" className="text-sm font-medium text-foreground">{t('agentConsole')}</span>
                <span className="rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
                  {agents.length}
                </span>
              </div>
              <div className="truncate text-xs text-muted-foreground" title={selected ? agentDetails(selected) : undefined}>
                {selected ? `${selected.name} · ${selected.busy ? t('busy') : t('idle')}` : t('noAgentSelected')}
              </div>
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="rounded-md p-1 text-muted-foreground hover:bg-accent hover:text-foreground"
            aria-label={t('closeAgents')}
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="min-h-0 flex-1">
          {loading ? (
            <div className="flex h-32 items-center justify-center text-muted-foreground">
              <Spinner className="h-5 w-5" />
            </div>
          ) : error ? (
            <div className="m-4 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {error}
            </div>
          ) : agents.length === 0 ? (
            <div className="flex h-full flex-col items-center justify-center gap-2 text-muted-foreground">
              <Monitor className="h-8 w-8 opacity-20" />
              <p className="text-sm">{t('noAgentsConnected')}</p>
            </div>
          ) : (
            <div className="flex h-full min-h-0 flex-col lg:flex-row">
              {showAgentList && (
                <AgentList
                  agents={agents}
                  selectedID={selectedID}
                  onRefresh={() => refresh(true)}
                  onSelect={setSelectedID}
                />
              )}
              <section className="flex min-h-0 min-w-0 flex-1 flex-col">
                {selected && (
                  <Suspense fallback={<div className="flex-1" />}>
                    <AgentTerminal agent={selected} />
                  </Suspense>
                )}
              </section>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function useAgentDirectory(open: boolean, focusAgentID?: string) {
  const { t } = useTranslation('agent')
  const [agents, setAgents] = useState<AgentInfo[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [selectedID, setSelectedID] = useState('')

  // Focus a specific node when the panel is opened from a deck node click.
  // Apply it exactly ONCE per open/focus request: `agents` is in the deps only
  // so we can wait for the roster to load, but the 5s poll replaces `agents`
  // every tick — without this guard the effect would re-fire and yank the
  // selection back to the focused node, defeating any manual agent switch.
  const focusAppliedRef = useRef(false)
  useEffect(() => {
    focusAppliedRef.current = false
  }, [open, focusAgentID])
  useEffect(() => {
    if (focusAppliedRef.current) return
    if (open && focusAgentID && agents.some((a) => a.id === focusAgentID)) {
      setSelectedID(focusAgentID)
      focusAppliedRef.current = true
    }
  }, [open, focusAgentID, agents])

  const refresh = useCallback((silent = false) => {
    if (!silent) {
      setLoading(true)
      setError('')
    }
    return listAgents()
      .then((items) => {
        setAgents(items)
        setSelectedID((current) => items.some((agent) => agent.id === current) ? current : items[0]?.id || '')
      })
      .catch((err: Error) => {
        if (!silent) setError(err.message || t('failedToLoadAgents'))
      })
      .finally(() => {
        if (!silent) setLoading(false)
      })
  }, [t])

  useEffect(() => {
    if (!open) return
    refresh()
  }, [open, refresh])

  // Silent roster poll while the console is open — paused when the tab is hidden.
  usePolling(() => refresh(true), 5000, open)

  const selected = agents.find((agent) => agent.id === selectedID) || agents[0] || null

  return { agents, error, loading, refresh, selected, selectedID, setSelectedID }
}

function AgentList({
  agents,
  onRefresh,
  onSelect,
  selectedID,
}: {
  agents: AgentInfo[]
  onRefresh: () => void
  onSelect: (id: string) => void
  selectedID: string
}) {
  const { t } = useTranslation('agent')
  const [query, setQuery] = useState('')

  // Busy agents first, then alphabetical — keeps active nodes at the top.
  const sorted = useMemo(
    () =>
      [...agents].sort((a, b) => {
        if (a.busy !== b.busy) return a.busy ? -1 : 1
        return (a.name || '').localeCompare(b.name || '')
      }),
    [agents],
  )
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return sorted
    return sorted.filter((a) =>
      `${a.name} ${a.identity?.model || ''} ${a.identity?.provider || ''} ${a.identity?.hostname || ''}`
        .toLowerCase()
        .includes(q),
    )
  }, [sorted, query])
  const busy = agents.filter((a) => a.busy).length
  const showFilter = agents.length > 6

  return (
    <aside className="flex max-h-52 w-full shrink-0 flex-col border-b border-border lg:max-h-none lg:w-64 lg:border-b-0 lg:border-r">
      <div className="flex h-10 items-center justify-between border-b border-border px-3">
        <span className="text-xs font-medium uppercase text-muted-foreground">
          {t('agents')}
          <span className="ml-1.5 font-mono text-[10px] normal-case text-muted-foreground/60">
            {busy}/{agents.length}
          </span>
        </span>
        <button
          type="button"
          onClick={onRefresh}
          className="rounded-md p-1 text-muted-foreground hover:bg-accent hover:text-foreground"
          aria-label={t('refreshAgents')}
        >
          <RefreshCw className="h-3.5 w-3.5" />
        </button>
      </div>
      {showFilter && (
        <div className="border-b border-border p-2">
          <div className="relative">
            <Search className="pointer-events-none absolute left-2 top-1/2 h-3 w-3 -translate-y-1/2 text-muted-foreground/60" />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={t('filterAgents')}
              aria-label={t('filterAgents')}
              className="w-full rounded-md border border-input bg-background py-1.5 pl-7 pr-2 text-xs text-foreground outline-none placeholder:text-muted-foreground/50 focus:border-primary/50 focus:ring-1 focus:ring-primary/20"
            />
          </div>
        </div>
      )}
      <div className="min-h-0 flex-1 overflow-auto p-2">
        {filtered.length === 0 ? (
          <p className="px-2 py-4 text-center text-xs text-muted-foreground/70">{t('noMatchingAgents')}</p>
        ) : (
          filtered.map((agent) => (
            <button
              key={agent.id}
              type="button"
              onClick={() => onSelect(agent.id)}
              title={agentDetails(agent)}
              className={cn(
                'mb-1 flex w-full items-start gap-2 rounded-md px-2 py-2 text-left transition-colors',
                selectedID === agent.id
                  ? 'bg-primary/10 text-foreground'
                  : 'text-muted-foreground hover:bg-accent hover:text-foreground',
              )}
            >
              <Circle
                className={cn(
                  'mt-1 h-2.5 w-2.5 shrink-0 fill-current',
                  agent.busy ? 'text-warning' : 'text-primary',
                )}
              />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm font-medium">{agent.name}</span>
                <span className="mt-0.5 block truncate text-xs">
                  {agent.busy ? t('busy') : t('idle')} · {formatRelativeTime(agent.connected_at, t)}
                </span>
              </span>
            </button>
          ))
        )}
      </div>
    </aside>
  )
}

function agentDetails(agent: AgentInfo) {
  const identity = agent.identity || {}
  const stats = agent.stats || {}
  const parts = [
    `name: ${agent.name}`,
    `id: ${agent.id}`,
    `state: ${agent.busy ? 'busy' : 'idle'}`,
    `connected: ${formatDateTime(agent.connected_at)}`,
    identity.hostname ? `host: ${identity.hostname}` : '',
    identity.username ? `user: ${identity.username}` : '',
    identity.working_dir ? `cwd: ${identity.working_dir}` : '',
    identity.os || identity.arch ? `runtime: ${[identity.os, identity.arch].filter(Boolean).join('/')}` : '',
    identity.pid ? `pid: ${identity.pid}` : '',
    identity.provider || identity.model ? `llm: ${[identity.provider, identity.model].filter(Boolean).join(' / ')}` : '',
    agent.commands?.length ? `commands: ${agent.commands.join(', ')}` : '',
    identity.capabilities?.length ? `capabilities: ${identity.capabilities.join(', ')}` : '',
    typeof stats.turns === 'number' ? `turns: ${stats.turns}` : '',
    typeof stats.tool_calls === 'number' ? `tool calls: ${stats.tool_calls}` : '',
    typeof stats.total_tokens === 'number' ? `tokens: ${stats.total_tokens}` : '',
  ]
  return parts.filter(Boolean).join('\n')
}

function formatDateTime(iso: string) {
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

function formatRelativeTime(iso: string, t: TFunction<'agent'>): string {
  try {
    const diff = Date.now() - new Date(iso).getTime()
    const mins = Math.floor(diff / 60000)
    if (mins < 1) return t('justNow')
    if (mins < 60) return t('minutesAgo', { count: mins })
    const hours = Math.floor(mins / 60)
    if (hours < 24) return t('hoursAgo', { count: hours })
    return t('daysAgo', { count: Math.floor(hours / 24) })
  } catch {
    return ''
  }
}
