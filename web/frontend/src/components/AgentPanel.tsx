import { useEffect, useState } from 'react'
import { Loader2, Monitor, X, Circle } from 'lucide-react'
import { listAgents } from '../api'
import type { AgentInfo } from '../api'

interface AgentPanelProps {
  open: boolean
  onClose: () => void
}

export default function AgentPanel({ open, onClose }: AgentPanelProps) {
  const [agents, setAgents] = useState<AgentInfo[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!open) return
    setLoading(true)
    setError('')
    listAgents()
      .then(setAgents)
      .catch((err: Error) => setError(err.message || 'Failed to load agents'))
      .finally(() => setLoading(false))
  }, [open])

  useEffect(() => {
    if (!open) return
    const interval = setInterval(() => {
      listAgents().then(setAgents).catch(() => {})
    }, 5000)
    return () => clearInterval(interval)
  }, [open])

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-background/80 px-4 py-8 backdrop-blur-sm">
      <div className="w-full max-w-2xl rounded-lg border border-border bg-card shadow-xl">
        <div className="flex items-center justify-between border-b border-border px-4 py-3">
          <div className="flex items-center gap-2">
            <Monitor className="h-4 w-4 text-cyber-400" />
            <div>
              <div className="text-sm font-medium text-foreground">Connected Agents</div>
              <div className="text-xs text-muted-foreground">
                {agents.length} agent{agents.length !== 1 ? 's' : ''} online
              </div>
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="rounded-md p-1 text-muted-foreground hover:bg-accent hover:text-foreground"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="p-4">
          {loading ? (
            <div className="flex h-32 items-center justify-center text-muted-foreground">
              <Loader2 className="h-5 w-5 animate-spin" />
            </div>
          ) : error ? (
            <div className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {error}
            </div>
          ) : agents.length === 0 ? (
            <div className="flex h-32 flex-col items-center justify-center gap-2 text-muted-foreground">
              <Monitor className="h-8 w-8 opacity-20" />
              <p className="text-sm">No agents connected</p>
              <p className="text-xs">
                Start an agent with <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">aiscan agent --loop --ioa-url http://&lt;web&gt;/ioa</code>
              </p>
            </div>
          ) : (
            <div className="space-y-2">
              {agents.map((agent) => (
                <div
                  key={agent.id}
                  className="flex items-center gap-3 rounded-md border border-border bg-secondary/30 px-3 py-2.5"
                >
                  <Circle
                    className={`h-2.5 w-2.5 shrink-0 fill-current ${
                      agent.busy
                        ? 'text-yellow-400'
                        : 'text-cyber-400'
                    }`}
                  />
                  <div className="min-w-0 flex-1">
                    <div className="flex items-baseline gap-2">
                      <span className="text-sm font-medium text-foreground truncate">
                        {agent.name}
                      </span>
                      <span className="text-xs text-muted-foreground">
                        {agent.busy ? 'busy' : 'idle'}
                      </span>
                    </div>
                    {agent.commands && agent.commands.length > 0 && (
                      <div className="mt-0.5 flex flex-wrap gap-1">
                        {agent.commands.map((cmd) => (
                          <span
                            key={cmd}
                            className="rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground"
                          >
                            {cmd}
                          </span>
                        ))}
                      </div>
                    )}
                  </div>
                  <span className="text-[10px] text-muted-foreground shrink-0">
                    {formatRelativeTime(agent.connected_at)}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function formatRelativeTime(iso: string): string {
  try {
    const diff = Date.now() - new Date(iso).getTime()
    const mins = Math.floor(diff / 60000)
    if (mins < 1) return 'just now'
    if (mins < 60) return `${mins}m ago`
    const hours = Math.floor(mins / 60)
    if (hours < 24) return `${hours}h ago`
    return `${Math.floor(hours / 24)}d ago`
  } catch {
    return ''
  }
}
