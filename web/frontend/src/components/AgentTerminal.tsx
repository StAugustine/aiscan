import { useEffect, useRef, useState, type ReactNode } from 'react'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal as XTerm } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { Plus, RefreshCw, Square, Terminal as TerminalIcon } from 'lucide-react'
import { agentTerminalWebSocketURL } from '../api'
import type { AgentInfo, TerminalMessage } from '../api'
import { cn } from '../lib/utils'

interface AgentTerminalProps {
  agent: AgentInfo
}

interface PTYSession {
  id: string
  kind?: string
  name?: string
  command?: string
  pid?: number
  state?: string
}

type TerminalStatus = 'connecting' | 'connected' | 'closed' | 'error'

export default function AgentTerminal({ agent }: AgentTerminalProps) {
  return (
    <div className="flex min-h-0 flex-1 flex-col xl:flex-row">
      <section className="flex min-h-[360px] flex-1 flex-col border-b border-border xl:min-h-0 xl:border-b-0 xl:border-r">
        <TerminalHeader title="Main REPL" />
        <ReplTerminal agent={agent} />
      </section>
      <section className="flex min-h-[420px] w-full flex-col xl:min-h-0 xl:w-[420px]">
        <TaskPTYPanel agent={agent} />
      </section>
    </div>
  )
}

function ReplTerminal({ agent }: { agent: AgentInfo }) {
  const [status, setStatus] = useState<TerminalStatus>('connecting')
  const [sessionID, setSessionID] = useState('')
  const wsRef = useRef<WebSocket | null>(null)
  const termRef = useRef<XTerm | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const mountRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    const mount = mountRef.current
    if (!mount) return

    setStatus('connecting')
    setSessionID('')
    const term = createTerminal()
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(mount)
    fit.fit()
    term.focus()
    termRef.current = term
    fitRef.current = fit

    const ws = new WebSocket(agentTerminalWebSocketURL(agent.id))
    wsRef.current = ws
    const replName = replSessionName()
    const send = (message: Record<string, unknown>) => {
      if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(message))
    }
    const size = () => ({ cols: term.cols, rows: term.rows })

    const dataDisposable = term.onData((data) => {
      send({ type: 'pty.input', payload: { data } })
    })
    const resizeDisposable = term.onResize(({ cols, rows }) => {
      send({ type: 'pty.resize', payload: { cols, rows } })
    })
    const resizeObserver = new ResizeObserver(() => fit.fit())
    resizeObserver.observe(mount)

    ws.onopen = () => {
      setStatus('connected')
      send({ type: 'pty.open', payload: { kind: 'repl', name: replName, singleton: true, ...size() } })
    }
    ws.onmessage = (event) => {
      const msg = parseTerminalMessage(event.data)
      if (!msg) return
      switch (msg.type) {
        case 'pty.opened':
        case 'pty.attached': {
          const id = stringPayload(msg, 'session_id')
          if (id) setSessionID(id)
          setStatus('connected')
          term.focus()
          break
        }
        case 'pty.output':
          writeTerminalData(term, msg)
          break
        case 'pty.closed':
          setStatus('closed')
          term.write('\r\n[session closed]\r\n')
          break
        case 'pty.error':
          setStatus('error')
          term.write(`\r\n[pty error] ${msg.data || 'unknown error'}\r\n`)
          break
      }
    }
    ws.onerror = () => setStatus('error')
    ws.onclose = () => setStatus((current) => (current === 'error' ? current : 'closed'))

    return () => {
      if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'pty.detach' }))
      ws.close()
      resizeObserver.disconnect()
      resizeDisposable.dispose()
      dataDisposable.dispose()
      term.dispose()
      wsRef.current = null
      termRef.current = null
      fitRef.current = null
    }
  }, [agent.id])

  return (
    <>
      <TerminalStatusLine status={status} sessionID={sessionID} />
      <div ref={mountRef} className="min-h-0 flex-1 bg-[#060a0d] p-2" />
    </>
  )
}

function replSessionName() {
  return 'main-repl'
}

