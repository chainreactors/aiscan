import { memo, useEffect, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import i18n from '../i18n'
import {
  AlertTriangle,
  CheckCircle2,
  CircleDashed,
  GitBranch,
  MessageSquare,
  Sparkles,
  Target,
  User,
  Wrench,
  X,
} from 'lucide-react'
import { cn } from '@aspect/theme'
import BrandMark from './brand/BrandMark'
import { MarkdownContent } from '@aspect/markdown'
import {
  AssistantResponse,
  ChatInput,
  ChatThinking,
  MessageBubble as ChatMessageBubble,
  ToolCallDisplay as ChatToolCall,
  resolveTimelineRenderer,
  summarizeArgs,
  type ChatAttachment,
  type CommandHint,
  type ExtensionTimelineItem,
} from '@aspect/viewer'
import { uploadChatFile } from '../api'
import type { ChatMessage, ScanResult } from '../api'
import type { AssistantResponseState, TimelineItem } from '../hooks/useChatSession'
import InstrumentIdle from './InstrumentIdle'
import { toViewerTimeline } from '../lib/timeline-mapper'

const workspaceClass = 'mx-auto w-full max-w-[96rem] px-4 sm:px-5 lg:px-6'
const contentOffsetClass = 'xl:ml-[6.75rem]'
const threadOffsetClass = 'xl:mr-[6.75rem]'

interface Props {
  timeline: TimelineItem[]
  streamingText: string | null
  streamingAgent: string | null
  scanResults: Map<string, ScanResult>
  isThinking: boolean
  isBusy: boolean
  error: string
  hasActiveSession: boolean
  activeSessionID: string | null
  agentOffline?: boolean
  agentName?: string
  onSend: (content: string, opts?: { persist?: boolean; maxTurns?: number; evalCriteria?: string; evalMaxRounds?: number }) => void
  onPause: () => void
  onClearError: () => void
  onShowScanDetail: (scanID: string) => void
  detailOpen: boolean
}

export default function ChatPanel({
  timeline,
  streamingText,
  streamingAgent,
  scanResults,
  isThinking,
  isBusy,
  error,
  activeSessionID,
  hasActiveSession,
  agentOffline,
  agentName,
  onSend,
  onPause,
  onClearError,
  onShowScanDetail,
  detailOpen,
}: Props) {
  const { t } = useTranslation('chat')
  const scrollRef = useRef<HTMLDivElement>(null)
  const bottomRef = useRef<HTMLDivElement>(null)
  const stickRef = useRef(true)
  const jumpRef = useRef(false)
  const inputFormClass = cn(contentOffsetClass, !detailOpen && threadOffsetClass)
  const hasAssistantResponse = timeline.some((item) => item.kind === 'assistant_response')
  const [persist, setPersist] = useState(false)
  const [persistMax, setPersistMax] = useState(20)
  const [evalCriteria, setEvalCriteria] = useState('')
  const [evalMaxRounds, setEvalMaxRounds] = useState(3)
  const evalRef = useRef<HTMLTextAreaElement>(null)
  // Screen-reader turn status. Streamed replies mutate the DOM silently, so
  // mirror the coarse turn phase into a polite live region below. It announces
  // transitions (thinking → responding → done), never the token stream itself
  // (a live region on the growing text would restart the reader on every delta).
  const [liveStatus, setLiveStatus] = useState('')
  const wasActiveRef = useRef(false)

  function sendOpts() {
    if (!persist) return undefined
    const criteria = evalCriteria.trim()
    if (criteria) return { persist: true, evalCriteria: criteria, evalMaxRounds }
    return { persist: true, maxTurns: persistMax > 0 ? persistMax : undefined }
  }

  // Slash commands the hub handles directly (see parseSlashCommand /
  // dispatchUserMessage in pkg/web/service.go). Descriptions are i18n'd; the
  // hint popup shows cmd + desc, so keep desc short and put full syntax here.
  const chatCommands: CommandHint[] = [
    { cmd: '/scan', desc: t('cmdScan'), usage: '/scan <target> [--mode full] [--verify] [--sniper] [--deep]' },
    { cmd: '/agents', desc: t('cmdAgents') },
    { cmd: '/shell', desc: t('cmdShell'), usage: '/shell <command>' },
    { cmd: '/help', desc: t('cmdHelp') },
  ]

  // Re-entering a session (switching sessions, or returning to the chat tab)
  // re-pins to the bottom and requests an instant jump, so we land on the
  // latest message instead of sitting at the top.
  useEffect(() => {
    stickRef.current = true
    jumpRef.current = true
    // Goal (persist/eval) mode is per-session intent. ChatPanel doesn't remount
    // on session switch, so clear it here — otherwise session A's done-when
    // criteria stays toggled on and gets silently sent with the next message in
    // session B (an unexpected multi-round agentic run against stale criteria).
    setPersist(false)
    setPersistMax(20)
    setEvalCriteria('')
    setEvalMaxRounds(3)
  }, [activeSessionID])

  useEffect(() => {
    if (!stickRef.current) return
    if (timeline.length === 0 && streamingText === null) return
    const behavior: ScrollBehavior = jumpRef.current ? 'auto' : 'smooth'
    jumpRef.current = false
    bottomRef.current?.scrollIntoView({ behavior })
    // Depend on `timeline` (not `timeline.length`): streamed deltas update the
    // last item in place, producing a new array reference but the same length,
    // so keying on length would freeze autoscroll mid-reply.
  }, [timeline, streamingText, isThinking])

  // Auto-grow the goal criteria textarea (min ~2 rows, capped) so long
  // natural-language goals stay readable instead of scrolling a one-liner.
  useEffect(() => {
    const el = evalRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = Math.min(el.scrollHeight, 144) + 'px'
  }, [evalCriteria, persist])

  // Derive the polite screen-reader status from the turn phase. `isBusy` keeps
  // the "working" state across tool-execution gaps (thinking false, no stream)
  // so the "done" cue fires once when the whole turn actually settles, not on
  // every pause between tool calls.
  useEffect(() => {
    const active = isBusy || isThinking || streamingText !== null
    if (streamingText !== null) setLiveStatus(t('a11yResponding'))
    else if (active) setLiveStatus(t('a11yThinking'))
    else if (wasActiveRef.current) setLiveStatus(t('a11yTurnDone'))
    wasActiveRef.current = active
  }, [isBusy, isThinking, streamingText, t])

  async function handleSendWithAttachments(content: string, attachments?: ChatAttachment[]) {
    if (!attachments?.length) {
      onSend(content, sendOpts())
      return
    }
    const contextParts: string[] = []
    for (const a of attachments) {
      if (a.mode === 'context') {
        const text = await a.file.text()
        contextParts.push(`<file name="${a.file.name}">\n${text}\n</file>`)
      } else if (a.mode === 'upload' && activeSessionID) {
        try {
          await uploadChatFile(activeSessionID, a.file)
        } catch { /* upload error shown via SSE system message */ }
      }
    }
    const fullContent = contextParts.length > 0
      ? `${contextParts.join('\n')}\n\n${content}`
      : content
    if (fullContent.trim()) onSend(fullContent, sendOpts())
  }

  function handleScroll() {
    const el = scrollRef.current
    if (!el) return
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 80
    stickRef.current = atBottom
  }

  return (
    <div className="flex min-w-0 flex-1 flex-col">
      {error && (
        <div
          role="alert"
          className="flex items-start gap-2 border-b border-destructive/30 bg-destructive/10 px-4 py-2 text-sm text-destructive animate-in fade-in slide-in-from-top-1 duration-200"
        >
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
          <span className="min-w-0 flex-1 break-words">{error}</span>
          <button type="button" aria-label={t('dismiss')} onClick={onClearError} className="rounded p-0.5 hover:bg-destructive/10">
            <X className="h-4 w-4" />
          </button>
        </div>
      )}

      <main className="flex min-h-0 flex-1 flex-col bg-transparent">
        {/* Off-screen polite live region — announces the turn phase to screen
            readers without reading the streamed tokens one by one. */}
        <div className="sr-only" role="status" aria-live="polite">{liveStatus}</div>
        <div
          ref={scrollRef}
          onScroll={handleScroll}
          className="min-h-0 flex-1 overflow-y-auto"
        >
          <div className={cn(workspaceClass, 'space-y-3 py-4')}>
            {!hasActiveSession && timeline.length === 0 && (
              <div className={inputFormClass}>
                <EmptyState
                  eyebrow={t('consoleReady')}
                  title={t('startConversation')}
                  subtitle={t('createSession')}
                />
              </div>
            )}
            {hasActiveSession && timeline.length === 0 && !isThinking && streamingText === null && (
              <div className={inputFormClass}>
                <EmptyState
                  eyebrow={t('readyEyebrow')}
                  title={t('ready')}
                  subtitle={
                    <>{t('readyHintBefore')}<code className="rounded bg-muted px-1 py-0.5 text-[10px] font-mono">/scan &lt;target&gt;</code>{t('readyHintAfter')}</>
                  }
                />
              </div>
            )}

            {timeline.map((item) => (
              <TimelineEntry
                key={item.id}
                item={item}
                scanResults={scanResults}
                detailOpen={detailOpen}
                onShowScanDetail={onShowScanDetail}
              />
            ))}

            {streamingText !== null && (
              <StreamingEntry
                text={streamingText}
                agentName={streamingAgent}
                detailOpen={detailOpen}
              />
            )}

            {isThinking && streamingText === null && !hasAssistantResponse && (
              <TimelineRow
                item={{
                  id: 'thinking-live',
                  kind: 'thinking',
                  timestamp: Date.now(),
                  agentName: streamingAgent || undefined,
                }}
                detailOpen={detailOpen}
              >
                <ChatThinking actorName={streamingAgent} />
              </TimelineRow>
            )}

            <div ref={bottomRef} />
          </div>
        </div>

        {hasActiveSession && (
          <div className="border-t border-border bg-card/80 backdrop-blur-sm">
            {agentOffline && (
              <div className={cn(workspaceClass, 'pt-2')}>
                <div className={inputFormClass}>
                  <div className="flex items-start gap-2 rounded-lg border border-warning/30 bg-warning/10 px-3 py-2 text-xs text-warning animate-in fade-in slide-in-from-bottom-1 duration-200">
                    <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                    <span className="min-w-0 break-words">
                      {agentName ? t('agentOfflineBannerNamed', { name: agentName }) : t('agentOfflineBanner')}
                    </span>
                  </div>
                </div>
              </div>
            )}
            {persist && (
              <div className={cn(workspaceClass, 'pt-2')}>
                <div className={inputFormClass}>
                  <div className="rounded-xl border border-primary/25 bg-primary/[0.04] px-3.5 py-2.5 animate-in fade-in slide-in-from-bottom-1 duration-200">
                    <div className="mb-2 flex items-center justify-between gap-2">
                      <span className="inline-flex items-center gap-1.5 text-xs font-medium text-primary">
                        <Target className="h-3.5 w-3.5" />
                        {t('evalCriteriaLabel')}
                        <span className="rounded bg-primary/10 px-1 py-px font-mono text-[10px] font-normal text-primary/70">-e</span>
                      </span>
                      {evalCriteria.trim() ? (
                        <div className="inline-flex shrink-0 items-center gap-2">
                          <span
                            className="inline-flex items-center gap-1 rounded-full bg-ai/10 px-2 py-0.5 text-[11px] font-medium text-ai"
                            title={t('evalModeHint', { rounds: evalMaxRounds })}
                          >
                            <Sparkles className="h-3 w-3" />
                            {t('evalModeBadge')}
                          </span>
                          <label className="inline-flex items-center gap-1.5 text-[11px] text-muted-foreground">
                            {t('evalRoundsLabel')}
                            <input
                              type="number"
                              min={1}
                              max={10}
                              value={evalMaxRounds}
                              onChange={(e) => setEvalMaxRounds(Math.min(10, Math.max(1, parseInt(e.target.value, 10) || 1)))}
                              className="w-12 rounded-md border border-border/70 bg-card/60 px-2 py-0.5 text-xs text-foreground focus:border-ai/50 focus:outline-none focus:ring-1 focus:ring-ai/20"
                            />
                          </label>
                        </div>
                      ) : (
                        <label className="inline-flex shrink-0 items-center gap-1.5 text-[11px] text-muted-foreground">
                          {t('persistMaxTurns')}
                          <input
                            type="number"
                            min={1}
                            max={200}
                            value={persistMax}
                            onChange={(e) => setPersistMax(Math.max(0, parseInt(e.target.value, 10) || 0))}
                            className="w-14 rounded-md border border-border/70 bg-card/60 px-2 py-0.5 text-xs text-foreground focus:border-primary/60 focus:outline-none focus:ring-1 focus:ring-primary/25"
                          />
                        </label>
                      )}
                    </div>
                    <textarea
                      ref={evalRef}
                      rows={2}
                      value={evalCriteria}
                      onChange={(e) => setEvalCriteria(e.target.value)}
                      placeholder={t('evalCriteriaPlaceholder')}
                      className="block max-h-36 min-h-[3.75rem] w-full resize-none overflow-y-auto rounded-lg border border-border/60 bg-card/50 px-3 py-2 text-xs leading-relaxed text-foreground placeholder:text-muted-foreground/60 focus:border-ai/50 focus:outline-none focus:ring-1 focus:ring-ai/20"
                    />
                    {evalCriteria.trim() && (
                      <p className="mt-1.5 text-[11px] leading-relaxed text-muted-foreground/70">
                        {t('evalModeHint', { rounds: evalMaxRounds })}
                      </p>
                    )}
                  </div>
                </div>
              </div>
            )}
            <div className={workspaceClass}>
              <div className={inputFormClass}>
                <ChatInput
                  className="!border-t-0 !bg-transparent !backdrop-blur-none"
                  leading={
                    <button
                      type="button"
                      onClick={() => setPersist((v) => !v)}
                      aria-pressed={persist}
                      title={t('persistHint')}
                      className={cn(
                        'inline-flex h-9 shrink-0 items-center gap-1.5 rounded-full border px-3 text-xs font-medium transition-colors',
                        persist
                          ? 'border-primary/60 bg-primary/15 text-primary'
                          : 'border-border/60 bg-card/50 text-muted-foreground hover:text-foreground',
                      )}
                    >
                      <Target className="h-3.5 w-3.5" />
                      {t('persistMode')}
                    </button>
                  }
                  onSend={handleSendWithAttachments}
                  onPause={onPause}
                  busy={isBusy}
                  commands={chatCommands}
                  placeholder={t('typeMessageWithCommands')}
                  enableAttachments={!!activeSessionID}
                />
              </div>
            </div>
          </div>
        )}
      </main>
    </div>
  )
}

// Memoized: during token streaming useChatSession mints a new `timeline` array
// on every message_delta, but only the streaming item's reference actually
// changes. Without memo, timeline.map re-renders EVERY settled entry each token
// — and each MessageBubble re-parses its markdown (remark) from scratch, so a
// 40-message transcript re-parses 40 docs per token. A shallow prop compare lets
// unchanged entries bail out; it holds only because `onShowScanDetail` is now a
// stable useCallback in App and `scanResults`/`detailOpen` don't change mid-stream.
const TimelineEntry = memo(function TimelineEntry({
  item,
  scanResults,
  detailOpen,
  onShowScanDetail,
}: {
  item: TimelineItem
  scanResults: Map<string, ScanResult>
  detailOpen: boolean
  onShowScanDetail: (scanID: string) => void
}) {
  const content = timelineContent(item, scanResults, onShowScanDetail)
  if (!content) return null

  return (
    <TimelineRow item={item} detailOpen={detailOpen}>
      {content}
    </TimelineRow>
  )
})

function StreamingEntry({
  text,
  agentName,
  detailOpen,
}: {
  text: string
  agentName: string | null
  detailOpen: boolean
}) {
  const now = new Date().toISOString()
  const message: ChatMessage = {
    id: 'streaming-assistant',
    session_id: '',
    role: 'assistant',
    agent_name: agentName || undefined,
    content: text,
    created_at: now,
  }

  return (
    <TimelineRow
      item={{ id: 'streaming-assistant', kind: 'message', timestamp: Date.now(), message }}
      detailOpen={detailOpen}
    >
      <ChatMessageBubble
        role="assistant"
        actorName={agentName || undefined}
        streaming
      >
        {text ? <MarkdownContent content={trimDisplayContent(text)} compact /> : null}
      </ChatMessageBubble>
    </TimelineRow>
  )
}

function TimelineRow({
  item,
  detailOpen,
  children,
}: {
  item: TimelineItem
  detailOpen: boolean
  children: ReactNode
}) {
  return (
    <div
      data-testid="chat-timeline-row"
      data-kind={item.kind}
      className={cn(
        'grid grid-cols-1 gap-y-1 animate-in fade-in slide-in-from-bottom-1 duration-200',
        'xl:gap-x-3',
        detailOpen ? 'xl:grid-cols-[6rem_minmax(0,1fr)]' : 'xl:grid-cols-[6rem_minmax(0,1fr)_6rem]',
      )}
    >
      <TimelineMark item={item} />
      <div data-testid="chat-content" className="min-w-0">
        {children}
      </div>
      {!detailOpen && <IOAThreadNote item={item} />}
    </div>
  )
}

function timelineContent(
  item: TimelineItem,
  scanResults: Map<string, ScanResult>,
  onShowScanDetail: (scanID: string) => void,
): ReactNode {
  switch (item.kind) {
    case 'message':
      if (!item.message) return null
      {
        const role = item.message.role === 'tool_call' || item.message.role === 'tool_result' ? 'system' : item.message.role
        return (
          <ChatMessageBubble
            role={role}
            actorName={item.message.agent_name}
            timestamp={item.message.created_at}
          >
            {item.message.content ? (
              <MarkdownContent content={trimDisplayContent(item.message.content)} compact={role !== 'system'} />
            ) : null}
          </ChatMessageBubble>
        )
      }

    case 'assistant_response':
      if (!item.assistantResponse) return null
      return <AssistantResponseEntry response={item.assistantResponse} />

    case 'tool_call':
      if (!item.toolCall) return null
      return (
        <ChatToolCall
          toolName={item.toolCall.toolName}
          toolArgs={item.toolCall.toolArgs}
          result={item.toolCall.result}
          pending={item.toolCall.pending}
        />
      )

    case 'scan_started':
    case 'scan_progress':
    case 'scan_complete': {
      const mapped = toViewerTimeline([item])
      const ext = mapped[0] as ExtensionTimelineItem | undefined
      if (!ext || ext.kind !== 'extension') return null
      const config = resolveTimelineRenderer(ext.extensionType)
      if (!config) return null
      const Renderer = config.renderer
      return <Renderer item={ext} context={{ scanResults, onShowScanDetail }} />
    }

    case 'thinking':
      return (
        <ChatThinking actorName={item.agentName}>
          {item.content?.trim() ? <MarkdownContent content={trimDisplayContent(item.content)} compact muted /> : null}
        </ChatThinking>
      )

    case 'agent_joined': {
      const mapped = toViewerTimeline([item])
      const ext = mapped[0] as ExtensionTimelineItem | undefined
      if (!ext || ext.kind !== 'extension') return null
      const config = resolveTimelineRenderer(ext.extensionType)
      if (!config) return null
      const AgentRenderer = config.renderer
      return <AgentRenderer item={ext} context={{}} />
    }

    case 'eval':
      return <EvalNote pass={!!item.evalPass} round={item.evalRound} reason={item.evalReason} />

    default:
      return null
  }
}

function EvalNote({ pass, round, reason }: { pass: boolean; round?: number; reason?: string }) {
  const { t } = useTranslation('chat')
  return (
    <div
      className={cn(
        'flex items-start gap-2 rounded-lg border px-3 py-2 text-xs',
        pass ? 'border-success/30 bg-success/5' : 'border-ai/30 bg-ai/5',
      )}
    >
      {pass ? (
        <CheckCircle2 className="mt-0.5 h-3.5 w-3.5 shrink-0 text-success" />
      ) : (
        <Sparkles className="mt-0.5 h-3.5 w-3.5 shrink-0 text-ai" />
      )}
      <div className="min-w-0">
        <span className={cn('font-medium', pass ? 'text-success' : 'text-ai')}>
          {t('evalRound', { round: (round ?? 0) + 1 })} · {pass ? t('evalPass') : t('evalFail')}
        </span>
        {reason ? <p className="mt-0.5 break-words text-muted-foreground">{reason}</p> : null}
      </div>
    </div>
  )
}

function AssistantResponseEntry({ response }: { response: AssistantResponseState }) {
  const { t } = useTranslation('chat')
  const message = response.response
  const hasThinking = !!response.thinking?.trim()
  const hasResponse = !!message?.content?.trim()

  return (
    <AssistantResponse
      actorName={response.agentName || message?.agent_name}
      timestamp={message?.created_at}
      streaming={response.streaming}
      thinking={hasThinking ? <MarkdownContent content={trimDisplayContent(response.thinking || '')} compact muted /> : undefined}
      tools={response.tools.length > 0 ? (
        <div className="space-y-2">
          {response.tools.map((tool) => (
            <ChatToolCall
              key={tool.id}
              toolName={tool.toolName}
              toolArgs={tool.toolArgs}
              result={tool.result}
              pending={tool.pending}
            />
          ))}
        </div>
      ) : undefined}
      response={hasResponse ? <MarkdownContent content={trimDisplayContent(message?.content || '')} compact /> : undefined}
      labels={{ tools: response.tools.length === 1 ? t('tool') : t('tools'), thinking: t('thinkingLabel'), response: t('responseLabel') }}
    />
  )
}

function TimelineMark({ item }: { item: TimelineItem }) {
  const { t } = useTranslation('chat')
  const descriptor = describeTimelineItem(item, t)
  if (!descriptor) return <div className="hidden xl:block" />

  return (
    <div className="hidden pr-2 pt-1 xl:block">
      <div className="relative min-h-8 border-r border-border/70 pr-3 text-right">
        <span
          className={cn(
            'absolute -right-[5px] top-1 flex h-2.5 w-2.5 items-center justify-center rounded-full border bg-background',
            descriptor.dotClass,
          )}
        />
        <div className="flex min-w-0 items-center justify-end gap-1.5 text-[11px] font-medium text-foreground">
          <span className="truncate">{descriptor.label}</span>
          {descriptor.icon}
        </div>
        <div className="mt-0.5 font-mono text-[10px] leading-4 text-muted-foreground">{descriptor.time}</div>
      </div>
    </div>
  )
}

function IOAThreadNote({ item }: { item: TimelineItem }) {
  const { t } = useTranslation('chat')
  const note = describeIOAThreadItem(item, t)
  if (!note) return <div className="hidden 2xl:block" />

  return (
    <div className="hidden pt-1 2xl:block">
      <div className="rounded-md border border-primary/25 bg-primary/5 px-2.5 py-2">
        <div className="flex min-w-0 items-center gap-1.5 text-[11px] font-medium text-primary">
          <GitBranch className="h-3 w-3 shrink-0" />
          <span className="truncate">{note.label}</span>
        </div>
        {note.detail && (
          <p className="mt-1 line-clamp-3 text-[11px] leading-4 text-muted-foreground">{note.detail}</p>
        )}
      </div>
    </div>
  )
}

function EmptyState({ eyebrow, title, subtitle }: { eyebrow: string; title: string; subtitle: ReactNode }) {
  return <InstrumentIdle eyebrow={eyebrow} title={title} subtitle={subtitle} className="py-16" />
}

interface TimelineDescriptor {
  label: string
  time: string
  icon: ReactNode
  dotClass: string
}

function describeTimelineItem(item: TimelineItem, t: (key: string) => string): TimelineDescriptor | null {
  const time = formatRailTime(item)

  switch (item.kind) {
    case 'message': {
      if (!item.message) return null
      const role = item.message.role
      if (role === 'user') {
        return {
          label: t('you'),
          time,
          icon: <User className="h-3 w-3 text-primary" />,
          dotClass: 'border-primary bg-primary',
        }
      }
      if (role === 'assistant') {
        return {
          label: item.message.agent_name || t('assistant'),
          time,
          icon: <BrandMark size={12} className="text-ai" />,
          dotClass: 'border-ai bg-ai',
        }
      }
      return {
        label: t('system'),
        time,
        icon: <MessageSquare className="h-3 w-3 text-muted-foreground" />,
        dotClass: 'border-border bg-muted-foreground/60',
      }
    }

    case 'assistant_response':
      return {
        label: item.assistantResponse?.agentName || item.agentName || t('assistant'),
        time,
        icon: <BrandMark size={12} className="text-ai" />,
        dotClass: item.assistantResponse?.streaming
          ? 'border-primary bg-background animate-pulse'
          : 'border-ai bg-ai',
      }

    case 'tool_call':
      return {
        label: item.toolCall?.toolName || t('tool'),
        time,
        icon: <Wrench className="h-3 w-3 text-warning" />,
        dotClass: item.toolCall?.pending ? 'border-warning bg-warning animate-pulse' : 'border-primary bg-primary',
      }

    case 'scan_started':
    case 'scan_progress':
    case 'scan_complete':
    case 'agent_joined': {
      const mapped = toViewerTimeline([item])
      const ext = mapped[0] as ExtensionTimelineItem | undefined
      if (!ext || ext.kind !== 'extension') return null
      const config = resolveTimelineRenderer(ext.extensionType)
      if (config?.mark) {
        const markLabel = typeof config.mark.label === 'function'
          ? config.mark.label(ext) : (config.mark.label || item.kind)
        const MarkIcon = config.mark.icon
        return {
          label: markLabel,
          time,
          icon: MarkIcon ? <MarkIcon className="h-3 w-3" /> : null,
          dotClass: config.mark.dotClass || 'border-border bg-muted-foreground/60',
        }
      }
      return null
    }

    case 'thinking':
      return {
        label: item.agentName || t('thinking'),
        time,
        icon: <CircleDashed className="h-3 w-3 animate-spin text-primary" />,
        dotClass: 'border-primary bg-background',
      }

    case 'eval':
      return {
        label: t('evalLabel'),
        time,
        icon: <Sparkles className="h-3 w-3 text-ai" />,
        dotClass: item.evalPass ? 'border-success bg-success' : 'border-ai bg-ai',
      }

    default:
      return null
  }
}

function describeIOAThreadItem(item: TimelineItem, t: (key: string) => string): { label: string; detail?: string } | null {
  if (item.kind === 'assistant_response' && item.assistantResponse) {
    const ioaTool = item.assistantResponse.tools.find((tool) => isIOATool(tool.toolName, tool.toolArgs))
    if (ioaTool) {
      return {
        label: ioaTool.toolName || 'ioa',
        detail: previewText(summarizeArgs(ioaTool.toolArgs) || ioaTool.result || '', 140),
      }
    }
  }

  if (item.kind === 'tool_call' && item.toolCall && isIOATool(item.toolCall.toolName, item.toolCall.toolArgs)) {
    return {
      label: item.toolCall.toolName || 'ioa',
      detail: previewText(summarizeArgs(item.toolCall.toolArgs) || item.toolCall.result || '', 140),
    }
  }

  if (item.kind === 'message' && item.message) {
    const metadata = item.message.metadata || {}
    const thread = metadata.ioa_thread || metadata.ioa_message || metadata.thread
    if (thread) {
      return {
        label: t('ioaMessage'),
        detail: previewText(typeof thread === 'string' ? thread : JSON.stringify(thread), 140),
      }
    }
  }

  return null
}

function isIOATool(toolName: string, toolArgs: string): boolean {
  const name = toolName.toLowerCase()
  if (name === 'ioa' || name.startsWith('ioa_') || name.startsWith('ioa.')) return true
  return /\bioa_(space|send|read)\b/i.test(toolArgs)
}

function formatRailTime(item: TimelineItem): string {
  const raw = item.message?.created_at ? new Date(item.message.created_at).getTime() : item.timestamp
  const date = new Date(raw)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleTimeString(i18n.language, { hour: '2-digit', minute: '2-digit' })
}

function previewText(value: string, max: number): string {
  const compact = value.replace(/\s+/g, ' ').trim()
  if (compact.length <= max) return compact
  return `${compact.slice(0, Math.max(0, max - 1))}...`
}

function trimDisplayContent(value: string): string {
  return value.replace(/[ \t\r\n]+$/g, '')
}
