import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { anyUnpack, timestampDate } from '@bufbuild/protobuf/wkt'
import { Button } from '@cyber/ui'
import { pendingGuardrailReviews, resolveGuardrailReview, subscribeAOPEvents } from '../api'
import { ReviewSchema, ReviewState, type Review } from '../cyber-proto'

// Events trigger a query; historical events never manufacture runnable approvals.
export function GuardrailReviews({ sessionId }: { sessionId: string | null }) {
  const { t } = useTranslation('app')
  const [reviews, setReviews] = useState<Review[]>([])
  const [error, setError] = useState('')
  const [resolving, setResolving] = useState<string | null>(null)
  const [refresh, setRefresh] = useState(0)
  useEffect(() => {
    if (!sessionId) return
    let disposed = false
    let inflight = false
    const load = async () => {
      if (inflight || disposed) return
      inflight = true
      try {
        const pending = await pendingGuardrailReviews(sessionId)
        if (!disposed) {
          setReviews(pending.filter(review => review.state === ReviewState.PENDING && !!review.operation?.operationId))
        }
      } catch {
        // Preserve visible reviews but prevent stale approvals while disconnected.
        if (!disposed) setReviews([])
      } finally { inflight = false }
    }
    void load()
    const timer = window.setInterval(() => { void load() }, 2000)
    const unsubscribe = subscribeAOPEvents(sessionId, event => {
      if (event.payload.case === 'extension' && anyUnpack(event.payload.value, ReviewSchema)) void load()
    }, () => { void load() })
    return () => { disposed = true; window.clearInterval(timer); unsubscribe() }
  }, [sessionId, refresh])

  const resolve = async (review: Review, approve: boolean) => {
    const id = review.operation?.operationId
    if (!id || !sessionId) return
    setResolving(id)
    setError('')
    try {
      await resolveGuardrailReview(sessionId, id, approve)
      setReviews(current => current.filter(item => item.operation?.operationId !== id))
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) }
    finally { setResolving(null); setRefresh(value => value + 1) }
  }
  if (reviews.length === 0 && !error) return null
  return <section aria-label={t('guardrailTitle')} className="fixed bottom-24 right-5 z-40 max-h-[60vh] w-[min(28rem,calc(100vw-2.5rem))] overflow-auto rounded-lg border border-border bg-background p-4 shadow-lg">
    <h2 className="mb-2 font-semibold">{t('guardrailTitle')}</h2>
    <p className="mb-2 text-xs text-muted-foreground">{t('guardrailReviewHint')}</p>
    {error && <p role="alert" className="mb-2 text-sm text-destructive">{error}</p>}
    {reviews.map(review => {
      const id = review.operation!.operationId
      const expired = !!review.expiresAt && timestampDate(review.expiresAt).getTime() <= Date.now()
      return <article key={id} className="mb-3 space-y-2 border-t border-border pt-2">
        <p className="text-sm font-semibold">{review.call?.name}</p>
        <p className="break-all text-xs text-muted-foreground">{t('guardrailContext', { session: review.sessionId, directory: review.call?.workingDirectory || '—' })}</p>
        <p className="text-sm">{review.decision?.reason}</p>
        <p className="break-all font-mono text-xs text-muted-foreground">{id}</p>
        <pre className="max-h-40 overflow-auto whitespace-pre-wrap break-all rounded bg-secondary p-2 text-xs">{new TextDecoder().decode(review.call?.arguments?.data)}</pre>
        {review.expiresAt && <p className="text-xs text-muted-foreground">{t('guardrailExpires', { time: timestampDate(review.expiresAt).toLocaleTimeString() })}</p>}
        <div className="flex gap-2">
          <Button size="sm" disabled={expired || resolving !== null} onClick={() => { void resolve(review, false) }}>{t('guardrailReject')}</Button>
          <Button size="sm" variant="outline" disabled={expired || resolving !== null} onClick={() => { void resolve(review, true) }}>{t('guardrailApprove')}</Button>
        </div>
      </article>
    })}
  </section>
}
