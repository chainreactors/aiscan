import { test, expect } from '@playwright/test'
import { fromBinary, fromJson, toBinary } from '@bufbuild/protobuf'
import { ListEventsRequestSchema, ListEventsResponseSchema } from '../cyber-ui/packages/aop/src/gen/aop/chat_pb'

test('frontend follows every history page beyond 500 events in cursor order', async ({ page }) => {
  const requested: string[] = []
  await page.route('**/cyber.rpc.chat.SessionService/ListEvents', async route => {
    const body = fromBinary(ListEventsRequestSchema, route.request().postDataBuffer()!)
    if (body.sessionId !== 'boundary-history') return route.continue()
    requested.push(body.afterCursor || '')
    const after = Number(body.afterCursor || 0)
    const count = after === 0 ? 500 : after === 500 ? 3 : 0
    const events = Array.from({ length: count }, (_, index) => {
      const cursor = after + index + 1
      return { cursor: String(cursor), event: { id: `event-${cursor}`, sessionId: 'boundary-history', emitter: 'cyber.web', message: { id: `message-${cursor}`, role: 'user', content: [{ text: { text: `context-${cursor}` } }] } } }
    })
    await route.fulfill({ contentType: 'application/proto', body: Buffer.from(toBinary(ListEventsResponseSchema, fromJson(ListEventsResponseSchema, { events, nextCursor: count ? String(after + count) : '' }))) })
  })
  await page.goto('/e2e/fixtures/api.html')
  await page.waitForFunction(() => !!(window as any).fixtureRuntime)
  const history = await page.evaluate(async () => {
    const api = await import('/src/api.ts')
    const list = api.listChatEvents || api.listChatMessages
    return (await list('boundary-history')).map(item => item.cursor)
  })
  expect(history).toEqual(Array.from({ length: 503 }, (_, index) => String(index + 1)))
  expect(requested).toEqual(['', '500', '503'])
})

test('a stuck history cursor fails explicitly instead of fetching indefinitely', async ({ page }) => {
  let requests = 0
  await page.route('**/cyber.rpc.chat.SessionService/ListEvents', async route => {
    if (fromBinary(ListEventsRequestSchema, route.request().postDataBuffer()!).sessionId !== 'boundary-stuck') return route.continue()
    requests++
    await route.fulfill({ contentType: 'application/proto', body: Buffer.from(toBinary(ListEventsResponseSchema, fromJson(ListEventsResponseSchema, { events: [], nextCursor: '12' }))) })
  })
  await page.goto('/e2e/fixtures/api.html')
  await page.waitForFunction(() => !!(window as any).fixtureRuntime)
  const error = await page.evaluate(async () => {
    const api = await import('/src/api.ts')
    const list = api.listChatEvents || api.listChatMessages
    try { await list('boundary-stuck'); return '' } catch (error) { return (error as Error).message }
  })
  expect(error).toContain('History cursor did not advance')
  expect(requests).toBe(2)
})
