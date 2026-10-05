import type { Node, Edge } from '@xyflow/react'
import type { Event as AOPEvent } from '../../cyber-ui/packages/aop/src/gen/aop/event_pb.js'
import { eventTime, jevEvent, runtimeEvents, type JEVProjection } from './jev-view'
import { anyUnpack } from '@bufbuild/protobuf/wkt'
import { RefSchema, StartedSchema } from '../../cyber-ui/packages/aop/src/gen/aop/operation/protocol_pb.js'

export type RuntimeNode = Node<{ label: string; kind: 'agent' | 'jev' | 'tool'; status: string; detail: string; sessionId: string; vertical?: boolean; nativeTool?: string; callIds?: string[] }>
export type RuntimeGraph = { nodes: RuntimeNode[]; edges: Edge[]; turn?: AOPEvent; segments?: JEVProjection['segments'] }

export function runtimeTurns(events: readonly AOPEvent[]): AOPEvent[] {
  const delegated = new Set(events.filter(e => e.payload.case === 'sessionStarted' && !!e.payload.value.parentToolCallId).map(e => e.sessionId))
  const seen = new Set<string>()
  return events.filter(e => {
    const key = JSON.stringify([e.sessionId, e.turnId])
    if (e.payload.case !== 'turnStarted' || delegated.has(e.sessionId) || seen.has(key)) return false
    seen.add(key); return true
  })
}
export const turnKey = (event: AOPEvent) => JSON.stringify([event.sessionId, event.turnId])

