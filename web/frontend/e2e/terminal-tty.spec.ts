import { expect, test, type Page } from '@playwright/test'

const token = process.env.ACCESS_KEY || 'test-token'

async function screen(page: Page) {
  return (await page.locator('.xterm-rows').innerText()).split('\n').map(line => line.trimEnd())
}

async function clearEditor(page: Page) {
  const input = page.getByRole('textbox', { name: 'Terminal input' })
  await input.press('Control+u')
  await input.press('Control+l')
  await expect.poll(async () => (await screen(page)).filter(line => line.trim())).toEqual(['aiscan ❯'])
  return input
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('cyber-locale', 'en')
    localStorage.setItem('cyber-interface-guide-seen', 'true')
  })
  expect((await page.request.post('/api/auth/login', { data: { token } })).ok()).toBe(true)
  await page.goto('/')
  await page.getByRole('button', { name: /agent\(s\) connected/ }).click()
  await page.getByRole('dialog', { name: 'Agent Console', exact: true })
    .getByRole('button').filter({ has: page.getByText('e2e-node', { exact: true }) }).click()
  await expect(page.locator('.xterm-rows')).toContainText('aiscan')
})

test('short REPL input stays on the prompt row and retains the editing cursor', async ({ page }) => {
  const input = await clearEditor(page)
  await input.pressSequentially('!echo TTY_END')
  await expect.poll(async () => (await screen(page)).filter(line => line.trim())).toEqual(['aiscan ❯ !echo TTY_END'])
  for (let i = 0; i < 3; i++) await input.press('ArrowLeft')
  await input.pressSequentially('KEPT_')
  await expect.poll(async () => (await screen(page)).filter(line => line.trim())).toEqual(['aiscan ❯ !echo TTY_KEPT_END'])
  await input.press('Enter')
  await expect.poll(() => screen(page)).toContain('TTY_KEPT_END')
  await expect.poll(() => screen(page)).toContain('aiscan ❯ !echo TTY_KEPT_END')
})

test('resizing a wrapped REPL draft preserves its text and cursor', async ({ page }) => {
  const input = await clearEditor(page)
  const word = `RESIZE_${'x'.repeat(60)}_END`
  await input.pressSequentially(`!echo ${word}`)
  const compact = async () => (await screen(page)).join('').replace(/\s/g, '')
  await expect.poll(compact).toBe(`aiscan❯!echo${word}`)
  for (const width of [390, 820, 1280]) {
    await page.setViewportSize({ width, height: 844 })
    await expect.poll(compact).toBe(`aiscan❯!echo${word}`)
  }
  for (let i = 0; i < 3; i++) await input.press('ArrowLeft')
  await input.pressSequentially('KEPT_')
  const edited = word.replace('_END', '_KEPT_END')
  await expect.poll(compact).toBe(`aiscan❯!echo${edited}`)
  await input.press('Enter')
  await expect.poll(compact).toBe(`aiscan❯!echo${edited}${edited}aiscan❯`)
})

test('async output preserves the next REPL draft while a task finishes', async ({ page }) => {
  const input = await clearEditor(page)
  await input.pressSequentially('Reply with exactly one word: PONG')
  await input.press('Enter')
  await expect(page.locator('.xterm-rows')).toContainText('talking')
  await input.pressSequentially('!echo ASYNC_END')
  for (let i = 0; i < 3; i++) await input.press('ArrowLeft')
  await expect.poll(() => screen(page)).toContain('PONG')
  await expect(page.locator('.xterm-rows')).toContainText('!echo ASYNC_END')
  await input.pressSequentially('KEPT_')
  await input.press('Enter')
  await expect.poll(() => screen(page)).toContain('ASYNC_KEPT_END')
})

test('shell PTY supports command editing and Ctrl+C after resizing', async ({ page }) => {
  await page.getByRole('button', { name: 'New shell PTY', exact: true }).click()
  await expect(page.getByRole('dialog', { name: 'Agent Console', exact: true })).toContainText('shell-e2e-node')
  const input = page.getByRole('textbox', { name: 'Terminal input' })
  await input.pressSequentially('echo SHELL_END')
  for (let i = 0; i < 3; i++) await input.press('ArrowLeft')
  await input.pressSequentially('KEPT_')
  await input.press('Enter')
  await expect.poll(() => screen(page)).toContain('SHELL_KEPT_END')
  await page.setViewportSize({ width: 820, height: 844 })
  await input.pressSequentially('echo SHOULD_BE_CANCELED')
  await input.press('Control+c')
  await input.pressSequentially('echo SHELL_RECOVERED')
  await input.press('Enter')
  await expect.poll(() => screen(page)).toContain('SHELL_RECOVERED')
  await input.pressSequentially('exit')
  await input.press('Enter')
  await page.getByRole('button', { name: /Main REPL/ }).click()
  await expect(page.locator('.xterm-rows')).toContainText('aiscan')
})
