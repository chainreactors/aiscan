import { expect, test, type APIRequestContext } from '@playwright/test'

const token = process.env.ACCESS_KEY || 'test-token'

async function configRPC(request: APIRequestContext, method: string, data: object = {}) {
  const response = await request.post('/cyber.rpc.config.ConfigService/' + method, {
    headers: { Authorization: 'Bearer ' + token, 'Connect-Protocol-Version': '1' },
    data,
  })
  expect(response.ok(), await response.text()).toBeTruthy()
  return response.json()
}

test('guardrail settings persist policy in its own section without activating JEV', async ({ page, request }) => {
  const original = (await configRPC(request, 'GetConfig')).config.extensions ?? {}
  try {
    await configRPC(request, 'UpdateConfig', { config: { extensions: {
      guardrail: { ...original.guardrail?.values, provider: 'none' },
    } } })
    expect((await page.request.post('/api/auth/login', { data: { token } })).ok()).toBeTruthy()
    await page.goto('/')
    await page.getByRole('button', { name: 'Open settings', exact: true }).click()
    await page.getByRole('button', { name: 'Guardrail', exact: true }).click()
    const enable = page.getByRole('switch', { name: 'Enable JEV tool guardrail' })
    await expect(enable).not.toBeChecked()
    await page.getByRole('combobox', { name: 'Policy level', exact: true }).click()
    await page.getByRole('option', { name: 'Strict: inspect active probes', exact: true }).click()
    await page.getByRole('combobox', { name: 'When JEV is unavailable', exact: true }).click()
    await page.getByRole('option', { name: 'Classify as requiring review', exact: true }).click()
    await page.getByRole('textbox', { name: 'Review timeout', exact: true }).fill('3m')
    await page.getByRole('button', { name: 'Save', exact: true }).click()
    await expect(page.getByRole('button', { name: 'Save', exact: true })).toHaveCount(0)

    const saved = (await configRPC(request, 'GetConfig')).config.extensions
    expect(saved.guardrail.values).toMatchObject({
      provider: 'none', review_timeout: '3m', jev: { level: 'strict', on_error: 'review' },
    })
    for (const field of ['enabled', 'level', 'on_error', 'criteria']) {
      expect(saved.jev?.values ?? {}).not.toHaveProperty(field)
    }
    await page.reload()
    await page.getByRole('button', { name: 'Open settings', exact: true }).click()
    await page.getByRole('button', { name: 'Guardrail', exact: true }).click()
    await expect(page.getByRole('combobox', { name: 'Policy level', exact: true })).toContainText('Strict: inspect active probes')
    await expect(page.getByRole('combobox', { name: 'When JEV is unavailable', exact: true })).toContainText('Classify as requiring review')
    await expect(page.getByRole('textbox', { name: 'Review timeout', exact: true })).toHaveValue('3m')
    await expect(enable).not.toBeChecked()
  } finally {
    await configRPC(request, 'UpdateConfig', { config: { extensions: {
      guardrail: original.guardrail?.values ?? {},
    } } })
  }
})
