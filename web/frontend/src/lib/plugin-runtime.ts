export type CapabilityManifest = {
  product: string
  version?: string
  profiles?: Array<{ id: string; title?: string }>
  capabilities: Array<{
    id: string
    title?: string
    version?: string
    api_routes?: string[]
    aop_types?: string[]
    ui_contributions?: string[]
  }>
}

export type WebPluginContext = {
  manifest: CapabilityManifest
  has: (capability: string) => boolean
  contribute: (slot: string, value: unknown) => () => void
}

export type WebPlugin = {
  id: string
  requires?: string[]
  apply: (context: WebPluginContext) => void | (() => void)
}

// A small Cordis-shaped runtime for the browser. Plugins own every effect they
// install and return a disposer; the host only decides which server-declared
// capabilities are available. Product packages can add plugins without making
// the shell import their views or protobufs.
export class WebPluginRuntime {
  private readonly contributions = new Map<string, Set<unknown>>()
  private readonly disposers: Array<() => void> = []
  private mounted = false

  constructor(readonly manifest: CapabilityManifest) {}

  has(capability: string): boolean {
    return this.manifest.capabilities.some(item => item.id === capability)
  }

  contribute(slot: string, value: unknown): () => void {
    let values = this.contributions.get(slot)
    if (!values) {
      values = new Set()
      this.contributions.set(slot, values)
    }
    values.add(value)
    return () => values?.delete(value)
  }

  values<T>(slot: string): T[] {
    return [...(this.contributions.get(slot) ?? [])] as T[]
  }

  mount(plugins: readonly WebPlugin[]): void {
    if (this.mounted) return
    this.mounted = true
    for (const plugin of plugins) {
      if (plugin.requires?.some(capability => !this.has(capability))) continue
      const disposer = plugin.apply({ manifest: this.manifest, has: this.has.bind(this), contribute: this.contribute.bind(this) })
      if (typeof disposer === 'function') this.disposers.push(disposer)
    }
  }

  dispose(): void {
    for (const dispose of this.disposers.splice(0).reverse()) dispose()
    this.contributions.clear()
    this.mounted = false
  }
}

export async function loadCapabilityManifest(signal?: AbortSignal): Promise<CapabilityManifest> {
  const response = await fetch('/api/manifest', { signal, cache: 'no-store' })
  if (!response.ok) throw new Error(`Failed to load capability manifest (${response.status})`)
  const value = await response.json() as Partial<CapabilityManifest>
  if (typeof value.product !== 'string' || !Array.isArray(value.capabilities)) throw new Error('Invalid capability manifest')
  return {
    product: value.product,
    version: value.version,
    profiles: Array.isArray(value.profiles)
      ? value.profiles.filter(item => typeof item?.id === 'string')
      : undefined,
    capabilities: value.capabilities.filter(item => typeof item?.id === 'string'),
  }
}

export function capabilityPlugin(id: string, requires: string[], apply: WebPlugin['apply']): WebPlugin {
  return { id, requires, apply }
}
