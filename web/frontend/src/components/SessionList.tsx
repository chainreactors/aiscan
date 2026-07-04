import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useTranslation } from 'react-i18next'
import {
  PanelLeftClose, PanelLeft,
  MessageSquare, Plus, Trash2, Circle,
  ChevronDown, ChevronRight, Monitor, Terminal,
  MonitorPlay, Loader2, AlertTriangle, Unplug,
} from 'lucide-react'
import { Button, Tooltip, TooltipTrigger, TooltipContent } from '@aspect/ui'
import { cn } from '@aspect/theme'
import { launchLocalAgent, listLocalAgents, stopLocalAgent } from '../api'
import type { AgentInfo, ChatSession, LocalAgentView } from '../api'
import { agentActivity } from '../lib/agentActivity'
import { agentMatchesSession } from '../lib/session-agent'
import { usePolling } from '../hooks/usePolling'
import i18n from '../i18n'

interface Props {
  open: boolean
  onToggle: () => void
  agents?: AgentInfo[]
  sessions?: ChatSession[]
  activeSessionID: string | null
  selectedAgentID: string | null
  terminalAgentID: string | null
  onSelectAgent: (id: string) => void
  onSelectSession: (id: string) => void
  onCreateSession: (agentID: string) => void
  onDeleteSession: (id: string) => void
  onOpenTerminal: (agentID: string) => void
}

