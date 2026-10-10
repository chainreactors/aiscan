import { ClaimType } from '../src/gen/decision/claim_pb'
import { test, expect, type Page } from '@playwright/test'
import { create, createRegistry, fromJson, toBinary } from '@bufbuild/protobuf'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { anyPack, timestampFromDate } from '@bufbuild/protobuf/wkt'
import { EventSchema } from '../cyber-ui/packages/aop/src/gen/aop/event_pb'
import { RuntimeEventSchema } from '../src/gen/types/jev_pb'
import { file_types_chat } from '../src/gen/types/chat_pb'
import { file_types_agent } from '../src/gen/types/agent_pb'
import { file_aop_operation_protocol } from '../cyber-ui/packages/aop/src/gen/aop/operation/protocol_pb'
import { file_aop_file_protocol } from '../cyber-ui/packages/aop/src/gen/aop/file/protocol_pb'
import { file_aop_pty_protocol } from '../cyber-ui/packages/aop/src/gen/aop/pty/protocol_pb'
import { reduceAOPToTimeline } from '../cyber-ui/packages/viewer/src/lib/aop-reducer'
import { isJEVBoundary, jevTimelineEvents, projectJEV, withJEV } from '../src/lib/jev-view'
import { withWorkflows, type WorkflowTurn } from '../src/lib/workflow-view'
import { controlFrames, controlGraph, controlReflexes, controlReflexRows, controlSnapshot } from '../src/lib/jev-control-flow'

test.use({ video: { mode: 'on', size: { width: 1440, height: 1000 } } })

const event = (seq: number, payload: any) => create(EventSchema, { id: `motion-${seq}`, seq: BigInt(seq), sessionId: 'session-1', turnId: 'turn-1',
  emitter: 'agent', emittedAt: timestampFromDate(new Date(1700000000000 + seq * 1000)), payload })
const trace = (seq: number, payload: any, background = false) => event(seq, { case: 'extension', value: anyPack(RuntimeEventSchema,
  create(RuntimeEventSchema, { taskId: 'task', segmentId: background ? '' : 'segment', step: 1, background, payload })) })
const definition = { id: 'inspect-evidence', when: 'Read the current documentation', decide: 'Select a recorded operation', observe: 'js:()=>({state:{},candidates:[]})' }
const claims = {
  next: { type: ClaimType.choice, context: "Choose the current operation." + "\nread: Read current evidence\ndefer: Return to main model", options: ["read","defer"] },
  enough: { type: ClaimType.choice, context: "Choose the current operation." + "\nyes: Evidence is complete\nno: More evidence required", options: ["yes","no"] },
}
const call = { id: 'read', name: 'bash', arguments: { data: new TextEncoder().encode('{"command":"playwright evaluate reference document.body.innerText"}') } }
const initial = [event(1, { case: 'turnStarted', value: {} }), event(2, { case: 'message', value: { id: 'plan', role: 'assistant', content: [
  { value: { case: 'reasoning', value: { text: 'Inspect the current documentation and verify the recorded evidence.' } } },
] } }), trace(3, { case: 'takeover', value: { definition } }), trace(4, { case: 'decisionRequest', value: { requestId: 'next', claims } })]
const answer = trace(5, { case: 'decisionResult', value: { requestId: 'next', elapsedMs: 186, evaluations: {
  next: { value: { case: 'choice', value: 'read' }, probabilities: { read: .94, defer: .06 } },
  enough: { value: { case: 'choice', value: 'no' }, probabilities: { yes: .2, no: .8 } },
} } })
const dispatch = trace(6, { case: 'dispatch', value: { call } })
const result = trace(7, { case: 'result', value: { elapsedMs: 42, result: { callId: 'read', name: 'bash', output: [{ value: { case: 'text', value: { text: 'Current page evidence: ensure_ascii escapes non-ASCII characters.' } } }] } } })
const next = trace(8, { case: 'decisionRequest', value: { requestId: 'report', claims: { next: { type: ClaimType.choice, context: "Choose the current operation." + "\nreport: Compose answer\ndefer: New reasoning", options: ["report","defer"] } } } })
const returned = [trace(9, { case: 'decisionResult', value: { requestId: 'report', elapsedMs: 153, evaluations: { next: { value: { case: 'choice', value: 'report' }, probabilities: { report: .97, defer: .03 } } } } }),
  trace(10, { case: 'handoff', value: { reason: 'report' } }),
  event(11, { case: 'message', value: { id: 'answer', role: 'assistant', content: [{ value: { case: 'text', value: { text: 'Verified against recorded documentation: ensure_ascii=True escapes non-ASCII characters.' } } }] } }),
  event(12, { case: 'turnEnded', value: { stopReason: 'completed' } })]

function scrollingLiveEvents() {
  const events = initial.slice(0, 3)
  for (let i = 0; i < 7; i++) {
    const tool = { ...call, id: `previous-${i}` }
    events.push(trace(4 + i * 2, { case: 'dispatch', value: { call: tool } }),
      trace(5 + i * 2, { case: 'result', value: { result: { callId: tool.id, name: tool.name } } }))
  }
  events.push(trace(18, { case: 'decisionRequest', value: { requestId: 'last', claims } }))
  return events
}

