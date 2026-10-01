import { expect, test, type APIRequestContext, type Page } from '@playwright/test'

const token = process.env.ACCESS_KEY || 'test-token'
let original: { jev: Record<string, unknown>; guardrail: Record<string, unknown> } | undefined

// Support both sidebar layouts: this suite verifies admission, not task navigation.
async function newTask(page: Page, node: string) {
  const direct = page.getByRole('button', { name: 'New task on ' + node, exact: true })
  if (await direct.count()) { await direct.click(); return }
  await page.getByRole('button', { name: 'Nodes', exact: true }).click()
  const group = page.getByRole('button').filter({ has: page.getByText(node, { exact: true }) }).first()
  await group.locator('..').getByRole('button', { name: 'New', exact: true }).click()
}

async function rpc(request: APIRequestContext, service: string, method: string, data: object) {
  const response = await request.post('/cyber.rpc.' + service + '/' + method, {
    headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json', 'Connect-Protocol-Version': '1' },
    data, timeout: 20_000,
  })
  expect(response.ok(), method + ' must succeed').toBeTruthy()
  return response.json()
}

// Teardown has its own timeout and restores both provider policy and interaction mode.
test.afterEach(async ({ request }) => {
  if (!original) return
  test.setTimeout(30_000)
  const node = process.env.CYBER_E2E_NODE || 'e2e-node'
  const agent = async () => (await rpc(request, 'agent.AgentService', 'ListAgents', {})).agents.find((item: any) => item.hello?.nodeId === node)
  const before = await agent()
  await rpc(request, 'config.ConfigService', 'UpdateConfig', { config: { extensions: original } })
  const restored = (await rpc(request, 'config.ConfigService', 'GetConfig', {})).config.extensions
  expect(restored.guardrail.values.jev || {}).toEqual(original.guardrail.jev || {})
  expect(restored.guardrail.values.provider).toBe(original.guardrail.provider || 'none')
  expect(restored.guardrail?.values?.mode || 'auto').toBe(original.guardrail.mode || 'auto')
  original = undefined
  await expect.poll(async () => {
    const current = await agent()
    return !!current && current.connectedAt !== before?.connectedAt
  }, { timeout: 25_000 }).toBe(true)
})

