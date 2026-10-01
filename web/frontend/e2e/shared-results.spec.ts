import { test, expect } from '@playwright/test'
import { readFile } from 'node:fs/promises'

test('the default cyber-ui AOP panel renders and downloads native file and record outputs', async ({ page }) => {
  await page.goto('/e2e/fixtures/shared-results.html')
  await page.getByRole('button', { name: '2 Tools', exact: true }).click()
  const file = page.getByTestId('tool-media')
  await page.getByRole('button', { name: /file report.txt/ }).click()
  await expect(file).toContainText('report.txt')
  const download = page.waitForEvent('download')
  await file.getByRole('link', { name: 'Download' }).click()
  const result = await download
  expect(result.suggestedFilename()).toBe('report.txt')
  expect(await readFile((await result.path())!, 'utf8')).toBe('scanner report\nready')
  const image = page.getByTestId('record-image')
  await expect(image).toBeVisible()
  await expect.poll(() => image.evaluate((img: HTMLImageElement) => img.naturalWidth)).toBe(1)
  await expect(page.getByTestId('record-status')).toContainText('1280 × 720')
})

test('shared EASM details retain anchors, folder controls, vulnerability tabs and traffic evidence', async ({ page }) => {
  await page.goto('/e2e/fixtures/shared-results.html')
  const assets = page.getByTestId('shared-assets')
  await expect(assets.locator('#asset-shared-demo-host-demo-ip')).toBeVisible()
  await expect(assets.getByRole('link', { name: 'Link to 127.0.0.1' })).toHaveAttribute('href', '#asset-shared-demo-host-demo-ip')
  await assets.getByRole('button', { name: 'Collapse all' }).click()
  await expect(assets.getByText('report', { exact: true })).toBeHidden()
  await assets.getByRole('button', { name: 'Expand all' }).click()
  await expect(assets.getByText('report', { exact: true })).toBeVisible()
  await assets.getByRole('button', { name: /^Vulns/ }).click()
  await expect(assets.getByRole('button', { name: /^Vulns/ })).toHaveAttribute('aria-pressed', 'true')
  await assets.getByText('Details', { exact: true }).click()
  const evidence = assets.getByTestId('http-evidence')
  await expect(evidence).toContainText('GET /api/nested/report HTTP/1.1')
  await expect(evidence).toContainText('HTTP/1.1 200 OK')
  await expect(evidence).toContainText('evidence preserved')
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await assets.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
})

test('shared tool definition cards preserve usage and aliases', async ({ page }) => {
  await page.goto('/e2e/fixtures/shared-results.html')
  const card = page.getByTestId('shared-tool')
  await card.getByRole('button', { name: /scan bash Scan a target/ }).click()
  await expect(card).toContainText('!scan -i TARGET')
  await expect(card).toContainText('!inspect')
})
