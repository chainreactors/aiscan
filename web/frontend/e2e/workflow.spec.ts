import { ClaimType } from '../src/gen/decision/claim_pb'
import { test, expect, type Page } from '@playwright/test'
import { showExecutionLanes } from './jev-helpers'
import { create, toBinary } from '@bufbuild/protobuf'
import { anyPack, timestampFromDate } from '@bufbuild/protobuf/wkt'
import { EventSchema } from '../cyber-ui/packages/aop/src/gen/aop/event_pb'
import { RuntimeEventSchema } from '../src/gen/types/jev_pb'
import { reduceAOPToTimeline } from '../cyber-ui/packages/viewer/src/lib/aop-reducer'
import { projectJEV, withJEV } from '../src/lib/jev-view'
import { withWorkflows, type WorkflowTurn } from '../src/lib/workflow-view'

function event(seq: number, payload: any, sessionId = 'session-1', turnId = 'turn-1') {
  return create(EventSchema, { id: `${sessionId}-${turnId}-${seq}`, seq: BigInt(seq), sessionId, turnId, emitter: 'agent',
    emittedAt: timestampFromDate(new Date(1700000000000 + seq * 1000)), payload })
}
function trace(seq: number, payload: any, background = false, session = 'session-1', turn = 'turn-1') {
  return event(seq, { case: 'extension', value: anyPack(RuntimeEventSchema, create(RuntimeEventSchema, {
    taskId: 'task', segmentId: background ? '' : 'loop', step: 1, background, payload,
  })) }, session, turn)
}
const question = { type: ClaimType.choice, context: "Choose the current operation." + "\nread: Read evidence\ndefer: More reasoning", options: ["read","defer"] }
const call = { id: 'call', name: 'read-evidence', arguments: { data: new TextEncoder().encode('{"target":"current"}') } }
const definition = { id: 'read-loop', when: 'Read current evidence', decide: 'Choose a supplied operation', observe: 'js:function(){return {report:1}}' }
const project = (events: ReturnType<typeof event>[]) => withWorkflows(withJEV(reduceAOPToTimeline(events), projectJEV(events)), events)
const turns = (events: ReturnType<typeof event>[]) => project(events).filter(item => item.kind === 'extension' && item.extensionType === 'workflow').map(item => (item as any).data.workflow as WorkflowTurn)
async function mount(page: Page) {
  await page.route('**/cyber.rpc.chat.SessionService/ListCommands', route => route.fulfill({ contentType: 'application/json', body: '{"commands":[]}' }))
  await page.addInitScript(() => localStorage.setItem('cyber-locale', 'zh'))
  await page.goto('/e2e/fixtures/jev.html')
  await page.waitForFunction(() => typeof (window as any).renderJEVEvents === 'function')
  await showExecutionLanes(page)
}
async function render(page: Page, events: ReturnType<typeof event>[], append = false) {
  await page.evaluate(({ values, append }) => (window as any).renderJEVEvents(values, append), { values: events.map(e => [...toBinary(EventSchema, e)]), append })
  await showExecutionLanes(page)
}

test('one turn owns ordinary tools and JEV calls, even with duplicated native and runtime deliveries', () => {
  const events = [event(1, { case: 'turnStarted', value: {} }), event(2, { case: 'toolCall', value: { ...call, id: 'ordinary' } }),
    event(3, { case: 'toolResult', value: { callId: 'ordinary', name: call.name } }), trace(4, { case: 'takeover', value: { definition } }),
    trace(5, { case: 'dispatch', value: { call } }), event(6, { case: 'toolCall', value: call }),
    trace(7, { case: 'result', value: { result: { callId: call.id, name: call.name } } }), event(8, { case: 'toolResult', value: { callId: call.id, name: call.name } }),
    trace(9, { case: 'handoff', value: { reason: 'report' } }), event(10, { case: 'message', value: { id: 'reply', role: 'assistant', content: [{ value: { case: 'text', value: { text: 'Actual answer' } } }] } }),
    event(11, { case: 'turnEnded', value: { stopReason: 'completed' } })]
  const workflows = turns([...events, ...events])
  expect(workflows).toHaveLength(1)
  expect(workflows[0].nodes.filter(node => node.kind === 'tool')).toHaveLength(2)
  expect(new Set(workflows[0].nodes.map(node => node.id)).size).toBe(workflows[0].nodes.length)
  expect(workflows[0].nodes[workflows[0].nodes.length - 1].kind).toBe('response')
})

