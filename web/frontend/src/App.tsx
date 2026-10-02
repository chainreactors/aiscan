import { useState, useEffect, useCallback, useMemo, lazy, Suspense, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Activity, LogOut, Menu, Monitor, Network, Settings } from 'lucide-react'
import SessionList from './components/SessionList'
import ChatPanel from './components/ChatPanel'
import ConfigPanel from './components/ConfigPanel'
import AgentPanel from './components/AgentPanel'
import ObservabilityPanel from './components/ObservabilityPanel'
import { useObservations } from '@/viewer'
import { assetMentionables } from './components/AssetPanel'
import MentionPicker from './components/MentionPicker'
import LLMHealth from './components/LLMHealth'
import QuickConnect from './components/QuickConnect'
import BrandLogo from './components/brand/BrandLogo'
const IOAConsole = lazy(() => import('./components/IOAConsole'))
import { Button, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Tooltip, TooltipContent, TooltipProvider, TooltipTrigger, useConfirm } from '@cyber/ui'
import { ThemeProvider } from '@cyber/theme'
import { activateLLMProfile, getConfigStatus, getIOAOverview, getStatus, logout, registerCapabilityProtocols } from './api'
import type { IOAMessage, IOANode, LLMProviderView, ServerStatus } from './api'
import type { SCONode } from '@cyber/cstx-easm'
import type { MentionPopupApi } from './viewer'
import { useChatSession } from './hooks/useChatSession'
import { useGuardrailReviews } from './hooks/useGuardrailReviews'
import { GuardrailToggle } from './components/GuardrailToggle'
import { usePolling } from './hooks/usePolling'
import { isSessionAgentOnline } from './lib/session-agent'
import type { IOAConsoleTarget } from './lib/ioa-navigation'
import { cn } from '@cyber/theme'
import { listSCONodes, subscribeCSTXChanges, syncCSTXArtifacts } from './lib/cstx-runtime'
import { capabilityPlugin, loadCapabilityManifest, WebPluginRuntime, type CapabilityManifest } from './lib/plugin-runtime'

const sidebarStorageKey = 'cyber-sidebar-open'
const NODE_TRANSPORT_CAPABILITIES = new Set(['repl', 'pty', 'tmux', 'file', 'sco'])

const EMPTY_SEED = { text: '', nonce: 0 }
type ToolPanel = 'ioa' | 'agents' | 'observability' | 'settings'

// Respect a previously-chosen theme on boot. ThemeProvider's own initializer is
// short-circuited by the `initial` prop (it returns `initial` before ever reading
// storage), so we read the persisted value here and feed it in as the initial —
// otherwise every reload snaps back to the light default.
function getInitialTheme(): 'light' | 'dark' {
  if (typeof window === 'undefined') return 'light'
  const v = window.localStorage.getItem('cyber-theme')
  return v === 'dark' || v === 'light' ? v : 'light'
}

function getInitialSidebarOpen() {
  if (typeof window === 'undefined') return true
  if (window.matchMedia('(max-width: 767px)').matches) return false
  const stored = window.localStorage.getItem(sidebarStorageKey)
  if (stored === 'true' || stored === 'false') return stored === 'true'
  return window.matchMedia('(min-width: 1024px)').matches
}

