import { test, expect } from '@playwright/test'
import { create, createRegistry, fromJson, toBinary, type MessageInitShape } from '@bufbuild/protobuf'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { anyPack, timestampFromDate } from '@bufbuild/protobuf/wkt'
import { EventSchema } from '../cyber-ui/packages/aop/src/gen/aop/event_pb'
import { RuntimeEventSchema } from '../src/gen/types/jev_pb'
import { ClaimType } from '../src/gen/decision/claim_pb'
import { claimDefinitions } from '../src/lib/jev-decisions'
import { file_types_chat } from '../src/gen/types/chat_pb'
import { file_types_agent } from '../src/gen/types/agent_pb'
import { file_aop_operation_protocol } from '../cyber-ui/packages/aop/src/gen/aop/operation/protocol_pb'
import { file_aop_file_protocol } from '../cyber-ui/packages/aop/src/gen/aop/file/protocol_pb'
import { file_aop_pty_protocol } from '../cyber-ui/packages/aop/src/gen/aop/pty/protocol_pb'
import { jevEvent } from '../src/lib/jev-view'
import { showExecutionLanes } from './jev-helpers'

const claim = '提交订单后结果未知时，读取当前订单状态；确认操作身份后继续，缺少查询能力时交还主模型。'
const source = 'js:function(context,args){return {defer:"waiting for a trusted contract"};}'
function event(seq: number, payload: MessageInitShape<typeof EventSchema>['payload']) {
  return create(EventSchema, { id: `v2-${seq}`, sessionId: 'session-1', turnId: 'turn-1', emitter: 'jev', seq: BigInt(seq), emittedAt: timestampFromDate(new Date(1700000000000 + seq * 1000)), payload })
}
function runtime(seq: number, payload: MessageInitShape<typeof RuntimeEventSchema>['payload'], background = false) {
  return event(seq, { case: 'extension', value: anyPack(RuntimeEventSchema, create(RuntimeEventSchema, { taskId: 'v2-task', segmentId: background ? '' : 'v2-segment', background, payload })) })
}
async function render(page: import('@playwright/test').Page, events: ReturnType<typeof event>[]) {
  await page.evaluate(values => (window as any).renderJEVEvents(values), events.map(value => [...toBinary(EventSchema, value)]))
  await showExecutionLanes(page)
}
test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('cyber-locale', 'zh'))
  await page.goto('/e2e/fixtures/jev.html')
})

