import { test, expect } from '@playwright/test'
import { groupGuardrailTurns, guardrailArguments, guardrailTimelineEvents, isGuardrailBoundary, withGuardrailReviews } from '../src/lib/guardrail-view'
import { create } from '@bufbuild/protobuf'
import { anyPack, timestampFromDate } from '@bufbuild/protobuf/wkt'
import { GuardrailDecisionSchema, GuardrailAction, ReviewSchema, ReviewState, type Review } from '../src/cyber-proto'
import { RefSchema, CompletedSchema, FailureKind } from '@cyber/aop'
import { EventSchema } from '../cyber-ui/packages/aop/src/gen/aop/event_pb'
import type { ViewerTimelineItem } from '../src/viewer'
import { reduceAOPToTimeline } from '../cyber-ui/packages/viewer/src/lib/aop-reducer'

function review(args: unknown, callId = 'call-1', operationId = 'op-1'): Review {
  return create(ReviewSchema, { call: { id: callId, name: 'bash', arguments: { data: new TextEncoder().encode(JSON.stringify(args)) } }, operation: { operationId }, sessionId: 'session-1', state: ReviewState.PENDING })
}

test('approval displays the exact multiline command and keeps additional arguments', () => {
  const command = "printf \"%s\\n\" \"a b\"\necho \"$HOME\"\n" + 'x'.repeat(500)
  const display = guardrailArguments(review({ command, timeout: 20, env: { MODE: 'read only' } }))
  expect(display.command).toBe(command)
  expect(display.fields).toEqual([['timeout', '20'], ['env', JSON.stringify({ MODE: 'read only' }, null, 2)]])
  expect(JSON.parse(display.raw)).toEqual({ command, timeout: 20, env: { MODE: 'read only' } })
})

test('non-shell tool arguments remain labeled without inventing a command', () => {
  const display = guardrailArguments(review({ path: 'a b.txt', limit: 0, recursive: false }))
  expect(display.command).toBeNull()
  expect(display.fields).toEqual([['path', 'a b.txt'], ['limit', '0'], ['recursive', 'false']])
})

test('approval follows its tool turn even when later timeline items exist', () => {
  const items: ViewerTimelineItem[] = [
    { id: 'turn-1', kind: 'assistant_response', timestamp: 1, tools: [{ id: 'call-1', toolName: 'bash', toolArgs: '{}', pending: true }], streaming: true },
    { id: 'later', kind: 'message', role: 'user', content: 'later message', timestamp: 2 },
  ]
  const result = withGuardrailReviews(items, [review({ command: 'echo hello' })])
  expect(result.map(item => item.id)).toEqual(['turn-1', 'guardrail:op-1', 'later'])
  expect(items).toHaveLength(2)
  expect(withGuardrailReviews(items, [])).toEqual(items)
})

test('parallel and unmatched invocations all retain distinct approval entries', () => {
  const result = withGuardrailReviews([], [review({ command: 'echo one' }), review({ command: 'echo two' }, 'call-2', 'op-2')])
  expect(result.map(item => item.id)).toEqual(['guardrail:op-1', 'guardrail:op-2'])
})

function reviewEvent(value: Review, seq: bigint) {
  return create(EventSchema, { id: 'event-' + seq, seq, sessionId: 'session-1', turnId: 'turn-1', emitter: 'guardrail',
    emittedAt: timestampFromDate(new Date(1700000000000 + Number(seq) * 1000)),
    payload: { case: 'extension', value: anyPack(ReviewSchema, value) },
  })
}

