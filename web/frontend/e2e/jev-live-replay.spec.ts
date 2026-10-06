import { test, expect } from '@playwright/test'
import { showExecutionLanes } from './jev-helpers'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { createRegistry, fromJson, toBinary } from '@bufbuild/protobuf'
import { EventSchema } from '../cyber-ui/packages/aop/src/gen/aop/event_pb'
import { RuntimeEventSchema } from '../src/gen/types/jev_pb'
import { file_types_chat } from '../src/gen/types/chat_pb'
import { file_types_agent } from '../src/gen/types/agent_pb'
import { file_aop_operation_protocol } from '../cyber-ui/packages/aop/src/gen/aop/operation/protocol_pb'
import { file_aop_file_protocol } from '../cyber-ui/packages/aop/src/gen/aop/file/protocol_pb'
import { file_aop_pty_protocol } from '../cyber-ui/packages/aop/src/gen/aop/pty/protocol_pb'
import { projectJEV } from '../src/lib/jev-view'

test('retained real events render their exact selections, publications and chronology', async ({ page }, info) => {
  const path = process.env.JEV_EVENTS_FILE || fileURLToPath(new URL('./fixtures/jev-history/live-events.json', import.meta.url))
  const registry = createRegistry(RuntimeEventSchema, file_types_chat, file_types_agent, file_aop_operation_protocol, file_aop_file_protocol, file_aop_pty_protocol)
  const events = JSON.parse(readFileSync(path!, 'utf8')).map((delivery: any) => fromJson(EventSchema, delivery.event, { registry }))
  const projection = projectJEV(events)
  expect(projection.records.length).toBeGreaterThan(0)
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  await page.addInitScript(() => localStorage.setItem('cyber-locale', 'zh'))
  await page.goto('/e2e/fixtures/jev.html')
  const binary = events.map((event: any) => [...toBinary(EventSchema, event)])
  await page.evaluate(values => (window as any).renderJEVEvents(values), [...binary, ...binary])
  await showExecutionLanes(page)
  await expect(page.getByTestId('jev-check')).toHaveCount(0)
  await expect(page.getByTestId('jev-compilation')).toHaveCount(0)
  const foreground = page.getByTestId('agent-workflow').first()
  if (projection.checks.length) {
    await expect(foreground).toBeVisible()
    await foreground.screenshot({ path: info.outputPath('real-foreground-decisions.png') })
  }
  // Swimlane headers and records share the board; verify the records themselves.
  for (const card of await page.getByTestId('agent-workflow').all()) {
    const lanes = await card.locator('[data-event-seq]').evaluateAll(nodes => {
      const groups: Record<string, number[]> = {}
      for (const node of nodes) (groups[node.getAttribute('data-workflow-lane')!] ||= []).push(Number(node.getAttribute('data-event-seq')))
      return Object.values(groups)
    })
    expect(lanes.length).toBeGreaterThan(0)
    for (const seqs of lanes) expect(seqs).toEqual([...seqs].sort((a, b) => a - b))
  }
  for (const record of projection.records) {
    if (record.value.payload.case !== 'decisionResult') continue
    const result = record.value.payload.value
    const request = projection.records.find(candidate => candidate.value.payload.case === 'decisionRequest' && candidate.value.payload.value.requestId === result.requestId)
    if (request) await page.locator(`[data-record-id="${request.event.id}"]`).click()
    const batch = page.locator(`[data-request-id="${result.requestId}"]`)
    for (const [id, answer] of Object.entries(result.evaluations)) {
      if (answer.value.case !== 'choice') continue
      await expect(batch.locator(`[data-question-id="${id}"] [data-option-id="${answer.value.value}"]`)).toHaveAttribute('data-selected', 'true')
      await expect(batch.locator(`[data-question-id="${id}"] [data-option-id="${answer.value.value}"]`)).toBeVisible()
    }
  }
  const publications = projection.records.filter(record => record.value.payload.case === 'libraryChange' && record.value.payload.value.state === 'claim_published').length
  expect(projection.records.filter(record => record.value.payload.case === 'libraryChange' && record.value.payload.value.claim).length).toBeGreaterThanOrEqual(publications)
  await expect(page.getByText('评审与归纳 · 发布后可供后续任务使用')).toHaveCount(0)
  expect(await page.locator('.chat-panel > div').evaluateAll(nodes => nodes.every(node => node.scrollWidth <= node.clientWidth + 1))).toBe(true)
  await page.screenshot({ path: info.outputPath('real-connected-loop.png'), fullPage: true })
  expect(errors).toEqual([])
})
