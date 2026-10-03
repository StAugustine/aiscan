import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { timestampDate } from '@bufbuild/protobuf/wkt'
import { ShieldAlert, ShieldCheck, ShieldX, Terminal, Check, X, Clock3, Loader2, ChevronRight } from 'lucide-react'
import { Button } from '@cyber/ui'
import { cn } from '@cyber/theme'
import { CodeBlock } from '@/markdown'
import { ReviewState, type Review } from '../cyber-proto'
import { guardrailArguments } from '../lib/guardrail-view'

export function GuardrailReviewCard({ review, actionable, reviewedAt, onResolve, embedded = false, unavailable = false, intercepted = false, outcome }: {
  review: Review
  actionable: boolean
  unavailable?: boolean
  intercepted?: boolean
  outcome?: string
  reviewedAt?: number
  embedded?: boolean
  onResolve: (review: Review, approve: boolean) => Promise<void>
}) {
  const { t, i18n } = useTranslation('app')
  const [error, setError] = useState('')
  const [resolving, setResolving] = useState<'approve' | 'reject' | null>(null)
  const submitting = useRef(false)
  const expiresAt = review.expiresAt ? timestampDate(review.expiresAt).getTime() : undefined
  const [now, setNow] = useState(Date.now)
  useEffect(() => {
    setNow(Date.now())
    if (!expiresAt || review.state !== ReviewState.PENDING) return
    const timer = window.setTimeout(() => setNow(Date.now()), Math.max(0, Math.min(expiresAt - Date.now(), 2147483647)))
    return () => window.clearTimeout(timer)
  }, [expiresAt, review.state])
  const expired = expiresAt !== undefined && expiresAt <= now
  const state = intercepted ? 'intercepted' : review.state === ReviewState.APPROVED ? 'approved'
    : review.state === ReviewState.REJECTED ? 'rejected'
    : review.state === ReviewState.EXPIRED || (review.state === ReviewState.PENDING && expired) ? 'expired'
    : review.state === ReviewState.CANCELED ? 'canceled'
    : (actionable || unavailable) && review.state === ReviewState.PENDING ? 'pending' : 'history'
  const pending = state === 'pending'
  const { command, fields, raw } = guardrailArguments(review)
  const reason = review.decision?.reason || ''
  const automatic = review.resolutionSource === 'auto'
  const policies = reason.split('\nConsequence assessment: ').map((text, index) => {
    const separator = text.startsWith('JEV ') ? text.indexOf(': ') : -1
    return { label: t(index === 0 ? 'guardrailRiskScreen' : 'guardrailConsequence'),
      details: separator >= 0 ? text.slice(0, separator) : '', reason: separator >= 0 ? text.slice(separator + 2) : text }
  })
  const Icon = pending ? ShieldAlert : state === 'approved' ? ShieldCheck : state === 'rejected' ? ShieldX : Clock3
  const accent = pending ? 'text-warning' : state === 'approved' ? 'text-emerald-700 dark:text-emerald-400'
    : state === 'rejected' ? 'text-destructive' : 'text-muted-foreground'
  const title = t(pending ? 'guardrailTitle' : automatic ? 'guardrailAutoState_' + state : 'guardrailState_' + state)
  const resolve = async (approve: boolean) => {
    if (submitting.current || unavailable || !pending || (expiresAt !== undefined && expiresAt <= Date.now())) return
    submitting.current = true
    setResolving(approve ? 'approve' : 'reject')
    setError('')
    try { await onResolve(review, approve) }
    catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) }
    finally { submitting.current = false; setResolving(null) }
  }
  const resolvedTime = reviewedAt !== undefined && <time data-guardrail-resolved-at dateTime={new Date(reviewedAt).toISOString()}
        title={new Date(reviewedAt).toLocaleString(i18n.language)} className="text-[11px] tabular-nums text-muted-foreground">
        {new Date(reviewedAt).toLocaleTimeString(i18n.language, { hour: '2-digit', minute: '2-digit', second: '2-digit' })}
      </time>
  const body = <div className="space-y-3 px-4 pb-4 pt-3">
      {(pending || state === 'history') && <p className="text-xs leading-relaxed text-muted-foreground">{t(pending ? 'guardrailReviewHint' : 'guardrailHint_history')}</p>}
      {!pending && state !== 'history' && <p className="text-xs leading-relaxed text-muted-foreground">{t((automatic ? 'guardrailAutoHint_' : 'guardrailHint_') + state)}</p>}
      {unavailable && <p role="status" className="text-sm text-warning">{t('guardrailDisconnected')}</p>}
      {pending && <div className="flex min-w-0 items-center gap-2 text-xs text-muted-foreground"><Terminal className="h-3.5 w-3.5 shrink-0" aria-hidden="true" /><span className="break-all font-mono">{review.call?.name}</span></div>}
      {review.resolutionSource && <p data-guardrail-resolution-source={review.resolutionSource} className="text-xs text-muted-foreground">{t('guardrailSource')}: {t('guardrailSource_' + review.resolutionSource)}</p>}
      {command !== null && <div data-guardrail-command className="min-w-0"><CodeBlock code={command} language="bash" copyable /></div>}
      {fields.length > 0 && <dl className="grid min-w-0 grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-2 text-xs">
        {fields.map(([key, value]) => <div key={key} className="contents"><dt className="text-muted-foreground">{key}</dt><dd className="min-w-0 whitespace-pre-wrap break-all font-mono">{value}</dd></div>)}
      </dl>}
      {review.call?.workingDirectory && <p className="break-all text-xs text-muted-foreground">{t('guardrailDirectory')}: <code>{review.call.workingDirectory}</code></p>}
      {review.decision?.reason && policies.map((policy, index) => <p key={index} data-guardrail-reason className={cn('whitespace-pre-wrap break-words border-l-2 pl-3 text-sm leading-relaxed', pending ? 'border-warning/40' : 'border-border text-muted-foreground')}><span className="mr-1 font-medium">{policy.label}:</span>{policy.reason}</p>)}
      <details className="group text-xs text-muted-foreground">
        <summary className="w-fit cursor-pointer select-none rounded-sm transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">{t(pending ? 'guardrailDetails' : 'guardrailRecordDetails')}</summary>
        <div className="mt-3 space-y-2 border-t border-border pt-3">
          {policies.filter(policy => policy.details).map((policy, index) => <p key={index}>{policy.label} · {t('guardrailPolicyDetails')}: {policy.details}</p>)}
          <p className="break-all">{t('guardrailSession')}: {review.sessionId}</p>
          <p className="break-all">{t('guardrailOperation')}: {review.operation?.operationId}</p>
          <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-all rounded bg-secondary p-2">{raw}</pre>
        </div>
      </details>
      {pending && error && <p role="alert" className="text-sm text-destructive">{error}</p>}
    </div>
  if (embedded && !pending) return <section aria-label={title}
    data-guardrail-review={review.operation?.operationId} data-guardrail-state={state} tabIndex={-1}
    className="min-w-0">
    <details className="group/review overflow-hidden rounded-lg">
      <summary className="flex cursor-pointer list-none flex-wrap items-center gap-x-2 gap-y-1 rounded-lg px-2 py-2 text-xs transition-colors hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring [&::-webkit-details-marker]:hidden">
        <ChevronRight className="h-3 w-3 shrink-0 text-muted-foreground transition-transform group-open/review:rotate-90" aria-hidden="true" />
        <Icon className={cn('h-3.5 w-3.5 shrink-0', accent)} aria-hidden="true" />
        <span className="shrink-0 text-muted-foreground">{t('guardrailRecordLabel')}</span>
        <span aria-live="polite" aria-atomic="true" className={cn('shrink-0 font-medium', accent)}>{title}</span>
        {outcome && <span data-guardrail-outcome={outcome} className={cn('shrink-0', outcome === 'succeeded' ? 'text-emerald-700 dark:text-emerald-400' : 'text-muted-foreground')}>· {t('guardrailOutcome_' + outcome)}</span>}
        <code data-guardrail-command-preview title={command ?? review.call?.name} className="min-w-16 flex-1 truncate text-muted-foreground">{command ?? review.call?.name}</code>
        {resolvedTime}
      </summary>
      {body}
    </details>
  </section>
  return <section aria-label={title} aria-busy={resolving !== null}
    data-guardrail-review={review.operation?.operationId} data-guardrail-state={state} tabIndex={-1}
    className={cn('min-w-0 overflow-hidden rounded-xl border bg-card',
      !embedded && 'shadow-sm', pending ? 'border-warning/40' : 'border-border')}>
    <div className={cn('flex flex-wrap items-center gap-x-2 gap-y-1 px-4 py-3', pending ? 'bg-warning/10' : 'bg-muted/30')}>
      <Icon className={cn('h-4 w-4 shrink-0', accent)} aria-hidden="true" />
      <h2 aria-live="polite" aria-atomic="true" className={cn('min-w-0 flex-1 text-sm font-medium', accent)}>{title}</h2>
      {!pending && <span className="max-w-32 truncate font-mono text-[11px] text-muted-foreground">{review.call?.name}</span>}
      {resolvedTime}
    </div>
    {body}
    {pending && <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border px-4 py-3">
      <p className="flex items-center gap-1.5 text-xs text-muted-foreground"><Clock3 className="h-3 w-3" aria-hidden="true" />{expiresAt && t('guardrailExpires', { time: new Date(expiresAt).toLocaleTimeString(i18n.language) })}</p>
      <div className="flex w-full gap-2 sm:w-auto">
        <Button size="sm" variant="outline" className="flex-1 sm:flex-none" disabled={resolving !== null || unavailable} onClick={() => { void resolve(false) }}>
          {resolving === 'reject' ? <Loader2 className="mr-1 h-3.5 w-3.5 animate-spin" aria-hidden="true" /> : <X className="mr-1 h-3.5 w-3.5" aria-hidden="true" />}{t('guardrailReject')}
        </Button>
        <Button size="sm" className="flex-1 sm:flex-none" disabled={resolving !== null || unavailable} onClick={() => { void resolve(true) }}>
          {resolving === 'approve' ? <Loader2 className="mr-1 h-3.5 w-3.5 animate-spin" aria-hidden="true" /> : <Check className="mr-1 h-3.5 w-3.5" aria-hidden="true" />}{t('guardrailApprove')}
        </Button>
      </div>
    </div>}
  </section>
}
