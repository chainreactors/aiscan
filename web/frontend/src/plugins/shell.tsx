import App from '../App'
import type { AppPlugin, HubServices } from '../runtime/services'

export const ShellUiPlugin: AppPlugin = {
  name: 'shell-ui', inject: ['connection', 'slots', 'workbench', 'panels', 'capabilities'],
  apply(ctx) {
    const services: HubServices = { connection: ctx.connection, capabilities: ctx.capabilities, workbench: ctx.workbench, panels: ctx.panels, slots: ctx.slots }
    ctx.effect(() => ctx.slots.declare(ctx, 'root'))
    ctx.effect(() => ctx.slots.declare(ctx, 'shell.header.actions'))
    ctx.effect(() => ctx.slots.declare(ctx, 'shell.panels'))
    ctx.effect(() => ctx.slots.declare(ctx, 'conversation.extensions'))
    const Shell = () => <App services={services} />
    ctx.effect(() => ctx.slots.contribute('root', 'workbench', Shell))
  },
}
