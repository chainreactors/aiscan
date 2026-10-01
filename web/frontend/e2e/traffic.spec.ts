import { observationEvents } from './fixtures/observations'
import { expect, test, type Page, type WebSocketRoute } from '@playwright/test'
import { create, fromBinary, toBinary, type DescMessage, type MessageShape } from '@bufbuild/protobuf'
import { anyPack, anyUnpack } from '@bufbuild/protobuf/wkt'
import { EventSchema, type Event } from '../cyber-ui/packages/aop/src/gen/aop/event_pb'
import { FlowSchema } from '../cyber-ui/packages/aop/src/gen/aop/traffic/protocol_pb'
import { EnvelopeSchema } from '../cyber-ui/packages/aop/src/gen/aop/envelope_pb'
import { ProtocolMessageSchema } from '../cyber-ui/packages/aop/src/gen/aop/protocol_pb'
import { ListEventsRequestSchema, ListEventsResponseSchema } from '../cyber-ui/packages/aop/src/gen/aop/chat_pb'
import { GetSessionRequestSchema, GetSessionResponseSchema, ListSessionsResponseSchema } from '../src/gen/types/chat_pb'
import { GetStatusResponseSchema } from '../src/gen/types/system_pb'
import { GetConfigResponseSchema } from '../src/gen/types/config_pb'
import { ListAgentsResponseSchema } from '../src/gen/types/agent_pb'
import { SyncArtifactsRequestSchema, SyncArtifactsResponseSchema } from '../src/gen/types/artifact_pb'

function event(id: string, sessionId = 'traffic-a', body = '{"captured":true}'): Event {
  return create(EventSchema, { id, sessionId, emitter: 'test-node', seq: BigInt(id.replace(/\D/g, '') || 1), payload: { case: 'extension', value: anyPack(FlowSchema, create(FlowSchema, {
    id, complete: true, request: { method: 'POST', url: `https://example.test/api/${id}?q=1`, protocol: 'HTTP/1.1', headers: [{ name: 'Content-Type', value: 'application/json' }], body: new TextEncoder().encode('{"query":"中文"}') },
    response: { statusCode: 201, reasonPhrase: 'Created', headers: [{ name: 'Set-Cookie', value: 'first=1' }, { name: 'Set-Cookie', value: 'second=2' }], body: new TextEncoder().encode(body) },
  })) } })
}

