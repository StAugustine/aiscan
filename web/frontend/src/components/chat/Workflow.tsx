import { useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { ArrowDownToLine, Bot, Check, ChevronDown, ChevronLeft, ChevronRight, CircuitBoard, Eye, FileText, GitBranch, Layers, Loader2, MessageSquare, PanelRight, Repeat2, Shield, Wrench, XCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { ViewerTimelineItem } from '@/viewer'
import { workflowNodeSummary, workflowPositions, type WorkflowNode, type WorkflowTurn } from '../../lib/workflow-view'
import { controlFrames, controlSnapshot } from '../../lib/jev-control-flow'
import { JEVWorkflowDetail } from './JEVTimeline'
import { ControlPlayback, JEVControlFlow } from './JEVControlFlow'
import './Workflow.css'

const icons = { decision: CircuitBoard, tool: Wrench, observation: Eye, takeover: Repeat2, handoff: Repeat2, boundary: CircuitBoard,
  generation: Bot, publication: Layers, reasoning: Bot, response: MessageSquare, agent: GitBranch, guardrail: Shield }
type Route = { id: string; source: string; target: string; path: string; feedback?: boolean }
const signature = (node: WorkflowNode) => `${node.state}:${node.related?.[node.related.length - 1]?.event.id || ''}`

export function Workflow({ workflow, renderItem }: { workflow: WorkflowTurn; renderItem: (item: ViewerTimelineItem) => ReactNode }) {
  const { t } = useTranslation('jev')
  const domId = useId(), board = useRef<HTMLDivElement>(null), disclosure = useRef<HTMLDetailsElement>(null)
  const [selected, setSelected] = useState<string>(), [routes, setRoutes] = useState<Route[]>([])
  const [scope, setScope] = useState<'all' | 'foreground' | 'background'>('all'), [expanded, setExpanded] = useState(true)
  const [view, setView] = useState<'flow' | 'records'>('flow')
  const [frameId, setFrameId] = useState<string>(), [playing, setPlaying] = useState(false)
  const [onScreen, setOnScreen] = useState(true)
  const [reducedMotion, setReducedMotion] = useState(() => matchMedia('(prefers-reduced-motion: reduce)').matches)
  const playbackTouched = useRef(false), autoStarted = useRef(false)
  const [inspecting, setInspecting] = useState(false)
  const [packets, setPackets] = useState<{ id: string; target: string }[]>([])
  const previous = useRef<Map<string, string>>()
  const initialView = useRef(true)
  const visible = useMemo(() => workflow.nodes.filter(node => scope === 'all' || !!node.background === (scope === 'background')), [workflow.nodes, scope])
  const lanes = useMemo(() => [...new Set(visible.map(node => node.lane))].sort((a, b) => {
    const rank = (lane: string) => {
      const node = visible.find(node => node.lane === lane)!
      return (node.sessionId === workflow.sessionId ? 0 : 4) + (node.background ? 3 : node.actor === 'JEV' ? 1 : node.kind === 'tool' ? 2 : 0)
    }
    return rank(a) - rank(b)
  }), [visible, workflow.sessionId])
  const positions = useMemo(() => workflowPositions(visible, lanes), [visible, lanes])
  const summaries = useMemo(() => new Map(workflow.nodes.map(node => [node.id, workflowNodeSummary(node)])), [workflow.nodes])
  const numbers = useMemo(() => new Map(workflow.nodes.map((node, i) => [node.id, i + 1])), [workflow.nodes])
  const liveNodes = workflow.nodes.filter(node => node.state === 'pending')
  const foreground = visible.filter(node => !node.background), pending = visible.filter(node => node.state === 'pending')
  const recordedCurrent = visible.find(node => node.id === selected) || pending.find(node => node.kind === 'guardrail') || pending.find(node => !node.background) || pending[0]
    || foreground[foreground.length - 1] || visible[visible.length - 1]
  const frames = useMemo(() => controlFrames(visible), [visible])
  const replaying = frameId !== undefined
  const currentFrame = [...frames].reverse().find(frame => frame.nodeId === recordedCurrent?.id)
  const cursor = replaying ? Math.max(0, frames.findIndex(frame => frame.id === frameId)) : frames.findIndex(frame => frame.id === currentFrame?.id)
  const frame = frames[cursor]
  const snapshot = useMemo(() => replaying ? controlSnapshot(visible, frames, cursor) : visible, [visible, frames, cursor, replaying])
  const current = replaying ? snapshot.find(node => node.id === frame?.nodeId) : recordedCurrent
  const detailNodes = replaying ? snapshot : workflow.nodes
  const index = visible.findIndex(node => node.id === current?.id)
  const currentLabel = current && (current.literal ? current.label : t(current.label))
  const failed = workflow.nodes.filter(node => node.state === 'failed').length
  const backgroundCount = workflow.nodes.filter(node => node.background).length
  const showDetail = inspecting && current && current.kind !== 'response'
  const selectNode = (id: string, focus = false) => {
    playbackTouched.current = true
    setFrameId(undefined); setPlaying(false)
    setInspecting(true)
    setSelected(id)
    if (focus) board.current?.querySelectorAll<HTMLButtonElement>('[data-workflow-node]').forEach(element => {
      if (element.dataset.workflowNode === id) element.focus({ preventScroll: true })
    })
  }
  const follow = () => { playbackTouched.current = true; setFrameId(undefined); setPlaying(false); setSelected(undefined) }
  useEffect(() => {
    const media = matchMedia('(prefers-reduced-motion: reduce)')
    const change = () => { setReducedMotion(media.matches); if (media.matches) setPlaying(false) }
    media.addEventListener('change', change)
    const observer = new IntersectionObserver(([entry]) => setOnScreen(entry.isIntersecting))
    if (disclosure.current) observer.observe(disclosure.current)
    return () => { media.removeEventListener('change', change); observer.disconnect() }
  }, [])
  useEffect(() => {
    if (playbackTouched.current) return
    // Live updates always use their real event state. A settled turn can replay.
    if (workflow.live || liveNodes.length) {
      autoStarted.current = false
      setFrameId(undefined); setPlaying(false)
      return
    }
    if (!autoStarted.current && !reducedMotion && frames.length > 1) {
      autoStarted.current = true
      setFrameId(frames[0].id); setPlaying(true)
    }
  }, [workflow.live, liveNodes.length, frames, reducedMotion])
  useEffect(() => {
    if (!playing || !expanded || !onScreen || !frames.length) return
    const ended = cursor >= frames.length - 1
    const timer = setTimeout(() => setFrameId(frames[ended ? 0 : cursor + 1].id), ended ? 1600 : 800)
    return () => clearTimeout(timer)
  }, [playing, frames, cursor, expanded, onScreen])
  useEffect(() => { if (workflow.live && disclosure.current) disclosure.current.open = true }, [workflow.live])
  useEffect(() => {
    const select = (event: Event) => {
      const id = (event as CustomEvent<string>).detail
      const node = workflow.nodes.find(node => node.id === id || node.related?.some(record => record.event.id === id))
      if (!node) return
      playbackTouched.current = true
      setScope('all')
      setFrameId(undefined); setPlaying(false)
      setSelected(node.id)
      setInspecting(true)
      if (disclosure.current) { disclosure.current.open = true; disclosure.current.scrollIntoView({ block: 'center', behavior: 'instant' }) }
    }
    window.addEventListener('cyber-workflow-select', select)
    return () => window.removeEventListener('cyber-workflow-select', select)
  }, [workflow.nodes])
  useLayoutEffect(() => {
    if (!expanded) return
    const initial = initialView.current
    initialView.current = false
    if (initial && !pending.length) return
    const viewport = board.current?.parentElement
    const node = board.current && [...board.current.querySelectorAll<HTMLElement>('[data-workflow-node]')].find(element => element.dataset.workflowNode === current?.id)
    if (!viewport || !node) return
    const bounds = viewport.getBoundingClientRect(), rect = node.getBoundingClientRect()
    if (rect.top < bounds.top || rect.bottom > bounds.bottom) viewport.scrollTop += rect.top - bounds.top - 60
    if (rect.left < bounds.left || rect.right > bounds.right) viewport.scrollLeft += rect.left - bounds.left - 18
  }, [current?.id, scope, expanded])
  useEffect(() => {
    const updates = workflow.nodes.filter(node => previous.current && previous.current.get(node.id) !== signature(node))
    previous.current = new Map(workflow.nodes.map(node => [node.id, signature(node)]))
    if (!updates.length) return
    setPackets(updates.map(node => ({ id: `${node.id}:${signature(node)}`, target: node.id })))
  }, [workflow.nodes])
  useEffect(() => {
    if (!packets.length) return
    const timer = setTimeout(() => setPackets([]), 1700)
    return () => clearTimeout(timer)
  }, [packets])
  useLayoutEffect(() => {
    const element = board.current
    if (!element || !expanded) return
    const measure = () => {
      const rect = element.getBoundingClientRect()
      const bounds = new Map([...element.querySelectorAll<HTMLElement>('[data-workflow-node]')].map(node => {
        const b = node.getBoundingClientRect()
        return [node.dataset.workflowNode!, { x: b.left - rect.left, y: b.top - rect.top, w: b.width, h: b.height }] as const
      }))
      const next = workflow.edges.flatMap(edge => {
        const a = bounds.get(edge.source), b = bounds.get(edge.target)
        if (!a || !b) return []
        const sameLane = Math.abs(a.x - b.x) < 1
        const right = b.x > a.x, sx = right ? a.x + a.w : a.x, tx = right ? b.x : b.x + b.w, bend = right ? 24 : -24
        const path = sameLane ? `M ${a.x + a.w / 2} ${a.y + a.h} L ${b.x + b.w / 2} ${b.y}`
          : `M ${sx} ${a.y + a.h / 2} C ${sx + bend} ${a.y + a.h / 2}, ${tx - bend} ${b.y + b.h / 2}, ${tx} ${b.y + b.h / 2}`
        return [{ ...edge, path }]
      })
      setRoutes(next)
    }
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    return () => observer.disconnect()
  }, [workflow.edges, visible, expanded, view, showDetail])
  return <details ref={disclosure} open onToggle={event => setExpanded(event.currentTarget.open)} className="agent-workflow" data-testid="agent-workflow" data-workflow-id={workflow.id}>
    <summary className="workflow-summary">
      {workflow.live ? <Loader2 className="workflow-summary-icon animate-spin" /> : <GitBranch className="workflow-summary-icon" />}
      <span>{t('workflow.title')}</span><span className="workflow-count">{t('workflow.steps', { count: workflow.nodes.length })}</span>
      {!!failed && <span className="workflow-failure">{t('workflow.failures', { count: failed })}</span>}
      <span className="workflow-state">{t(workflow.live ? 'workflow.live' : liveNodes.some(node => node.background) ? 'workflow.backgroundLive' : 'workflow.recorded')}</span><ChevronDown className="workflow-chevron" />
    </summary>
    <div className="workflow-toolbar">
      <div className="workflow-scopes" role="group" aria-label={t('workflow.scope')}>
        {(['all', 'foreground', 'background'] as const).filter(value => value === 'all' || (value === 'background' ? backgroundCount > 0 : workflow.nodes.length > backgroundCount)).map(value => <button key={value} type="button" aria-pressed={scope === value}
          onClick={() => { setScope(value); follow() }}>{t(`workflow.${value}`)}<span>{value === 'all' ? workflow.nodes.length : value === 'background' ? backgroundCount : workflow.nodes.length - backgroundCount}</span></button>)}
      </div>
      <button type="button" className="workflow-render-toggle" data-testid="workflow-render-toggle" title={t(view === 'flow' ? 'control.toRecords' : 'control.toFlow')}
        aria-label={t(view === 'flow' ? 'control.toRecords' : 'control.toFlow')} onClick={() => setView(value => value === 'flow' ? 'records' : 'flow')}>
        <Repeat2 /><span>{t(`control.${view}`)}</span>
      </button>
      {current?.kind !== 'response' && <button type="button" className="workflow-inspect" aria-pressed={inspecting} aria-controls={`${domId}-detail`}
        onClick={() => { if (!inspecting) { playbackTouched.current = true; setPlaying(false) }; setInspecting(value => !value) }}><PanelRight />{t(inspecting ? 'workflow.hideDetail' : 'workflow.inspect')}</button>
      }
      <button type="button" className="workflow-follow" aria-pressed={selected === undefined && !replaying} onClick={follow}><ArrowDownToLine />{t(liveNodes.length ? 'workflow.followLive' : 'workflow.latest')}</button>
    </div>
    <div className="workflow-layout" data-view={view} data-inspecting={!!showDetail}>
      <div className="workflow-visual">
      {view === 'flow' ? <JEVControlFlow nodes={snapshot} current={current} frame={frame} previous={[...frames.slice(0, cursor)].reverse().find(previous => previous.stage !== frame?.stage && previous.stage !== 'background')}
        moving={playing || !replaying && (current?.state === 'pending' || packets.some(packet => packet.target === current?.id))} replaying={replaying} playing={playing}
        onSelect={selectNode} /> :
      <div className="workflow-records">
        <p className="workflow-record-hint"><span>{t('workflow.laneGuide')}</span><span className="workflow-mobile-guide">{t('workflow.mobileGuide')}</span></p>
        <div className="workflow-viewport" role="region" aria-label={t('control.records')} tabIndex={0}>
        <div className="workflow-board" ref={board} data-diagram="swimlane" style={{ gridTemplateColumns: `48px repeat(${lanes.length}, minmax(130px, 1fr))` }}>
          <div className="workflow-time-heading">{t('workflow.timeOrder')}</div>
          <svg className="workflow-wires" aria-hidden="true">
            <defs><marker id={`${domId}-arrow`} viewBox="0 0 6 6" refX="5" refY="3" markerWidth="5" markerHeight="5" orient="auto"><path d="M 0 0 L 6 3 L 0 6" fill="currentColor" /></marker></defs>
            {routes.map(route => {
              const source = workflow.nodes.find(node => node.id === route.source)
              const target = workflow.nodes.find(node => node.id === route.target), active = replaying ? route.target === current?.id : target?.state === 'pending'
              return <g key={route.id} data-workflow-edge={route.id} data-active={active}
                data-actor={source?.background ? 'background' : source?.actor === 'JEV' ? 'JEV' : source?.kind === 'tool' ? 'Executor' : 'LLM'}>
                <path d={route.path} className={`workflow-wire ${active ? 'is-active' : ''} ${route.feedback ? 'is-feedback' : ''}`} markerEnd={`url(#${domId}-arrow)`} />
                {active && (!replaying || playing) && <circle r="3" className="workflow-packet" style={{ offsetPath: `path('${route.path}')` }} />}
                {packets.filter(packet => packet.target === route.target).map(packet => <circle key={packet.id} r="3.5" className="workflow-packet is-arrival" style={{ offsetPath: `path('${route.path}')` }} />)}
              </g>
            })}
          </svg>
          {lanes.map(lane => {
            const nodes = visible.filter(node => node.lane === lane), first = nodes[0]
            const recorded = replaying ? snapshot.filter(node => node.lane === lane).length : nodes.length
            return <div key={lane} className="workflow-lane" data-background={first.background || undefined} data-actor={first.background ? 'background' : first.actor === 'JEV' ? 'JEV' : first.kind === 'tool' ? 'Executor' : 'LLM'} style={{ gridColumn: lanes.indexOf(lane) + 2, gridRow: `1 / span ${visible.length + 1}` }}>
              <div className="workflow-lane-label">{first.background ? <Layers /> : first.actor === 'JEV' ? <CircuitBoard /> : first.kind === 'tool' ? <Wrench /> : first.sessionId === workflow.sessionId ? <Bot /> : <GitBranch />}
                <span>{first.background ? t('background') : first.actor === 'JEV' ? 'JEV' : first.kind === 'tool' ? t('workflow.executor') : first.sessionId === workflow.sessionId ? 'LLM' : first.actor}</span>
                <span className="workflow-lane-count" aria-label={t('workflow.laneProgress', { seen: recorded, total: nodes.length })}>{recorded} / {nodes.length}</span>
                <span className="workflow-lane-progress" aria-hidden="true">{Array.from({ length: 12 }, (_, index) => <i key={index} data-filled={index < Math.ceil(recorded / nodes.length * 12)} />)}</span>
              </div>
            </div>
          })}
          {visible.map(node => {
                const Icon = icons[node.kind as keyof typeof icons] || FileText
                const position = positions.get(node.id)!
                return <div key={node.id} className="workflow-step" style={{ gridColumn: `1 / -1`, gridRow: position.row }}>
                  <div className="workflow-time" style={{ gridColumn: 1 }}><strong>{numbers.get(node.id)}</strong><span>+{Math.max(0, (node.timestamp - workflow.timestamp) / 1000).toFixed(1)}s</span></div>
                  <button type="button" className="workflow-node" data-workflow-node={node.id} data-state={node.state}
                  data-background={node.background || undefined} style={{ gridColumn: position.column, gridRow: 1 }}
                  data-kind={node.kind} data-event-kind={node.record?.value.payload.case} data-event-seq={node.record ? String(node.record.event.seq) : undefined}
                  data-workflow-lane={node.lane}
                  data-record-id={node.record?.event.id} aria-pressed={node.id === current?.id} aria-controls={`${domId}-detail`}
                  data-replay-state={replaying ? snapshot.find(value => value.id === node.id)?.state || 'upcoming' : undefined}
                  onClick={() => selectNode(node.id)} onKeyDown={event => {
                    const offset = event.key === 'ArrowDown' || event.key === 'ArrowRight' ? 1 : event.key === 'ArrowUp' || event.key === 'ArrowLeft' ? -1 : 0
                    const destination = event.key === 'Home' ? visible[0] : event.key === 'End' ? visible[visible.length - 1] : offset ? visible[visible.indexOf(node) + offset] : undefined
                    if (destination) { event.preventDefault(); selectNode(destination.id, true) }
                  }}>
                  <span className="workflow-node-role"><Icon /><span>{node.actor === 'Executor' ? t('workflow.executor') : node.actor}</span><span className="workflow-node-number">{numbers.get(node.id)}</span></span>
                  <span className="workflow-node-title">{node.literal ? node.label : t(node.label)}</span>
                  {node.kind === 'response' ? <span className="workflow-node-description">{t('control.responseBelow')}</span>
                    : summaries.get(node.id) && <span className="workflow-node-description" title={summaries.get(node.id)}>{summaries.get(node.id)}</span>}
                  <span className="workflow-node-state">{node.state === 'pending' ? <Loader2 className="animate-spin" /> : node.state === 'failed' ? <XCircle /> : node.state === 'interrupted' ? <Repeat2 /> : <Check />}{t(`workflow.states.${node.state}`)}</span>
                </button></div>
          })}
        </div>
      </div></div>}
      <ControlPlayback playing={playing} frames={frames} cursor={cursor} onPlay={() => {
        playbackTouched.current = true
        if (playing) { setPlaying(false); return }
        if (!frames.length) return
        setFrameId(frames[replaying && cursor < frames.length - 1 ? cursor : 0].id); setPlaying(true)
      }} onSeek={index => { playbackTouched.current = true; setPlaying(false); setFrameId(frames[index]?.id) }} onLive={follow} />
      </div>
      {showDetail && <section id={`${domId}-detail`} className="workflow-detail" data-testid="workflow-detail" data-selected-node={current.id} aria-label={currentLabel}>
        <header className="workflow-detail-heading"><div className="workflow-detail-title"><span className="workflow-detail-number">{numbers.get(current.id)}</span><strong>{currentLabel}</strong><span>{t(`workflow.states.${current.state}`)}</span></div>
          <div className="workflow-navigation"><button type="button" disabled={index <= 0} aria-label={t('workflow.previous')} onClick={() => selectNode(visible[index - 1].id)}><ChevronLeft /></button>
            <span>{index + 1} / {visible.length}</span><button type="button" disabled={index >= visible.length - 1} aria-label={t('workflow.next')} onClick={() => selectNode(visible[index + 1].id)}><ChevronRight /></button></div></header>
        <div className="workflow-detail-body" key={current.id}>
          {current.record ? <JEVWorkflowDetail node={current} nodes={detailNodes} /> : current.item && renderItem(current.item)}
        </div>
      </section>}
    </div>
  </details>
}
