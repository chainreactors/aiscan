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
import { controlFrames } from '../src/lib/jev-control-flow'

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

async function mount(page: Page) {
  await page.route('**/cyber.rpc.chat.SessionService/ListCommands', route => route.fulfill({ contentType: 'application/json', body: '{"commands":[]}' }))
  await page.addInitScript(() => localStorage.setItem('cyber-locale', 'zh'))
  await page.goto('/e2e/fixtures/jev.html')
  await page.waitForFunction(() => typeof (window as any).renderJEVEvents === 'function')
}
async function render(page: Page, events: ReturnType<typeof event>[], append = false) {
  await page.evaluate(({ values, append }) => (window as any).renderJEVEvents(values, append), { values: events.map(value => [...toBinary(EventSchema, value)]), append })
}

test('actual events move control through judgments, native execution, feedback and model return', async ({ page }, info) => {
  await page.setViewportSize({ width: 1440, height: 1100 })
  await mount(page)
  await render(page, initial)
  const flow = page.getByTestId('jev-control-flow')
  await expect(flow).toHaveAttribute('data-stage', 'judgment')
  await expect(flow.locator('[data-control-question]')).toHaveCount(2)
  await expect(flow.locator('[data-control-question][data-choice]')).toHaveCount(0)
  await expect(flow.locator('[data-control-route=model-judgment] .control-particle')).toBeVisible()
  await render(page, [answer, dispatch], true)
  await expect(flow).toHaveAttribute('data-stage', 'execution')
  await expect(flow.locator('[data-control-question=next]')).toHaveAttribute('data-choice', 'read')
  await expect(flow.locator('[data-control-route=judgment-executor-0]')).toHaveAttribute('data-active', 'true')
  await expect(flow.locator('[data-control-stage=execution]')).toHaveAttribute('data-state', 'pending')
  await render(page, [result], true)
  await expect(flow).toHaveAttribute('data-stage', 'feedback')
  await expect(flow.locator('[data-control-route=executor-0-feedback]')).toHaveAttribute('data-active', 'true')
  await expect(flow.locator('[data-control-stage=feedback]')).toContainText('Current page evidence')
  await render(page, [next], true)
  await expect(flow.locator('[data-control-route=feedback-judgment]')).toHaveAttribute('data-active', 'true')
  await expect(flow.locator('[data-control-route=feedback-judgment] .control-particle')).toBeVisible()
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
  await page.getByRole('button', { name: '查看详情', exact: true }).click()
  await expect(page.getByTestId('jev-decision')).toHaveAttribute('data-state', 'judging')
  await expect(page.locator('[data-control-question][data-choice]')).toHaveCount(0)
  await expect(page.locator('[data-control-stage=execution]')).toHaveCount(0)
  await seek(3) // Answer: choices become visible.
  await expect(page.getByTestId('jev-decision')).toHaveAttribute('data-state', 'answered')
  await expect(page.locator('[data-control-question=next]')).toHaveAttribute('data-choice', 'read')
  await seek(4) // Dispatch: no result from the following frame.
  await expect(page.getByTestId('workflow-detail')).not.toContainText('Current page evidence')
  await expect(page.locator('[data-control-stage=execution]')).toHaveAttribute('data-state', 'pending')
  await seek(5)
  await expect(page.getByTestId('workflow-detail')).toContainText('Current page evidence')
  await page.getByRole('button', { name: '播放执行回放', exact: true }).click()
  await expect(page.getByTestId('jev-control-flow')).toHaveAttribute('data-replaying', 'true')
  await expect.poll(() => progress.inputValue()).not.toBe('5')
  await page.getByRole('button', { name: '暂停执行回放', exact: true }).click()
  const paused = await progress.inputValue()
  await page.waitForTimeout(950)
  await expect(progress).toHaveValue(paused)
  await page.getByRole('button', { name: '返回当前', exact: true }).click()
  await expect(page.getByTestId('jev-control-flow')).toHaveAttribute('data-replaying', 'false')
  await expect(page.getByTestId('workflow-detail')).toHaveCount(0)
  await expect(page.getByTestId('assistant-response-content')).toContainText('Verified against recorded documentation')
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
    await expect(page.getByRole('button', { name: '暂停执行回放', exact: true })).toBeVisible()
    const progress = page.getByRole('slider', { name: '执行回放进度' })
    const final = await progress.getAttribute('max')
    await expect(progress).toHaveValue(final!, { timeout: 15_000 })
    await expect(page.getByTestId('jev-control-flow')).toHaveAttribute('data-stage', 'return')
    await expect(page.getByTestId('workflow-detail')).toHaveCount(0)
    await expect(page.getByTestId('assistant-response-content')).toContainText('Verified against recorded documentation')
    await page.screenshot({ path: info.outputPath('replay-completed.png'), fullPage: true })
    await expect(progress).toHaveValue('0', { timeout: 4000 })
    await expect(page.getByRole('button', { name: '暂停执行回放', exact: true })).toBeVisible()
    await page.getByRole('button', { name: '暂停执行回放', exact: true }).click()
  })
})