for (const state of [ReviewState.APPROVED, ReviewState.REJECTED]) {
  test('automatic consequence audit ' + ReviewState[state] + ' preserves one turn and never offers human approval', () => {
    const call = create(EventSchema, { id: 'call', seq: 1n, sessionId: 'session-1', turnId: 'turn-1', emitter: 'agent',
      payload: { case: 'toolCall', value: { id: 'call-1', name: 'bash' } } })
    const risk = create(EventSchema, { ...call, id: 'risk', seq: 2n, emitter: 'guardrail',
      extensions: [anyPack(RefSchema, create(RefSchema, { operationId: 'op-1', callId: 'call-1' }))],
      payload: { case: 'extension', value: anyPack(GuardrailDecisionSchema, create(GuardrailDecisionSchema, { action: GuardrailAction.REVIEW, reason: 'potential change' })) } })
    const audit = reviewEvent(create(ReviewSchema, { ...review({ command: 'echo checked' }), state, resolutionSource: 'auto' }), 3n)
    const output = create(EventSchema, { ...call, id: 'result', seq: 4n,
      payload: { case: 'toolResult', value: { callId: 'call-1', name: 'bash', output: [], isError: state === ReviewState.REJECTED } } })
    const answer = create(EventSchema, { ...call, id: 'answer', seq: 5n,
      payload: { case: 'message', value: { id: 'answer', role: 'assistant', content: [{ value: { case: 'text', value: { text: 'Done' } } }] } } })
    const events = [call, risk, audit, output, answer]
    const grouped = groupGuardrailTurns(withGuardrailReviews(reduceAOPToTimeline(guardrailTimelineEvents(events),
      { responseBoundary: isGuardrailBoundary }), [], events))
    expect(grouped).toHaveLength(1)
    const turn = grouped[0]
    expect(turn.kind).toBe('assistant_response')
    if (turn.kind !== 'assistant_response') throw new Error('missing turn')
    const records = turn.steps?.filter(item => item.kind === 'extension')
    expect(records).toHaveLength(1)
    expect(records?.[0]).toMatchObject({ id: 'guardrail:op-1', data: { review: { state, resolutionSource: 'auto' },
      actionable: false, awaiting: false, intercepted: false, outcome: state === ReviewState.APPROVED ? 'succeeded' : undefined } })
  })
}

for (const state of [ReviewState.APPROVED, ReviewState.REJECTED, ReviewState.EXPIRED, ReviewState.CANCELED]) {
  test('terminal review ' + ReviewState[state] + ' survives replay without restoring authorization', () => {
    const pending = review({ command: 'echo safe' })
    const terminal = create(ReviewSchema, { ...pending, state })
    const events = [reviewEvent(terminal, 2n), reviewEvent(pending, 1n), reviewEvent(pending, 3n)]
    const result = withGuardrailReviews([], [pending, pending], events)
    expect(result).toHaveLength(1)
    expect(result[0]).toMatchObject({ id: 'guardrail:op-1', data: { review: { state }, actionable: false, reviewedAt: 1700000002000 } })
    expect(withGuardrailReviews([], [], events)).toEqual(result)
  })
}

test('historical pending reviews are not actionable until confirmed by the runtime', () => {
  const pending = review({ command: 'echo safe' })
  const events = [reviewEvent(pending, 1n)]
  expect(withGuardrailReviews([], [], events)[0]).toMatchObject({ data: { actionable: false } })
  expect(withGuardrailReviews([], [pending], events)[0]).toMatchObject({ data: { actionable: true } })
})

