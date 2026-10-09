import { expect, test } from '@playwright/test'

const fixtureURL = `http://127.0.0.1:${process.env.CYBER_UI_TEST_PORT || '38185'}`

test.beforeEach(async ({ page }) => {
  await page.goto(`${fixtureURL}/e2e/fixtures/terminal-rendering.html`)
  await page.waitForFunction(() => !!(window as any).terminalFixture)
})

test('PTY output preserves UTF-8 characters across byte-sized frames', async ({ page }) => {
  const line = await page.evaluate(async () => {
    const { terminal, write } = (window as any).terminalFixture
    for (const byte of new TextEncoder().encode('中文🧪')) write([byte])
    await new Promise<void>(resolve => terminal.write(new Uint8Array(), resolve))
    return terminal.buffer.active.getLine(0).translateToString(true)
  })
  expect(line).toBe('中文🧪')
})

test('raw PTY line feeds preserve the column instead of injecting carriage returns', async ({ page }) => {
  const lines = await page.evaluate(async () => {
    const { terminal, write } = (window as any).terminalFixture
    write(Array.from(new TextEncoder().encode('abc\nZ')))
    await new Promise<void>(resolve => terminal.write(new Uint8Array(), resolve))
    return [0, 1].map(row => terminal.buffer.active.getLine(row).translateToString(true))
  })
  expect(lines).toEqual(['abc', '   Z'])
})

test('terminal is measured before the caller can attach a PTY', async ({ page }) => {
  const size = await page.evaluate(() => {
    const { readyCols, measuredCols } = (window as any).terminalFixture
    return { readyCols, measuredCols }
  })
  expect(size.readyCols).toBe(size.measuredCols)
  expect(size.readyCols).toBeGreaterThan(2)
  expect(size.readyCols).toBeLessThan(80)
})
