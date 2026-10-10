import { createClient, Code, ConnectError } from '@connectrpc/connect'
import { createConnectTransport } from '@connectrpc/connect-web'
import { AOPClient } from '@cyber/aop'
import {
  AgentService, ArtifactService, ConfigService, ScanService, SessionService, SystemService,
} from '../cyber-proto'
import type { DescMessage } from '@bufbuild/protobuf'
import type { AuthService } from './auth'

export function createConnection(auth: AuthService, generation: number) {
  const controller = new AbortController()
  const transport = createConnectTransport({
    baseUrl: window.location.origin, useBinaryFormat: true,
    interceptors: [next => async request => {
      if (!auth.isCurrent(generation) || controller.signal.aborted) throw new Error('Connection is inactive')
      try {
        const response = await next({ ...request, signal: AbortSignal.any([request.signal, controller.signal]) })
        if (!auth.isCurrent(generation) || controller.signal.aborted) throw new Error('Connection is inactive')
        return response
      }
      catch (error) {
        if (ConnectError.from(error).code === Code.Unauthenticated) auth.invalidate(generation)
        throw error
      }
    }],
  })
  const rpc = {
    sessions: createClient(SessionService, transport), scans: createClient(ScanService, transport),
    config: createClient(ConfigService, transport), agents: createClient(AgentService, transport),
    system: createClient(SystemService, transport), artifacts: createClient(ArtifactService, transport),
  }
  const client = new AOPClient()
  let closed = false
  const assertActive = () => {
    if (closed || !auth.isCurrent(generation)) throw new Error('Connection is inactive')
  }
  // Plugins may use the shared client but cannot reopen it after Hub disposal.
  // Captured subscription/listener disposers still release the physical client.
  const aop = {
    get connected() { return !closed && auth.isCurrent(generation) && client.connected },
    async connect() { assertActive(); await client.connect(); assertActive() },
    request: (async (...args) => { assertActive(); const value = await client.request(...args); assertActive(); return value }) as AOPClient['request'],
    send: ((...args) => { assertActive(); return client.send(...args) }) as AOPClient['send'],
    subscribe: ((schema, value, receive, options) => {
      assertActive()
      return client.subscribe(schema, value, (payload, envelope) => {
        if (!closed && auth.isCurrent(generation)) receive(payload, envelope)
      }, options)
    }) as AOPClient['subscribe'],
    onConnectionChange(listener: (connected: boolean) => void) { assertActive(); return client.onConnectionChange(listener) },
  }
  const codecs = new Set<string>()
  const register = (schema: DescMessage) => {
    assertActive()
    if (codecs.has(schema.typeName)) return
    client.register(schema)
    codecs.add(schema.typeName)
  }
  return {
    generation, rpc, aop, register,
    async fetch(input: RequestInfo | URL, init?: RequestInit) {
      if (closed || !auth.isCurrent(generation)) throw new Error('Connection is inactive')
      const response = await fetch(input, { ...init, signal: init?.signal
        ? AbortSignal.any([init.signal, controller.signal]) : controller.signal })
      if (response.status === 401) auth.invalidate(generation)
      if (closed || !auth.isCurrent(generation)) throw new Error('Connection is inactive')
      return response
    },
    close() { if (closed) return; closed = true; controller.abort(); client.close(); codecs.clear() },
  }
}
export type ConnectionService = ReturnType<typeof createConnection>
