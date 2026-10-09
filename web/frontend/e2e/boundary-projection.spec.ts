import { test, expect } from '@playwright/test'

test('shared run projection scopes child turns and ignores replayed or late terminal frames', async ({ page }) => {
  await page.goto('/')
  const result = await page.evaluate(async () => {
    const { createAOPTimelineReducer } = await import('/cyber-ui/packages/viewer/src/lib/aop-reducer.ts')
    const { EventSchema } = await import('/cyber-ui/packages/aop/src/index.ts')
    const { create } = await import('/node_modules/.vite/deps/@bufbuild_protobuf.js')
    const reducer = createAOPTimelineReducer({ streaming: true })
    const events: any[] = []
    const add = (id: string, sessionId: string, turnId: string, payload: any) => events.push(create(EventSchema, { id, sessionId, turnId, emitter: 'agent', payload }))
    const current = () => { reducer(events); const state = reducer.runState('root'); return { active: state.activeTurnID, thinking: state.isThinking } }
    add('start', 'root', 'old', { case: 'turnStarted', value: {} })
    const started = current()
    add('end', 'root', 'old', { case: 'turnEnded', value: { stopReason: 'completed' } })
    add('next', 'root', 'new', { case: 'turnStarted', value: {} })
    const next = current()
    add('child', 'child', 'new', { case: 'turnEnded', value: { stopReason: 'completed' } })
    add('old-end-new-id', 'root', 'old', { case: 'turnEnded', value: { stopReason: 'completed' } })
    add('late-old-delta', 'root', 'old', { case: 'messageDelta', value: { messageId: 'answer', value: { case: 'text', value: 'late text' } } })
    events.push(events[0])
    const late = current()
    const cards = reducer(events).filter(item => item.kind === 'assistant_response').map(item => ({ text: item.response?.content, streaming: item.streaming }))
    return { started, next, late, cards, child: reducer.runState('child').activeTurnID }
  })
  expect(result).toEqual({ started: { active: 'old', thinking: true }, next: { active: 'new', thinking: true },
    late: { active: 'new', thinking: true }, cards: [{ text: 'late text', streaming: false }], child: '' })
})

test('replaced history resets run state and controls do not allocate timeline cards', async ({ page }) => {
  await page.goto('/')
  const result = await page.evaluate(async () => {
    const { createAOPTimelineReducer } = await import('/cyber-ui/packages/viewer/src/lib/aop-reducer.ts')
    const { EventSchema } = await import('/cyber-ui/packages/aop/src/index.ts')
    const { create } = await import('/node_modules/.vite/deps/@bufbuild_protobuf.js')
    const reducer = createAOPTimelineReducer({ timeline: false })
    const end = create(EventSchema, { id: 'end', sessionId: 'root', turnId: 'old', payload: { case: 'turnEnded', value: {} } })
    reducer([end])
    const ended = [...reducer.runState('root').ended]
    const start = create(EventSchema, { id: 'start', sessionId: 'root', turnId: 'new', payload: { case: 'turnStarted', value: {} } })
    const cards = reducer([start])
    return { ended, active: reducer.runState('root').activeTurnID, after: [...reducer.runState('root').ended], cards }
  })
  expect(result).toEqual({ ended: ['old'], active: 'new', after: [], cards: [] })
})

test('a hub terminal finishes node response cards before a later turn starts', async ({ page }) => {
  await page.goto('/')
  const result = await page.evaluate(async () => {
    const { createAOPTimelineReducer } = await import('/cyber-ui/packages/viewer/src/lib/aop-reducer.ts')
    const { EventSchema } = await import('/cyber-ui/packages/aop/src/index.ts')
    const { create } = await import('/node_modules/.vite/deps/@bufbuild_protobuf.js')
    const events = [
      create(EventSchema, { id: 'answer', sessionId: 'root', turnId: 'old', emitter: 'node', payload: {
        case: 'messageDelta', value: { messageId: 'answer', value: { case: 'text', value: 'partial answer' } },
      } }),
      create(EventSchema, { id: 'disconnected', sessionId: 'root', turnId: 'old', emitter: 'cyber.web', payload: {
        case: 'turnEnded', value: { stopReason: 'disconnected', usage: { totalTokens: 11n } },
      } }),
      create(EventSchema, { id: 'next', sessionId: 'root', turnId: 'new', emitter: 'node', payload: { case: 'turnStarted', value: {} } }),
    ]
    const reducer = createAOPTimelineReducer({ streaming: true })
    const cards = reducer(events).filter(item => item.kind === 'assistant_response')
    return { active: reducer.runState('root').activeTurnID, oldStreaming: cards[0].streaming, usage: cards[0].response?.metadata?.usage }
  })
  expect(result).toEqual({ active: 'new', oldStreaming: false, usage: { input_tokens: 0, output_tokens: 0, total_tokens: 11, model: '' } })
})
