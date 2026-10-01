import { createContext, useContext, type ReactNode } from 'react'
import type { Event } from '@cyber/aop'
import { useTranslation } from 'react-i18next'
import { ToolResultDisplay } from '@/viewer'
import { recordMediaURL } from '../../lib/record-result'
import { useObservationLabels } from '../../lib/observation-labels'
import ScannerToolCall, { type ScannerToolCallProps } from './ScannerToolCall'

const SessionContext = createContext('')

export function ToolResultsProvider({ sessionID, children }: { sessionID: string | null; children: ReactNode }) {
  return <SessionContext.Provider value={sessionID || ''}>{children}</SessionContext.Provider>
}

/** Host bindings only; typed result projection and rendering belong to cyber-ui. */
export function useToolPresentation(session?: string | null, events?: readonly Event[]) {
  const contextualSession = useContext(SessionContext)
  const sessionID = session === undefined ? contextualSession : session || ''
  const { t } = useTranslation('chat')
  return {
    resolveMedia: (_media, index, eventID, download) => sessionID && eventID && (!events || events.some(event => event.id === eventID && event.payload.case === 'toolResult' && event.payload.value.name === 'record'))
      ? recordMediaURL(sessionID, eventID, index, download) : undefined,
    labels: { arguments: t('toolCard.arguments'), result: t('toolCard.result'), completed: t('toolCard.completed'), failed: t('toolCard.failed'), running: t('toolCard.running') },
    recordLabels: {
      title: t('record.title'), desktop: t('record.desktop'), window: t('record.window'), empty: t('record.empty'),
      download: t('record.download'), openImage: t('record.openImage'), unavailable: t('record.unavailable'), loadFailed: t('record.loadFailed'),
      rawOutput: t('toolCard.rawOutput'), duration: seconds => t('record.duration', { seconds }), frames: count => t('record.frames', { count }),
      actions: Object.fromEntries(['screenshot', 'record', 'start', 'stop', 'status'].map(action => [action, t(`record.actions.${action}`)])),
      states: Object.fromEntries(['starting', 'recording', 'stopping', 'completed', 'failed'].map(state => [state, t(`record.states.${state}`)])),
    },
  } satisfies Pick<ScannerToolCallProps, 'resolveMedia' | 'labels' | 'recordLabels'>
}

export function ToolCallResult(props: ScannerToolCallProps) {
  const presentation = useToolPresentation()
  const observationLabels = useObservationLabels()
  return props.toolName === 'record'
    ? <ToolResultDisplay {...props} {...presentation} observationLabels={observationLabels} />
    : <ScannerToolCall {...props} observationLabels={observationLabels} />
}
