import { useState, useRef, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Check, Copy, Link } from 'lucide-react'
import { Button, Tooltip, TooltipContent, TooltipTrigger } from '@aspect/ui'
import { cn } from '@aspect/theme'

type OS = 'linux' | 'darwin' | 'windows'

const OS_OPTIONS: { value: OS; label: string }[] = [
  { value: 'linux', label: 'Linux' },
  { value: 'darwin', label: 'macOS' },
  { value: 'windows', label: 'Windows' },
]

interface Props {
  ioaURL: string | undefined
}

function detectOS(): OS {
  const ua = navigator.userAgent.toLowerCase()
  if (ua.includes('win')) return 'windows'
  if (ua.includes('mac')) return 'darwin'
  return 'linux'
}

function binaryName(os: OS): string {
  const arch = 'amd64'
  switch (os) {
    case 'windows':
      return `aiscan-full_windows_${arch}.zip`
    case 'darwin':
      return `aiscan-full_darwin_${arch}.zip`
    default:
      return `aiscan-full_linux_${arch}.zip`
  }
}

function buildCommand(os: OS, ioaURL: string): string {
  const dlURL = `https://github.com/chainreactors/aiscan/releases/latest/download/${binaryName(os)}`
  if (os === 'windows') {
    return `powershell -c "Invoke-WebRequest '${dlURL}' -OutFile aiscan.zip; Expand-Archive aiscan.zip -DestinationPath .; .\\aiscan-full.exe agent --server-url '${ioaURL}'"`
  }
  const bin = 'aiscan-full'
  return `curl -sL '${dlURL}' -o aiscan.zip && unzip -o aiscan.zip ${bin} && chmod +x ${bin} && ./${bin} agent --server-url '${ioaURL}'`
}

export default function QuickConnect({ ioaURL }: Props) {
  const { t } = useTranslation('app')
  const [open, setOpen] = useState(false)
  const [os, setOS] = useState<OS>(detectOS)
  const [copied, setCopied] = useState(false)
  const panelRef = useRef<HTMLDivElement>(null)

  const handleCopy = useCallback(async () => {
    if (!ioaURL) return
    await navigator.clipboard.writeText(buildCommand(os, ioaURL))
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }, [os, ioaURL])

  useEffect(() => {
    if (!open) return
    function onClickOutside(e: MouseEvent) {
      if (panelRef.current && !panelRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', onClickOutside)
    return () => document.removeEventListener('mousedown', onClickOutside)
  }, [open])

  if (!ioaURL) return null

  const command = buildCommand(os, ioaURL)

  return (
    <div className="relative" ref={panelRef}>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            aria-label={t('quickConnect')}
            onClick={() => setOpen(!open)}
            className={cn('hover:text-foreground', open ? 'text-foreground' : 'text-muted-foreground')}
          >
            <Link className="h-3.5 w-3.5" />
          </Button>
        </TooltipTrigger>
        <TooltipContent>{t('quickConnect')}</TooltipContent>
      </Tooltip>

      {open && (
        <div className="absolute right-0 top-full z-50 mt-2 w-[32rem] max-w-[90vw] rounded-lg border border-border bg-popover p-3 shadow-lg">
          <div className="mb-2 flex items-center justify-between">
            <span className="text-xs font-medium text-foreground">{t('quickConnectTitle')}</span>
            <div className="flex gap-1">
              {OS_OPTIONS.map((o) => (
                <button
                  key={o.value}
                  type="button"
                  onClick={() => { setOS(o.value); setCopied(false) }}
                  className={cn(
                    'rounded px-2 py-0.5 text-[11px] transition-colors',
                    os === o.value
                      ? 'bg-primary text-primary-foreground'
                      : 'bg-secondary text-muted-foreground hover:text-foreground',
                  )}
                >
                  {o.label}
                </button>
              ))}
            </div>
          </div>

          <div className="group relative rounded-md bg-muted/50 p-2">
            <pre className="overflow-x-auto whitespace-pre-wrap break-all font-mono text-[11px] leading-relaxed text-foreground/90 pr-8">
              {command}
            </pre>
            <button
              type="button"
              onClick={handleCopy}
              className="absolute right-2 top-2 rounded p-1 text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground"
              aria-label="Copy"
            >
              {copied ? <Check className="h-3.5 w-3.5 text-emerald-500" /> : <Copy className="h-3.5 w-3.5" />}
            </button>
          </div>

          <p className="mt-2 text-[10px] text-muted-foreground">
            {t('quickConnectHint')}
          </p>
        </div>
      )}
    </div>
  )
}