export default function SessionList({
  open, onToggle, agents = [], sessions = [],
  activeSessionID, selectedAgentID, terminalAgentID,
  onSelectAgent, onSelectSession, onCreateSession, onDeleteSession, onOpenTerminal,
}: Props) {
  const { t } = useTranslation('sidebar')
  // Attach each session to a connected agent by id-or-name (see
  // agentMatchesSession — the hub re-mints agent ids on reconnect, so match the
  // stable name too). Whatever no live agent claims is "orphaned": its bound
  // node is offline. Sessions persist server-side, so without this those
  // sessions would silently drop out of the sidebar — which nests sessions
  // under live agents — even though their transcripts are still openable.
  // Group the orphans by their bound agent name so they get a dedicated,
  // read-only "offline" section below instead of vanishing.
  const { groups, orphanGroups } = useMemo(() => {
    const claimed = new Set<string>()
    const groups = agents.map((agent) => {
      const own = sessions.filter((s) => !claimed.has(s.id) && agentMatchesSession(agent, s))
      own.forEach((s) => claimed.add(s.id))
      return { agent, sessions: own }
    })
    const orphanMap = new Map<string, ChatSession[]>()
    for (const s of sessions) {
      if (claimed.has(s.id)) continue
      const key = s.agent_name || s.agent_id || 'unknown'
      const list = orphanMap.get(key) || []
      list.push(s)
      orphanMap.set(key, list)
    }
    const orphanGroups = [...orphanMap.entries()]
      .map(([name, list]) => ({ name, sessions: list }))
      .sort((a, b) => a.name.localeCompare(b.name))
    return { groups, orphanGroups }
  }, [agents, sessions])

  return (
    <>
      {open && (
        <button
          type="button"
          aria-label={t('closeSidebarOverlay')}
          onClick={onToggle}
          className="fixed inset-0 z-30 bg-background/60 backdrop-blur-[1px] md:hidden"
        />
      )}
      <aside
        className={cn(
          'flex flex-col border-r border-border bg-card/95 backdrop-blur-sm transition-all duration-200 ease-in-out shrink-0 md:bg-card/50',
          open
            ? 'fixed inset-y-0 left-0 z-40 w-48 shadow-elevated md:relative md:inset-auto md:z-auto md:shadow-none'
            : 'w-12',
        )}
      >
        {/* Header */}
        <div className={cn('flex items-center border-b border-border/60', open ? 'p-3 gap-2.5' : 'p-2 flex-col gap-2')}>
          {open ? (
            <>
              <div className="flex-1 min-w-0">
                <div className="mono-label text-muted-foreground/70">{t('tagline')}</div>
              </div>
              <LocalAgentControl />
              <Button variant="ghost" size="icon" onClick={onToggle} className="h-7 w-7 text-muted-foreground" aria-label={t('collapseSidebar')}>
                <PanelLeftClose className="w-4 h-4" />
              </Button>
            </>
          ) : (
            <Tooltip>
              <TooltipTrigger asChild>
                <button type="button" onClick={onToggle} aria-label={t('expandSidebar')} className="flex h-8 w-8 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-accent hover:text-foreground">
                  <PanelLeft className="h-4 w-4" />
                </button>
              </TooltipTrigger>
              <TooltipContent side="right">{t('expandSidebar')}</TooltipContent>
            </Tooltip>
          )}
        </div>

        {/* Content */}
        {open ? (
          <div className="flex-1 overflow-auto p-2 animate-fade-in">
            {agents.length === 0 && orphanGroups.length === 0 ? (
              <div className="flex flex-col items-center justify-center py-8 text-center">
                <Monitor className="h-8 w-8 text-muted-foreground/20" />
                <p className="mt-2 text-xs text-muted-foreground">{t('noAgentsConnected')}</p>
                <p className="mt-1 text-[10px] text-muted-foreground/60">{t('startAgentToBegin')}</p>
              </div>
            ) : (
              <div className="space-y-1">
                {/* No live agents, but orphaned sessions remain — keep the
                    "launch an agent" nudge as a slim banner so the guidance
                    isn't lost now that the full empty state is suppressed. */}
                {agents.length === 0 && (
                  <div className="mb-1 flex items-center gap-1.5 rounded-md border border-warning/20 bg-warning/5 px-2 py-1.5 text-[10px] text-muted-foreground">
                    <Monitor className="h-3 w-3 shrink-0 text-muted-foreground/40" />
                    <span className="min-w-0">{t('startAgentToBegin')}</span>
                  </div>
                )}
                {groups.map(({ agent, sessions: own }) => (
                  <AgentGroup
                    key={agent.id}
                    agent={agent}
                    sessions={own}
                    isSelected={agent.id === selectedAgentID}
                    activeSessionID={activeSessionID}
                    terminalActive={agent.id === terminalAgentID}
                    onSelectAgent={() => onSelectAgent(agent.id)}
                    onSelectSession={onSelectSession}
                    onCreateSession={() => onCreateSession(agent.id)}
                    onDeleteSession={onDeleteSession}
                    onOpenTerminal={() => onOpenTerminal(agent.id)}
                  />
                ))}
                {orphanGroups.length > 0 && (
                  <div className="mt-2 space-y-0.5 border-t border-border/50 pt-2">
                    <div className="flex items-center gap-1.5 px-2 pb-1">
                      <Unplug className="h-3 w-3 text-muted-foreground/50" />
                      <span className="mono-label text-muted-foreground/70">{t('offlineSessions')}</span>
                    </div>
                    {orphanGroups.map((g) => (
                      <OfflineAgentGroup
                        key={g.name}
                        name={g.name}
                        sessions={g.sessions}
                        activeSessionID={activeSessionID}
                        defaultOpen={agents.length === 0 || g.sessions.some((s) => s.id === activeSessionID)}
                        onSelectSession={onSelectSession}
                        onDeleteSession={onDeleteSession}
                      />
                    ))}
                  </div>
                )}
              </div>
            )}
          </div>
        ) : (
          <div className="flex flex-col items-center gap-2 pt-3">
            {agents.map((agent) => (
              <Tooltip key={agent.id}>
                <TooltipTrigger asChild>
                  <button
                    type="button"
                    onClick={() => { onSelectAgent(agent.id); onToggle() }}
                    className={cn(
                      'relative p-1.5 rounded-md transition-colors',
                      agent.id === selectedAgentID ? 'bg-primary/10' : 'hover:bg-accent',
                    )}
                  >
                    <Monitor className="w-4 h-4 text-muted-foreground" />
                    <Circle className={cn(
                      'absolute -top-0.5 -right-0.5 h-2.5 w-2.5 fill-current',
                      agent.busy ? 'text-warning' : 'text-primary',
                    )} />
                  </button>
                </TooltipTrigger>
                <TooltipContent side="right">{agent.name}</TooltipContent>
              </Tooltip>
            ))}
          </div>
        )}
      </aside>
    </>
  )
}

