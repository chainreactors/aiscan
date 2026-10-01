import React, { useState } from 'react'
import { createRoot } from 'react-dom/client'
import { create, fromBinary } from '@bufbuild/protobuf'
import { timestampFromDate } from '@bufbuild/protobuf/wkt'
import { TooltipProvider } from '@cyber/ui'
import { EventSchema, type Event } from '@cyber/aop'
import ChatPanel from '../../src/components/ChatPanel'
import '../../src/i18n'
import '../../src/index.css'

function Fixture() {
  const [events, setEvents] = useState<Event[]>([])
  const [busy, setBusy] = useState(false)
  ;(window as any).renderRecordEvents = (values: number[][], running = false) => {
    setEvents(values.map(value => fromBinary(EventSchema, new Uint8Array(value))))
    setBusy(running)
  }
  ;(window as any).showRecordDemo = async () => {
    const canvas = document.createElement('canvas')
    canvas.width = 960; canvas.height = 540
    const ctx = canvas.getContext('2d')!
    const draw = (frame = 0) => {
      ctx.fillStyle = '#0b1120'; ctx.fillRect(0, 0, 960, 540)
      ctx.fillStyle = '#122238'; ctx.fillRect(24, 24, 912, 68)
      ctx.fillStyle = '#93c5fd'; ctx.font = 'bold 26px sans-serif'; ctx.fillText('aiscan · record preview', 48, 67)
      ctx.fillStyle = '#94a3b8'; ctx.font = '18px sans-serif'; ctx.fillText('Desktop / window capture', 48, 136)
      for (let i = 0; i < 3; i++) {
        ctx.fillStyle = '#162840'; ctx.fillRect(48 + i * 292, 168, 264, 138)
        ctx.fillStyle = '#cbd5e1'; ctx.font = '16px sans-serif'; ctx.fillText(['Screenshot', 'MP4 / H.264', 'Recording status'][i], 66 + i * 292, 200)
        ctx.fillStyle = '#60a5fa'; ctx.font = 'bold 30px sans-serif'; ctx.fillText(['960 × 540', '10 FPS', 'Completed'][i], 66 + i * 292, 260)
      }
      ctx.fillStyle = '#60a5fa'; ctx.fillRect(48, 350, 70 + frame * 60, 12)
      ctx.fillStyle = '#94a3b8'; ctx.font = '16px sans-serif'; ctx.fillText('Generated demonstration media', 48, 480)
    }
    draw()
    const image = new Uint8Array(await (await new Promise<Blob>(resolve => canvas.toBlob(blob => resolve(blob!), 'image/png'))).arrayBuffer())
    let video: Uint8Array | undefined
    const type = 'video/mp4;codecs=avc1.420028'
    if (MediaRecorder.isTypeSupported(type)) {
      const stream = canvas.captureStream(10)
      const recorder = new MediaRecorder(stream, { mimeType: type })
      const chunks: Blob[] = []
      const done = new Promise<void>(resolve => { recorder.onstop = () => resolve() })
      recorder.ondataavailable = event => chunks.push(event.data)
      recorder.start()
      for (let frame = 0; frame < 12; frame++) {
        draw(frame)
        await new Promise(resolve => setTimeout(resolve, 100))
      }
      recorder.stop(); await done
      stream.getTracks().forEach(track => track.stop())
      video = new Uint8Array(await new Blob(chunks).arrayBuffer())
    }
    const values: Event[] = []
    const add = (payload: Event['payload']) => values.push(create(EventSchema, { id: `demo-${values.length}`, seq: BigInt(values.length + 1),
      sessionId: 'demo-session', turnId: 'demo-turn', emitter: 'agent', emittedAt: timestampFromDate(new Date(Date.now() + values.length)), payload }))
    const tool = (id: string, action: string, info: unknown, kind?: string, bytes?: Uint8Array) => {
      add({ case: 'toolCall', value: { id, name: 'record', arguments: { data: new TextEncoder().encode(JSON.stringify({ action })), mediaType: 'application/json' } } })
      add({ case: 'toolResult', value: { callId: id, name: 'record', output: [
        { value: { case: 'text', value: { text: JSON.stringify(info) } } },
        ...(bytes ? [{ value: { case: 'media' as const, value: { kind: kind!, resource: {
          mediaType: kind === 'image' ? 'image/png' : 'video/mp4', filename: kind === 'image' ? 'desktop.png' : 'capture.mp4', source: { case: 'data' as const, value: bytes },
        } } } }] : []),
      ] } })
    }
    tool('demo-shot', 'screenshot', { action: 'screenshot', target: { kind: 'desktop', width: 960, height: 540 }, bytes: image.length }, 'image', image)
    if (video) tool('demo-video', 'record', { recording_id: 'demo-recording', state: 'completed', target: { kind: 'window', title: 'aiscan demo', width: 960, height: 540 }, fps: 10, frames: 12, duration_ms: 1200, bytes: video.length }, 'video', video)
    tool('demo-status', 'status', [{ recording_id: 'active-recording', state: 'recording', fps: 30, target: { kind: 'desktop' } }, { recording_id: 'demo-recording', state: 'completed', frames: 12, duration_ms: 1200 }])
    add({ case: 'message', value: { id: 'demo-answer', role: 'assistant', content: [{ value: { case: 'text', value: { text: '截图、录屏和录制状态预览。演示媒体由页面生成。' } } }] } })
    add({ case: 'turnEnded', value: { stopReason: 'completed' } })
    setEvents(values)
    return { image: Array.from(image), video: video ? Array.from(video) : [] }
  }
  return <TooltipProvider><div className="h-screen"><ChatPanel timeline={[]} aopEvents={events}
    guardrailReviews={[]} onResolveGuardrail={async () => {}}
    isThinking={false} isBusy={busy} canPause={false} error="" hasActiveSession activeSessionID="chat-1"
    onSend={async () => true} ensureSession={async () => 'chat-1'} onPause={() => {}} onClearError={() => {}} />
  </div></TooltipProvider>
}

createRoot(document.getElementById('root')!).render(<Fixture />)
