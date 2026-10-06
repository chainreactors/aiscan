import { ClaimSchema, ClaimType } from '../../gen/decision/claim_pb'
import { useId, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { ArrowDownLeft, Bot, Check, CircuitBoard, GitBranch, Layers, Loader2, Pause, Play, RotateCcw, Terminal, XCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { decisionOptions, decisionQuestions, decisionText , evaluationChoice, evaluationNumber, evaluationType } from '../../lib/jev-decisions'
import { workflowNodeSummary, type WorkflowNode } from '../../lib/workflow-view'
import { controlFeedback, type ControlFrame, type ControlStage } from '../../lib/jev-control-flow'
import './JEVControlFlow.css'

type Wire = { id: string; source: string; target: string; path: string; feedback?: boolean }

export function JEVControlFlow({ nodes, current, frame, previous, moving, replaying, playing, onSelect }: {
  nodes: WorkflowNode[]; current?: WorkflowNode; frame?: ControlFrame; previous?: ControlFrame; moving: boolean
  replaying: boolean; playing: boolean; onSelect: (id: string) => void
}) {
  const { t } = useTranslation('jev')
  const id = useId(), diagram = useRef<HTMLDivElement>(null), [wires, setWires] = useState<Wire[]>([])
  const stage = frame?.stage
  const foreground = nodes.filter(node => !node.background)
  const latest = (predicate: (node: WorkflowNode) => boolean) => [...nodes].reverse().find(predicate)
  const model = latest(node => !node.background && (node.kind === 'reasoning' || node.kind === 'generation'))
  const response = latest(node => !node.background && (node.kind === 'response' || node.kind === 'handoff'
    || node.record?.value.payload.case === 'boundary' && node.record.value.payload.value.reason !== 'checking'))
  const decision = current?.kind === 'decision' ? current : latest(node => node.kind === 'decision' && !!node.background === (stage === 'background'))
  const request = decision?.record?.value.payload
  const result = decision?.related?.find(record => record.value.payload.case === 'decisionResult')?.value.payload
  const questions = request?.case === 'decisionRequest' ? decisionQuestions(request.value.claims) : []
  const answers = result?.case === 'decisionResult' ? result.value : request?.case === 'decisionResult' ? request.value : undefined
  const background = latest(node => !!node.background)
  const toolNodes = foreground.filter(node => node.kind === 'tool' || node.kind === 'agent')
  const executors = useMemo(() => {
    const groups = new Map<string, WorkflowNode[]>()
    for (const node of toolNodes) {
      const key = JSON.stringify([node.sessionId, node.kind === 'agent' ? node.actor : node.label])
      groups.set(key, [...(groups.get(key) || []), node])
    }
    const values = [...groups.values()]
    // Keep the current executor visible even in a large recorded tool catalog.
    if (values.length > 4) values.sort((a, b) => Number(b.some(node => node.id === current?.id)) - Number(a.some(node => node.id === current?.id)))
    return values.slice(0, 4)
  }, [nodes, current?.id])
  const routes = useMemo(() => [
    { id: 'model-judgment', source: 'model', target: 'judgment' },
    { id: 'feedback-judgment', source: 'feedback', target: 'judgment', feedback: true },
    { id: 'judgment-return', source: 'judgment', target: 'return', feedback: true },
    { id: 'feedback-return', source: 'feedback', target: 'return' },
    ...executors.flatMap((_, index) => [
      { id: `judgment-executor-${index}`, source: 'judgment', target: `executor-${index}` },
      { id: `model-executor-${index}`, source: 'model', target: `executor-${index}`, feedback: true },
      { id: `executor-${index}-feedback`, source: `executor-${index}`, target: 'feedback' },
    ]),
  ], [executors.length])
  const selectedExecutor = executors.findIndex(group => group.some(node => node.id === current?.id))
  const activeExecutor = selectedExecutor >= 0 ? selectedExecutor : executors.findIndex(group => group.some(node => node.sessionId === current?.sessionId && node.state === 'pending'))
  const priorStage = previous?.stage
  const activeRoutes = new Set<string>()
  if (stage === 'judgment' && priorStage === 'feedback') activeRoutes.add('feedback-judgment')
  if (stage === 'judgment' && priorStage === 'model') activeRoutes.add('model-judgment')
  if (stage === 'execution' && activeExecutor >= 0) activeRoutes.add(`${current?.record ? 'judgment' : 'model'}-executor-${activeExecutor}`)
  if (stage === 'feedback' && activeExecutor >= 0) activeRoutes.add(`executor-${activeExecutor}-feedback`)
  if (stage === 'return' && foreground.some(node => node.kind === 'handoff' || node.record?.value.payload.case === 'boundary' && node.record.value.payload.value.reason !== 'checking'))
    activeRoutes.add(priorStage === 'feedback' ? 'feedback-return' : 'judgment-return')
  if (moving && !replaying) executors.forEach((group, index) => {
    const running = group.find(node => node.state === 'pending')
    if (running) activeRoutes.add(`${running.record ? 'judgment' : 'model'}-executor-${index}`)
  })
  useLayoutEffect(() => {
    const element = diagram.current
    if (!element) return
    const measure = () => {
      const rect = element.getBoundingClientRect()
      const boxes = new Map([...element.querySelectorAll<HTMLElement>('[data-control-anchor]')].map(anchor => {
        const b = anchor.getBoundingClientRect()
        return [anchor.dataset.controlAnchor!, { x: b.left - rect.left, y: b.top - rect.top, w: b.width, h: b.height }]
      }))
      setWires(routes.flatMap(route => {
        const a = boxes.get(route.source), b = boxes.get(route.target)
        if (!a || !b) return []
        const middle = (a.y + a.h + b.y) / 2
        const path = route.id === 'feedback-judgment'
          ? `M ${a.x} ${a.y + a.h / 2} H 12 V ${b.y + b.h / 2} H ${b.x}`
          : route.id === 'judgment-return'
          ? `M ${a.x + a.w} ${a.y + a.h / 2} H ${rect.width - 12} V ${b.y + b.h / 2} H ${b.x + b.w}`
          : route.id.startsWith('model-executor-')
          ? `M ${a.x + a.w} ${a.y + a.h / 2} H ${rect.width - 7} V ${b.y - 10} H ${b.x + b.w / 2} V ${b.y}`
          : `M ${a.x + a.w / 2} ${a.y + a.h} C ${a.x + a.w / 2} ${middle}, ${b.x + b.w / 2} ${middle}, ${b.x + b.w / 2} ${b.y}`
        return [{ ...route, path }]
      }))
    }
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    return () => observer.disconnect()
  }, [routes, nodes])
  const stateIcon = (node?: WorkflowNode) => node?.state === 'pending' ? <Loader2 className="control-spin" /> : node?.state === 'failed' ? <XCircle /> : node ? <Check /> : null
  const card = (anchor: string, title: string, actor: string, node: WorkflowNode | undefined, active: boolean, subtitle: string) =>
    <button type="button" data-control-anchor={anchor} data-control-stage={anchor} data-active={active} data-state={node?.state} disabled={!node}
      className={`control-card control-${anchor}`} onClick={() => node && onSelect(node.id)}>
      <span className="control-card-title">{anchor === 'feedback' ? <ArrowDownLeft /> : <Bot />}<strong>{title}</strong><span>{actor}</span>{active && <i className={moving ? 'is-moving' : ''} />}</span>
      <span className="control-card-subtitle">{subtitle}</span>
      <span className="control-card-status">{stateIcon(node)}{node ? node.kind === 'response' ? t(node.state === 'pending' ? 'control.composingResponse' : 'control.responseBelow')
        : (anchor === 'feedback' ? controlFeedback(node) : workflowNodeSummary(node)) || t(node.literal ? 'workflow.states.' + node.state : node.label) : t('control.notRecorded')}</span>
    </button>
  const observation = current?.kind === 'tool' && stage === 'feedback' ? current : latest(node => !node.background && (node.kind === 'observation' || node.kind === 'tool' && node.state !== 'pending'))
  const selectedStage: ControlStage | undefined = current?.background ? 'background' : stage
  return <div className="jev-control-flow" data-testid="jev-control-flow" data-stage={selectedStage} data-replaying={replaying}>
    <div className="control-heading"><div><GitBranch /><strong>{t('control.title')}</strong></div><span className="control-mode"><i className={moving ? 'is-moving' : ''} />{t(replaying ? playing ? 'control.playing' : 'control.paused' : moving ? 'control.live' : 'control.recorded')}</span></div>
    <div className="control-diagram" ref={diagram}>
      <svg className="control-wires" aria-hidden="true"><defs><marker id={`${id}-arrow`} viewBox="0 0 8 8" refX="7" refY="4" markerWidth="5" markerHeight="5" orient="auto"><path d="M 1 1 L 7 4 L 1 7" fill="none" stroke="context-stroke" strokeWidth="1.4" /></marker></defs>
        {wires.map(wire => <g key={wire.id} data-control-route={wire.id} data-active={activeRoutes.has(wire.id)}>
          <path d={wire.path} className={`control-wire ${wire.feedback ? 'is-feedback' : ''}`} markerEnd={`url(#${id}-arrow)`} />
          {moving && activeRoutes.has(wire.id) && <circle r="3" className="control-particle" style={{ offsetPath: `path('${wire.path}')` }} />}
        </g>)}
      </svg>
      {card('model', t('control.generate'), model?.actor || 'LLM', model, selectedStage === 'model', t('control.modelRole'))}
      <div className="control-card control-judgment" data-control-anchor="judgment" data-control-stage="judgment" data-active={selectedStage === 'judgment' || selectedStage === 'background' && current?.kind === 'decision'} data-state={decision?.state}>
        <button type="button" className="control-judgment-heading" disabled={!decision} onClick={() => decision && onSelect(decision.id)}><CircuitBoard /><strong>JEV</strong><span>{t('control.forkLayer')}</span><b>{nodes.filter(node => node.kind === 'decision').length}<small>{t('judgments')}</small></b></button>
        <div className="control-forks" data-testid="control-forks">
          {questions.slice(0, 4).map(([questionId, question]) => {
            const answer = answers?.evaluations[questionId], options = decisionOptions(question, answer), chosen = options.find(option => option.selected), top = options[0]
            const probability = chosen?.probability ?? (question.type === ClaimType.noul ? evaluationNumber(answer) : undefined)
            return <div className="control-fork" key={questionId} data-control-question={questionId} data-choice={evaluationChoice(answer) || undefined}>
              <span title={question.context}>{t(`questionTitles.${questionId}`, { defaultValue: question.context || questionId })}</span>
              <div className="control-distribution" aria-hidden="true">{options.map(option => <i key={option.id} data-chosen={option.selected} style={{ width: `${(option.probability ?? 0) * 100}%` }} />)}{!options.some(option => option.probability !== undefined) && <i className="is-unknown" />}</div>
              <b>{probability !== undefined ? `${(probability * 100).toFixed(1)}%` : question.type === ClaimType.score && evaluationNumber(answer) !== undefined ? evaluationNumber(answer)?.toFixed(2) : '—'}</b>
              <span className="control-choice" title={chosen?.description}>{evaluationChoice(answer) || (answer ? ClaimType[question.type] : t('control.waiting'))}</span>
              {chosen && top && !top.selected && <span className="sr-only">{t('selected')}: {chosen.id}</span>}
            </div>
          })}
          {!questions.length && <span className="control-empty">{t(decision ? 'control.answersOnly' : 'control.waitingJudgment')}</span>}
        </div>
        <div className="control-judgment-footer"><span>{questions.length > 1 ? t('parallelQuestions', { count: questions.length }) : t('control.finiteChoice')}</span><span>{answers ? `${Number(answers.elapsedMs)} ms` : decision?.state === 'pending' ? t('awaitingAnswer') : t('control.notRecorded')}</span></div>
      </div>
      <div className="control-executors" style={{ gridTemplateColumns: `repeat(${Math.max(1, executors.length)}, minmax(0, 1fr))` }}>
        {executors.map((group, index) => {
          const node = group.find(node => node.id === current?.id) || group[group.length - 1]
          return <button key={JSON.stringify([node.sessionId, node.label])} type="button" className="control-card control-executor" data-control-anchor={`executor-${index}`} data-control-stage="execution"
            data-active={selectedStage === 'execution' && activeExecutor === index || !replaying && node.state === 'pending'} data-state={node.state} onClick={() => onSelect(node.id)}>
            <span className="control-card-title">{node.kind === 'agent' ? <GitBranch /> : <Terminal />}<strong>{node.label}</strong><span>{group.length}</span></span>
            <span className="control-card-subtitle">{node.kind === 'agent' ? node.actor : t('control.nativeCall')}</span>
            <span className="control-executor-command" title={workflowNodeSummary(node)}>{workflowNodeSummary(node) || '—'}</span>
            <span className="control-card-status">{stateIcon(node)}{t(`workflow.states.${node.state}`)}</span>
          </button>
        })}
        {!executors.length && <div className="control-executor-empty"><Terminal />{t('control.waitingTool')}</div>}
      </div>
      {card('feedback', t('control.feedback'), 'Observe', observation, selectedStage === 'feedback', t('control.feedbackRole'))}
      {card('return', t('control.return'), response?.actor === 'JEV' ? 'LLM' : response?.actor || 'LLM', response, selectedStage === 'return', t('control.returnRole'))}
      {background && <button type="button" className="control-background" data-control-stage="background" data-active={selectedStage === 'background'} onClick={() => onSelect(background.id)}><Layers /><span>{t('workflow.background')}</span><strong>{t(background.literal ? 'workflow.states.' + background.state : background.label)}</strong>{stateIcon(background)}</button>}
    </div>
  </div>
}

export function ControlPlayback({ playing, frames, cursor, onPlay, onSeek, onLive }: {
  playing: boolean; frames: ControlFrame[]; cursor: number
  onPlay: () => void; onSeek: (cursor: number) => void; onLive: () => void
}) {
  const { t } = useTranslation('jev')
  return <div className="control-playback"><button type="button" className="control-play" aria-label={t(playing ? 'control.pause' : 'control.play')} onClick={onPlay} disabled={!frames.length}>{playing ? <Pause /> : <Play />}</button>
      <span className="control-frame-counter">{Math.max(0, cursor + 1)} / {frames.length}</span>
      <input type="range" min="0" max={Math.max(0, frames.length - 1)} value={Math.max(0, cursor)} aria-label={t('control.progress')} onChange={event => onSeek(Number(event.target.value))} />
      <button type="button" className="control-live" onClick={onLive}><RotateCcw />{t('control.toLive')}</button>
  </div>
}
