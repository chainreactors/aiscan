import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { anyUnpack } from '@bufbuild/protobuf/wkt'
import { newID as safeUUID } from '@cyber/aop'
import { createAOPTimelineReducer } from '@/viewer'
import { ScanStatus, SessionScanEventSchema, WebMessageMetadataSchema } from '../cyber-proto'
import { usePolling } from './usePolling'
import {
  cancelChatSession,
  closeChatSession,
  createChatSession,
  deleteChatSession,
  executeChatCommand,
  getChatSession,
  listAgents,
  listChatEvents,
  listChatSessions,
  updateChatSession,
  type SessionFilters,
  resetChatSession,
  sendChatMessage,
  subscribeAOPEvents,
} from '../api'
import type { AgentView, AOPEvent, AOPSession, SessionRecord } from '../api'
import {
  isRootPath,
  parseRoute,
  setSessionRoute,
  type RouteMode,
} from '../lib/route'

function aopExtension(event: AOPEvent): Record<string, unknown> | undefined {
  for (const extension of event.extensions) {
    const value = anyUnpack(extension, WebMessageMetadataSchema)
    if (value) return { nodeId: value.nodeId, code: value.code, params: value.params, agentList: value.agentList }
  }
  return undefined
}

export type TimelineItemKind = 'message' | 'scan_complete' | 'thinking'

// Only pending local messages need a separate render model. Durable messages
// are rendered directly from the AOP event history.
export interface ChatMessage {
  id: string
  session_id: string
  role: 'user' | 'assistant' | 'system'
  node_id?: string
  agent_name?: string
  content: string
  metadata?: Record<string, unknown>
  created_at: string
  cursor?: number
  turn_id?: string
}

export interface TimelineItem {
  id: string
  kind: TimelineItemKind
  timestamp: number
  message?: ChatMessage
  scanID?: string
  agentName?: string
  content?: string
}

// A per-session snapshot of the durable conversation state — everything the
// panel renders that survives a switch away and back. Cached in memory so a
// revisit repaints instantly instead of flashing blank for a network fetch.
interface AOPHistory {
  events: AOPEvent[]
  cursor: string
}

// Deterministic roster order. The hub returns agents in Go-map iteration order,
// which is randomized per request; without a stable sort the sidebar reshuffles
// on every 5s poll. Ordering by node URI keeps the list — and any "first agent"
// auto-pick — put across refreshes.
function sortAgentsByNode(list: AgentView[]): AgentView[] {
  return [...list].sort((a, b) => (a.hello?.nodeId || '').localeCompare(b.hello?.nodeId || ''))
}

function readSessionFilters(): SessionFilters {
  const params = new URLSearchParams(window.location.search)
  return { search: params.get('search') || '', nodeId: params.get('node') || '', archived: params.get('archived') === 'true' }
}

