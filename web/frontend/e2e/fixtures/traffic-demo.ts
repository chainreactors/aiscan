import { observationEvents } from './observations'
import { create, fromBinary, toBinary } from '@bufbuild/protobuf'
import { anyPack, anyUnpack } from '@bufbuild/protobuf/wkt'
import { EventSchema } from '../../cyber-ui/packages/aop/src/gen/aop/event_pb'
import { TextContentSchema } from '../../cyber-ui/packages/aop/src/gen/aop/content_pb'
import { FlowSchema } from '../../cyber-ui/packages/aop/src/gen/aop/traffic/protocol_pb'
import { EnvelopeSchema } from '../../cyber-ui/packages/aop/src/gen/aop/envelope_pb'
import { ProtocolMessageSchema } from '../../cyber-ui/packages/aop/src/gen/aop/protocol_pb'
import { ListEventsResponseSchema } from '../../cyber-ui/packages/aop/src/gen/aop/chat_pb'
import { GetSessionResponseSchema, ListSessionsResponseSchema } from '../../src/gen/types/chat_pb'
import { GetStatusResponseSchema } from '../../src/gen/types/system_pb'
import { GetConfigResponseSchema, LLMProbeResultSchema } from '../../src/gen/types/config_pb'
import { ListAgentsResponseSchema } from '../../src/gen/types/agent_pb'
import { ArtifactSchema } from '../../cyber-ui/packages/aop/src/index'
import { SyncArtifactsRequestSchema, SyncArtifactsResponseSchema } from '../../src/gen/types/artifact_pb'

const session = { session: { id: 'traffic-preview', nodeId: 'local', title: '工具时间线与统一可观测预览' } }
const events = observationEvents('traffic-preview')
const historic = observationEvents('previous-session')[4]
historic.id = 'historical-asset'
if (historic.payload.case === 'extension') {
  const artifact = anyUnpack(historic.payload.value, ArtifactSchema)!
  artifact.target = '10.42.0.5:443'
  artifact.data = new TextEncoder().encode('{"ip":"10.42.0.5","port":"443","protocol":"https"}')
  historic.payload.value = anyPack(ArtifactSchema, artifact)
}
const archived = [historic, events[4]]

export function artifactArchiveDemo(request: number[]) {
  const cursor = fromBinary(SyncArtifactsRequestSchema, new Uint8Array(request)).afterCursor
  return Array.from(toBinary(SyncArtifactsResponseSchema, create(SyncArtifactsResponseSchema, {
    artifacts: archived.slice(Number(cursor || 0)).map((event, index) => ({ event, cursor: String(Number(cursor || 0) + index + 1) })),
  })))
}

export function trafficDemo(image?: Uint8Array) {
  if (image?.length) {
    const result = events.find(event => event.payload.case === 'toolResult' && event.payload.value.name === 'record')!
    if (result.payload.case === 'toolResult') {
      const media = result.payload.value.output[1].value
      if (media.case === 'media' && media.value.resource) media.value.resource.source = { case: 'data', value: image }
      result.payload.value.output[0].value = { case: 'text', value: create(TextContentSchema, { text: JSON.stringify({ action: 'screenshot', target: { kind: 'desktop', width: 960, height: 540 }, bytes: image.length }) }) }
    }
  }
  return {
    ListSessions: Array.from(toBinary(ListSessionsResponseSchema, create(ListSessionsResponseSchema, { sessions: [session] }))),
    GetSession: Array.from(toBinary(GetSessionResponseSchema, create(GetSessionResponseSchema, { session }))),
    ListEvents: Array.from(toBinary(ListEventsResponseSchema, create(ListEventsResponseSchema, { events: events.map((event, index) => ({ event, cursor: String(index + 1) })) }))),
    GetStatus: Array.from(toBinary(GetStatusResponseSchema, create(GetStatusResponseSchema, { status: { version: 'traffic-preview', llmAvailable: true, llmApiKeyConfigured: true, llmModel: 'Preview', configLoaded: true } }))),
    GetConfig: Array.from(toBinary(GetConfigResponseSchema, create(GetConfigResponseSchema, { config: { loaded: true } }))),
    TestLLM: Array.from(toBinary(LLMProbeResultSchema, create(LLMProbeResultSchema, { ok: true, model: 'Preview' }))),
    ListAgents: Array.from(toBinary(ListAgentsResponseSchema, create(ListAgentsResponseSchema, { agents: [
      { hello: { nodeId: 'local', name: '扫描 Agent', tools: [
        { name: 'record', type: 'function', description: '捕获截图与录制视频', inputSchema: { mediaType: 'application/json', data: new TextEncoder().encode('{"type":"object","properties":{"action":{"type":"string","enum":["screenshot","start","stop"]}},"required":["action"]}') } },
      ] }, commands: [
        { name: '!gogo', description: '发现主机、端口与服务，输出 CSTX 资产', usage: '!gogo -i <target> -p <ports>', aliases: ['!scan'] },
      ] },
      { hello: { nodeId: 'inspector', name: '分析 Agent', tools: [
        { name: 'read', type: 'function', description: '读取指定文件', inputSchema: { mediaType: 'application/json', data: new TextEncoder().encode('{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}') } },
      ] }, commands: [
        { name: '!inspect', description: '读取文件并检查捕获的数据', usage: '!inspect <path>', aliases: ['!analyze'] },
      ] },
    ] }))),
  }
}

/** Deliver one additional capture through the actual browser subscription. */
export function liveTrafficDemo(frame: number[]) {
  const envelope = fromBinary(EnvelopeSchema, new Uint8Array(frame))
  const protocol = envelope.payload && anyUnpack(envelope.payload, ProtocolMessageSchema)
  if (protocol?.message.case !== 'watchEventsRequest') return
  const event = fromBinary(EventSchema, toBinary(EventSchema, events[2]))
  event.id = 'traffic-demo-live'
  event.seq = BigInt(events.length + 1)
  if (event.payload.case !== 'extension') return
  const flow = anyUnpack(event.payload.value, FlowSchema)!
  flow.id = 'flow-live'
  flow.request!.url = 'https://example.test/api/live'
  event.payload = { case: 'extension', value: anyPack(FlowSchema, flow) }
  return Array.from(toBinary(EnvelopeSchema, create(EnvelopeSchema, { id: 'live-delivery', replyTo: envelope.id, deliveryCursor: String(events.length + 1), payload: anyPack(ProtocolMessageSchema, create(ProtocolMessageSchema, { message: { case: 'event', value: event } })) })))
}
