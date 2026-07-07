import { useState, type ReactNode } from 'react'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { cn } from '@aspect/theme'

export interface MessageBubbleProps {
  role: 'user' | 'assistant' | 'system'
  actorName?: string | null
  content?: ReactNode
  timestamp?: string
  streaming?: boolean
  defaultExpanded?: boolean
  className?: string
  children?: ReactNode
}

export default function MessageBubble({
  role,
  actorName,
  content,
  timestamp,
  streaming,
  defaultExpanded = true,
  className,
  children,
}: MessageBubbleProps) {
  const [expanded, setExpanded] = useState(defaultExpanded)
  const time = timestamp ? new Date(timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false }) : ''
  const body = children ?? content

  if (role === 'system') {
    return (
      <div
        className={cn(
          'w-full rounded-md border border-border bg-muted/50 px-4 py-2 text-xs text-muted-foreground',
          className,
        )}
      >
        {body || <span className="italic">Empty message</span>}
      </div>
    )
  }

  const isUser = role === 'user'
  const label = isUser ? 'You' : actorName || 'Assistant'
  const Chevron = expanded ? ChevronDown : ChevronRight

  return (
    <div className={cn('flex w-full flex-col', isUser && 'items-end', className)}>
      {/* Actor + time — shown only below xl. At xl the conversation's timeline
          rail already carries actor + time, so an in-bubble header would just
          duplicate it (and in a second, less-refined format). Below xl the rail
          is hidden, so this becomes the sole label; it also toggles collapse. */}
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        className={cn(
          'flex items-center gap-2 text-[10px] text-muted-foreground xl:hidden',
          isUser && 'flex-row-reverse',
        )}
      >
        <span className="font-medium">{label}</span>
        {time && <span className="font-mono">{time}</span>}
        <Chevron className="h-2.5 w-2.5 shrink-0 text-muted-foreground" />
      </button>

      {/* body */}
      {expanded && (
        <div
          className={cn(
            'mt-1 min-w-0 rounded-lg px-3 py-2 text-sm leading-relaxed xl:mt-0',
            isUser
              ? 'max-w-[min(85%,52rem)] bg-primary/10 text-foreground'
              : 'w-full border border-border border-l-2 border-l-ai/40 bg-card text-foreground shadow-soft',
            streaming && 'border-primary/40',
          )}
        >
          {body || (!streaming && <span className="text-muted-foreground italic">Empty message</span>)}
          {streaming && <StreamingCursor />}
        </div>
      )}
    </div>
  )
}

export function StreamingCursor() {
  return (
    <span className="ml-0.5 inline-block h-[14px] w-[2px] animate-pulse bg-primary align-text-bottom" />
  )
}