test('approval stays between the original tool call and the streamed continuation', () => {
  const pending = review({ command: 'echo safe' })
  const call = create(EventSchema, { id: 'call', seq: 1n, sessionId: 'session-1', turnId: 'turn-1', emitter: 'agent',
    emittedAt: timestampFromDate(new Date(1700000001000)),
    payload: { case: 'toolCall', value: { id: 'call-1', name: 'bash' } },
  })
  const requested = reviewEvent(pending, 2n)
  const approved = reviewEvent(create(ReviewSchema, { ...pending, state: ReviewState.APPROVED }), 3n)
  const output = create(EventSchema, { ...call, id: 'result', seq: 4n, emittedAt: timestampFromDate(new Date(1700000004000)),
    payload: { case: 'toolResult', value: { callId: 'call-1', name: 'bash', output: [{ value: { case: 'text', value: { text: 'safe' } } }] } },
  })
  const delta = create(EventSchema, { ...call, id: 'delta', seq: 5n, emittedAt: timestampFromDate(new Date(1700000005000)),
    payload: { case: 'messageDelta', value: { messageId: 'after-review', value: { case: 'text', value: 'Completed' } } },
  })
  const complete = create(EventSchema, { ...call, id: 'answer', seq: 6n, emittedAt: timestampFromDate(new Date(1700000006000)),
    payload: { case: 'message', value: { id: 'after-review', role: 'assistant', content: [{ value: { case: 'text', value: { text: 'Completed' } } }] } },
  })
  const project = (events: typeof call[]) => withGuardrailReviews(
    reduceAOPToTimeline(guardrailTimelineEvents(events), { streaming: true, lifecycle: 'errors', responseBoundary: isGuardrailBoundary }), [], events)
  const waiting = project([call, requested])
  const streaming = project([call, requested, approved, output, delta])
  const replayed = project([call, requested, approved, output, delta, complete])
  expect(waiting.map(item => item.kind)).toEqual(['assistant_response', 'extension'])
  expect(replayed.map(item => item.kind)).toEqual(['assistant_response', 'extension', 'assistant_response'])
  expect(replayed[0]).toMatchObject({ tools: [{ id: 'call-1', result: 'safe', pending: false }] })
  expect(replayed[0]).not.toHaveProperty('response')
  expect(replayed[1]).toMatchObject({ id: waiting[1].id, timestamp: waiting[1].timestamp,
    data: { review: { state: ReviewState.APPROVED }, reviewedAt: 1700000003000 } })
  expect(streaming[2]).toMatchObject({ streaming: true, response: { content: 'Completed' } })
  expect(replayed[2]).toMatchObject({ streaming: false, response: { content: 'Completed' } })
  expect(requested.emitter).toBe('guardrail')
})

test('approval boundaries respect session and turn when tool call identifiers repeat', () => {
  const call = create(EventSchema, { id: 'call', seq: 1n, sessionId: 'session-1', turnId: 'turn-1', emitter: 'first-agent',
    payload: { case: 'toolCall', value: { id: 'call-1', name: 'bash' } } })
  const otherSession = create(EventSchema, { ...call, id: 'other-session', sessionId: 'session-2', emitter: 'second-agent' })
  const otherTurn = create(EventSchema, { ...call, id: 'other-turn', turnId: 'turn-2', emitter: 'third-agent' })
  const request = reviewEvent(review({ command: 'echo safe' }), 2n)
  const adapted = guardrailTimelineEvents([call, otherSession, otherTurn, request])
  expect(adapted[3].emitter).toBe('first-agent')
  expect(adapted.slice(0, 3)).toEqual([call, otherSession, otherTurn])
  expect(request.emitter).toBe('guardrail')
})

test('the native approval event position wins over a reused call ID from an older turn', () => {
  const pending = review({ command: 'echo safe' })
  const event = reviewEvent(pending, 2n)
  const tool = { id: 'call-1', toolName: 'bash', toolArgs: '{}', pending: false }
  const items: ViewerTimelineItem[] = [
    { id: 'old-turn', kind: 'assistant_response', timestamp: 1, tools: [tool], streaming: false },
    { id: 'new-turn', kind: 'assistant_response', timestamp: 2, tools: [tool], streaming: true },
    { id: event.id, kind: 'extension', extensionType: 'type.googleapis.com/cyber.guardrail.Review', timestamp: 3, data: {} },
    { id: 'continuation', kind: 'assistant_response', timestamp: 4, tools: [], response: { content: 'Done' }, streaming: false },
  ]
  expect(withGuardrailReviews(items, [pending], [event]).map(item => item.id))
    .toEqual(['old-turn', 'new-turn', 'guardrail:op-1', 'continuation'])
})

test('raw review rows collapse into one operation record without hiding other extensions', () => {
  const pending = review({ command: 'echo safe' })
  const items: ViewerTimelineItem[] = [
    { id: 'raw-review', kind: 'extension', extensionType: 'type.googleapis.com/cyber.guardrail.Review', timestamp: 1, data: {} },
    { id: 'raw-policy', kind: 'extension', extensionType: 'type.googleapis.com/cyber.guardrail.Decision', timestamp: 1, data: {} },
    { id: 'other', kind: 'extension', extensionType: 'scan', timestamp: 1, data: {} },
  ]
  expect(withGuardrailReviews(items, [pending], [reviewEvent(pending, 1n)]).map(item => item.id)).toEqual(['other', 'guardrail:op-1'])
})

