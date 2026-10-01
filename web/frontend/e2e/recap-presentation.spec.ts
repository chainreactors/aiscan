import { test, expect } from '@playwright/test'
import { create, toBinary, type MessageInitShape } from '@bufbuild/protobuf'
import { anyPack, timestampFromDate } from '@bufbuild/protobuf/wkt'
import { EventSchema, type Event } from '../cyber-ui/packages/aop/src/gen/aop/event_pb'
import { reduceAOPToTimeline } from '../cyber-ui/packages/viewer/src/lib/aop-reducer'
import { RecapSchema, ReviewSchema, ReviewState } from '../src/cyber-proto'
import { withRecaps } from '../src/lib/recap-view'
import { groupGuardrailTurns, guardrailTimelineEvents, isGuardrailBoundary, withGuardrailReviews } from '../src/lib/guardrail-view'

function event(seq: number, payload: MessageInitShape<typeof EventSchema>['payload'], turnId = 'turn-1', sessionId = 'session-1') {
  return create(EventSchema, { id: 'event-' + seq, seq: BigInt(seq), sessionId, turnId, emitter: 'agent',
    emittedAt: timestampFromDate(new Date(1700000000000 + seq * 1000)), payload })
}

function answer(seq = 1, turnId = 'turn-1', sessionId = 'session-1') {
  return event(seq, { case: 'message', value: {
    id: 'answer-' + seq, role: 'assistant', content: [{ value: { case: 'text', value: { text: 'Task answer ' + turnId } } }],
  } }, turnId, sessionId)
}

function ended(seq = 2, turnId = 'turn-1', sessionId = 'session-1') {
  return event(seq, { case: 'turnEnded', value: { stopReason: 'completed' } }, turnId, sessionId)
}

function recap(seq = 3, text = 'Checked the implementation.', turnId = 'turn-1', sessionId = 'session-1') {
  return event(seq, { case: 'extension', value: anyPack(RecapSchema, create(RecapSchema, { text })) }, turnId, sessionId)
}

function project(events: Event[]) {
  return withRecaps(groupGuardrailTurns(withGuardrailReviews(reduceAOPToTimeline(guardrailTimelineEvents(events),
    { streaming: true, lifecycle: 'errors', responseBoundary: isGuardrailBoundary }), [], events)), events)
}

test('late recap belongs to its finished turn, without changing ordering or streaming state', () => {
  const events = [answer(), ended(), answer(3, 'turn-2')]
  const base = project(events)
  const result = project([...events, recap(4)])
  expect(result.map(item => [item.id, item.timestamp])).toEqual(base.map(item => [item.id, item.timestamp]))
  expect(result).toHaveLength(2)
  expect(result[0]).toMatchObject({ sessionId: 'session-1', turnId: 'turn-1', streaming: false,
    response: { content: 'Task answer turn-1', metadata: { recap: 'Checked the implementation.' } } })
  expect(result[1]).toEqual(base[1])
  expect(base[0]).not.toHaveProperty('response.metadata.recap')
})

test('recap requires completion, isolates sessions and deduplicates replay by sequence', () => {
  const events = [answer(), answer(2, 'turn-1', 'session-2'), ended(3), ended(4, 'turn-1', 'session-2')]
  const annotations = [recap(5, 'old'), recap(7, 'latest'), recap(6, 'other session', 'turn-1', 'session-2'), recap(5, 'old')]
  const result = project([...events, ...annotations])
  expect(result).toHaveLength(2)
  expect(result[0]).toMatchObject({ response: { metadata: { recap: 'latest' } } })
  expect(result[1]).toMatchObject({ response: { metadata: { recap: 'other session' } } })
  expect(project([answer(), recap()])[0]).not.toHaveProperty('response.metadata.recap')
  expect(project([recap(), ended(), answer()])[0]).toHaveProperty('response.metadata.recap')
})

