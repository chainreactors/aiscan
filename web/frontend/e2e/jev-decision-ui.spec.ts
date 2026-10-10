import { ClaimType } from '../src/gen/decision/claim_pb'
import { test, expect } from '@playwright/test'
import { create, toBinary, type MessageInitShape } from '@bufbuild/protobuf'
import { anyPack, timestampFromDate } from '@bufbuild/protobuf/wkt'
import { EventSchema } from '../cyber-ui/packages/aop/src/gen/aop/event_pb'
import { RuntimeEventSchema } from '../src/gen/types/jev_pb'

const definition = { id: 'evidence-loop', when: 'Read requested public evidence', decide: 'Inspect, execute a bound read, then report or defer', observe: 'js:({state: {}, candidates: []})', claimIds: ['evidence-claim'] }
const claims = {
  next: { type: ClaimType.choice, context: 'Choose the next operation' + "\nwinner: Highest probability\npicked: Actual returned selection\nunknown: Probability not returned", options: ["winner","picked","unknown"] },
  risk: { type: ClaimType.score, context: 'How much risk does this action carry?', options: ['Low', 'Medium', 'High'] },
  enough: { type: ClaimType.noul, context: 'Is the evidence sufficient?' },
}
function event(seq: number, payload: MessageInitShape<typeof EventSchema>['payload']) {
  return create(EventSchema, { id: `event-${seq}`, sessionId: 'session-1', turnId: 'turn-1', emitter: 'agent', seq: BigInt(seq), emittedAt: timestampFromDate(new Date(1700000000000 + seq * 1000)), payload })
}
function runtime(seq: number, payload: MessageInitShape<typeof RuntimeEventSchema>['payload'], background = false) {
  return event(seq, { case: 'extension', value: anyPack(RuntimeEventSchema, create(RuntimeEventSchema, { taskId: 'task', segmentId: background ? '' : 'segment', step: background ? 0 : 1, background, payload })) })
}
async function render(page: import('@playwright/test').Page, events: ReturnType<typeof event>[], append = false) {
  await page.evaluate(({ values, append }) => (window as any).renderJEVEvents(values, append), { values: events.map(value => [...toBinary(EventSchema, value)]), append })
}

test('live judgments show choice distribution, native score and noul before actual execution feedback', async ({ page }, info) => {
  await page.addInitScript(() => localStorage.setItem('cyber-locale', 'zh'))
  await page.goto('/e2e/fixtures/jev.html')
  await render(page, [event(1, { case: 'turnStarted', value: {} }),
    runtime(2, { case: 'observation', value: { stateJson: '{"page":"Public documentation","items":[1,2]}', candidatesJson: '{}' } }),
    runtime(3, { case: 'decisionRequest', value: { requestId: 'live', purpose: 'jev_execution', claims } })])
  await expect(page.getByTestId('agent-workflow')).toHaveAttribute('open', '')
  await page.locator('[data-record-id="event-3"]').click()
  await expect(page.getByTestId('jev-decision')).toHaveAttribute('data-state', 'judging')
  await expect(page.locator('[data-selected=true]')).toHaveCount(0)
  await expect(page.getByTestId('jev-question').filter({ hasText: '正在判断' })).toHaveCount(3)
  const call = { id: 'read', name: 'read-evidence', arguments: { data: new TextEncoder().encode('{"target":"public"}') } }
  await render(page, [runtime(4, { case: 'decisionResult', value: { requestId: 'live', elapsedMs: 275, usage: {inputTokens:120n,outputTokens:15n}, evaluations: {
    next: { value: { case: 'choice', value: 'picked' }, confidence: .7, probabilities: { winner: .7, picked: .3 } },
    risk: { value: { case: 'score', value: 1.4 }, confidence: .8 }, enough: { value: { case: 'noul', value: .73 }, confidence: .6 },
  } } }), runtime(5, { case: 'takeover', value: { definition } }), runtime(6, { case: 'dispatch', value: { call, candidateId: 'picked', read: true } })], true)
  await expect(page.getByTestId('jev-check')).toHaveCount(0)
  await expect(page.getByTestId('agent-workflow')).toHaveAttribute('open', '')
  await page.locator('[data-record-id="event-3"]').click()
  await expect(page.getByTestId('jev-token-usage')).toContainText('JEV · 输入 120 · 输出 15 token')
  const choice = page.locator('[data-question-id=next]')
  await expect(choice.locator('[data-option-id]')).toHaveCount(3)
  expect(await choice.locator('[data-option-id]').evaluateAll(nodes => nodes.map(node => node.getAttribute('data-option-id')))).toEqual(['winner', 'picked', 'unknown'])
  await expect(choice.locator('[data-option-id=picked]')).toHaveAttribute('data-selected', 'true')
  await expect(choice.locator('[data-option-id=unknown]').getByTestId('jev-probability')).toHaveText('—')
  const score = page.locator('[data-question-id=risk]')
  await expect(score.getByTestId('jev-scale')).toContainText('1.40')
  await expect(score.locator('.jev-scalar-track span')).toHaveAttribute('style', 'left: 70%;')
  await expect(score.getByTestId('jev-scale')).not.toContainText('%')
  await expect(page.locator('[data-question-id=enough]').getByTestId('jev-scale')).toContainText('73.0%')
  await page.locator('[data-record-id="event-5"]').click()
  await expect(page.locator('[data-control-reflex]')).toBeVisible()
  await render(page, [runtime(7, { case: 'result', value: { elapsedMs: 42, result: { callId: 'read', name: 'read-evidence', output: [{ value: { case: 'text', value: { text: 'Recorded evidence' } } }] } } }),
    runtime(8, { case: 'handoff', value: { reason: 'report' } }), event(9, { case: 'turnEnded', value: { stopReason: 'completed' } })], true)
  await expect(page.getByTestId('agent-workflow')).toContainText('已交还 LLM')
  expect(await page.getByTestId('agent-workflow').locator('[data-event-kind]').evaluateAll(nodes => nodes.map(node => node.getAttribute('data-event-kind')).sort())).toEqual(['decisionRequest', 'dispatch', 'handoff', 'observation', 'result', 'takeover'])
  await page.screenshot({ path: info.outputPath('live-typed-decisions.png'), fullPage: true })
})

