import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { anyUnpack, timestampDate } from '@bufbuild/protobuf/wkt'
import { aopClient, pendingGuardrailReviews, resolveGuardrailReview, type AOPEvent } from '../api'
import { ReviewSchema, ReviewState, type Review } from '../cyber-proto'

interface ReviewEntry {
  reviews: Review[]
  unavailable: boolean
  version: number
  pending?: { version: number; promise: Promise<void> }
}

// One runtime cache serves rendering and authorization. Historical events only
// invalidate it; they never restore an approval button themselves.
export function useGuardrailReviews(sessionIds: string[], activeSessionId: string | null, events: AOPEvent[]) {
  const entries = useRef(new Map<string, ReviewEntry>())
  const [revision, setRevision] = useState(0)
  const redraw = useCallback(() => setRevision(value => value + 1), [])
  const schedule = useRef<(ids: string[], invalidate?: boolean) => void>(() => {})
  const reload = useRef<(id: string) => Promise<void>>(async () => {})
  const sessionKey = JSON.stringify([...new Set(sessionIds)].sort())
  useEffect(() => {
    const ids: string[] = JSON.parse(sessionKey)
    const allowed = new Set(ids)
    let disposed = false
    for (const id of entries.current.keys()) if (!allowed.has(id)) entries.current.delete(id)
    for (const id of ids) {
      const entry = entries.current.get(id)
      if (entry) entry.version++
      else entries.current.set(id, { reviews: [], unavailable: true, version: 0 })
    }
    redraw()
    const load = (id: string): Promise<void> => {
      const entry = entries.current.get(id)
      if (disposed || !allowed.has(id) || !entry) return Promise.resolve()
      const pending = entry.pending
      if (pending) return pending.promise.then(() => {
        if (entry.version !== pending.version && aopClient.connected) return load(id)
      })
      const version = entry.version
      const promise = pendingGuardrailReviews(id).then(reviews => {
        if (disposed || version !== entry.version) return
        entry.reviews = reviews.filter(review => review.state === ReviewState.PENDING && !!review.operation?.operationId
          && (!review.expiresAt || timestampDate(review.expiresAt).getTime() > Date.now()))
        entry.unavailable = false
        redraw()
      }).catch(() => {
        if (!disposed && version === entry.version) { entry.unavailable = true; redraw() }
      }).finally(() => { if (entry.pending?.promise === promise) entry.pending = undefined })
      entry.pending = { version, promise }
      return promise
    }
    reload.current = load
    const queued = new Set<string>()
    let draining = false
    const drain = async () => {
      if (disposed || draining || document.hidden) return
      draining = true
      await Promise.all(Array.from({ length: Math.min(ids.length, 4) }, async () => {
        while (!disposed && !document.hidden && queued.size) {
          const id = queued.values().next().value!
          queued.delete(id)
          await load(id)
        }
      }))
      draining = false
    }
    const enqueue = (requested: string[], invalidate = false) => {
      if (disposed) return
      for (const id of requested) {
        const entry = entries.current.get(id)
        if (!allowed.has(id) || !entry) continue
        if (invalidate) entry.version++
        if (entry.pending?.version === entry.version) continue
        queued.add(id)
      }
      queueMicrotask(() => { void drain() })
    }
    schedule.current = enqueue
    const connection = (connected: boolean) => {
      if (connected) { enqueue(ids); return }
      for (const entry of entries.current.values()) { entry.version++; entry.unavailable = true }
      redraw()
    }
    const unsubscribe = aopClient.onConnectionChange(connection)
    if (!aopClient.connected) connection(false)
    enqueue(ids)
    // Compensation covers inactive sessions, missed notifications and expiry.
    const timer = window.setInterval(() => enqueue(ids), 5000)
    const visible = () => { if (!document.hidden) enqueue(ids) }
    document.addEventListener('visibilitychange', visible)
    return () => { disposed = true; unsubscribe(); window.clearInterval(timer); document.removeEventListener('visibilitychange', visible) }
  }, [sessionKey, redraw])

  const latestReview = useMemo(() => {
    for (let index = events.length - 1; index >= 0; index--) {
      const event = events[index]
      try { if (event.payload.case === 'extension' && anyUnpack(event.payload.value, ReviewSchema)) return event }
      catch { /* Malformed extensions cannot enable approval actions. */ }
    }
  }, [events])
  useEffect(() => {
    if (activeSessionId) schedule.current([activeSessionId], !!latestReview)
  }, [activeSessionId, sessionKey, latestReview])

  const resolve = useCallback(async (sessionId: string, review: Review, approve: boolean) => {
    const operationId = review.operation?.operationId
    if (!operationId) throw new Error('Missing review operation')
    const entry = entries.current.get(sessionId)
    if (!aopClient.connected || !entry || entry.unavailable) throw new Error('Approval state is unavailable; reconnect before resolving.')
    try { await resolveGuardrailReview(sessionId, operationId, approve) }
    catch (error) { await reload.current(sessionId); throw error }
    // A query issued before authorization cannot restore the old button.
    entry.version++
    entry.reviews = entry.reviews.filter(item => item.operation?.operationId !== operationId)
    redraw()
    await reload.current(sessionId)
  }, [redraw])
  const { bySession, unavailable } = useMemo(() => ({
    bySession: Object.fromEntries([...entries.current].map(([id, entry]) => [id, entry.reviews])),
    unavailable: Object.fromEntries([...entries.current].map(([id, entry]) => [id, entry.unavailable])),
  }), [revision, sessionKey])
  return { bySession, unavailable, resolve }
}
