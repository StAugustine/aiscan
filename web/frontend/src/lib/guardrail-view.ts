import { create } from '@bufbuild/protobuf'
import { RefSchema, CompletedSchema, FailureKind } from '@cyber/aop'
import { anyUnpack, timestampDate } from '@bufbuild/protobuf/wkt'
import { GuardrailDecisionSchema, ReviewSchema, ReviewState, GuardrailAction, type Review } from '../cyber-proto'
import type { AOPEvent, ViewerTimelineItem } from '@/viewer'

function eventReview(event: AOPEvent): Review | undefined {
  if (event.payload.case !== 'extension') return undefined
  try { return anyUnpack(event.payload.value, ReviewSchema) } catch { return undefined }
}

function eventDecision(event: AOPEvent) {
  if (event.payload.case !== 'extension') return undefined
  try { return anyUnpack(event.payload.value, GuardrailDecisionSchema) } catch { return undefined }
}

function operationRef(event: AOPEvent) {
  for (const extension of event.extensions) {
    const ref = anyUnpack(extension, RefSchema)
    if (ref?.operationId) return ref
  }
  return undefined
}

// The shared reducer scopes response boundaries by session, turn and emitter.
// Associate the approval boundary with the tool's agent for presentation only;
// keep the original persisted event and its guardrail emitter untouched.
export function guardrailTimelineEvents(events: AOPEvent[]): AOPEvent[] {
  const actors = new Map<string, string>()
  const key = (event: AOPEvent, callId: string) => JSON.stringify([event.sessionId, event.turnId, callId])
  for (const event of events) {
    if (event.payload.case === 'toolCall') actors.set(key(event, event.payload.value.id), event.emitter)
  }
  return events.map(event => {
    const review = eventReview(event)
    const decision = eventDecision(event)
    const callId = review?.call?.id || operationRef(event)?.callId
    if (!callId || !(review?.state === ReviewState.PENDING || (decision && decision.action > GuardrailAction.RECORD))) return event
    const emitter = actors.get(key(event, callId))
    return emitter === undefined ? event : { ...event, emitter }
  })
}

export function isGuardrailBoundary(event: AOPEvent): boolean {
  const decision = eventDecision(event)
  return eventReview(event)?.state === ReviewState.PENDING || !!(decision && decision.action > GuardrailAction.RECORD)
}

export function guardrailArguments(review: Review) {
  const text = new TextDecoder().decode(review.call?.arguments?.data)
  let value: unknown = text
  try { value = JSON.parse(text) } catch { /* Preserve plain tool input. */ }
  const record = value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : null
  const commandKey = record && ['command', 'cmd', 'script'].find(key => typeof record[key] === 'string')
  const command = commandKey && record ? record[commandKey] as string : typeof value === 'string' ? value : null
  const fields = record ? Object.entries(record).filter(([key]) => key !== commandKey).map(([key, item]) => [key, typeof item === 'string' ? item : JSON.stringify(item, null, 2)]) : []
  if (command === null && !record) fields.push(['input', JSON.stringify(value, null, 2)])
  return { command, fields, raw: typeof value === 'string' ? text : JSON.stringify(value, null, 2) }
}

export function isResolvedReview(review: Review): boolean {
  return [ReviewState.APPROVED, ReviewState.REJECTED, ReviewState.EXPIRED, ReviewState.CANCELED].includes(review.state)
}

