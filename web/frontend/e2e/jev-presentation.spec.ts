import { ClaimType } from '../src/gen/decision/claim_pb'
import { test, expect } from '@playwright/test'
import { create, type MessageInitShape } from '@bufbuild/protobuf'
import { anyPack, timestampFromDate } from '@bufbuild/protobuf/wkt'
import { RuntimeEventSchema, type RuntimeEvent } from '../src/gen/types/jev_pb'
import { EventSchema } from '../cyber-ui/packages/aop/src/gen/aop/event_pb'
import { RefSchema, StartedSchema } from '../cyber-ui/packages/aop/src/gen/aop/operation/protocol_pb'
import { projectJEV, withJEV, jevTimelineEvents, isJEVBoundary } from '../src/lib/jev-view'
import { projectRuntimeNetwork } from '../src/lib/jev-network'
import { reduceAOPToTimeline } from '../cyber-ui/packages/viewer/src/lib/aop-reducer'
import { EvaluationSchema, ClaimSchema } from '../src/gen/decision/claim_pb'
import { decisionOptions, decisionQuestions } from '../src/lib/jev-decisions'

function event(seq: number, payload: MessageInitShape<typeof EventSchema>['payload'], sessionId = 'session', turnId = 'turn') {
  return create(EventSchema, { id: `event:${sessionId}:${seq}`, sessionId, turnId, emitter: 'agent', seq: BigInt(seq),
    emittedAt: timestampFromDate(new Date(1700000000000 + seq * 1000)), payload })
}
function runtime(seq: number, payload: MessageInitShape<typeof RuntimeEventSchema>['payload'], fields: Partial<RuntimeEvent> = {}, sessionId = 'session', turnId = 'turn') {
  return event(seq, { case: 'extension', value: anyPack(RuntimeEventSchema, create(RuntimeEventSchema, {
    taskId: 'task', segmentId: 'segment', step: 1, payload, ...fields,
  })) }, sessionId, turnId)
}
function scene(seq = 2, fields: Partial<RuntimeEvent> = {}) {
  return runtime(seq, { case: 'takeover', value: { definition: { id: 'r1', when: 'Read current evidence', observe: 'js:original', decide: 'Choose current bindings' } } }, fields)
}
function dispatch(seq = 3, fields: Partial<RuntimeEvent> = {}) {
  return runtime(seq, { case: 'dispatch', value: { call: { id: 'call', name: 'arbitrary-tool', arguments: { data: new TextEncoder().encode('{}') } } } }, { callId: 'call', ...fields })
}
const start = () => event(1, { case: 'turnStarted', value: {} })
const result = () => runtime(5, { case: 'result', value: { result: { callId: 'call', name: 'arbitrary-tool' } } }, { callId: 'call' })
const handoff = () => runtime(6, { case: 'handoff', value: { reason: 'report' } })
const end = () => event(7, { case: 'turnEnded', value: { stopReason: 'completed' } })

test('replay deduplicates the continuous segment, tools and associated observations', () => {
  const observation = event(4, { case: 'extension', value: { typeUrl: 'example.Observation' } })
  observation.extensions = [anyPack(RefSchema, create(RefSchema, { operationId: 'op', callId: 'call' }))]
  const events = [start(), scene(), dispatch(), observation, result(), handoff(), end()]
  const projection = projectJEV([...events, ...events])
  expect(projection.segments).toHaveLength(1)
  expect(projection.segments[0]).toMatchObject({ status: 'handed_off', reason: 'report', definition: { observe: 'js:original' },
    steps: [{ call: { name: 'arbitrary-tool' }, result: { callId: 'call' }, observations: [{ id: observation.id }] }] })
  const base = reduceAOPToTimeline(events)
  const timeline = withJEV(base, projection)
  expect(timeline.filter(item => item.kind === 'extension' && item.extensionType === 'jev_segment')).toHaveLength(1)
  expect(timeline.some(item => item.id === observation.id)).toBe(false)
})