async function application(page: Page, initial: Event[], archived: Event[] = []) {
  const history = new Map<string, Event[]>([['traffic-a', initial], ['traffic-b', []]])
  const watches = new Map<string, { socket: WebSocketRoute; id: string; afterCursor: string }>()
  const proto = <D extends DescMessage>(schema: D, value: MessageShape<D>) => ({ contentType: 'application/proto', body: Buffer.from(toBinary(schema, value)) })
  await page.addInitScript(() => { localStorage.setItem('cyber-locale', 'en'); localStorage.setItem('cyber-sidebar-open', 'true') })
  await page.route('**/api/auth/session', route => route.fulfill({ json: { authenticated: true } }))
  await page.route('**/ioa/*', route => route.fulfill({ json: [] }))
  await page.route('**/cyber.rpc.*/**', route => route.fulfill({ contentType: 'application/proto', body: '' }))
  await page.route('**/GetStatus', route => route.fulfill(proto(GetStatusResponseSchema, create(GetStatusResponseSchema, { status: { version: 'traffic-preview', llmAvailable: true, llmModel: 'Preview', configLoaded: true } }))))
  await page.route('**/GetConfig', route => route.fulfill(proto(GetConfigResponseSchema, create(GetConfigResponseSchema, { config: { loaded: true } }))))
  await page.route('**/SyncArtifacts', route => {
    const request = fromBinary(SyncArtifactsRequestSchema, route.request().postDataBuffer()!)
    archived.push(...request.artifacts)
    return route.fulfill(proto(SyncArtifactsResponseSchema, create(SyncArtifactsResponseSchema, {
      artifacts: archived.slice(Number(request.afterCursor || 0)).map((event, index) => ({ event, cursor: String(Number(request.afterCursor || 0) + index + 1) })),
    })))
  })
  const records = ['traffic-a', 'traffic-b'].map(id => ({ session: { id, title: id === 'traffic-a' ? 'Traffic capture' : 'Other session', nodeId: 'test-node' } }))
  await page.route('**/ListSessions', route => route.fulfill(proto(ListSessionsResponseSchema, create(ListSessionsResponseSchema, { sessions: records }))))
  await page.route('**/GetSession', route => {
    const request = fromBinary(GetSessionRequestSchema, route.request().postDataBuffer()!)
    return route.fulfill(proto(GetSessionResponseSchema, create(GetSessionResponseSchema, { session: records.find(record => record.session.id === request.sessionId) })))
  })
  await page.route('**/ListEvents', route => {
    const request = fromBinary(ListEventsRequestSchema, route.request().postDataBuffer()!)
    const events = history.get(request.sessionId) || []
    return route.fulfill(proto(ListEventsResponseSchema, create(ListEventsResponseSchema, { events: events.slice(Number(request.afterCursor || 0)).map((event, index) => ({ event, cursor: String(Number(request.afterCursor || 0) + index + 1) })) })))
  })
  await page.routeWebSocket('**/api/aop/application/ws', socket => {
    socket.onMessage(frame => {
      if (typeof frame === 'string') return
      const envelope = fromBinary(EnvelopeSchema, frame)
      const core = envelope.payload && anyUnpack(envelope.payload, ProtocolMessageSchema)
      if (core?.message.case === 'watchEventsRequest') watches.set(core.message.value.sessionId, { socket, id: envelope.id, afterCursor: core.message.value.afterCursor })
    })
  })
  return {
    watches,
    send(event: Event, persist = true) {
      const events = history.get(event.sessionId)!
      if (persist) events.push(event)
      const watch = watches.get(event.sessionId)!
      watch.socket.send(Buffer.from(toBinary(EnvelopeSchema, create(EnvelopeSchema, { id: `delivery-${event.id}`, replyTo: watch.id, deliveryCursor: String(events.length), payload: anyPack(ProtocolMessageSchema, create(ProtocolMessageSchema, { message: { case: 'event', value: event } })) }))))
    },
  }
}