// Durable events own history; only the live runtime query can enable actions.
// A replayed pending event must never resurrect an already resolved approval.
export function withGuardrailReviews(items: ViewerTimelineItem[], reviews: Review[], events: AOPEvent[] = [], connected = true): ViewerTimelineItem[] {
  // A delegated session owns its reviews just as it owns its tool responses.
  const childSessions = new Set(items.flatMap(item => item.kind === 'subagent_run' && item.sessionID ? [item.sessionID] : []))
  const result = items.map(item => item.kind === 'subagent_run' && item.sessionID
    ? { ...item, items: withGuardrailReviews(item.items, reviews.filter(review => review.sessionId === item.sessionID), events.filter(event => event.sessionId === item.sessionID), connected) }
    : item)
  const history = new Map<string, { review: Review; event?: AOPEvent }>()
  const started = new Map<string, AOPEvent>()
  for (const event of events) {
    if (childSessions.has(event.sessionId)) continue
    const review = eventReview(event)
    const id = review?.operation?.operationId
    if (!review || !id || review.state === ReviewState.UNSPECIFIED) continue
    const first = started.get(id)
    if (!first || event.seq < first.seq) started.set(id, event)
    const previous = history.get(id)
    if (previous && isResolvedReview(previous.review) && !isResolvedReview(review)) continue
    if (previous?.event && isResolvedReview(previous.review) === isResolvedReview(review) && previous.event.seq > event.seq) continue
    history.set(id, { review, event })
  }
  // A Decision without a Review is still an interception, including auto mode.
  // Its original operation identity makes the later human Review replace it.
  const intercepted = new Set<string>()
  for (const event of events) {
    if (childSessions.has(event.sessionId)) continue
    const decision = eventDecision(event)
    const ref = operationRef(event)
    if (!decision || decision.action <= GuardrailAction.RECORD || !ref || history.has(ref.operationId)) continue
    const callEvent = [...events].reverse().find(candidate => candidate.payload.case === 'toolCall'
      && candidate.sessionId === event.sessionId && candidate.turnId === event.turnId
      && candidate.payload.value.id === ref.callId && candidate.seq <= event.seq)
    const review = create(ReviewSchema, { operation: ref, sessionId: event.sessionId, decision,
      call: callEvent?.payload.case === 'toolCall' ? callEvent.payload.value : undefined })
    history.set(ref.operationId, { review, event })
    started.set(ref.operationId, event)
    intercepted.add(ref.operationId)
  }
  const pending = new Set<string>()
  for (const review of reviews) {
    if (childSessions.has(review.sessionId)) continue
    const id = review.operation?.operationId
    if (!id || review.state !== ReviewState.PENDING) continue
    pending.add(id)
    intercepted.delete(id)
    const previous = history.get(id)
    if (!previous || !isResolvedReview(previous.review)) history.set(id, { ...previous, review })
  }
  const contains = (item: ViewerTimelineItem, id: string, responsePrefix?: string): boolean => {
    if (item.kind === 'assistant_response') return (!responsePrefix || item.id.startsWith(responsePrefix)) && item.tools.some(tool => tool.id === id)
    if (item.kind === 'tool_call') return item.toolCall.id === id
    if (item.kind === 'subagent_run') return item.items.some(child => contains(child, id, responsePrefix))
    return false
  }
  for (const [id, { review, event }] of history) {
    const requested = started.get(id)
    const invocation = [...events].reverse().find(candidate => candidate.payload.case === 'toolCall'
      && candidate.payload.value.id === review.call?.id
      && candidate.sessionId === (requested?.sessionId || review.sessionId)
      && (!requested || (candidate.turnId === requested.turnId && candidate.seq <= requested.seq)))
    const responsePrefix = invocation ? invocation.sessionId + ':' + invocation.emitter + ':' + (invocation.turnId || 'session') + ':response:' : undefined
    const eventIndex = requested ? result.findIndex(item => item.id === requested.id && item.kind === 'extension') : -1
    // Live pending queries can arrive before their event. A reused call ID must
    // still anchor to this turn, never to an earlier conversation bubble.
    const index = review.call?.id ? result.findIndex(item => contains(item, review.call!.id, responsePrefix)) : -1
    const anchor = index >= 0 ? result[index] : result[result.length - 1]
    const sameOperation = (candidate: AOPEvent) => {
      const ref = operationRef(candidate)
      if (ref) return ref.operationId === id && candidate.sessionId === review.sessionId
      return !!invocation && candidate.sessionId === invocation.sessionId && candidate.turnId === invocation.turnId
        && candidate.seq > invocation.seq && !events.some(next => next.payload.case === 'toolCall'
          && next.payload.value.id === review.call?.id && next.sessionId === invocation.sessionId
          && next.turnId === invocation.turnId && next.seq > invocation.seq && next.seq < candidate.seq)
    }
    const completedEvent = [...events].reverse().find(candidate => candidate.payload.case === 'extension'
      && operationRef(candidate)?.operationId === id && candidate.sessionId === review.sessionId
      && anyUnpack(candidate.payload.value, CompletedSchema))
    const completed = completedEvent?.payload.case === 'extension' ? anyUnpack(completedEvent.payload.value, CompletedSchema) : undefined
    const resultEvent = [...events].reverse().find(candidate => candidate.payload.case === 'toolResult'
      && candidate.payload.value.callId === review.call?.id && sameOperation(candidate))
    const toolResult = resultEvent?.payload.case === 'toolResult' ? resultEvent.payload.value : undefined
    const outcome = review.state !== ReviewState.APPROVED ? undefined
      : completed?.failure?.kind === FailureKind.CANCELED ? 'canceled'
      : completed && !completed.startedAt ? 'notStarted'
      : toolResult ? (toolResult.isError ? 'failed' : 'succeeded')
      : completed?.failure ? 'failed' : undefined
    const item: ViewerTimelineItem = {
      id: 'guardrail:' + id, kind: 'extension', extensionType: 'guardrail',
      // Resolving updates this entry in place; it never moves behind later work.
      timestamp: requested?.emittedAt ? timestampDate(requested.emittedAt).getTime() : anchor?.timestamp ?? 0,
      actorName: review.call?.name,
      data: { review, intercepted: intercepted.has(id), outcome, actionable: connected && pending.has(id) && !isResolvedReview(review),
        awaiting: pending.has(id) && !isResolvedReview(review),
        unavailable: !connected && pending.has(id) && !isResolvedReview(review),
        responsePrefix,
        reviewedAt: isResolvedReview(review) && event?.emittedAt ? timestampDate(event.emittedAt).getTime() : undefined },
    }
    // Render in the canonical event's slot, even when different turns reuse a
    // call ID. Tool anchoring is only a fallback until live events have arrived.
    if (eventIndex >= 0) {
      result[eventIndex] = item
      continue
    }
    let insertion = index >= 0 ? index + 1 : result.length
    while (insertion < result.length && result[insertion].kind === 'extension' && result[insertion].id.startsWith('guardrail:')) insertion++
    result.splice(insertion, 0, item)
  }
  // Later state updates do not create more rows or leave empty grid entries.
  return result.filter(item => item.kind !== 'extension'
    || ![ReviewSchema.typeName, GuardrailDecisionSchema.typeName].some(name => item.extensionType === name || item.extensionType.endsWith('/' + name)))
}

