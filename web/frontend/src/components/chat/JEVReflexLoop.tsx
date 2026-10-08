import { useId, useLayoutEffect, useMemo, useRef, useState, type ReactNode, type RefObject } from 'react'
import { Repeat2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { controlReflexRows, type ControlNode, type ControlReflex, type ControlRoute } from '../../lib/jev-control-flow'
import { workflowDecision } from '../../lib/workflow-view'

type Wire = ControlRoute & { path: string; loop: boolean }
type Box = { x: number; y: number; w: number; h: number }

export function useControlWires(diagram: RefObject<HTMLDivElement>, routes: ControlRoute[], groupByCard?: Map<string, string>) {
  const [wires, setWires] = useState<Wire[]>([])
  useLayoutEffect(() => {
    const element = diagram.current
    if (!element) return
    const measure = () => {
      const rect = element.getBoundingClientRect()
      const box = (anchor: HTMLElement): Box => {
        const b = anchor.getBoundingClientRect()
        return { x: b.left - rect.left, y: b.top - rect.top, w: b.width, h: b.height }
      }
      const boxes = new Map([...element.querySelectorAll<HTMLElement>('[data-control-anchor]')]
        .map(anchor => [anchor.dataset.controlAnchor!, box(anchor)]))
      const groups = new Map([...element.querySelectorAll<HTMLElement>('[data-control-reflex]')]
        .map(anchor => [anchor.dataset.controlReflex!, box(anchor)]))
      setWires(routes.flatMap(route => {
        const a = groups.get(groupByCard?.get(route.source) || '') || boxes.get(route.source)
        const b = groups.get(groupByCard?.get(route.target) || '') || boxes.get(route.target)
        if (!a || !b) return []
        const crossColumn = Math.abs(a.x - b.x) > 30 && !groupByCard
        const loop = crossColumn && a.x > b.x
        let path: string
        if (crossColumn) {
          const sx = loop ? a.x : a.x + a.w, sy = a.y + a.h / 2
          const tx = loop ? b.x + b.w : b.x, ty = b.y + b.h / 2, middle = (sx + tx) / 2
          path = `M ${sx} ${sy} C ${middle} ${sy}, ${middle} ${ty}, ${tx} ${ty}`
        } else {
          const sx = a.x + a.w / 2, sy = a.y + a.h, tx = b.x + b.w / 2, ty = b.y, middle = (sy + ty) / 2
          path = `M ${sx} ${sy} C ${sx} ${middle}, ${tx} ${middle}, ${tx} ${ty}`
        }
        return [{ ...route, path, loop }]
      }))
    }
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    // Media or inline output can resize a card while a taller sibling keeps
    // the diagram's overall height unchanged. Keep its return arrow attached.
    element.querySelectorAll('[data-control-anchor]').forEach(anchor => observer.observe(anchor))
    return () => observer.disconnect()
  }, [diagram, routes, groupByCard])
  return wires
}

export function ControlConnections({ wires, nodes, activeRoutes, moving }: {
  wires: Wire[]; nodes: ControlNode[]; activeRoutes: Set<string>; moving: boolean
}) {
  const id = useId()
  const byId = new Map(nodes.map(card => [card.id, card]))
  return <svg className="control-wires" aria-hidden="true"><defs><marker id={`${id}-arrow`} viewBox="0 0 8 8" refX="7" refY="4" markerWidth="5" markerHeight="5" orient="auto"><path d="M 1 1 L 7 4 L 1 7" fill="none" stroke="context-stroke" strokeWidth="1.4" /></marker></defs>
    {wires.map(wire => {
      const source = byId.get(wire.source), target = byId.get(wire.target)
      if (!source || !target) return null
      return <g key={wire.id} data-control-route={wire.id} data-source-node={source.node.id} data-target-node={target.node.id}
        data-source-stage={source.stage} data-target-stage={target.stage} data-loop={wire.loop || undefined} data-active={activeRoutes.has(wire.id)}>
        <path d={wire.path} className={`control-wire ${wire.feedback ? 'is-feedback' : ''}`} markerEnd={`url(#${id}-arrow)`} />
        {moving && activeRoutes.has(wire.id) && <circle r="3" className="control-particle" style={{ offsetPath: `path('${wire.path}')` }} />}
      </g>
    })}
  </svg>
}

export function JEVReflexLoop({ group, routes, activeRoutes, moving, replaying, active, renderCard }: {
  group: ControlReflex; routes: ControlRoute[]; activeRoutes: Set<string>; moving: boolean; replaying: boolean
  active: (card: ControlNode) => boolean; renderCard: (card: ControlNode) => ReactNode
}) {
  const { t } = useTranslation('jev'), diagram = useRef<HTMLDivElement>(null)
  const rows = useMemo(() => controlReflexRows(group), [group])
  const wires = useControlWires(diagram, routes)
  const payload = group.takeover.node.record?.value.payload
  const definition = payload?.case === 'takeover' ? payload.value.definition : undefined
  const decisions = group.nodes.filter(card => card.node.kind === 'decision')
  const claims = decisions.reduce((count, card) => {
    const { request, result } = workflowDecision(card.node)
    return count + new Set([...Object.keys(request?.claims || {}), ...Object.keys(result?.evaluations || {})]).size
  }, 0)
  const calls = group.nodes.filter(card => card.node.kind === 'tool' && card.stage === 'execution').length
  const running = !group.handoff && (replaying || group.nodes.some(card => card.state === 'pending'))
  return <section className="control-reflex" data-control-reflex={group.id} data-reflex-id={definition?.id}
    data-state={group.handoff ? 'handed-off' : running ? 'running' : 'recorded'} data-active={group.nodes.some(active)} aria-label={t('control.reflexLoop')}>
    <header className="control-reflex-header"><div className="control-reflex-heading"><Repeat2 /><strong>{t('control.reflexLoop')}</strong>
      <span>{t(group.handoff ? 'control.loopReturned' : running ? 'running' : 'control.recorded')}</span></div>
    <div className="control-reflex-meta"><code>{definition?.id}</code><span>{t('control.loopCounts', { decisions: decisions.length, claims, calls })}</span></div>
    {!!rows.length && <div className="control-reflex-hint">{t('control.loopScroll')}</div>}</header>
    <div className="control-reflex-scroll" role="region" aria-label={t('control.reflexLoop')} tabIndex={0}><div className="control-reflex-diagram" ref={diagram}>
      <ControlConnections wires={wires} nodes={group.nodes} activeRoutes={activeRoutes} moving={moving} />
      <div className="control-reflex-entry">{renderCard(group.takeover)}</div>
      {!!rows.length && <div className="control-reflex-columns"><span>JEV · Claim</span><span>TOOL · {t('control.toolResult')}</span></div>}
      {rows.map(row => <div key={row.id} className="control-reflex-row">
        {!!row.other.length && <div className="control-reflex-context">{row.other.map(renderCard)}</div>}
        {!!row.judgments.length && <div className="control-reflex-judgments">{row.judgments.map(renderCard)}</div>}
        {!!row.tools.length && <div className="control-reflex-tools">{row.tools.map(bundle =>
          <div className="control-reflex-invocation" key={bundle[0].id}>{bundle.map(renderCard)}</div>)}</div>}
      </div>)}
      {group.handoff && <div className="control-reflex-exit">{renderCard(group.handoff)}</div>}
    </div></div>
  </section>
}