test('compilation keeps generation, review, publication and a failed retry in chronological order', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('cyber-locale', 'zh'))
  await page.goto('/e2e/fixtures/jev.html')
  const records = [runtime(1, { case: 'generation', value: { kind: 'claim_llm', state: 'started' } }, true),
    runtime(2, { case: 'generation', value: { kind: 'claim_llm', state: 'finished', output: '[{"type":"choice","context":"Evidence is needed. read: Read evidence; stop: Done.","options":["read","stop"]}]' } }, true),
    runtime(3, { case: 'libraryChange', value: { state: 'claim_published', claim: { id: 'evidence-claim', type: ClaimType.choice, context: 'Evidence is needed. read: Read evidence; stop: Done.', options: ['read', 'stop'] } } }, true),
    runtime(4, { case: 'decisionRequest', value: { requestId: 'compile', claims: { compile: { type: ClaimType.choice, context: "Choose the current operation." + "\ncompile: Build scene\ndefer: Wait", options: ["compile","defer"] } } } }, true),
    runtime(5, { case: 'decisionResult', value: { requestId: 'compile', evaluations: { compile: { value: { case: 'choice', value: 'compile' }, probabilities: { compile: .8, defer: .2 } } } } }, true),
    runtime(6, { case: 'generation', value: { kind: 'reflex_llm', state: 'started' } }, true),
    runtime(7, { case: 'generation', value: { kind: 'reflex_llm', state: 'finished', error: 'Invalid binding', errorStage:'reader', attempt:2,requestId:'reflex-draft-2' } }, true),
    runtime(8, { case: 'libraryChange', value: { state: 'failed', reason: 'Native binding is missing' } }, true)]
  await render(page, [...records, ...records])
  await expect(page.getByTestId('agent-workflow')).toHaveCount(1)
  expect(await page.getByTestId('agent-workflow').locator('[data-event-seq]').evaluateAll(nodes => nodes.map(node => node.getAttribute('data-event-seq')))).toEqual(['1', '3', '4', '6', '7', '8'])
  await page.locator('[data-record-id="event-1"]').click()
  await expect(page.locator('[data-record-id=event-1]').getByTestId('jev-claim-definition')).toHaveCount(0)
  await page.locator('[data-record-id="event-3"]').click()
  await expect(page.getByTestId('jev-claim-definition')).toHaveCount(1)
  await page.locator('[data-record-id="event-4"]').click()
  await expect(page.getByTestId('jev-decision')).toBeVisible()
  await expect(page.getByTestId('workflow-detail')).toHaveCount(0)
  await expect(page.locator('.jev-inspector, .jev-flow-node')).toHaveCount(0)
  await expect(page.getByTestId('jev-reflex-definition')).toHaveCount(0)
  await page.locator('[data-record-id="event-7"]').click()
  await expect(page.locator('[data-control-current=true]')).toContainText('生成失败')
  await expect(page.locator('[data-control-current=true]')).toContainText('Invalid binding')
  await expect(page.locator('[data-generation-request-id="reflex-draft-2"]')).toContainText('第 2 次生成 · 错误阶段：reader')
  await expect(page.getByTestId('jev-token-usage').filter({hasText:'Reflex LLM'})).toContainText('token 用量未知')
})