test('malformed or empty annotations disappear without hiding unrelated extensions', () => {
  const invalid = event(3, { case: 'extension', value: {
    typeUrl: 'type.googleapis.com/' + RecapSchema.typeName, value: new Uint8Array([255]),
  } })
  const unrelated = event(4, { case: 'extension', value: { typeUrl: 'example.Other' } })
  const result = project([answer(), ended(), invalid, unrelated, recap(5, '  ')])
  expect(result.map(item => item.kind)).toEqual(['assistant_response', 'extension'])
  expect(result[0]).not.toHaveProperty('response.metadata.recap')
  expect(result[1]).toHaveProperty('extensionType', 'example.Other')
})

function reviewedTask() {
  const call = event(1, { case: 'toolCall', value: { id: 'call-1', name: 'bash' } })
  const review = create(ReviewSchema, { sessionId: 'session-1', call: { id: 'call-1', name: 'bash' },
    operation: { operationId: 'op-1' }, state: ReviewState.PENDING })
  return [call, event(2, { case: 'extension', value: anyPack(ReviewSchema, review) }),
    event(3, { case: 'extension', value: anyPack(ReviewSchema, create(ReviewSchema, { ...review, state: ReviewState.APPROVED })) }),
    event(4, { case: 'toolResult', value: { callId: 'call-1', name: 'bash' } }),
    answer(5), ended(6)]
}

test('an approval-segmented turn owns one recap after its final response', () => {
  const result = project([...reviewedTask(), recap(7)])
  expect(result).toHaveLength(1)
  expect(result[0]).toHaveProperty('response.metadata.recap', 'Checked the implementation.')
  if (result[0].kind !== 'assistant_response') throw new Error('missing response')
  expect(result[0].steps?.filter(step => step.kind === 'assistant_response')).toHaveLength(2)
  expect(result[0].steps?.every(step => step.kind !== 'assistant_response' || !step.response?.metadata?.recap)).toBe(true)
})

test('failed work can display its recap without changing the terminal error', () => {
  const failure = event(2, { case: 'turnEnded', value: { stopReason: 'error', error: { message: 'provider failed' } } })
  const base = project([answer(), failure])
  const result = project([answer(), failure, recap(3, 'Checked the code; verification failed.')])
  expect(result[0]).toHaveProperty('response.metadata.recap', 'Checked the code; verification failed.')
  expect(result.slice(1)).toEqual(base.slice(1))
})

test('ChatPanel renders one plain-text footer after the answer and preserves composer input', async ({ page }) => {
  await page.goto('/e2e/fixtures/recap.html')
  const show = async (events: Event[]) => {
    await page.waitForFunction(() => typeof (window as any).renderRecapEvents === 'function')
    await page.evaluate(values => (window as any).renderRecapEvents(values), events.map(value => Array.from(toBinary(EventSchema, value))))
  }
  const events = reviewedTask()
  await show(events)
  await expect(page.getByTestId('task-recap')).toHaveCount(0)
  const composer = page.locator('textarea').first()
  await composer.fill('Next task draft')
  const text = '已检查实现并通过测试；<b>plain text</b>。' + '这是一条较长的摘要，用于检查窄屏显示。'.repeat(3)
  await show([...events, recap(7, text), recap(7, text)])
  const footer = page.getByTestId('assistant-response-footer')
  await expect(page.getByTestId('task-recap')).toHaveText(text)
  await expect(footer).toHaveCount(1)
  await expect(footer.locator('b')).toHaveCount(0)
  await expect(page.getByTestId('assistant-response')).toHaveCount(1)
  await expect(composer).toHaveValue('Next task draft')
  const response = await page.getByTestId('assistant-response-content').last().boundingBox()
  const annotation = await footer.boundingBox()
  expect(annotation!.y).toBeGreaterThanOrEqual(response!.y + response!.height)
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(footer).toBeVisible()
  expect(await footer.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  await page.screenshot({ path: test.info().outputPath('recap-mobile.png'), fullPage: true })
})
