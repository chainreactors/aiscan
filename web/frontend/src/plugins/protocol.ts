import type { AppPlugin } from '../runtime/services'
import { CommandProtocolMessageSchema, ReloadProtocolMessageSchema, GuardrailProtocolMessageSchema, JEVProtocolMessageSchema } from '../cyber-proto'

// Application namespaces belong to composition; the transport only owns codecs.
export const ApplicationProtocolPlugin: AppPlugin = {
  name: 'application-protocols', inject: ['connection'],
  apply(ctx) {
    for (const schema of [CommandProtocolMessageSchema, ReloadProtocolMessageSchema, GuardrailProtocolMessageSchema, JEVProtocolMessageSchema]) ctx.connection.register(schema)
  },
}
