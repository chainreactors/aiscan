import type { AuthService } from './auth'
import type { Context, Plugin } from 'cordis'
import { createConnection, type ConnectionService } from './connection'
import { CapabilityService } from './capabilities'
import { PanelNavigation, WorkbenchService } from './workbench'
import { Slots, slotServices } from './slots'

export type HubServices = { connection: ConnectionService; capabilities: CapabilityService; workbench: WorkbenchService; panels: PanelNavigation; slots: Slots }
// Application services remain an explicit contract, independent of Cordis'
// internal declaration paths and the package manager's dependency layout.
export interface AppContext extends Context {
  auth: AuthService
  connection: ConnectionService
  capabilities: CapabilityService
  workbench: WorkbenchService
  panels: PanelNavigation
  slots: Slots
  scanAdvertised: true
  scanProtocol: { connection: ConnectionService }
  'slot.root': true
  'slot.shell.header.actions': true
  'slot.shell.panels': true
  'slot.conversation.extensions': true
}
export type AppPlugin<T = undefined> = Omit<Plugin.Object<T>, 'apply' | 'inject'> & {
  inject?: (keyof AppContext & string)[]
  apply(ctx: AppContext, config: T): unknown
}
export const hubServiceKeys = ['connection', 'capabilities', 'workbench', 'panels', 'slots', 'scanAdvertised', 'scanProtocol', ...Object.values(slotServices)] as const

export function isolateHub(parent: Context): Context {
  return hubServiceKeys.reduce((ctx, key) => ctx.isolate(key), parent)
}
export function createHubServices(auth: AuthService, generation: number): HubServices {
  const connection = createConnection(auth, generation)
  return { connection, capabilities: new CapabilityService(connection), workbench: new WorkbenchService(), panels: new PanelNavigation(), slots: new Slots() }
}