test('WebUI receives live native traffic, restores history, and isolates sessions', async ({ page }, testInfo) => {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  const first = event('flow-1')
  const wire = await application(page, [first])
  await page.goto('/sessions/traffic-a')
  await expect.poll(() => wire.watches.get('traffic-a')?.afterCursor).toBe('1')
  await page.getByRole('button', { name: 'View observations (1)', exact: true }).click()
  await expect(page.getByTestId('observation-count')).toHaveText('1 / 1')
  const detail = page.getByTestId('traffic-detail')
  await expect(detail).toContainText('POST /api/flow-1?q=1 HTTP/1.1')
  await expect(detail).toContainText('中文')
  await expect(detail).toContainText('Set-Cookie: first=1')
  await expect(detail).toContainText('Set-Cookie: second=2')
  const second = event('flow-2', 'traffic-a', 'live response')
  wire.send(second)
  wire.send(second, false)
  await expect(page.getByTestId('observation-count')).toHaveText('2 / 2')
  await expect(detail).toContainText('live response')
  await page.getByTestId('observation-list').getByRole('button').filter({ hasText: 'flow-1' }).click()
  wire.send(event('flow-3'))
  await expect(page.getByTestId('observation-count')).toHaveText('3 / 3')
  await expect(detail).toContainText('/api/flow-1?q=1')
  await page.getByRole('searchbox').fill('flow-2')
  await expect(page.getByTestId('observation-count')).toHaveText('1 / 3')
  await expect(detail).toContainText('live response')
  await page.getByRole('searchbox').fill('')
  await page.screenshot({ path: testInfo.outputPath('traffic-live.png') })
  await page.getByRole('dialog', { name: 'Observability', exact: true }).getByRole('button', { name: 'Close panel', exact: true }).click()
  await page.locator('[data-session-id="traffic-b"]').getByRole('button').first().click()
  await expect(page.getByRole('button', { name: 'View observations (0)', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'View observations (0)', exact: true }).click()
  await expect(page.getByTestId('observation-count')).toHaveText('0 / 0')
  await expect(page.getByTestId('observation-list')).not.toContainText('flow-1')
  await page.getByRole('dialog', { name: 'Observability', exact: true }).getByRole('button', { name: 'Close panel', exact: true }).click()
  await page.locator('[data-session-id="traffic-a"]').getByRole('button').first().click()
  await expect(page.getByRole('button', { name: 'View observations (3)', exact: true })).toBeVisible()
  await page.reload()
  await page.getByRole('button', { name: 'View observations (3)', exact: true }).click()
  await expect(page.getByTestId('observation-count')).toHaveText('3 / 3')
  await expect(page.getByTestId('observation-list').getByRole('button')).toHaveCount(3)
  expect(errors).toEqual([])
})

test('partial, binary and failed captures render at mobile widths', async ({ page }, testInfo) => {
  const partial = event('partial-1')
  partial.payload.case === 'extension' && (partial.payload.value = anyPack(FlowSchema, create(FlowSchema, { id: 'partial-1', error: 'response body interrupted', request: { method: 'GET', url: 'https://example.test/binary', protocol: 'HTTP/1.1' }, response: { statusCode: 206, body: new Uint8Array([0, 255, 17]) } })))
  const wire = await application(page, [partial])
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/sessions/traffic-a')
  await page.getByRole('button', { name: 'View observations (1)', exact: true }).click()
  const detail = page.getByTestId('traffic-detail')
  await page.getByTestId('observation-list').getByRole('button').first().click()
  await expect(detail).toContainText('Incomplete capture')
  await expect(detail).toContainText('response body interrupted')
  await expect(detail).toContainText('00 ff 11')
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390)
  await page.keyboard.press('Escape')
  await expect(detail).toBeHidden()
  await page.screenshot({ path: testInfo.outputPath('traffic-mobile.png') })
  await expect.poll(() => wire.watches.has('traffic-a')).toBe(true)
  const failed = event('failed-2')
  failed.payload = { case: 'extension', value: anyPack(FlowSchema, create(FlowSchema, { id: 'failed-2', error: 'upstream connection refused', request: { method: 'GET', url: 'https://example.test/failure', protocol: 'HTTP/1.1' } })) }
  wire.send(failed)
  await page.getByTestId('observation-list').getByRole('button').filter({ hasText: '/failure' }).click()
  await expect(detail.getByRole('alert')).toContainText('upstream connection refused')
  await expect(detail).toContainText('Request error')
})

