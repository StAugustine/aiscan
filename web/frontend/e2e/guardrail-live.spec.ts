import { expect, test, type APIRequestContext } from '@playwright/test'

const token = process.env.ACCESS_KEY || 'test-token'
let original: { jev: Record<string, unknown>; guardrail: Record<string, unknown> } | undefined

async function rpc(request: APIRequestContext, service: string, method: string, data: object) {
  const response = await request.post('/cyber.rpc.' + service + '/' + method, {
    headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json', 'Connect-Protocol-Version': '1' },
    data, timeout: 20_000,
  })
  expect(response.ok(), method + ' must succeed').toBeTruthy()
  return response.json()
}

// Teardown has its own timeout and restores both provider policy and core mode.
test.afterEach(async ({ request }) => {
  if (!original) return
  test.setTimeout(30_000)
  await rpc(request, 'config.ConfigService', 'UpdateConfig', { config: { extensions: original } })
  const restored = (await rpc(request, 'config.ConfigService', 'GetConfig', {})).config.extensions
  expect(restored.jev.values.criteria || {}).toEqual(original.jev.criteria || {})
  expect(restored.jev.values.enabled).toBe(original.jev.enabled)
  expect(restored.guardrail?.values?.mode || 'safe').toBe(original.guardrail.mode || 'safe')
  original = undefined
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
    record: 'Low risk local actions EXCEPT commands containing GUARDRAIL_REVIEW_TEST or GUARDRAIL_BLOCK_TEST. Test run ' + Date.now(),
    review: 'Commands containing GUARDRAIL_REVIEW_TEST require review, even if they only echo a marker. This is an explicit operator UI test policy. Also review target changes or uncertain effects.',
    block: 'Commands containing GUARDRAIL_BLOCK_TEST must be blocked, even if they only echo a marker. This is an explicit operator UI test policy. Also block destructive actions and data leakage.',
  }
  await rpc(page.request, 'config.ConfigService', 'UpdateConfig', {
    config: { extensions: {
      jev: { ...original.jev, enabled: true, on_error: 'block', criteria },
      guardrail: { ...original.guardrail, mode: 'safe' },
    } },
  })
  await waitForReload(previous)
  await page.goto('/')
  const expand = page.getByRole('button', { name: 'Expand sidebar', exact: true })
  if (await expand.isVisible()) await expand.click()
  const results: object[] = []
  let mode = 'safe'
  for (const scenario of [
    { mode: 'safe', risk: 'REVIEW', action: 'approve' },
    { mode: 'safe', risk: 'BLOCK', action: 'approve' },
    { mode: 'safe', risk: 'BLOCK', action: 'reject' },
    { mode: 'auto', risk: 'REVIEW', action: 'continue' },
    { mode: 'auto', risk: 'BLOCK', action: 'continue' },
  ]) {
    if (scenario.mode !== mode) {
      const before = await agent()
      await page.getByRole('button', { name: 'Switch guardrail mode: currently Safe mode', exact: true }).click()
      await waitForReload(before)
      await expect(page.getByRole('button', { name: 'Switch guardrail mode: currently Automatic mode', exact: true })).toBeVisible()
      const saved = (await rpc(page.request, 'config.ConfigService', 'GetConfig', {})).config.extensions
      expect(saved.guardrail.values.mode).toBe('auto')
      expect(saved.jev.values.criteria).toEqual(criteria)
      mode = scenario.mode
    }
    const previousURL = page.url()
    await page.getByRole('button', { name: 'New task on ' + node, exact: true }).click()
    await page.waitForURL(url => url.href !== previousURL && /^\/sessions\/[^/]+$/.test(url.pathname), { timeout: 15_000 })
    const marker = 'GUARDRAIL_' + scenario.risk + '_TEST_' + scenario.mode.toUpperCase() + '_' + scenario.action.toUpperCase()
    const recovery = 'GUARDRAIL_RECOVERY_' + scenario.risk
    const prompt = scenario.mode === 'safe'
      ? 'Controlled UI test. Call bash exactly once with command = ' + JSON.stringify('echo ' + marker) + '. Do not call other tools, change the command or retry after a denial. Wait for human authorization if requested. After the tool returns, respond only TEST_FINISHED.'
      : 'Controlled UI test. First call bash exactly once with command = ' + JSON.stringify('echo ' + marker) + '. If the guardrail intercepts it, do not retry that command. Instead call bash once with command = ' + JSON.stringify('echo ' + recovery) + '. After the second result, respond only TEST_FINISHED. Use no other tools.'
    await page.getByRole('textbox', { name: 'Your goal', exact: true }).fill(prompt)
    await page.getByRole('button', { name: 'Send message', exact: true }).click()
    const sessionId = new URL(page.url()).pathname.split('/').pop()!
    const events = async () => (await rpc(page.request, 'chat.SessionService', 'ListEvents', { sessionId })).events.map((item: any) => item.event)
    const review = page.getByRole('region', { name: 'Tool approval required', exact: true })
    const name = scenario.mode + '-' + scenario.risk.toLowerCase() + '-' + scenario.action
    if (scenario.mode === 'safe') {
      await expect(review).toBeVisible({ timeout: 45_000 })
      await expect(review).toContainText('GUARDRAIL_' + scenario.risk + '_TEST')
      expect((await events()).filter((event: any) => event.toolResult)).toHaveLength(0)
      const before = await review.textContent()
      await page.screenshot({ path: testInfo.outputPath(name + '-pending.png') })
      await page.reload()
      await expect(review).toBeVisible()
      expect(await review.textContent()).toBe(before)
      await page.getByRole('button', { name: scenario.action === 'approve' ? 'Authorize and continue' : 'Reject', exact: true }).click()
      await expect(review).toHaveCount(0)
    }
    await expect.poll(async () => (await events()).some((event: any) => event.turnEnded),
      { timeout: 60_000, intervals: [500, 1000, 2000] }).toBe(true)
    const history = await events()
    const calls = history.filter((event: any) => event.toolCall)
    const outputs = history.filter((event: any) => event.toolResult).map((event: any) => event.toolResult)
    const count = scenario.mode === 'auto' ? 2 : 1
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
    if (scenario.mode === 'auto') {
      expect(!!outputs[1].isError).toBe(false)
      expect(outputText(outputs[1]).trim()).toBe(recovery)
      expect(new Set(history.filter((event: any) => event.toolCall || event.toolResult).map((event: any) => event.turnId)).size).toBe(1)
    }
    await expect(review).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Authorize and continue', exact: true })).toHaveCount(0)
    await page.getByRole('button', { name: new RegExp('^' + count + ' Tools?$', 'i') }).click()
    await page.getByRole('button', { name: 'bash echo ' + marker, exact: true }).click()
    await expect(page.getByText(scenario.action === 'approve' ? marker : /operation denied/, { exact: scenario.action === 'approve' })).toBeVisible()
    if (scenario.mode === 'auto') {
      await page.getByRole('button', { name: 'bash echo ' + recovery, exact: true }).click()
      await expect(page.getByText(recovery, { exact: true })).toBeVisible()
    }
    await page.screenshot({ path: testInfo.outputPath(name + '.png') })
    await page.reload()
    await expect(page.getByRole('button', { name: 'Open settings', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Authorize and continue', exact: true })).toHaveCount(0)
    results.push({ ...scenario, sessionId, calls: calls.length, results: outputs.length })
    console.log('Live guardrail verified:', name, sessionId)
  }
  await testInfo.attach('live-guardrail-results', { body: JSON.stringify(results, null, 2), contentType: 'application/json' })
})
