import { ClaimSchema, ClaimType } from '../../gen/decision/claim_pb'
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { ChevronDown, CircuitBoard, Loader2, Repeat2, ArrowRight, Layers, Bot, Wrench, Eye, Check, XCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { create } from '@bufbuild/protobuf'
import { registerTimelineRenderer, ToolResultDisplay } from '@/viewer'
import { CodeBlock } from '@/markdown'
import type { ExtensionTimelineItem } from '@/viewer'
import { DecisionRequestSchema } from '../../gen/types/jev_pb'
import { claimDefinitions, decisionOptions, parseJEVJSON , evaluationChoice, evaluationNumber, evaluationType } from '../../lib/jev-decisions'
import { useToolPresentation } from './ToolCallResult'
import { useObservationLabels } from '../../lib/observation-labels'
import type { JEVSegment, JEVCompilation, JEVRecord, JEVCheck } from '../../lib/jev-view'
import { DecisionBatch, TokenUsageLine } from './JEVDecision'
import { JEVDefinition } from './JEVDefinition'
import type { WorkflowNode } from '../../lib/workflow-view'
import './JEVTimeline.css'

function useLiveDisclosure(running: boolean) {
  const ref = useRef<HTMLDetailsElement>(null)
  useEffect(() => { if (running && ref.current) ref.current.open = true }, [running])
  return ref
}

function FlowSection({ title, icon, children }: { title: string; icon: ReactNode; children?: ReactNode }) {
  return <section className="jev-flow-section">
    <div className="flex flex-wrap items-center gap-2 text-xs font-medium">{icon}<span>{title}</span></div>
    {children && <div className="mt-2 min-w-0">{children}</div>}
  </section>
}

function Detail({ title, children }: { title: string; children: ReactNode }) {
  return <details className="jev-detail"><summary className="cursor-pointer text-[11px] text-muted-foreground">{title}</summary><div className="mt-2 min-w-0">{children}</div></details>
}

function CompilerDiagnosticView({ output }: { output: string }) {
  const { t } = useTranslation('jev')
  const parsed = parseJEVJSON(output)
  if (!parsed || typeof parsed !== 'object' || !('diagnostic' in parsed)) return null
  const diagnostic = parsed.diagnostic
  if (!diagnostic || typeof diagnostic !== 'object') return null
  const value = diagnostic as Record<string, unknown>
  const code = String(value.code || '')
  return <div data-testid="jev-compiler-diagnostic" className="mt-2 space-y-2 text-[11px]">
    <p className="font-medium">{t('compilerDiagnostic')} · {t(`diagnosticCodes.${code}`, { defaultValue: code })}</p>
    {typeof value.message === 'string' && <p className="whitespace-pre-wrap break-words text-muted-foreground">{value.message}</p>}
    {typeof value.recorded === 'number' && <p>{t('replayProgress', { replayed: Number(value.replayed || 0), recorded: value.recorded })}</p>}
    {typeof value.action === 'string' && <p className="whitespace-pre-wrap break-words"><span className="font-medium">{t('diagnosticAction')}：</span>{value.action}</p>}
    {value.expected !== undefined && <Detail title={t('expectedBinding')}><CodeBlock code={JSON.stringify(value.expected, null, 2)} language="json" maxHeight={200} /></Detail>}
    {value.actual !== undefined && <Detail title={t('actualBinding')}><CodeBlock code={JSON.stringify(value.actual, null, 2)} language="json" maxHeight={200} /></Detail>}
  </div>
}

function Facts({ json }: { json: string }) {
  const value = parseJEVJSON(json)
  function field(item: unknown, depth = 0): ReactNode {
    if (Array.isArray(item)) return <ol className="space-y-1">{item.map((value, i) => <li key={i}>{field(value, depth + 1)}</li>)}</ol>
    if (item && typeof item === 'object' && depth < 4) return <dl className="jev-facts">{Object.entries(item).map(([key, value]) => <div key={key}>
      <dt>{key}</dt><dd>{field(value, depth + 1)}</dd></div>)}</dl>
    return <span className="whitespace-pre-wrap break-words">{item && typeof item === 'object' ? JSON.stringify(item) : String(item ?? '—')}</span>
  }
  return <div className="jev-facts-scroll">{field(value)}</div>
}

function CompilationFeedback({ reason }: { reason: string }) {
  const { t } = useTranslation('jev')
  const marker = 'Actual evaluated native bindings: '
  const [message, detail] = reason.split(marker)
  const end = detail?.lastIndexOf('}. Check every call')
  const bindings = end === undefined || end < 0 ? undefined : detail.slice(0, end + 1)
  return <div data-testid="jev-compilation-error" className="max-h-72 overflow-y-auto space-y-3">
    <p className="whitespace-pre-wrap break-words text-xs text-destructive">{message}</p>
    {bindings && <div><p className="mb-2 text-[11px] text-muted-foreground">{t('nativeBinding')}</p><Facts json={bindings} /></div>}
    {detail && <p className="whitespace-pre-wrap break-words text-[11px] text-muted-foreground">{end === undefined || end < 0 ? detail : detail.slice(end + 2)}</p>}
  </div>
}

function GeneratedClaims({ output }: { output: string }) {
  return <>{claimDefinitions(parseJEVJSON(output)).map((value, index) => <JEVDefinition key={index} compact value={value} />)}</>
}

// The workflow owns the selection. Only this selected node mounts its evidence.
export function JEVWorkflowDetail({ node, nodes }: { node: WorkflowNode; nodes: WorkflowNode[] }) {
  const { t } = useTranslation('jev')
  const presentation = useToolPresentation(node.sessionId)
  const observationLabels = useObservationLabels()
  const records = node.related || [], payload = node.record?.value.payload
  if (!payload) return null
  switch (payload.case) {
    case 'decisionRequest': {
      const result = records.find(record => record.value.payload.case === 'decisionResult')?.value.payload
      return <DecisionBatch request={payload.value} result={result?.case === 'decisionResult' ? result.value : undefined} />
    }
    case 'decisionResult': return <DecisionBatch request={create(DecisionRequestSchema, { requestId: payload.value.requestId, purpose: payload.value.purpose,
      claims: Object.fromEntries(Object.entries(payload.value.evaluations).map(([id, answer]) => [id, create(ClaimSchema, { type: evaluationType(answer) })])) })} result={payload.value} />
    case 'dispatch': case 'result': {
      const resultRecord = records.find(record => record.value.payload.case === 'result')?.value.payload
      const result = resultRecord?.case === 'result' ? resultRecord.value.result : node.step?.result
      const call = payload.case === 'dispatch' ? payload.value.call : node.step?.call
      return <div className="space-y-3">
        <ToolResultDisplay {...presentation} observationLabels={observationLabels} toolName={call?.name || result?.name || ''}
          toolArgs={call?.arguments?.data ? new TextDecoder().decode(call.arguments.data) : ''} toolResult={result}
          result={result?.output.flatMap(part => part.value.case === 'text' ? [part.value.value.text] : []).join('\n')}
          pending={node.state === 'pending'} error={result?.isError} observations={node.step?.observations || []} defaultExpanded />
        {node.state === 'interrupted' && <p className="text-xs text-muted-foreground">{t('missingFeedback')}</p>}
        {resultRecord?.case === 'result' && <p className="text-[11px] tabular-nums text-muted-foreground">{Number(resultRecord.value.elapsedMs)} ms</p>}
      </div>
    }
    case 'observation': return <div className="space-y-3"><Facts json={payload.value.stateJson} />{payload.value.candidatesJson && <Facts json={payload.value.candidatesJson} />}</div>
    case 'takeover': return payload.value.definition && <JEVDefinition value={payload.value.definition} />
    case 'handoff': return <div className="space-y-2 text-xs"><p>{payload.value.detail || payload.value.reason}</p>{payload.value.code && <code>{payload.value.code}</code>}{payload.value.effectsJson && <Facts json={payload.value.effectsJson} />}{payload.value.resultJson && <Facts json={payload.value.resultJson} />}</div>
    case 'boundary': return <p className="text-xs leading-relaxed text-muted-foreground">{t(`reasons.${payload.value.reason}`, { defaultValue: payload.value.reason })}</p>
    case 'generation': {
      const last = records[records.length - 1]?.value.payload
      const generation = last?.case === 'generation' ? last.value : payload.value
      const output = parseJEVJSON(generation.output)
      const drafts = generation.kind === 'claim_llm' ? claimDefinitions(output)
        : generation.kind === 'reflex_llm' && generation.output ? [output && typeof output === 'object' && 'observe' in output ? output : { observe: generation.output }] : []
      const targets = drafts.map(draft => nodes.find(other => {
        const change = other.record?.value.payload
        if (other.sessionId !== node.sessionId || other.turnId !== node.turnId || other.timestamp < node.timestamp || change?.case !== 'libraryChange') return false
        return 'context' in draft ? change.value.state === 'claim_published' && !!change.value.claim
          && draft.type === change.value.claim.type && draft.context === change.value.claim.context && JSON.stringify(draft.options) === JSON.stringify(change.value.claim.options)
          : ['reflex_published', 'reflex_candidate'].includes(change.value.state) && change.value.reflex?.observe === draft.observe
      }))
      const published = generation.state === 'finished' && !generation.error && drafts.length > 0 && targets.every(Boolean)
      const publications = [...new Map(targets.filter((target): target is WorkflowNode => !!target).map(target => [target.id, target])).values()]
      return <div className="space-y-3">
        <p className="text-xs text-muted-foreground">{t(generation.state === 'started' ? 'generationStarted' : generation.error ? 'generationFailed' : 'generationFinished')}</p>
        {(generation.attempt > 0 || generation.errorStage) && <p className="text-[11px] text-muted-foreground" data-generation-request-id={generation.requestId}>{t('generationAttempt', { count: generation.attempt })}{generation.errorStage && ` · ${t('errorStage', { stage: generation.errorStage })}`}</p>}
        {generation.state === 'finished' && generation.kind !== 'reflex_validation' && <TokenUsageLine source={generation.kind === 'parameters_llm' ? t('foregroundLLM') : generation.kind === 'claim_llm' ? 'Claim LLM' : 'Reflex LLM'} usage={generation.usage} />}
        {generation.error && <CompilationFeedback reason={generation.error} />}
        {generation.kind === 'reflex_validation' && generation.output && <CompilerDiagnosticView output={generation.output} />}
        {published ? <div className="space-y-2"><p className="text-xs text-muted-foreground">{t('workflow.artifactAtPublication')}</p>
          {publications.map(publication => <button type="button" key={publication.id} className="flex items-center gap-1.5 rounded px-2 py-1 text-xs text-primary hover:bg-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-ring"
            onClick={() => window.dispatchEvent(new CustomEvent('cyber-workflow-select', { detail: publication.id }))}><ArrowRight className="h-3 w-3" />{t('workflow.openPublication')}</button>)}</div> : generation.output && (generation.kind === 'claim_llm' && !generation.error
          ? <GeneratedClaims output={generation.output} /> : <CodeBlock code={generation.output} language={generation.kind === 'reflex_llm' ? 'javascript' : 'text'} maxHeight={300} />)}
      </div>
    }
    case 'libraryChange': return <div className="space-y-3">
      {payload.value.claim && <JEVDefinition value={payload.value.claim} />}{payload.value.reflex && <JEVDefinition value={payload.value.reflex} />}
      {payload.value.errorStage && <p className="text-[11px] text-muted-foreground">{t('errorStage', { stage: payload.value.errorStage })}</p>}
      {payload.value.reason && <CompilationFeedback reason={payload.value.reason} />}
    </div>
    default: return null
  }
}

// Requests and their answers share one node. Independent heads branch in
// parallel; execution, feedback and compilation retain recorded event order.
function RecordedFlow({ records, segment, context }: { records: JEVRecord[]; segment?: JEVSegment; context?: ReactNode }) {
  const { t } = useTranslation('jev')
  const [selected, select] = useState<string>()
  const history = useRef<HTMLDivElement>(null)
  const presentation = useToolPresentation(segment?.sessionId || records[0]?.event.sessionId)
  const observationLabels = useObservationLabels()
  const answers = new Map(records.flatMap(({ value }) => value.payload.case === 'decisionResult' ? [[value.payload.value.requestId, value.payload.value] as const] : []))
  const requests = new Set(records.flatMap(({ value }) => value.payload.case === 'decisionRequest' ? [value.payload.value.requestId] : []))
  const completedCalls = new Set(records.flatMap(({ value }) => value.payload.case === 'result' ? [value.payload.value.result?.callId] : []))
  const generations = new Map<string, string>()
  for (const { event, value } of records) if (value.payload.case === 'generation') {
    const generation = value.payload.value
    if (generation.state === 'started') generations.set(generation.kind, event.id)
    else generations.delete(generation.kind)
  }
  const liveGenerations = new Set(generations.values())
  function redundant(record: JEVRecord, index: number, frame: JEVRecord[]) {
    const p = record.value.payload
    if (p.case === 'boundary' && p.value.reason === 'checking' && records.some(record => record.value.payload.case === 'decisionRequest')) return true
    if (p.case === 'generation' && p.value.state === 'started' && !liveGenerations.has(record.event.id)) return true
    if (p.case === 'libraryChange') {
      if (p.value.state === 'settled') return true
      const previous = frame[index - 1]?.value.payload
      if (previous?.case === 'libraryChange' && previous.value.state === p.value.state && previous.value.reason === p.value.reason) return true
    }
    return p.case === 'decisionResult' && requests.has(p.value.requestId)
  }
  const visible = records.filter((record, index) => !redundant(record, index, records))
  const meaningful = visible.filter(({ value }) => value.payload.case !== 'boundary' && !(value.payload.case === 'libraryChange' && value.payload.value.state === 'settled'))
  const current = visible.find(record => record.event.id === selected) || meaningful[meaningful.length - 1] || visible[visible.length - 1]
  useEffect(() => {
    const rail = history.current, node = rail?.querySelector<HTMLElement>('[aria-pressed="true"]')
    if (!rail || !node) return
    const bounds = rail.getBoundingClientRect(), item = node.getBoundingClientRect()
    if (item.right > bounds.right) rail.scrollLeft += item.right - bounds.right + 10
    else if (item.left < bounds.left) rail.scrollLeft -= bounds.left - item.left + 10
  }, [current?.event.id, records.length])
  const currentIndex = records.findIndex(record => record.event.id === current?.event.id)
  let previousRequest = -1
  for (let i = 0; i <= currentIndex; i++) if (records[i].value.payload.case === 'decisionRequest') previousRequest = i
  const start = previousRequest < 0 ? 0 : previousRequest
  const followingRequest = records.slice(start + 1).findIndex(record => record.value.payload.case === 'decisionRequest')
  const end = followingRequest < 0 ? records.length : start + 1 + followingRequest
  function label(record: JEVRecord) {
    const p = record.value.payload
    switch (p.case) {
      case 'decisionRequest': return `JEV · ${t('judgment')}`
      case 'decisionResult': return 'JEV'
      case 'generation': return `${p.value.kind === 'parameters_llm' ? t('runtimeArguments') : p.value.kind === 'claim_llm' ? 'Claim' : 'Reflex'} · ${t(p.value.state === 'started' ? 'generationRequested' : p.value.error ? 'generationFailed' : 'generationFinished')}`
      case 'libraryChange': return t(`compilation.${p.value.state}`, { defaultValue: p.value.state })
      case 'dispatch': return `${t(p.value.read ? 'read' : 'execute')} · ${p.value.call?.name}`
      case 'result': return t(p.value.result?.isError ? 'executionFailed' : 'executionResult')
      case 'observation': return t('inputState')
      case 'takeover': return t('takeover')
      case 'boundary': return p.value.reason === 'checking' ? 'JEV' : 'LLM'
      case 'handoff': return t('handoff')
      default: return ''
    }
  }
  return <div className="jev-flow" data-testid="jev-flow">
    <div ref={history} className="jev-history" role="group" aria-label={t('recordedCycle')}>
      {visible.map((record, index) => <div key={record.event.id} className="jev-history-step" data-event-seq={String(record.event.seq)} data-event-kind={record.value.payload.case}>
        {!!index && <ArrowRight className="jev-history-arrow" />}
        <button id={`jev-node-${record.event.id}`} type="button" aria-pressed={record.event.id === current?.event.id} onClick={() => select(record.event.id)}
          className="jev-history-node" data-record-id={record.event.id}>
          <span className="jev-history-number">{index + 1}</span><span>{label(record)}</span>
          {record.value.payload.case === 'decisionRequest' && <span className="jev-history-result">{Object.entries(answers.get(record.value.payload.value.requestId)?.evaluations || {}).map(([id, answer]) => evaluationChoice(answer)
            ? t(`optionTitles.${evaluationChoice(answer)}`, { defaultValue: /^c[0-9a-f]+$/.test(evaluationChoice(answer) || '') ? t('knownScene') : decisionOptions(record.value.payload.case === 'decisionRequest' ? record.value.payload.value.claims[id] : create(ClaimSchema), answer).find(option => option.selected)?.description || evaluationChoice(answer) })
            : evaluationNumber(answer) ?? '').join(' · ') || t('awaitingAnswer')}</span>}
        </button>
      </div>)}
    </div>
    <div className="jev-flow-body" data-testid="jev-flow-body" role="region" aria-labelledby={current ? `jev-node-${current.event.id}` : undefined}>
    {context}
    {records.slice(start, end).map(({ event, value }, frameIndex, frame) => {
    if (redundant({ event, value }, frameIndex, frame)) return null
    if (value.payload.case === 'boundary' && value.payload.value.reason === 'checking') return null
    if (value.payload.case === 'libraryChange' && value.payload.value.state === 'failed') {
      const reason = value.payload.value.reason
      if (frame.slice(0, frameIndex).some(record => record.value.payload.case === 'generation' && record.value.payload.value.error === reason)) return null
    }
    const payload = value.payload
    let content: ReactNode
    switch (payload.case) {
      case 'decisionRequest': content = <DecisionBatch bodyOnly request={payload.value} result={answers.get(payload.value.requestId)} />; break
      case 'decisionResult':
        if (requests.has(payload.value.requestId)) return null
        content = <DecisionBatch bodyOnly request={create(DecisionRequestSchema, { requestId: payload.value.requestId, purpose: payload.value.purpose,
          claims: Object.fromEntries(Object.entries(payload.value.evaluations).map(([id, answer]) => [id, create(ClaimSchema, { type: evaluationType(answer) })])) })} result={payload.value} />
        break
      case 'observation': {
        content = <FlowSection title={t('inputState')} icon={<Eye className="h-3.5 w-3.5" />}>
          <Detail title={t('stateAndCandidates')}><Facts json={payload.value.stateJson} />
            {payload.value.candidatesJson && <><p className="mt-3 text-[11px] text-muted-foreground">{t('nativeBinding')}</p><Facts json={payload.value.candidatesJson} /></>}
          </Detail></FlowSection>; break
      }
      case 'boundary': content = <p className="text-[11px] text-muted-foreground">{t(`reasons.${payload.value.reason}`, { defaultValue: payload.value.reason })}</p>; break
      case 'takeover': content = <FlowSection title={t('applicability')} icon={<Repeat2 className="h-3.5 w-3.5 text-emerald-500" />}>
        {payload.value.definition && <JEVDefinition value={payload.value.definition} compact />}
      </FlowSection>; break
      case 'dispatch': content = <FlowSection title={t('nativeBinding')} icon={!completedCalls.has(payload.value.call?.id) && segment?.status === 'running' ? <Loader2 className="h-3.5 w-3.5 animate-spin text-amber-500" /> : <Wrench className="h-3.5 w-3.5 text-amber-500" />}>
        {!completedCalls.has(payload.value.call?.id) && <p className="text-[11px] text-muted-foreground">{t(segment?.status === 'running' ? 'waitingFeedback' : 'missingFeedback')}</p>}
        {payload.value.candidateId && <p className="break-all font-mono text-[10px] text-muted-foreground">{payload.value.candidateId}</p>}
        <Detail title={t('nativeBinding')}><Facts json={new TextDecoder().decode(payload.value.call?.arguments?.data)} /></Detail>
      </FlowSection>; break
      case 'result': {
        const step = segment?.steps.find(step => step.call?.id === payload.value.result?.callId)
        content = <FlowSection title={`${Number(payload.value.elapsedMs)} ms`} icon={payload.value.result?.isError ? <XCircle className="h-3.5 w-3.5 text-destructive" /> : <Check className="h-3.5 w-3.5 text-emerald-500" />}>
          <ToolResultDisplay {...presentation} observationLabels={observationLabels} toolName={payload.value.result?.name || step?.call?.name || ''}
            toolArgs={new TextDecoder().decode(step?.call?.arguments?.data)} toolResult={payload.value.result}
            result={payload.value.result?.output.flatMap(part => part.value.case === 'text' ? [part.value.value.text] : []).join('\n')}
            pending={false} observations={step?.observations || []} />
        </FlowSection>; break
      }
      case 'handoff': content = <div className="space-y-2 text-[11px] text-muted-foreground"><p>{payload.value.detail || t(`reasons.${payload.value.reason}`, { defaultValue: payload.value.reason })}</p>{payload.value.code && <code>{payload.value.code}</code>}{payload.value.effectsJson && <Facts json={payload.value.effectsJson} />}{payload.value.resultJson && <Facts json={payload.value.resultJson} />}</div>; break
      case 'generation': content = <FlowSection title={payload.value.kind === 'parameters_llm' ? t('runtimeArguments') : payload.value.kind === 'compiler_round' ? t('compilerRound') : payload.value.kind === 'reflex_validation' ? t('mechanismValidation') : payload.value.kind === 'claim_llm' ? 'Claim' : 'Reflex'} icon={liveGenerations.has(event.id) ? <Loader2 className="h-3.5 w-3.5 animate-spin text-blue-500" /> : <Bot className="h-3.5 w-3.5 text-blue-500" />}>
        {(payload.value.attempt > 0 || payload.value.errorStage) && <p className="text-[10px] text-muted-foreground" data-generation-request-id={payload.value.requestId}>
          {payload.value.attempt > 0 && t('generationAttempt', { count: payload.value.attempt })}{payload.value.errorStage && ` · ${t('errorStage', { stage: payload.value.errorStage })}`}
        </p>}
        {payload.value.kind === 'claim_llm' && payload.value.state === 'finished' && !payload.value.error && <div className="mt-2"><GeneratedClaims output={payload.value.output} /></div>}
        {payload.value.state === 'finished' && <div className="mt-2"><p className="text-[11px] text-muted-foreground">{Number(payload.value.elapsedMs)} ms</p>
          {payload.value.kind !== 'reflex_validation' && <TokenUsageLine source={payload.value.kind === 'parameters_llm' ? t('foregroundLLM') : payload.value.kind === 'claim_llm' ? 'Claim LLM' : 'Reflex LLM'} usage={payload.value.usage} />}
          {payload.value.error && <CompilationFeedback reason={payload.value.error} />}
          {payload.value.kind === 'reflex_validation' && payload.value.output && <CompilerDiagnosticView output={payload.value.output} />}
          {payload.value.output && (payload.value.kind !== 'claim_llm' || payload.value.error) && <Detail title={t('generatedArtifact')}><CodeBlock code={payload.value.output} language={payload.value.kind === 'reflex_llm' ? 'javascript' : 'text'} maxHeight={280} /></Detail>}
        </div>}
      </FlowSection>; break
      case 'libraryChange': content = <>
        {payload.value.claim && <JEVDefinition value={payload.value.claim} compact />}
        {payload.value.reflex && <JEVDefinition value={payload.value.reflex} compact />}
        {!!payload.value.reason && <div>
          {payload.value.errorStage && <p className="text-[10px] text-muted-foreground">{t('errorStage', { stage: payload.value.errorStage })}</p>}
          <CompilationFeedback reason={payload.value.reason} />
        </div>}
      </>; break
      default: return null
    }
    const index = records.findIndex(record => record.event.id === event.id)
    const state = [...records.slice(0, index)].reverse().find(record => record.value.payload.case === 'observation')?.value.payload
    return <div className="jev-body-section" key={event.id} data-node-body={event.id}>
      {payload.case === 'decisionRequest' && state?.case === 'observation' && !frame.some(record => record.value.payload.case === 'observation') && <FlowSection title={t('inputState')} icon={<Eye className="h-3.5 w-3.5" />}><Facts json={state.value.stateJson} /></FlowSection>}
      {payload.case === 'decisionRequest' && segment?.definition && !records.slice(start, end).some(record => record.value.payload.case === 'takeover') && <JEVDefinition value={segment.definition} compact />}
      {content}
    </div>
  })}</div>
    {!records[0]?.value.background && <div className="jev-flow-return"><Repeat2 /><span>{t('feedbackCycle')}</span></div>}
  </div>
}

export function JEVSegmentView({ segment }: { segment: JEVSegment }) {
  const { t } = useTranslation('jev')
  const calls = segment.steps.filter(s => s.call), last = calls[calls.length - 1]
  const decisions = segment.records.filter(r => r.value.payload.case === 'decisionResult').length
  const running = segment.status === 'running'
  const ref = useLiveDisclosure(running)
  return <details ref={ref} className="group min-w-0 border-l-2 border-emerald-500/60 pl-3" data-testid="jev-segment" id={`jev-${segment.id}`}>
    <summary className="flex cursor-pointer list-none flex-wrap items-center gap-x-3 gap-y-1 py-2 text-sm">
      {running ? <Loader2 className="h-4 w-4 shrink-0 animate-spin text-emerald-500" /> : <Repeat2 className="h-4 w-4 shrink-0 text-emerald-500" />}
      <span className="font-medium">{t(running ? 'running' : segment.status === 'ended' ? 'loopEnded' : 'handoff')}</span>
      <span className="text-xs tabular-nums text-muted-foreground">{t('counts', { calls: calls.length, decisions })}</span>
      <ChevronDown className="ml-auto h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform group-open:rotate-180" />
      <div className="basis-full pl-7 text-xs text-muted-foreground">{running ? `${t('step', { count: last?.index || 1 })} · ${last?.call?.name || t('judgment')}` : t(`reasons.${segment.reason}`, { defaultValue: segment.reason })}</div>
    </summary>
    <div className="min-w-0 pb-3">
      {segment.previousId && <a href={`#jev-${segment.previousId}`} className="mb-2 inline-flex items-center gap-1 text-xs text-muted-foreground underline underline-offset-2"><ArrowRight className="h-3 w-3" />{t('previousSegment')}</a>}
      <RecordedFlow records={segment.records} segment={segment} />
    </div>
  </details>
}

export function JEVCompilationView({ compilation }: { compilation: JEVCompilation }) {
  const { t } = useTranslation('jev')
  const decisions = compilation.records.filter(r => r.value.payload.case === 'decisionResult').length
  const ref = useLiveDisclosure(['reviewing', 'compiling', 'generating'].includes(compilation.state))
  return <details ref={ref} className="group min-w-0 border-l-2 border-border pl-3" data-testid="jev-compilation">
    <summary className="flex cursor-pointer list-none flex-wrap items-center gap-2 py-2 text-xs text-muted-foreground">
      <Layers className="h-3.5 w-3.5 shrink-0" /><span>{t('background')}</span><span>· {t(`compilation.${compilation.state}`, { defaultValue: compilation.state })}</span>
      {!!decisions && <span className="tabular-nums">· {decisions} {t('judgments')}</span>}
      <ChevronDown className="ml-auto h-3.5 w-3.5 transition-transform group-open:rotate-180" />
    </summary>
    <div className="pb-3">
      <RecordedFlow records={compilation.records} context={!!compilation.foregroundCalls?.length && <div className="jev-task-path" data-testid="jev-task-path">
        <p className="text-[11px] font-medium">{t('foregroundExecution')} · LLM</p>
        <div className="jev-task-operations">{compilation.foregroundCalls.map(({ call, result }, index) => <span key={call.id}>
          {!!index && <ArrowRight />}<span title={new TextDecoder().decode(call.arguments?.data)}><Wrench />{index + 1} · {call.name}
            {result ? result.isError ? <XCircle className="text-destructive" /> : <Check className="text-emerald-500" /> : <Loader2 className="animate-spin" />}</span>
        </span>)}</div>
        <p className="mt-2 text-[10px] text-muted-foreground">{t('backgroundObserves')}</p>
      </div>} />
    </div>
  </details>
}

function JEVCheckView({ check }: { check: JEVCheck }) {
  const { t } = useTranslation('jev')
  const count = check.records.filter(record => record.value.payload.case === 'decisionResult').length
  const latest = [...check.records].reverse().find(r => r.value.payload.case === 'decisionResult')?.value.payload
  const entry = latest?.case === 'decisionResult' ? latest.value.evaluations.entry : undefined
  const ref = useLiveDisclosure(check.status === 'running')
  return <details ref={ref} className="group min-w-0 border-l-2 border-border pl-3" data-testid="jev-check" id={`jev-${check.id}`}>
    <summary className="flex cursor-pointer list-none flex-wrap items-center gap-2 py-2 text-xs text-muted-foreground">
      {check.status === 'running' ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <CircuitBoard className="h-3.5 w-3.5 text-emerald-500" />}
      <span>{t('check')} {check.iteration ? `· ${check.iteration}` : ''}</span><ArrowRight className="h-3 w-3" /><span>{t(check.status === 'running' ? 'checking' : 'modelContinues')}</span>
      {evaluationChoice(entry) && <span className="tabular-nums">· {t('entryChoice')} {t(`optionTitles.${evaluationChoice(entry)}`, { defaultValue: t('knownScene') })}</span>}
      {!!count && <span>· {count} {t('judgments')}</span>}
      <ChevronDown className="ml-auto h-3.5 w-3.5 transition-transform group-open:rotate-180" />
    </summary>
    <div className="pb-3">
      <div className="mb-2 flex flex-wrap gap-3 text-[10px] text-muted-foreground">
        {check.previousId && <a href={`#jev-${check.previousId}`} className="underline underline-offset-2">{t('previousJudgment')}</a>}
        {check.nextId && <a href={`#jev-${check.nextId}`} className="underline underline-offset-2">{t('nextJudgment')}</a>}
      </div>
      <RecordedFlow records={check.records} />
    </div>
  </details>
}

registerTimelineRenderer('jev_segment', {
  renderer: ({ item }: { item: ExtensionTimelineItem }) => <JEVSegmentView segment={item.data.segment as JEVSegment} />,
  mark: { label: 'JEV', icon: Repeat2, dotClass: 'border-emerald-500 bg-emerald-500' },
})
registerTimelineRenderer('jev_compilation', {
  renderer: ({ item }: { item: ExtensionTimelineItem }) => <JEVCompilationView compilation={item.data.compilation as JEVCompilation} />,
  mark: { label: 'Reflex', icon: Layers, dotClass: 'border-border bg-muted-foreground/60' },
})
registerTimelineRenderer('jev_check', {
  renderer: ({ item }: { item: ExtensionTimelineItem }) => <JEVCheckView check={item.data.check as JEVCheck} />,
  mark: { label: 'JEV', icon: CircuitBoard, dotClass: 'border-border bg-muted-foreground/60' },
})
