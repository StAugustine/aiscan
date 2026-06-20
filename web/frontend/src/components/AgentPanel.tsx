import { useEffect, useState, type ReactNode } from 'react'
import { Activity, Cpu, Database, Fingerprint, Loader2, Monitor, X, Circle, RefreshCw } from 'lucide-react'
import { listAgents } from '../api'
import type { AgentInfo } from '../api'
import AgentTerminal from './AgentTerminal'
import { cn } from '../lib/utils'

interface AgentPanelProps {
  open: boolean
  onClose: () => void
}

export default function AgentPanel({ open, onClose }: AgentPanelProps) {
  const [agents, setAgents] = useState<AgentInfo[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [selectedID, setSelectedID] = useState('')

  useEffect(() => {
    if (!open) return
    setLoading(true)
    setError('')
    listAgents()
      .then((items) => {
        setAgents(items)
        setSelectedID((current) => current || items[0]?.id || '')
      })
      .catch((err: Error) => setError(err.message || 'Failed to load agents'))
      .finally(() => setLoading(false))
  }, [open])

  useEffect(() => {
    if (!open) return
    const interval = setInterval(() => {
      listAgents()
        .then((items) => {
          setAgents(items)
          setSelectedID((current) => items.some((agent) => agent.id === current) ? current : items[0]?.id || '')
        })
        .catch(() => {})
    }, 5000)
    return () => clearInterval(interval)
  }, [open])

  if (!open) return null
  const selected = agents.find((agent) => agent.id === selectedID) || agents[0] || null

  return (
    <div className="fixed inset-0 z-50 flex justify-end bg-background/70 backdrop-blur-sm">
      <div className="flex h-full w-full max-w-7xl flex-col border-l border-border bg-card shadow-xl">
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

        <div className="min-h-0 flex-1">
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
            </div>
          ) : (
            <div className="flex h-full min-h-0 flex-col lg:flex-row">
              <aside className="flex max-h-60 w-full shrink-0 flex-col border-b border-border lg:max-h-none lg:w-80 lg:border-b-0 lg:border-r">
                <div className="flex items-center justify-between border-b border-border px-3 py-2">
                  <span className="text-xs font-medium uppercase text-muted-foreground">Agents</span>
                  <button
                    type="button"
                    onClick={() => listAgents().then(setAgents).catch(() => {})}
                    className="rounded-md p-1 text-muted-foreground hover:bg-accent hover:text-foreground"
                    aria-label="Refresh agents"
                  >
                    <RefreshCw className="h-3.5 w-3.5" />
                  </button>
                </div>
                <div className="min-h-0 flex-1 overflow-auto p-2">
                  {agents.map((agent) => (
                    <button
                      key={agent.id}
                      type="button"
                      onClick={() => setSelectedID(agent.id)}
                      className={cn(
                        'mb-1 flex w-full items-start gap-3 rounded-md px-3 py-2.5 text-left transition-colors',
                        selected?.id === agent.id
                          ? 'bg-cyber-400/10 text-foreground'
                          : 'text-muted-foreground hover:bg-accent hover:text-foreground',
                      )}
                    >
                      <Circle
                        className={cn(
                          'mt-1 h-2.5 w-2.5 shrink-0 fill-current',
                          agent.busy ? 'text-yellow-400' : 'text-cyber-400',
                        )}
                      />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-sm font-medium">{agent.name}</span>
                        <span className="mt-0.5 block truncate text-xs">
                          {agent.busy ? 'busy' : 'idle'} · {formatRelativeTime(agent.connected_at)}
                        </span>
                      </span>
                    </button>
                  ))}
                </div>
              </aside>

              <section className="flex min-h-0 flex-1 flex-col">
                {selected && (
                  <>
                    <div className="border-b border-border px-4 py-3">
                      <div className="flex flex-wrap items-start justify-between gap-3">
                        <div className="min-w-0">
                          <div className="truncate text-sm font-medium text-foreground">{selected.name}</div>
                          <div className="mt-0.5 truncate font-mono text-xs text-muted-foreground">{selected.id}</div>
                        </div>
                        <div className="flex items-center gap-2 text-xs text-muted-foreground">
                          <Circle
                            className={cn(
                              'h-2.5 w-2.5 fill-current',
                              selected.busy ? 'text-yellow-400' : 'text-cyber-400',
                            )}
                          />
                          <span>{selected.busy ? 'busy' : 'idle'}</span>
                          <span>{formatRelativeTime(selected.connected_at)}</span>
                        </div>
                      </div>
                      {selected.commands && selected.commands.length > 0 && (
                        <div className="mt-2 flex flex-wrap gap-1">
                          {selected.commands.map((cmd) => (
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
                    <AgentOverview agent={selected} />
                    <AgentTerminal agent={selected} />
                  </>
                )}
              </section>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function AgentOverview({ agent }: { agent: AgentInfo }) {
  const identity = agent.identity || {}
  const stats = agent.stats || {}
  const runtime = [identity.os, identity.arch].filter(Boolean).join('/')
  const ioaLabel = identity.node_id ? identity.node_id : identity.ioa_url ? 'configured' : 'disabled'

  return (
    <div className="border-b border-border px-4 py-3">
      <div className="grid gap-3 text-xs md:grid-cols-4">
        <InfoMetric icon={<Fingerprint className="h-3.5 w-3.5" />} label="IOA" value={ioaLabel} sub={identity.space ? `space ${identity.space}` : identity.node_name} />
        <InfoMetric icon={<Cpu className="h-3.5 w-3.5" />} label="Runtime" value={runtime || 'unknown'} sub={identity.hostname || identity.username} />
        <InfoMetric icon={<Activity className="h-3.5 w-3.5" />} label="Tokens" value={formatNumber(stats.total_tokens)} sub={`${formatNumber(stats.turns)} turns · ${formatNumber(stats.tool_calls)} tools`} />
        <InfoMetric icon={<Database className="h-3.5 w-3.5" />} label="Findings" value={`${formatNumber(stats.assets)} assets`} sub={`${formatNumber(stats.loots)} loots`} />
      </div>
      {(identity.provider || identity.model || identity.working_dir) && (
        <div className="mt-2 grid gap-2 text-[11px] text-muted-foreground md:grid-cols-2">
          <div className="min-w-0 truncate font-mono">{[identity.provider, identity.model].filter(Boolean).join(' · ') || 'provider not configured'}</div>
          <div className="min-w-0 truncate font-mono md:text-right">{identity.working_dir || ''}</div>
        </div>
      )}
    </div>
  )
}

function InfoMetric({ icon, label, value, sub }: { icon: ReactNode; label: string; value: string; sub?: string }) {
  return (
    <div className="min-w-0">
      <div className="flex items-center gap-1.5 text-[10px] uppercase text-muted-foreground">
        {icon}
        <span>{label}</span>
      </div>
      <div className="mt-1 truncate font-mono text-sm text-foreground">{value || '0'}</div>
      {sub && <div className="mt-0.5 truncate text-[11px] text-muted-foreground">{sub}</div>}
    </div>
  )
}

function formatNumber(value: number | undefined) {
  return typeof value === 'number' && Number.isFinite(value) ? value.toLocaleString() : '0'
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
