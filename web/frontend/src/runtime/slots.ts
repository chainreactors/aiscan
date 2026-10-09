import type { ComponentType } from 'react'
import type { Context } from 'cordis'
import type { TimelineRendererConfig } from '../viewer'
import type { PanelNavigation, PanelSelection, WorkbenchSnapshot } from './workbench'
import { Store, type Disposer } from './store'

export type PanelViewProps = { workbench: WorkbenchSnapshot; panels: PanelNavigation; selection: PanelSelection }
export type SlotTypes = {
  root: ComponentType
  'shell.header.actions': ComponentType<PanelViewProps>
  'shell.panels': { component: ComponentType<PanelViewProps>; mount: 'persistent' | 'session' | 'open' }
  'conversation.extensions': TimelineRendererConfig
}
export type SlotName = keyof SlotTypes
export const slotServices = {
  root: 'slot.root',
  'shell.header.actions': 'slot.shell.header.actions',
  'shell.panels': 'slot.shell.panels',
  'conversation.extensions': 'slot.conversation.extensions',
} as const satisfies Record<SlotName, string>
export type Contribution<K extends SlotName> = { id: string; order: number; value: SlotTypes[K] }
type SlotState<K extends SlotName> = {
  declared: boolean; entries: Map<string, Contribution<K>>; store: Store<readonly Contribution<K>[]>
}

// Each declaration provides a Cordis service. Contribution plugins inject their
// host services, so revocation runs their disposers before a host can return.
export class Slots {
  readonly revision = new Store(0)
  private states = new Map<SlotName, SlotState<any>>()
  private state<K extends SlotName>(name: K): SlotState<K> {
    let state = this.states.get(name)
    if (!state) { state = { declared: false, entries: new Map(), store: new Store([]) }; this.states.set(name, state) }
    return state
  }
  private publish<K extends SlotName>(name: K) {
    const state = this.state(name)
    const entries = state.declared ? [...state.entries.values()].sort((a, b) => a.order - b.order || a.id.localeCompare(b.id)) : []
    state.store.set(entries)
    this.revision.set(this.revision.getSnapshot() + 1)
  }
  declare<K extends SlotName>(ctx: Context, name: K): () => Promise<void> {
    const state = this.state(name)
    if (state.declared) throw new Error(`Slot ${name} is already declared`)
    const revoke = ctx.provide(slotServices[name], true)
    state.declared = true
    this.publish(name)
    let disposed = false
    return async () => {
      if (disposed) return
      disposed = true
      state.declared = false
      this.publish(name)
      await revoke()
    }
  }
  contribute<K extends SlotName>(name: K, id: string, value: SlotTypes[K], order = 0): Disposer {
    const state = this.state(name)
    if (state.entries.has(id) || name === 'root' && state.entries.size) throw new Error(`Duplicate contribution ${name}:${id}`)
    state.entries.set(id, { id, order, value })
    this.publish(name)
    let disposed = false
    return () => { if (disposed) return; disposed = true; state.entries.delete(id); this.publish(name) }
  }
  observe<K extends SlotName>(name: K) { return this.state(name).store }
  resolve = (extensionType: string) => this.state('conversation.extensions').declared
    ? this.state('conversation.extensions').entries.get(extensionType)?.value : undefined
}
