import type { AOPEvent } from '@/viewer'
import { eventTime } from './jev-view'
import type { WorkflowEdge, WorkflowNode, WorkflowState } from './workflow-view'

export type ControlStage = 'model' | 'judgment' | 'execution' | 'feedback' | 'return' | 'background'
export type ControlFrame = { id: string; nodeId: string; timestamp: number; stage: ControlStage; state: WorkflowState; eventId?: string }

function stage(node: WorkflowNode, event?: AOPEvent): ControlStage {
  if (node.background) return 'background'
  const payload = event ? node.related?.find(record => record.event.id === event.id)?.value.payload : node.record?.value.payload
  if (payload?.case === 'result' || event?.payload.case === 'toolResult' || node.kind === 'observation') return 'feedback'
  if (node.kind === 'handoff' || node.kind === 'response' || node.kind === 'boundary' && payload?.case === 'boundary' && payload.value.reason !== 'checking') return 'return'
  if (node.kind === 'tool' || node.kind === 'agent' || node.kind === 'guardrail') return 'execution'
  if (node.actor === 'JEV') return 'judgment'
  return 'model'
}

export function controlFrames(nodes: WorkflowNode[]): ControlFrame[] {
  return nodes.flatMap(node => {
    const events = node.related?.map(record => record.event) || node.events || []
    if (!events.length) return [{ id: node.id, nodeId: node.id, timestamp: node.timestamp, stage: stage(node), state: node.state }]
    return events.map(event => {
      const payload = node.related?.find(record => record.event.id === event.id)?.value.payload
      const pending = payload?.case === 'decisionRequest' || payload?.case === 'dispatch'
        || payload?.case === 'generation' && payload.value.state === 'started' || event.payload.case === 'toolCall'
      const failed = payload?.case === 'decisionResult' && !!payload.value.error || payload?.case === 'result' && !!payload.value.result?.isError
        || payload?.case === 'generation' && !!payload.value.error || payload?.case === 'libraryChange' && ['failed', 'draft_rejected'].includes(payload.value.state)
        || event.payload.case === 'toolResult' && event.payload.value.isError
      return { id: JSON.stringify([node.id, event.id]), nodeId: node.id, eventId: event.id, timestamp: eventTime(event), stage: stage(node, event),
        state: failed ? 'failed' : pending ? 'pending' : 'completed' } satisfies ControlFrame
    })
  }).sort((a, b) => a.timestamp - b.timestamp)
}

export type ControlNode = {
  id: string; node: WorkflowNode; stage: ControlStage; state: WorkflowState; timestamp: number; frames: string[]; row: number
}
export type ControlRoute = { id: string; source: string; target: string; feedback: boolean }
export type ControlReflex = { id: string; takeover: ControlNode; nodes: ControlNode[]; handoff?: ControlNode }

// Membership comes from a recorded takeover and its segment, never from tool
// names. Entry checks, background work and another invocation stay outside.
export function controlReflexes(nodes: ControlNode[]): ControlReflex[] {
  const groups: ControlReflex[] = [], active = new Map<string, ControlReflex>(), owners = new Map<string, ControlReflex>()
  for (const card of nodes) {
    const { node } = card, value = node.record?.value
    if (!value?.segmentId || node.background) continue
    const key = JSON.stringify([node.sessionId, node.turnId, value.segmentId])
    if (node.kind === 'takeover') {
      const group = { id: card.id, takeover: card, nodes: [card] }
      groups.push(group); active.set(key, group); owners.set(node.id, group)
      continue
    }
    const group = owners.get(node.id) || active.get(key)
    if (!group) continue
    group.nodes.push(card); owners.set(node.id, group)
    if (node.kind === 'handoff') { group.handoff = card; active.delete(key) }
  }
  return groups
}

export type ControlReflexRow = { id: string; judgments: ControlNode[]; tools: ControlNode[][]; other: ControlNode[] }

// One semantic turn can contain several calls and results. Keep them together
// inside the Reflex instead of laying every card out as another global phase.
export function controlReflexRows(group: ControlReflex): ControlReflexRow[] {
  const rows: ControlReflexRow[] = [], calls = new Map<string, ControlNode[]>()
  let row: ControlReflexRow | undefined
  const nextRow = (card: ControlNode) => {
    row = { id: card.id, judgments: [], tools: [], other: [] }; rows.push(row)
    return row
  }
  for (const card of group.nodes) {
    if (card === group.takeover || card === group.handoff) continue
    if (card.node.kind === 'decision') { nextRow(card).judgments.push(card); continue }
    if (card.node.kind === 'tool') {
      const invocation = calls.get(card.node.id)
      if (invocation) { invocation.push(card); continue }
      if (!row || row.other.length) nextRow(card)
      const bundle = [card]; row!.tools.push(bundle); calls.set(card.node.id, bundle)
      continue
    }
    nextRow(card).other.push(card)
    row = undefined
  }
  return rows
}

