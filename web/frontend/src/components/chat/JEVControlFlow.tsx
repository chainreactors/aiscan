import { useLayoutEffect, useMemo, useRef, type ReactNode } from 'react'
import { ArrowDownLeft, Bot, Check, CircuitBoard, Eye, GitBranch, Layers, Loader2, MessageSquare, Repeat2, RotateCcw, Shield, Terminal, XCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { type WorkflowEdge, type WorkflowNode } from '../../lib/workflow-view'
import { scrollWorkflowViewport } from '../../lib/workflow-scroll'
import { controlGraph, controlReflexes, type ControlFrame, type ControlNode, type ControlReflex } from '../../lib/jev-control-flow'
import { JEVHelp } from './JEVHelp'
import { WorkflowNodeContent } from './JEVTimeline'
import { WorkflowToolContent } from './WorkflowToolContent'
import type { ViewerTimelineItem } from '@/viewer'
import { ControlConnections, JEVReflexLoop, useControlWires } from './JEVReflexLoop'
import './JEVControlFlow.css'

const icons = { tool: Terminal, agent: GitBranch, observation: Eye, takeover: Repeat2, handoff: Repeat2, boundary: CircuitBoard,
  decision: CircuitBoard, generation: Bot, publication: Layers, reasoning: Bot, response: MessageSquare, guardrail: Shield }

export function JEVControlFlow({ nodes, edges, current, frame, moving, replaying, playing, followCurrent, revealVersion, onSelect, renderItem }: {
  nodes: WorkflowNode[]; edges: WorkflowEdge[]; current?: WorkflowNode; frame?: ControlFrame; moving: boolean
  replaying: boolean; playing: boolean; onSelect: (id: string, breakpoint?: string) => void
  followCurrent: boolean; revealVersion: number; renderItem: (item: ViewerTimelineItem) => ReactNode
}) {
  const { t } = useTranslation('jev')
  const diagram = useRef<HTMLDivElement>(null), viewport = useRef<HTMLDivElement>(null)
  const revealed = useRef(revealVersion)
  const graph = useMemo(() => controlGraph(nodes, edges), [nodes, edges])
  const groups = useMemo(() => controlReflexes(graph.nodes), [graph])
  const groupByCard = useMemo(() => new Map(groups.flatMap(group => group.nodes.map(card => [card.id, group.id]))), [groups])
  const rows = useMemo(() => {
    const rows = new Map<string, { cards: ControlNode[]; group?: ControlReflex }>()
    for (const card of graph.nodes) {
      const groupId = groupByCard.get(card.id), key = groupId || `row:${card.row}`
      if (!rows.has(key)) rows.set(key, { cards: [], group: groups.find(group => group.id === groupId) })
      rows.get(key)!.cards.push(card)
    }
    return [...rows.entries()]
  }, [graph, groups, groupByCard])
  const outerRoutes = useMemo(() => graph.routes.filter(route => !groupByCard.has(route.source)
    || groupByCard.get(route.source) !== groupByCard.get(route.target)), [graph, groupByCard])
  const wires = useControlWires(diagram, outerRoutes, groupByCard)
  const active = (card: ControlNode) => card.frames.includes(frame?.id || '') || !replaying && card.node.state === 'pending'
    && (card.node.kind !== 'tool' || card.stage === 'execution' || card.stage === 'background')
  const activeRoutes = new Set(graph.routes.filter(route => {
    const target = graph.nodes.find(card => card.id === route.target)!
    return active(target)
  }).map(route => route.id))
  useLayoutEffect(() => {
    const requested = revealed.current !== revealVersion
    revealed.current = revealVersion
    if (!requested && !followCurrent) return
    const container = viewport.current
    const card = diagram.current && [...diagram.current.querySelectorAll<HTMLElement>('[data-control-anchor]')]
      .find(element => element.dataset.controlCurrent === 'true')
    if (!container || !card) return
    const bounds = container.getBoundingClientRect(), rect = card.getBoundingClientRect()
    const top = rect.top < bounds.top || rect.bottom > bounds.bottom ? container.scrollTop + rect.top - bounds.top - 12 : container.scrollTop
    scrollWorkflowViewport(container, top, container.scrollLeft)
    const loop = card.closest('.control-reflex-scroll')
    if (loop instanceof HTMLElement) {
      const bounds = loop.getBoundingClientRect()
      const left = rect.left < bounds.left || rect.right > bounds.right ? loop.scrollLeft + rect.left - bounds.left - 12 : loop.scrollLeft
      scrollWorkflowViewport(loop, loop.scrollTop, left)
    }
  }, [frame?.id, current?.id, followCurrent, revealVersion])
  const stateIcon = (state: WorkflowNode['state']) => state === 'pending' ? <Loader2 className="control-spin" />
    : state === 'failed' ? <XCircle /> : state === 'interrupted' ? <Repeat2 /> : <Check />
  const renderCard = (card: ControlNode) => {
    const { node, stage } = card
    const isCurrent = card.frames.includes(frame?.id || '')
    const record = stage === 'feedback' && node.kind === 'tool'
      ? node.related?.find(record => record.value.payload.case === 'result') || node.record : node.record
    const attributes = { 'data-control-anchor': card.id, 'data-control-node': node.id, 'data-control-stage': stage,
      'data-kind': node.kind, 'data-background': !!node.background, 'data-active': active(card), 'data-control-current': isCurrent,
      'data-state': card.state, 'data-record-id': record?.event.id, 'data-event-kind': record?.value.payload.case,
      'data-event-seq': record?.event.seq.toString() }
    const Icon = stage === 'feedback' ? ArrowDownLeft : icons[node.kind as keyof typeof icons] || Bot
    const title = stage === 'feedback' && node.kind === 'tool' ? t('control.toolResult') : node.literal ? node.label : t(node.label)
    const actor = node.kind === 'tool' ? 'TOOL' : node.actor === 'JEV' ? 'JEV' : 'LLM'
    const select = () => onSelect(node.id, card.frames[card.frames.length - 1])
    return <article key={card.id} className={`control-card control-${stage}`} {...attributes} data-actor={actor}
      role="group" tabIndex={0} aria-label={`${actor} · ${title}`} aria-current={isCurrent ? 'step' : undefined}
      onClick={event => {
        const target = event.target as HTMLElement
        if (target.closest('button, a, input, textarea, select, summary, [role=button]')) return
        event.currentTarget.focus({ preventScroll: true }); select()
      }} onKeyDown={event => {
        if (event.target !== event.currentTarget) return
        const step = event.key === 'ArrowDown' || event.key === 'ArrowRight' ? 1 : event.key === 'ArrowUp' || event.key === 'ArrowLeft' ? -1 : 0
        const destination = event.key === 'Home' ? graph.nodes[0] : event.key === 'End' ? graph.nodes[graph.nodes.length - 1] : step ? graph.nodes[graph.nodes.indexOf(card) + step] : undefined
        if (destination) {
          event.preventDefault()
          const element = [...(diagram.current?.querySelectorAll<HTMLElement>('[data-control-anchor]') || [])].find(element => element.dataset.controlAnchor === destination.id)
          element?.focus({ preventScroll: true }); onSelect(destination.node.id, destination.frames[destination.frames.length - 1])
        } else if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); select() }
      }}>
      <header className="control-card-title"><Icon /><strong>{title}</strong><span className="control-actor">{actor}{node.background ? ` · ${t('workflow.background')}` : ''}</span><i className={isCurrent && moving ? 'is-moving' : ''} style={{ visibility: isCurrent ? 'visible' : 'hidden' }} /></header>
      <div className="workflow-node-content" data-testid="workflow-node-content">
        {node.kind === 'response' ? <p className="workflow-content-label">{t(node.state === 'pending' ? 'control.composingResponse' : 'control.responseBelow')}</p>
          : node.record ? <WorkflowNodeContent node={node} nodes={nodes} stage={stage} />
          : node.kind === 'tool' ? <WorkflowToolContent node={node} stage={stage} />
          : node.item && renderItem(node.item)}
      </div>
      <footer className="control-card-status">{stateIcon(card.state)}{t(`workflow.states.${card.state}`)}</footer>
    </article>
  }
  return <div className="jev-control-flow" data-testid="jev-control-flow" data-stage={frame?.stage} data-replaying={replaying}>
    <div className="control-heading"><div><GitBranch /><strong>{t('control.title')}</strong><JEVHelp title={t('control.flowHelp')}><p>{t('control.flowHelpText')}</p></JEVHelp></div><span className="control-mode"><i className={moving ? 'is-moving' : ''} />{t(playing ? 'control.playing' : replaying ? 'control.history' : moving ? 'control.live' : 'control.recorded')}</span></div>
    <div className="control-role-legend" aria-label={t('control.roles')}><span data-actor="LLM"><Bot />LLM</span><span data-actor="JEV"><CircuitBoard />JEV · Claim</span><span data-actor="TOOL"><Terminal />TOOL</span></div>
    <div className="control-viewport" ref={viewport} role="region" aria-label={t('control.title')} tabIndex={0}>
    <div className="control-diagram" ref={diagram}>
      <ControlConnections wires={wires} nodes={graph.nodes} activeRoutes={activeRoutes} moving={moving} />
      {rows.map(([key, row]) => row.group ? <JEVReflexLoop key={key} group={row.group}
        routes={graph.routes.filter(route => groupByCard.get(route.source) === row.group!.id && groupByCard.get(route.target) === row.group!.id)}
        activeRoutes={activeRoutes} moving={moving} replaying={replaying} active={active} renderCard={renderCard} />
        : <div className="control-row" key={key}>{row.cards.map(renderCard)}</div>)}
    </div>
    </div>
  </div>
}

export function ControlPlayback({ frames, cursor, onSeek, onLive }: {
  frames: ControlFrame[]; cursor: number
  onSeek: (cursor: number) => void; onLive: () => void
}) {
  const { t } = useTranslation('jev')
  return <div className="control-playback">
      <span className="control-frame-counter">{Math.max(0, cursor + 1)} / {frames.length}</span>
      <input type="range" min="0" max={Math.max(0, frames.length - 1)} value={Math.max(0, cursor)} aria-label={t('control.progress')} onChange={event => onSeek(Number(event.target.value))} />
      <button type="button" className="control-live" onClick={onLive}><RotateCcw />{t('control.toLive')}</button>
  </div>
}
