import type { Plugin } from 'cordis'
import { ShellUiPlugin } from './shell'
import { ScanAdvertisementPlugin, ScanProtocolPlugin } from './scan'
import { ApplicationProtocolPlugin } from './protocol'
import { AssetsUiPlugin, ToolsUiPlugin, AgentsUiPlugin, SettingsUiPlugin, ObservabilityUiPlugin, ReflexUiPlugin, IOAUiPlugin } from './panels'
import { ScanRendererPlugin, AgentRendererPlugin, JEVRendererPlugin } from './conversation'

// Add a plugin here to contribute application UI. App and cyber-ui need no edits.
export const frontendPlugins: readonly Plugin[] = [
  ApplicationProtocolPlugin, ShellUiPlugin, ReflexUiPlugin, ObservabilityUiPlugin, IOAUiPlugin, AgentsUiPlugin, AssetsUiPlugin, ToolsUiPlugin, SettingsUiPlugin,
  ScanAdvertisementPlugin, ScanProtocolPlugin, ScanRendererPlugin, AgentRendererPlugin, JEVRendererPlugin,
]
