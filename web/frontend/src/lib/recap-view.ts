import { anyUnpack } from '@bufbuild/protobuf/wkt'
import { RecapSchema } from '../cyber-proto'
import type { AOPEvent, ViewerTimelineItem } from '@/viewer'

const key = (sessionId?: string, turnId?: string) => JSON.stringify([sessionId, turnId])

// Recaps annotate the last response of their original turn. They never create
// a timeline row, move a response, or participate in execution lifecycle.
export function withRecaps(items: ViewerTimelineItem[], events: AOPEvent[]): ViewerTimelineItem[] {
  const ended = new Set(events.filter(event => event.payload.case === 'turnEnded').map(event => key(event.sessionId, event.turnId)))
  const recaps = new Map<string, { text: string; seq: bigint }>()
  for (const event of events) {
    if (event.payload.case !== 'extension' || !event.turnId) continue
    try {
      const recap = anyUnpack(event.payload.value, RecapSchema)
      if (!recap?.text.trim()) continue
      const id = key(event.sessionId, event.turnId)
      if (!ended.has(id) || (recaps.get(id)?.seq ?? -1n) >= event.seq) continue
      recaps.set(id, { text: recap.text, seq: event.seq })
    } catch { /* Malformed optional annotations do not affect the transcript. */ }
  }
  const attach = (source: ViewerTimelineItem[]): ViewerTimelineItem[] => {
    const result = source.filter(item => item.kind !== 'extension'
      || (item.extensionType !== RecapSchema.typeName && !item.extensionType.endsWith('/' + RecapSchema.typeName)))
    const last = new Map<string, number>()
    result.forEach((item, index) => {
      if (item.kind === 'assistant_response') last.set(key(item.sessionId, item.turnId), index)
    })
    return result.map((item, index) => {
      if (item.kind === 'subagent_run') return { ...item, items: attach(item.items) }
      if (item.kind !== 'assistant_response') return item
      const id = key(item.sessionId, item.turnId)
      const recap = recaps.get(id)
      if (!recap || last.get(id) !== index) return item
      return { ...item, response: { content: item.response?.content ?? '',
        metadata: { ...item.response?.metadata, recap: recap.text } } }
    })
  }
  return attach(items)
}
