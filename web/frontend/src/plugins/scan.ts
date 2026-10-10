import type { Context } from 'cordis'
import type { AppPlugin } from '../runtime/services'
import { ScanProtocolMessageSchema } from '../cyber-proto'

export const ScanAdvertisementPlugin: AppPlugin = {
  name: 'scan-discovery', inject: ['capabilities'],
  apply(ctx) {
    const capabilities = ctx.capabilities
    ctx.effect(() => {
      let advertisement: ReturnType<Context['plugin']> | undefined
      let stopped = false
      let transition = Promise.resolve()
      const reconcile = async () => {
        if (stopped) return
        const advertised = capabilities.getSnapshot().advertised.includes('scan')
        if (advertised && !advertisement) {
          advertisement = ctx.plugin({ name: 'scan-advertisement', apply(scope) { scope.provide('scanAdvertised', true) } })
          await advertisement
        }
        if (!advertised && advertisement) { await advertisement.dispose(); advertisement = undefined }
      }
      const schedule = () => { transition = transition.then(reconcile).catch(error => ctx.logger.error(error)) }
      const unsubscribe = capabilities.subscribe(schedule)
      schedule()
      return async () => { stopped = true; unsubscribe(); await transition; await advertisement?.dispose() }
    })
  },
}

export const ScanProtocolPlugin: AppPlugin = {
  name: 'scan-protocol', inject: ['connection', 'scanAdvertised'],
  apply(ctx) {
    // AOPClient has no unregister. Keep the codec once per connection, while
    // Cordis withdraws this service and its consumers when advertising stops.
    ctx.connection.register(ScanProtocolMessageSchema)
    ctx.provide('scanProtocol', { connection: ctx.connection })
  },
}
