import { test, expect, type Page } from '@playwright/test'
import { create, toBinary } from '@bufbuild/protobuf'
import { timestampFromDate } from '@bufbuild/protobuf/wkt'
import { EventSchema, type Event } from '@cyber/aop'
import { recordMediaURL } from '../src/lib/record-result'
import { recordingInfo } from '../cyber-ui/packages/viewer/src/lib/record-result'
import { reduceAOPToTimeline } from '../cyber-ui/packages/viewer/src/lib/aop-reducer'

const encoder = new TextEncoder()
const png = new Uint8Array(Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jkAAAAABJRU5ErkJggg==', 'base64'))

function event(seq: number, payload: Event['payload'], turnId = 'turn-1', sessionId = 'physical-session'): Event {
  return create(EventSchema, { id: `event-${sessionId}-${seq}`, seq: BigInt(seq), sessionId, turnId, emitter: 'agent',
    emittedAt: timestampFromDate(new Date(1700000000000 + seq * 1000)), payload })
}

function call(seq = 1, action = 'screenshot', turnId = 'turn-1', sessionId = 'physical-session') {
  return event(seq, { case: 'toolCall', value: { id: 'call-1', name: 'record', arguments: { data: encoder.encode(JSON.stringify({ action })), mediaType: 'application/json' } } }, turnId, sessionId)
}

function result(seq = 2, output: Record<string, unknown> | unknown[] = { action: 'screenshot', target: { kind: 'desktop', width: 1280, height: 720 }, output: '/runner/shot.png', bytes: 68 },
  kind: 'image' | 'video' | null = 'image', turnId = 'turn-1', sessionId = 'physical-session', error = false) {
  return event(seq, { case: 'toolResult', value: { callId: 'call-1', name: 'record', isError: error,
    output: [
      { value: { case: 'text', value: { text: JSON.stringify(output) } } },
      ...(kind ? [{ value: { case: 'media' as const, value: { kind, resource: {
        mediaType: kind === 'image' ? 'image/png' : 'video/mp4', filename: kind === 'image' ? 'shot.png' : 'capture.mp4',
        source: kind === 'image' ? { case: 'data' as const, value: png } : { case: 'uri' as const, value: '.cyber/record/capture.mp4' },
      } } } }] : []),
    ],
  } }, turnId, sessionId)
}

const ended = (seq = 3, turnId = 'turn-1', sessionId = 'physical-session') => event(seq, { case: 'turnEnded', value: { stopReason: 'completed' } }, turnId, sessionId)

test('record results retain typed media and isolate reused call IDs by session and turn', () => {
  const one = result()
  const two = result(4, { state: 'completed' }, 'video', 'turn-2')
  const other = result(5, { state: 'recording' }, null, 'turn-1', 'other-session')
  const responses = reduceAOPToTimeline([one, two, other, one]).filter(item => item.kind === 'assistant_response')
  expect(responses).toHaveLength(3)
  expect(responses[0]).toMatchObject({ sessionId: 'physical-session', turnId: 'turn-1', tools: [{ resultEventId: one.id }] })
  expect(responses[0].tools[0].toolResult).toBe(one.payload.value)
  expect(responses[1].tools[0].toolResult).toBe(two.payload.value)
  expect(responses[0].tools[0].toolResult?.output[1].value).toMatchObject({ case: 'media', value: { resource: { source: { case: 'data', value: png } } } })
  expect(responses[1].tools[0].toolResult?.output[1].value).toMatchObject({ case: 'media', value: { resource: { source: { case: 'uri', value: '.cyber/record/capture.mp4' } } } })
  expect(recordingInfo('{"state":"recording","frames":false,"target":{"title":42}}')).toEqual([{ state: 'recording', frames: false, target: { title: 42 } }])
  expect(recordingInfo('not JSON')).toEqual([])
  expect(recordingInfo('[null,42,{"error":"capture failed"}]')).toEqual([{ error: 'capture failed' }])
  expect(recordMediaURL('chat/id', 'event?1', 2, true)).toBe('/api/sessions/chat%2Fid/media/event%3F1/2?download=1')
})

async function openFixture(page: Page) {
  await page.addInitScript(() => localStorage.setItem('cyber-locale', 'zh'))
  await page.route('**/cyber.rpc.chat.SessionService/ListCommands', route => route.fulfill({ json: { commands: [] } }))
  await page.goto('/e2e/fixtures/record.html')
  await page.waitForFunction(() => typeof (window as any).renderRecordEvents === 'function')
}

async function show(page: Page, events: Event[], busy = false) {
  await page.evaluate(({ values, busy }) => (window as any).renderRecordEvents(values, busy), {
    values: events.map(value => Array.from(toBinary(EventSchema, value))), busy,
  })
}

async function expandTools(page: Page) {
  const cards = page.getByTestId('assistant-response')
  for (const card of await cards.all()) {
    const button = card.getByRole('button', { name: /工具|Tools?/ })
    if (await button.count() && await button.first().getAttribute('aria-expanded') === 'false') await button.first().click()
  }
}

test('screenshots render inline, open and download, and survive history replay on a narrow screen', async ({ page }) => {
  await openFixture(page)
  const events = [call(), result(), ended()]
  await show(page, events)
  await expandTools(page)
  const image = page.getByTestId('record-image')
  await expect(image).toBeVisible()
  await expect.poll(() => image.evaluate((img: HTMLImageElement) => img.naturalWidth)).toBe(1)
  await expect(page.getByTestId('record-status')).toContainText('1280 × 720')
  await expect(page.getByRole('link', { name: '打开截图' })).toHaveAttribute('href', /^blob:/)
  const url = recordMediaURL('chat-1', events[1].id, 1, true)
  await page.route(`**${url}`, route => route.fulfill({ body: Buffer.from(png), headers: { 'Content-Type': 'image/png', 'Content-Disposition': 'attachment; filename="shot.png"' } }))
  const downloading = page.waitForEvent('download')
  await page.getByRole('link', { name: '下载', exact: true }).click()
  expect((await downloading).suggestedFilename()).toBe('shot.png')
  await page.locator('textarea').first().fill('后续任务草稿')
  await show(page, [...events, events[1]])
  await expect(image).toHaveCount(1)
  await expect(page.locator('textarea').first()).toHaveValue('后续任务草稿')
  await page.setViewportSize({ width: 390, height: 844 })
  await expect(image).toBeVisible()
  expect(await page.getByTestId('record-result').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  await page.screenshot({ path: test.info().outputPath('record-mobile.png'), fullPage: true })
  await page.reload()
  await page.waitForFunction(() => typeof (window as any).renderRecordEvents === 'function')
  await show(page, events)
  await expandTools(page)
  await expect(image).toBeVisible()
})

test('recording status and capture errors remain readable without fabricating media', async ({ page }) => {
  await openFixture(page)
  const status = result(2, [
    { recording_id: 'active', state: 'recording', fps: 30, target: { kind: 'window', title: '测试窗口' } },
    { recording_id: 'done', state: 'completed', duration_ms: 1500, frames: 45, bytes: 2048 },
  ], null)
  await show(page, [call(1, 'status'), status, ended()])
  await expandTools(page)
  await expect(page.getByTestId('record-status')).toHaveCount(2)
  await expect(page.getByTestId('record-status').first()).toContainText('录制中')
  await expect(page.getByTestId('record-status').last()).toContainText('1.5 秒')
  await expect(page.getByTestId('record-status').last()).toContainText('45 帧')
  await expect(page.getByTestId('record-media')).toHaveCount(0)
  await show(page, [call(1, 'record'), result(2, { state: 'failed', error: 'Capture window was closed' }, null, 'turn-1', 'physical-session', true), ended()])
  await expect(page.getByRole('alert')).toContainText('Capture window was closed')
  await show(page, [call(1, 'status'), result(2, [], null), ended()])
  await expect(page.getByTestId('record-result')).toContainText('暂无录制')
})

test('completed MP4 plays and seeks through the session media URL, with a download and load failure state', async ({ page }) => {
  await openFixture(page)
  // Generate synthetic frames in the browser so this checks an actual playable
  // MP4 without a native recorder, external fixture URL or desktop capture.
  const bytes = await page.evaluate(async () => {
    const type = 'video/mp4;codecs=avc1.42001E'
    if (!MediaRecorder.isTypeSupported(type)) throw new Error('Chromium has no MP4/H.264 MediaRecorder support')
    const canvas = document.createElement('canvas')
    canvas.width = 160; canvas.height = 120
    const ctx = canvas.getContext('2d')!
    const stream = canvas.captureStream(10)
    const recorder = new MediaRecorder(stream, { mimeType: type })
    const chunks: Blob[] = []
    recorder.ondataavailable = event => chunks.push(event.data)
    const finished = new Promise<void>(resolve => { recorder.onstop = () => resolve() })
    recorder.start()
    for (let frame = 0; frame < 12; frame++) {
      ctx.fillStyle = frame % 2 ? '#2463eb' : '#123456'
      ctx.fillRect(0, 0, canvas.width, canvas.height)
      await new Promise(resolve => setTimeout(resolve, 100))
    }
    recorder.stop()
    await finished
    stream.getTracks().forEach(track => track.stop())
    return Array.from(new Uint8Array(await new Blob(chunks, { type: 'video/mp4' }).arrayBuffer()))
  })
  const media = result(2, { recording_id: 'done', state: 'completed', duration_ms: 1200, fps: 10, frames: 12, bytes: bytes.length }, 'video')
  const url = recordMediaURL('chat-1', media.id, 1)
  let rangeReads = 0
  await page.route(`**${url}*`, async route => {
    const request = route.request()
    const range = request.headers().range
    if (range) rangeReads++
    const match = range?.match(/^bytes=(\d+)-(\d*)$/)
    const start = match ? Number(match[1]) : 0
    const end = match?.[2] ? Number(match[2]) : bytes.length - 1
    await route.fulfill({ status: match ? 206 : 200, body: Buffer.from(bytes.slice(start, end + 1)), headers: {
      'Content-Type': 'video/mp4', 'Accept-Ranges': 'bytes',
      ...(match ? { 'Content-Range': `bytes ${start}-${end}/${bytes.length}` } : {}),
      ...(request.url().includes('download=1') ? { 'Content-Disposition': 'attachment; filename="capture.mp4"' } : {}),
    } })
  })
  await show(page, [call(1, 'record'), media, ended()])
  await expandTools(page)
  const video = page.getByTestId('record-video')
  await expect(video).toHaveAttribute('src', url)
  await expect.poll(() => video.evaluate((element: HTMLVideoElement) => element.readyState)).toBeGreaterThanOrEqual(2)
  await video.evaluate(async (element: HTMLVideoElement) => { element.muted = true; await element.play() })
  await expect.poll(() => video.evaluate((element: HTMLVideoElement) => element.currentTime)).toBeGreaterThan(0)
  await video.evaluate((element: HTMLVideoElement) => { element.pause(); element.currentTime = 0.5 })
  await expect.poll(() => video.evaluate((element: HTMLVideoElement) => element.currentTime)).toBeGreaterThanOrEqual(0.5)
  expect(rangeReads).toBeGreaterThan(0)
  const downloading = page.waitForEvent('download')
  await page.getByRole('link', { name: '下载', exact: true }).click()
  expect((await downloading).suggestedFilename()).toBe('capture.mp4')
  await page.screenshot({ path: test.info().outputPath('record-desktop.png'), fullPage: true })
  await page.unroute(`**${url}*`)
  await page.route('**/api/sessions/chat-1/media/**', route => route.fulfill({ status: 502, body: 'node disconnected' }))
  await show(page, [call(1, 'record'), result(4, { state: 'completed' }, 'video'), ended(5)])
  await expect(page.getByRole('alert')).toContainText('媒体加载失败')
})