// Nodes and routes come from actual session/delegation/call events, never from
// parsing the generated Observe source or assuming a particular tool family.
export function projectRuntimeNetwork(source: readonly AOPEvent[], projection: JEVProjection, selected?: string, vertical = false): RuntimeGraph {
  const events = runtimeEvents(source)
  const turns = runtimeTurns(events)
  const turn = turns.find(e => turnKey(e) === selected) || turns[turns.length - 1]
  if (!turn) return { nodes: [], edges: [] }
  const sessions = new Map<string, AOPEvent | undefined>([[turn.sessionId, undefined]])
  const nextTurn = turns[turns.indexOf(turn) + 1]
  const starts = events.filter(e => e.payload.case === 'sessionStarted' && !!e.payload.value.parentToolCallId && eventTime(e) >= eventTime(turn)
    && (!nextTurn || eventTime(e) < eventTime(nextTurn)))
  for (let pass = 0; pass < starts.length; pass++) {
    let changed = false
    for (const start of starts) {
      if (start.payload.case !== 'sessionStarted' || sessions.has(start.sessionId) || !sessions.has(start.payload.value.parentSessionId)) continue
      sessions.set(start.sessionId, start); changed = true
    }
    if (!changed) break
  }
  const inTurn = (session: string, turnId: string, timestamp: number) => sessions.has(session) && (session === turn.sessionId
    ? turnId === turn.turnId : timestamp >= eventTime(turn) && (!nextTurn || timestamp < eventTime(nextTurn)))
  const scoped = events.filter(e => inTurn(e.sessionId, e.turnId, eventTime(e)))
  const segments = projection.segments.filter(s => inTurn(s.sessionId, s.turnId, s.timestamp))
  const nodes: RuntimeNode[] = [], routes = new Map<string, Edge>()
  const route = (source: string, target: string, label: string, count = 1, active = false) => {
    const id = JSON.stringify([source, target, label]), previous = routes.get(id)
    const total = Number(previous?.data?.count || 0) + count
    routes.set(id, { id, source, target, label: `${label} · ${total}`, animated: active || previous?.animated,
      ...(['handoff', 'results', 'modelCalls'].includes(label) ? { sourceHandle: 'feedback-out', targetHandle: 'feedback-in' } : {}),
      type: 'smoothstep', data: { count: total }, style: { stroke: 'hsl(var(--muted-foreground))' },
      labelStyle: { fill: 'hsl(var(--foreground))', fontSize: 11 }, labelBgStyle: { fill: 'hsl(var(--background))' } })
  }
  let y = 0
  for (const [sessionId, start] of sessions) {
    const own = scoped.filter(e => e.sessionId === sessionId && !jevEvent(e)), ownSegments = segments.filter(s => s.sessionId === sessionId)
    const ended = own.some(e => e.payload.case === 'turnEnded' || e.payload.case === 'sessionEnded')
    const running = ownSegments.find(s => s.status === 'running')
    const agentId = `agent:${sessionId}`, jevId = `jev:${sessionId}`
    const label = start?.emitter || turn.emitter || 'Agent'
    nodes.push({ id: agentId, type: 'runtime', position: { x: 0, y }, data: { label, kind: 'agent',
      status: ended ? 'ended' : running ? 'delegated' : 'model', detail: sessionId, sessionId } })
    if (start?.payload.case === 'sessionStarted') route(`agent:${start.payload.value.parentSessionId}`, agentId, 'delegation')
    const decisions = projection.records.filter(r => !r.value.background && r.event.sessionId === sessionId
      && inTurn(r.event.sessionId, r.event.turnId, eventTime(r.event)) && r.value.payload.case === 'decisionResult')
    const checks = projection.checks.filter(check => check.sessionId === sessionId && inTurn(check.sessionId, check.turnId, check.timestamp))
    if (ownSegments.length || decisions.length || checks.length) {
      nodes.push({ id: jevId, type: 'runtime', position: { x: 300, y }, data: { label: 'JEV', kind: 'jev',
        status: running ? 'running' : checks.some(check => check.status === 'running') ? 'checking' : 'handed_off', detail: running?.definition?.id || ownSegments[ownSegments.length - 1]?.definition?.id || '', sessionId } })
      if (decisions.length) route(agentId, jevId, 'judgments', decisions.length, !!running)
      const entries = checks.flatMap(check => check.records).filter(record => record.value.payload.case === 'boundary' && record.value.payload.value.reason === 'checking').length
      if (entries) route(agentId, jevId, 'checks', entries)
      for (const segment of ownSegments) if (segment.status !== 'running') route(jevId, agentId, 'handoff')
    }
    const tools = new Map<string, { model: number; controller: number; returned: number; pending: boolean }>()
    const completed = new Set(own.flatMap(event => event.payload.case === 'toolResult' ? [event.payload.value.callId] : []))
    for (const event of own) {
      if (event.payload.case !== 'toolCall') continue
      const name = event.payload.value.name
      const record = tools.get(name) || { model: 0, controller: 0, returned: 0, pending: false }
      record.model++; record.pending ||= !completed.has(event.payload.value.id) && !ended; tools.set(name, record)
    }
    for (const segment of ownSegments) for (const step of segment.steps) {
      if (!step.call) continue
      const record = tools.get(step.call.name) || { model: 0, controller: 0, returned: 0, pending: false }
      record.controller++; if (step.result) record.returned++
      record.pending ||= !step.result && segment.status === 'running'; tools.set(step.call.name, record)
    }
    let index = 0
    for (const [name, record] of tools) {
      const id = `tool:${JSON.stringify([sessionId, name])}`
      nodes.push({ id, type: 'runtime', position: { x: 610, y: y + index++ * 125 }, data: { label: name, kind: 'tool',
        status: record.pending ? 'executing' : 'observed', detail: `${record.model + record.controller}`, nativeTool: name, sessionId } })
      if (record.model) route(agentId, id, 'modelCalls', record.model)
      if (record.controller) route(jevId, id, 'dispatches', record.controller, record.pending)
      if (record.returned) route(id, jevId, 'results', record.returned)
    }
    const commandCalls = new Map<string, string>()
    for (const event of own) if (event.payload.case === 'toolCall') commandCalls.set(event.payload.value.id, event.payload.value.name)
    for (const segment of ownSegments) for (const step of segment.steps) if (step.call) commandCalls.set(step.call.id, step.call.name)
    const commands = new Map<string, { parent: string; name: string; count: number; callIds: string[] }>()
    for (const event of own) {
      if (event.payload.case !== 'extension') continue
      try {
        const started = anyUnpack(event.payload.value, StartedSchema)
        if (started?.kind !== 'command') continue
        const ref = event.extensions.map(extension => anyUnpack(extension, RefSchema)).find(Boolean)
        const parent = ref && commandCalls.get(ref.callId)
        if (!parent) continue
        const key = JSON.stringify([parent, started.name]), previous = commands.get(key)
        commands.set(key, { parent, name: started.name, count: (previous?.count || 0) + 1, callIds: [...(previous?.callIds || []), ref!.callId] })
      } catch { /* An invalid optional observation cannot invent a route. */ }
    }
    let commandIndex = 0
    for (const command of commands.values()) {
      const id = `command:${JSON.stringify([sessionId, command.parent, command.name])}`
      nodes.push({ id, type: 'runtime', position: { x: 920, y: y + commandIndex++ * 125 }, data: {
        label: command.name, kind: 'tool', status: tools.get(command.parent)?.pending ? 'executing' : 'observed', detail: String(command.count), nativeTool: command.parent, sessionId, callIds: command.callIds,
      } })
      route(`tool:${JSON.stringify([sessionId, command.parent])}`, id, 'nativeCommand', command.count)
    }
    y += Math.max(1, tools.size, commands.size) * 125 + 120
  }
  if (vertical) nodes.forEach((node, index) => {
    node.position = { x: 0, y: index * 140 }
    node.data.vertical = true
  })
  return { nodes, edges: [...routes.values()], turn, segments }
}