test('reused request and call IDs stay separate across sessions and turns', () => {
  const events = ['a', 'b'].flatMap((session, index) => ['one', 'two'].flatMap((turn, turnIndex) => {
    const n = index * 20 + turnIndex * 10
    return [event(n + 1, { case: 'turnStarted', value: {} }, session, turn), trace(n + 2, { case: 'takeover', value: { definition } }, false, session, turn),
      trace(n + 3, { case: 'decisionRequest', value: { requestId: 'same', claims: { next: question } } }, false, session, turn),
      trace(n + 4, { case: 'decisionResult', value: { requestId: 'same', evaluations: { next: { value: { case: 'choice', value: 'read' } } } } }, false, session, turn),
      trace(n + 5, { case: 'dispatch', value: { call } }, false, session, turn), trace(n + 6, { case: 'result', value: { result: { callId: call.id, name: call.name } } }, false, session, turn)]
  }))
  const workflows = turns(events)
  expect(workflows).toHaveLength(4)
  for (const workflow of workflows) {
    expect(workflow.nodes.filter(node => node.kind === 'decision')).toHaveLength(1)
    expect(workflow.nodes.filter(node => node.kind === 'tool')).toHaveLength(1)
    expect(workflow.nodes.find(node => node.kind === 'decision')?.state).toBe('completed')
  }
})

test('delegated execution branches once and background events stay in their original completed turn', () => {
  const events = [event(1, { case: 'turnStarted', value: {} }), event(2, { case: 'toolCall', value: { id: 'delegate', name: 'subagent' } }),
    event(3, { case: 'sessionStarted', value: { parentSessionId: 'session-1', parentToolCallId: 'delegate', agentName: 'worker' } }, 'child', ''),
    event(4, { case: 'turnStarted', value: {} }, 'child', 'child-turn'), event(5, { case: 'toolCall', value: call }, 'child', 'child-turn'),
    event(6, { case: 'toolResult', value: { callId: call.id, name: call.name } }, 'child', 'child-turn'),
    event(7, { case: 'sessionEnded', value: { reason: 'completed' } }, 'child', 'child-turn'), event(8, { case: 'toolResult', value: { callId: 'delegate', name: 'subagent' } }),
    event(9, { case: 'turnEnded', value: { stopReason: 'completed' } }), trace(10, { case: 'generation', value: { kind: 'claim_llm', state: 'started', requestId: 'background' } }, true)]
  // ChatPanel already folds child content into a subagent_run entry.
  const root = reduceAOPToTimeline(events.filter(e => e.sessionId === 'session-1'))
  const child = reduceAOPToTimeline(events.filter(e => e.sessionId === 'child'))
  const items = withJEV([...root, { id: 'child', kind: 'subagent_run' as const, name: 'worker', prompt: 'Read evidence', timestamp: 1700000003000,
    sessionID: 'child', status: 'completed' as const, items: child }], projectJEV(events))
  const workflow = (withWorkflows(items, events).find(item => item.kind === 'extension' && item.extensionType === 'workflow') as any).data.workflow as WorkflowTurn
  expect(workflow.live).toBe(false)
  expect(workflow.nodes.filter(node => node.sessionId === 'child' && node.kind === 'tool')).toHaveLength(1)
  expect(workflow.edges.some(edge => workflow.nodes.find(node => node.id === edge.target)?.sessionId === 'child')).toBe(true)
  expect(workflow.nodes.find(node => node.background)?.state).toBe('pending')
})

