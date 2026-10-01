import { Activity } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { Event } from '@cyber/aop'
import { ObservabilityPanel as ObservabilityView, ObservationDisplay } from '@/viewer'
import { useObservationLabels } from '../lib/observation-labels'
import { useToolPresentation } from './chat/ToolCallResult'
import { ToolDrawer } from './layout/ToolDrawer'
import AssetPanel from './AssetPanel'

export function ObservedEvent({ event }: { event: Event }) {
  return <ObservationDisplay event={event} labels={useObservationLabels()} />
}

export default function ObservabilityPanel({ open, onClose, events, sessionID, assetCount, onSendToChat, onAssetsChanged }: {
  open: boolean; onClose: () => void; events: readonly Event[]; sessionID: string | null
  assetCount: number; onSendToChat: (text: string) => void; onAssetsChanged: () => void
}) {
  const { t } = useTranslation('observe')
  const labels = useObservationLabels()
  const toolProps = useToolPresentation(sessionID, events)
  return <ToolDrawer open={open} onClose={onClose} icon={Activity} title={t('title')} description={t('description')}
    contentProps={{ onInteractOutside: event => event.preventDefault() }}>
    <ObservabilityView events={events} labels={labels} toolProps={toolProps} assetCount={assetCount}
      assets={<AssetPanel open={open} onClose={onClose} onSendToChat={onSendToChat} onChanged={onAssetsChanged} />} />
  </ToolDrawer>
}