test('parallel approvals sharing a tool turn keep stable order and deduplicate', () => {
  const first = review({ command: 'echo one' })
  const second = review({ command: 'echo two' }, 'call-2', 'op-2')
  const items: ViewerTimelineItem[] = [{ id: 'turn-1', kind: 'assistant_response', timestamp: 1,
    tools: [{ id: 'call-1', toolName: 'bash', toolArgs: '{}', pending: true }, { id: 'call-2', toolName: 'bash', toolArgs: '{}', pending: true }], streaming: true }]
  expect(withGuardrailReviews(items, [first, second, first]).map(item => item.id))
    .toEqual(['turn-1', 'guardrail:op-1', 'guardrail:op-2'])
})

function toolEvent(callId: string, seq: bigint, sessionId = 'session-1', turnId = 'turn-1') {
  return create(EventSchema, { id: sessionId + ':' + turnId + ':call:' + seq, seq, sessionId, turnId, emitter: 'agent',
    emittedAt: timestampFromDate(new Date(1700000000000 + Number(seq) * 1000)),
    payload: { case: 'toolCall', value: { id: callId, name: 'bash' } },
  })
}

function projectTurn(events: ReturnType<typeof toolEvent>[], pending: Review[] = []) {
  return groupGuardrailTurns(withGuardrailReviews(reduceAOPToTimeline(guardrailTimelineEvents(events),
    { streaming: true, lifecycle: 'errors', responseBoundary: isGuardrailBoundary }), pending, events))
}

test('multiple approvals and continuation share one stable turn bubble through resolution and replay', () => {
  const first = review({ command: 'echo first' })
  const second = review({ command: 'echo second' }, 'call-2', 'op-2')
  const firstCall = toolEvent('call-1', 1n)
  const firstRequest = reviewEvent(first, 2n)
  const firstApproved = reviewEvent(create(ReviewSchema, { ...first, state: ReviewState.APPROVED }), 3n)
  const firstResult = create(EventSchema, { ...firstCall, id: 'result-1', seq: 4n,
    payload: { case: 'toolResult', value: { callId: 'call-1', name: 'bash', output: [{ value: { case: 'text', value: { text: 'first' } } }] } } })
  const secondCall = toolEvent('call-2', 5n)
  const secondRequest = reviewEvent(second, 6n)
  const secondRejected = reviewEvent(create(ReviewSchema, { ...second, state: ReviewState.REJECTED }), 7n)
  const secondResult = create(EventSchema, { ...secondCall, id: 'result-2', seq: 8n,
    payload: { case: 'toolResult', value: { callId: 'call-2', name: 'bash', isError: true, output: [] } } })
  const delta = create(EventSchema, { ...secondCall, id: 'delta', seq: 9n,
    payload: { case: 'messageDelta', value: { messageId: 'conclusion', value: { case: 'text', value: 'Finished both' } } } })
  const complete = create(EventSchema, { ...secondCall, id: 'complete', seq: 10n,
    payload: { case: 'message', value: { id: 'conclusion', role: 'assistant', content: [{ value: { case: 'text', value: { text: 'Finished both' } } }] } } })
  const events = [firstCall, firstRequest, firstApproved, firstResult, secondCall, secondRequest, secondRejected, secondResult, delta, complete]
  const initial = projectTurn(events.slice(0, 2), [first])
  const waiting = projectTurn(events.slice(0, 6), [second])
  const streaming = projectTurn(events.slice(0, 9))
  const replay = projectTurn(events)
  for (const result of [initial, waiting, streaming, replay]) {
    expect(result).toHaveLength(1)
    expect(result[0].id).toBe(initial[0].id)
  }
  const turn = waiting[0]
  expect(turn.kind).toBe('assistant_response')
  if (turn.kind !== 'assistant_response') return
  expect(turn.tools.map(tool => tool.id)).toEqual(['call-1', 'call-2'])
  expect(turn.steps?.map(step => step.kind)).toEqual(['assistant_response', 'extension', 'assistant_response', 'extension'])
  expect(turn.steps?.[1]).toMatchObject({ id: 'guardrail:op-1', data: { actionable: false, review: { state: ReviewState.APPROVED } } })
  expect(turn.steps?.[3]).toMatchObject({ id: 'guardrail:op-2', data: { actionable: true, review: { state: ReviewState.PENDING } } })
  expect(streaming[0]).toMatchObject({ streaming: true, response: { content: 'Finished both' } })
  expect(replay[0]).toMatchObject({ streaming: false, response: { content: 'Finished both' } })
  if (replay[0].kind !== 'assistant_response') return
  expect(replay[0].steps?.map(step => step.kind)).toEqual(['assistant_response', 'extension', 'assistant_response', 'extension', 'assistant_response'])
  expect(replay[0].steps?.[3]).toMatchObject({ id: 'guardrail:op-2', data: { actionable: false, review: { state: ReviewState.REJECTED } } })
  expect(projectTurn(events, [first, second])).toEqual(replay)
  expect(firstRequest.emitter).toBe('guardrail')
})