async function mount(page: Page) {
  await page.route('**/cyber.rpc.chat.SessionService/ListCommands', route => route.fulfill({ contentType: 'application/json', body: '{"commands":[]}' }))
  await page.addInitScript(() => localStorage.setItem('cyber-locale', 'zh'))
  await page.goto('/e2e/fixtures/jev.html')
  await page.waitForFunction(() => typeof (window as any).renderJEVEvents === 'function')
}
async function render(page: Page, events: ReturnType<typeof event>[], append = false) {
  await page.evaluate(({ values, append }) => (window as any).renderJEVEvents(values, append), { values: events.map(value => [...toBinary(EventSchema, value)]), append })
}

const workflowsFor = (events: ReturnType<typeof event>[]) => withWorkflows(
  withJEV(reduceAOPToTimeline(jevTimelineEvents(events), { responseBoundary: isJEVBoundary }), projectJEV(events)), events)
  .flatMap(item => item.kind === 'extension' && item.extensionType === 'workflow' ? [item.data.workflow as WorkflowTurn] : [])

test('graph topology follows recorded parallel calls, their results and the join', () => {
  const other = { ...call, id: 'other', name: 'http-reader' }
  const events = [initial[0], initial[1], event(3, { case: 'toolCall', value: call }), event(4, { case: 'toolCall', value: other }),
    event(5, { case: 'toolResult', value: { callId: other.id, name: other.name } }),
    event(6, { case: 'toolResult', value: { callId: call.id, name: call.name, isError: true } }), ...returned.slice(2)]
  const workflow = workflowsFor(events)[0], graph = controlGraph(workflow.nodes, workflow.edges)
  const calls = graph.nodes.filter(node => node.stage === 'execution'), feedback = graph.nodes.filter(node => node.stage === 'feedback')
  expect(calls).toHaveLength(2)
  expect(calls[0].row).toBe(calls[1].row)
  expect(feedback).toHaveLength(2)
  for (const result of feedback) {
    const incoming = graph.routes.filter(route => route.target === result.id)
    expect(incoming).toHaveLength(1)
    expect(graph.nodes.find(node => node.id === incoming[0].source)?.node.id).toBe(result.node.id)
  }
  expect(feedback.find(node => node.node.label === call.name)?.state).toBe('failed')
  const response = graph.nodes.find(node => node.node.kind === 'response')!
  expect(graph.routes.filter(route => route.target === response.id).map(route => route.source).sort()).toEqual(feedback.map(node => node.id).sort())
  expect(graph.nodes.some(node => node.stage === 'judgment')).toBe(false)
})

test('replay graph excludes future ordinary tool results and preserves invocation identity', () => {
  const events = [initial[0], event(2, { case: 'toolCall', value: call }),
    event(3, { case: 'toolResult', value: { callId: call.id, name: call.name } }),
    event(4, { case: 'toolCall', value: { ...call, id: 'retry' } })]
  const workflow = workflowsFor(events)[0], frames = controlFrames(workflow.nodes)
  const graph = controlGraph(controlSnapshot(workflow.nodes, frames, 0), workflow.edges)
  expect(graph.nodes.map(node => node.stage)).toEqual(['execution'])
  expect(graph.routes).toHaveLength(0)
  const complete = controlGraph(workflow.nodes, workflow.edges)
  expect(complete.nodes.filter(node => node.stage === 'execution')).toHaveLength(2)
  expect(complete.routes.some(route => complete.nodes.find(node => node.id === route.source)?.stage === 'feedback'
    && complete.nodes.find(node => node.id === route.target)?.stage === 'execution')).toBe(true)
})

test('Reflex containers follow takeover boundaries and keep ordinary and background work outside', () => {
  const scoped = (seq: number, payload: any, segmentId = 'segment', background = false) => event(seq, {
    case: 'extension', value: anyPack(RuntimeEventSchema, create(RuntimeEventSchema, { taskId: 'task', segmentId, background, payload })),
  })
  const events = [initial[0], initial[1], scoped(3, { case: 'decisionRequest', value: { requestId: 'entry', claims } }),
    scoped(4, { case: 'decisionResult', value: { requestId: 'entry' } }), scoped(5, { case: 'takeover', value: { definition } }),
    scoped(6, { case: 'dispatch', value: { call } }), scoped(7, { case: 'result', value: { result: { callId: call.id, name: call.name } } }),
    scoped(8, { case: 'handoff', value: { reason: 'defer' } }),
    event(9, { case: 'toolCall', value: { ...call, id: 'ordinary' } }), event(10, { case: 'toolResult', value: { callId: 'ordinary', name: call.name } }),
    scoped(11, { case: 'takeover', value: { definition } }, 'another-segment'),
    scoped(12, { case: 'decisionRequest', value: { requestId: 'retry', claims } }, 'another-segment'),
    scoped(13, { case: 'decisionResult', value: { requestId: 'retry' } }, 'another-segment'),
    scoped(14, { case: 'dispatch', value: { call: { ...call, id: 'retry-call' } } }, 'another-segment'),
    scoped(15, { case: 'result', value: { result: { callId: 'retry-call', name: call.name } } }, 'another-segment'),
    scoped(16, { case: 'handoff', value: { reason: 'report' } }, 'another-segment'),
    scoped(17, { case: 'generation', value: { kind: 'claim_llm', state: 'started', requestId: 'background' } }, 'another-segment', true)]
  const workflow = workflowsFor(events)[0], graph = controlGraph(workflow.nodes, workflow.edges)
  const groups = controlReflexes(graph.nodes)
  expect(groups).toHaveLength(2)
  expect(groups.map(group => group.nodes.length)).toEqual([4, 5])
  expect(groups.every(group => group.handoff?.node.kind === 'handoff')).toBe(true)
  expect(groups.flatMap(group => group.nodes).some(card => card.node.background || card.node.events?.length)).toBe(false)
  const rows = controlReflexRows(groups[1])
  expect(rows).toHaveLength(1)
  expect(rows[0].judgments).toHaveLength(1)
  expect(rows[0].tools[0].map(card => card.stage)).toEqual(['execution', 'feedback'])
  const frames = controlFrames(workflow.nodes)
  const before = frames.findIndex(frame => frame.nodeId === groups[0].takeover.node.id) - 1
  expect(controlReflexes(controlGraph(controlSnapshot(workflow.nodes, frames, before)).nodes)).toHaveLength(0)
  const during = frames.findIndex(frame => frame.nodeId === groups[0].nodes[1].node.id && frame.stage === 'execution')
  const visible = controlReflexes(controlGraph(controlSnapshot(workflow.nodes, frames, during)).nodes)
  expect(visible).toHaveLength(1)
  expect(visible[0].handoff).toBeUndefined()
  expect(visible[0].nodes.some(card => card.stage === 'feedback')).toBe(false)
})

