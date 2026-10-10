import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { createRegistry, fromJson, toBinary } from '@bufbuild/protobuf'
import { EventSchema } from '../cyber-ui/packages/aop/src/gen/aop/event_pb'
import { RuntimeEventSchema } from '../src/gen/types/jev_pb'
import { projectJEV } from '../src/lib/jev-view'

test('recorded provider events render honest background outcomes and deduplicate replay', async ({ page }, info) => {
  const path = process.env.JEV_REPLAY_LOG || fileURLToPath(new URL('./fixtures/jev-history/live-protocol.jsonl', import.meta.url))
  const registry = createRegistry(RuntimeEventSchema)
  const events = readFileSync(path!, 'utf8').split(/\r?\n/).filter(Boolean).flatMap(line => {
    const event = JSON.parse(line).payload?.event
    return event?.extension?.['@type']?.endsWith('cyber.jev.RuntimeEvent')
      ? [fromJson(EventSchema, event, { registry })] : []
  })
  const projection = projectJEV(events)
  expect(projection.records.length).toBeGreaterThan(0)
  expect(projection.compilations).toHaveLength(3)
  expect(projection.segments).toHaveLength(0)
  expect(projection.compilations.some(compilation => compilation.records.some(record =>
    record.value.payload.case === 'libraryChange' && record.value.payload.value.state === 'failed'))).toBe(true)
  await page.addInitScript(() => localStorage.setItem('cyber-locale', 'zh'))
  await page.goto('/e2e/fixtures/jev.html')
  const binary = events.map(event => [...toBinary(EventSchema, event)])
  await page.evaluate(values => (window as any).renderJEVEvents(values), [...binary, ...binary])
  await expect(page.getByTestId('agent-workflow')).toHaveCount(3)
  await expect(page.getByTestId('jev-compilation')).toHaveCount(0)
  await expect(page.getByTestId('jev-segment')).toHaveCount(0)
  const failure = projection.compilations[0].records.find(record => record.value.payload.case === 'libraryChange' && record.value.payload.value.state === 'failed')
  expect(failure?.value.payload.case).toBe('libraryChange')
  const reason = failure?.value.payload.case === 'libraryChange' ? failure.value.payload.value.reason : ''
  expect(reason).not.toBe('')
  const generation = projection.compilations[0].records.find(record => record.value.payload.case === 'generation' && record.value.payload.value.error === reason)
  await page.evaluate(id => window.dispatchEvent(new CustomEvent('cyber-workflow-select', { detail: id })), (generation || failure)!.event.id)
  const feedback = page.getByTestId('jev-compilation-error').filter({ hasText: reason.split('Actual evaluated native bindings: ')[0] }).first()
  await expect(feedback).toBeVisible()
  await page.screenshot({ path: info.outputPath('real-background-failure.png'), fullPage: true })
  await expect(feedback).toContainText(reason.split('Actual evaluated native bindings: ')[0])
  expect(await feedback.evaluate(element => element.clientHeight)).toBeLessThanOrEqual(288)
})