export default function App() {
  const { t } = useTranslation('app')
  const { t: tc } = useTranslation('chat')
  const { t: to } = useTranslation('observe')
  const confirm = useConfirm()
  const chat = useChatSession()
  const observations = useObservations(chat.aopEvents)
  const guardrailSessions = useMemo(() => {
    const online = new Set(chat.agents.map(agent => agent.hello?.nodeId))
    const ids = chat.sessions.filter(record => online.has(record.session?.nodeId) && record.session?.state !== 'closed')
      .map(record => record.session?.id || '').filter(Boolean)
    if (chat.activeSessionID && !ids.includes(chat.activeSessionID)) ids.push(chat.activeSessionID)
    return ids
  }, [chat.agents, chat.sessions, chat.activeSessionID])
  const guardrails = useGuardrailReviews(guardrailSessions, chat.activeSessionID, chat.aopEvents)
  const pendingReviewCounts = useMemo(() => Object.fromEntries(Object.entries(guardrails.bySession).map(([id, reviews]) => [id, reviews.length])), [guardrails.bySession])
  const [serverStatus, setServerStatus] = useState<ServerStatus | null>(null)
  const [llmProfiles, setLLMProfiles] = useState<LLMProviderView[]>([])
  const [activeLLMProfile, setActiveLLMProfile] = useState('')
  const [switchingLLM, setSwitchingLLM] = useState(false)
  const [activeToolPanel, setActiveToolPanel] = useState<ToolPanel | null>(null)
  const [settingsSection, setSettingsSection] = useState<'llm' | 'jev'>('llm')
  const [ioaConsoleTarget, setIOAConsoleTarget] = useState<IOAConsoleTarget | null>(null)
  const [agentPanelFocusNodeID, setAgentPanelFocusNodeID] = useState<string | null>(null)
  const [sidebarOpen, setSidebarOpen] = useState(getInitialSidebarOpen)
  // Bumped after a settings save so the header LLM health dot re-probes.
  const [healthNonce, setHealthNonce] = useState(0)
  const [capabilityManifest, setCapabilityManifest] = useState<CapabilityManifest | null>(null)
  // A profile can contribute a capability through its connected node. The
  // server manifest describes mounted Hub services; node hello capabilities
  // describe external profile services. The runtime sees the union.
  const effectiveManifest = useMemo<CapabilityManifest | null>(() => {
    if (!capabilityManifest) return null
    const ids = new Set(capabilityManifest.capabilities.map(item => item.id))
    for (const agent of chat.agents) {
      for (const id of agent.hello?.capabilities || []) {
        if (!NODE_TRANSPORT_CAPABILITIES.has(id)) ids.add(id)
      }
    }
    return { ...capabilityManifest, capabilities: [...ids].map(id => capabilityManifest.capabilities.find(item => item.id === id) || { id }) }
  }, [capabilityManifest, chat.agents])
  const pluginRuntime = useMemo(() => effectiveManifest ? new WebPluginRuntime(effectiveManifest) : null, [effectiveManifest])
  const capabilityIDs = useMemo(() => effectiveManifest?.capabilities.map(item => item.id) || [], [effectiveManifest])
  const scanEnabled = capabilityIDs.includes('scan')

  useEffect(() => {
    const controller = new AbortController()
    void loadCapabilityManifest(controller.signal).then(setCapabilityManifest).catch(() => {
      // Older hubs have no manifest endpoint; the core shell remains usable.
      setCapabilityManifest({ product: 'cyber-harness', capabilities: [{ id: 'core' }] })
    })
    return () => controller.abort()
  }, [])

  useEffect(() => {
    if (!pluginRuntime) return
    pluginRuntime.mount([
      capabilityPlugin('scan-protocol', ['scan'], () => {
        registerCapabilityProtocols('scan')
      }),
    ])
    return () => pluginRuntime.dispose()
  }, [pluginRuntime])

  const toggleToolPanel = useCallback((panel: ToolPanel) => {
    setActiveToolPanel((current) => current === panel ? null : panel)
  }, [])

  const openSettings = useCallback((section: 'llm' | 'jev' = 'llm') => {
    setSettingsSection(section)
    setActiveToolPanel('settings')
  }, [])

  const openIOAConsole = useCallback((target?: IOAConsoleTarget) => {
    setIOAConsoleTarget(target ?? null)
    setActiveToolPanel('ioa')
  }, [])

  const refreshStatus = useCallback(async () => {
    const [statusResult, configResult] = await Promise.allSettled([getStatus(), getConfigStatus()])
    if (statusResult.status === 'fulfilled') setServerStatus(statusResult.value)
    if (configResult.status === 'fulfilled') {
      const profiles = configResult.value.llm?.providers ?? []
      setLLMProfiles(profiles)
      setActiveLLMProfile(configResult.value.llm?.activeProfile || profiles[0]?.id || '')
    }
  }, [])

  useEffect(() => {
    refreshStatus()
  }, [refreshStatus])

  // Keep the header (model + agent count + health base) fresh without a reload.
  usePolling(refreshStatus, 30000)

  useEffect(() => {
    window.localStorage.setItem(sidebarStorageKey, String(sidebarOpen))
  }, [sidebarOpen])

  // Sources for the chat composer's "@" picker: CSTX assets, plus IOA
  // nodes/messages. Both refresh when the timeline advances — a finished scan or
  // agent turn often means new assets landed or new IOA traffic was exchanged.
  const [scoNodes, setScoNodes] = useState<SCONode[]>([])
  const [ioaAvailable, setIoaAvailable] = useState(false)
  const [ioaNodes, setIoaNodes] = useState<IOANode[]>([])
  const [ioaMessages, setIoaMessages] = useState<IOAMessage[]>([])
  const [composerSeed, setComposerSeed] = useState(EMPTY_SEED)

  const refreshSCONodes = useCallback(async () => {
    if (!scanEnabled) {
      setScoNodes([])
      return
    }
    try {
      const { items: data } = await listSCONodes()
      setScoNodes(data)
    } catch { /* non-critical */ }
  }, [scanEnabled])

  const refreshIOA = useCallback(async () => {
    try {
      const overview = await getIOAOverview()
      setIoaAvailable(true)
      setIoaNodes(overview.nodes)
      setIoaMessages(overview.messages)
    } catch { setIoaAvailable(false) }
  }, [])

  useEffect(() => {
    if (!scanEnabled) {
      setScoNodes([])
      return
    }
    void refreshSCONodes()
    const unsubscribe = subscribeCSTXChanges(() => { void refreshSCONodes() })
    return unsubscribe
  }, [refreshSCONodes, scanEnabled])
  // Refresh mentionables when scans finish (timeline changes often signal new results)
  useEffect(() => {
    if (scanEnabled) void syncCSTXArtifacts().catch(() => {})
    void refreshIOA()
  }, [chat.timeline.length, refreshIOA, scanEnabled])

  const mentionables = useMemo(() => assetMentionables(scoNodes), [scoNodes])

  const handleAssetSendToChat = useCallback((text: string) => {
    setComposerSeed({ text, nonce: Date.now() })
  }, [])

  // Always render the picker: even with no assets or IOA traffic yet, the File
  // category (attach) is available inside a live session, so "@" is never a
  // dead key. The picker itself decides which category tabs to show.
  const renderMentionPopup = useCallback(
    (api: MentionPopupApi) => (
      <MentionPicker {...api} nodes={scoNodes} ioaNodes={ioaNodes} ioaMessages={ioaMessages} />
    ),
    [scoNodes, ioaNodes, ioaMessages],
  )

  const model = serverStatus?.llmModel || ''

  const handleSwitchLLM = useCallback(async (profileID: string) => {
    if (!profileID || profileID === activeLLMProfile) return
    setSwitchingLLM(true)
    try {
      const next = await activateLLMProfile(profileID)
      setLLMProfiles(next.llm?.providers ?? [])
      setActiveLLMProfile(next.llm?.activeProfile || profileID)
      await refreshStatus()
      setHealthNonce((nonce) => nonce + 1)
    } catch {
      openSettings()
    } finally {
      setSwitchingLLM(false)
    }
  }, [activeLLMProfile, refreshStatus, openSettings])
  const activeSession = chat.activeSessionRecord?.session?.id === chat.activeSessionID ? chat.activeSessionRecord : chat.sessions.find((s) => s.session?.id === chat.activeSessionID) || null
  const executionNode = chat.agents.find((a) => a.hello?.nodeId === (activeSession?.session?.nodeId || chat.selectedNodeID))
  // The open session's bound agent has dropped off the live roster (its node
  // exited / the hub restarted). The transcript still shows, but a new turn
  // can't be dispatched until it reconnects — surface that in the chat panel.
  const activeAgentOffline = !!activeSession && !isSessionAgentOnline(activeSession, chat.agents)

  // On phones the sidebar is an overlay drawer (see SessionList); entering a
  // conversation or terminal should dismiss it so the content isn't left covered.
  // No-op at md+ where the sidebar is a docked rail that shares the row.
  function closeSidebarOnMobile() {
    if (typeof window !== 'undefined' && window.matchMedia('(max-width: 767px)').matches) {
      setSidebarOpen(false)
    }
  }

  function handleOpenTerminal(nodeID: string) {
    setAgentPanelFocusNodeID(nodeID)
    setActiveToolPanel('agents')
    chat.selectNode(nodeID)
    closeSidebarOnMobile()
  }

  function handleSelectSession(id: string) {
    setActiveToolPanel((current) => current === 'agents' ? null : current)
    chat.selectSession(id)
    closeSidebarOnMobile()
  }

  function handleCreateSession(nodeID: string) {
    setActiveToolPanel((current) => current === 'agents' ? null : current)
    chat.createSession(nodeID)
    closeSidebarOnMobile()
  }

  // Deleting a session also tears down its live subscription, so confirm first —
  // matches every other destructive action in the app (node / config).
  async function handleDeleteSession(id: string) {
    if (!(await confirm({ description: tc('deleteSessionConfirm'), destructive: true }))) return
    void chat.deleteSession(id)
  }

  // Agent node clicked (roster / terminal open) → open the agent console focused
  // on that node.
  function handleOpenNode(nodeID: string) {
    setAgentPanelFocusNodeID(nodeID)
    setActiveToolPanel('agents')
  }

  function handleOpenAgentPanel() {
    setAgentPanelFocusNodeID(null)
    toggleToolPanel('agents')
  }

  return (
    <ThemeProvider initial={getInitialTheme()} storageKey="cyber-theme" className="aspect-theme-root h-full text-foreground font-sans antialiased">
    <TooltipProvider delayDuration={300}>
      <div className="flex h-[100dvh] flex-col overflow-hidden" data-cyber-product={effectiveManifest?.product || 'cyber-harness'} data-cyber-capabilities={effectiveManifest?.capabilities.map(item => item.id).join(',') || 'core'}>
        <header className="relative z-[60] flex min-h-12 shrink-0 items-center justify-between gap-1 border-b border-border/60 bg-background px-2 pt-safe sm:gap-2 sm:px-4">
          <div className="flex min-w-0 items-center gap-1 sm:gap-2">
            {/* Phone-only drawer opener — the collapsed sidebar is hidden below md,
                so the session history opens from here (Doubao-style). */}
            <Button
              variant="ghost"
              size="icon-xs"
              onClick={() => setSidebarOpen(true)}
              aria-label={t('openSessions')}
              className="-ml-1 shrink-0 text-muted-foreground md:hidden"
            >
              <Menu className="h-4 w-4" />
            </Button>
            <BrandLogo size={22} className="hidden shrink-0 sm:block" />
            <span className="shrink-0 text-sm font-semibold tracking-tight text-foreground">Cyber</span>
            <span className="max-w-48 truncate text-xs text-muted-foreground" title={activeSession?.session?.nodeId || chat.selectedNodeID || ''}>{executionNode?.hello?.name || activeSession?.agentName || 'Node'} · {executionNode?.status?.model || '—'}</span>
            <span className="text-[10px] text-muted-foreground">Hub</span>
            <LLMProfileSwitcher
              profiles={llmProfiles}
              activeProfileID={activeLLMProfile}
              fallbackModel={model}
              disabled={switchingLLM}
              onChange={handleSwitchLLM}
            />
            <span className="hidden sm:contents">
              <LLMHealth onOpenSettings={() => openSettings()} reloadSignal={healthNonce} />
            </span>
          </div>
          <div className="flex items-center gap-0.5 sm:gap-2">
            <GuardrailToggle disabled={activeToolPanel === 'settings'} onConfigure={() => openSettings('jev')} />
            <span className="relative">
              <HeaderIconButton label={to('open', { count: observations.length })} active={activeToolPanel === 'observability'} toolDrawerTrigger onClick={() => toggleToolPanel('observability')}>
                <Activity className="h-3.5 w-3.5" />
              </HeaderIconButton>
              {observations.length > 0 && <span className="pointer-events-none absolute bottom-0 right-0 h-1.5 w-1.5 rounded-full bg-primary" />}
            </span>
            {ioaAvailable && <IOAConsoleButton open={activeToolPanel === 'ioa'} onClick={() => {
              setIOAConsoleTarget(null)
              toggleToolPanel('ioa')
            }} />}
            <AgentsButton count={chat.agents.length} open={activeToolPanel === 'agents'} onClick={handleOpenAgentPanel} />
            <QuickConnect serverURL={serverStatus?.serverUrl} version={serverStatus?.version} profiles={effectiveManifest?.profiles} />
            {/* Separate workspace nav (observations / IOA / agents / connect) from the
                account utilities (settings / logout) so the row reads as two groups. */}
            <span className="mx-0.5 hidden h-5 w-px shrink-0 bg-border/70 sm:block" aria-hidden="true" />
            <HeaderIconButton label={t('openSettings')} active={activeToolPanel === 'settings'} toolDrawerTrigger onClick={() => {
              if (activeToolPanel === 'settings') setActiveToolPanel(null)
              else openSettings()
            }}>
              <Settings className="h-3.5 w-3.5" />
            </HeaderIconButton>
            <HeaderIconButton label={t('logout')} onClick={() => { void logout() }}>
              <LogOut className="h-3.5 w-3.5" />
            </HeaderIconButton>
          </div>
        </header>

        <div className="flex min-h-0 flex-1 overflow-hidden">
          <SessionList
            open={sidebarOpen}
            onToggle={() => setSidebarOpen(!sidebarOpen)}
            agents={chat.agents}
            sessions={chat.sessions}
            pendingReviewCounts={pendingReviewCounts}
            filters={chat.sessionFilters}
            onFilter={chat.filterSessions}
            onUpdateSession={chat.updateSession}
            activeSessionID={chat.activeSessionID}
            activeSessionNodeID={activeSession?.session?.nodeId || null}
            activeSessionBusy={chat.busy}
            selectedNodeID={chat.selectedNodeID}
            onSelectSession={handleSelectSession}
            onCreateSession={handleCreateSession}
            onDeleteSession={handleDeleteSession}
          />

          <ChatPanel
            timeline={chat.timeline}
            guardrailUnavailable={guardrails.unavailable[chat.activeSessionID || ''] === true}
            guardrailReviews={guardrails.bySession[chat.activeSessionID || ''] || []}
            onResolveGuardrail={(review, approve) => guardrails.resolve(chat.activeSessionID!, review, approve)}
            aopEvents={chat.aopEvents}
            isThinking={chat.isThinking}
            isBusy={chat.busy}
            canPause={chat.canPause}
            error={chat.error}
            activeSessionID={chat.activeSessionID}
            hasActiveSession={chat.activeSessionID !== null}
            agentOffline={activeAgentOffline}
            agentName={activeSession?.agentName}
            agents={chat.agents.map((a) => ({ nodeID: a.hello?.nodeId || '', name: a.hello?.name || '' }))}
            onCreateSession={handleCreateSession}
            onOpenTerminal={handleOpenTerminal}
            onOpenIOA={openIOAConsole}
            mentionables={mentionables}
            renderMentionPopup={renderMentionPopup}
            injectText={composerSeed}
            onSend={chat.sendMessage}
            ensureSession={chat.ensureSession}
            onPause={chat.cancelMessage}
            onClearError={chat.clearError}
          />
        </div>
      </div>

      <ConfigPanel
        open={activeToolPanel === 'settings'}
        status={serverStatus}
        capabilities={capabilityIDs}
        initialSection={settingsSection}
        onClose={() => setActiveToolPanel(null)}
        onSaved={() => { refreshStatus(); setHealthNonce((n) => n + 1) }}
      />

      <AgentPanel
        open={activeToolPanel === 'agents'}
        agents={chat.agents}
        focusNodeID={agentPanelFocusNodeID ?? undefined}
        onClose={() => setActiveToolPanel(null)}
      />

      <ObservabilityPanel key={chat.activeSessionID || 'no-session'} open={activeToolPanel === 'observability'}
        events={chat.aopEvents} sessionID={chat.activeSessionID} assetCount={scoNodes.length}
        onClose={() => setActiveToolPanel(null)} onSendToChat={handleAssetSendToChat} onAssetsChanged={refreshSCONodes} />

      {activeToolPanel === 'ioa' && (
        <Suspense fallback={null}>
          <IOAConsole
            open
            initialSpaceID={ioaConsoleTarget?.spaceID}
            initialMessageID={ioaConsoleTarget?.messageID}
            onClose={() => {
              setActiveToolPanel(null)
              setIOAConsoleTarget(null)
            }}
          />
        </Suspense>
      )}
    </TooltipProvider>
    </ThemeProvider>
  )
}

