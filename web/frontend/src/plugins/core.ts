import { bindAuth, bindConnection } from '../runtime/api-bridge'
import type { AuthService } from '../runtime/auth'
import type { AppPlugin, HubServices } from '../runtime/services'

export const AuthPlugin: AppPlugin<AuthService> = {
  name: 'auth',
  apply(ctx, auth) {
    ctx.provide('auth', auth)
    ctx.effect(() => bindAuth(auth))
  },
}
export const ConnectionPlugin: AppPlugin<HubServices> = {
  name: 'connection',
  apply(ctx, services) { ctx.provide('connection', services.connection) },
}
export const LegacyApiBridgePlugin: AppPlugin = {
  name: 'legacy-api-bridge', inject: ['connection'],
  apply(ctx) { ctx.effect(() => bindConnection(ctx.connection)) },
}
export const CapabilityPlugin: AppPlugin<HubServices> = {
  name: 'capabilities', inject: ['connection'],
  apply(ctx, services) {
    ctx.provide('capabilities', services.capabilities)
    ctx.effect(() => {
      void services.capabilities.refresh()
      return () => services.capabilities.dispose()
    })
  },
}
export const WorkbenchBridgePlugin: AppPlugin<HubServices> = {
  name: 'workbench-bridge', apply(ctx, services) { ctx.provide('workbench', services.workbench) },
}
export const PanelNavigationPlugin: AppPlugin<HubServices> = {
  name: 'panel-navigation', apply(ctx, services) { ctx.provide('panels', services.panels) },
}
export const SlotsPlugin: AppPlugin<HubServices> = {
  name: 'slots', inject: ['panels'], apply(ctx, services) {
    const panels = ctx.panels
    ctx.provide('slots', services.slots)
    ctx.effect(() => services.slots.observe('shell.panels').subscribe(() => {
      const selected = panels.getSnapshot()
      if (selected && !services.slots.observe('shell.panels').getSnapshot().some(entry => entry.id === selected.id)) panels.close()
    }))
  },
}
