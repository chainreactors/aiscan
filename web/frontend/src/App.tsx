import { useState, useEffect, useCallback, useMemo, lazy, Suspense, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Cloud, Monitor, Settings } from 'lucide-react'
import LanguageToggle from './components/LanguageToggle'
import SessionList from './components/SessionList'
import ChatPanel from './components/ChatPanel'
import DetailPanel from './components/DetailPanel'
import ScanWorkspace from './components/ScanWorkspace'
import IntelRail, { type ReasoningSummary } from './components/deck/IntelRail'
import DeckTopBar from './components/deck/DeckTopBar'
import ProjectSelector from './components/deck/ProjectSelector'
import DeckAmbient from './components/deck/DeckAmbient'
import ConfigPanel from './components/ConfigPanel'
import DeployPanel from './components/DeployPanel'
import AgentPanel from './components/AgentPanel'
import { useConfirm } from './components/ConfirmDialog'
// Lazy: the agent terminal drags in @xterm (~its own chunk) but only renders
// when a node's console is opened — keep it out of the first-paint bundle.
const AgentTerminal = lazy(() => import('./components/terminal'))
import { ThemeToggle } from '@aspect/ui'
import { ThemeProvider, useTheme } from '@aspect/theme'
import { getStatus } from './api'
import type { ScanJob, ServerStatus } from './api'
import { useScanSession } from './hooks/useScanSession'
import { useChatSession, agentNodeKey, type TimelineItem } from './hooks/useChatSession'
import { useAssetPool } from './hooks/useAssetPool'
import { useProject } from './hooks/useProject'
import { toolCallAssets } from './lib/agent-assets'
import { isCompletionTool } from './lib/agent-tools'
import { isSessionAgentOnline } from './lib/session-agent'
import { formatElapsed } from './lib/deck'
import { parseRoute, sessionRoutePath } from './lib/scan-route'
import { TooltipProvider } from '@aspect/ui'
import { cn } from '@aspect/theme'

const sidebarStorageKey = 'aiscan-sidebar-open'

type AppView = 'chat' | 'scan'

// Respect a previously-chosen theme on boot. ThemeProvider's own initializer is
// short-circuited by the `initial` prop (it returns `initial` before ever reading
// storage), so we read the persisted value here and feed it in as the initial —
// otherwise every reload snaps back to the light default. First run (nothing
// stored) still lands on the light day field.
function getInitialTheme(): 'light' | 'dark' {
  if (typeof window === 'undefined') return 'light'
  const v = window.localStorage.getItem('aiscan-theme')
  return v === 'dark' || v === 'light' ? v : 'light'
}

function getInitialSidebarOpen() {
  if (typeof window === 'undefined') return true
  if (window.matchMedia('(max-width: 767px)').matches) return false
  const stored = window.localStorage.getItem(sidebarStorageKey)
  if (stored === 'true' || stored === 'false') return stored === 'true'
  return window.matchMedia('(min-width: 1024px)').matches
}

function clip(s: string, n: number): string {
  return s.length > n ? s.slice(0, n - 1).trimEnd() + '…' : s
}

// A bare host / ip / url token with no prose — raw data the model is dumping
// (e.g. a list of discovered domains), not reasoning. Filtered so the panel
// stays a signal feed instead of a data dump.
function looksLikeData(line: string): boolean {
  if (/\s/.test(line)) return false // has whitespace → prose, keep it
  return (
    /^[\w-]+(\.[\w-]+)+(:\d+)?$/.test(line) || // domain / host[:port]
    /^\d{1,3}(\.\d{1,3}){3}(:\d+)?$/.test(line) || // ipv4[:port]
    /^https?:\/\//i.test(line) // url
  )
}