test('turn grouping isolates sessions and turns when tool identifiers are reused', () => {
  const events = [
    ...['turn-1', 'turn-2'].flatMap((turnId, index) => {
      const request = reviewEvent(review({ command: 'echo ' + turnId }, 'call-1', 'op-' + turnId), BigInt(index * 2 + 2))
      return [toolEvent('call-1', BigInt(index * 2 + 1), 'session-1', turnId), { ...request, turnId }]
    }),
    toolEvent('call-1', 5n, 'session-2'),
    { ...reviewEvent(create(ReviewSchema, { ...review({ command: 'echo session two' }, 'call-1', 'op-session-2'), sessionId: 'session-2' }), 6n), sessionId: 'session-2' },
  ]
  const turns = projectTurn(events)
  expect(turns).toHaveLength(3)
  expect(new Set(turns.map(turn => turn.id)).size).toBe(3)
  const ids = turns.map(turn => turn.kind === 'assistant_response' ? turn.steps?.map(step => step.id).filter(id => id.startsWith('guardrail:')) : [])
  expect(ids).toEqual([['guardrail:op-turn-1'], ['guardrail:op-turn-2'], ['guardrail:op-session-2']])
})

test('turn grouping preserves ordinary turns and unmatched reviews', () => {
  const normal = reduceAOPToTimeline([toolEvent('normal', 1n)], { streaming: true })
  expect(groupGuardrailTurns(normal)).toEqual(normal)
  const orphan = withGuardrailReviews(normal, [review({ command: 'echo unmatched' }, 'missing')])
  expect(groupGuardrailTurns(orphan)).toEqual(orphan)
  expect(orphan).toHaveLength(2)
})

test('delegated session reviews stay inside the delegated turn without duplicate root entries', () => {
  const pending = review({ command: 'echo child' })
  const events = [toolEvent('call-1', 1n), reviewEvent(pending, 2n)]
  const child: ViewerTimelineItem = { id: 'child', kind: 'subagent_run', sessionID: 'session-1', timestamp: 1,
    name: 'child', prompt: '', status: 'running', items: reduceAOPToTimeline(guardrailTimelineEvents(events),
      { streaming: true, lifecycle: 'errors', responseBoundary: isGuardrailBoundary }) }
  const result = groupGuardrailTurns(withGuardrailReviews([child], [pending], events))
  expect(result).toHaveLength(1)
  if (result[0].kind !== 'subagent_run') throw new Error('delegated run missing')
  expect(result[0].items).toHaveLength(1)
  const turn = result[0].items[0]
  if (turn.kind !== 'assistant_response') throw new Error('delegated turn missing')
  expect(turn.steps?.[1]).toMatchObject({ id: 'guardrail:op-1', data: { actionable: true } })
  expect(child.items).toHaveLength(2)
})


