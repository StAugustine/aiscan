import { test, expect } from '@playwright/test'

// Exercise the actual shared browser client with a deterministic socket. The
// full-stack suite separately covers real Go/WebSocket/provider interactions.
test.beforeEach(async ({ page }) => {
  await page.goto('/')
  await page.clock.install()
  await page.evaluate(async () => {
    const { AOPClient, newID, EnvelopeSchema, AOPProtocolMessageSchema } = await import('/cyber-ui/packages/aop/src/index.ts')
    const { create, fromBinary, toBinary } = await import('/node_modules/.vite/deps/@bufbuild_protobuf.js')
    const { anyPack } = await import('/node_modules/.vite/deps/@bufbuild_protobuf_wkt.js')
    class Socket {
      static OPEN = 1
      readyState = 0
      binaryType = ''
      onopen: (() => void) | null = null
      onclose: (() => void) | null = null
      onmessage: ((event: { data: ArrayBuffer }) => void) | null = null
      frames: any[] = []
      constructor() { (window as any).fixtureSockets.push(this) }
      send(frame: Uint8Array) { this.frames.push(fromBinary(EnvelopeSchema, frame)) }
      open() { this.readyState = 1; this.onopen?.() }
      close() { this.readyState = 3; this.onclose?.() }
      deliver(replyTo: string, cursor = '') {
        const payload = create(AOPProtocolMessageSchema, { message: { case: 'protocolError', value: { message: 'fixture response' } } })
        const frame = toBinary(EnvelopeSchema, create(EnvelopeSchema, { id: newID(), replyTo, deliveryCursor: cursor, payload: anyPack(AOPProtocolMessageSchema, payload) }))
        this.onmessage?.({ data: frame.buffer.slice(frame.byteOffset, frame.byteOffset + frame.byteLength) as ArrayBuffer })
      }
    }
    Object.assign(window, { fixtureSockets: [], fixtureResult: [], Socket, AOPClient, newID, schema: AOPProtocolMessageSchema, make: create })
    window.WebSocket = Socket as any
    Object.assign(window, { client: new AOPClient('ws://fixture') })
  })
})

test('lost unary receipt times out by default and allows a later request', async ({ page }) => {
  await page.evaluate(() => {
    const w = window as any
    w.client.request(w.schema, w.make(w.schema), { id: 'lost' }).catch((error: Error) => w.fixtureResult.push(error.message))
    w.fixtureSockets[0].open()
  })
  await page.clock.fastForward(30_001)
  expect(await page.evaluate(() => (window as any).fixtureResult)).toEqual(['AOP request timed out'])
  const result = await page.evaluate(async () => {
    const w = window as any
    const next = w.client.request(w.schema, w.make(w.schema), { id: 'next' })
    w.fixtureSockets[0].deliver('next')
    return (await next).message.value.message
  })
  expect(result).toBe('fixture response')
})

test('concurrent requests with the same ID reject the duplicate without orphaning the first', async ({ page }) => {
  const result = await page.evaluate(async () => {
    const w = window as any
    const first = w.client.request(w.schema, w.make(w.schema), { id: 'collision' })
    w.fixtureSockets[0].open()
    const second = await w.client.request(w.schema, w.make(w.schema), { id: 'collision' }).then(() => 'accepted', (error: Error) => error.message)
    w.fixtureSockets[0].deliver('collision')
    return { first: (await first).message.value.message, second }
  })
  expect(result).toEqual({ first: 'fixture response', second: 'AOP request collision is already pending' })
})

test('an explicit zero timeout preserves requests that deliberately wait longer than the default', async ({ page }) => {
  await page.evaluate(() => {
    const w = window as any
    w.client.request(w.schema, w.make(w.schema), { id: 'unbounded', timeoutMs: 0 })
      .then((reply: any) => w.fixtureResult.push(reply.message.value.message), (error: Error) => w.fixtureResult.push(error.message))
    w.fixtureSockets[0].open()
  })
  await page.clock.fastForward(30_001)
  expect(await page.evaluate(() => (window as any).fixtureResult)).toEqual([])
  await page.evaluate(() => (window as any).fixtureSockets[0].deliver('unbounded'))
  await expect.poll(() => page.evaluate(() => (window as any).fixtureResult)).toEqual(['fixture response'])
})

test('unsubscribe before the socket opens does not dispatch an abandoned watch', async ({ page }) => {
  const frames = await page.evaluate(() => {
    const w = window as any
    const stop = w.client.subscribe(w.schema, w.make(w.schema), () => {}, { id: 'abandoned', durable: true })
    stop()
    w.fixtureSockets[0].open()
    return w.fixtureSockets[0].frames.map((frame: any) => frame.id)
  })
  expect(frames).toEqual([])
})

test('an old unsubscribe cannot cancel a replacement using the same callback', async ({ page }) => {
  const result = await page.evaluate(() => {
    const w = window as any
    const receive = (payload: any) => w.fixtureResult.push(payload.message.value.message)
    const stopOld = w.client.subscribe(w.schema, w.make(w.schema), receive, { id: 'replaced', durable: true })
    w.fixtureSockets[0].open()
    w.client.subscribe(w.schema, w.make(w.schema), receive, { id: 'replaced', durable: true })
    stopOld()
    w.fixtureSockets[0].deliver('replaced', '31')
    return { received: w.fixtureResult, frames: w.fixtureSockets[0].frames.map((frame: any) => frame.id) }
  })
  expect(result).toEqual({ received: ['fixture response'], frames: ['replaced', 'replaced'] })
})

