import { defineConfig } from '@playwright/test'
import { fileURLToPath } from 'node:url'
import { uiTestFiles } from './suites'

// Source fixtures require Vite; the embedded server serves the production build.
const port = process.env.CYBER_UI_TEST_PORT || '38185'
const baseURL = process.env.UI_TEST_BASE_URL || `http://127.0.0.1:${port}`
export default defineConfig({
  testDir: '.',
  testMatch: uiTestFiles,
  timeout: 45_000, expect: { timeout: 8_000 }, workers: 1,
  outputDir: '../test-results/ui',
  reporter: [['list'], ['html', { outputFolder: '../playwright-report/ui', open: 'never' }]],
  webServer: process.env.UI_TEST_BASE_URL ? undefined : {
    command: `node node_modules/vite/bin/vite.js --host 127.0.0.1 --port ${port} --strictPort`,
    cwd: fileURLToPath(new URL('..', import.meta.url)), url: baseURL, reuseExistingServer: false,
  },
  use: { baseURL, headless: true, viewport: { width: 1440, height: 900 },
    screenshot: 'only-on-failure', trace: 'retain-on-failure' },
})
