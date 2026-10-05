import { anyUnpack } from '@bufbuild/protobuf/wkt'
import { RuntimeEventSchema, type RuntimeEvent, type ReflexDefinition } from '../gen/types/jev_pb.js'
import { RefSchema } from '../../cyber-ui/packages/aop/src/gen/aop/operation/protocol_pb.js'
import type { Event as AOPEvent } from '../../cyber-ui/packages/aop/src/gen/aop/event_pb.js'
import type { ToolCall, ToolResult } from '../../cyber-ui/packages/aop/src/gen/aop/content_pb.js'
import type { ViewerTimelineItem } from '../viewer'

export type JEVRecord = { event: AOPEvent; value: RuntimeEvent }
export type JEVStep = {
  index: number; call?: ToolCall; result?: ToolResult; elapsedMs?: number
  candidate?: string; read?: boolean; observations: AOPEvent[]; records: JEVRecord[]
}
export type JEVSegment = {
  id: string; sessionId: string; turnId: string; taskId: string; previousId: string
  timestamp: number; definition?: ReflexDefinition; records: JEVRecord[]; steps: JEVStep[]
  status: 'running' | 'handed_off' | 'ended'; reason: string
}
export type JEVCompilation = {
  id: string; sessionId: string; turnId: string; taskId: string; timestamp: number
  records: JEVRecord[]; state: string; foregroundCalls?: { call: ToolCall; result?: ToolResult }[]
}
export type JEVProjection = { segments: JEVSegment[]; compilations: JEVCompilation[]; records: JEVRecord[]; checks: JEVCheck[] }
export type JEVCheck = Omit<JEVSegment, 'definition' | 'steps' | 'previousId'> & { iteration?: number; previousId?: string; nextId?: string }

const decoded = new WeakMap<AOPEvent, RuntimeEvent | undefined>()
export function jevEvent(event: AOPEvent): RuntimeEvent | undefined {
  if (event.payload.case !== 'extension') return undefined
  if (!decoded.has(event)) {
    try { decoded.set(event, anyUnpack(event.payload.value, RuntimeEventSchema)) }
    catch { decoded.set(event, undefined) }
  }
  return decoded.get(event)
}
export function eventTime(event: AOPEvent): number {
  return event.emittedAt ? Number(event.emittedAt.seconds) * 1000 + event.emittedAt.nanos / 1_000_000 : 0
}
const scope = (session: string, turn: string, id = '') => JSON.stringify([session, turn, id])

// Chat and the runtime network share this projection. Only foreground events
// change segment state; compilation retains its source turn.
export function runtimeEvents(source: readonly AOPEvent[]): AOPEvent[] {
  const unique = new Map<string, AOPEvent>()
  source.forEach((event, index) => {
    const id = scope(event.sessionId, '', event.id || `${event.emitter}:${event.seq || index}`)
    if (!unique.has(id)) unique.set(id, event)
  })
  return [...unique.values()].sort((a, b) => eventTime(a) - eventTime(b)
    || (a.sessionId === b.sessionId ? Number(a.seq - b.seq) : 0))
}