test('replacing a queued watch dispatches only its current request', async ({ page }) => {
  const frames = await page.evaluate(() => {
    const w = window as any
    w.client.subscribe(w.schema, w.make(w.schema), () => {}, { id: 'replaced', durable: true })
    w.client.subscribe(w.schema, w.make(w.schema), () => {}, { id: 'replaced', durable: true })
    w.fixtureSockets[0].open()
    return w.fixtureSockets[0].frames.map((frame: any) => frame.id)
  })
  expect(frames).toEqual(['replaced'])
})

test('a subscription cannot steal the response to a pending unary request', async ({ page }) => {
  const result = await page.evaluate(async () => {
    const w = window as any
    const request = w.client.request(w.schema, w.make(w.schema), { id: 'collision' })
    w.fixtureSockets[0].open()
    let error = ''
    try { w.client.subscribe(w.schema, w.make(w.schema), () => {}, { id: 'collision' }) } catch (e) { error = String(e) }
    w.fixtureSockets[0].deliver('collision')
    return { error, response: (await request).message.value.message }
  })
  expect(result).toEqual({ error: 'Error: AOP request collision is already pending', response: 'fixture response' })
})

test('closing during a pending handshake prevents a late open from reviving the client', async ({ page }) => {
  const result = await page.evaluate(async () => {
    const w = window as any
    const connection = w.client.connect().then(() => 'connected', (error: Error) => error.message)
    w.client.close()
    w.fixtureSockets[0].open()
    return { connected: w.client.connected, outcome: await connection }
  })
  expect(result.connected).toBe(false)
  expect(result.outcome).not.toBe('connected')
})

test('explicit close cancels an already scheduled reconnect', async ({ page }) => {
  await page.evaluate(async () => {
    const w = window as any
    const connected = w.client.connect()
    w.fixtureSockets[0].open()
    await connected
    w.fixtureSockets[0].close()
    w.client.close()
  })
  await page.clock.fastForward(10_000)
  expect(await page.evaluate(() => (window as any).fixtureSockets.length)).toBe(1)
})

test('a stale socket close cannot interrupt a new handshake', async ({ page }) => {
  const result = await page.evaluate(async () => {
    const w = window as any
    let connected = w.client.connect()
    w.fixtureSockets[0].open()
    await connected
    w.fixtureSockets[0].close()
    connected = w.client.connect()
    w.fixtureSockets[0].close()
    const same = w.client.connect()
    w.fixtureSockets[1].open()
    await Promise.all([same, connected])
    return { sockets: w.fixtureSockets.length, connected: w.client.connected }
  })
  expect(result).toEqual({ sockets: 2, connected: true })
})

test('malformed binary frames do not throw or destroy a healthy subscription', async ({ page }) => {
  const result = await page.evaluate(() => {
    const w = window as any
    w.client.subscribe(w.schema, w.make(w.schema), (payload: any) => w.fixtureResult.push(payload.message.value.message), { id: 'watch', durable: true })
    w.fixtureSockets[0].open()
    let error = ''
    try { w.fixtureSockets[0].onmessage({ data: new Uint8Array([255, 255, 255]).buffer }) } catch (e) { error = String(e) }
    w.fixtureSockets[0].deliver('watch', '17')
    return { error, received: w.fixtureResult }
  })
  expect(result).toEqual({ error: '', received: ['fixture response'] })
})

test('durable watches resume from their received cursor exactly once', async ({ page }) => {
  await page.evaluate(() => {
    const w = window as any
    w.fixtureCursors = []
    w.client.subscribe(w.schema, w.make(w.schema), () => {}, {
      id: 'watch', durable: true, resume: (cursor: string) => { w.fixtureCursors.push(cursor); return w.make(w.schema) },
    })
    w.fixtureSockets[0].open()
    w.fixtureSockets[0].deliver('watch', '29')
    w.fixtureSockets[0].close()
  })
  await page.clock.fastForward(251)
  await page.evaluate(() => (window as any).fixtureSockets[1].open())
  expect(await page.evaluate(() => ({ cursors: (window as any).fixtureCursors, frames: (window as any).fixtureSockets[1].frames.map((frame: any) => frame.id) }))).toEqual({ cursors: ['29'], frames: ['watch'] })
})

test('HTTP/insecure contexts and failing randomUUID share a working ID fallback', async ({ page }) => {
  const result = await page.evaluate(() => {
    const w = window as any
    Object.defineProperty(crypto, 'randomUUID', { configurable: true, value: () => { throw new Error('not a secure context') } })
    const ids = Array.from({ length: 100 }, () => w.newID())
    return { unique: new Set(ids).size, uuids: ids.every((id: string) => /^[0-9a-f-]{36}$/.test(id)) }
  })
  expect(result).toEqual({ unique: 100, uuids: true })
})