export function useChatSession() {
  const { t } = useTranslation('chat')
  const [agents, setAgents] = useState<AgentView[]>([])
  const [selectedNodeID, setSelectedNodeID] = useState<string | null>(null)
  const [sessionFilters, setSessionFilters] = useState(readSessionFilters)
  const sessionQueryVersion = useRef(0)
  const [sessions, setSessions] = useState<SessionRecord[]>([])
  const [activeSessionRecord, setActiveSessionRecord] = useState<SessionRecord | null>(null)
  const [activeSessionID, setActiveSessionID] = useState<string | null>(null)
  const [timeline, setTimeline] = useState<TimelineItem[]>([])
  const [history, setHistory] = useState<AOPHistory>({ events: [], cursor: '' })
  const aopEvents = history.events
  const [runRequestPending, setRunRequestPending] = useState(false)
  const [receiptTurnID, setReceiptTurnID] = useState('')
  const projection = useMemo(() => createAOPTimelineReducer({ timeline: false }), [activeSessionID])
  const run = useMemo(() => {
    projection(history.events)
    return projection.runState(activeSessionID || '')
  }, [projection, history.events, activeSessionID])
  const activeTurnID = run.activeTurnID || (run.ended.has(receiptTurnID) ? '' : receiptTurnID)
  const isThinking = run.isThinking
  const [error, setError] = useState('')
  const creatingSession = useRef<Promise<string | null> | null>(null)
  const retrySessionCreation = useRef<{ nodeID: string; sessionID: string; requestID: string } | null>(null)
  const retrySubmissions = useRef(new Map<string, { signature: string; messageID: string; requestID: string; turnID: string }>())
  const submittingSessions = useRef(new Set<string | null>())
  const unsubRef = useRef<(() => void) | null>(null)
  const activationRef = useRef(0)
  const activeSessionRef = useRef<string | null>(null)
  const receivedEventIDsRef = useRef<Set<string>>(new Set())
  const sessionCacheRef = useRef<Map<string, AOPHistory>>(new Map())

  // Cache canonical events and the source cursor, without another message or
  // timeline graph. Re-entry fetches only events after this durable cursor.
  useEffect(() => {
    if (!activeSessionID) return
    sessionCacheRef.current.set(activeSessionID, history)
  }, [activeSessionID, history])

  const refreshAgents = useCallback(async () => {
    try {
      const list = sortAgentsByNode(await listAgents())
      setAgents(list)
      setSelectedNodeID((current) => {
        // node_id survives reconnects. Keep an absent selection so a temporary
        // disconnect does not silently retarget the operator to another node.
        return current || list[0]?.hello?.nodeId || null
      })
    } catch {}
  }, [])

  const refreshSessions = useCallback(async () => {
    const version = ++sessionQueryVersion.current
    try {
      const records = await listChatSessions({ search: sessionFilters.search, archived: sessionFilters.archived })
      if (version === sessionQueryVersion.current) setSessions(records)
    } catch (error) { if (version === sessionQueryVersion.current) setError(String(error)) }
  }, [sessionFilters.search, sessionFilters.archived])

  function filterSessions(patch: Partial<SessionFilters>) {
    const next = { ...sessionFilters, ...patch }
    const url = new URL(window.location.href)
    url.searchParams.delete('target')
    url.searchParams.delete('view')
    for (const [key, value] of Object.entries({ search: next.search, node: next.nodeId, archived: next.archived ? 'true' : '' })) {
      if (value) url.searchParams.set(key, value); else url.searchParams.delete(key)
    }
    window.history.replaceState({}, '', url)
    setSessionFilters(next)
  }

  async function updateSession(id: string, patch: { title?: string; archived?: boolean }) {
    try { const record = await updateChatSession(id, patch); if (activeSessionRef.current === id) setActiveSessionRecord(record); await refreshSessions() }
    catch (error) { setError(String(error)); throw error }
  }

  useEffect(() => {
    const restore = () => setSessionFilters(readSessionFilters())
    window.addEventListener('popstate', restore)
    const url = new URL(window.location.href)
    if (url.searchParams.has('view') || url.searchParams.has('target')) {
      url.searchParams.delete('view')
      url.searchParams.delete('target')
      window.history.replaceState({}, '', url)
    }
    return () => window.removeEventListener('popstate', restore)
  }, [])

  useEffect(() => {
    refreshAgents()
    refreshSessions()
  }, [refreshAgents, refreshSessions])
  // Roster poll — paused while the tab is hidden (this runs for the whole app
  // lifetime, so a backgrounded tab would otherwise keep issuing AgentService queries
  // every 5s forever).
  usePolling(refreshAgents, 5000)

  function closeSubscription() {
    if (unsubRef.current) {
      unsubRef.current()
      unsubRef.current = null
    }
  }

  // Clear admission receipts and optimistic state. Run state comes from events.
  function resetTransientState() {
    receivedEventIDsRef.current.clear()
    setReceiptTurnID('')
    setRunRequestPending(false)
    setError('')
  }

  function resetSessionState() {
    activeSessionRef.current = null
    setTimeline([])
    setHistory({ events: [], cursor: '' })
    resetTransientState()
  }

  // Repaint a cached session's durable state instantly (see sessionCacheRef).
  // Runs the same transient wipe as a cold open so a half-streamed response or
  // stale thinking dots from the previous session can't bleed across the switch.
  function restoreSnapshot(snap: AOPHistory) {
    setTimeline([])
    setHistory({ events: snap.events, cursor: snap.cursor })
    resetTransientState()
  }

  function handleAOPEvent(event: AOPEvent, cursor: string) {
    if (event.id) {
      if (receivedEventIDsRef.current.has(event.id)) {
        if (cursor) setHistory(previous => ({ ...previous, cursor }))
        return
      }
      receivedEventIDsRef.current.add(event.id)
    }
    setHistory(previous => ({ events: [...previous.events, event], cursor: cursor || previous.cursor }))
    // Child-session events belong in the conversation renderer, while their
    // lifecycle must not change the root session's Pause target.
    if (event.sessionId !== activeSessionRef.current) return
    switch (event.payload.case) {
      case 'message':
        if (event.payload.value.role === 'user') {
          const messageID = event.payload.value.id
          setTimeline(previous => previous.filter(item => item.id !== messageID))
        }
        break
      case 'extension': {
        const extension = event.payload.value
        try {
          const scan = anyUnpack(extension, SessionScanEventSchema)
          if (!scan) break
          if (!scan.scanId || ![ScanStatus.COMPLETED, ScanStatus.FAILED, ScanStatus.CANCELED].includes(scan.status)) break
          const timelineID = `scanres-${scan.scanId}`
          setTimeline((previous) => previous.some((item) => item.id === timelineID)
            ? previous
            : [...previous, { id: timelineID, kind: 'scan_complete', timestamp: Date.now(), scanID: scan.scanId }])
        } catch {
          // Ignore malformed application extensions; the AOP stream remains usable.
        }
        break
      }
      case 'error': {
        const data = event.payload.value
        // Hub-originated failures carry a translatable code plus i18n params
        // in the cyber.web extension; agent errors are plain text.
        const params = aopExtension(event)?.params as Record<string, unknown> | undefined
        if (data.code) setError(t(`sys.${data.code}`, { ...(params || {}), defaultValue: data.message || '' }))
        else setError(String(data.message ?? 'Agent error'))
        break
      }
    }
  }

  function indexHistory(events: AOPEvent[]) {
    receivedEventIDsRef.current = new Set(events.map(event => event.id).filter(Boolean))
    const scans: TimelineItem[] = []
    for (const event of events) {
      if (event.sessionId !== activeSessionRef.current || event.payload.case !== 'extension') continue
      try {
        const scan = anyUnpack(event.payload.value, SessionScanEventSchema)
        if (!scan?.scanId || ![ScanStatus.COMPLETED, ScanStatus.FAILED, ScanStatus.CANCELED].includes(scan.status)) continue
        const id = `scanres-${scan.scanId}`
        if (!scans.some(item => item.id === id)) scans.push({ id, kind: 'scan_complete', timestamp: event.emittedAt ? Number(event.emittedAt.seconds) * 1000 : 0, scanID: scan.scanId })
      } catch { /* malformed application extension */ }
    }
    setTimeline(previous => [...previous.filter(item => !scans.some(scan => scan.id === item.id)), ...scans])
  }

  async function activateSession(id: string, route: RouteMode) {
    const activation = ++activationRef.current
    closeSubscription()
    // Paint the last-seen conversation from cache synchronously — this runs
    // before the first await, so React batches it with the state below into a
    // single render and the panel jumps straight to the cached messages instead
    // of flashing blank while we revalidate. A cold session has no snapshot yet,
    // so it clears to empty and waits for the fetch as before.
    const cached = sessionCacheRef.current.get(id)
    if (cached) restoreSnapshot(cached)
    else resetSessionState()
    setActiveSessionID(id)
    setActiveSessionRecord(null)
    // Mirror into the ref synchronously so a send issued immediately after
    // activation (e.g. the deck's Command Cortex) targets the new session
    // without waiting for the activeSessionID effect to flush on re-render.
    activeSessionRef.current = id
    if (cached) indexHistory(cached.events)
    setSessionRoute(id, route)

    let afterCursor = cached?.cursor || ''
    try {
      const deliveries = await listChatEvents(id, afterCursor)
      if (activation !== activationRef.current) return
      const known = new Set(cached?.events.map(event => event.id) || [])
      const incoming = deliveries.flatMap(delivery => delivery.event && !known.has(delivery.event.id) ? [delivery.event] : [])
      const events = incoming.length ? [...(cached?.events || []), ...incoming] : cached?.events || []
      afterCursor = deliveries[deliveries.length - 1]?.cursor || afterCursor
      setHistory({ events, cursor: afterCursor })
      indexHistory(events)
      unsubRef.current = subscribeAOPEvents(id, (event, cursor) => {
        if (activation === activationRef.current) handleAOPEvent(event, cursor)
      }, afterCursor)

      const session = await getChatSession(id)
      if (activation !== activationRef.current) return
      setActiveSessionRecord(session)
      const scanIDs = Array.isArray(session.extensions.scan?.ids)
        ? session.extensions.scan.ids.filter((id): id is string => typeof id === 'string')
        : []
      if (scanIDs.length) {
        // Keep status cards visible even if archive sync or parsing fails.
        setTimeline((previous) => {
          const next = [...previous]
          for (const scanID of scanIDs) {
            const id = `scanres-${scanID}`
            if (!next.some((item) => item.id === id)) next.push({ id, kind: 'scan_complete', timestamp: Date.now(), scanID })
          }
          return next
        })
      }
    } catch (error) {
      if (activation === activationRef.current) setError(String(error))
    }

    if (activation !== activationRef.current) return
    if (!unsubRef.current) unsubRef.current = subscribeAOPEvents(id, (event, cursor) => {
      if (activation === activationRef.current) handleAOPEvent(event, cursor)
    }, afterCursor)
  }

  async function handleCreateSession(nodeID: string) {
    try {
      const session = await createChatSession(nodeID)
      setSelectedNodeID(nodeID)
      await refreshSessions()
      await activateSession(session.id, 'push')
    } catch (err: any) {
      setError(err.message || 'Failed to create session')
    }
  }

  async function handleDeleteSession(id: string) {
    try {
      await deleteChatSession(id)
      sessionCacheRef.current.delete(id)
      if (activeSessionID === id) {
        activationRef.current++
        closeSubscription()
        resetSessionState()
        setActiveSessionID(null)
        window.history.pushState({}, '', '/')
      }
      await refreshSessions()
    } catch (err: any) {
      setError(err.message || 'Failed to delete session')
    }
  }

  async function handleSendMessage(content: string, opts?: { persist?: boolean; evalCriteria?: string; evalRounds?: string; sessionID?: string }): Promise<boolean> {
    const submissionScope = opts?.sessionID || activeSessionRef.current
    if (!content.trim() || submittingSessions.current.has(submissionScope)) return false
    submittingSessions.current.add(submissionScope)
    let optimisticID = ''
    let sessionID: string | null = null
    try {
      sessionID = opts?.sessionID || await ensureSession()
      if (!sessionID) return false
      const trimmed = content.trim()
      const lower = trimmed.toLowerCase()
      if (lower === '/clear') {
        const next = await resetChatSession(sessionID)
        if (next.session?.id) await activateSession(next.session.id, 'push')
        await refreshSessions()
        return true
      }
      if (lower === '/stop') { await handleCancelMessage(); return true }
      if (lower === '/exit' || lower === '/quit') { await closeChatSession(sessionID); await refreshSessions(); return true }
      const continueSession = lower === '/continue'
      const runContent = lower.startsWith('/followup ') ? trimmed.slice(trimmed.indexOf(' ') + 1).trim() : trimmed
      const command = !continueSession && (runContent.startsWith('!') || (runContent.startsWith('/') && !runContent.startsWith('/skill:') && !lower.startsWith('/followup ')))
      const signature = JSON.stringify([sessionID, runContent, opts])
      if (retrySubmissions.current.get(sessionID)?.signature !== signature) retrySubmissions.current.set(sessionID, { signature, messageID: safeUUID(), requestID: safeUUID(), turnID: safeUUID() })
      const submission = retrySubmissions.current.get(sessionID)!
      optimisticID = submission.messageID
      if (!continueSession && !command && activeSessionRef.current === sessionID) {
        const message: ChatMessage = { id: optimisticID, session_id: sessionID, role: 'user', content: runContent, created_at: new Date().toISOString() }
        setTimeline((previous) => previous.some((m) => m.id === message.id) ? previous : [...previous, { id: message.id, kind: 'message', timestamp: Date.now(), message }])
      }
      if (activeSessionRef.current === sessionID) { setError(''); setRunRequestPending(true) }
      if (command) await executeChatCommand(sessionID, runContent, submission.requestID)
      else {
        const sent = await sendChatMessage(sessionID, runContent, { ...opts, ...submission, continueSession })
        // Acceptance may queue behind the current run. Its receipt must not
        // redirect Pause away from the running turn; turnStarted selects the
        // next turn once the runtime actually starts it.
        if (activeSessionRef.current === sessionID && !projection.runState(sessionID).activeTurnID && sent.turnId) setReceiptTurnID(sent.turnId)
      }
      retrySubmissions.current.delete(sessionID)
      await refreshSessions()
      return true
    } catch (error) {
      if (sessionID && (error as { rejected?: boolean }).rejected) retrySubmissions.current.delete(sessionID)
      if (activeSessionRef.current === sessionID) {
        setTimeline((previous) => previous.filter((m) => m.id !== optimisticID))
        setError(error instanceof Error ? error.message : 'Failed to send message')
      }
      return false
    } finally { submittingSessions.current.delete(submissionScope); if (activeSessionRef.current === sessionID) setRunRequestPending(false) }
  }

  // Make sure a chat session is active, lazily creating one on the selected (or
  // first connected) node if none is open. Returns the session id, or null if
  // no node is connected / creation failed (error already surfaced). Factored
  // out of handleCommand so the asset-pool "reference" flow can seed a composer
  // draft into a guaranteed-live session without also sending a message.
  async function ensureSession(): Promise<string | null> {
    if (activeSessionRef.current) return activeSessionRef.current
    if (creatingSession.current) return creatingSession.current
    creatingSession.current = createSessionForInput().finally(() => { creatingSession.current = null })
    return creatingSession.current
  }

  async function createSessionForInput(): Promise<string | null> {
    // Prefer the selected node only while it's actually connected; a selection
    // left dangling by a node that went away falls back to the first agent.
    const connected = agents.find((a) => a.hello?.nodeId === selectedNodeID)
    const nodeID = connected?.hello?.nodeId || agents[0]?.hello?.nodeId
    if (!nodeID) {
      setError('No node connected — launch a local agent or connect one first.')
      return null
    }
    try {
      if (retrySessionCreation.current?.nodeID !== nodeID) retrySessionCreation.current = { nodeID, sessionID: safeUUID(), requestID: safeUUID() }
      const session = await createChatSession(nodeID, undefined, undefined, retrySessionCreation.current)
      retrySessionCreation.current = null
      setSelectedNodeID(nodeID)
      activeSessionRef.current = session.id
      await activateSession(session.id, 'push')
      await refreshSessions()
      return session.id
    } catch (err: any) {
      if (err.rejected) retrySessionCreation.current = null
      setError(err.message || 'Failed to start session')
      return null
    }
  }

  // Deck "Command Cortex" entrypoint: route a free-form command from the
  // operation deck into the chat workspace. When no session is open yet it
  // spins one up on the active node first, so the typed text is never dropped.
  async function handleCommand(content: string) {
    const trimmed = content.trim()
    if (!trimmed) return
    if (!(await ensureSession())) return
    await handleSendMessage(trimmed)
  }

  // Channel-2 "quick dispatch": fire a target at an agent in its OWN fresh
  // session (titled with the target), auto-sending the prompt. Deliberately
  // bypasses handleSendMessage — that only targets the ACTIVE session and writes
  // an optimistic bubble, neither of which fits a background dispatch. Returns
  // the new session (or null if no node is connected / it failed).
  async function quickDispatch(
    target: string,
    prompt: string,
    nodeID?: string,
    opts?: { activate?: boolean; skipRefresh?: boolean },
  ): Promise<AOPSession | null> {
    const connected = agents.find((a) => a.hello?.nodeId === selectedNodeID)
    const targetNodeID = nodeID || connected?.hello?.nodeId || agents[0]?.hello?.nodeId
    if (!targetNodeID) {
      setError('No node connected — launch a local agent or connect one first.')
      return null
    }
    try {
      const session = await createChatSession(targetNodeID, target)
      await sendChatMessage(session.id, prompt)
      if (!opts?.skipRefresh) await refreshSessions()
      if (opts?.activate) {
        setSelectedNodeID(targetNodeID)
        await activateSession(session.id, 'push')
      }
      return session
    } catch (err: any) {
      setError(err.message || 'Failed to dispatch agent')
      return null
    }
  }

  // Scan-deck AI actions (数据分析 / 资产评估 / 复测): each opens its OWN fresh
  // session — linked to the originating scan and titled by kind — activates it
  // (routing to the chat workspace), then auto-sends the seed prompt so the
  // agent's streaming run IS the process. The scan deck reverse-finds this
  // session by scan_id to mirror its final conclusion back. Returns the new
  // session id, or null if no node is connected / it failed.
  async function startReportSession(args: {
    title: string
    seedPrompt: string
    scanID?: string
  }): Promise<string | null> {
    const connected = agents.find((a) => a.hello?.nodeId === selectedNodeID)
    const nodeID = connected?.hello?.nodeId || agents[0]?.hello?.nodeId
    if (!nodeID) {
      setError('No node connected — launch a local agent or connect one first.')
      return null
    }
    try {
      const session = await createChatSession(nodeID, args.title, args.scanID)
      setSelectedNodeID(nodeID)
      await refreshSessions()
      await activateSession(session.id, 'push')
      await handleSendMessage(args.seedPrompt)
      return session.id
    } catch (err: any) {
      setError(err.message || 'Failed to start session')
      return null
    }
  }

  // Channel-2 batch fan-out: one fresh session per target, distributed across
  // the connected fleet round-robin so independent nodes run in parallel (a lone
  // node just serializes them on its own task queue). Concurrency-capped so
  // selecting a large pool doesn't fire hundreds of requests at once.
  async function batchQuickDispatch(items: { target: string; prompt: string }[]) {
    const fleet = agents
    if (fleet.length === 0) {
      setError('No node connected — launch a local agent or connect one first.')
      return
    }
    const CONCURRENCY = 6
    for (let i = 0; i < items.length; i += CONCURRENCY) {
      const batch = items.slice(i, i + CONCURRENCY)
      await Promise.all(
        batch.map((it, j) =>
          quickDispatch(it.target, it.prompt, fleet[(i + j) % fleet.length].hello?.nodeId, { skipRefresh: true }),
        ),
      )
    }
    await refreshSessions()
  }

  async function handleCancelMessage() {
    const sessionID = activeSessionRef.current
    const turnID = activeTurnID
    const activation = activationRef.current
    if (!sessionID || !turnID) return
    try {
      await cancelChatSession(sessionID, turnID)
      await refreshSessions()
    } catch (err: any) {
      if (activation === activationRef.current && activeSessionRef.current === sessionID) setError(err.message || 'Failed to pause response')
    }
  }

  useEffect(() => {
    const applyRoute = () => {
      const route = parseRoute(window.location.pathname)
      if (route.kind === 'session') {
        void activateSession(route.id, 'none')
        return
      }
      // Any other path is a retired route (for example a /scans/<id> bookmark).
      // Nothing renders it, so show the session list and normalize the URL.
      if (!isRootPath(window.location.pathname)) {
        window.history.replaceState({}, '', '/')
      }
      activationRef.current++
      closeSubscription()
      resetSessionState()
      setActiveSessionID(null)
    }
    applyRoute()
    window.addEventListener('popstate', applyRoute)
    return () => {
      window.removeEventListener('popstate', applyRoute)
      closeSubscription()
    }
  }, [])

  const clearError = useCallback(() => setError(''), [])

  return {
    agents,
    selectedNodeID,
    sessions,
    sessionFilters, filterSessions, updateSession,
    activeSessionID,
    activeSessionRecord,
    timeline,
    aopEvents,
    isThinking,
    busy: runRequestPending || activeTurnID !== '',
    canPause: activeTurnID !== '',
    error,
    selectNode: (nodeID: string) => {
      setSelectedNodeID(nodeID)
    },
    createSession: handleCreateSession,
    selectSession: (id: string) => activateSession(id, 'push'),
    deleteSession: handleDeleteSession,
    sendMessage: handleSendMessage,
    command: handleCommand,
    ensureSession,
    quickDispatch,
    startReportSession,
    batchQuickDispatch,
    cancelMessage: handleCancelMessage,
    clearError,
  }
}