// Shell-command tools carry no network "target" — their arg is a whole script.
// Surface what's actually running: the first real command line, skipping the '#'
// comment header the agent tends to write (which otherwise leaks into the panel
// as a truncated, '#'-prefixed sentence). Newlines/runs of whitespace collapse
// to a single space; a comment-only command falls back to that comment, stripped
// of its '#'/'+' noise, so there's always something to show.
function commandLabel(cmd: string): string {
  const lines = cmd.split('\n').map((l) => l.trim()).filter(Boolean)
  const code = lines.find((l) => !l.startsWith('#'))
  if (code) return code.replace(/\s+/g, ' ')
  const comment = lines.find((l) => !l.startsWith('#!')) ?? lines[0] ?? ''
  return comment.replace(/^#+\s*/, '').replace(/^[+\-*]\s*/, '').trim()
}

// Pull the one field that says what a tool is actually acting on, so a call
// reads as "→ scan  example.com" rather than an opaque tool name. Shell commands
// are the exception: their command/cmd/script arg is run through commandLabel
// (above) instead of shown raw. Falls back to the raw arg string when the
// payload isn't the expected JSON object.
function toolTarget(toolArgs: string): string {
  const raw = (toolArgs || '').trim()
  if (!raw) return ''
  const CMD_KEYS = new Set(['command', 'cmd', 'script'])
  try {
    const a = JSON.parse(raw)
    if (a && typeof a === 'object' && !Array.isArray(a)) {
      const rec = a as Record<string, unknown>
      for (const k of ['target', 'url', 'host', 'domain', 'query', 'q', 'path', 'file', 'filename', 'command', 'cmd', 'script', 'name', 'input']) {
        const v = rec[k]
        if (typeof v === 'string' && v.trim()) return CMD_KEYS.has(k) ? commandLabel(v) : v.trim()
        if (Array.isArray(v) && v.length) return v.filter((x) => typeof x === 'string').slice(0, 3).join(', ')
      }
      return ''
    }
    return typeof a === 'string' ? a : ''
  } catch {
    return raw // not JSON — a bare arg string
  }
}

// Summarise the current agent turn for the Cortex panel: the full transcript
// already lives in the chat, so the rail shows only a glanceable status —
// current intent, the tool running now, and how much ground the turn covered.
// Scoped to the latest user turn so counts read as "this run", not lifetime.
// (On the scan deck the same panel is fed by scanner logs instead.)
function summarizeAgentActivity(timeline: TimelineItem[]): ReasoningSummary {
  let start = 0
  for (let i = timeline.length - 1; i >= 0; i--) {
    if (timeline[i].kind === 'message' && timeline[i].message?.role === 'user') {
      start = i + 1
      break
    }
  }
  let focus = ''
  let nowTool = ''
  let nowTarget = ''
  let toolCount = 0
  const targets = new Set<string>()
  let streaming = false
  for (const item of timeline.slice(start)) {
    if (item.kind !== 'assistant_response' || !item.assistantResponse) continue
    const ar = item.assistantResponse
    if (ar.streaming) streaming = true
    if (ar.thinking) {
      for (const raw of ar.thinking.split('\n')) {
        const s = raw.trim()
        if (s && !looksLikeData(s)) focus = clip(s, 140) // last meaningful line wins
      }
    }
    for (const tool of ar.tools) {
      if (isCompletionTool(tool.toolName)) continue
      toolCount++
      if (tool.pending) {
        nowTool = tool.toolName
        nowTarget = toolTarget(tool.toolArgs)
      }
      // Count only real recon targets (hosts/IPs/URLs), not the bash commands,
      // queries, or file paths that toolTarget also surfaces — otherwise every
      // distinct command string counts as a "目标" and the tally just mirrors
      // toolCount. Uses the same extraction as the asset pool (structured args +
      // recon stdout) so the stat matches the discovered-assets list below it.
      for (const cand of toolCallAssets(tool.toolName, tool.toolArgs, tool.result)) targets.add(cand)
    }
  }
  return { focus, nowTool, nowTarget: clip(nowTarget, 64), toolCount, targetCount: targets.size, streaming }
}

function getInitialView(): AppView {
  // The CORTEX operation deck is the landing experience; only an explicit
  // chat session route (/sessions/<id>) opens the agent chat workspace.
  if (typeof window === 'undefined') return 'scan'
  return parseRoute(window.location.pathname).kind === 'session' ? 'chat' : 'scan'
}

export default function App() {
  const { t } = useTranslation('app')
  const { t: tc } = useTranslation('chat')
  const { t: td } = useTranslation('deck')
  const confirm = useConfirm()
  const chat = useChatSession()
  const { project, projects, setProject, createProject, deleteProject, refresh: refreshProjects } = useProject()
  const scanSession = useScanSession(project)
  const assetPool = useAssetPool(chat.timeline, project, refreshProjects)
  const [analysisAvailable, setAnalysisAvailable] = useState(true)
  const [serverStatus, setServerStatus] = useState<ServerStatus | null>(null)
  const [configOpen, setConfigOpen] = useState(false)
  const [deployOpen, setDeployOpen] = useState(false)
  const [agentPanelOpen, setAgentPanelOpen] = useState(false)
  const [agentPanelFocusID, setAgentPanelFocusID] = useState<string | null>(null)
  const [sidebarOpen, setSidebarOpen] = useState(getInitialSidebarOpen)
  const [detailOpen, setDetailOpen] = useState(true)
  // Track the terminal target by the node's STABLE key, not its transient agent
  // id: the hub mints a fresh id on every reconnect, so keying on id would drop
  // the terminal (and never restore it) when a node bounces to reload config.
  const [terminalNodeKey, setTerminalNodeKey] = useState<string | null>(null)
  const [view, setView] = useState<AppView>(getInitialView)

  // Stable so ChatPanel's memoized TimelineEntry rows aren't re-rendered on
  // every streamed token. chat.showScanDetail is itself a stable useCallback.
  const handleShowScanDetail = useCallback((scanID: string) => {
    chat.showScanDetail(scanID)
    setDetailOpen(true)
  }, [chat.showScanDetail])

  const refreshStatus = useCallback(async () => {
    try {
      const status = await getStatus()
      setServerStatus(status)
      setAnalysisAvailable(status.llm_available)
    } catch {
      setAnalysisAvailable(true)
    }
  }, [])

  useEffect(() => {
    refreshStatus()
  }, [refreshStatus])

  useEffect(() => {
    window.localStorage.setItem(sidebarStorageKey, String(sidebarOpen))
  }, [sidebarOpen])

  useEffect(() => {
    const syncViewFromRoute = () => {
      const route = parseRoute(window.location.pathname)
      // root + scan routes land on the deck; only a session route opens chat
      setView(route.kind === 'session' ? 'chat' : 'scan')
      if (route.kind === 'session') {
        setAgentPanelOpen(false)
      }
    }
    syncViewFromRoute()
    window.addEventListener('popstate', syncViewFromRoute)
    return () => window.removeEventListener('popstate', syncViewFromRoute)
  }, [])

  const detailResult = chat.detailScanID ? chat.scanResults.get(chat.detailScanID) ?? null : null
  const showDetail = detailOpen && !!chat.detailScanID && !!detailResult
  const terminalAgent = terminalNodeKey ? chat.agents.find((a) => agentNodeKey(a) === terminalNodeKey) ?? null : null

  // Live elapsed clock for the shared top bar (ticks only while scanning).
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!scanSession.scanning) return
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [scanSession.scanning])
  const elapsed = formatElapsed(scanSession.activeScan, scanSession.result, now)
  const model = serverStatus?.llm_model || chat.agents.find((a) => a.identity?.model)?.identity?.model || 'cortex'
  const providerLabel = (serverStatus?.llm_provider || model).toUpperCase()
  const agentSummary = useMemo(() => summarizeAgentActivity(chat.timeline), [chat.timeline])
  const activeSession = chat.sessions.find((s) => s.id === chat.activeSessionID) || null
  // The open session's bound agent has dropped off the live roster (its node
  // exited / the hub restarted). The transcript still shows, but a new turn
  // can't be dispatched until it reconnects — surface that in the chat panel.
  const activeAgentOffline = !!activeSession && !isSessionAgentOnline(activeSession, chat.agents)
  const chatCrumb = terminalAgent?.name || activeSession?.title || undefined

  function handleOpenTerminal(agentID: string) {
    const a = chat.agents.find((x) => x.id === agentID)
    setTerminalNodeKey(a ? agentNodeKey(a) : agentID)
    chat.selectAgent(agentID)
  }

  function handleSelectSession(id: string) {
    setTerminalNodeKey(null)
    chat.selectSession(id)
  }

  function handleCreateSession(agentID: string) {
    setTerminalNodeKey(null)
    chat.createSession(agentID)
  }

  // SCAN ⇄ AGENT toggle in the shared top bar. Swaps only the body beneath it;
  // the route is kept in sync so reload / back-forward land on the same view.
  function switchView(next: AppView) {
    setTerminalNodeKey(null)
    setView(next)
    if (next === 'chat') {
      const path = chat.activeSessionID ? sessionRoutePath(chat.activeSessionID) : '/'
      window.history.pushState({}, '', path)
    } else if (parseRoute(window.location.pathname).kind === 'session') {
      // scan / ioa are deck-side; drop a stale session route so reload / back
      // doesn't snap back into the chat workspace.
      window.history.pushState({}, '', '/')
    }
  }

  // Command Cortex: send the typed command into the chat workspace (creating a
  // session on the active node if needed). The hook owns session + routing.
  function handleCommandCortex(text: string) {
    setTerminalNodeKey(null)
    setView('chat')
    void chat.command(text)
  }

  // Dispatch a pooled asset to an AI agent node: hand the target to the active
  // node's chat as a recon + security-test instruction, then open the console
  // to watch it work.
  function handleDispatchAgent(target: string) {
    setTerminalNodeKey(null)
    setView('chat')
    void chat.command(td('dispatchAgentPrompt', { target }))
  }

  // Quick local scan of a pooled asset from anywhere (e.g. the chat-side rail).
  function handleScanAsset(target: string) {
    setTerminalNodeKey(null)
    setView('scan')
    scanSession.submit(target, 'quick', { verify: false, sniper: false, deep: false })
  }

  // IOA node clicked on the deck → open the agent console focused on that node.
  function handleOpenNode(agentID: string) {
    setAgentPanelFocusID(agentID)
    setAgentPanelOpen(true)
  }

  function handleOpenAgentPanel() {
    setAgentPanelFocusID(null)
    setAgentPanelOpen(true)
  }

  function handleSelectScan(scan: ScanJob) {
    setTerminalNodeKey(null)
    setView('scan')
    scanSession.selectScan(scan)
  }

  async function handleDeleteScan(scan: ScanJob) {
    if (!(await confirm({ description: td('deleteScanConfirm'), destructive: true }))) return
    void scanSession.removeScan(scan.id)
  }

  // Deleting a session also tears down its live subscription, so confirm first —
  // matches every other destructive action in the app (scan / credential / node).
  async function handleDeleteSession(id: string) {
    if (!(await confirm({ description: tc('deleteSessionConfirm'), destructive: true }))) return
    void chat.deleteSession(id)
  }

  return (
    <ThemeProvider initial={getInitialTheme()} storageKey="aiscan-theme" className="aspect-theme-root h-full text-foreground font-sans antialiased">
    <TooltipProvider delayDuration={300}>
      <div className="flex h-screen flex-col overflow-hidden">
        <DeckAmbient scanning={scanSession.scanning} />
        <DeckTopBar
          view={view}
          onSwitchView={switchView}
          model={model}
          analysisAvailable={analysisAvailable}
          scan={scanSession.activeScan}
          scans={scanSession.scans}
          onSelectScan={handleSelectScan}
          onDeleteScan={handleDeleteScan}
          scanning={scanSession.scanning}
          elapsed={elapsed}
          chatCrumb={chatCrumb}
          actions={
            view === 'scan' ? (
              <>
                <ProjectSelector project={project} projects={projects} onSelect={setProject} onCreate={createProject} onDelete={deleteProject} />
                <ScanAgentsButton count={chat.agents.length} onClick={handleOpenAgentPanel} />
                <HeaderIconButton label={t('openDeploy')} onClick={() => setDeployOpen(true)}>
                  <Cloud className="h-3.5 w-3.5" />
                </HeaderIconButton>
                <HeaderIconButton label={t('openSettings')} onClick={() => setConfigOpen(true)}>
                  <Settings className="h-3.5 w-3.5" />
                </HeaderIconButton>
                <LanguageToggle />
                <ConnectedThemeToggle />
              </>
            ) : (
              <>
                <ProjectSelector project={project} projects={projects} onSelect={setProject} onCreate={createProject} onDelete={deleteProject} />
                <ScanAgentsButton count={chat.agents.length} onClick={handleOpenAgentPanel} />
                <HeaderIconButton label={tc('cloudDeploy')} onClick={() => setDeployOpen(true)}>
                  <Cloud className="h-3.5 w-3.5" />
                </HeaderIconButton>
                <HeaderIconButton label={tc('llmConfig')} onClick={() => setConfigOpen(true)}>
                  <Settings className="h-3.5 w-3.5" />
                </HeaderIconButton>
                <LanguageToggle />
                <ConnectedThemeToggle />
              </>
            )
          }
        />

        <div className="flex min-h-0 flex-1 overflow-hidden">
          {/* key={view} replays the soft fade/rise on every SCAN⇄AGENT swap so the
              body cross-dissolves instead of snapping. overflow-hidden on the parent
              keeps the transient mount reflow from flashing a document scrollbar. */}
          <div key={view} className="flex min-h-0 w-full animate-fade-in">
          {view === 'scan' ? (
            <main className="flex min-h-0 min-w-0 flex-1">
              <ScanWorkspace
                activeScan={scanSession.activeScan}
                lines={scanSession.progressLines}
                result={scanSession.result}
                scanning={scanSession.scanning}
                error={scanSession.error}
                analysisAvailable={analysisAvailable}
                agents={chat.agents}
                llmModel={serverStatus?.llm_model}
                llmProvider={serverStatus?.llm_provider}
                now={now}
                onSubmit={scanSession.submit}
                onClearError={scanSession.clearError}
                onCommandCortex={handleCommandCortex}
                onOpenNode={handleOpenNode}
                assets={assetPool.assets}
                onAddAsset={(raw) => assetPool.add(raw, 'manual')}
                onRemoveAsset={assetPool.remove}
                onDispatchAgent={handleDispatchAgent}
              />
            </main>
          ) : (
            <>
              <SessionList
                open={sidebarOpen}
                onToggle={() => setSidebarOpen(!sidebarOpen)}
                agents={chat.agents}
                sessions={chat.sessions}
                activeSessionID={chat.activeSessionID}
                selectedAgentID={chat.selectedAgentID}
                terminalAgentID={terminalAgent?.id ?? null}
                onSelectAgent={chat.selectAgent}
                onSelectSession={handleSelectSession}
                onCreateSession={handleCreateSession}
                onDeleteSession={handleDeleteSession}
                onOpenTerminal={handleOpenTerminal}
              />

              {terminalAgent ? (
                <section className="relative min-h-0 min-w-0 flex-1">
                  <div className="absolute inset-0 flex flex-col">
                    <Suspense fallback={<div className="flex-1" />}>
                      <AgentTerminal agent={terminalAgent} />
                    </Suspense>
                  </div>
                </section>
              ) : (
                <>
                  <ChatPanel
                    timeline={chat.timeline}
                    streamingText={chat.streamingText}
                    streamingAgent={chat.streamingAgent}
                    scanResults={chat.scanResults}
                    isThinking={chat.isThinking}
                    isBusy={chat.busy}
                    error={chat.error}
                    activeSessionID={chat.activeSessionID}
                    hasActiveSession={chat.activeSessionID !== null}
                    agentOffline={activeAgentOffline}
                    agentName={activeSession?.agent_name}
                    onSend={chat.sendMessage}
                    onPause={chat.cancelMessage}
                    onClearError={chat.clearError}
                    onShowScanDetail={handleShowScanDetail}
                    detailOpen={showDetail}
                  />

                  {/* Cortex reasoning + asset pool + agent nodes rail — the
                      reasoning channel belongs to the agent console (fed by the
                      live turn) and the asset pool is shared with the scan deck,
                      so both ride alongside the chat. Throughput / command box
                      are scan-deck concerns and stay hidden here. */}
                  <div className="hidden min-h-0 w-[348px] shrink-0 xl:flex">
                    <IntelRail
                      reasoningSummary={agentSummary}
                      agents={chat.agents}
                      providerLabel={providerLabel}
                      scanning={chat.isThinking}
                      onOpenNode={handleOpenNode}
                      showCommand={false}
                      assets={assetPool.assets}
                      onAddAsset={(raw) => assetPool.add(raw, 'manual')}
                      onRemoveAsset={assetPool.remove}
                      onScanAsset={handleScanAsset}
                      onDispatchAsset={handleDispatchAgent}
                      hasAgents={chat.agents.length > 0}
                      scanBusy={scanSession.scanning}
                    />
                  </div>

                  <div
                    className={cn(
                      'shrink-0 transition-[width,opacity] duration-200 ease-in-out overflow-hidden',
                      showDetail ? 'w-full lg:w-[28rem] opacity-100' : 'w-0 opacity-0',
                    )}
                  >
                    {showDetail && (
                      <DetailPanel
                        scanID={chat.detailScanID!}
                        result={detailResult}
                        onClose={() => setDetailOpen(false)}
                      />
                    )}
                  </div>
                </>
              )}
            </>
          )}
          </div>
        </div>
      </div>

      <ConfigPanel
        open={configOpen}
        status={serverStatus}
        onClose={() => setConfigOpen(false)}
        onSaved={refreshStatus}
      />

      <DeployPanel open={deployOpen} onClose={() => setDeployOpen(false)} defaultSpace={project} />

      <AgentPanel
        open={agentPanelOpen}
        focusAgentID={agentPanelFocusID ?? undefined}
        onClose={() => setAgentPanelOpen(false)}
      />
    </TooltipProvider>
    </ThemeProvider>
  )
}