test('parallel approvals keep independent actions inside a shared turn', () => {
  const first = review({ command: 'echo first' })
  const second = review({ command: 'echo second' }, 'call-2', 'op-2')
  const events = [toolEvent('call-1', 1n), toolEvent('call-2', 2n), reviewEvent(first, 3n), reviewEvent(second, 4n)]
  const waiting = projectTurn(events, [first, second])
  const updated = projectTurn([...events, reviewEvent(create(ReviewSchema, { ...second, state: ReviewState.REJECTED }), 5n)], [first])
  for (const result of [waiting, updated]) {
    expect(result).toHaveLength(1)
    if (result[0].kind !== 'assistant_response') throw new Error('turn missing')
    expect(result[0].steps?.map(step => step.kind)).toEqual(['assistant_response', 'extension', 'extension'])
    expect(result[0].steps?.slice(1).map(step => step.id)).toEqual(['guardrail:op-1', 'guardrail:op-2'])
  }
  if (updated[0].kind !== 'assistant_response') return
  expect(updated[0].steps?.[1]).toMatchObject({ data: { actionable: true, review: { state: ReviewState.PENDING } } })
  expect(updated[0].steps?.[2]).toMatchObject({ data: { actionable: false, review: { state: ReviewState.REJECTED } } })
})


test('turn identity survives replay compaction of the first streaming event', () => {
  const call = toolEvent('call-1', 2n)
  const delta = create(EventSchema, { ...call, id: 'transient-delta', seq: 1n,
    payload: { case: 'messageDelta', value: { messageId: 'thinking', value: { case: 'reasoning', value: 'Checking command' } } } })
  const request = reviewEvent(review({ command: 'echo first' }), 3n)
  const live = projectTurn([delta, call, request])
  const replay = projectTurn([call, request])
  expect(live).toHaveLength(1)
  expect(replay).toHaveLength(1)
  expect(live[0].id).toBe(replay[0].id)
  expect(live[0].id).toBe('session-1:agent:turn-1:response:turn')
})


test('a live review arriving before its event stays after its own user turn even with reused call IDs', () => {
  const previous = review({ command: 'echo repeated' }, 'repeated-call', 'previous-operation')
  const current = review({ command: 'echo repeated' }, 'repeated-call', 'current-operation')
  const firstCall = toolEvent('repeated-call', 1n)
  const request = reviewEvent(previous, 2n)
  const approved = reviewEvent(create(ReviewSchema, { ...previous, state: ReviewState.APPROVED }), 3n)
  const userMessage = create(EventSchema, { ...toolEvent('', 4n, 'session-1', 'turn-2'), id: 'next-user-message',
    payload: { case: 'message', value: { id: 'next-user-message', role: 'user', content: [{ value: { case: 'text', value: { text: 'Repeat in a new turn' } } }] } } })
  const secondCall = toolEvent('repeated-call', 5n, 'session-1', 'turn-2')
  const events = [firstCall, request, approved, userMessage, secondCall]
  const waiting = projectTurn(events, [current])
  expect(waiting.map(item => item.kind)).toEqual(['assistant_response', 'message', 'assistant_response'])
  if (waiting[0].kind !== 'assistant_response' || waiting[2].kind !== 'assistant_response') throw new Error('Turn cards missing')
  expect(waiting[0].steps?.filter(step => step.kind === 'extension')).toMatchObject([
    { id: 'guardrail:previous-operation', data: { actionable: false, review: { state: ReviewState.APPROVED } } },
  ])
  expect(waiting[2].steps?.filter(step => step.kind === 'extension')).toMatchObject([
    { id: 'guardrail:current-operation', data: { actionable: true, review: { state: ReviewState.PENDING } } },
  ])
  const newRequest = { ...reviewEvent(current, 6n), turnId: 'turn-2' }
  const rejected = { ...reviewEvent(create(ReviewSchema, { ...current, state: ReviewState.REJECTED }), 7n), turnId: 'turn-2' }
  const replay = projectTurn([...events, newRequest, rejected])
  expect(replay.map(item => item.id)).toEqual(waiting.map(item => item.id))
  expect(replay[0]).toEqual(waiting[0])
  if (replay[2].kind !== 'assistant_response') throw new Error('Second turn card missing')
  expect(replay[2].steps?.filter(step => step.kind === 'extension')).toMatchObject([
    { id: 'guardrail:current-operation', data: { actionable: false, review: { state: ReviewState.REJECTED } } },
  ])
})


function decisionEvent(action = GuardrailAction.BLOCK, seq = 2n, operationId = 'op-1') {
  return create(EventSchema, { id: 'decision-' + seq, seq, sessionId: 'session-1', turnId: 'turn-1', emitter: 'guardrail',
    extensions: [anyPack(RefSchema, create(RefSchema, { operationId, callId: 'call-1' }))],
    payload: { case: 'extension', value: anyPack(GuardrailDecisionSchema, create(GuardrailDecisionSchema, { action, reason: 'matched policy' })) },
  })
}

