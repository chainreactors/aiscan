import { useMemo, useState } from 'react'
import {
  Shield, PanelLeftClose, PanelLeft,
  MessageSquare, Plus, Trash2, Circle,
  ChevronDown, ChevronRight, Monitor, Terminal,
  Search, X, Layers, ShieldAlert, Activity,
} from 'lucide-react'
import { Button, Tooltip, TooltipTrigger, TooltipContent, Input } from '@aspect/ui'
import { cn } from '@aspect/theme'
import type { AgentInfo, ChatSession, ScanJob } from '../api'

type SidebarTab = 'agents' | 'scans'

interface AppSidebarProps {
  open: boolean
  onToggle: () => void
  view: 'chat' | 'scan'
  onChangeView: (view: 'chat' | 'scan') => void
  agents: AgentInfo[]
  sessions: ChatSession[]
  activeSessionID: string | null
  selectedAgentID: string | null
  terminalAgentID: string | null
  onSelectAgent: (id: string) => void
  onSelectSession: (id: string) => void
  onCreateSession: (agentID: string) => void
  onDeleteSession: (id: string) => void
  onOpenTerminal: (agentID: string) => void
  scans: ScanJob[]
  activeScanID?: string
  onSelectScan: (scan: ScanJob) => void
}

export default function AppSidebar({
  open, onToggle, view, onChangeView,
  agents, sessions, activeSessionID, selectedAgentID, terminalAgentID,
  onSelectAgent, onSelectSession, onCreateSession, onDeleteSession, onOpenTerminal,
  scans, activeScanID, onSelectScan,
}: AppSidebarProps) {
  const [tab, setTab] = useState<SidebarTab>(view === 'scan' ? 'scans' : 'agents')

  const runningScans = scans.filter((s) => s.status === 'running' || s.status === 'queued').length

  function handleTabChange(newTab: SidebarTab) {
    setTab(newTab)
    onChangeView(newTab === 'scans' ? 'scan' : 'chat')
  }

  function handleSelectScan(scan: ScanJob) {
    setTab('scans')
    onSelectScan(scan)
  }

  function handleSelectSession(id: string) {
    setTab('agents')
    onSelectSession(id)
  }

  return (
    <>
      {open && (
        <button
          type="button"
          aria-label="Close sidebar overlay"
          onClick={onToggle}
          className="fixed inset-0 z-30 bg-background/60 backdrop-blur-[1px] md:hidden"
        />
      )}
      <aside
        className={cn(
          'flex flex-col border-r border-border bg-card/95 backdrop-blur-sm transition-all duration-200 ease-in-out shrink-0 md:bg-card/50',
          open
            ? 'fixed inset-y-0 left-0 z-40 w-72 shadow-xl md:relative md:inset-auto md:z-auto md:shadow-none'
            : 'w-12',
        )}
      >
        {/* Header */}
        <div className={cn('flex items-center border-b border-border', open ? 'p-3 gap-3' : 'p-2 flex-col gap-2')}>
          {open ? (
            <>
              <Shield className="w-5 h-5 text-primary shrink-0" />
              <div className="flex-1 min-w-0">
                <h1 className="text-sm font-bold text-primary">AIScan</h1>
                <div className="text-[10px] text-muted-foreground">
                  {agents.length} agent{agents.length !== 1 ? 's' : ''}
                  {runningScans > 0 && ` · ${runningScans} scanning`}
                </div>
              </div>
              <Button variant="ghost" size="icon" onClick={onToggle} className="h-7 w-7 text-muted-foreground" aria-label="Collapse sidebar">
                <PanelLeftClose className="w-4 h-4" />
              </Button>
            </>
          ) : (
            <Tooltip>
              <TooltipTrigger asChild>
                <button type="button" onClick={onToggle} aria-label="Expand sidebar" className="p-1 rounded-md hover:bg-accent transition-colors">
                  <Shield className="w-5 h-5 text-primary" />
                </button>
              </TooltipTrigger>
              <TooltipContent side="right">AIScan</TooltipContent>
            </Tooltip>
          )}
        </div>

        {open ? (
          <>
            {/* Segment control */}
            <div className="shrink-0 border-b border-border p-2">
              <div className="relative flex h-8 rounded-md bg-secondary/60 p-0.5">
                <div
                  className={cn(
                    'absolute top-0.5 h-[calc(100%-4px)] w-[calc(50%-2px)] rounded-[5px] bg-background shadow-sm transition-transform duration-200',
                    tab === 'scans' ? 'translate-x-[calc(100%+2px)]' : 'translate-x-0',
                  )}
                />
                <button
                  type="button"
                  onClick={() => handleTabChange('agents')}
                  className={cn(
                    'relative z-10 flex flex-1 items-center justify-center gap-1.5 rounded-[5px] text-[11px] font-medium transition-colors',
                    tab === 'agents' ? 'text-foreground' : 'text-muted-foreground hover:text-foreground/70',
                  )}
                >
                  <MessageSquare className="h-3 w-3" />
                  Chat
                </button>
                <button
                  type="button"
                  onClick={() => handleTabChange('scans')}
                  className={cn(
                    'relative z-10 flex flex-1 items-center justify-center gap-1.5 rounded-[5px] text-[11px] font-medium transition-colors',
                    tab === 'scans' ? 'text-foreground' : 'text-muted-foreground hover:text-foreground/70',
                  )}
                >
                  <Activity className="h-3 w-3" />
                  Scan
                  {runningScans > 0 && (
                    <span className="flex h-4 min-w-4 items-center justify-center rounded-full bg-primary/20 px-1 text-[9px] font-bold text-primary">
                      {runningScans}
                    </span>
                  )}
                </button>
              </div>
            </div>

            {/* Content */}
            <div className="flex-1 min-h-0 overflow-auto">
              {tab === 'agents' ? (
                <AgentsContent
                  agents={agents}
                  sessions={sessions}
                  activeSessionID={activeSessionID}
                  selectedAgentID={selectedAgentID}
                  terminalAgentID={terminalAgentID}
                  onSelectAgent={onSelectAgent}
                  onSelectSession={handleSelectSession}
                  onCreateSession={onCreateSession}
                  onDeleteSession={onDeleteSession}
                  onOpenTerminal={onOpenTerminal}
                />
              ) : (
                <ScansContent
                  scans={scans}
                  activeScanID={activeScanID}
                  onSelectScan={handleSelectScan}
                />
              )}
            </div>
          </>
        ) : (
          /* Collapsed state */
          <div className="flex flex-col items-center gap-1 pt-3">
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  type="button"
                  onClick={() => { onChangeView('chat'); setTab('agents'); if (!open) onToggle() }}
                  className={cn(
                    'relative p-1.5 rounded-md transition-colors',
                    view === 'chat' ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:bg-accent',
                  )}
                >
                  <MessageSquare className="w-4 h-4" />
                </button>
              </TooltipTrigger>
              <TooltipContent side="right">Chat</TooltipContent>
            </Tooltip>

            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  type="button"
                  onClick={() => { onChangeView('scan'); setTab('scans'); if (!open) onToggle() }}
                  className={cn(
                    'relative p-1.5 rounded-md transition-colors',
                    view === 'scan' ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:bg-accent',
                  )}
                >
                  <Activity className="w-4 h-4" />
                  {runningScans > 0 && (
                    <span className="absolute -top-0.5 -right-0.5 flex h-3.5 w-3.5 items-center justify-center rounded-full bg-primary text-[8px] font-bold text-white">
                      {runningScans > 9 ? '9+' : runningScans}
                    </span>
                  )}
                </button>
              </TooltipTrigger>
              <TooltipContent side="right">
                Scan{runningScans > 0 ? ` (${runningScans} running)` : ''}
              </TooltipContent>
            </Tooltip>

            <div className="my-1 h-px w-6 bg-border" />

            {agents.map((agent) => (
              <Tooltip key={agent.id}>
                <TooltipTrigger asChild>
                  <button
                    type="button"
                    onClick={() => { onSelectAgent(agent.id); onChangeView('chat'); setTab('agents'); onToggle() }}
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

            <Tooltip>
              <TooltipTrigger asChild>
                <Button variant="ghost" size="icon" onClick={onToggle} className="mt-1 h-7 w-7 text-muted-foreground" aria-label="Expand sidebar">
                  <PanelLeft className="w-3.5 h-3.5" />
                </Button>
              </TooltipTrigger>
              <TooltipContent side="right">Expand sidebar</TooltipContent>
            </Tooltip>
          </div>
        )}
      </aside>
    </>
  )
}

/* ---------- Agents tab content ---------- */

function AgentsContent({
  agents, sessions, activeSessionID, selectedAgentID, terminalAgentID,
  onSelectAgent, onSelectSession, onCreateSession, onDeleteSession, onOpenTerminal,
}: {
  agents: AgentInfo[]
  sessions: ChatSession[]
  activeSessionID: string | null
  selectedAgentID: string | null
  terminalAgentID: string | null
  onSelectAgent: (id: string) => void
  onSelectSession: (id: string) => void
  onCreateSession: (agentID: string) => void
  onDeleteSession: (id: string) => void
  onOpenTerminal: (agentID: string) => void
}) {
  const sessionsByAgent = useMemo(() => {
    const map = new Map<string, ChatSession[]>()
    for (const s of sessions) {
      const list = map.get(s.agent_id) || []
      list.push(s)
      map.set(s.agent_id, list)
    }
    return map
  }, [sessions])

  if (agents.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center py-8 text-center px-4">
        <Monitor className="h-8 w-8 text-muted-foreground/20" />
        <p className="mt-2 text-xs text-muted-foreground">No agents connected</p>
        <p className="mt-1 text-[10px] text-muted-foreground/60">Start an aiscan agent to begin</p>
      </div>
    )
  }

  return (
    <div className="space-y-1 p-2">
      {agents.map((agent) => (
        <AgentGroup
          key={agent.id}
          agent={agent}
          sessions={sessionsByAgent.get(agent.id) || []}
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
    </div>
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
  const [expanded, setExpanded] = useState(isSelected || sessions.some((s) => s.id === activeSessionID))
  const identity = agent.identity || {}
  const llm = [identity.provider, identity.model].filter(Boolean).join('/')

  function handleToggle() {
    setExpanded(!expanded)
    onSelectAgent()
  }

  return (
    <div className="rounded-lg">
      <div className={cn(
        'rounded-md px-2 py-2 transition-colors',
        isSelected ? 'bg-primary/5' : 'hover:bg-accent/50',
      )}>
        <button type="button" onClick={handleToggle} className="flex w-full items-center gap-2 text-left">
          <Circle className={cn('h-2.5 w-2.5 shrink-0 fill-current', agent.busy ? 'text-warning' : 'text-primary')} />
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-1.5">
              <span className="truncate text-xs font-semibold text-foreground">{agent.name}</span>
              <span className="text-[9px] text-muted-foreground">{agent.busy ? 'busy' : 'idle'}</span>
            </div>
            {llm && <div className="truncate text-[10px] text-muted-foreground">{llm}</div>}
          </div>
          {expanded ? <ChevronDown className="h-3 w-3 shrink-0 text-muted-foreground" /> : <ChevronRight className="h-3 w-3 shrink-0 text-muted-foreground" />}
        </button>
        <div className="mt-1.5 flex items-center gap-1">
          <button
            type="button"
            onClick={(e) => { e.stopPropagation(); onOpenTerminal() }}
            className={cn(
              'flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] font-medium transition-colors',
              terminalActive ? 'bg-primary/15 text-primary' : 'text-muted-foreground hover:bg-accent hover:text-foreground',
            )}
          >
            <Terminal className="h-2.5 w-2.5" />
            Terminal
          </button>
          <button
            type="button"
            onClick={(e) => { e.stopPropagation(); onCreateSession() }}
            className="flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground hover:bg-accent hover:text-foreground transition-colors"
          >
            <Plus className="h-2.5 w-2.5" />
            New
          </button>
          {sessions.length > 0 && (
            <span className="ml-auto text-[9px] font-mono text-muted-foreground">{sessions.length}</span>
          )}
        </div>
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

function SessionItem({ session, active, onSelect, onDelete }: {
  session: ChatSession; active: boolean; onSelect: () => void; onDelete: () => void
}) {
  const title = session.title || 'New session'
  const time = new Date(session.updated_at).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })

  return (
    <div className={cn(
      'group flex items-center gap-1.5 rounded-md px-2 py-1 cursor-pointer transition-colors',
      active ? 'bg-primary/10 text-foreground' : 'text-muted-foreground hover:bg-accent hover:text-foreground',
    )}>
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
        aria-label="Delete session"
      >
        <Trash2 className="h-2.5 w-2.5" />
      </button>
    </div>
  )
}

/* ---------- Scans tab content ---------- */

function ScansContent({ scans, activeScanID, onSelectScan }: {
  scans: ScanJob[]; activeScanID?: string; onSelectScan: (scan: ScanJob) => void
}) {
  const [query, setQuery] = useState('')
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    return q ? scans.filter((s) => s.target.toLowerCase().includes(q)) : scans
  }, [query, scans])

  const running = scans.filter((s) => s.status === 'running' || s.status === 'queued').length
  const completed = scans.filter((s) => s.status === 'completed').length

  return (
    <div className="flex flex-col">
      <div className="shrink-0 p-2 border-b border-border">
        <div className="relative">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search targets"
            aria-label="Search scan targets"
            className="h-8 pl-8 pr-8 text-xs"
          />
          {query && (
            <button
              type="button"
              aria-label="Clear search"
              onClick={() => setQuery('')}
              className="absolute right-1.5 top-1/2 inline-flex h-5 w-5 -translate-y-1/2 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-foreground"
            >
              <X className="h-3 w-3" />
            </button>
          )}
        </div>
      </div>
      <div className="flex h-8 shrink-0 items-center justify-between gap-2 border-b border-border px-3 text-[10px] uppercase text-muted-foreground">
        <span>History</span>
        <span className="truncate">
          {running > 0 && <span className="text-primary">{running} running</span>}
          {running > 0 && completed > 0 && ' · '}
          {completed > 0 && `${completed} done`}
          {running === 0 && completed === 0 && `${scans.length} total`}
        </span>
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-2">
        {filtered.length === 0 ? (
          <div className="px-2 py-3 text-xs text-muted-foreground">
            {query.trim() ? 'No matching targets.' : 'No scans yet.'}
          </div>
        ) : (
          filtered.map((scan) => (
            <ScanNavButton
              key={scan.id}
              active={scan.id === activeScanID}
              scan={scan}
              onClick={() => onSelectScan(scan)}
            />
          ))
        )}
      </div>
    </div>
  )
}

function ScanNavButton({ active, onClick, scan }: { active: boolean; onClick: () => void; scan: ScanJob }) {
  const assets = scan.result?.assets?.length || 0
  const loots = scanLootCount(scan.result)
  const badges = scanBadges(scan)

  return (
    <button
      type="button"
      onClick={onClick}
      aria-current={active ? 'true' : undefined}
      className={cn(
        'mb-1 flex w-full items-start gap-2 rounded-md px-2 py-2 text-left text-xs transition-colors',
        active ? 'bg-primary/10 text-foreground' : 'text-muted-foreground hover:bg-accent hover:text-foreground',
      )}
    >
      <Circle className={cn('mt-1 h-2.5 w-2.5 shrink-0 fill-current', scanStateColor(scan.status))} />
      <span className="min-w-0 flex-1">
        <span className="flex min-w-0 items-center gap-1.5">
          <span className="min-w-0 flex-1 truncate font-mono font-medium">{scan.target}</span>
          <span className={cn('shrink-0 text-[10px]', scanStateTextColor(scan.status))}>
            {scanStatusLabel(scan.status)}
          </span>
        </span>
        <span className="mt-0.5 block truncate font-mono">{scan.mode}</span>
        <span className="mt-0.5 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5 text-[10px]">
          {assets > 0 && (
            <span className="inline-flex items-center gap-0.5 text-primary">
              <Layers className="h-3 w-3" />
              <span className="font-mono">{assets}</span>
            </span>
          )}
          {loots > 0 && (
            <span className="inline-flex items-center gap-0.5 text-red-700 dark:text-red-300">
              <ShieldAlert className="h-3 w-3" />
              <span className="font-mono">{loots}</span>
            </span>
          )}
          {badges.map((b) => <span key={b} className="text-muted-foreground/80">{b}</span>)}
          <span className="truncate text-muted-foreground/60">{shortTime(scan.created_at)}</span>
        </span>
      </span>
    </button>
  )
}

/* ---------- Helpers ---------- */

function scanLootCount(result?: ScanJob['result']) {
  if (!result) return 0
  if (result.loots && result.loots.length > 0) {
    return result.loots.filter((l) => l.kind.toLowerCase() !== 'fingerprint').length
  }
  return (result.assets || []).reduce((sum, a) => (
    sum + (a.items || []).filter((i) => i.kind === 'loot' && (typeof i.data?.kind === 'string' ? i.data.kind : '').toLowerCase() !== 'fingerprint').length
  ), 0)
}

function scanBadges(scan: ScanJob) {
  const b: string[] = []
  if (scan.verify || (scan.ai && !scan.sniper)) b.push('Verify')
  if (scan.sniper || (scan.ai && !scan.verify)) b.push('Sniper')
  if (scan.deep) b.push('Deep')
  return b
}

function scanStatusLabel(status: string) {
  const labels: Record<string, string> = { queued: 'queued', running: 'running', completed: 'done', failed: 'failed', canceled: 'canceled', ready: 'ready' }
  return labels[status] || status || 'ready'
}

function scanStateColor(status: string) {
  const map: Record<string, string> = { running: 'text-primary', queued: 'text-primary', completed: 'text-muted-foreground', failed: 'text-destructive', canceled: 'text-warning' }
  return map[status] || 'text-muted-foreground'
}

function scanStateTextColor(status: string) {
  const map: Record<string, string> = { running: 'text-primary', queued: 'text-primary', failed: 'text-destructive', canceled: 'text-yellow-700 dark:text-yellow-300' }
  return map[status] || 'text-muted-foreground'
}

function shortTime(value: string) {
  try {
    return new Date(value).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  } catch {
    return value
  }
}
