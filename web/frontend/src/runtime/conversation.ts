import type { TimelineRendererConfig } from '../viewer'
export type ExtensionResolver = (type: string) => TimelineRendererConfig | undefined
export const emptyExtensionResolver: ExtensionResolver = () => undefined
