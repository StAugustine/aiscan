import { test, expect, type Page } from '@playwright/test'

async function state(page: Page) { return JSON.parse(await page.getByTestId('state').innerText()) }
test.beforeEach(async ({ page }) => {
  await page.clock.install()
  await page.goto('/e2e/fixtures/boundary-guardrail.html')
  await expect.poll(async () => (await state(page)).unavailable).toEqual({ a: false, b: false })
})

test('initial, active and connection refreshes coalesce into one query per session', async ({ page }) => {
  expect(await page.evaluate(() => (window as any).guardrailFixture.queries.map((query: any) => query.sessionId))).toEqual(['a', 'b'])
})

test('polling a large session roster keeps at most four outstanding queries', async ({ page }) => {
  await page.evaluate(() => {
    const f = (window as any).guardrailFixture
    f.hold = true; f.queries.length = 0
    f.configure(Array.from({ length: 11 }, (_, index) => `session-${index}`), null)
  })
  await expect.poll(() => page.evaluate(() => (window as any).guardrailFixture.held.length)).toBe(4)
  await page.evaluate(() => (window as any).guardrailFixture.release())
  await expect.poll(() => page.evaluate(() => (window as any).guardrailFixture.held.length)).toBe(4)
  await page.evaluate(() => (window as any).guardrailFixture.release())
  await expect.poll(() => page.evaluate(() => (window as any).guardrailFixture.held.length)).toBe(3)
  await page.evaluate(() => (window as any).guardrailFixture.release())
  await expect.poll(() => page.evaluate(() => (window as any).guardrailFixture.queries.length)).toBe(11)
})

test('a query returning after approval cannot restore the resolved button', async ({ page }) => {
  await page.evaluate(() => {
    const f = (window as any).guardrailFixture
    f.server.set('a', [f.review('a')]); f.emit('a')
  })
  await expect(page.getByRole('button', { name: 'a-operation' })).toBeEnabled()
  await page.evaluate(() => { (window as any).guardrailFixture.hold = true })
  await page.clock.fastForward(5001)
  await expect.poll(() => page.evaluate(() => (window as any).guardrailFixture.held.length)).toBe(2)
  await page.getByRole('button', { name: 'a-operation' }).click()
  await expect(page.getByRole('button', { name: 'a-operation' })).toHaveCount(0)
  await page.evaluate(() => {
    const f = (window as any).guardrailFixture
    f.renders.length = 0; f.hold = false; f.release()
  })
  await expect.poll(() => page.evaluate(() => (window as any).guardrailFixture.outcomes)).toEqual(['resolved'])
  expect(await page.evaluate(() => (window as any).guardrailFixture.renders.some((render: any) => render.a?.includes('a-operation')))).toBe(false)
  expect((await state(page)).reviews.a).toEqual([])
})

test('query failure blocks only that session and reconnect restores authoritative reviews', async ({ page }) => {
  await page.evaluate(() => {
    const f = (window as any).guardrailFixture
    f.server.set('a', [f.review('a')]); f.server.set('b', [f.review('b')]); f.failures.add('a')
  })
  await page.clock.fastForward(5001)
  await expect.poll(async () => (await state(page)).unavailable).toEqual({ a: true, b: false })
  await page.evaluate(() => { const f = (window as any).guardrailFixture; f.resolve('a'); f.resolve('b') })
  await expect.poll(() => page.evaluate(() => (window as any).guardrailFixture.resolves)).toEqual(['b:b-operation'])
  expect(await page.evaluate(() => (window as any).guardrailFixture.outcomes)).toContain('Approval state is unavailable; reconnect before resolving.')
  await page.evaluate(() => {
    const w = window as any
    w.guardrailFixture.failures.clear(); w.guardrailFixture.sockets.at(-1).close()
  })
  await expect.poll(async () => (await state(page)).unavailable.a).toBe(true)
  await page.clock.fastForward(251)
  await expect(page.getByRole('button', { name: 'a-operation' })).toBeEnabled()
})

test('hidden tabs defer refresh and visibility resumes it without overlapping queries', async ({ page }) => {
  await page.evaluate(() => {
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => true })
    ;(window as any).guardrailFixture.queries.length = 0
  })
  await page.clock.fastForward(10_001)
  expect(await page.evaluate(() => (window as any).guardrailFixture.queries.length)).toBe(0)
  await page.evaluate(() => {
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => false })
    document.dispatchEvent(new Event('visibilitychange'))
  })
  await expect.poll(() => page.evaluate(() => (window as any).guardrailFixture.queries.map((q: any) => q.sessionId))).toEqual(['a', 'b'])
})

test('expired, malformed and historical reviews never authorize a runtime action', async ({ page }) => {
  await page.evaluate(() => {
    const f = (window as any).guardrailFixture
    f.server.set('a', [f.review('a', 'expired', -1), f.review('a', 'live', 6000), { ...f.review('a', 'missing'), operation: undefined }])
    f.emit('a', true)
  })
  await page.clock.fastForward(5001)
  await expect(page.getByRole('button', { name: 'live', exact: true })).toBeEnabled()
  expect((await state(page)).reviews.a).toEqual(['live'])
  await page.clock.fastForward(5001)
  await expect(page.getByRole('button')).toHaveCount(0)
  await page.evaluate(() => { const f = (window as any).guardrailFixture; f.server.set('a', []); f.emit('a') })
  await expect.poll(async () => (await state(page)).reviews.a).toEqual([])
})

test('a removed session cannot reappear when its old pending query completes', async ({ page }) => {
  await page.evaluate(() => {
    const f = (window as any).guardrailFixture
    f.server.set('a', [f.review('a')]); f.hold = true
  })
  await page.clock.fastForward(5001)
  await expect.poll(() => page.evaluate(() => (window as any).guardrailFixture.held.length)).toBe(2)
  await page.evaluate(() => {
    const f = (window as any).guardrailFixture
    f.configure(['b'], 'b'); f.hold = false; f.release()
  })
  await expect.poll(async () => Object.keys((await state(page)).reviews)).toEqual(['b'])
  await expect.poll(async () => (await state(page)).unavailable.b).toBe(false)
  await expect(page.getByRole('button', { name: 'a-operation' })).toHaveCount(0)
})