// Keep each turn's session/actor/turn identity and renderer. Approval pauses are
// chronological inner steps, not additional conversation bubbles or protocol DTOs.
export function groupGuardrailTurns(items: ViewerTimelineItem[]): ViewerTimelineItem[] {
  const source = items.map(item => item.kind === 'subagent_run' ? { ...item, items: groupGuardrailTurns(item.items) } : item)
  const prefixes = [...new Set(source.flatMap(item => item.kind === 'extension' && item.extensionType === 'guardrail'
    && typeof item.data.responsePrefix === 'string' ? [item.data.responsePrefix] : []))]
  const firstResponses = new Map(prefixes.map(prefix => [prefix, source.find(item => item.kind === 'assistant_response' && item.id.startsWith(prefix))]))
  const grouped = new Map<string, Extract<ViewerTimelineItem, { kind: 'assistant_response' }>>()
  const result: ViewerTimelineItem[] = []
  for (const item of source) {
    const prefix = item.kind === 'assistant_response' ? prefixes.find(value => item.id.startsWith(value))
      : item.kind === 'extension' && item.extensionType === 'guardrail' ? item.data.responsePrefix as string | undefined : undefined
    const first = prefix ? firstResponses.get(prefix) : undefined
    if (!prefix || first?.kind !== 'assistant_response' || (item.kind !== 'assistant_response' && item.kind !== 'extension')) {
      result.push(item)
      continue
    }
    let turn = grouped.get(prefix)
    if (!turn) {
      // Live deltas can disappear from compacted history, so the first segment's
      // event ID is not a durable identity for the containing turn.
      turn = { ...first, id: prefix + 'turn', tools: [], steps: [], streaming: false }
      grouped.set(prefix, turn)
      result.push(turn)
    }
    turn.steps!.push(item)
    if (item.kind === 'assistant_response') {
      turn.tools.push(...item.tools)
      turn.streaming = item.streaming
      turn.thinking = item.thinking
      turn.response = item.response
    }
  }
  return result
}