test('one real Playwright Reflex retains all eight judgments and five native steps', async ({ page }) => {
  const fixture = JSON.parse(readFileSync(fileURLToPath(new URL('./fixtures/jev-history/reflex-reuse-events.json', import.meta.url)), 'utf8'))
  const registry = createRegistry(RuntimeEventSchema, file_types_chat, file_types_agent, file_aop_operation_protocol, file_aop_file_protocol, file_aop_pty_protocol)
  const events = fixture.filter((event: any) => event.sessionId === 'fresh-reuse-1')
    .map((event: any) => fromJson(EventSchema, event, { registry }))
  const workflow = workflowsFor(events).find(workflow => workflow.nodes.some(node => node.kind === 'takeover'))!
  expect(workflow.nodes.filter(node => node.kind === 'decision')).toHaveLength(8)
  expect(workflow.nodes.filter(node => node.kind === 'tool')).toHaveLength(5)
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await mount(page)
  await render(page, events)
  const flow = page.getByTestId('jev-control-flow').filter({ has: page.locator('[data-kind=takeover]') })
  await expect(flow).toHaveCount(1)
  await expect(flow.locator('[data-kind=decision]')).toHaveCount(8)
  await expect(flow.locator('[data-control-stage=execution][data-kind=tool]')).toHaveCount(5)
  await expect(flow.locator('[data-control-stage=feedback]')).toHaveCount(5)
  await expect(flow.locator('[data-source-stage=feedback][data-target-stage=judgment]')).toHaveCount(5)
  const invocationIds = await flow.locator('[data-control-stage=execution]').evaluateAll(elements => elements.map(element => (element as HTMLElement).dataset.controlNode))
  expect(new Set(invocationIds).size).toBe(5)
})

