import { useTranslation } from 'react-i18next'
import type { ObservationLabels } from '@/viewer'

/** Translation bindings for the shared native-event renderers. */
export function useObservationLabels() {
  const { t } = useTranslation('observe')
  const { t: traffic } = useTranslation('traffic')
  return {
    ...Object.fromEntries(['all', 'search', 'empty', 'emptyHint', 'filteredEmpty', 'clearFilters', 'related', 'session', 'assets', 'activity', 'events', 'metadata', 'back', 'copy', 'copied', 'newItems', 'jumpToLatest', 'started', 'completed', 'failed', 'allowed', 'denied', 'canceled'].map(key => [key, t(key)])),
    categories: Object.fromEntries(['tool', 'traffic', 'file', 'record', 'cstx', 'command', 'process', 'other'].map(key => [key, t(`categories.${key}`)])),
    file: Object.fromEntries(['access', 'read', 'write', 'edit', 'create', 'delete', 'size', 'transferred', 'edits', 'directory', 'tool', 'snapshot', 'control', 'unknown'].map(key => [key, t(`file.${key}`)])),
    cstx: Object.fromEntries(['loading', 'failed', 'raw', 'hosts', 'noHosts', 'ips', 'ports', 'apps', 'urls', 'frameworks', 'vulns'].map(key => [key, t(`cstx.${key}`)])),
    traffic: Object.fromEntries(['request', 'response', 'partial', 'loadFailed', 'requestError', 'noResponse', 'emptyBody'].map(key => [key, traffic(key)])),
  } satisfies ObservationLabels & { all?: string; search?: string }
}