function TaskPTYPanel({ agent }: { agent: AgentInfo }) {
  const [status, setStatus] = useState<TerminalStatus>('connecting')
  const [sessions, setSessions] = useState<PTYSession[]>([])
  const [activeID, setActiveID] = useState('')
  const activeRef = useRef('')
  const wsRef = useRef<WebSocket | null>(null)
  const termRef = useRef<XTerm | null>(null)
  const mountRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    activeRef.current = activeID
  }, [activeID])

  useEffect(() => {
    const mount = mountRef.current
    if (!mount) return

    setStatus('connecting')
    setSessions([])
    setActiveID('')
    activeRef.current = ''
    const term = createTerminal()
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(mount)
    fit.fit()
    termRef.current = term

    const ws = new WebSocket(agentTerminalWebSocketURL(agent.id))
    wsRef.current = ws
    const send = (message: Record<string, unknown>) => {
      if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(message))
    }
    const size = () => ({ cols: term.cols, rows: term.rows })

    const dataDisposable = term.onData((data) => {
      if (!activeRef.current) return
      send({ type: 'pty.input', payload: { session_id: activeRef.current, data } })
    })
    const resizeDisposable = term.onResize(({ cols, rows }) => {
      if (!activeRef.current) return
      send({ type: 'pty.resize', payload: { session_id: activeRef.current, cols, rows } })
    })
    const resizeObserver = new ResizeObserver(() => fit.fit())
    resizeObserver.observe(mount)

    ws.onopen = () => {
      setStatus('connected')
      send({ type: 'pty.list' })
    }
    ws.onmessage = (event) => {
      const msg = parseTerminalMessage(event.data)
      if (!msg) return
      switch (msg.type) {
        case 'pty.sessions':
          setSessions(sessionPayload(msg))
          break
        case 'pty.opened':
        case 'pty.attached': {
          const id = stringPayload(msg, 'session_id')
          if (id) {
            activeRef.current = id
            setActiveID(id)
          }
          setStatus('connected')
          send({ type: 'pty.list' })
          term.focus()
          break
        }
        case 'pty.output':
          writeTerminalData(term, msg)
          break
        case 'pty.closed':
          activeRef.current = ''
          setActiveID('')
          setStatus('closed')
          term.write('\r\n[session closed]\r\n')
          send({ type: 'pty.list' })
          break
        case 'pty.detached':
          activeRef.current = ''
          setActiveID('')
          break
        case 'pty.error':
          setStatus('error')
          term.write(`\r\n[pty error] ${msg.data || 'unknown error'}\r\n`)
          break
      }
    }
    ws.onerror = () => setStatus('error')
    ws.onclose = () => setStatus((current) => (current === 'error' ? current : 'closed'))

    return () => {
      if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: 'pty.detach' }))
      ws.close()
      resizeObserver.disconnect()
      resizeDisposable.dispose()
      dataDisposable.dispose()
      term.dispose()
      wsRef.current = null
      termRef.current = null
    }
  }, [agent.id])

  function send(message: Record<string, unknown>) {
    if (wsRef.current?.readyState === WebSocket.OPEN) wsRef.current.send(JSON.stringify(message))
  }

  function terminalSize() {
    const term = termRef.current
    return term ? { cols: term.cols, rows: term.rows } : { cols: 80, rows: 24 }
  }

  function refreshSessions() {
    send({ type: 'pty.list' })
  }

  function attachSession(id: string) {
    activeRef.current = id
    setActiveID(id)
    termRef.current?.reset()
    send({ type: 'pty.attach', payload: { session_id: id, ...terminalSize() } })
  }

  function openShell() {
    activeRef.current = ''
    setActiveID('')
    termRef.current?.reset()
    send({ type: 'pty.open', payload: { kind: 'shell', name: `shell-${agent.name}`, ...terminalSize() } })
  }

  function stopSession() {
    if (!activeRef.current) return
    send({ type: 'pty.kill', payload: { session_id: activeRef.current } })
  }

  const taskSessions = sessions.filter((session) => session.kind !== 'repl')

  return (
    <>
      <TerminalHeader
        title="Task PTYs"
        actions={
          <>
            <IconButton label="New shell PTY" onClick={openShell}>
              <Plus className="h-3.5 w-3.5" />
            </IconButton>
            <IconButton label="Refresh sessions" onClick={refreshSessions}>
              <RefreshCw className="h-3.5 w-3.5" />
            </IconButton>
            <IconButton label="Stop active session" onClick={stopSession} disabled={!activeID}>
              <Square className="h-3.5 w-3.5" />
            </IconButton>
          </>
        }
      />
      <TerminalStatusLine status={status} sessionID={activeID} />
      <div className="max-h-44 shrink-0 overflow-auto border-b border-border p-2">
        {taskSessions.length === 0 ? (
          <div className="px-2 py-4 text-xs text-muted-foreground">No task PTYs</div>
        ) : (
          taskSessions.map((session) => (
            <button
              key={session.id}
              type="button"
              onClick={() => attachSession(session.id)}
              className={cn(
                'mb-1 w-full rounded-md px-2 py-2 text-left text-xs transition-colors',
                session.id === activeID
                  ? 'bg-cyber-400/10 text-foreground'
                  : 'text-muted-foreground hover:bg-accent hover:text-foreground',
              )}
            >
              <span className="block truncate font-medium">{session.name || session.id}</span>
              <span className="mt-0.5 block truncate font-mono">
                {session.kind || 'task'} · {session.state || 'unknown'} · {session.id}
              </span>
              {session.command && <span className="mt-0.5 block truncate font-mono opacity-70">{session.command}</span>}
            </button>
          ))
        )}
      </div>
      <div ref={mountRef} className="min-h-0 flex-1 bg-[#060a0d] p-2" />
    </>
  )
}