test('native observations share tool timeline cards and a searchable observability panel', async ({ page }, testInfo) => {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  const events = observationEvents()
  const wire = await application(page, events)
  await page.goto('/sessions/traffic-a')
  const tools = page.getByTestId('assistant-response').getByRole('button', { name: /^2 Tools$/ })
  await tools.click()
  const timeline = page.getByTestId('tool-observations')
  await expect(timeline).toHaveCount(1)
  await expect(timeline.getByTestId('traffic-observation')).toHaveCount(1)
  await expect(timeline.getByTestId('file-observation')).toContainText('reports/scan.txt')
  await expect(timeline.getByTestId('cstx-observation')).toContainText('CSTX · gogo')
  await expect(timeline.getByTestId('operation-observation')).toHaveCount(0)
  await expect(page.getByTestId('record-image')).toBeVisible()
  await expect.poll(() => wire.watches.has('traffic-a')).toBe(true)
  const late = event('flow-late')
  late.emitter = 'local'; late.turnId = 'observed-turn'
  late.extensions = events[2].extensions
  wire.send(late); wire.send(late, false)
  await expect(timeline.getByTestId('traffic-observation')).toHaveCount(2)
  await page.getByRole('button', { name: 'View observations (13)', exact: true }).click()
  const panel = page.getByTestId('observability-panel'), detail = panel.getByTestId('observation-detail')
  await expect(panel.getByTestId('observation-count')).toHaveText('9 / 9')
  const firstRow = panel.getByTestId('observation-list').getByRole('button').first()
  await firstRow.focus()
  await page.keyboard.press('ArrowDown')
  await expect(panel.getByTestId('observation-list').getByRole('button').nth(1)).toHaveAttribute('aria-pressed', 'true')
  await page.keyboard.press('/')
  await expect(panel.getByRole('searchbox')).toBeFocused()
  await panel.getByRole('searchbox').fill('')
  await detail.getByRole('button', { name: 'Copy event ID' }).click()
  await expect(detail.getByRole('button', { name: 'Copied' })).toBeVisible()
  await panel.locator('[data-observation-kind="file"]').click()
  await expect(panel.getByTestId('observation-count')).toHaveText('1 / 9')
  await expect(detail.getByTestId('file-observation')).toContainText('1234 B')
  await panel.locator('[data-observation-kind="cstx"]').click()
  await expect(detail).toContainText('127.0.0.1')
  await expect(detail.getByText('8080', { exact: true })).toBeVisible()
  await expect(detail.getByRole('status')).toHaveCount(0)
  await expect(detail.getByRole('alert')).toHaveCount(0)
  await panel.locator('[data-observation-kind="record"]').click()
  await expect(detail.getByTestId('record-image')).toBeVisible()
  await expect(detail.getByTestId('record-image')).toHaveJSProperty('naturalWidth', 1)
  await panel.locator('[data-observation-kind="process"]').click()
  await expect(detail.getByRole('alert')).toContainText('Process exited with code 2')
  await expect(detail).toContainText('4321')
  await panel.locator('[data-observation-kind="command"]').click()
  await expect(detail).toContainText('!scan --target example.test')
  await panel.locator('[data-observation-kind="other"]').click()
  await expect(detail).toContainText('example.CustomObservation')
  await expect(detail).toContainText('01 02 ff')
  await panel.locator('[data-observation-kind="tool"]').click()
  await panel.getByRole('searchbox').fill('scan-call')
  await expect(panel.getByTestId('observation-count')).toHaveText('1 / 9')
  await panel.locator('[data-observation-view="events"]').click()
  await expect(panel.getByTestId('observation-count')).toHaveText('4 / 13')
  await panel.getByTestId('observation-list').getByRole('button').last().click()
  await expect(detail).not.toContainText('Scan completed; report saved.')
  await expect(detail.getByTestId('tool-observations')).toHaveCount(0)
  await panel.locator('[data-observation-view="activity"]').click()
  await panel.getByRole('searchbox').fill('')
  await panel.getByTestId('observation-list').getByRole('button').filter({ hasText: 'bash' }).first().click()
  await expect(detail.getByTestId('tool-observations')).toContainText('reports/scan.txt')
  await page.screenshot({ path: testInfo.outputPath('observability.png') })
  expect(errors).toEqual([])
})

test('historical assets remain in the unified library and can be reused in chat', async ({ page }) => {
  const historic = observationEvents('historical-session')[4]
  historic.id = 'historical-asset'
  await application(page, [], [historic])
  await page.goto('/sessions/traffic-a')
  await page.getByRole('button', { name: 'View observations (0)', exact: true }).click()
  const panel = page.getByTestId('observability-panel')
  await expect(panel.getByTestId('observation-count')).toHaveText('0 / 0')
  await panel.getByRole('tab', { name: /Asset library/ }).click()
  const library = page.getByTestId('asset-library')
  await expect(library).toContainText('127.0.0.1')
  const panelBounds = await panel.boundingBox(), libraryBounds = await library.boundingBox()
  expect(libraryBounds!.y - panelBounds!.y).toBeLessThan(80)
  expect(libraryBounds!.height).toBeGreaterThan(panelBounds!.height - 80)
  await expect(library.getByRole('tab', { name: /Ports/ })).toBeVisible()
  await library.getByRole('button', { name: 'Import', exact: true }).click()
  const importDialog = page.getByRole('dialog', { name: 'Import data', exact: true })
  await expect(importDialog).toBeVisible()
  await importDialog.getByRole('button', { name: 'Cancel', exact: true }).click()
  await expect(library).toBeVisible()
  await page.reload()
  await page.getByRole('button', { name: 'View observations (0)', exact: true }).click()
  await panel.getByRole('tab', { name: /Asset library/ }).click()
  await expect(library).toContainText('127.0.0.1')
  await library.getByRole('tab', { name: /IPs/ }).click()
  await library.getByPlaceholder('Search... (cstx_type:ip name~example)').fill('name~127.0.0.1')
  await library.getByRole('checkbox', { name: 'Select all filtered rows' }).check()
  await library.getByRole('button', { name: 'Send to chat', exact: true }).click()
  await expect(page.getByTestId('observability-panel')).toHaveCount(0)
  await expect(page.locator('textarea').first()).toHaveValue(/127\.0\.0\.1/)
})

