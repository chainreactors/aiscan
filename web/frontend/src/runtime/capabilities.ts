import { Store } from './store'
import type { ConnectionService } from './connection'
export type CapabilityManifest = {
  product: string
  version?: string
  profiles?: Array<{ id: string; title?: string }>
  capabilities: Array<{ id: string; title?: string; version?: string; api_routes?: string[]; aop_types?: string[]; ui_contributions?: string[] }>
}
export type CapabilitySnapshot = {
  manifest: CapabilityManifest | null
  nodes: Readonly<Record<string, readonly string[]>>
  advertised: readonly string[]
  error: string | null
}
const nodeTransportCapabilities = new Set(['repl', 'pty', 'tmux', 'file', 'sco'])
export class CapabilityService extends Store<CapabilitySnapshot> {
  private stopped = false
  private timer?: ReturnType<typeof setTimeout>
  private refreshing?: Promise<void>
  private controller = new AbortController()
  constructor(private connection: ConnectionService) { super({ manifest: null, nodes: {}, advertised: [], error: null }) }
  setNodes(nodes: Readonly<Record<string, readonly string[]>>) {
    if (this.stopped) return
    if (JSON.stringify(nodes) === JSON.stringify(this.getSnapshot().nodes)) return
    this.publish({ ...this.getSnapshot(), nodes: Object.fromEntries(Object.entries(nodes).map(([id, capabilities]) => [id, [...capabilities]])) })
  }
  private publish(snapshot: CapabilitySnapshot) {
    this.set({ ...snapshot, advertised: [...new Set([
      ...(snapshot.manifest?.capabilities.map(item => item.id) ?? []),
      ...Object.values(snapshot.nodes).flat().filter(id => !nodeTransportCapabilities.has(id)),
    ])].sort() })
  }
  refresh(): Promise<void> {
    if (this.stopped) return Promise.resolve()
    return this.refreshing ??= this.load().finally(() => { this.refreshing = undefined })
  }
  private async load(): Promise<void> {
    clearTimeout(this.timer)
    try {
      const response = await this.connection.fetch('/api/manifest', { cache: 'no-store', signal: this.controller.signal })
      let manifest: CapabilityManifest
      if (response.status === 404) manifest = { product: 'cyber-harness', capabilities: [{ id: 'core' }] }
      else {
        if (!response.ok) throw new Error(`Failed to load capability manifest (${response.status})`)
        const value = await response.json() as CapabilityManifest
        if (typeof value.product !== 'string' || !Array.isArray(value.capabilities)) throw new Error('Invalid capability manifest')
        manifest = { ...value, capabilities: value.capabilities.filter(item => typeof item?.id === 'string'),
          profiles: value.profiles?.filter(item => typeof item?.id === 'string') }
      }
      if (!this.stopped) this.publish({ ...this.getSnapshot(), manifest, error: null })
    } catch (error) {
      if (this.stopped || this.controller.signal.aborted) return
      this.publish({ ...this.getSnapshot(), error: String(error) })
      this.timer = setTimeout(() => { void this.refresh() }, 3000)
    }
  }
  async dispose() {
    this.stopped = true
    clearTimeout(this.timer)
    this.controller.abort()
    await this.refreshing
  }
}
