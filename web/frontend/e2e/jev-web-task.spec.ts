import { expect, test } from '@playwright/test'
import { readFileSync, writeFileSync } from 'node:fs'
import { create, fromBinary, toBinary, type MessageInitShape } from '@bufbuild/protobuf'
import { anyPack, anyUnpack } from '@bufbuild/protobuf/wkt'
import { EnvelopeSchema, AOPProtocolMessageSchema } from '../cyber-ui/packages/aop/src/index'
import { ProtocolMessageSchema } from '../src/gen/types/jev_pb'

// The Go harness starts the current full application and checks business state.
// This driver submits through the production composer; it supplies no responses,
// Claim, Reflex, event fixture, or network interception.
test('real task submitted through the complete production frontend', async ({ page }, info) => {
  const manifestPath = process.env.JEV_WEB_TASK
  test.skip(!manifestPath, 'invoked by the complete Web task harness')
  const task = JSON.parse(readFileSync(manifestPath!, 'utf8'))
  const errors: string[] = []
  const frames: { time: number; direction: string; type: string; kind?: string }[] = []
  let turnId = '', endedAt = 0, startedAt = 0
  page.on('pageerror', error => errors.push(error.message))
  page.on('websocket', socket => {
    for (const direction of ['framesent', 'framereceived'] as const) socket.on(direction, ({ payload }) => {
      if (typeof payload === 'string') return
      const envelope = fromBinary(EnvelopeSchema, payload)
      if (!envelope.payload) return
      const core = anyUnpack(envelope.payload, AOPProtocolMessageSchema)
      const jev = anyUnpack(envelope.payload, ProtocolMessageSchema)
      frames.push({ time: Date.now(), direction, type: envelope.payload.typeUrl, kind: core?.message.case || jev?.message.case })
      if (core?.message.case === 'runTurnRequest') turnId = core.message.value.turnId
      if (core?.message.case === 'event' && core.message.value.turnId === turnId && core.message.value.payload.case === 'turnEnded') endedAt ||= Date.now()
    })
  })
  await page.addInitScript(() => localStorage.setItem('cyber-locale', 'en'))
  await page.goto('/')
  await page.getByLabel('Access token').fill(task.accessToken)
  await page.getByRole('button', { name: 'Sign in', exact: true }).click()
  const node = page.locator('aside [data-node-id="local"]')
  await expect(node).toBeVisible()
  // New browser contexts see the production interface tour before creating a task.
  const tour = page.getByRole('button', { name: 'Close interface tour', exact: true })
  if (await tour.waitFor({ state: 'visible', timeout: 5_000 }).then(() => true).catch(() => false)) await tour.click()
  if (task.sessionId) await page.goto(`/sessions/${task.sessionId}?node=local`)
  else {
    await node.getByRole('button', { name: /New task on/ }).click()
    await expect(page).toHaveURL(/\/sessions\//)
  }
  const sessionId = new URL(page.url()).pathname.split('/').at(-1)!

  async function jevRequest(message: MessageInitShape<typeof ProtocolMessageSchema>) {
    const id = `web-jev-${Date.now()}-${Math.random()}`
    const bytes = [...toBinary(EnvelopeSchema, create(EnvelopeSchema, { id, payload: anyPack(ProtocolMessageSchema, create(ProtocolMessageSchema, message)) }))]
    const reply = await page.evaluate(({ id, bytes }) => new Promise<number[]>((resolve, reject) => {
      const socket = new WebSocket(`${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/api/aop/application/ws`)
      socket.binaryType = 'arraybuffer'
      const timeout = setTimeout(() => { socket.close(); reject(new Error('JEV control response timeout')) }, 370_000)
      socket.onopen = () => socket.send(new Uint8Array(bytes))
      socket.onerror = () => { clearTimeout(timeout); reject(new Error('JEV control socket failed')) }
      // A dedicated socket has exactly one request and no event subscription.
      socket.onmessage = event => {
        if (!(event.data instanceof ArrayBuffer)) return
        clearTimeout(timeout); resolve([...new Uint8Array(event.data)]); socket.close()
      }
    }), { id, bytes })
    const envelope = fromBinary(EnvelopeSchema, new Uint8Array(reply))
    expect(envelope.replyTo).toBe(id)
    const result = envelope.payload && anyUnpack(envelope.payload, ProtocolMessageSchema)
    if (!result) throw new Error('JEV protocol rejected the control request')
    return result
  }
  const initial = await jevRequest({ message: { case: 'request', value: { sessionId } } })
  expect(initial.message.case).toBe('library')
  if (initial.message.case !== 'library') throw new Error('Missing Reflex library')
  expect(initial.message.value.mode).toBe(task.mode)
  if (task.emptyLibrary) {
    expect(initial.message.value.reflexes).toHaveLength(0)
    expect(initial.message.value.claims).toHaveLength(0)
  }

  let events: any[] = [], failure = ''
  try {
    await page.getByRole('textbox', { name: 'Your goal' }).fill(task.prompt)
    startedAt = Date.now()
    await page.getByRole('button', { name: 'Send message', exact: true }).click()
    await expect.poll(() => turnId, { timeout: 15_000 }).not.toBe('')
    await expect.poll(() => endedAt, { timeout: 300_000 }).toBeGreaterThan(0)
    await expect(page.getByRole('button', { name: 'Pause response' })).toHaveCount(0)
  } catch (error) {
    failure = String(error)
    const pause = page.getByRole('button', { name: 'Pause response' })
    if (await pause.isVisible()) await pause.click()
    if (turnId) await expect.poll(() => endedAt, { timeout: 20_000 }).toBeGreaterThan(0).catch(() => {})
  }
  const idle = await jevRequest({ message: { case: 'waitIdle', value: { sessionId, timeoutMs: 360_000 } } })
  const settled = idle.message.case === 'idle' && idle.message.value.settled
  const settledAt = Date.now()
  let cursor = ''
  do {
    const response = await page.request.post('/cyber.rpc.chat.SessionService/ListEvents', {
      headers: { 'Content-Type': 'application/json', 'Connect-Protocol-Version': '1' },
      data: { sessionId, limit: 500, afterCursor: cursor },
    })
    expect(response.ok()).toBe(true)
    const history = await response.json()
    events.push(...(history.events || []).map((delivery: any) => delivery.event))
    cursor = history.nextCursor || ''
  } while (cursor)
  events = [...new Map(events.map(event => [event.id, event])).values()]
  const current = events.filter(event => event.turnId === turnId)
  const end = current.find(event => event.turnEnded)?.turnEnded
  if (end?.stopReason !== 'completed') failure ||= `Turn did not complete: ${JSON.stringify(end)}`
  const output = current.filter(event => event.message?.role === 'assistant').at(-1)?.message?.content
    ?.map((block: any) => block.text?.text || '').join('\n') || ''
  const result = {
    session_id: sessionId, turn_id: turnId, output, events, errors, frames,
    foreground_ms: endedAt && startedAt ? endedAt - startedAt : null,
    including_background_ms: startedAt ? settledAt - startedAt : null,
    background_settled: settled, failure,
  }
  writeFileSync(task.resultPath, JSON.stringify(result, null, 2))
  if (!failure) await expect(page.getByTestId('task-context')).toContainText('Completed')
  await page.screenshot({ path: info.outputPath('task-complete.png'), fullPage: true })
  await page.getByRole('button', { name: 'Reflex', exact: true }).click()
  const panel = page.getByRole('dialog', { name: 'Reflex', exact: true })
  await expect(panel).toBeVisible()
  await expect(panel.getByRole('alert')).toHaveCount(0)
  await page.screenshot({ path: info.outputPath('runtime-network.png') })
  await page.getByRole('tab', { name: 'Reflex library', exact: true }).click()
  await page.screenshot({ path: info.outputPath('reflex-library.png') })
  await page.setViewportSize({ width: 390, height: 844 })
  expect(await panel.evaluate(element => element.scrollWidth - element.clientWidth)).toBeLessThanOrEqual(1)
  await page.screenshot({ path: info.outputPath('reflex-library-mobile.png') })
  await page.getByRole('tab', { name: 'Runtime network', exact: true }).click()
  await page.screenshot({ path: info.outputPath('runtime-network-mobile.png') })
  expect(frames.filter(frame => frame.direction === 'framesent' && frame.kind === 'runTurnRequest')).toHaveLength(1)
  expect(errors).toEqual([])
  expect(settled).toBe(true)
  expect(failure).toBe('')
  expect(output.trim()).not.toBe('')
})