export function projectJEV(source: readonly AOPEvent[]): JEVProjection {
  const events = runtimeEvents(source)
  const ended = new Set(events.filter(e => e.payload.case === 'turnEnded' || e.payload.case === 'sessionEnded')
    .map(e => scope(e.sessionId, e.payload.case === 'sessionEnded' ? '' : e.turnId)))
  const segments = new Map<string, JEVSegment>(), compilations = new Map<string, JEVCompilation>()
  const records: JEVRecord[] = [], calls = new Map<string, JEVStep>()
  for (const event of events) {
    const value = jevEvent(event)
    if (!value) continue
    const record = { event, value }; records.push(record)
    if (value.background) {
      const id = scope(event.sessionId, event.turnId, value.taskId)
      let compilation = compilations.get(id)
      if (!compilation) compilations.set(id, compilation = { id, sessionId: event.sessionId, turnId: event.turnId,
        taskId: value.taskId, timestamp: eventTime(event), records: [], state: 'reviewing' })
      compilation.records.push(record)
      if (value.payload.case === 'decisionRequest') compilation.state = 'reviewing'
      if (value.payload.case === 'decisionResult') compilation.state = value.payload.value.error ? 'failed' : 'reviewing'
      if (value.payload.case === 'libraryChange') {
        const state = value.payload.value.state
        if (state !== 'settled') compilation.state = state
        else if (!['reflex_published', 'reflex_candidate', 'claim_published', 'failed', 'deferred', 'retired'].includes(compilation.state)) compilation.state = 'deferred'
      }
      if (value.payload.case === 'generation') compilation.state = value.payload.value.error ? 'failed'
        : value.payload.value.state === 'started' ? 'generating' : 'reviewing'
      continue
    }
    if (!value.segmentId) continue
    const id = scope(event.sessionId, event.turnId, value.segmentId)
    let segment = segments.get(id)
    if (!segment) segments.set(id, segment = { id: value.segmentId, sessionId: event.sessionId, turnId: event.turnId,
      taskId: value.taskId, previousId: value.previousSegmentId, timestamp: eventTime(event), records: [], steps: [], status: 'running', reason: '' })
    segment.records.push(record)
    if (value.payload.case === 'takeover') segment.definition = value.payload.value.definition
    if (value.payload.case === 'handoff') { segment.status = 'handed_off'; segment.reason = value.payload.value.reason }
    if (value.step) {
      let step = segment.steps.find(s => s.index === value.step)
      if (!step) segment.steps.push(step = { index: value.step, observations: [], records: [] })
      step.records.push(record)
      if (value.payload.case === 'dispatch') {
        step.call = value.payload.value.call; step.candidate = value.payload.value.candidateId; step.read = value.payload.value.read
        if (step.call?.id) calls.set(scope(event.sessionId, event.turnId, step.call.id), step)
      }
      if (value.payload.case === 'result') { step.result = value.payload.value.result; step.elapsedMs = Number(value.payload.value.elapsedMs) }
    }
  }
  const operations = new Map<string, JEVStep>()
  const references = events.flatMap(event => {
    if (jevEvent(event)) return []
    for (const extension of event.extensions) {
      try { const ref = anyUnpack(extension, RefSchema); if (ref) return [{ event, ref }] } catch { /* Optional sidecar. */ }
    }
    return []
  })
  // Resolve explicit call IDs before operation ancestry; never guess by time.
  for (let pass = 0; pass <= references.length; pass++) {
    let changed = false
    for (const { event, ref } of references) {
      const key = scope(event.sessionId, event.turnId, ref.operationId)
      if (operations.has(key)) continue
      const step = calls.get(scope(event.sessionId, event.turnId, ref.callId))
        || operations.get(scope(event.sessionId, event.turnId, ref.parentOperationId))
      if (step && ref.operationId) { operations.set(key, step); changed = true }
    }
    if (!changed) break
  }
  for (const { event, ref } of references) {
    const step = calls.get(scope(event.sessionId, event.turnId, ref.callId)) || operations.get(scope(event.sessionId, event.turnId, ref.operationId))
    if (step) step.observations.push(event)
  }
  const visible = [...segments.values()].filter(segment => segment.definition)
  for (const segment of visible) {
    if (segment.status === 'running' && (ended.has(scope(segment.sessionId, segment.turnId)) || ended.has(scope(segment.sessionId, '')))) {
      segment.status = 'ended'; segment.reason = 'turn_ended'
    }
  }
  const checks = new Map<string, JEVCheck>()
  for (const compilation of compilations.values()) {
    const taskEvents = events.filter(event => event.sessionId === compilation.sessionId && event.turnId === compilation.turnId && !jevEvent(event))
    const results = new Map(taskEvents.flatMap(event => event.payload.case === 'toolResult' ? [[event.payload.value.callId, event.payload.value] as const] : []))
    compilation.foregroundCalls = taskEvents.flatMap(event => event.payload.case === 'toolCall' && event.emitter !== 'jev'
      ? [{ call: event.payload.value, result: results.get(event.payload.value.id) }] : [])
  }
  for (const segment of segments.values()) {
    if (segment.definition) continue
    const key = scope(segment.sessionId, segment.turnId, segment.id)
    const check: JEVCheck = { id: key, sessionId: segment.sessionId, turnId: segment.turnId, taskId: segment.taskId,
      timestamp: segment.timestamp, records: segment.records, status: 'running', reason: '' }
    checks.set(key, check)
    for (const record of segment.records) if (record.value.payload.case === 'boundary') {
      check.reason = record.value.payload.value.reason
      check.status = check.reason === 'checking' ? 'running' : 'handed_off'
    }
    if (ended.has(scope(segment.sessionId, segment.turnId)) || ended.has(scope(segment.sessionId, ''))) check.status = 'ended'
  }
  const previousChecks = new Map<string, JEVCheck>()
  for (const check of checks.values()) {
    const key = scope(check.sessionId, check.turnId)
    const previous = previousChecks.get(key)
    check.iteration = (previous?.iteration || 0) + 1
    if (previous) { check.previousId = previous.id; previous.nextId = check.id }
    previousChecks.set(key, check)
  }
  return { segments: visible, compilations: [...compilations.values()], records, checks: [...checks.values()] }
}

