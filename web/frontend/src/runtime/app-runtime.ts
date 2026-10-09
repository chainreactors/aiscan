import { Context, type Fiber, type Plugin } from 'cordis'
import { flushSync } from 'react-dom'
import { AuthService } from './auth'
import { stopAdmission } from './api-bridge'
import { Store } from './store'
import { createHubServices, isolateHub, type HubServices } from './services'
import { AuthPlugin, CapabilityPlugin, ConnectionPlugin, LegacyApiBridgePlugin, PanelNavigationPlugin, SlotsPlugin, WorkbenchBridgePlugin } from '../plugins/core'
import { ReactRendererPlugin } from '../plugins/renderer'
import { frontendPlugins } from '../plugins/composition'

export class AppRuntime {
  readonly context = new Context()
  readonly auth = new AuthService()
  readonly hub = new Store<HubServices | null>(null)
  private browser?: Fiber
  private hubFiber?: Fiber
  private features: Fiber[] = []
  private core: Fiber[] = []
  private services?: HubServices
  private authFiber?: Fiber
  private rendererFiber?: Fiber
  private unsubscribe?: () => void
  private queue: Promise<void> = Promise.resolve()
  private starting?: Promise<void>
  private disposing?: Promise<void>
  private stopped = false
  readonly errors = new Store<readonly string[]>([])
  constructor(private plugins: readonly Plugin[] = frontendPlugins) {}

  start(container: HTMLElement): Promise<void> {
    if (this.stopped) return Promise.reject(new Error('Runtime has been disposed'))
    return this.starting ??= this.boot(container).catch(async error => {
      this.stopped = true
      this.errors.set([...this.errors.getSnapshot(), String(error)])
      this.auth.dispose()
      this.unsubscribe?.()
      await this.queue
      await this.disposeHub()
      await this.rendererFiber?.dispose()
      await this.authFiber?.dispose()
      await this.auth.dispose()
      await this.browser?.dispose()
      throw error
    })
  }
  private async boot(container: HTMLElement) {
    this.browser = this.context.plugin({ name: 'browser-app', apply() {} })
    await this.browser
    const ctx = this.browser.ctx
    this.authFiber = ctx.plugin(AuthPlugin, this.auth)
    await this.authFiber
    this.rendererFiber = ctx.plugin(ReactRendererPlugin, { container, hub: this.hub, errors: this.errors, retry: this.retry })
    await this.rendererFiber
    this.unsubscribe = this.auth.subscribe(() => {
      if (this.auth.getSnapshot().state !== 'authenticated') stopAdmission(this.services?.connection)
      void this.schedule()
    })
    this.auth.afterInvalidate = () => this.queue
    await this.auth.check()
    await this.queue
  }
  private schedule() {
    return this.queue = this.queue.then(() => this.reconcile()).catch(error => {
      if (!this.stopped) this.errors.set([...this.errors.getSnapshot(), String(error)])
    })
  }
  retry = () => {
    if (this.stopped) return Promise.resolve()
    this.errors.set([])
    return this.schedule()
  }
  private async reconcile() {
    const snapshot = this.auth.getSnapshot()
    if (snapshot.state === 'authenticated' && this.services?.connection.generation === snapshot.generation) return
    await this.disposeHub()
    if (this.stopped || !this.auth.isCurrent(snapshot.generation) || snapshot.state !== 'authenticated') return
    const scope = isolateHub(this.browser!.ctx)
    this.hubFiber = scope.plugin({ name: 'authenticated-hub', apply() {} })
    try {
      await this.hubFiber
      if (this.stopped || !this.auth.isCurrent(snapshot.generation)) { await this.disposeHub(); return }
      const ctx = this.hubFiber.ctx
      const services = createHubServices(this.auth, snapshot.generation)
      this.services = services
      for (const plugin of [ConnectionPlugin, LegacyApiBridgePlugin, CapabilityPlugin, WorkbenchBridgePlugin, PanelNavigationPlugin, SlotsPlugin]) {
        const fiber = ctx.plugin(plugin, services)
        this.core.push(fiber)
        await fiber
      }
      for (const plugin of this.plugins) {
        const fiber = ctx.plugin(plugin)
        this.features.push(fiber)
        await fiber
      }
      if (this.stopped || !this.auth.isCurrent(snapshot.generation)) { await this.disposeHub(); return }
      this.errors.set([])
      this.hub.set(services)
    } catch (error) { await this.disposeHub(); throw error }
  }
  private async disposeHub() {
    if (!this.services && !this.hubFiber) return
    stopAdmission(this.services?.connection)
    // Explicit sequence: unmount React consumers before removing contributions,
    // then await every feature/core disposer before closing the shared transport.
    flushSync(() => this.hub.set(null))
    for (const fiber of this.features.splice(0).reverse()) await fiber.dispose()
    for (const fiber of this.core.splice(0).reverse()) await fiber.dispose()
    // This also awaits plugins added dynamically under the Hub context.
    await this.hubFiber?.dispose()
    this.hubFiber = undefined
    this.services?.connection.close()
    this.services?.workbench.set(null)
    this.services = undefined
  }
  diagnostics() {
    return [...this.context.registry.values()].flatMap(runtime => [...runtime.fibers].map(fiber => ({
      id: fiber.uid, name: fiber.name, state: fiber.state, effects: fiber.getEffects(),
    })))
  }
  dispose(): Promise<void> {
    return this.disposing ??= (async () => {
      this.stopped = true
      this.unsubscribe?.()
      this.auth.dispose()
      stopAdmission(this.services?.connection)
      await this.starting?.catch(() => {})
      await this.queue
      // boot() may have installed the listener while dispose() was awaiting it.
      this.unsubscribe?.()
      await this.disposeHub()
      await this.rendererFiber?.dispose()
      await this.authFiber?.dispose()
      await this.auth.dispose()
      await this.browser?.dispose()
    })()
  }
}