function LLMProfileSwitcher({
  profiles,
  activeProfileID,
  fallbackModel,
  disabled,
  onChange,
}: {
  profiles: LLMProviderView[]
  activeProfileID: string
  fallbackModel: string
  disabled: boolean
  onChange: (profileID: string) => void
}) {
  const { t } = useTranslation('app')

  if (profiles.length === 0) {
    return <span className="ml-1 hidden font-mono text-[10px] uppercase tracking-wider text-muted-foreground sm:inline">{fallbackModel}</span>
  }

  return (
    <Select value={activeProfileID || profiles[0].id} onValueChange={onChange} disabled={disabled}>
      <SelectTrigger
        aria-label={t('switchLLMProfile')}
        className="ml-1 hidden h-7 w-auto min-w-[120px] max-w-[230px] gap-1 border-0 bg-transparent px-2 font-mono text-[10px] text-muted-foreground shadow-none hover:bg-muted/60 hover:text-foreground sm:flex"
      >
        <SelectValue placeholder={fallbackModel} />
      </SelectTrigger>
      <SelectContent align="start">
        {profiles.map(profile => (
          <SelectItem key={profile.id} value={profile.id}>
            {profile.name || profile.model || profile.provider}
            {profile.model && profile.name !== profile.model ? ` · ${profile.model}` : ''}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

function AgentsButton({ count, open, onClick }: { count: number; open: boolean; onClick: () => void }) {
  const { t } = useTranslation('app')
  const active = count > 0
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="xs"
          active={open}
          data-tool-drawer-trigger
          onClick={onClick}
          aria-label={active ? t('agentsConnected', { count }) : t('noAgents')}
          className={cn(
            'h-7 w-7 shrink-0 cursor-pointer gap-0 rounded-md border px-0 hover:opacity-80 sm:w-auto sm:gap-1.5 sm:px-2.5',
            // A connection count is neutral status, not an alert — keep warm hues for
            // severity only. Blue when connected, quiet neutral when none.
            active
              ? 'border-primary/30'
              : 'border-border bg-secondary/50 text-muted-foreground hover:bg-secondary/50 hover:text-muted-foreground',
          )}
        >
          <Monitor className="h-3 w-3" aria-hidden="true" />
          <span className="hidden font-mono sm:inline" aria-hidden="true">{count}</span>
        </Button>
      </TooltipTrigger>
      <TooltipContent>{active ? t('agentsConnected', { count }) : t('noAgents')}</TooltipContent>
    </Tooltip>
  )
}

function IOAConsoleButton({ open, onClick }: { open: boolean; onClick: () => void }) {
  const { t } = useTranslation('ioa')
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="xs"
          active={open}
          data-tool-drawer-trigger
          onClick={onClick}
          aria-label={t('openConsole')}
          className="h-7 w-7 shrink-0 cursor-pointer gap-0 rounded-md border border-border bg-secondary/50 px-0 text-muted-foreground hover:border-primary/30 hover:bg-primary/10 hover:text-primary sm:w-auto sm:gap-1.5 sm:px-2.5"
        >
          <Network className="h-3 w-3" aria-hidden="true" />
          <span className="hidden font-mono text-[10px] font-semibold sm:inline" aria-hidden="true">IOA</span>
        </Button>
      </TooltipTrigger>
      <TooltipContent>{t('openConsole')}</TooltipContent>
    </Tooltip>
  )
}

function HeaderIconButton({ children, label, onClick, active, toolDrawerTrigger }: { children: ReactNode; label: string; onClick: () => void; active?: boolean; toolDrawerTrigger?: boolean }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          active={active}
          data-tool-drawer-trigger={toolDrawerTrigger ? '' : undefined}
          aria-label={label}
          onClick={onClick}
          className={cn('hover:text-foreground', !active && 'text-muted-foreground')}
        >
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}
