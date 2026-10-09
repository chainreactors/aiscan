import type { ServerStatus } from '../api'
import type { useChatSession } from '../hooks/useChatSession'
import type { IOAConsoleTarget } from '../lib/ioa-navigation'
import { Store } from './store'

export type WorkbenchSnapshot = {
  chat: Readonly<ReturnType<typeof useChatSession>>
  serverStatus: ServerStatus | null
  capabilityIDs: readonly string[]
  ioaEnabled: boolean
  observationCount: number
  assetCount: number
  refreshStatus: () => Promise<void>
  refreshAssets: () => Promise<void>
  sendToChat: (text: string) => void
}
export class WorkbenchService extends Store<WorkbenchSnapshot | null> {
  constructor() { super(null) }
  publish(snapshot: WorkbenchSnapshot) { this.set(snapshot) }
}
export type PanelArgs = { section?: 'llm' | 'jev'; focusNodeID?: string; target?: IOAConsoleTarget }
export type PanelSelection = { id: string; args: PanelArgs } | null
export class PanelNavigation extends Store<PanelSelection> {
  constructor() { super(null) }
  open = (id: string, args: PanelArgs = {}) => { this.set({ id, args }) }
  toggle = (id: string, args: PanelArgs = {}) => { this.getSnapshot()?.id === id ? this.close() : this.open(id, args) }
  close = () => { this.set(null) }
  remove(id: string) { if (this.getSnapshot()?.id === id) this.close() }
}
