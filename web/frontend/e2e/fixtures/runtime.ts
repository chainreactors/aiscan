import { Context } from 'cordis'
import { AuthService } from '../../src/runtime/auth'
import { stopAdmission } from '../../src/runtime/api-bridge'
import { createHubServices, isolateHub, type AppPlugin } from '../../src/runtime/services'
import { AuthPlugin, ConnectionPlugin, LegacyApiBridgePlugin, SlotsPlugin, PanelNavigationPlugin, WorkbenchBridgePlugin } from '../../src/plugins/core'
import { ScanRendererPlugin, AgentRendererPlugin, JEVRendererPlugin } from '../../src/plugins/conversation'
import { ApplicationProtocolPlugin } from '../../src/plugins/protocol'

// Standalone source fixtures opt into a Hub explicitly. Importing api.ts never
// creates another connection. A fixture owns its own React root and mock server.
export async function startFixtureRuntime() {
  const root = new Context()
  const owner = isolateHub(root).plugin({ name: 'fixture-hub', apply() {} })
  await owner
  const scope = owner.ctx
  const auth = new AuthService()
  auth.set({ state: 'authenticated', generation: 1 })
  const services = createHubServices(auth, 1), { connection } = services
  const fibers = []
  const authFiber = root.plugin(AuthPlugin, auth)
  await authFiber
  for (const plugin of [ConnectionPlugin, LegacyApiBridgePlugin, PanelNavigationPlugin, WorkbenchBridgePlugin, SlotsPlugin]) {
    const fiber = scope.plugin(plugin, services)
    fibers.push(fiber)
    await fiber
  }
  const DeclarationPlugin: AppPlugin = { name: 'fixture-slots', inject: ['slots'], apply(ctx) {
    const slots = ctx.slots
    for (const name of ['root', 'shell.header.actions', 'shell.panels', 'conversation.extensions'] as const) ctx.effect(() => slots.declare(ctx, name))
  } }
  const declarations = scope.plugin(DeclarationPlugin)
  fibers.push(declarations)
  await declarations
  for (const plugin of [ApplicationProtocolPlugin, ScanRendererPlugin, AgentRendererPlugin, JEVRendererPlugin]) {
    const fiber = scope.plugin(plugin)
    fibers.push(fiber)
    await fiber
  }
  let disposing: Promise<void> | undefined
  const close = () => { connection.close() }
  window.addEventListener('beforeunload', close, { once: true })
  const dispose = () => disposing ??= (async () => {
    stopAdmission(connection)
    auth.dispose()
    window.removeEventListener('beforeunload', close)
    for (const fiber of fibers.splice(0).reverse()) await fiber.dispose()
    await owner.dispose()
    await services.capabilities.dispose()
    services.workbench.set(null)
    services.panels.close()
    connection.close()
    await authFiber.dispose()
    await auth.dispose()
  })()
  return { ...services, context: scope, auth, dispose }
}