test('Agent management shows only the selected agent tools, with shared definitions and aliases', async ({ page }) => {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  await application(page, [])
  const agents = create(ListAgentsResponseSchema, { agents: [
    { hello: { nodeId: 'test-node', name: 'Scanner', tools: [{ name: 'record', type: 'function', description: 'Capture screen', inputSchema: { mediaType: 'application/json', data: new TextEncoder().encode('{"type":"object","properties":{"action":{"enum":["screenshot"]}}}') } }] }, commands: [{ name: '!scan', description: 'Scan target', usage: '!scan --target <host>', aliases: ['!probe'] }] },
    { hello: { nodeId: 'other-node', name: 'Inspector', tools: [{ name: 'read', type: 'function', description: 'Read a file' }] }, commands: [{ name: '!inspect', description: 'Inspect files', usage: '!inspect <path>' }] },
  ] })
  await page.route('**/ListAgents', route => route.fulfill({ contentType: 'application/proto', body: Buffer.from(toBinary(ListAgentsResponseSchema, agents)) }))
  await page.goto('/sessions/traffic-a')
  await page.getByRole('button', { name: '2 agent(s) connected', exact: true }).click()
  const management = page.getByRole('dialog', { name: 'Agent management', exact: true })
  await expect(management).not.toContainText('NaN')
  await management.getByRole('button', { name: /Scanner idle/ }).click()
  await management.getByRole('tab', { name: /^Tools/ }).click()
  const registry = page.getByTestId('tool-registry')
  await expect(registry).toHaveAttribute('data-agent-id', 'test-node')
  await expect(registry).toContainText('Scan target')
  await expect(registry).not.toContainText('Inspect files')
  await registry.getByRole('button', { name: /record function Capture screen/ }).click()
  await expect(registry).toContainText('Input parameters')
  await expect(registry).toContainText('screenshot')
  await expect(registry).not.toContainText('Read a file')
  await registry.getByRole('textbox').fill('!probe')
  await registry.getByRole('button', { name: /scan bash Scan target/ }).click()
  await expect(registry).toContainText('!scan --target <host>')
  await expect(registry).toContainText('Aliases')
  await management.getByRole('button', { name: /Inspector idle/ }).click()
  await expect(registry).toHaveAttribute('data-agent-id', 'other-node')
  await expect(registry).toContainText('Inspect files')
  await expect(registry).not.toContainText('Scan target')
  await expect(registry).toContainText('Read a file')
  await expect(registry).not.toContainText('Capture screen')
  await registry.getByRole('textbox').fill('missing-tool')
  await expect(registry).toContainText('No matching tools')
  await management.getByRole('button', { name: /Scanner idle/ }).click()
  await expect(registry.getByRole('textbox')).toHaveValue('')
  await expect(registry).toContainText('Scan target')
  await management.getByRole('button', { name: 'Close panel', exact: true }).click()
  await page.getByRole('button', { name: 'Open settings', exact: true }).click()
  await expect(page.getByTestId('tool-registry')).toHaveCount(0)
  expect(errors).toEqual([])
})
