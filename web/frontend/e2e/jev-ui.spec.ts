import { test, expect } from '@playwright/test'
import { create, toBinary } from '@bufbuild/protobuf'
import { EventSchema } from '../cyber-ui/packages/aop/src/gen/aop/event_pb'

for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }]) {
  for (const theme of ['light', 'dark']) {
    test(`JEV timeline and Reflex menu ${viewport.width} ${theme}`, async ({ page }, info) => {
      const errors: string[] = []
      page.on('pageerror', error => errors.push(error.message))
      await page.setViewportSize(viewport)
      await page.addInitScript(({ theme }) => {
        localStorage.setItem('cyber-locale', 'zh')
        document.addEventListener('DOMContentLoaded', () => document.documentElement.classList.toggle('dark', theme === 'dark'), { once: true })
      }, { theme })
      await page.goto('/e2e/fixtures/jev.html')
      await expect(page.getByTestId('agent-workflow')).toHaveCount(1)
      await expect(page.getByTestId('agent-workflow')).toContainText('已交还 LLM')
      await page.locator('[data-control-node][data-kind=tool][data-control-stage=execution]').first().click()
      await expect(page.locator('[data-control-current=true]')).toContainText('playwright open')
      await page.screenshot({ path: info.outputPath('timeline.png'), fullPage: true })
      await page.getByRole('button', { name: 'Reflex', exact: true }).click()
      await expect(page.getByRole('tab', { name: '运行网络' })).toHaveAttribute('aria-selected', 'true')
      await expect(page.locator('.react-flow__node')).toHaveCount(4)
      await expect(page.locator('.react-flow__node').filter({ hasText: 'playwright' })).toBeVisible()
      await expect.poll(async () => (await page.getByRole('dialog').boundingBox())?.x).toBe(viewport.width < 768 ? 0 : viewport.width * 0.25)
      await expect.poll(async () => page.locator('.react-flow__node').first().evaluate(node => node.getBoundingClientRect().width)).toBeGreaterThan(150)
      await expect.poll(async () => page.getByTestId('reflex-network').evaluate(network => {
        const bounds = network.getBoundingClientRect()
        return [...network.querySelectorAll('.react-flow__node')].every(node => {
          const rect = node.getBoundingClientRect()
          return rect.left >= bounds.left && rect.right <= bounds.right && rect.top >= bounds.top && rect.bottom <= bounds.bottom
        })
      })).toBe(true)
      // Real event streams keep updating after the graph has been measured.
      // Replacing projected nodes must preserve their measured dimensions.
      for (let seq = 20; seq < 23; seq++) {
        const update = create(EventSchema, { id: `late-${seq}`, seq: BigInt(seq), sessionId: 'session-1', turnId: 'turn-1',
          payload: { case: 'turnEnded', value: { stopReason: 'completed' } } })
        await page.evaluate(value => (window as any).renderJEVEvents([value], true), [...toBinary(EventSchema, update)])
        for (const node of await page.locator('.react-flow__node').all()) await expect(node).toBeVisible()
      }
      await page.mouse.move(0, 0)
      await expect(page.getByRole('tooltip')).toHaveCount(0)
      await page.screenshot({ path: info.outputPath('network.png'), fullPage: true })
      await page.getByRole('tab', { name: 'Reflex 库' }).click()
      await expect(page.getByPlaceholder('搜索 Reflex / Claim')).toBeVisible()
      await expect(page.getByRole('dialog').getByText('判断策略', { exact: true })).toBeVisible()
      await expect(page.getByRole('dialog').getByText('inspect', { exact: true })).toBeVisible()
      await expect(page.getByRole('dialog')).toContainText('document.body.innerText')
      await page.screenshot({ path: info.outputPath('library.png'), fullPage: true })
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
      expect(errors).toEqual([])
    })
  }
}