export function isJEVBoundary(event: AOPEvent): boolean {
  const value = jevEvent(event)
  return !value?.background && (value?.payload.case === 'takeover' || value?.payload.case === 'handoff' || value?.payload.case === 'boundary')
}
export function jevTimelineEvents(events: AOPEvent[]): AOPEvent[] {
  const actors = new Map<string, string>()
  return events.map(event => {
    const key = scope(event.sessionId, event.turnId)
    if (event.emitter !== 'jev' && ['turnStarted', 'textDelta', 'thinkingDelta', 'toolCall', 'message'].includes(event.payload.case || '')) actors.set(key, event.emitter)
    return isJEVBoundary(event) && actors.has(key) ? { ...event, emitter: actors.get(key)! } : event
  })
}

export function withJEV(items: ViewerTimelineItem[], projection: JEVProjection): ViewerTimelineItem[] {
  const children = new Set(items.flatMap(item => item.kind === 'subagent_run' && item.sessionID ? [item.sessionID] : []))
  const attached = new Set(projection.segments.flatMap(segment => segment.steps.flatMap(step => step.observations.map(event => event.id))))
  const result = items.filter(item => item.kind !== 'extension' || !(item.extensionType.endsWith(RuntimeEventSchema.typeName)
    || (item.event && attached.has(item.event.id))))
    .map(item => item.kind === 'subagent_run' && item.sessionID ? { ...item, items: withJEV(item.items, {
      ...projection, segments: projection.segments.filter(s => s.sessionId === item.sessionID), compilations: projection.compilations.filter(c => c.sessionId === item.sessionID), checks: projection.checks.filter(c => c.sessionId === item.sessionID),
    }) } : item)
  for (const segment of projection.segments) {
    if (!children.has(segment.sessionId)) result.push({ id: `jev:${segment.id}`, kind: 'extension', extensionType: 'jev_segment',
      timestamp: segment.timestamp, actorName: 'JEV', data: { segment } })
  }
  for (const compilation of projection.compilations) {
    if (!children.has(compilation.sessionId)) result.push({ id: `jev_compile:${compilation.id}`, kind: 'extension', extensionType: 'jev_compilation',
      timestamp: compilation.timestamp, actorName: 'JEV', data: { compilation } })
  }
  for (const check of projection.checks) {
    if (!children.has(check.sessionId)) result.push({ id: `jev_check:${check.id}`, kind: 'extension', extensionType: 'jev_check',
      timestamp: check.timestamp, actorName: 'JEV', data: { check } })
  }
  return result.sort((a, b) => a.timestamp - b.timestamp || a.id.localeCompare(b.id))
}