for (const width of [1440, 390]) test(`Reflex is one enclosing loop with Claim and tool result columns at ${width}px`, async ({ page }, info) => {
  await page.setViewportSize({ width, height: 1000 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await mount(page)
  await render(page, [...initial, answer, dispatch, result, next, ...returned])
  const flow = page.getByTestId('jev-control-flow'), loop = flow.locator('[data-control-reflex]')
  await expect(loop).toHaveCount(1)
  await expect(loop).toHaveAttribute('data-reflex-id', definition.id)
  await expect(loop).toHaveAttribute('data-state', 'handed-off')
  await expect(loop.locator('[data-kind=decision]')).toHaveCount(2)
  await expect(loop.locator('[data-runtime-claim]')).toHaveCount(6)
  await expect(loop.locator('.control-reflex-invocation')).toHaveCount(1)
  await expect(loop.locator('.control-reflex-invocation [data-control-stage=feedback]')).toContainText('工具结果')
  await expect(loop.locator('.control-reflex-invocation')).toContainText('Current page evidence')
  await expect(loop.locator('[data-source-stage=feedback][data-target-stage=judgment][data-loop=true]')).toHaveCount(1)
  await expect(flow.locator('.control-row [data-kind=reasoning]')).toHaveCount(1)
  await expect(flow.locator('.control-row [data-kind=response]')).toHaveCount(1)
  const geometry = await loop.evaluate(element => {
    const claim = element.querySelector('[data-kind=decision]')!.getBoundingClientRect()
    const tool = element.querySelector('.control-reflex-invocation')!.getBoundingClientRect()
    const scroll = element.querySelector('.control-reflex-scroll')!
    return { claimX: claim.left, toolX: tool.left, claimY: claim.top, toolY: tool.top, scrolls: scroll.scrollWidth > scroll.clientWidth }
  })
  expect(geometry.toolX).toBeGreaterThan(geometry.claimX)
  expect(Math.abs(geometry.toolY - geometry.claimY)).toBeLessThan(2)
  expect(geometry.scrolls).toBe(width === 390)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await flow.locator('.control-viewport').evaluate(element => { element.scrollTop = 0 })
  await loop.locator('.control-reflex-scroll').evaluate(element => { element.scrollLeft = 0 })
  await page.screenshot({ path: info.outputPath(`reflex-loop-${width}.png`), fullPage: true })
  await loop.locator('.control-reflex-invocation [data-control-stage=feedback]').click()
  await expect(page.locator('[data-control-stage=feedback]')).toContainText('Current page evidence')
})

for (const width of [1440, 390]) test(`continuous claims render their changing questions and options at ${width}px`, async ({ page }, info) => {
  await page.setViewportSize({ width, height: 1000 })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await mount(page)
  const claims = [
    { question: 'Claim 1: Is the search field visible on the current page?', option: 'Fill the currently visible search field' },
    { question: 'Claim 2: Does the filled form have a current submit control?', option: 'Click the submit control found in this snapshot' },
    { question: 'Claim 3: Does the current receipt confirm this search?', option: 'Read the receipt for the submitted search' },
  ]
  const events = [initial[0], initial[2]]
  for (const [index, claim] of claims.entries()) {
    const seq = 4 + index * 4, requestId = `claim-round-${index}`, tool = { ...call, id: `claim-call-${index}` }
    events.push(trace(seq, { case: 'decisionRequest', value: { requestId, claims: {
      entry: { type: ClaimType.choice, context: claim.question, options: [claim.option, `Wait for current evidence in round ${index + 1}`] },
      ...(index === 0 ? { confidence: { type: ClaimType.noul, context: 'Is this snapshot from the current page?' } } : {}),
    } } }), trace(seq + 1, { case: 'decisionResult', value: { requestId, evaluations: {
      entry: { value: { case: 'choice', value: claim.option }, probabilities: { [claim.option]: .9, [`Wait for current evidence in round ${index + 1}`]: .1 } },
      ...(index === 0 ? { confidence: { value: { case: 'noul', value: .95 } } } : {}),
    } } }), trace(seq + 2, { case: 'dispatch', value: { call: tool } }),
    trace(seq + 3, { case: 'result', value: { result: { callId: tool.id, name: tool.name } } }))
  }
  await render(page, events.slice(0, 3))
  const flow = page.getByTestId('jev-control-flow')
  await expect(flow.locator('[data-request-id=claim-round-0]')).toContainText(claims[0].question)
  await expect(flow.locator('[data-request-id=claim-round-0]')).toHaveAttribute('data-state', 'judging')
  await expect(flow.locator('[data-runtime-claim]')).toHaveCount(2)
  await expect(flow.locator('[data-runtime-claim][data-selected]')).toHaveCount(0)
  await render(page, events.slice(3), true)
  await expect(flow.locator('[data-kind=decision]')).toHaveCount(3)
  await expect(flow.locator('[data-control-question]')).toHaveCount(4)
  await expect(flow.locator('[data-control-stage=execution]')).toHaveCount(3)
  await expect(flow.locator('[data-source-stage=feedback][data-target-stage=judgment]')).toHaveCount(2)
  await expect(flow.locator('[data-control-stage=return]')).toHaveCount(0)
  await expect(flow.locator('[data-runtime-claim]')).toHaveCount(6)
  await expect(flow.locator('[data-runtime-claim][data-selected=true]')).toHaveCount(3)
  for (const [index, claim] of claims.entries()) {
    const judgment = flow.locator(`[data-request-id=claim-round-${index}]`)
    await expect(judgment.locator('[data-control-question=entry] > p').first()).toHaveText(claim.question)
    await expect(judgment.locator('[data-control-question=entry] [data-selected=true] > div:first-child > span:first-child')).toHaveText(claim.option)
    await expect(judgment.locator('.jev-option').filter({ hasText: `Wait for current evidence in round ${index + 1}` })).toContainText(`Wait for current evidence in round ${index + 1}`)
  }
  await flow.locator('[data-request-id=claim-round-2]').scrollIntoViewIfNeeded()
  await page.screenshot({ path: info.outputPath(`continuous-claims-flow-${width}.png`) })
  await flow.locator('[data-request-id=claim-round-2] > div').first().click()
  const detail = page.locator('[data-control-current=true]')
  await expect(detail).toContainText(claims[2].question)
  await expect(detail.locator('.jev-option').filter({ hasText: claims[2].option })).toContainText(claims[2].option)
  const judgments = page.locator('[data-control-node][data-kind=decision]')
  await expect(judgments).toHaveCount(3)
  for (const [index, claim] of claims.entries()) await expect(judgments.nth(index)).toContainText(claim.question)
  await expect(page.locator('[data-control-question]')).toHaveCount(4)
  await expect(page.locator('[data-runtime-claim]')).toHaveCount(6)
  await expect(page.locator('[data-runtime-claim][data-selected=true]')).toHaveCount(3)
  const slider = page.getByRole('slider', { name: '执行回放进度' })
  await slider.focus()
  await slider.press('Home')
  await slider.press('ArrowRight')
  await expect(page.locator('[data-runtime-claim]')).toHaveCount(2)
  await expect(page.locator('[data-runtime-claim][data-selected]')).toHaveCount(0)
  await slider.press('ArrowRight')
  await expect(page.locator('[data-runtime-claim][data-selected=true]')).toHaveCount(1)
  await slider.press('End')
  await judgments.last().scrollIntoViewIfNeeded()
  await page.screenshot({ path: info.outputPath(`continuous-claims-${width}.png`) })
})

test('decision-only and background-only records do not invent model, tools, feedback or handoff', async ({ page }) => {
  await mount(page)
  await render(page, [initial[0], initial[3], answer])
  const flow = page.getByTestId('jev-control-flow')
  await expect(flow.locator('[data-control-anchor]')).toHaveCount(1)
  await expect(flow.locator('[data-kind=decision]')).toHaveCount(1)
  await expect(flow.locator('[data-control-route]')).toHaveCount(0)
  await render(page, [initial[0], trace(3, { case: 'decisionRequest', value: { requestId: 'compile', claims } }, true)])
  await expect(flow.locator('[data-control-anchor]')).toHaveCount(1)
  await expect(flow.locator('[data-control-stage=background]')).toHaveCount(1)
  await expect(flow.locator('[data-control-route]')).toHaveCount(0)
})

test('repeated calls and more than four executors retain every real step without a fixed pipeline', async ({ page }) => {
  await mount(page)
  const events = [initial[0]]
  for (let i = 0; i < 6; i++) {
    const tool = { ...call, id: `call-${i}`, name: i < 2 ? 'bash' : `reader-${i}` }
    events.push(event(2 + i * 2, { case: 'toolCall', value: tool }),
      event(3 + i * 2, { case: 'toolResult', value: { callId: tool.id, name: tool.name, isError: i === 0 } }))
  }
  await render(page, events)
  const flow = page.getByTestId('jev-control-flow')
  await expect(flow.locator('[data-control-stage=execution]')).toHaveCount(6)
  await expect(flow.locator('[data-control-stage=feedback]')).toHaveCount(6)
  await expect(flow.locator('[data-control-stage=model], [data-control-stage=judgment], [data-control-stage=return]')).toHaveCount(0)
  await expect(flow.locator('[data-source-stage=feedback][data-target-stage=execution]')).toHaveCount(5)
  await expect(flow.locator('[data-control-stage=feedback][data-state=failed]')).toHaveCount(1)
})

for (const width of [390, 1440]) test(`six parallel executors remain readable within the diagram at ${width}px`, async ({ page }) => {
  await page.setViewportSize({ width, height: 1000 })
  await mount(page)
  const tools = Array.from({ length: 6 }, (_, index) => event(index + 3, { case: 'toolCall', value: { ...call, id: `parallel-${index}`, name: `reader-${index}` } }))
  await render(page, [initial[0], initial[1], ...tools])
  const flow = page.getByTestId('jev-control-flow')
  await expect(flow.locator('[data-control-stage=execution]')).toHaveCount(6)
  await expect(flow.locator('[data-source-stage=model][data-target-stage=execution]')).toHaveCount(6)
  expect(await flow.locator('.control-viewport').evaluate(element => element.scrollWidth <= element.clientWidth + 1)).toBe(true)
  for (const card of await flow.locator('[data-control-stage=execution]').all()) {
    expect(await card.evaluate(element => element.getBoundingClientRect().width)).toBeGreaterThan(200)
  }
})

test('actual events move control through judgments, native execution, feedback and model return', async ({ page }, info) => {
  await page.setViewportSize({ width: 1440, height: 1100 })
  await mount(page)
  await render(page, initial)
  const flow = page.getByTestId('jev-control-flow')
  await expect(flow).toHaveAttribute('data-stage', 'judgment')
  await expect(flow.locator('[data-control-question]')).toHaveCount(2)
  await expect(flow.locator('[data-control-question][data-choice]')).toHaveCount(0)
  await expect(flow.locator('[data-control-route][data-target-stage=judgment][data-active=true] .control-particle')).toBeVisible()
  await render(page, [answer, dispatch], true)
  await expect(flow).toHaveAttribute('data-stage', 'execution')
  await expect(flow.locator('[data-control-question=next]')).toHaveAttribute('data-choice', 'read')
  await expect(flow.locator('[data-source-stage=judgment][data-target-stage=execution]')).toHaveAttribute('data-active', 'true')
  await expect(flow.locator('[data-control-stage=execution]')).toHaveAttribute('data-state', 'pending')
  await render(page, [result], true)
  await expect(flow).toHaveAttribute('data-stage', 'feedback')
  await expect(flow.locator('[data-source-stage=execution][data-target-stage=feedback]')).toHaveAttribute('data-active', 'true')
  await expect(flow.locator('[data-control-stage=feedback]')).toContainText('Current page evidence')
  await render(page, [next], true)
  await expect(flow.locator('[data-source-stage=feedback][data-target-stage=judgment]')).toHaveAttribute('data-active', 'true')
  await expect(flow.locator('[data-source-stage=feedback][data-target-stage=judgment] .control-particle')).toBeVisible()
  await render(page, returned, true)
  await page.locator('.workflow-follow').click()
  await expect(flow).toHaveAttribute('data-stage', 'return')
  await expect(page.getByTestId('workflow-detail')).toHaveCount(0)
  await expect(page.getByTestId('assistant-response-content')).toContainText('Verified against recorded documentation')
  await expect(flow).not.toContainText('Verified against recorded documentation')
  await page.screenshot({ path: info.outputPath('dynamic-control-flow.png'), fullPage: true })
})

test('scrubbing and replay reveal only evidence available at that event', async ({ page }) => {
  await mount(page)
  await render(page, [...initial, answer, dispatch, result, next, ...returned])
  const progress = page.getByRole('slider', { name: '执行回放进度' })
  const seek = async (index: number) => { await progress.focus(); await progress.press('Home'); for (let i = 0; i < index; i++) await progress.press('ArrowRight') }
  await seek(2) // Request: its result is not visible yet.
  await expect(page.getByTestId('jev-decision')).toHaveAttribute('data-state', 'judging')
  await expect(page.locator('[data-control-question][data-choice]')).toHaveCount(0)
  await expect(page.locator('[data-control-stage=execution]')).toHaveCount(0)
  await seek(3) // Answer: choices become visible.
  await expect(page.getByTestId('jev-decision')).toHaveAttribute('data-state', 'answered')
  await expect(page.locator('[data-control-question=next]')).toHaveAttribute('data-choice', 'read')
  await seek(4) // Dispatch: no result from the following frame.
  await expect(page.getByTestId('jev-control-flow')).not.toContainText('Current page evidence')
  await expect(page.locator('[data-control-stage=execution]')).toHaveAttribute('data-state', 'pending')
  await seek(5)
  await expect(page.locator('[data-control-stage=feedback]')).toContainText('Current page evidence')
  const paused = await progress.inputValue()
  await page.waitForTimeout(950)
  await expect(progress).toHaveValue(paused)
  await page.getByRole('button', { name: '返回当前', exact: true }).click()
  await expect(page.getByTestId('jev-control-flow')).toHaveAttribute('data-replaying', 'false')
  await expect(page.getByTestId('workflow-detail')).toHaveCount(0)
  await expect(page.getByTestId('assistant-response-content')).toContainText('Verified against recorded documentation')
  await expect(page.getByTestId('agent-workflow')).toHaveAttribute('data-animating', 'true')
  const resumed = await progress.inputValue()
  await expect.poll(() => progress.inputValue()).not.toBe(resumed)
})

for (const width of [390, 1440]) for (const theme of ['light', 'dark']) test(`dynamic view stays inside its container ${width} ${theme}`, async ({ page }, info) => {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  await page.setViewportSize({ width, height: 1100 })
  await mount(page)
  await page.evaluate(theme => document.documentElement.classList.toggle('dark', theme === 'dark'), theme)
  await render(page, [...initial, answer, dispatch])
  await expect(page.getByTestId('jev-control-flow')).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  expect(await page.getByTestId('jev-control-flow').evaluate(element => element.scrollWidth <= element.clientWidth + 1)).toBe(true)
  await page.screenshot({ path: info.outputPath(`control-flow-${width}-${theme}.png`), fullPage: true })
  await page.emulateMedia({ reducedMotion: 'reduce' })
  for (const particle of await page.locator('.control-particle').all()) await expect(particle).toBeHidden()
  await expect(page.locator('[data-control-stage=execution]')).toHaveAttribute('data-state', 'pending')
  expect(errors).toEqual([])
})

test.describe('shareable motion preview', () => {
  test('record the complete control loop replay', async ({ page }, info) => {
    await page.setViewportSize({ width: 1440, height: 1000 })
    await mount(page)
    await page.evaluate(() => document.documentElement.classList.add('dark'))
    await render(page, [...initial, answer, dispatch, result, next, ...returned])
    await expect(page.getByTestId('agent-workflow')).toHaveAttribute('data-animating', 'true')
    await expect(page.locator('.control-play')).toHaveCount(0)
    const progress = page.getByRole('slider', { name: '执行回放进度' })
    const final = await progress.getAttribute('max')
    await expect(progress).toHaveValue(final!, { timeout: 15_000 })
    await expect(page.getByTestId('jev-control-flow')).toHaveAttribute('data-stage', 'return')
    await expect(page.getByTestId('workflow-detail')).toHaveCount(0)
    await expect(page.getByTestId('assistant-response-content')).toContainText('Verified against recorded documentation')
    await page.screenshot({ path: info.outputPath('replay-completed.png'), fullPage: true })
    await expect(progress).toHaveValue('0', { timeout: 4000 })
    await expect(page.getByTestId('agent-workflow')).toHaveAttribute('data-animating', 'true')
  })
})

test('ordinary tool calls use the model path and background decisions do not control execution', async ({ page }) => {
  await mount(page)
  await render(page, [initial[0], initial[1], event(3, { case: 'toolCall', value: call })])
  await expect(page.locator('[data-source-stage=model][data-target-stage=execution]')).toHaveAttribute('data-active', 'true')
  await expect(page.locator('[data-source-stage=judgment][data-target-stage=execution]')).toHaveCount(0)
  await expect(page.locator('[data-control-stage=judgment]')).toHaveCount(0)
  await render(page, [event(4, { case: 'toolResult', value: { callId: call.id, name: call.name } }), event(5, { case: 'turnEnded', value: {} }),
    trace(6, { case: 'decisionRequest', value: { requestId: 'background', claims } }, true)], true)
  await expect(page.getByTestId('jev-control-flow')).toHaveAttribute('data-stage', 'background')
  await expect(page.locator('[data-source-stage=background][data-target-stage=execution]')).toHaveCount(0)
})

test('parallel tools retain their own arrival route while sibling calls are still running', async ({ page }) => {
  await mount(page)
  const other = { ...call, id: 'http', name: 'http-reader' }
  await render(page, [initial[0], initial[1], event(3, { case: 'toolCall', value: call }), event(4, { case: 'toolCall', value: other })])
  await expect(page.locator('[data-source-stage=model][data-target-stage=execution][data-active=true]')).toHaveCount(2)
  await render(page, [event(5, { case: 'toolResult', value: { callId: 'http', name: 'http-reader' } })], true)
  // Inspect the arrival while bash remains pending, using the real replay frame.
  const progress = page.getByRole('slider', { name: '执行回放进度' })
  await progress.focus(); await progress.press('End')
  await expect(page.getByTestId('jev-control-flow')).toHaveAttribute('data-stage', 'feedback')
  await expect(page.locator('[data-source-stage=execution][data-target-stage=feedback]')).toHaveCount(1)
  await expect(page.locator('[data-source-stage=execution][data-target-stage=feedback]')).toHaveAttribute('data-active', 'true')
  await expect(page.locator('[data-control-stage=execution][data-state=pending]')).toHaveCount(1)
})

test('retained real execution drives native results and finite choices in the dynamic view', async ({ page }, info) => {
  const path = process.env.JEV_EVENTS_FILE || fileURLToPath(new URL('./fixtures/jev-history/live-events.json', import.meta.url))
  const registry = createRegistry(RuntimeEventSchema, file_types_chat, file_types_agent, file_aop_operation_protocol, file_aop_file_protocol, file_aop_pty_protocol)
  const events = JSON.parse(readFileSync(path!, 'utf8')).map((delivery: any) => fromJson(EventSchema, delivery.event, { registry }))
  const items = withWorkflows(withJEV(reduceAOPToTimeline(jevTimelineEvents(events), { responseBoundary: isJEVBoundary }), projectJEV(events)), events)
  const workflows = items.filter(item => item.kind === 'extension' && item.extensionType === 'workflow').map(item => (item as any).data.workflow as WorkflowTurn)
  await page.setViewportSize({ width: 1440, height: 1100 })
  await mount(page)
  await render(page, [...events, ...events])
  await expect(page.getByTestId('jev-control-flow')).toHaveCount(workflows.length)
  for (const workflow of workflows) {
    const index = workflows.indexOf(workflow), flow = page.getByTestId('jev-control-flow').nth(index)
    const frames = controlFrames(workflow.nodes)
    const resultIndex = frames.findIndex(frame => frame.stage === 'feedback' && frame.state === 'completed')
    if (resultIndex >= 0) {
      const progress = page.getByTestId('agent-workflow').nth(index).getByRole('slider')
      await progress.focus()
      await progress.press('Home')
      for (let i = 0; i < resultIndex; i++) await progress.press('ArrowRight')
      await expect(flow).toHaveAttribute('data-stage', 'feedback')
      await expect(flow.locator('[data-control-stage=feedback]')).not.toContainText('尚未记录')
    }
  }
  await page.screenshot({ path: info.outputPath('real-dynamic-flow.png'), fullPage: true })
})

test('one unified graph ignores old view preferences and needs no view or detail controls', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('cyber-workflow-view', 'records'))
  await mount(page)
  await render(page, [...initial, answer, dispatch, result, next, ...returned])
  await expect(page.getByTestId('jev-control-flow')).toBeVisible()
  await expect(page.locator('.workflow-render-toggle, .workflow-inspect, .workflow-navigation, .workflow-board')).toHaveCount(0)
  await expect(page.getByTestId('agent-workflow')).toHaveAttribute('data-animating', 'true')
  await expect(page.getByTestId('jev-decision')).toHaveCount(2)
  await expect(page.getByTestId('workflow-tool-arguments')).toHaveCount(1)
  await expect(page.getByTestId('workflow-tool-result')).toHaveCount(1)
  await expect(page.getByTestId('workflow-detail')).toHaveCount(0)
})