test('live decision and tool results update the same nodes and animate only actual routes', async ({ page }, info) => {
  await mount(page)
  const initial = [event(1, { case: 'turnStarted', value: {} }), trace(2, { case: 'takeover', value: { definition } }),
    trace(3, { case: 'decisionRequest', value: { requestId: 'next', claims: { next: question } } })]
  await render(page, initial)
  const decision = page.locator('[data-workflow-node][data-kind=decision]')
  await expect(decision).toHaveAttribute('data-state', 'pending')
  const id = await decision.getAttribute('data-workflow-node')
  await expect(page.locator('.workflow-packet')).toHaveCount(1)
  await render(page, [trace(4, { case: 'decisionResult', value: { requestId: 'next', evaluations: { next: { value: { case: 'choice', value: 'read' }, probabilities: { read: .95, defer: .05 } } } } }),
    trace(5, { case: 'dispatch', value: { call } })], true)
  await expect(decision).toHaveAttribute('data-workflow-node', id!)
  await expect(decision).toHaveAttribute('data-state', 'completed')
  const tool = page.locator('[data-workflow-node][data-kind=tool]')
  await expect(tool).toHaveCount(1)
  await render(page, [trace(6, { case: 'result', value: { result: { callId: call.id, name: call.name, output: [{ value: { case: 'text', value: { text: 'Unique result evidence' } } }] } } })], true)
  await expect(tool).toHaveCount(1)
  await tool.click()
  await expect(page.getByTestId('workflow-detail')).toContainText('Unique result evidence')
  await expect(page.getByText('Unique result evidence', { exact: true })).toHaveCount(1)
  await render(page, initial, true)
  await expect(page.getByTestId('agent-workflow')).toHaveCount(1)
  await expect(page.getByTestId('workflow-detail')).toHaveCount(1)
  await page.screenshot({ path: info.outputPath('live-workflow.png'), fullPage: true })
})

for (const width of [390, 1440]) for (const theme of ['light', 'dark']) test(`workflow, single inspector and keyboard navigation ${width} ${theme}`, async ({ page }, info) => {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  await page.setViewportSize({ width, height: 900 })
  await mount(page)
  await page.evaluate(theme => document.documentElement.classList.toggle('dark', theme === 'dark'), theme)
  await expect(page.getByTestId('agent-workflow')).toHaveCount(1)
  await expect(page.locator('[data-workflow-node][data-kind=tool]')).toHaveCount(2)
  const decision = page.locator('[data-workflow-node][data-kind=decision]')
  await decision.focus()
  await page.keyboard.press('Enter')
  await expect(decision).toHaveAttribute('aria-pressed', 'true')
  await expect(page.locator('[data-selected=true]')).toHaveCount(1)
  await expect(page.getByTestId('workflow-detail')).toHaveCount(1)
  await expect(page.getByTestId('jev-segment')).toHaveCount(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: info.outputPath(`workflow-${width}-${theme}.png`), fullPage: true })
  expect(errors).toEqual([])
})

test('reduced motion disables packets while preserving pending status and feedback', async ({ page }) => {
  await mount(page)
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await render(page, [event(1, { case: 'turnStarted', value: {} }), trace(2, { case: 'observation', value: { stateJson: '{}' } }),
    trace(3, { case: 'decisionRequest', value: { requestId: 'pending', claims: { next: question } } })])
  await expect(page.locator('[data-workflow-node][data-kind=decision]')).toHaveAttribute('data-state', 'pending')
  for (const packet of await page.locator('.workflow-packet').all()) await expect(packet).toBeHidden()
  expect(await page.locator('.workflow-wire.is-active').evaluate(el => getComputedStyle(el).animationName)).toBe('none')
})

for (const width of [390, 1440]) test(`recorded order, summaries and scoped navigation at ${width}px`, async ({ page }) => {
  await page.setViewportSize({ width, height: 900 })
  await mount(page)
  const nodes = page.locator('[data-workflow-node]')
  const geometry = await nodes.evaluateAll(elements => elements.map(element => {
    const rect = element.getBoundingClientRect()
    return { top: rect.top, left: rect.left }
  }))
  for (let i = 1; i < geometry.length; i++) {
    expect(geometry[i].top).toBeGreaterThan(geometry[i - 1].top)
  }
  expect(await page.locator('.workflow-viewport').evaluate(element => element.scrollWidth <= element.clientWidth + 1)).toBe(true)
  await expect(page.locator('[data-kind=tool] .workflow-node-description').first()).toContainText('playwright open')
  await expect(page.locator('[data-kind=decision] .workflow-node-description')).toContainText('inspect')
  await page.getByRole('button', { name: '前台执行 6', exact: true }).click()
  await expect(nodes).toHaveCount(6)
  await expect(page.locator('[data-background=true][data-workflow-node]')).toHaveCount(0)
  await nodes.first().focus()
  await page.keyboard.press('ArrowDown')
  await expect(nodes.nth(1)).toBeFocused()
  await expect(nodes.nth(1)).toHaveAttribute('aria-pressed', 'true')
  await page.getByRole('button', { name: '下一步', exact: true }).click()
  await expect(nodes.nth(2)).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByTestId('workflow-detail')).toContainText('playwright open')
  await page.getByRole('button', { name: '后台归纳 1', exact: true }).click()
  await expect(nodes).toHaveCount(1)
  await expect(page.getByTestId('jev-reflex-definition')).toBeVisible()
  await expect(page.getByRole('button', { name: '上一步', exact: true })).toBeDisabled()
  await expect(page.getByRole('button', { name: '下一步', exact: true })).toBeDisabled()
})

