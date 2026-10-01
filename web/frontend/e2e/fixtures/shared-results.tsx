import React from 'react'
import { createRoot } from 'react-dom/client'
import { create } from '@bufbuild/protobuf'
import { EventSchema, type Event } from '@cyber/aop'
import { AOPChatPanel, ToolDefinitionCard } from '@cyber/viewer'
import { buildSCOModel, EasmResultView, type SCONode } from '@cyber/cstx-easm'
import { TooltipProvider } from '@cyber/ui'
import '../../src/index.css'

const encoder = new TextEncoder()
const png = Uint8Array.from(atob('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jkAAAAABJRU5ErkJggg=='), char => char.charCodeAt(0))
const events: Event[] = []
function add(payload: Event['payload']) {
  events.push(create(EventSchema, { id: `shared-${events.length}`, seq: BigInt(events.length + 1), sessionId: 'shared-session', turnId: 'shared-turn', payload }))
}
add({ case: 'toolCall', value: { id: 'file', name: 'file', arguments: { mediaType: 'application/json', data: encoder.encode('{"path":"report.txt"}') } } })
add({ case: 'toolResult', value: { callId: 'file', name: 'file', output: [{ value: { case: 'media', value: {
  kind: 'file', resource: { filename: 'report.txt', mediaType: 'text/plain', source: { case: 'data', value: encoder.encode('scanner report\nready') } },
} } }] } })
add({ case: 'toolCall', value: { id: 'record', name: 'record', arguments: { mediaType: 'application/json', data: encoder.encode('{"action":"screenshot"}') } } })
add({ case: 'toolResult', value: { callId: 'record', name: 'record', output: [
  { value: { case: 'text', value: { text: '{"target":{"kind":"desktop","width":1280,"height":720},"bytes":68}' } } },
  { value: { case: 'media', value: { kind: 'image', resource: { filename: 'desktop.png', mediaType: 'image/png', source: { case: 'data', value: png } } } } },
] } })
add({ case: 'turnEnded', value: { stopReason: 'completed' } })

const nodes: SCONode[] = [
  { cstx_type: 'ip', cstx_id: 'demo-ip', ip: '127.0.0.1' },
  { cstx_type: 'port', cstx_id: 'demo-port', ip: '127.0.0.1', port: '8080', protocol: 'http' },
  { cstx_type: 'url', cstx_id: 'demo-url', ip: '127.0.0.1', port: 8080, scheme: 'http', host: '127.0.0.1:8080', path: '/api/nested/report', status_code: 200 },
  { cstx_type: 'vuln', cstx_id: 'demo-vuln', ip: '127.0.0.1', port: '8080', name: 'Demo vulnerability', severity: 'high',
    request: 'GET /api/nested/report HTTP/1.1\r\nHost: 127.0.0.1:8080\r\n\r\n', response: 'HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\nevidence preserved' },
]

createRoot(document.getElementById('root')!).render(<TooltipProvider>
  <main className="mx-auto max-w-4xl space-y-6 p-4">
    <section className="flex h-[32rem] flex-col rounded border border-border" data-testid="shared-chat"><AOPChatPanel events={events} lifecycle="none" /></section>
    <section data-testid="shared-assets"><EasmResultView model={buildSCOModel(nodes)} anchorPrefix="shared-demo" /></section>
    <section data-testid="shared-tool"><ToolDefinitionCard name="!scan" description="Scan a target" usage="!scan -i TARGET" aliases={['!inspect']} /></section>
  </main>
</TooltipProvider>)