test('late background publication never resurrects an ended turn or replaces its definition snapshot', () => {
  const published = runtime(10, { case: 'libraryChange', value: { state: 'reflex_published', reflex: { id: 'r2', observe: 'js:new' }, replacedReflexId: 'r1' } }, { background: true, segmentId: '', reflexId: 'r2' })
  const projection = projectJEV([start(), scene(), dispatch(), end(), published])
  expect(projection.segments[0]).toMatchObject({ status: 'ended', reason: 'turn_ended', definition: { id: 'r1', observe: 'js:original' } })
  expect(projection.compilations[0]).toMatchObject({ state: 'reflex_published', turnId: 'turn' })
  expect(projectRuntimeNetwork([start(), scene(), end(), published], projection).nodes.find(n => n.data.kind === 'agent')?.data.status).toBe('ended')
})

test('background retry becomes live again after a failed generation', () => {
  const failure = runtime(2, { case: 'libraryChange', value: { state: 'failed', reason: 'Invalid draft' } }, { background: true, segmentId: '' })
  const request = runtime(3, { case: 'decisionRequest', value: { requestId: 'retry', purpose: 'jev_reflex', claims: {} } }, { background: true, segmentId: '' })
  expect(projectJEV([start(), failure]).compilations[0].state).toBe('failed')
  expect(projectJEV([start(), failure, request]).compilations[0].state).toBe('reviewing')
})

test('a subsequent takeover creates a linked segment while retaining the previous handoff', () => {
  const next = { segmentId: 'next', previousSegmentId: 'segment' }
  const projection = projectJEV([start(), scene(), handoff(), scene(8, next), dispatch(9, next)])
  expect(projection.segments).toHaveLength(2)
  expect(projection.segments[0].status).toBe('handed_off')
  expect(projection.segments[1]).toMatchObject({ id: 'next', previousId: 'segment', status: 'running' })
})

test('Chat boundaries split the real model response around takeover and handoff', () => {
  const before = event(1, { case: 'message', value: { id: 'before', role: 'assistant', content: [{ value: { case: 'text', value: { text: 'Preparing' } } }] } })
  const after = event(8, { case: 'message', value: { id: 'after', role: 'assistant', content: [{ value: { case: 'text', value: { text: 'Answer from evidence' } } }] } })
  const events = [before, scene(), dispatch(), result(), handoff(), after, end()]
  const timeline = withJEV(reduceAOPToTimeline(jevTimelineEvents(events), { responseBoundary: isJEVBoundary }), projectJEV(events))
  expect(timeline.map(item => item.kind)).toEqual(['assistant_response', 'extension', 'assistant_response'])
})

test('network renders only observed tools and actual delegated sessions', () => {
  const child = event(4, { case: 'sessionStarted', value: { parentSessionId: 'session', parentToolCallId: 'delegate', agentName: 'researcher' } }, 'child', '')
  const childCall = event(5, { case: 'toolCall', value: { id: 'http', name: 'http-reader' } }, 'child', 'child-turn')
  const unrelated = event(6, { case: 'sessionStarted', value: { parentSessionId: 'session', agentName: 'ordinary-session' } }, 'unrelated', '')
  const events = [start(), scene(), dispatch(), child, childCall, unrelated, result(), handoff()]
  const graph = projectRuntimeNetwork(events, projectJEV(events))
  expect(graph.nodes.map(n => n.data.label)).toContain('arbitrary-tool')
  expect(graph.nodes.map(n => n.data.label)).toContain('http-reader')
  expect(graph.nodes.some(n => n.id === 'agent:child')).toBe(true)
  expect(graph.nodes.some(n => n.id === 'agent:unrelated')).toBe(false)
  expect(graph.nodes.some(n => /Playwright|Reflex/.test(n.data.label))).toBe(false)
  expect(graph.edges.some(e => e.source === 'agent:session' && e.target === 'agent:child')).toBe(true)
})

test('call IDs are scoped to their actual session and turn', () => {
  const other = runtime(6, { case: 'result', value: { result: { callId: 'call', name: 'arbitrary-tool', isError: true } } }, { callId: 'call' }, 'other', 'turn')
  const projection = projectJEV([start(), scene(), dispatch(), other])
  expect(projection.segments[0].steps[0].result).toBeUndefined()
})