test('Markdown replies stay outside the diagram and details open only on inspection', async ({ page }) => {
  await mount(page)
  const reply = page.getByTestId('assistant-response-content')
  await expect(reply).toHaveCount(1)
  await expect(page.getByTestId('agent-workflow').getByTestId('assistant-response-content')).toHaveCount(0)
  await expect(page.getByTestId('workflow-detail')).toHaveCount(0)
  const decision = page.locator('[data-workflow-node][data-kind=decision]')
  await decision.click()
  await expect(page.getByTestId('workflow-detail')).toBeVisible()
  await page.getByRole('button', { name: '收起详情', exact: true }).click()
  await expect(page.getByTestId('workflow-detail')).toHaveCount(0)
  await expect(reply).toHaveCount(1)
})

test('history stays selected during live updates and follow returns to the active node', async ({ page }) => {
  await mount(page)
  await render(page, [event(1, { case: 'turnStarted', value: {} }), trace(2, { case: 'takeover', value: { definition } }),
    trace(3, { case: 'decisionRequest', value: { requestId: 'next', claims: { next: question } } })])
  const takeover = page.locator('[data-record-id="session-1-turn-1-2"]')
  await takeover.click()
  await render(page, [trace(4, { case: 'decisionResult', value: { requestId: 'next', evaluations: { next: { value: { case: 'choice', value: 'read' } } } } }),
    trace(5, { case: 'dispatch', value: { call } })], true)
  await expect(takeover).toHaveAttribute('aria-pressed', 'true')
  const follow = page.getByRole('button', { name: '跟随运行', exact: true })
  await expect(follow).toHaveAttribute('aria-pressed', 'false')
  await follow.click()
  await expect(page.locator('[data-kind=tool]')).toHaveAttribute('aria-pressed', 'true')
  await expect(follow).toHaveAttribute('aria-pressed', 'true')
  // A paired result ID must navigate to its request node, even from a drawer.
  await page.evaluate(() => window.dispatchEvent(new CustomEvent('cyber-workflow-select', { detail: 'session-1-turn-1-4' })))
  await expect(page.locator('[data-kind=decision]')).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByTestId('jev-decision')).toHaveAttribute('data-state', 'answered')
})

test('generated drafts link to their matching publication and retain unrelated drafts', async ({ page }) => {
  await mount(page)
  const draft = { type: 'choice', context: 'Unique generated condition. read: Read evidence; stop: Finish.', options: ['read', 'stop'] }
  await render(page, [event(1, { case: 'turnStarted', value: {} }),
    trace(2, { case: 'generation', value: { kind: 'claim_llm', state: 'finished', output: JSON.stringify([draft]), requestId: 'draft' } }, true),
    trace(3, { case: 'libraryChange', value: { state: 'claim_published', claim: { ...draft, type: ClaimType.choice, id: 'other', context: 'An unrelated condition' } } }, true)])
  await page.locator('[data-kind=generation]').click()
  await expect(page.getByTestId('jev-claim-definition')).toContainText(draft.context)
  await expect(page.getByRole('button', { name: '查看发布内容', exact: true })).toHaveCount(0)
  await render(page, [trace(4, { case: 'libraryChange', value: { state: 'claim_published', claim: { ...draft, type: ClaimType.choice, id: 'reordered', options: ['stop', 'read'] } } }, true)], true)
  await expect(page.getByRole('button', { name: '查看发布内容', exact: true })).toHaveCount(0)
  await render(page, [trace(5, { case: 'libraryChange', value: { state: 'claim_published', claim: { ...draft, type: ClaimType.choice, id: 'matching' } } }, true)], true)
  await expect(page.getByTestId('jev-claim-definition')).toHaveCount(0)
  await page.getByRole('button', { name: '查看发布内容', exact: true }).click()
  await expect(page.locator('[data-record-id="session-1-turn-1-5"]')).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByTestId('jev-claim-definition')).toContainText(draft.context)
})
