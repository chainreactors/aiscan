import { Bot, FileSearch } from 'lucide-react'
import type { TimelineRendererConfig } from '@/viewer'
import i18n from '../i18n'
import ScanSummaryCard from '../components/chat/ScanSummaryCard'

export const scanRenderer: TimelineRendererConfig = {
  renderer: ({ item }) => {
    const scanID = item.data.scanID as string
    // Render the status even while the browser is rebuilding archived results.
    return (
      <ScanSummaryCard
        scanID={scanID}
      />
    )
  },
  mark: {
    label: () => i18n.t('scan:results'),
    icon: FileSearch,
    dotClass: 'border-muted-foreground bg-muted',
  },
}

export const agentRenderer: TimelineRendererConfig = {
  renderer: ({ item }) => (
    <div className="flex items-center justify-center gap-2 py-1">
      <div className="h-px flex-1 bg-border" />
      <span className="flex items-center gap-1.5 text-[10px] text-muted-foreground">
        <Bot className="h-3 w-3" />
        {i18n.t('chat:agentJoined', { name: (item.data.agentName as string) || i18n.t('chat:agent') })}
      </span>
      <div className="h-px flex-1 bg-border" />
    </div>
  ),
  mark: {
    label: () => i18n.t('chat:agent'),
    icon: Bot,
    dotClass: 'border-primary bg-primary',
  },
}