function ConnectedThemeToggle() {
  const { isDark, toggle } = useTheme()
  return <ThemeToggle isDark={isDark} onToggle={toggle} size="sm" />
}

function ScanAgentsButton({ count, onClick }: { count: number; onClick: () => void }) {
  const { t } = useTranslation('app')
  const active = count > 0
  return (
    <button
      type="button"
      onClick={onClick}
      title={active ? t('agentsConnected', { count }) : t('noAgents')}
      aria-label={active ? t('agentsConnected', { count }) : t('noAgents')}
      className={cn(
        'inline-flex h-7 shrink-0 cursor-pointer items-center gap-1.5 rounded-md border px-2 text-[10px] font-medium transition-colors hover:opacity-80',
        // A connection count is neutral status, not an alert — keep warm hues for
        // severity only. Blue when connected, quiet neutral when none.
        active
          ? 'border-primary/30 bg-primary/10 text-primary'
          : 'border-border bg-secondary/50 text-muted-foreground',
      )}
    >
      <Monitor className="h-3 w-3" aria-hidden="true" />
      <span className="font-mono" aria-hidden="true">{count}</span>
    </button>
  )
}

function HeaderIconButton({ children, label, onClick, active }: { children: ReactNode; label: string; onClick: () => void; active?: boolean }) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={onClick}
      className={cn(
        'inline-flex h-7 w-7 items-center justify-center rounded-md hover:bg-accent hover:text-foreground',
        active ? 'bg-primary/10 text-primary' : 'text-muted-foreground',
      )}
    >
      {children}
    </button>
  )
}