function AgentGroup({
  agent, sessions, isSelected, activeSessionID, terminalActive,
  onSelectAgent, onSelectSession, onCreateSession, onDeleteSession, onOpenTerminal,
}: {
  agent: AgentInfo
  sessions: ChatSession[]
  isSelected: boolean
  activeSessionID: string | null
  terminalActive: boolean
  onSelectAgent: () => void
  onSelectSession: (id: string) => void
  onCreateSession: () => void
  onDeleteSession: (id: string) => void
  onOpenTerminal: () => void
}) {
  const { t } = useTranslation('sidebar')
  const [expanded, setExpanded] = useState(isSelected || sessions.some((s) => s.id === activeSessionID))
  const identity = agent.identity || {}
  const llm = [identity.provider, identity.model].filter(Boolean).join('/')
  const act = agentActivity(agent)

  function handleToggle() {
    setExpanded(!expanded)
    onSelectAgent()
  }

  return (
    <div className="rounded-lg">
      {/* Agent card */}
      <div className={cn(
        'rounded-md px-2 py-2 transition-colors',
        isSelected ? 'bg-primary/5' : 'hover:bg-accent/50',
      )}>
        <button
          type="button"
          onClick={handleToggle}
          className="flex w-full items-center gap-2 text-left"
        >
          <Circle className={cn(
            'h-2.5 w-2.5 shrink-0 fill-current',
            agent.busy ? 'text-warning' : 'text-primary',
          )} />
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-1.5">
              <span className="truncate text-xs font-semibold text-foreground">{agent.name}</span>
              <span className="text-[9px] text-muted-foreground">{agent.busy ? t('busy') : t('idle')}</span>
            </div>
            {act?.kind === 'tool' ? (
              <div className="truncate text-[10px] text-warning">
                ▸ {act.tool}
                {act.detail && <span className="text-muted-foreground"> · {act.detail}</span>}
              </div>
            ) : act?.kind === 'thinking' ? (
              <div className="truncate text-[10px] text-warning">{t('working')}</div>
            ) : llm ? (
              <div className="truncate text-[10px] text-muted-foreground">{llm}</div>
            ) : null}
          </div>
          {expanded ? (
            <ChevronDown className="h-3 w-3 shrink-0 text-muted-foreground" />
          ) : (
            <ChevronRight className="h-3 w-3 shrink-0 text-muted-foreground" />
          )}
        </button>

        {/* Action buttons on the agent card */}
        <div className="mt-1.5 flex items-center gap-1">
          <button
            type="button"
            onClick={(e) => { e.stopPropagation(); onOpenTerminal() }}
            className={cn(
              'flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] font-medium transition-colors',
              terminalActive
                ? 'bg-primary/15 text-primary'
                : 'text-muted-foreground hover:bg-accent hover:text-foreground',
            )}
          >
            <Terminal className="h-2.5 w-2.5" />
            {t('terminal')}
          </button>
          <button
            type="button"
            onClick={(e) => { e.stopPropagation(); setExpanded(true); onCreateSession() }}
            className="flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground hover:bg-accent hover:text-foreground transition-colors"
          >
            <Plus className="h-2.5 w-2.5" />
            {t('new')}
          </button>
          {sessions.length > 0 && (
            <span className="ml-auto text-[9px] font-mono text-muted-foreground">{t('sessionsCount', { count: sessions.length })}</span>
          )}
        </div>
      </div>

      {/* Sessions list (second level) */}
      {expanded && sessions.length > 0 && (
        <div className="ml-3 mt-0.5 space-y-0.5 border-l border-border pl-2 animate-in fade-in slide-in-from-top-1 duration-150">
          {sessions.map((session) => (
            <SessionItem
              key={session.id}
              session={session}
              active={session.id === activeSessionID}
              onSelect={() => onSelectSession(session.id)}
              onDelete={() => onDeleteSession(session.id)}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function SessionItem({
  session, active, onSelect, onDelete,
}: {
  session: ChatSession
  active: boolean
  onSelect: () => void
  onDelete: () => void
}) {
  const { t } = useTranslation('sidebar')
  const title = session.title || t('newSession')
  const time = new Date(session.updated_at).toLocaleDateString(i18n.language, { month: 'short', day: 'numeric' })

  return (
    <div
      className={cn(
        'group flex items-center gap-1.5 rounded-md px-2 py-1 cursor-pointer transition-colors',
        active ? 'bg-primary/10 text-foreground' : 'text-muted-foreground hover:bg-accent hover:text-foreground',
      )}
    >
      <button type="button" onClick={onSelect} className="flex-1 min-w-0 text-left">
        <div className="flex items-center gap-1.5">
          <MessageSquare className="h-2.5 w-2.5 shrink-0" />
          <span className="truncate text-[11px] font-medium">{title}</span>
        </div>
        <div className="mt-0.5 text-[9px] text-muted-foreground">{time}</div>
      </button>
      <button
        type="button"
        onClick={(e) => { e.stopPropagation(); onDelete() }}
        className="invisible shrink-0 rounded p-0.5 text-muted-foreground hover:bg-destructive/10 hover:text-destructive group-hover:visible"
        aria-label={t('deleteSession')}
      >
        <Trash2 className="h-2.5 w-2.5" />
      </button>
    </div>
  )
}

// OfflineAgentGroup lists sessions whose bound agent is no longer connected.
// Sessions live server-side, so when a node goes away (a local agent's process
// exiting, the hub restarting) its sessions would otherwise be stranded —
// dropped from the sidebar, which nests sessions under live agents, even though
// their transcripts are still openable. Surface them here, read-only: you can
// reopen (to read history) or delete them, but there's no connected agent to
// start a new turn on, so the terminal / new-session actions are omitted. A
// banner in the chat panel spells out that a reconnect is needed to continue.
function OfflineAgentGroup({
  name, sessions, activeSessionID, defaultOpen, onSelectSession, onDeleteSession,
}: {
  name: string
  sessions: ChatSession[]
  activeSessionID: string | null
  defaultOpen: boolean
  onSelectSession: (id: string) => void
  onDeleteSession: (id: string) => void
}) {
  const { t } = useTranslation('sidebar')
  const [expanded, setExpanded] = useState(defaultOpen)

  return (
    <div className="rounded-lg">
      <div className="rounded-md px-2 py-1.5 transition-colors hover:bg-accent/40">
        <button
          type="button"
          onClick={() => setExpanded(!expanded)}
          className="flex w-full items-center gap-2 text-left"
        >
          <Circle className="h-2.5 w-2.5 shrink-0 fill-current text-muted-foreground/40" />
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-1.5">
              <span className="truncate text-xs font-medium text-muted-foreground">{name}</span>
              <span className="text-[9px] text-warning">{t('agentOffline')}</span>
            </div>
          </div>
          <span className="text-[9px] font-mono text-muted-foreground/60">{t('sessionsCount', { count: sessions.length })}</span>
          {expanded ? (
            <ChevronDown className="h-3 w-3 shrink-0 text-muted-foreground" />
          ) : (
            <ChevronRight className="h-3 w-3 shrink-0 text-muted-foreground" />
          )}
        </button>
      </div>

      {expanded && sessions.length > 0 && (
        <div className="ml-3 mt-0.5 space-y-0.5 border-l border-border pl-2 animate-in fade-in slide-in-from-top-1 duration-150">
          {sessions.map((session) => (
            <SessionItem
              key={session.id}
              session={session}
              active={session.id === activeSessionID}
              onSelect={() => onSelectSession(session.id)}
              onDelete={() => onDeleteSession(session.id)}
            />
          ))}
        </div>
      )}
    </div>
  )
}

// LocalAgentControl is the top-left launcher for hub-hosted agents: a button that
// spawns an `aiscan agent` on the console host (wired back over loopback) and a
// popover to watch/delete the ones it started. Connected nodes also appear in the
// roster below — this popover owns their process lifecycle, not the conversation.
function LocalAgentControl() {
  const { t } = useTranslation('sidebar')
  const [open, setOpen] = useState(false)
  const [items, setItems] = useState<LocalAgentView[]>([])
  const [launching, setLaunching] = useState(false)
  const [error, setError] = useState('')
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null)
  const ref = useRef<HTMLDivElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)

  // The panel is portaled to <body> so it escapes the sidebar's backdrop-blur
  // stacking context and the app shell's overflow-hidden clip. Anchor its right
  // edge under the trigger, then clamp into the viewport so it never spills off
  // the left edge (the sidebar sits flush-left, so a right-anchored panel would
  // otherwise get cut off there).
  const POPOVER_W = 256 // matches w-64 below
  useLayoutEffect(() => {
    if (!open) return
    const place = () => {
      const r = ref.current?.getBoundingClientRect()
      if (!r) return
      const left = Math.max(8, Math.min(r.right - POPOVER_W, window.innerWidth - POPOVER_W - 8))
      setPos({ top: r.bottom + 6, left })
    }
    place()
    window.addEventListener('resize', place)
    window.addEventListener('scroll', place, true)
    return () => {
      window.removeEventListener('resize', place)
      window.removeEventListener('scroll', place, true)
    }
  }, [open])

  const refresh = useCallback(async () => {
    try {
      setItems(await listLocalAgents())
    } catch {
      /* transient / admin-gated — keep the last known roster */
    }
  }, [])

  // Immediate load on mount and whenever the popover opens (so a just-launched
  // node shows promptly).
  useEffect(() => {
    void refresh()
  }, [open, refresh])

  // Poll for the badge + roster; tighter cadence while the popover is open. The
  // poll pauses while the tab is hidden and catches up on return to foreground.
  usePolling(refresh, open ? 2500 : 8000)

  // Close on outside click (buttons inside `ref` keep it open).
  useEffect(() => {
    if (!open) return
    const onDoc = (e: MouseEvent) => {
      const target = e.target as Node
      if (ref.current?.contains(target) || panelRef.current?.contains(target)) return
      setOpen(false)
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [open])

  const launch = async () => {
    setLaunching(true)
    setError('')
    try {
      await launchLocalAgent()
      await refresh()
    } catch (err) {
      setError((err as Error)?.message || t('launchLocal'))
    } finally {
      setLaunching(false)
    }
  }

  const remove = async (name: string) => {
    setError('')
    try {
      await stopLocalAgent(name)
      await refresh()
    } catch (err) {
      setError((err as Error)?.message || t('stopLocal'))
    }
  }

  return (
    <div ref={ref} className="relative">
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            type="button"
            onClick={() => setOpen((v) => !v)}
            aria-label={t('launchLocal')}
            className={cn(
              'relative flex h-7 w-7 items-center justify-center rounded-md transition-colors',
              open ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:bg-accent hover:text-foreground',
            )}
          >
            <MonitorPlay className="h-4 w-4" />
            {items.length > 0 && (
              <span className="absolute -right-0.5 -top-0.5 flex h-3.5 min-w-3.5 items-center justify-center rounded-full bg-primary px-0.5 text-[8px] font-bold text-primary-foreground">
                {items.length > 9 ? '9+' : items.length}
              </span>
            )}
          </button>
        </TooltipTrigger>
        <TooltipContent side="bottom">{t('localAgents')}</TooltipContent>
      </Tooltip>

      {open && pos && createPortal(
        <div
          ref={panelRef}
          style={{ top: pos.top, left: pos.left, width: POPOVER_W }}
          className="fixed z-[70] rounded-xl border border-border bg-popover p-2 shadow-elevated animate-in fade-in zoom-in-95 duration-150"
        >
          <div className="px-1 pb-1.5">
            <span className="text-xs font-semibold text-foreground">{t('localAgents')}</span>
          </div>

          {error && (
            <div className="mb-1.5 flex items-start gap-1.5 rounded-md border border-destructive/30 bg-destructive/10 px-2 py-1.5 text-[11px] text-destructive">
              <AlertTriangle className="mt-0.5 h-3 w-3 shrink-0" />
              <span className="min-w-0 break-words">{error}</span>
            </div>
          )}

          <div className="max-h-56 space-y-0.5 overflow-auto">
            {items.length === 0 ? (
              <p className="px-1 py-3 text-center text-[11px] text-muted-foreground/70">{t('noLocalAgents')}</p>
            ) : (
              items.map((it) => <LocalAgentRow key={it.name} item={it} onRemove={() => void remove(it.name)} />)
            )}
          </div>

          <button
            type="button"
            onClick={() => void launch()}
            disabled={launching}
            className="mt-1.5 flex w-full items-center justify-center gap-1.5 rounded-md bg-primary/10 px-2 py-1.5 text-xs font-medium text-primary transition-colors hover:bg-primary/20 disabled:opacity-60"
          >
            {launching ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Plus className="h-3.5 w-3.5" />}
            {launching ? t('launching') : t('launchLocal')}
          </button>
          <p className="mt-1 px-1 text-[10px] leading-snug text-muted-foreground/70">{t('localHint')}</p>
        </div>,
        document.body,
      )}
    </div>
  )
}

function LocalAgentRow({ item, onRemove }: { item: LocalAgentView; onRemove: () => void }) {
  const { t } = useTranslation('sidebar')
  const connected = item.registered

  return (
    <div className="group flex items-center gap-2 rounded-md px-1.5 py-1.5 hover:bg-accent/50">
      <Circle className={cn('h-2 w-2 shrink-0 fill-current', connected ? 'text-primary' : 'text-warning animate-pulse')} />
      <div className="min-w-0 flex-1">
        <div className="truncate text-xs font-medium text-foreground">{item.name}</div>
        <div className="truncate text-[10px] text-muted-foreground">
          {connected ? t('localConnected') : t('localStarting')}
          {item.busy ? ` · ${t('busy')}` : ''}
          {item.pid ? ` · pid ${item.pid}` : ''}
        </div>
      </div>
      <button
        type="button"
        onClick={onRemove}
        aria-label={t('stopLocal')}
        title={t('stopLocal')}
        className="shrink-0 rounded p-1 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
      >
        <Trash2 className="h-3 w-3" />
      </button>
    </div>
  )
}
