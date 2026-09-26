import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Bot, ShieldCheck, ShieldOff, UserRound } from 'lucide-react'
import { Button, Tooltip, TooltipContent, TooltipTrigger } from '@cyber/ui'
import { cn } from '@cyber/theme'
import { CONFIG_CHANGED_EVENT, getConfigStatus, setGuardrailEnabled, setGuardrailMode } from '../api'

export function GuardrailToggle({ disabled = false }: { disabled?: boolean }) {
  const { t } = useTranslation('app')
  const [enabled, setEnabled] = useState<boolean | null>(null)
  const [mode, setMode] = useState<'safe' | 'auto'>('safe')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  useEffect(() => {
    let disposed = false
    const refresh = () => {
      void getConfigStatus().then(config => {
        if (!disposed) {
          setEnabled(config.extensions.jev?.values?.enabled === true)
          setMode(config.extensions.guardrail?.values?.mode === 'auto' ? 'auto' : 'safe')
        }
      }).catch(cause => { if (!disposed) setError(cause instanceof Error ? cause.message : String(cause)) })
    }
    refresh()
    window.addEventListener(CONFIG_CHANGED_EVENT, refresh)
    return () => { disposed = true; window.removeEventListener(CONFIG_CHANGED_EVENT, refresh) }
  }, [])
  const update = async (nextMode?: 'safe' | 'auto') => {
    if (enabled === null || saving) return
    setSaving(true)
    setError('')
    try {
      const config = await (nextMode ? setGuardrailMode(nextMode) : setGuardrailEnabled(!enabled))
      setEnabled(config.extensions.jev?.values?.enabled === true)
      setMode(config.extensions.guardrail?.values?.mode === 'auto' ? 'auto' : 'safe')
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) }
    finally { setSaving(false) }
  }
  const label = t(saving ? 'guardrailSaving' : enabled ? 'guardrailOn' : 'guardrailOff')
  const Icon = enabled ? ShieldCheck : ShieldOff
  const ModeIcon = mode === 'safe' ? UserRound : Bot
  return <div className="relative flex shrink-0 items-center gap-1">
    <Tooltip>
      <TooltipTrigger asChild>
        <Button type="button" role="switch" aria-label={t('guardrailSwitch')} aria-checked={enabled === true} aria-busy={saving}
          variant="ghost" size="xs" disabled={disabled || saving || enabled === null} onClick={() => { void update() }}
          className={cn('h-7 gap-1.5 rounded-md border px-2', enabled ? 'border-emerald-600/40 text-emerald-700 dark:text-emerald-400' : 'border-border text-muted-foreground')}>
          <Icon className="h-3.5 w-3.5" /><span className="hidden text-xs sm:inline">{label}</span>
        </Button>
      </TooltipTrigger>
      <TooltipContent>{t('guardrailSwitchHint')}</TooltipContent>
    </Tooltip>
    {enabled && <Tooltip>
      <TooltipTrigger asChild>
        <Button type="button" aria-label={t('guardrailModeSwitch', { mode: t('guardrailMode_' + mode) })}
          variant="ghost" size="xs" disabled={disabled || saving} onClick={() => { void update(mode === 'safe' ? 'auto' : 'safe') }}
          className="h-7 gap-1 rounded-md border border-border px-2 text-muted-foreground">
          <ModeIcon className="h-3.5 w-3.5" /><span className="hidden text-xs sm:inline">{t('guardrailMode_' + mode)}</span>
        </Button>
      </TooltipTrigger>
      <TooltipContent>{t('guardrailModeHint_' + mode)}</TooltipContent>
    </Tooltip>}
    {error && <div role="alert" className="absolute right-0 top-9 z-[80] w-80 rounded-md border border-destructive/40 bg-background p-3 text-xs text-destructive shadow-lg">
      {error}<button type="button" className="ml-2 underline" onClick={() => setError('')}>{t('closePanel')}</button>
    </div>}
  </div>
}
