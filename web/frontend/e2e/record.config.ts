import { defineConfig } from '@playwright/test'
import { fileURLToPath } from 'node:url'

const baseURL = process.env.BASE_URL || 'http://127.0.0.1:38119'

export default defineConfig({
  testDir: '.', testMatch: ['record.spec.ts', 'shared-results.spec.ts', 'traffic.spec.ts'], timeout: 60000,
  expect: { timeout: 10000 }, workers: 1, retries: 0, reporter: 'list',
  outputDir: '../../../.runlogs/record-ui/playwright',
  webServer: process.env.BASE_URL ? undefined : {
    cwd: fileURLToPath(new URL('..', import.meta.url)),
    command: 'node node_modules/vite/bin/vite.js --host 127.0.0.1 --port 38119 --strictPort',
    url: baseURL, reuseExistingServer: false,
  },
  use: { baseURL, headless: true, viewport: { width: 1280, height: 900 }, screenshot: 'only-on-failure', trace: 'retain-on-failure' },
})