test('network deduplicates command observations and excludes delegated activity from later root turns', () => {
  const command = event(4, { case: 'extension', value: anyPack(StartedSchema, create(StartedSchema, { kind: 'command', name: 'native-command' })) })
  command.extensions = [anyPack(RefSchema, create(RefSchema, { callId: 'call', operationId: 'operation' }))]
  const child = event(5, { case: 'sessionStarted', value: { parentSessionId: 'session', parentToolCallId: 'delegate' } }, 'child', '')
  child.emitter = 'worker'
  const nextTurn = event(9, { case: 'turnStarted', value: {} }, 'session', 'next-turn')
  const lateChildCall = event(10, { case: 'toolCall', value: { id: 'later', name: 'later-tool' } }, 'child', 'late-turn')
  const events = [start(), scene(), dispatch(), command, command, child, end(), nextTurn, lateChildCall]
  const graph = projectRuntimeNetwork(events, projectJEV(events), JSON.stringify(['session', 'turn']), true)
  expect(graph.nodes.find(node => node.id === 'agent:child')?.data.label).toBe('worker')
  expect(graph.nodes.map(node => node.data.label)).not.toContain('later-tool')
  const native = graph.nodes.find(node => node.data.label === 'native-command')
  expect(native?.data.callIds).toEqual(['call'])
  expect(graph.edges.find(edge => edge.target === native?.id)?.data?.count).toBe(1)
  expect(graph.nodes.every(node => node.position.x === 0 && node.data.vertical)).toBe(true)
})

test('entry checks keep their boundaries and preceding checks survive a later takeover', () => {
  const checking = runtime(2, { case: 'boundary', value: { reason: 'checking' } })
  const finished = runtime(4, { case: 'boundary', value: { reason: 'observation_unavailable' } })
  const next = { segmentId: 'next' }
  const another = runtime(5, { case: 'boundary', value: { reason: 'checking' } }, next)
  const returned = runtime(6, { case: 'boundary', value: { reason: 'defer' } }, next)
  const events = [start(), checking, finished, another, returned, end()]
  const projection = projectJEV([...events, ...events])
  expect(projection.checks).toHaveLength(2)
  expect(projection.checks.map(check => check.reason)).toEqual(['observation_unavailable', 'defer'])
  expect(projection.checks.map(check => check.iteration)).toEqual([1, 2])
  expect(projection.checks[0].nextId).toBe(projection.checks[1].id)
  expect(projection.checks[1].previousId).toBe(projection.checks[0].id)
  expect(withJEV(reduceAOPToTimeline(events), projection).filter(item => item.kind === 'extension' && item.extensionType === 'jev_check')).toHaveLength(2)
  expect(projectJEV([...events, scene(8)]).checks).toHaveLength(1)
})

test('choice ranks the distribution independently of the returned selection and keeps unknown probabilities', () => {
  const question = create(ClaimSchema, { type: ClaimType.choice, context: "Choose the current operation." + "\nmissing: Unknown\nz: Tie\na: Tie\nwinner: Most likely\npicked: Returned choice", options: ["missing","z","a","winner","picked"] })
  const answer = create(EvaluationSchema, { value: { case: 'choice', value: 'picked' }, probabilities: { winner: .6, picked: .2, z: .1, a: .1 } })
  const options = decisionOptions(question, answer)
  expect(options.map(option => option.id)).toEqual(['winner', 'picked', 'a', 'z', 'missing'])
  expect(options.filter(option => option.selected).map(option => option.id)).toEqual(['picked'])
  expect(options.at(-1)?.probability).toBeUndefined()
  expect(decisionQuestions({ z: question, generation: question, entry: question, a: question }).map(([id]) => id)).toEqual(['entry', 'generation', 'a', 'z'])
})

test('foreground checks split model output at the actual checkpoint', () => {
  const before = event(1, { case: 'message', value: { id: 'before', role: 'assistant', content: [{ value: { case: 'text', value: { text: 'Inspecting' } } }] } })
  const checking = runtime(2, { case: 'boundary', value: { reason: 'checking' } })
  const finished = runtime(3, { case: 'boundary', value: { reason: 'defer' } })
  const after = event(4, { case: 'message', value: { id: 'after', role: 'assistant', content: [{ value: { case: 'text', value: { text: 'Continuing from this check' } } }] } })
  const events = [before, checking, finished, after]
  const timeline = withJEV(reduceAOPToTimeline(jevTimelineEvents(events), { responseBoundary: isJEVBoundary }), projectJEV(events))
  expect(timeline.map(item => item.kind)).toEqual(['assistant_response', 'extension', 'assistant_response'])
})
