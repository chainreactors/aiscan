import { startFixtureRuntime } from './runtime'
import React, { useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { create, fromBinary, toBinary } from '@bufbuild/protobuf'
import { anyPack, anyUnpack, timestampFromDate } from '@bufbuild/protobuf/wkt'
import { EnvelopeSchema, AOPProtocolMessageSchema, EventSchema } from '@cyber/aop'
import { GuardrailProtocolMessageSchema, ReviewSchema, ReviewState, type Review } from '../../src/cyber-proto'
import { aopClient } from '../../src/api'
import { useGuardrailReviews } from '../../src/hooks/useGuardrailReviews'

type Query = { socket: Socket; id: string; sessionId: string; reviews: Review[] }
const fixture = {
  sockets: [] as Socket[], queries: [] as Query[], held: [] as Query[], hold: false,
  server: new Map<string, Review[]>(), failures: new Set<string>(), resolves: [] as string[],
  outcomes: [] as string[], renders: [] as Record<string, string[]>[],
  review(sessionId: string, operationId = `${sessionId}-operation`, expiresIn?: number) {
    return create(ReviewSchema, { sessionId, operation: { operationId }, state: ReviewState.PENDING,
      ...(expiresIn === undefined ? {} : { expiresAt: timestampFromDate(new Date(Date.now() + expiresIn)) }) })
  },
  respond(query: Query) {
    const message = fixture.failures.has(query.sessionId)
      ? create(AOPProtocolMessageSchema, { message: { case: 'protocolError', value: { code: 'UNAVAILABLE', message: 'fixture query failure' } } })
      : create(GuardrailProtocolMessageSchema, { message: { case: 'pendingResult', value: { reviews: query.reviews } } })
    query.socket.reply(query.id, message)
  },
  release() { for (const query of fixture.held.splice(0)) fixture.respond(query) },
  configure: (_ids: string[], _active: string | null) => {},
  emit: (_sessionId: string, _malformed = false) => {},
  resolve: (_sessionId: string, _review?: Review) => {},
}

class Socket {
  static OPEN = 1
  readyState = 0
  binaryType = ''
  onopen: (() => void) | null = null
  onclose: (() => void) | null = null
  onmessage: ((event: { data: ArrayBuffer }) => void) | null = null
  constructor() {
    fixture.sockets.push(this)
    queueMicrotask(() => { this.readyState = 1; this.onopen?.() })
  }
  send(data: Uint8Array) {
    const envelope = fromBinary(EnvelopeSchema, data)
    const request = anyUnpack(envelope.payload!, GuardrailProtocolMessageSchema)
    if (request?.message.case === 'pending') {
      const sessionId = request.message.value.sessionId
      const query = { socket: this, id: envelope.id, sessionId, reviews: fixture.server.get(sessionId) || [] }
      fixture.queries.push(query)
      if (fixture.hold) fixture.held.push(query)
      else queueMicrotask(() => fixture.respond(query))
    } else if (request?.message.case === 'resolve') {
      const { sessionId, operationId } = request.message.value
      fixture.resolves.push(`${sessionId}:${operationId}`)
      fixture.server.set(sessionId, (fixture.server.get(sessionId) || []).filter(review => review.operation?.operationId !== operationId))
      queueMicrotask(() => this.reply(envelope.id, create(GuardrailProtocolMessageSchema, { message: { case: 'resolved', value: {} } })))
    }
  }
  reply(replyTo: string, message: ReturnType<typeof create<typeof GuardrailProtocolMessageSchema>> | ReturnType<typeof create<typeof AOPProtocolMessageSchema>>) {
    if (this.readyState !== 1) return
    const schema = message.$typeName === 'cyber.guardrail.ProtocolMessage' ? GuardrailProtocolMessageSchema : AOPProtocolMessageSchema
    const frame = toBinary(EnvelopeSchema, create(EnvelopeSchema, { replyTo, payload: anyPack(schema, message as any) }))
    this.onmessage?.({ data: frame.buffer.slice(frame.byteOffset, frame.byteOffset + frame.byteLength) as ArrayBuffer })
  }
  close() { this.readyState = 3; this.onclose?.() }
}
window.WebSocket = Socket as any
const runtime = await startFixtureRuntime()
Object.assign(window, { guardrailFixture: fixture, guardrailClient: aopClient })

function Fixture() {
  const [ids, setIds] = useState(['a', 'b'])
  const [active, setActive] = useState<string | null>('a')
  const [events, setEvents] = useState<ReturnType<typeof create<typeof EventSchema>>[]>([])
  const { bySession, unavailable, resolve } = useGuardrailReviews(ids, active, events)
  fixture.configure = (next, selection) => { setIds(next); setActive(selection) }
  fixture.emit = (sessionId, malformed) => setEvents(current => [...current, create(EventSchema, {
    id: `review-${current.length}`, sessionId, payload: { case: 'extension', value: malformed
      ? { typeUrl: 'type.googleapis.com/cyber.guardrail.Review', value: new Uint8Array([255]) }
      : anyPack(ReviewSchema, fixture.review(sessionId)) },
  })])
  fixture.resolve = (sessionId, review) => {
    void resolve(sessionId, review || fixture.review(sessionId), true)
      .then(() => fixture.outcomes.push('resolved'), error => fixture.outcomes.push(error.message))
  }
  const visible = Object.fromEntries(Object.entries(bySession).map(([id, reviews]) => [id, reviews.map(review => review.operation!.operationId)]))
  useEffect(() => { fixture.renders.push(visible) })
  return <><pre data-testid="state">{JSON.stringify({ reviews: visible, unavailable })}</pre>
    {Object.entries(bySession).flatMap(([id, reviews]) => reviews.map(review => <button key={`${id}:${review.operation!.operationId}`}
      disabled={unavailable[id]} onClick={() => fixture.resolve(id, review)}>{review.operation!.operationId}</button>))}</>
}
createRoot(document.getElementById('root')!).render(<Fixture />)