test('reduced motion keeps the complete recorded diagram static without playback buttons', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await mount(page)
  await render(page, [...initial, answer, dispatch, result, next, ...returned])
  await expect(page.getByTestId('jev-control-flow')).toHaveAttribute('data-replaying', 'false')
  await expect(page.getByTestId('agent-workflow')).toHaveAttribute('data-animating', 'false')
  await expect(page.getByRole('button', { name: /播放执行回放|暂停执行回放/ })).toHaveCount(0)
  await page.waitForTimeout(1000)
  await expect(page.getByTestId('jev-control-flow')).toHaveAttribute('data-replaying', 'false')
})

for (const width of [390, 1440]) test(`loop animation preserves manual scrolling and layout at ${width}px`, async ({ page }) => {
  await page.setViewportSize({ width, height: 1000 })
  await mount(page)
  await render(page, [...initial, answer, dispatch, result, next, ...returned])
  const workflow = page.getByTestId('agent-workflow')
  const viewport = workflow.locator('.control-viewport')
  await viewport.scrollIntoViewIfNeeded()
  await viewport.evaluate(element => { element.scrollTop = 150 })
  await viewport.hover()
  await page.mouse.wheel(0, 50)
  await expect(workflow).toHaveAttribute('data-following', 'false')
  const horizontal = workflow.locator('.control-reflex-scroll')
  if (width === 390) {
    await horizontal.evaluate(element => { element.scrollLeft = 60 })
    await expect.poll(() => horizontal.evaluate(element => element.scrollLeft)).toBeGreaterThan(0)
  }
  await expect(workflow).toHaveAttribute('data-animating', 'true')
  await expect(page.getByRole('button', { name: /播放执行回放|暂停执行回放/ })).toHaveCount(0)
  // Exercise every frame, including the last-to-first wrap, without waiting in real time.
  await page.clock.install()
  await page.clock.pauseAt(new Date())
  const geometry = () => workflow.evaluate(element => {
    const viewport = element.querySelector('.control-viewport')!
    const horizontal = element.querySelector('.control-reflex-scroll') || viewport
    const conversation = element.closest('.overflow-y-auto')!
    return { top: viewport.scrollTop, left: horizontal.scrollLeft, height: viewport.scrollHeight,
      workflowHeight: element.getBoundingClientRect().height, conversationTop: conversation.scrollTop }
  })
  const before = await geometry()
  const progress = workflow.getByRole('slider')
  const last = Number(await progress.getAttribute('max'))
  const visited = new Set<number>()
  for (let i = 0; i <= last + 1; i++) {
    const cursor = Number(await progress.inputValue())
    visited.add(cursor)
    await page.clock.runFor(cursor === last ? 1700 : 900)
    await expect.poll(() => progress.inputValue()).not.toBe(String(cursor))
    expect(await geometry()).toEqual(before)
  }
  expect(visited.has(0)).toBe(true)
  expect(visited.has(last)).toBe(true)
  await expect(workflow).toHaveAttribute('data-animating', 'true')
})