test('ordinary chat preserves reasoning and tool disclosures without JEV activity', async ({ page }) => {
  await mount(page)
  await render(page, [initial[0], initial[1], event(3, { case: 'toolCall', value: call })])
  await expect(page.getByTestId('agent-workflow')).toHaveCount(0)
  await page.getByRole('button', { name: '思考', exact: true }).click()
  await expect(page.getByRole('region', { name: '思考', exact: true })).toContainText('Inspect the current documentation')
  await expect(page.getByRole('button', { name: /1.*工具/ })).toBeVisible()
})

test('ordinary tool calls use the model path and background decisions do not control execution', async ({ page }) => {
  await mount(page)
  await render(page, [initial[0], initial[1],
    trace(3, { case: 'decisionRequest', value: { requestId: 'background-entry', claims } }, true),
    event(4, { case: 'toolCall', value: call })])
  await expect(page.locator('[data-control-route=model-executor-0]')).toHaveAttribute('data-active', 'true')
  await expect(page.locator('[data-control-route=judgment-executor-0]')).toHaveAttribute('data-active', 'false')
  await render(page, [event(5, { case: 'toolResult', value: { callId: call.id, name: call.name } }), event(6, { case: 'turnEnded', value: {} }),
    trace(7, { case: 'decisionRequest', value: { requestId: 'background', claims } }, true)], true)

  await expect(page.getByTestId('jev-control-flow')).toHaveAttribute('data-stage', 'background')
  await expect(page.locator('[data-control-route^=judgment-executor][data-active=true]')).toHaveCount(0)
})

test('parallel tools retain their own arrival route while sibling calls are still running', async ({ page }) => {
  await mount(page)
  const other = { ...call, id: 'http', name: 'http-reader' }
  await render(page, [initial[0], initial[1],
    trace(3, { case: 'decisionRequest', value: { requestId: 'background-entry', claims } }, true),
    event(4, { case: 'toolCall', value: call }), event(5, { case: 'toolCall', value: other })])
  await expect(page.locator('[data-control-route^=model-executor][data-active=true]')).toHaveCount(2)
  await render(page, [event(6, { case: 'toolResult', value: { callId: 'http', name: 'http-reader' } })], true)
  // Inspect the arrival while bash remains pending, using the real replay frame.
  const progress = page.getByRole('slider', { name: '执行回放进度' })
  await progress.focus(); await progress.press('End')
  await expect(page.getByTestId('jev-control-flow')).toHaveAttribute('data-stage', 'feedback')
  await expect(page.locator('[data-control-route=executor-1-feedback]')).toHaveAttribute('data-active', 'true')
  await expect(page.locator('[data-control-route=executor-0-feedback]')).toHaveAttribute('data-active', 'false')
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

test('one rendering button defaults to flow and preserves the shared playback position', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('cyber-workflow-view', 'records'))
  await mount(page)
  await render(page, [...initial, answer, dispatch, result, next, ...returned])
  await expect(page.getByTestId('jev-control-flow')).toBeVisible()
  await expect(page.getByTestId('workflow-render-toggle')).toHaveCount(1)
  await expect(page.locator('.workflow-view-tabs')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '暂停执行回放', exact: true })).toBeVisible()
  const progress = page.getByRole('slider', { name: '执行回放进度' })
  await progress.focus(); await progress.press('Home'); await progress.press('ArrowRight')
  const cursor = await progress.inputValue()
  await page.getByRole('button', { name: '切换为泳道图', exact: true }).click()
  await expect(page.locator('[data-diagram=swimlane]')).toBeVisible()
  await expect(progress).toHaveValue(cursor)
  await expect(page.locator('[data-workflow-node][aria-pressed=true]')).toHaveCount(1)
  await page.getByRole('button', { name: '切换为流程图', exact: true }).click()
  await expect(progress).toHaveValue(cursor)
  await expect(page.getByTestId('jev-control-flow')).toHaveAttribute('data-replaying', 'true')
  await page.getByRole('button', { name: '播放执行回放', exact: true }).click()
  await expect.poll(() => progress.inputValue()).not.toBe(cursor)
  await page.getByRole('button', { name: '暂停执行回放', exact: true }).click()
  const paused = await progress.inputValue()
  await page.waitForTimeout(1000)
  await expect(progress).toHaveValue(paused)
})

test('reduced motion keeps the recorded diagram static until playback is requested', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await mount(page)
  await render(page, [...initial, answer, dispatch, result, next, ...returned])
  await expect(page.getByTestId('jev-control-flow')).toHaveAttribute('data-replaying', 'false')
  await expect(page.getByRole('button', { name: '播放执行回放', exact: true })).toBeVisible()
  await page.waitForTimeout(1000)
  await expect(page.getByTestId('jev-control-flow')).toHaveAttribute('data-replaying', 'false')
})