test('natural-language Claims and candidates remain distinct from qualified Reflexes', async ({ page }) => {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  await page.evaluate(({ claim, source, type }) => (window as any).renderJEVLibrary({ mode: 'auto', status: 'ready',
    claims: [{ id: 'claim-natural', type, context: claim }],
    candidates: [{ id: 'candidate-v2', when: 'Inspect an order', observe: source, apiVersion: 2, manifestJson: '{"steps":{}}', blocker: 'no complete replay of the current recorded trajectory' }],
    reflexes: [{ id: 'qualified-v2', when: 'Inspect an order with native contracts', observe: source, apiVersion: 2, qualificationJson: '{"format":"native-mechanism/1","checks":["syntax","recorded_replay"],"replayed":1,"coverage_gaps":["fault branch has no recorded evidence"]}', manifestJson: '{"steps":{}}' }],
  }), { claim, source, type: ClaimType.noul })
  await page.getByRole('button', { name: 'Reflex', exact: true }).click()
  await page.getByRole('tab', { name: 'Reflex 库' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('button').filter({ hasText: 'claim-natural' }).click()
  await expect(dialog.getByTestId('jev-claim-definition')).toContainText(claim)
  await expect(dialog.locator('[data-option-id]')).toHaveCount(0)
  await dialog.getByRole('button').filter({ hasText: 'candidate-v2' }).click()
  await expect(dialog.getByTestId('jev-reflex-definition')).toContainText('待验证候选 · API 2')
  await expect(dialog.getByTestId('jev-candidate-blocker')).toContainText('no complete replay')
  await dialog.getByRole('button').filter({ hasText: 'qualified-v2' }).click()
  await expect(dialog.getByTestId('jev-reflex-definition')).toContainText('已通过机制验证 · API 2')
  await expect(dialog.getByTestId('jev-reflex-definition')).toContainText('fault branch has no recorded evidence')
  expect(errors).toEqual([])
})

test('compiler rounds and mechanism diagnostics retain separate usage', async ({ page }, info) => {
  const events = [event(1, { case: 'turnStarted', value: {} }),
    runtime(2, { case: 'generation', value: { kind: 'compiler_round', state: 'finished', requestId: 'round-1', parentRequestId: 'compiler', attempt: 1, usage: { inputTokens: 100n, outputTokens: 10n } } }, true),
    runtime(3, { case: 'generation', value: { kind: 'reflex_validation', state: 'finished', requestId: 'validator', parentRequestId: 'compiler', attempt: 1, error: 'native read flag contradicts trusted contract' } }, true),
    runtime(4, { case: 'generation', value: { kind: 'reflex_llm', state: 'finished', requestId: 'compiler', usage: { inputTokens: 100n, outputTokens: 10n } } }, true),
    runtime(5, { case: 'decisionResult', value: { requestId: 'background-judge', usage: { inputTokens: 20n, outputTokens: 2n } } }, true),
    runtime(6, { case: 'generation', value: { kind: 'parameters_llm', state: 'finished', usage: { inputTokens: 30n, outputTokens: 3n } } }),
    runtime(7, { case: 'decisionResult', value: { requestId: 'runtime-judge', usage: { inputTokens: 40n, outputTokens: 4n } } }),
    event(8, { case: 'usage', value: { inputTokens: 50n, outputTokens: 5n } }),
    runtime(9, { case: 'decisionResult', value: { requestId: 'missing-usage' } })]
  await render(page, [...events, ...events])
  await page.locator('[data-record-id="v2-3"]').click()
  const detail = page.getByTestId('workflow-detail')
  await expect(detail).toContainText('native read flag contradicts trusted contract')
  await expect(detail).not.toContainText('用量缺失')
  await page.getByRole('button', { name: 'Reflex', exact: true }).click()
  const usage = page.getByTestId('jev-usage-separation')
  await expect(usage).toContainText('编译用量')
  await expect(usage).toContainText('120')
  await expect(usage).toContainText('运行用量')
  await expect(usage).toContainText('12')
  await expect(usage).toContainText('1 次用量缺失')
  await expect(usage).toContainText('费用未确认')
  await page.screenshot({ path: info.outputPath('compile-runtime-usage.png'), fullPage: true })
})

test('typed Claim generation rejects legacy drafts and links to publication', async ({ page }) => {
  expect(claimDefinitions({ claims: [{ text: claim }, '检查当前查询能力是否可用。'] })).toEqual([])
  await render(page, [event(1, { case: 'turnStarted', value: {} }),
    runtime(2, { case: 'generation', value: { kind: 'claim_llm', state: 'finished', output: JSON.stringify({ claims: [{ type: 'noul', context: claim }, { type: 'noul', context: '检查当前查询能力是否可用。' }] }) } }, true)])
  await page.locator('[data-record-id="v2-2"]').click()
  await expect(page.getByTestId('workflow-detail').getByTestId('jev-claim-definition')).toHaveCount(2)
  await expect(page.getByTestId('workflow-detail')).toContainText(claim)
  await render(page, [event(1, { case: 'turnStarted', value: {} }),
    runtime(2, { case: 'generation', value: { kind: 'claim_llm', state: 'finished', output: JSON.stringify({ claims: [{ type: 'noul', context: claim }] }) } }, true),
    runtime(3, { case: 'libraryChange', value: { state: 'claim_published', claim: { id: 'claim-natural', type: ClaimType.noul, context: claim } } }, true)])
  await page.locator('[data-record-id="v2-2"]').click()
  await page.getByRole('button', { name: '查看发布内容' }).click()
  await expect(page.getByTestId('workflow-detail')).toContainText(claim)
})

test('handoff preserves reason, effect states and the computed result before any call', async ({ page }) => {
  await render(page, [event(1, { case: 'turnStarted', value: {} }),
    runtime(2, { case: 'takeover', value: { definition: { id: 'v2', apiVersion: 2, when: 'Inspect orders', observe: source } } }),
    runtime(3, { case: 'handoff', value: { reason: 'defer', code: 'missing_input', detail: 'Order identity is required', effectsJson: '{"effect-1":{"step":"submit","occurrence":0,"state":"unknown"}}', resultJson: '{"defer":"missing parameters","parameters":"order_id"}' } })])
  await page.locator('[data-record-id="v2-3"]').click()
  const detail = page.getByTestId('workflow-detail')
  await expect(detail).toContainText('Order identity is required')
  await expect(detail).toContainText('missing_input')
  await expect(detail).toContainText('unknown')
  await expect(detail).toContainText('order_id')
})

test('compiler diagnostics explain replay position, exact mismatch and repair action', async ({ page }) => {
  const diagnostic = { code: 'native_call_mismatch', stage: 'replay', status: 'repair',
    message: 'Call 1 used a changed argument', action: 'Recover the exact current value using inspect_evidence and validate again.',
    replayed: 1, recorded: 4, expected: ['experiment', 'append', 'current actor'], actual: ['experiment', 'append', 'copied actor'] }
  await render(page, [event(1, { case: 'turnStarted', value: {} }), runtime(2, { case: 'generation', value: {
    kind: 'reflex_validation', state: 'finished', attempt: 5, error: diagnostic.message, output: JSON.stringify({ artifact: {}, diagnostic }),
  } }, true)])
  await page.locator('[data-record-id="v2-2"]').click()
  const detail = page.getByTestId('jev-compiler-diagnostic')
  await expect(detail).toContainText('原生调用不匹配')
  await expect(detail).toContainText('已回放 1 / 4 个结果')
  await expect(detail).toContainText('inspect_evidence')
  await detail.getByText('已记录的预期调用／结果', { exact: true }).click()
  await detail.getByText('生成的实际调用／结果', { exact: true }).click()
  await expect(detail).toContainText('current actor')
  await expect(detail).toContainText('copied actor')
})

test('completion and unsupported evidence diagnostics retain distinct repair actions', async ({ page }) => {
  const diagnostics = [
    { code: 'completion_missing', stage: 'completion', status: 'repair', message: 'All calls replayed but the function returned defer.', action: 'Process the fresh execute return value and produce report.', replayed: 3, recorded: 3 },
    { code: 'recorded_capability_unavailable', stage: 'native_contract', status: 'waiting', message: 'Compound native result has no trusted operation contract.', action: 'Wait for a supported real trajectory; preserve the candidate.' },
  ]
  await render(page, [event(1, { case: 'turnStarted', value: {} }), ...diagnostics.map((diagnostic, index) => runtime(index + 2, {
    case: 'generation', value: { kind: 'reflex_validation', state: 'finished', attempt: index + 1, error: diagnostic.message, output: JSON.stringify({ artifact: {}, diagnostic }) },
  }, true))])
  await page.locator('[data-record-id="v2-2"]').click()
  const detail = page.getByTestId('jev-compiler-diagnostic')
  await expect(detail).toContainText('缺少完成报告')
  await expect(detail).toContainText('已回放 3 / 3 个结果')
  await expect(detail).toContainText('fresh execute return value')
  await page.locator('[data-record-id="v2-3"]').click()
  await expect(detail).toContainText('等待受支持的原生轨迹')
  await expect(detail).toContainText('preserve the candidate')
})

test('production profile streaming events render publication, native execution and composition', async ({ page }, info) => {
  const path = process.env.JEV_PROFILE_FLOW_EVENTS || fileURLToPath(new URL('./fixtures/jev-history/profile-events.json', import.meta.url))
  const registry = createRegistry(RuntimeEventSchema, file_types_chat, file_types_agent, file_aop_operation_protocol, file_aop_file_protocol, file_aop_pty_protocol)
  const events = JSON.parse(readFileSync(path!, 'utf8')).map((row: any) => fromJson(EventSchema, row.event, { registry }))
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  await render(page, events)
  await expect(page.getByText('Current sessions verified from native evidence.', { exact: true })).toHaveCount(3)
  const publication = events.find((event: any) => jevEvent(event)?.payload.case === 'libraryChange'
    && (jevEvent(event)?.payload as any).value.state === 'reflex_published')
  expect(publication).toBeTruthy()
  await page.locator(`[data-record-id="${publication.id}"]`).click()
  await expect(page.getByTestId('workflow-detail')).toContainText('已通过机制验证')
  const handoff = events.find((event: any) => jevEvent(event)?.payload.case === 'handoff'
    && (jevEvent(event)?.payload as any).value.reason === 'report')
  expect(handoff).toBeTruthy()
  await page.locator(`[data-record-id="${handoff.id}"]`).click()
  await expect(page.getByRole('region', { name: '已交还 LLM', exact: true })).toContainText('report')
  await expect(page.getByTestId('agent-workflow')).toHaveCount(3)
  await page.screenshot({ path: info.outputPath('production-profile-flow.png'), fullPage: true })
  expect(errors).toEqual([])
})