test('live workflow follows only until user scrolls and resumes on explicit follow', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1000 })
  await mount(page)
  await render(page, scrollingLiveEvents())
  const workflow = page.getByTestId('agent-workflow')
  const viewport = workflow.locator('.control-viewport')
  await expect.poll(() => viewport.evaluate(element => element.scrollTop)).toBeGreaterThan(300)
  await viewport.hover()
  await page.mouse.wheel(0, -250)
  await expect(workflow).toHaveAttribute('data-following', 'false')
  await page.waitForTimeout(150) // Wait for the wheel gesture to finish.
  await viewport.evaluate(element => { element.scrollTop = 100 })
  const top = await viewport.evaluate(element => element.scrollTop)
  await render(page, [trace(19, { case: 'decisionResult', value: { requestId: 'last', evaluations: { next: { value: { case: 'choice', value: 'read' } } } } }),
    trace(20, { case: 'dispatch', value: { call: { ...call, id: 'new-call' } } })], true)
  await expect(workflow.locator('.workflow-follow')).toHaveAttribute('aria-pressed', 'false')
  expect(await viewport.evaluate(element => element.scrollTop)).toBe(top)
  await workflow.locator('.workflow-follow').click()
  await expect(workflow).toHaveAttribute('data-following', 'true')
  await expect.poll(() => viewport.evaluate(element => element.scrollTop)).toBeGreaterThan(top)
  await render(page, [trace(21, { case: 'result', value: { result: { callId: 'new-call', name: call.name } } }),
    trace(22, { case: 'decisionRequest', value: { requestId: 'again', claims } })], true)
  await expect(workflow.locator('.workflow-follow')).toHaveAttribute('aria-pressed', 'true')
  await expect(workflow).toHaveAttribute('data-animating', 'false')
})

