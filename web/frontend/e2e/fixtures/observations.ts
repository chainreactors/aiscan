import { create, type MessageInitShape } from '@bufbuild/protobuf'
import { anyPack } from '@bufbuild/protobuf/wkt'
import { ArtifactSchema, CompletedSchema, Correlation, EventSchema, FileAccessOp, FileAccessSchema, FileAccessSource, PtySessionSchema, RefSchema, StartedSchema, TrafficFlowSchema, type Event } from '../../cyber-ui/packages/aop/src/index'

/** Original native messages shared by browser checks and the headed preview. */
export function observationEvents(sessionId = 'traffic-a'): Event[] {
  const encode = (text: string) => new TextEncoder().encode(text)
  const events: Event[] = []
  const add = (payload: MessageInitShape<typeof EventSchema>['payload'], extensions: Event['extensions'] = []) => events.push(create(EventSchema, {
    id: `observation-${events.length + 1}`, sessionId, emitter: 'local', turnId: 'observed-turn', seq: BigInt(events.length + 1),
    emittedAt: { seconds: BigInt(1790840000 + events.length) }, payload, extensions,
  }))
  const ref = (callId: string, operationId = callId) => anyPack(RefSchema, create(RefSchema, { callId, operationId, correlation: Correlation.EXPLICIT }))
  add({ case: 'toolCall', value: { id: 'scan-call', name: 'bash', arguments: { mediaType: 'application/json', data: encode('{"command":"scan example.test > report.txt"}') } } })
  add({ case: 'extension', value: anyPack(StartedSchema, create(StartedSchema, { kind: 'tool', name: 'bash' })) }, [ref('scan-call')])
  add({ case: 'extension', value: anyPack(TrafficFlowSchema, create(TrafficFlowSchema, {
    id: 'captured-http', complete: true, request: { method: 'GET', url: 'https://example.test/api/assets', protocol: 'HTTP/1.1' },
    response: { statusCode: 200, reasonPhrase: 'OK', body: encode('{"host":"127.0.0.1","port":8080}') },
  })) }, [ref('scan-call', 'http-operation')])
  add({ case: 'extension', value: anyPack(FileAccessSchema, create(FileAccessSchema, {
    path: 'reports/scan.txt', workDir: 'D:/workspace', op: FileAccessOp.WRITE, source: FileAccessSource.TOOL, size: 1234n, bytes: 1234n,
  })) }, [ref('scan-call', 'file-operation')])
  add({ case: 'extension', value: anyPack(ArtifactSchema, create(ArtifactSchema, {
    tool: 'gogo', kind: 'service', target: '127.0.0.1:8080', mediaType: 'application/json', data: encode('{"ip":"127.0.0.1","port":"8080","protocol":"http"}'),
  })) }, [ref('scan-call', 'asset-operation')])
  add({ case: 'extension', value: anyPack(CompletedSchema, create(CompletedSchema, { kind: 'tool', name: 'bash', startedAt: events[0].emittedAt })) }, [ref('scan-call')])
  add({ case: 'toolResult', value: { callId: 'scan-call', name: 'bash', output: [{ value: { case: 'text', value: { text: 'Scan completed; report saved.' } } }] } })
  add({ case: 'toolCall', value: { id: 'record-call', name: 'record', arguments: { mediaType: 'application/json', data: encode('{"action":"screenshot"}') } } })
  add({ case: 'toolResult', value: { callId: 'record-call', name: 'record', output: [
    { value: { case: 'text', value: { text: '{"target":{"kind":"desktop","width":1280,"height":720},"bytes":68}' } } },
    { value: { case: 'media', value: { kind: 'image', resource: { filename: 'desktop.png', mediaType: 'image/png', source: { case: 'data', value: Uint8Array.from(atob('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jkAAAAABJRU5ErkJggg=='), character => character.charCodeAt(0)) } } } } },
  ] } })
  add({ case: 'extension', value: anyPack(CompletedSchema, create(CompletedSchema, { kind: 'command', name: '!scan --target example.test' })) }, [ref('', 'command-operation')])
  add({ case: 'extension', value: anyPack(CompletedSchema, create(CompletedSchema, { kind: 'process', name: 'scanner --version', failure: { message: 'Process exited with code 2' } })) }, [ref('', 'process-operation'), anyPack(PtySessionSchema, create(PtySessionSchema, { pid: 4321, state: 'exited', exitCode: 2 }))])
  add({ case: 'extension', value: { typeUrl: 'type.googleapis.com/example.CustomObservation', value: new Uint8Array([1, 2, 255]) } })
  add({ case: 'turnEnded', value: { stopReason: 'completed' } })
  return events
}
