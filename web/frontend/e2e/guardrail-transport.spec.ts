import { test, expect } from '@playwright/test'
import { create } from '@bufbuild/protobuf'
import { AOPClient } from '../cyber-ui/packages/aop/src/client'
import { GuardrailProtocolMessageSchema } from '../src/cyber-proto'

class Socket {
  static OPEN = 1
  static instances: Socket[] = []
  readyState = 0
  binaryType = ''
  onopen?: () => void
  onclose?: () => void
  onerror?: () => void
  onmessage?: (event: unknown) => void
  sent: unknown[] = []
  constructor(_url: string) { Socket.instances.push(this) }
  open() { this.readyState = 1; this.onopen?.() }
  close() { this.readyState = 3; this.onclose?.() }
  send(value: unknown) { this.sent.push(value) }
}

const nativeSocket = globalThis.WebSocket
test.beforeEach(() => {
  Socket.instances = []
  globalThis.WebSocket = Socket as unknown as typeof WebSocket
})
test.afterEach(() => { globalThis.WebSocket = nativeSocket })

test('offline approval never enters the outbound queue', async () => {
  const client = new AOPClient('ws://test')
  try {
    const request = create(GuardrailProtocolMessageSchema, { message: { case: 'resolve', value: { operationId: 'op', approve: true } } })
    await expect(client.request(GuardrailProtocolMessageSchema, request, { requireConnected: true, timeoutMs: 100 })).rejects.toThrow('disconnected')
    expect(Socket.instances).toHaveLength(0)
    const connected = client.connect()
    Socket.instances[0].open()
    await connected
    expect(Socket.instances[0].sent).toHaveLength(0)
  } finally { client.close() }
})

test('expired pending query is removed before reconnection can send it', async () => {
  const client = new AOPClient('ws://test')
  try {
    const request = create(GuardrailProtocolMessageSchema, { message: { case: 'pending', value: { sessionId: 'session' } } })
    await expect(client.request(GuardrailProtocolMessageSchema, request, { timeoutMs: 20 })).rejects.toThrow('timed out')
    Socket.instances[0].open()
    expect(Socket.instances[0].sent).toHaveLength(0)
  } finally { client.close() }
})

test('connection observers disable review actions and an interrupted approval is not replayed', async () => {
  const client = new AOPClient('ws://test')
  const states: boolean[] = []
  const unsubscribe = client.onConnectionChange(value => states.push(value))
  try {
    const ready = client.connect()
    const socket = Socket.instances[0]
    socket.open()
    await ready
    const result = client.request(GuardrailProtocolMessageSchema,
      create(GuardrailProtocolMessageSchema, { message: { case: 'resolve', value: { operationId: 'op', approve: true } } }),
      { requireConnected: true, timeoutMs: 100 })
    const rejected = expect(result).rejects.toThrow('disconnected')
    socket.close()
    await rejected
    expect(states).toEqual([true, false])
    const reconnect = client.connect()
    const next = Socket.instances[1]
    next.open()
    await reconnect
    expect(next.sent).toHaveLength(0)
    expect(states).toEqual([true, false, true])
  } finally { unsubscribe(); client.close() }
})
