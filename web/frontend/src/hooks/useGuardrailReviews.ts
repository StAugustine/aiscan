import { useCallback, useEffect, useRef, useState } from 'react'
import { anyUnpack, timestampDate } from '@bufbuild/protobuf/wkt'
import { aopClient, pendingGuardrailReviews, resolveGuardrailReview, subscribeAOPEvents } from '../api'
import { ReviewSchema, ReviewState, type Review } from '../cyber-proto'

// Transcript and sidebar share current runtime state, never replayed approvals.
export function useGuardrailReviews(sessionIds: string[], activeSessionId: string | null) {
  const [bySession, setBySession] = useState<Record<string, Review[]>>({})
  const [unavailable, setUnavailable] = useState<Record<string, boolean>>({})
  const disconnected = useRef(new Set<string>())
  const reload = useRef<(id: string) => Promise<void>>(async () => {})
  const revisions = useRef(new Map<string, number>())
  const sessionKey = JSON.stringify([...new Set(sessionIds)].sort())
  useEffect(() => {
    const ids: string[] = JSON.parse(sessionKey)
    const allowed = new Set(ids)
    const inflight = new Map<string, Promise<void>>()
    let disposed = false
    setBySession(current => Object.fromEntries(Object.entries(current).filter(([id]) => allowed.has(id))))
    const load = (id: string): Promise<void> => {
      if (disposed || !allowed.has(id)) return Promise.resolve()
      const pending = inflight.get(id)
      if (pending) return pending
      const version = revisions.current.get(id) || 0
      const request = pendingGuardrailReviews(id).then(reviews => {
        if (disposed || version !== (revisions.current.get(id) || 0)) return
        const current = reviews.filter(review => review.state === ReviewState.PENDING && !!review.operation?.operationId
          && (!review.expiresAt || timestampDate(review.expiresAt).getTime() > Date.now()))
        disconnected.current.delete(id)
        setUnavailable(previous => ({ ...previous, [id]: false }))
        setBySession(previous => ({ ...previous, [id]: current }))
      }).catch(() => {
        if (!disposed && version === (revisions.current.get(id) || 0)) {
          disconnected.current.add(id)
          setUnavailable(previous => ({ ...previous, [id]: true }))
        }
      }).finally(() => { inflight.delete(id) })
      inflight.set(id, request)
      return request
    }
    reload.current = load
    let scanning = false
    const scan = async () => {
      if (scanning || disposed || document.hidden) return
      scanning = true
      let cursor = 0
      await Promise.all(Array.from({ length: Math.min(ids.length, 4) }, async () => {
        while (!disposed && cursor < ids.length) await load(ids[cursor++])
      }))
      scanning = false
    }
    const connection = (connected: boolean) => {
      if (connected) { void scan(); return }
      for (const id of ids) {
        revisions.current.set(id, (revisions.current.get(id) || 0) + 1)
        disconnected.current.add(id)
      }
      setUnavailable(previous => ({ ...previous, ...Object.fromEntries(ids.map(id => [id, true])) }))
    }
    const unsubscribeConnection = aopClient.onConnectionChange(connection)
    if (!aopClient.connected) connection(false)
    void scan()
    const timer = window.setInterval(() => { void scan() }, 5000)
    const visible = () => { if (!document.hidden) void scan() }
    document.addEventListener('visibilitychange', visible)
    return () => { disposed = true; unsubscribeConnection(); window.clearInterval(timer); document.removeEventListener('visibilitychange', visible) }
  }, [sessionKey])

  useEffect(() => {
    if (!activeSessionId) return
    const load = () => { void reload.current(activeSessionId) }
    load()
    const timer = window.setInterval(() => { if (!document.hidden) load() }, 2000)
    const unsubscribe = subscribeAOPEvents(activeSessionId, event => {
      if (event.payload.case === 'extension' && anyUnpack(event.payload.value, ReviewSchema)) load()
    }, load)
    return () => { window.clearInterval(timer); unsubscribe() }
  }, [activeSessionId, sessionKey])

  const resolve = useCallback(async (sessionId: string, review: Review, approve: boolean) => {
    const operationId = review.operation?.operationId
    if (!operationId) throw new Error('Missing review operation')
    if (!aopClient.connected || disconnected.current.has(sessionId)) throw new Error('Approval state is unavailable; reconnect before resolving.')
    try { await resolveGuardrailReview(sessionId, operationId, approve) }
    catch (error) { await reload.current(sessionId); throw error }
    // A query started before authorization cannot restore the old button.
    revisions.current.set(sessionId, (revisions.current.get(sessionId) || 0) + 1)
    setBySession(previous => ({ ...previous, [sessionId]: (previous[sessionId] || []).filter(item => item.operation?.operationId !== operationId) }))
    await reload.current(sessionId)
  }, [])
  return { bySession, unavailable, resolve }
}
