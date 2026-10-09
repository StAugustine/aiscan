import { useTranslation } from 'react-i18next'
import type { Event as AOPEvent, TokenUsage } from '../../../cyber-ui/packages/aop/src/gen/aop/event_pb'
import { jevEvent, runtimeEvents } from '../../lib/jev-view'

type Bucket = { input: bigint; output: bigint; missing: number; requests: number }
export function JEVUsage({ events }: { events: readonly AOPEvent[] }) {
  const { t } = useTranslation('jev')
  const build: Bucket = { input: 0n, output: 0n, missing: 0, requests: 0 }
  const runtime: Bucket = { input: 0n, output: 0n, missing: 0, requests: 0 }
  function add(bucket: Bucket, usage?: TokenUsage) {
    bucket.requests++
    if (!usage) { bucket.missing++; return }
    bucket.input += usage.inputTokens; bucket.output += usage.outputTokens
    bucket.missing += Number(usage.detail.usage_missing || 0n)
  }
  for (const event of runtimeEvents(events)) {
    const value = jevEvent(event), payload = value?.payload
    if (payload?.case === 'decisionResult') add(value?.background ? build : runtime, payload.value.usage)
    if (payload?.case === 'generation' && payload.value.state === 'finished') {
      // reflex_llm includes all compiler rounds. Count the aggregate once.
      if (['claim_llm', 'reflex_llm'].includes(payload.value.kind)) add(build, payload.value.usage)
      if (payload.value.kind === 'parameters_llm') add(runtime, payload.value.usage)
    }
    if (event.payload.case === 'usage') add(runtime, event.payload.value)
  }
  if (!build.requests && !runtime.requests) return null
  return <div className="space-y-1 border-b border-border/60 px-4 py-2 text-[11px] text-muted-foreground" data-testid="jev-usage-separation">
    {([['buildUsage', build], ['runtimeUsage', runtime]] as const).map(([label, bucket]) => <p key={label}>{t(label)} · {t('tokenUsage', { input: bucket.input.toString(), output: bucket.output.toString() })}{bucket.missing > 0 && ` · ${t('usageMissing', { count: bucket.missing })}`}</p>)}
    <p>{t('costUnknown')}</p>
  </div>
}
