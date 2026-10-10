import type { AppPlugin } from '../runtime/services'
import type { TimelineRendererConfig } from '../viewer'
import { scanRenderer, agentRenderer } from '../lib/chat-extensions'
import { jevSegmentRenderer, jevCompilationRenderer, jevCheckRenderer } from '../components/chat/JEVTimeline'

function renderers(name: string, entries: Readonly<Record<string, TimelineRendererConfig>>): AppPlugin {
  return { name, inject: ['slots', 'slot.conversation.extensions'], apply(ctx) {
    const slots = ctx.slots
    for (const [id, renderer] of Object.entries(entries)) ctx.effect(() => slots.contribute('conversation.extensions', id, renderer))
  } }
}
// Archive rendering does not depend on online capabilities.
export const ScanRendererPlugin = renderers('scan-renderer', { scan_complete: scanRenderer })
export const AgentRendererPlugin = renderers('agent-renderer', { agent_joined: agentRenderer })
export const JEVRendererPlugin = renderers('jev-renderer', {
  jev_segment: jevSegmentRenderer, jev_compilation: jevCompilationRenderer, jev_check: jevCheckRenderer,
})
