import { test, expect } from '@playwright/test'
import { fromBinary } from '@bufbuild/protobuf'
import { anyUnpack } from '@bufbuild/protobuf/wkt'
import { EnvelopeSchema, AOPProtocolMessageSchema } from '../cyber-ui/packages/aop/src/index'
import { ProtocolMessageSchema } from '../src/gen/types/jev_pb'

test('Reflex header queries the bound session over the real AOP socket without starting a turn', async ({ page }, info) => {
  const login = await page.request.post('/api/auth/login', { data: { token: process.env.ACCESS_KEY || 'test-token' } })
  expect(login.ok()).toBe(true)
  const queries: string[] = [], modes: string[] = [], turns: string[] = []
  const expectedMode = process.env.JEV_E2E_MODE || 'off'
  let counts = { claims: 0, reflexes: 0 }
  page.on('websocket', socket => {
    socket.on('framesent', ({ payload }) => {
      if (typeof payload === 'string') return
      const envelope = fromBinary(EnvelopeSchema, payload)
      if (!envelope.payload) return
      const jev = anyUnpack(envelope.payload, ProtocolMessageSchema)
      if (jev?.message.case === 'request') queries.push(jev.message.value.sessionId)
      const core = anyUnpack(envelope.payload, AOPProtocolMessageSchema)
      if (core?.message.case === 'runTurnRequest') turns.push(core.message.value.turnId)
    })
    socket.on('framereceived', ({ payload }) => {
      if (typeof payload === 'string') return
      const envelope = fromBinary(EnvelopeSchema, payload)
      if (!envelope.payload) return
      const jev = anyUnpack(envelope.payload, ProtocolMessageSchema)
      if (jev?.message.case === 'library') {
        modes.push(jev.message.value.mode)
        counts = { claims: jev.message.value.claims.length, reflexes: jev.message.value.reflexes.length }
      }
    })
  })
  await page.goto('/')
  await expect(page.locator('aside [data-node-id="local"]')).toBeVisible()
  await page.locator('aside [data-node-id="local"]').getByRole('button', { name: /New task on/ }).click()
  await expect.poll(() => new URL(page.url()).pathname).toMatch(/^\/sessions\//)
  const sessionId = new URL(page.url()).pathname.split('/').at(-1)!
  try {
    await page.getByRole('button', { name: 'Reflex', exact: true }).click()
    const panel = page.getByRole('dialog', { name: 'Reflex', exact: true })
    await expect(panel).toBeVisible()
    await expect.poll(() => modes).toContain(expectedMode)
    expect(queries).toEqual([sessionId])
    expect(turns).toEqual([])
    await page.getByRole('tab', { name: 'Reflex library' }).click()
    await expect(panel.getByText(`Reflex ${counts.reflexes}`, { exact: true })).toBeVisible()
    await expect(panel.getByText(`Claim ${counts.claims}`, { exact: true })).toBeVisible()
    if (!counts.claims && !counts.reflexes) await expect(panel.getByText('No published Reflex or Claim', { exact: true })).toBeVisible()
    await expect(panel.getByRole('alert')).toHaveCount(0)
    await page.screenshot({ path: info.outputPath('reflex-query.png') })
  } finally {
    const deleted = await page.request.post('/cyber.rpc.chat.SessionService/DeleteSession', {
      headers: { 'Content-Type': 'application/json', 'Connect-Protocol-Version': '1' },
      data: { requestId: `delete-${sessionId}`, sessionId },
    })
    expect(deleted.ok()).toBe(true)
  }
})