function TerminalHeader({ title, actions }: { title: string; actions?: ReactNode }) {
  return (
    <div className="flex h-11 shrink-0 items-center justify-between border-b border-border px-3">
      <div className="flex min-w-0 items-center gap-2">
        <TerminalIcon className="h-4 w-4 shrink-0 text-cyber-400" />
        <span className="truncate text-sm font-medium text-foreground">{title}</span>
      </div>
      {actions && <div className="flex items-center gap-1">{actions}</div>}
    </div>
  )
}

function TerminalStatusLine({ status, sessionID }: { status: TerminalStatus; sessionID: string }) {
  return (
    <div className="h-7 shrink-0 border-b border-border px-3 py-1 font-mono text-[11px] text-muted-foreground">
      {status}{sessionID ? ` · ${sessionID}` : ''}
    </div>
  )
}

function IconButton({
  children,
  disabled,
  label,
  onClick,
}: {
  children: ReactNode
  disabled?: boolean
  label: string
  onClick: () => void
}) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      disabled={disabled}
      onClick={onClick}
      className="inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground disabled:cursor-not-allowed disabled:opacity-40"
    >
      {children}
    </button>
  )
}

function createTerminal() {
  return new XTerm({
    cursorBlink: true,
    fontFamily: 'JetBrains Mono, Consolas, ui-monospace, SFMono-Regular, monospace',
    fontSize: 13,
    lineHeight: 1.25,
    scrollback: 8000,
    convertEol: false,
    allowProposedApi: true,
    theme: {
      background: '#060a0d',
      foreground: '#d7e1e8',
      cursor: '#33d17a',
      selectionBackground: '#1f6feb55',
      black: '#0b1117',
      red: '#ff6b6b',
      green: '#33d17a',
      yellow: '#f4d35e',
      blue: '#4ea1ff',
      magenta: '#c778dd',
      cyan: '#56b6c2',
      white: '#d7e1e8',
      brightBlack: '#5c6773',
      brightRed: '#ff8a8a',
      brightGreen: '#5ee08f',
      brightYellow: '#ffe08a',
      brightBlue: '#79b8ff',
      brightMagenta: '#d8a4ec',
      brightCyan: '#7fd5df',
      brightWhite: '#ffffff',
    },
  })
}

function parseTerminalMessage(value: unknown): TerminalMessage | null {
  if (typeof value !== 'string') return null
  try {
    return JSON.parse(value) as TerminalMessage
  } catch {
    return null
  }
}

function writeTerminalData(term: XTerm, msg: TerminalMessage) {
  if (msg.data) {
    term.write(msg.data)
    return
  }
  if (!msg.data_b64) return
  const binary = atob(msg.data_b64)
  const data = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i += 1) {
    data[i] = binary.charCodeAt(i)
  }
  term.write(data)
}

function stringPayload(msg: TerminalMessage, key: string): string {
  const value = msg.payload?.[key]
  return typeof value === 'string' ? value : ''
}

function sessionPayload(msg: TerminalMessage): PTYSession[] {
  const value = msg.payload?.sessions
  if (!Array.isArray(value)) return []
  return value.filter(isSession)
}

function isSession(value: unknown): value is PTYSession {
  if (!value || typeof value !== 'object') return false
  return typeof (value as PTYSession).id === 'string'
}