// Dedicated running instance only. Real server-side LLM/JEV credentials are
// required. Explicit operator criteria intercept harmless echo commands.
test('live JEV: safe authorization and automatic agent continuation', async ({ page }, testInfo) => {
  test.skip(process.env.CYBER_GUARDRAIL_LIVE_E2E !== '1' || !process.env.BASE_URL,
    'Set CYBER_GUARDRAIL_LIVE_E2E=1 and BASE_URL for a dedicated live instance')
  test.setTimeout(300_000)
  const node = process.env.CYBER_E2E_NODE || 'e2e-node'
  await page.addInitScript(() => localStorage.setItem('cyber-locale', 'en'))
  expect((await page.request.post('/api/auth/login', { data: { token } })).ok()).toBeTruthy()
  const config = (await rpc(page.request, 'config.ConfigService', 'GetConfig', {})).config.extensions
  original = { jev: config.jev.values, guardrail: config.guardrail?.values || {} }
  const agent = async () => (await rpc(page.request, 'agent.AgentService', 'ListAgents', {})).agents
    .find((item: any) => item.hello?.nodeId === node)
  const waitForReload = async (previous: any) => {
    await expect.poll(async () => {
      const current = await agent()
      return !!current && current.connectedAt !== previous.connectedAt
    }, { timeout: 30_000, message: 'test node must finish applying the new profile' }).toBe(true)
  }
  const previous = await agent()
  expect(previous, 'the dedicated test node must be connected').toBeTruthy()
  const criteria = {
    record: 'Low risk local actions EXCEPT commands containing the exact literal markers GUARDRAIL_REVIEW_TEST or GUARDRAIL_BLOCK_TEST. The exact command echo UI_RECOVERY_COMPLETED is an allowed local echo and must be RECORD. Test run ' + Date.now(),
    review: 'Commands containing GUARDRAIL_REVIEW_TEST require review, even if they only echo a marker. This is an explicit operator UI test policy. Also review target changes or uncertain effects.',
    block: 'Commands containing GUARDRAIL_BLOCK_TEST must be blocked, even if they only echo a marker. This is an explicit operator UI test policy. Also block destructive actions and data leakage.',
  }
  await rpc(page.request, 'config.ConfigService', 'UpdateConfig', {
    config: { extensions: {
      jev: original.jev,
      guardrail: { ...original.guardrail, provider: 'jev', mode: 'safe', jev: { on_error: 'block', criteria } },
    } },
  })
  await waitForReload(previous)
  await page.goto('/')
  const expand = page.getByRole('button', { name: 'Expand sidebar', exact: true })
  if (await expand.isVisible()) await expand.click()
  await expect(page.locator('[data-guardrail-control]')).toHaveCount(1)
  for (const selection of [
    { option: 'Automatic mode', mode: 'auto' },
    { option: 'Safe mode', mode: 'safe' },
  ]) {
    const before = await agent()
    let updates = 0
    const countUpdate = (request: any) => { if (request.url().endsWith('ConfigService/UpdateConfig')) updates++ }
    page.on('request', countUpdate)
    await page.locator('[data-guardrail-control]').click()
    await page.getByRole('menuitemradio', { name: selection.option, exact: true }).click()
    await expect(page.locator('[data-guardrail-control]')).toHaveAccessibleName('Guardrail: ' + selection.option)
    await expect(page.getByRole('menu')).toHaveCount(0)
    await expect(page.locator('[data-guardrail-control]')).toHaveAttribute('aria-busy', 'false')
    expect((await agent()).connectedAt, 'mode update must preserve the node connection').toBe(before.connectedAt)
    page.off('request', countUpdate)
    expect(updates, 'changing mode applies one configuration update').toBe(1)
    const saved = (await rpc(page.request, 'config.ConfigService', 'GetConfig', {})).config.extensions
    expect(saved.guardrail.values.provider).toBe('jev')
    expect(saved.guardrail.values.mode).toBe(selection.mode)
    expect(saved.guardrail.values.jev.criteria).toEqual(criteria)
    await expect(page.locator('[data-guardrail-control]')).toHaveCount(1)
  }
  const results: object[] = []
  let mode = 'safe'
  for (const scenario of [
    { mode: 'safe', risk: 'REVIEW', action: 'approve' },
    { mode: 'safe', risk: 'BLOCK', action: 'approve' },
    { mode: 'safe', risk: 'BLOCK', action: 'reject' },
    { mode: 'auto', risk: 'REVIEW', action: 'approve' },
    { mode: 'auto', risk: 'BLOCK', action: 'approve' },
  ]) {
    if (scenario.mode !== mode) {
      const before = await agent()
      await page.getByRole('button', { name: 'Guardrail: Safe mode', exact: true }).click()
      await page.getByRole('menuitemradio', { name: 'Automatic mode', exact: true }).click()
      await expect(page.locator('[data-guardrail-control]')).toHaveAttribute('aria-busy', 'false')
      expect((await agent()).connectedAt).toBe(before.connectedAt)
      await expect(page.getByRole('button', { name: 'Guardrail: Automatic mode', exact: true })).toBeVisible()
      const saved = (await rpc(page.request, 'config.ConfigService', 'GetConfig', {})).config.extensions
      expect(saved.guardrail.values.mode).toBe('auto')
      expect(saved.guardrail.values.jev.criteria).toEqual(criteria)
      mode = scenario.mode
    }
    const previousURL = page.url()
    await newTask(page, node)
    await page.waitForURL(url => url.href !== previousURL && /^\/sessions\/[^/]+$/.test(url.pathname), { timeout: 15_000 })
    const marker = 'GUARDRAIL_' + scenario.risk + '_TEST_' + scenario.mode.toUpperCase() + '_' + scenario.action.toUpperCase()
    const recovery = 'UI_RECOVERY_COMPLETED'
    const prompt = scenario.action === 'approve' || scenario.mode === 'safe'
      ? 'Controlled UI test. Call bash exactly once with command = ' + JSON.stringify('echo ' + marker) + '. Do not call other tools, change the command or retry after a denial. Wait for human authorization if requested. After the tool returns, respond only TEST_FINISHED.'
      : 'Controlled UI test. First call bash exactly once with command = ' + JSON.stringify('echo ' + marker) + '. If the guardrail intercepts it, do not retry that command. Instead call bash once with command = ' + JSON.stringify('echo ' + recovery) + '. After the second result, respond only TEST_FINISHED. Use no other tools.'
    await page.getByRole('textbox', { name: 'Your goal', exact: true }).fill(prompt)
    await page.getByRole('button', { name: 'Send message', exact: true }).click()
    const sessionId = new URL(page.url()).pathname.split('/').pop()!
    const events = async () => (await rpc(page.request, 'chat.SessionService', 'ListEvents', { sessionId })).events.map((item: any) => item.event)
    const review = page.getByRole('region', { name: 'Tool approval required', exact: true })
    const transcriptOrder = () => page.locator('[data-guardrail-turn-steps] > *')
      .evaluateAll(nodes => nodes.map(node => node.hasAttribute('data-guardrail-turn-step') ? 'approval' : 'assistant'))
    const name = scenario.mode + '-' + scenario.risk.toLowerCase() + '-' + scenario.action
    if (scenario.mode === 'safe') {
      await expect(review).toBeVisible({ timeout: 45_000 })
      await expect.poll(transcriptOrder).toEqual(['assistant', 'approval'])
      await expect(page.getByTestId('assistant-response')).toHaveCount(1)
      await expect(page.locator('[data-guardrail-turn]')).toHaveCount(1)
      await expect(page.locator('[data-guardrail-timeline-entry]')).toHaveCount(0)
      await expect(page.getByTestId('assistant-response').locator('[data-guardrail-review]')).toHaveCount(1)
      await expect(review).toContainText('GUARDRAIL_' + scenario.risk + '_TEST')
      expect((await events()).filter((event: any) => event.toolResult)).toHaveLength(0)
      expect(await review.evaluate(element => getComputedStyle(element).position)).toBe('static')
      await expect(review.locator('[data-guardrail-command] pre')).toHaveText('echo ' + marker)
      await expect(review.locator('details')).not.toHaveAttribute('open', '')
      const row = page.locator('[data-session-id="' + sessionId + '"]')
      await expect(row).toHaveAttribute('data-needs-attention', 'true')
      await expect(row.getByRole('status', { name: '1 pending approvals', exact: true })).toBeVisible()
      if (scenario.risk === 'REVIEW') {
        // A cold reload outside this session must still discover its pending badge.
        await page.goto('/')
        await page.reload()
        await expect(row).toHaveAttribute('data-needs-attention', 'true')
        await expect(row.getByRole('status', { name: '1 pending approvals', exact: true })).toBeVisible()
        await row.getByRole('button').first().click()
        await expect(review).toBeVisible()
        await expect(review.locator('[data-guardrail-command] pre')).toHaveText('echo ' + marker)
      }
      const before = await review.textContent()
      await page.screenshot({ path: testInfo.outputPath(name + '-pending.png') })
      await page.reload()
      await expect(review).toBeVisible()
      expect(await review.textContent()).toBe(before)
      const resolveButton = page.getByRole('button', { name: scenario.action === 'approve' ? 'Authorize and continue' : 'Reject', exact: true })
      if (scenario.risk === 'REVIEW') {
        // Two immediate clicks before React re-renders must submit only once.
        await resolveButton.evaluate(button => { (button as HTMLButtonElement).click(); (button as HTMLButtonElement).click() })
      } else await resolveButton.click()
      await expect(review).toHaveCount(0)
      const record = page.locator('[data-guardrail-review]')
      await expect(record).toHaveCount(1)
      await expect(record).toHaveAttribute('data-guardrail-state', scenario.action === 'approve' ? 'approved' : 'rejected')
      await expect(record.locator('[data-guardrail-command-preview]')).toHaveText('echo ' + marker)
      await expect(record.locator('details').first()).not.toHaveAttribute('open', '')
      await record.locator('summary').first().click()
      await expect(record.locator('[data-guardrail-command] pre')).toBeVisible()
      await expect(record.locator('[data-guardrail-command] pre')).toHaveText('echo ' + marker)
      await record.locator('summary').first().click()
      await expect(record.locator('[data-guardrail-resolved-at]')).toBeVisible()
      await expect(record.getByRole('button', { name: /Authorize|Reject/ })).toHaveCount(0)
      await expect(row).not.toHaveAttribute('data-needs-attention', 'true')
    }
    await expect.poll(async () => (await events()).some((event: any) => event.turnEnded),
      { timeout: 60_000, intervals: [500, 1000, 2000] }).toBe(true)
    const history = await events()
    const calls = history.filter((event: any) => event.toolCall)
    const outputs = history.filter((event: any) => event.toolResult).map((event: any) => event.toolResult)
    const count = 1
    expect(calls).toHaveLength(count)
    expect(outputs).toHaveLength(count)
    expect(!!outputs[0].isError).toBe(scenario.action !== 'approve')
    const outputText = (output: any) => output.output.map((content: any) => content.text?.text || '').join('')
    if (scenario.action === 'approve') expect(outputText(outputs[0]).trim()).toBe(marker)
    else {
      expect(outputText(outputs[0])).toContain('operation denied')
      expect(outputText(outputs[0]).trim()).not.toBe(marker)
      expect(outputText(outputs[0])).toContain(scenario.mode === 'auto' ? 'was not executed' : 'REJECTED')
    }
    if (scenario.action === 'approve') await expect(page.locator('[data-guardrail-outcome="succeeded"]')).toBeVisible()
    await expect(page.locator('[data-guardrail-turn-summary]')).toHaveAttribute('data-guardrail-count', '1')
    if (scenario.mode === 'auto') {
      const record = page.locator('[data-guardrail-review]')
      await expect(record).toHaveAttribute('data-guardrail-state', 'approved')
      await expect(record).toContainText('Automatically allowed')
      await record.locator('summary').first().click()
      await expect(record.locator('[data-guardrail-resolution-source]')).toHaveAttribute('data-guardrail-resolution-source', 'auto')
      await expect(record.locator('[data-guardrail-reason]')).toHaveCount(2)
      await record.locator('summary').first().click()
      const audits = history.filter((event: any) => event.extension?.['@type']?.endsWith('/cyber.guardrail.Review'))
      expect(audits).toHaveLength(1)
      expect(audits[0].extension.resolutionSource).toBe('auto')
      expect(audits[0].extension.state).toBe('REVIEW_STATE_APPROVED')
    }
    await expect(review).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Authorize and continue', exact: true })).toHaveCount(0)
    for (const tools of await page.getByRole('button', { name: /^1 Tool$/i }).all()) await tools.click()
    await page.getByRole('button', { name: 'bash echo ' + marker, exact: true }).click()
    await expect(page.getByText(scenario.action === 'approve' ? marker : /operation denied/, { exact: scenario.action === 'approve' })).toBeVisible()
    await page.screenshot({ path: testInfo.outputPath(name + '.png') })
    await page.reload()
    await expect(page.getByRole('button', { name: 'Open settings', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Authorize and continue', exact: true })).toHaveCount(0)
    if (scenario.mode === 'auto') {
      await expect(page.locator('[data-guardrail-review]')).toHaveAttribute('data-guardrail-state', 'approved')
      await expect(page.locator('[data-guardrail-turn-summary]')).toHaveAttribute('data-guardrail-count', '1')
    }
    if (scenario.mode === 'safe') {
      const record = page.locator('[data-guardrail-review]')
      await expect.poll(transcriptOrder).toEqual(['assistant', 'approval', 'assistant'])
      await expect(record).toHaveCount(1)
      await expect(record).toHaveAttribute('data-guardrail-state', scenario.action === 'approve' ? 'approved' : 'rejected')
      const resolvedAt = await record.locator('[data-guardrail-resolved-at]').getAttribute('datetime')
      expect(resolvedAt).toBeTruthy()
      const reviewEvents = history.filter((event: any) => event.extension?.['@type']?.endsWith('/cyber.guardrail.Review'))
      expect(reviewEvents.map((event: any) => event.extension.state)).toEqual(['REVIEW_STATE_PENDING', scenario.action === 'approve' ? 'REVIEW_STATE_APPROVED' : 'REVIEW_STATE_REJECTED'])
      await page.goto('/')
      await page.locator('[data-session-id="' + sessionId + '"]').getByRole('button').first().click()
      await expect(record).toHaveCount(1)
      await expect(record.locator('[data-guardrail-resolved-at]')).toHaveAttribute('datetime', resolvedAt!)
      await expect.poll(transcriptOrder).toEqual(['assistant', 'approval', 'assistant'])
      await expect(record.getByRole('button', { name: /Authorize|Reject/ })).toHaveCount(0)
      await expect(page.locator('[data-session-id="' + sessionId + '"]')).not.toHaveAttribute('data-needs-attention', 'true')
    }
    results.push({ ...scenario, sessionId, calls: calls.length, results: outputs.length })
    console.log('Live guardrail verified:', name, sessionId)
  }
  await testInfo.attach('live-guardrail-results', { body: JSON.stringify(results, null, 2), contentType: 'application/json' })
})


test('live JEV: multiple turns retain review records and independent counts', async ({ page }, testInfo) => {
  test.skip(process.env.CYBER_GUARDRAIL_LIVE_E2E !== '1' || !process.env.BASE_URL,
    'Set CYBER_GUARDRAIL_LIVE_E2E=1 and BASE_URL for a dedicated live instance')
  test.setTimeout(180_000)
  const node = process.env.CYBER_E2E_NODE || 'e2e-node'
  await page.addInitScript(() => localStorage.setItem('cyber-locale', 'en'))
  expect((await page.request.post('/api/auth/login', { data: { token } })).ok()).toBeTruthy()
  const extensions = (await rpc(page.request, 'config.ConfigService', 'GetConfig', {})).config.extensions
  original = { jev: extensions.jev.values, guardrail: extensions.guardrail?.values || {} }
  const agent = async () => (await rpc(page.request, 'agent.AgentService', 'ListAgents', {})).agents.find((item: any) => item.hello?.nodeId === node)
  const before = await agent()
  await rpc(page.request, 'config.ConfigService', 'UpdateConfig', { config: { extensions: {
    jev: { ...original.jev, enabled: true, on_error: 'block', criteria: {
      record: 'Allow low-risk local commands except commands containing GUARDRAIL_MULTI_TEST.',
      review: 'Every command containing GUARDRAIL_MULTI_TEST must require review, even harmless echo. Explicit operator UI test policy. Run ' + Date.now(),
      block: 'Block destructive actions and data leakage.',
    } },
    guardrail: { ...original.guardrail, mode: 'safe' },
  } } })
  await expect.poll(async () => {
    const current = await agent()
    return !!current && current.connectedAt !== before?.connectedAt
  }, { timeout: 30_000 }).toBe(true)
  let transportOffline = false
  const sockets: Array<{ close: () => void }> = []
  await page.routeWebSocket('**/api/aop/application/ws', socket => {
    if (transportOffline) { socket.close(); return }
    socket.connectToServer()
    sockets.push(socket)
  })
  await page.goto('/')
  const expand = page.getByRole('button', { name: 'Expand sidebar', exact: true })
  if (await expand.isVisible()) await expand.click()
  await newTask(page, node)
  await page.waitForURL(/\/sessions\/[^/?]+/, { timeout: 15_000 })
  const firstCommand = 'echo GUARDRAIL_MULTI_TEST_FIRST'
  const secondCommand = 'echo GUARDRAIL_MULTI_TEST_SECOND'
  await page.getByRole('textbox', { name: 'Your goal', exact: true }).fill(
    'Controlled UI test. Make exactly two SEQUENTIAL bash calls in this single turn. First command = ' + JSON.stringify(firstCommand)
    + '. Wait for its result. Then second command = ' + JSON.stringify(secondCommand)
    + '. Wait for human authorization when requested. After second result (success or denial), respond only MULTI_TEST_FINISHED. Do not retry or use other tools.')
  await page.getByRole('button', { name: 'Send message', exact: true }).click()
  const sessionId = new URL(page.url()).pathname.split('/').pop()!
  const events = async () => (await rpc(page.request, 'chat.SessionService', 'ListEvents', { sessionId })).events.map((item: any) => item.event)
  const bubble = page.getByTestId('assistant-response')
  const pending = page.locator('[data-guardrail-state="pending"]')
  await expect(pending).toHaveCount(1, { timeout: 45_000 })
  await expect(pending.locator('[data-guardrail-command] pre')).toHaveText(firstCommand)
  const connectedBeforeSwitch = await agent()
  for (const option of ['Automatic mode', 'Safe mode']) {
    await page.locator('[data-guardrail-control]').click()
    await page.getByRole('menuitemradio', { name: option, exact: true }).click()
    await expect(page.locator('[data-guardrail-control]')).toHaveAccessibleName('Guardrail: ' + option)
    await expect(page.getByRole('menu')).toHaveCount(0)
    await expect(page.locator('[data-guardrail-control]')).toHaveAttribute('aria-busy', 'false')
    await expect(pending).toHaveCount(1)
    expect((await events()).filter((event: any) => event.toolResult)).toHaveLength(0)
    expect((await agent()).connectedAt).toBe(connectedBeforeSwitch.connectedAt)
  }
  await bubble.locator('[data-guardrail-turn-summary]').click()
  await expect(pending.getByRole('button', { name: 'Authorize and continue', exact: true })).toBeInViewport()
  const attention = page.locator('[data-session-id="' + sessionId + '"]')
  transportOffline = true
  for (const socket of sockets) socket.close()
  await expect(pending).toContainText('Connection interrupted', { timeout: 15_000 })
  await expect(pending.getByRole('button', { name: 'Authorize and continue', exact: true })).toBeDisabled()
  await expect(pending.getByRole('button', { name: 'Reject', exact: true })).toBeDisabled()
  await expect(attention).toHaveAttribute('data-needs-attention', 'true')
  await expect(bubble.locator('[data-guardrail-turn-summary]')).toHaveAttribute('data-guardrail-pending-count', '1')
  transportOffline = false
  await expect(pending.getByRole('button', { name: 'Authorize and continue', exact: true })).toBeEnabled({ timeout: 20_000 })
  await expect(pending).not.toContainText('Connection interrupted')
  const firstOperation = await pending.getAttribute('data-guardrail-review')
  const turnID = await bubble.locator('[data-guardrail-turn]').getAttribute('data-guardrail-turn')
  await pending.getByRole('button', { name: 'Authorize and continue', exact: true }).click()
  await expect(pending.locator('[data-guardrail-command] pre')).toHaveText(secondCommand, { timeout: 45_000 })
  await expect(pending.getByRole('button', { name: 'Authorize and continue', exact: true })).toBeInViewport({ ratio: 1 })
  await expect(bubble.locator('[data-guardrail-turn-summary]')).toHaveAttribute('data-guardrail-pending-count', '1')
  const secondOperation = await pending.getAttribute('data-guardrail-review')
  expect(secondOperation).not.toBe(firstOperation)
  const firstRecord = page.locator('[data-guardrail-review="' + firstOperation + '"]')
  await expect(firstRecord).toHaveAttribute('data-guardrail-state', 'approved')
  await expect(firstRecord.locator('[data-guardrail-command-preview]')).toHaveText(firstCommand)
  await expect(bubble).toHaveCount(1)
  await expect(bubble.locator('[data-guardrail-review]')).toHaveCount(2)
  await expect(bubble.locator('[data-guardrail-turn-summary]')).toHaveAttribute('data-guardrail-count', '2')
  await expect(bubble.locator('[data-guardrail-turn]')).toHaveAttribute('data-guardrail-turn', turnID!)
  const row = page.locator('[data-session-id="' + sessionId + '"]')
  await expect(row.getByRole('status', { name: '1 pending approvals', exact: true })).toBeVisible()
  expect((await events()).filter((event: any) => event.toolResult)).toHaveLength(1)
  await page.screenshot({ path: testInfo.outputPath('two-approvals-one-turn-pending.png') })
  await page.reload()
  await expect(bubble).toHaveCount(1)
  await expect(firstRecord).toHaveAttribute('data-guardrail-state', 'approved')
  await expect(pending).toHaveAttribute('data-guardrail-review', secondOperation!)
  await expect(pending.locator('[data-guardrail-command] pre')).toHaveText(secondCommand)
  await pending.getByRole('button', { name: 'Reject', exact: true }).click()
  await expect(pending).toHaveCount(0)
  await expect.poll(async () => (await events()).some((event: any) => event.turnEnded), { timeout: 60_000 }).toBe(true)
  const history = await events()
  const calls = history.filter((event: any) => event.toolCall)
  const results = history.filter((event: any) => event.toolResult)
  expect(calls).toHaveLength(2)
  expect(results).toHaveLength(2)
  expect(new Set([...calls, ...results].map((event: any) => event.turnId)).size).toBe(1)
  expect(!!results[0].toolResult.isError).toBe(false)
  expect(!!results[1].toolResult.isError).toBe(true)
  const output = (event: any) => event.toolResult.output.map((content: any) => content.text?.text || '').join('')
  expect(output(results[0]).trim()).toBe('GUARDRAIL_MULTI_TEST_FIRST')
  expect(output(results[1])).toContain('REJECTED')
  await page.reload()
  await expect(bubble).toHaveCount(1)
  await expect(bubble.locator('[data-guardrail-turn]')).toHaveAttribute('data-guardrail-turn', turnID!)
  await expect(bubble.locator('[data-guardrail-review]')).toHaveCount(2)
  await expect(bubble.locator('[data-guardrail-turn-summary]')).toHaveAttribute('data-guardrail-count', '2')
  await expect(firstRecord).toHaveAttribute('data-guardrail-state', 'approved')
  await expect(page.locator('[data-guardrail-review="' + secondOperation + '"]')).toHaveAttribute('data-guardrail-state', 'rejected')
  await expect(bubble).toContainText('MULTI_TEST_FINISHED')
  await expect(bubble.getByRole('button', { name: /Authorize and continue|Reject/ })).toHaveCount(0)
  await expect(row).not.toHaveAttribute('data-needs-attention', 'true')
  await expect(bubble.locator('[data-guardrail-turn-steps] > *')).toHaveCount(5)
  await page.screenshot({ path: testInfo.outputPath('two-approvals-one-turn-complete.png') })
  await expect(bubble.locator('[data-guardrail-turn-summary]')).toHaveAttribute('data-guardrail-pending-count', '0')
  const scroller = page.getByTestId('conversation-timeline')
  await scroller.hover()
  await page.mouse.wheel(0, -1000)
  await expect.poll(() => scroller.evaluate(el => el.scrollTop)).toBe(0)
  await firstRecord.locator('summary').first().click()
  await expect(firstRecord.locator('[data-guardrail-reason]')).toBeVisible()
  await scroller.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))))
  expect(await scroller.evaluate(el => el.scrollTop), 'reading an earlier review must not jump to the bottom').toBe(0)
  await firstRecord.locator('summary').first().click()
  await scroller.evaluate(el => { el.scrollTop = el.scrollHeight })
  const previousRecords = await bubble.locator('[data-guardrail-review]').evaluateAll(nodes => nodes.map(node => ({
    id: node.getAttribute('data-guardrail-review'), state: node.getAttribute('data-guardrail-state'),
    at: node.querySelector('time')?.getAttribute('datetime'),
  })))
  // The same command in another user turn still needs a new decision and record.
  await page.getByRole('textbox', { name: 'Your goal', exact: true }).fill(
    'New controlled UI test turn. Call bash exactly once with command = ' + JSON.stringify(firstCommand)
    + '. Wait for a fresh human approval even though this command ran earlier. Do not retry or use other tools. After its result respond only NEXT_TURN_FINISHED.')
  await page.getByRole('button', { name: 'Send message', exact: true }).click()
  await expect(pending).toHaveCount(1, { timeout: 45_000 })
  await expect(bubble).toHaveCount(2)
  const previousTurn = bubble.first()
  const nextTurn = bubble.nth(1)
  await expect(previousTurn.locator('[data-guardrail-turn]')).toHaveAttribute('data-guardrail-turn', turnID!)
  await expect(previousTurn.locator('[data-guardrail-review]')).toHaveCount(2)
  await expect(previousTurn.locator('[data-guardrail-turn-summary]')).toHaveAttribute('data-guardrail-count', '2')
  await expect(previousTurn.locator('[data-guardrail-turn-summary]')).toHaveAttribute('data-guardrail-pending-count', '0')
  await expect(nextTurn.locator('[data-guardrail-turn-summary]')).toHaveAttribute('data-guardrail-count', '1')
  await expect(nextTurn.locator('[data-guardrail-turn-summary]')).toHaveAttribute('data-guardrail-pending-count', '1')
  const nextOperation = await pending.getAttribute('data-guardrail-review')
  expect([firstOperation, secondOperation]).not.toContain(nextOperation)
  await expect(nextTurn.locator('[data-guardrail-command] pre')).toHaveText(firstCommand)
  await pending.getByRole('button', { name: 'Authorize and continue', exact: true }).click()
  await expect.poll(async () => (await events()).filter((event: any) => event.turnEnded).length, { timeout: 60_000 }).toBe(2)
  await page.reload()
  await expect(bubble).toHaveCount(2)
  await expect(page.locator('[data-guardrail-review]')).toHaveCount(3)
  await expect(nextTurn.locator('[data-guardrail-review]')).toHaveAttribute('data-guardrail-state', 'approved')
  await expect(nextTurn.locator('[data-guardrail-turn-summary]')).toHaveAttribute('data-guardrail-count', '1')
  await expect(nextTurn.locator('[data-guardrail-turn-summary]')).toHaveAttribute('data-guardrail-pending-count', '0')
  await expect(previousTurn.locator('[data-guardrail-turn-summary]')).toHaveAttribute('data-guardrail-count', '2')
  expect(await previousTurn.locator('[data-guardrail-review]').evaluateAll(nodes => nodes.map(node => ({
    id: node.getAttribute('data-guardrail-review'), state: node.getAttribute('data-guardrail-state'),
    at: node.querySelector('time')?.getAttribute('datetime'),
  })))).toEqual(previousRecords)
  await expect(nextTurn).toContainText('NEXT_TURN_FINISHED')
  const nextRecord = nextTurn.locator('[data-guardrail-review]')
  await nextRecord.locator('summary').first().click()
  await expect(nextRecord.locator('[data-guardrail-reason]')).toBeVisible()
  await expect(nextRecord.locator('[data-guardrail-resolution-source]')).toHaveAttribute('data-guardrail-resolution-source', 'control')
  await expect(nextRecord.locator('[data-guardrail-command] pre')).toHaveText(firstCommand)
  await nextRecord.locator('summary').first().click()
  await expect(row).not.toHaveAttribute('data-needs-attention', 'true')
  await page.screenshot({ path: testInfo.outputPath('multiple-turns-review-history.png') })
  console.log('Live multi-turn review history verified:', sessionId)
})
