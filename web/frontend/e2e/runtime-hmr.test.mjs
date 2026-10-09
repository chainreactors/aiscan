import assert from 'node:assert/strict'
import { test } from 'node:test'
import { fileURLToPath } from 'node:url'
import { createServer } from 'vite'
import { chromium } from '@playwright/test'

test('the actual Vite entry replaces its runtime without reloading or retaining the old React root', { timeout: 45_000 }, async () => {
  const root = fileURLToPath(new URL('..', import.meta.url))
  const server = await createServer({ root, server: { host: '127.0.0.1', port: 0 } })
  let browser
  try {
    await server.listen()
    browser = await chromium.launch()
    const page = await browser.newPage()
    const errors = []
    page.on('pageerror', error => errors.push(error.message))
    await page.route('**/api/auth/session', route => route.fulfill({ json: { authenticated: false } }))
    await page.goto(`http://127.0.0.1:${server.httpServer.address().port}`)
    await page.waitForFunction(() => window.__CYBER_APP_RUNTIME__?.auth.getSnapshot().state === 'unauthenticated')
    await page.evaluate(() => { window.savedRuntime = window.__CYBER_APP_RUNTIME__; window.hmrSentinel = 'survived' })
    const main = await server.moduleGraph.getModuleByUrl('/src/main.tsx')
    assert.ok(main?.isSelfAccepting, 'main.tsx must accept Vite HMR updates')
    await server.reloadModule(main)
    await page.waitForFunction(() => window.__CYBER_APP_RUNTIME__ !== window.savedRuntime &&
      window.__CYBER_APP_RUNTIME__?.auth.getSnapshot().state === 'unauthenticated')
    const result = await page.evaluate(async () => {
      await window.__CYBER_RUNTIME_TRANSITION__
      const previousRemaining = window.savedRuntime.diagnostics().length
      const rendererCount = window.__CYBER_APP_RUNTIME__.diagnostics().filter(item => item.name === 'react-renderer').length
      await window.__CYBER_APP_RUNTIME__.dispose()
      return { previousRemaining, rendererCount, sentinel: window.hmrSentinel, remaining: window.__CYBER_APP_RUNTIME__.diagnostics().length }
    })
    assert.deepEqual(result, { previousRemaining: 0, rendererCount: 1, sentinel: 'survived', remaining: 0 })
    assert.deepEqual(errors, [])
  } finally {
    await browser?.close()
    await server.close()
  }
})