test('automatic interceptions share turn history and retain stable counts through replay', () => {
  const call = toolEvent('call-1', 1n)
  const decision = decisionEvent()
  const continuation = create(EventSchema, { ...call, id: 'continue', seq: 4n,
    payload: { case: 'message', value: { id: 'answer', role: 'assistant', content: [{ value: { case: 'text', value: { text: 'Safer plan' } } }] } } })
  const turns = projectTurn([call, decision, continuation])
  expect(turns).toHaveLength(1)
  if (turns[0].kind !== 'assistant_response') throw new Error('turn missing')
  expect(turns[0].steps?.map(step => step.kind)).toEqual(['assistant_response', 'extension', 'assistant_response'])
  expect(turns[0].steps?.[1]).toMatchObject({ id: 'guardrail:op-1', data: { intercepted: true, actionable: false } })
  expect(projectTurn([call, decision, decision, continuation])).toEqual(turns)
})

test('a human review replaces its policy decision without double counting', () => {
  const pending = review({ command: 'echo' })
  const events = [toolEvent('call-1', 1n), decisionEvent(), reviewEvent(pending, 3n)]
  const turn = projectTurn(events, [pending])[0]
  if (turn.kind !== 'assistant_response') throw new Error('turn missing')
  expect(turn.steps?.filter(step => step.kind === 'extension')).toMatchObject([
    { id: 'guardrail:op-1', data: { intercepted: false, actionable: true } },
  ])
  expect(withGuardrailReviews([], [pending], events, false)[0]).toMatchObject({
    data: { awaiting: true, unavailable: true, actionable: false },
  })
})

test('passing decisions never count as interceptions', () => {
  const call = toolEvent('call-1', 1n)
  expect(projectTurn([call, decisionEvent(GuardrailAction.RECORD)])[0]).not.toHaveProperty('steps')
})

test('approval outcome is derived from the exact operation and never a later reused call ID', () => {
  const pending = review({ command: 'echo' })
  const events = [toolEvent('call-1', 1n), reviewEvent(pending, 2n),
    reviewEvent(create(ReviewSchema, { ...pending, state: ReviewState.APPROVED }), 3n)]
  const other = create(EventSchema, { ...toolEvent('call-1', 4n), id:'other-result',
    extensions: [anyPack(RefSchema, create(RefSchema, { operationId:'other',callId:'call-1' }))],
    payload: {case:'toolResult',value:{callId:'call-1',isError:false}} })
  expect(withGuardrailReviews([], [], [...events, other])[0]).toMatchObject({data:{outcome:undefined}})
  for (const isError of [false,true]) {
    const result = create(EventSchema,{...other,extensions:[anyPack(RefSchema,create(RefSchema,{operationId:'op-1',callId:'call-1'}))],
      payload:{case:'toolResult',value:{callId:'call-1',isError}}})
    expect(withGuardrailReviews([], [], [...events,result])[0]).toMatchObject({data:{outcome:isError?'failed':'succeeded'}})
  }
  const completion = create(EventSchema,{...other,
    extensions:[anyPack(RefSchema,create(RefSchema,{operationId:'op-1'}))],
    payload:{case:'extension',value:anyPack(CompletedSchema,create(CompletedSchema,{kind:'tool',failure:{kind:FailureKind.CANCELED}}))}})
  expect(withGuardrailReviews([], [], [...events,completion])[0]).toMatchObject({data:{outcome:'canceled'}})
})

test('a repeated call later in the same turn cannot overwrite an earlier authorization outcome', () => {
  const pending = review({command:'echo'})
  const events = [toolEvent('call-1',1n),reviewEvent(create(ReviewSchema,{...pending,state:ReviewState.APPROVED}),2n),
    toolEvent('call-1',3n),
    create(EventSchema,{...toolEvent('call-1',4n),payload:{case:'toolResult',value:{callId:'call-1',isError:false}}})]
  expect(withGuardrailReviews([],[],events)[0]).toMatchObject({data:{outcome:undefined}})
})