// Project recorded transitions, rather than assigning every task to a fixed set
// of roles. A call and its result have distinct cards, paired by invocation ID.
// Calls dispatched before a result share a predecessor and remain parallel.
export function controlGraph(nodes: WorkflowNode[], edges: WorkflowEdge[] = []): { nodes: ControlNode[]; routes: ControlRoute[] } {
  const byId = new Map(nodes.map(node => [node.id, node])), cards = new Map<string, ControlNode>(), routes = new Map<string, ControlRoute>()
  const streams = new Map<string, { frontier: Set<string>; pending: Set<string>; dispatchFrom: Set<string> }>()
  const streamId = (node: WorkflowNode) => JSON.stringify([node.sessionId, node.turnId, !!node.background])
  const connect = (source: string, target: string) => {
    if (source === target) return
    const id = JSON.stringify([source, target])
    routes.set(id, { id, source, target, feedback: cards.get(source)?.stage === 'feedback' || cards.get(target)?.stage === 'feedback' })
  }
  for (const frame of controlFrames(nodes)) {
    const node = byId.get(frame.nodeId)!, id = JSON.stringify([node.id, frame.stage])
    const existing = cards.get(id)
    if (existing) { existing.frames.push(frame.id); existing.state = node.state; continue }
    cards.set(id, { id, node, stage: frame.stage, state: frame.stage === 'feedback' ? frame.state : node.state,
      timestamp: frame.timestamp, frames: [frame.id], row: 0 })
    const key = streamId(node)
    let stream = streams.get(key)
    if (!stream) { stream = { frontier: new Set(), pending: new Set(), dispatchFrom: new Set() }; streams.set(key, stream) }
    const execution = JSON.stringify([node.id, 'execution'])
    if (frame.stage === 'feedback' && cards.has(execution)) {
      connect(execution, id)
      stream.pending.delete(execution)
      stream.frontier.add(id)
    } else if (frame.stage === 'execution' && node.kind === 'tool') {
      if (!stream.pending.size || stream.frontier.size) {
        stream.dispatchFrom = new Set(stream.frontier)
        stream.frontier.clear()
      }
      for (const source of stream.dispatchFrom) connect(source, id)
      stream.pending.add(id)
    } else {
      for (const source of stream.frontier) connect(source, id)
      stream.frontier = new Set([id])
      stream.dispatchFrom = new Set([id])
    }
  }
  // Preserve recorded links into delegated sessions and background work without
  // allowing their later decisions to take over the foreground execution path.
  for (const edge of edges) {
    const source = byId.get(edge.source), target = byId.get(edge.target)
    if (!source || !target || streamId(source) === streamId(target)) continue
    const to = [...cards.values()].find(card => card.node.id === target.id)
    const from = to && [...cards.values()].reverse().find(card => card.node.id === source.id && card.timestamp <= to.timestamp)
    if (from && to) connect(from.id, to.id)
  }
  const ordered = [...cards.values()].sort((a, b) => a.timestamp - b.timestamp)
  for (const card of ordered) for (const route of routes.values()) {
    if (route.target === card.id) card.row = Math.max(card.row, cards.get(route.source)!.row + 1)
  }
  return { nodes: ordered, routes: [...routes.values()] }
}

// Replay never reveals an answer, result or later publication before its event.
export function controlSnapshot(nodes: WorkflowNode[], frames: ControlFrame[], cursor: number): WorkflowNode[] {
  const seen = frames.slice(0, cursor + 1), ids = new Set(seen.map(frame => frame.nodeId))
  return nodes.filter(node => ids.has(node.id)).map(node => {
    const nodeFrames = seen.filter(frame => frame.nodeId === node.id), last = nodeFrames[nodeFrames.length - 1]
    const eventIds = new Set(nodeFrames.map(frame => frame.eventId))
    const related = node.related?.filter(record => eventIds.has(record.event.id))
    const result = related?.find(record => record.value.payload.case === 'result')?.value.payload
    const nativeResult = node.events?.find(event => eventIds.has(event.id) && event.payload.case === 'toolResult')?.payload
    return { ...node, state: last.state, related, events: node.events?.filter(event => eventIds.has(event.id)),
      step: node.step ? { ...node.step, result: result?.case === 'result' ? result.value.result : undefined,
        observations: node.step.observations.filter(event => eventTime(event) <= last.timestamp) } : undefined,
      item: node.item?.kind === 'tool_call' ? { ...node.item, toolCall: { ...node.item.toolCall,
        pending: last.state === 'pending', error: last.state === 'failed', result: nativeResult?.case === 'toolResult' ? node.item.toolCall.result : undefined,
        toolResult: nativeResult?.case === 'toolResult' ? nativeResult.value : undefined,
        observations: node.item.toolCall.observations?.filter(event => eventTime(event) <= last.timestamp) } } : node.item,
    }
  })
}
