import type { TimelineItem as ViewerTimelineItem } from '@/viewer'
import type { TimelineItem as LocalTimelineItem } from '../hooks/useChatSession'

// Only extension-kind items reach here — message/assistant_response/tool_call/
// thinking are rendered inline by ChatPanel and never passed to this mapper.
export function toViewerExtensionItem(item: LocalTimelineItem): ViewerTimelineItem | null {
  switch (item.kind) {
    case 'scan_started':
    case 'scan_progress':
      return {
        id: item.id,
        kind: 'extension',
        timestamp: item.timestamp,
        extensionType: 'scan_started',
        data: {
          scanID: item.scanID || '',
          lines: item.scanLines || [],
        },
      }

    case 'scan_complete':
      return {
        id: item.id,
        kind: 'extension',
        timestamp: item.timestamp,
        extensionType: 'scan_complete',
        data: {
          scanID: item.scanID || '',
          result: item.scanResult,
        },
      }

    case 'agent_joined':
      return {
        id: item.id,
        kind: 'extension',
        timestamp: item.timestamp,
        extensionType: 'agent_joined',
        data: {
          agentName: item.agentName || '',
        },
      }

    default:
      return null
  }
}
