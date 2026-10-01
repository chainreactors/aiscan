import React, { useState } from 'react'
import { createRoot } from 'react-dom/client'
import { create, fromBinary } from '@bufbuild/protobuf'
import { anyPack } from '@bufbuild/protobuf/wkt'
import { TooltipProvider } from '@cyber/ui'
import { EventSchema } from '../../cyber-ui/packages/aop/src/gen/aop/event_pb'
import { RecapSchema } from '../../src/cyber-proto'
import ChatPanel from '../../src/components/ChatPanel'
import '../../src/i18n'
import '../../src/index.css'

const base = { sessionId: 'session-1', turnId: 'turn-1', emitter: 'agent' }
const initial = [
  create(EventSchema, { ...base, id: 'answer', seq: 1n, payload: { case: 'message', value: {
    id: 'answer', role: 'assistant', content: [{ value: { case: 'text', value: { text: '已修复任务结束后的摘要显示，并验证跨轮次归属。' } } }],
  } } }),
  create(EventSchema, { ...base, id: 'end', seq: 2n, payload: { case: 'turnEnded', value: { stopReason: 'completed' } } }),
  create(EventSchema, { ...base, id: 'recap', seq: 3n, emitter: 'recap', payload: { case: 'extension', value:
    anyPack(RecapSchema, create(RecapSchema, { text: '修复摘要显示与任务归属，相关测试已通过。' })) } }),
]

function Fixture() {
  const [events, setEvents] = useState(initial)
  ;(window as any).renderRecapEvents = (values: number[][]) => setEvents(values.map(value => fromBinary(EventSchema, new Uint8Array(value))))
  return <TooltipProvider><div className="h-screen"><ChatPanel timeline={[]} aopEvents={events}
    guardrailReviews={[]} onResolveGuardrail={async () => {}} scanResults={new Map()}
    isThinking={false} isBusy={false} canPause={false} error="" hasActiveSession activeSessionID={null}
    onSend={async () => true} ensureSession={async () => null} onPause={() => {}} onClearError={() => {}} />
  </div></TooltipProvider>
}

createRoot(document.getElementById('root')!).render(<Fixture />)
