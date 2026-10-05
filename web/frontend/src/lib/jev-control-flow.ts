import type { AOPEvent } from '@/viewer'
import { eventTime } from './jev-view'
import type { WorkflowNode, WorkflowState } from './workflow-view'

export type ControlStage = 'model' | 'judgment' | 'execution' | 'feedback' | 'return' | 'background'
export type ControlFrame = { id: string; nodeId: string; timestamp: number; stage: ControlStage; state: WorkflowState; eventId?: string }

export function controlFeedback(node: WorkflowNode): string {
  const result = node.related?.find(record => record.value.payload.case === 'result')?.value.payload
  const payload = node.record?.value.payload
  const output = result?.case === 'result' ? result.value.result?.output.flatMap(part => part.value.case === 'text' ? [part.value.value.text] : []).join(' ')
    : node.item?.kind === 'tool_call' ? node.item.toolCall.result : payload?.case === 'observation' ? payload.value.stateJson : undefined
  return (output || '').replace(/\s+/g, ' ').trim().slice(0, 160)
}

function stage(node: WorkflowNode, event?: AOPEvent): ControlStage {
  if (node.background) return 'background'
  const payload = node.related?.find(record => record.event.id === event?.id)?.value.payload
  if (payload?.case === 'result' || event?.payload.case === 'toolResult' || node.kind === 'observation') return 'feedback'
  if (node.kind === 'handoff' || node.kind === 'response' || node.kind === 'boundary' && payload?.case === 'boundary' && payload.value.reason !== 'checking') return 'return'
  if (node.kind === 'tool' || node.kind === 'agent' || node.kind === 'guardrail') return 'execution'
  if (node.actor === 'JEV') return 'judgment'
  return 'model'
}

export function controlFrames(nodes: WorkflowNode[]): ControlFrame[] {
  return nodes.flatMap(node => {
    const events = node.related?.map(record => record.event) || node.events || []
    if (!events.length) return [{ id: node.id, nodeId: node.id, timestamp: node.timestamp, stage: stage(node), state: node.state }]
    return events.map(event => {
      const payload = node.related?.find(record => record.event.id === event.id)?.value.payload
      const pending = payload?.case === 'decisionRequest' || payload?.case === 'dispatch'
        || payload?.case === 'generation' && payload.value.state === 'started' || event.payload.case === 'toolCall'
      const failed = payload?.case === 'decisionResult' && !!payload.value.error || payload?.case === 'result' && !!payload.value.result?.isError
        || payload?.case === 'generation' && !!payload.value.error || payload?.case === 'libraryChange' && ['failed', 'draft_rejected'].includes(payload.value.state)
        || event.payload.case === 'toolResult' && event.payload.value.isError
      return { id: JSON.stringify([node.id, event.id]), nodeId: node.id, eventId: event.id, timestamp: eventTime(event), stage: stage(node, event),
        state: failed ? 'failed' : pending ? 'pending' : 'completed' } satisfies ControlFrame
    })
  }).sort((a, b) => a.timestamp - b.timestamp)
}

// Replay never reveals an answer, result or later publication before its event.
export function controlSnapshot(nodes: WorkflowNode[], frames: ControlFrame[], cursor: number): WorkflowNode[] {
  const seen = frames.slice(0, cursor + 1), ids = new Set(seen.map(frame => frame.nodeId))
  return nodes.filter(node => ids.has(node.id)).map(node => {
    const nodeFrames = seen.filter(frame => frame.nodeId === node.id), last = nodeFrames[nodeFrames.length - 1]
    const eventIds = new Set(nodeFrames.map(frame => frame.eventId))
    const related = node.related?.filter(record => eventIds.has(record.event.id))
    const result = related?.find(record => record.value.payload.case === 'result')?.value.payload
    const nativeResult = node.events?.find(event => eventIds.has(event.id) && event.payload.case === 'toolResult')?.payload
    return { ...node, state: last.state, related,
      step: node.step ? { ...node.step, result: result?.case === 'result' ? result.value.result : undefined,
        observations: node.step.observations.filter(event => eventTime(event) <= last.timestamp) } : undefined,
      item: node.item?.kind === 'tool_call' ? { ...node.item, toolCall: { ...node.item.toolCall,
        pending: last.state === 'pending', error: last.state === 'failed', result: nativeResult?.case === 'toolResult' ? node.item.toolCall.result : undefined,
        toolResult: nativeResult?.case === 'toolResult' ? nativeResult.value : undefined,
        observations: node.item.toolCall.observations?.filter(event => eventTime(event) <= last.timestamp) } } : node.item,
    }
  })
}