for (const gesture of ['keyboard', 'touch', 'scrollbar']) test(`${gesture} scrolling detaches live following without treating automatic positioning as manual`, async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1000 })
  await mount(page)
  await render(page, scrollingLiveEvents())
  const workflow = page.getByTestId('agent-workflow')
  const viewport = workflow.locator('.control-viewport')
  await expect.poll(() => viewport.evaluate(element => element.scrollTop)).toBeGreaterThan(300)
  await expect(workflow).toHaveAttribute('data-following', 'true')
  if (gesture === 'keyboard') {
    await viewport.focus()
    await viewport.press('PageUp')
  } else if (gesture === 'touch') {
    await viewport.dispatchEvent('touchmove', { touches: [{ identifier: 0, clientX: 300, clientY: 300 }] })
  } else {
    await page.addStyleTag({ content: '.control-viewport::-webkit-scrollbar { width: 16px; } .control-viewport::-webkit-scrollbar-thumb { background: #777; }' })
    const bounds = await viewport.boundingBox()
    expect(bounds).not.toBeNull()
    // Click the track above its current thumb, as a real native scrollbar action.
    await page.mouse.click(bounds!.x + bounds!.width - 6, bounds!.y + 60)
  }
  await expect(workflow).toHaveAttribute('data-following', 'false')
  await workflow.locator('.workflow-follow').click()
  await expect(workflow).toHaveAttribute('data-following', 'true')
  await page.waitForTimeout(150)
  await expect(workflow).toHaveAttribute('data-following', 'true')
})

test('clicking any card seeks its own breakpoint and leaving focus resumes the loop', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 1000 })
  await mount(page)
  const events = [...initial, answer, dispatch, result, next, ...returned]
  await render(page, events)
  const workflow = page.getByTestId('agent-workflow')
  const progress = workflow.getByRole('slider')
  const frames = controlFrames(workflowsFor(events)[0].nodes)
  const cards = workflow.locator('[data-control-node]')
  // The diagram retains all cards while focused, so every real breakpoint remains reachable.
  const count = await cards.count()
  for (let i = 0; i < count; i++) {
    const card = cards.nth(i)
    const nodeId = await card.getAttribute('data-control-node')
    const stage = await card.getAttribute('data-control-stage')
    const candidates = frames.filter(frame => frame.nodeId === nodeId && frame.stage === stage)
    const breakpoint = candidates[candidates.length - 1]
    expect(breakpoint).toBeDefined()
    if (await card.getAttribute('data-kind') === 'decision') {
      // The body of a judgment card, including its Claims, is clickable too.
      await card.locator('[data-runtime-claim]').first().click()
    } else await card.click()
    await expect(progress).toHaveValue(String(frames.indexOf(breakpoint)))
    await expect(workflow).toHaveAttribute('data-animating', 'false')
    await expect(cards).toHaveCount(count)
    if (stage === 'execution' && nodeId === frames.find(frame => frame.eventId === dispatch.id)?.nodeId)
      await expect(card).not.toContainText('Current page evidence')
    if (i === 0) {
      await page.waitForTimeout(1000)
      await expect(progress).toHaveValue(String(frames.indexOf(breakpoint)))
    }
    // A focusable region outside the card resumes from this exact frame.
    await workflow.locator('.control-viewport').focus()
    await expect(workflow).toHaveAttribute('data-animating', 'true')
    await expect(workflow.getByTestId('workflow-detail')).toHaveCount(0)
    await expect(progress).toHaveValue(String(frames.indexOf(breakpoint)))
    const viewport = workflow.locator('.control-viewport')
    const top = await viewport.evaluate(element => element.scrollTop)
    await expect.poll(() => progress.inputValue()).not.toBe(String(frames.indexOf(breakpoint)))
    expect(await viewport.evaluate(element => element.scrollTop)).toBe(top)
  }
})
