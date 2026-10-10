import { lazy, Suspense, type ComponentType, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Activity, Box, CircuitBoard, Monitor, Network, Settings, Wrench } from 'lucide-react'
import { Button, Tooltip, TooltipContent, TooltipTrigger } from '@cyber/ui'
import { cn } from '@cyber/theme'
import type { AppPlugin } from '../runtime/services'
import type { PanelViewProps } from '../runtime/slots'
import ConfigPanel from '../components/ConfigPanel'
import AgentPanel from '../components/AgentPanel'
import ObservabilityPanel from '../components/ObservabilityPanel'
import ReflexPanel from '../components/ReflexPanel'
import AssetPanel from '../components/AssetPanel'
import ToolRegistryPanel from '../components/ToolRegistryPanel'
const IOAConsole = lazy(() => import('../components/IOAConsole'))

function ReflexAction({ panels, selection }: PanelViewProps) {
  const { t } = useTranslation('app')
  return <HeaderIconButton label="Reflex" guideTarget="reflex" active={selection?.id === 'reflex'} toolDrawerTrigger onClick={() => panels.toggle('reflex')}><CircuitBoard className="h-3.5 w-3.5" /></HeaderIconButton>
}
function ObservabilityAction({ panels, selection, workbench }: PanelViewProps) {
  const { t } = useTranslation('app'), { t: to } = useTranslation('observe')
  return <span className="relative"><HeaderIconButton label={to('open', { count: workbench.observationCount })} guideTarget="observations" active={selection?.id === 'observability'} toolDrawerTrigger onClick={() => panels.toggle('observability')}><Activity className="h-3.5 w-3.5" /></HeaderIconButton>
    {workbench.observationCount > 0 && <span className="pointer-events-none absolute bottom-0 right-0 h-1.5 w-1.5 rounded-full bg-primary" />}</span>
}
function AgentsAction({ panels, selection, workbench }: PanelViewProps) {
  return <AgentsButton count={workbench.chat.agents.length} open={selection?.id === 'agents'} onClick={() => panels.toggle('agents')} />
}
function SettingsAction({ panels, selection }: PanelViewProps) {
  const { t } = useTranslation('app')
  return <><span className="mx-0.5 hidden h-5 w-px shrink-0 bg-border/70 sm:block" aria-hidden="true" /><HeaderIconButton label={t('openSettings')} guideTarget="settings" active={selection?.id === 'settings'} toolDrawerTrigger onClick={() => panels.toggle('settings', { section: 'llm' })}><Settings className="h-3.5 w-3.5" /></HeaderIconButton></>
}
function IOAAction({ panels, selection }: PanelViewProps) {
  return <IOAConsoleButton open={selection?.id === 'ioa'} onClick={() => panels.toggle('ioa')} />
}
function AgentsView({ panels, selection, workbench }: PanelViewProps) {
  return <AgentPanel open={selection?.id === 'agents'} agents={workbench.chat.agents} focusNodeID={selection?.id === 'agents' ? selection.args.focusNodeID : undefined} onClose={panels.close} />
}
function SettingsView({ panels, selection, workbench }: PanelViewProps) {
  return <ConfigPanel open={selection?.id === 'settings'} status={workbench.serverStatus} capabilities={[...workbench.capabilityIDs]} initialSection={selection?.id === 'settings' ? selection.args.section ?? 'llm' : 'llm'} onClose={panels.close} onSaved={() => { void workbench.refreshStatus() }} />
}
function ObservabilityView({ panels, selection, workbench }: PanelViewProps) {
  return <ObservabilityPanel open={selection?.id === 'observability'} events={workbench.chat.aopEvents} sessionID={workbench.chat.activeSessionID} assetCount={workbench.assetCount} onClose={panels.close} onSendToChat={workbench.sendToChat} onAssetsChanged={workbench.refreshAssets} />
}
function ReflexView({ panels, selection, workbench }: PanelViewProps) {
  return <ReflexPanel open={selection?.id === 'reflex'} sessionID={workbench.chat.activeSessionID} events={workbench.chat.aopEvents} onClose={panels.close} />
}
function IOAView({ panels, selection }: PanelViewProps) {
  return <Suspense fallback={null}><IOAConsole open initialSpaceID={selection?.args.target?.spaceID} initialMessageID={selection?.args.target?.messageID} onClose={panels.close} /></Suspense>
}

function panelPlugin(id: string, order: number, action: ComponentType<PanelViewProps>, component: ComponentType<PanelViewProps>, mount: 'persistent' | 'session' | 'open'): AppPlugin {
  return { name: `${id}-ui`, inject: ['slots', 'panels', 'slot.shell.header.actions', 'slot.shell.panels'], apply(ctx) {
    const slots = ctx.slots, panels = ctx.panels
    ctx.effect(() => slots.contribute('shell.header.actions', id, action, order))
    ctx.effect(() => {
      const remove = slots.contribute('shell.panels', id, { component, mount }, order)
      return () => { remove(); panels.remove(id) }
    })
  } }
}
export const ReflexUiPlugin = panelPlugin('reflex', 10, ReflexAction, ReflexView, 'session')
export const ObservabilityUiPlugin = panelPlugin('observability', 20, ObservabilityAction, ObservabilityView, 'session')
export const AgentsUiPlugin = panelPlugin('agents', 40, AgentsAction, AgentsView, 'persistent')
export const SettingsUiPlugin = panelPlugin('settings', 50, SettingsAction, SettingsView, 'persistent')
export const IOAUiPlugin: AppPlugin = {
  name: 'ioa-ui', inject: ['slots', 'workbench', 'panels', 'slot.shell.header.actions', 'slot.shell.panels'], apply(ctx) {
    const slots = ctx.slots, workbench = ctx.workbench, panels = ctx.panels
    ctx.effect(() => {
      let remove: (() => void) | undefined
      const reconcile = () => {
        if (workbench.getSnapshot()?.ioaEnabled && !remove) {
          const removeAction = slots.contribute('shell.header.actions', 'ioa', IOAAction, 30)
          try {
            const removePanel = slots.contribute('shell.panels', 'ioa', { component: IOAView, mount: 'open' }, 30)
            remove = () => { removeAction(); removePanel(); panels.remove('ioa') }
          } catch (error) { removeAction(); throw error }
        } else if (!workbench.getSnapshot()?.ioaEnabled && remove) { remove(); remove = undefined }
      }
      reconcile()
      const unsubscribe = workbench.subscribe(reconcile)
      return () => { unsubscribe(); remove?.() }
    })
  },
}

function AgentsButton({ count, open, onClick }: { count: number; open: boolean; onClick: () => void }) {
  const { t } = useTranslation('app')
  const active = count > 0
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="xs"
          active={open}
          data-tool-drawer-trigger
          onClick={onClick}
          aria-label={active ? t('agentsConnected', { count }) : t('noAgents')}
          data-ui-guide="agents"
          className={cn(
            'h-7 w-7 shrink-0 cursor-pointer gap-0 rounded-md border px-0 hover:opacity-80 sm:w-auto sm:gap-1.5 sm:px-2.5',
            // A connection count is neutral status, not an alert — keep warm hues for
            // severity only. Blue when connected, quiet neutral when none.
            active
              ? 'border-primary/30'
              : 'border-border bg-secondary/50 text-muted-foreground hover:bg-secondary/50 hover:text-muted-foreground',
          )}
        >
          <Monitor className="h-3 w-3" aria-hidden="true" />
          <span className="hidden font-mono sm:inline" aria-hidden="true">{count}</span>
        </Button>
      </TooltipTrigger>
      <TooltipContent>{active ? t('agentsConnected', { count }) : t('noAgents')}</TooltipContent>
    </Tooltip>
  )
}

function IOAConsoleButton({ open, onClick }: { open: boolean; onClick: () => void }) {
  const { t } = useTranslation('ioa')
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="xs"
          active={open}
          data-tool-drawer-trigger
          onClick={onClick}
          aria-label={t('openConsole')}
          data-ui-guide="ioa"
          className="h-7 w-7 shrink-0 cursor-pointer gap-0 rounded-md border border-border bg-secondary/50 px-0 text-muted-foreground hover:border-primary/30 hover:bg-primary/10 hover:text-primary sm:w-auto sm:gap-1.5 sm:px-2.5"
        >
          <Network className="h-3 w-3" aria-hidden="true" />
          <span className="hidden font-mono text-xs font-semibold sm:inline" aria-hidden="true">IOA</span>
        </Button>
      </TooltipTrigger>
      <TooltipContent>{t('openConsole')}</TooltipContent>
    </Tooltip>
  )
}

function HeaderIconButton({ children, label, hint, guideTarget, onClick, active, toolDrawerTrigger }: { children: ReactNode; label: string; hint?: string; guideTarget?: string; onClick: () => void; active?: boolean; toolDrawerTrigger?: boolean }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          active={active}
          data-tool-drawer-trigger={toolDrawerTrigger ? '' : undefined}
          aria-label={label}
          data-ui-guide={guideTarget}
          onClick={onClick}
          className={cn('h-6 w-6 shrink-0 rounded-lg hover:text-foreground sm:h-7 sm:w-7', !active && 'text-muted-foreground')}
        >
          {children}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{hint || label}</TooltipContent>
    </Tooltip>
  )
}


function AssetsAction({ panels, selection, workbench }: PanelViewProps) {
  return <AssetPoolButton count={workbench.assetCount} open={selection?.id === 'assets'} onClick={() => panels.toggle('assets')} />
}
function AssetsView({ panels, selection, workbench }: PanelViewProps) {
  return <AssetPanel open={selection?.id === 'assets'} onClose={panels.close} onSendToChat={workbench.sendToChat} onChanged={workbench.refreshAssets} />
}
function ToolsAction({ panels, selection, workbench }: PanelViewProps) {
  const count = workbench.chat.agents.reduce((sum, agent) => sum + agent.commands.filter(command => command.name.startsWith('!')).length, 0)
  return <ToolsButton count={count} open={selection?.id === 'tools'} onClick={() => panels.toggle('tools')} />
}
function ToolsView({ panels, selection, workbench }: PanelViewProps) {
  return <ToolRegistryPanel open={selection?.id === 'tools'} agents={workbench.chat.agents} onClose={panels.close} />
}
// Retain the published workbench's asset and tool navigation as contributions.
export const AssetsUiPlugin = panelPlugin('assets', 25, AssetsAction, AssetsView, 'persistent')
export const ToolsUiPlugin = panelPlugin('tools', 45, ToolsAction, ToolsView, 'persistent')
function AssetPoolButton({ count, open, onClick }: { count: number; open: boolean; onClick: () => void }) {
  const { t } = useTranslation('assets')
  const active = count > 0
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="xs"
          active={open}
          data-tool-drawer-trigger
          onClick={onClick}
          aria-label={t('openAssets')}
          data-ui-guide="assets"
          className={cn(
            'h-7 w-7 shrink-0 cursor-pointer gap-0 rounded-md border px-0 hover:opacity-80 sm:w-auto sm:gap-1.5 sm:px-2.5',
            active
              ? 'border-primary/30'
              : 'border-border bg-secondary/50 text-muted-foreground hover:bg-secondary/50 hover:text-muted-foreground',
          )}
        >
          <Box className="h-3 w-3" aria-hidden="true" />
          <span className="hidden font-mono sm:inline" aria-hidden="true">{count}</span>
        </Button>
      </TooltipTrigger>
      <TooltipContent>{t('openAssets')}</TooltipContent>
    </Tooltip>
  )
}

function ToolsButton({ count, open, onClick }: { count: number; open: boolean; onClick: () => void }) {
  const { t } = useTranslation('tools')
  const active = count > 0
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="xs"
          active={open}
          data-tool-drawer-trigger
          onClick={onClick}
          aria-label={active ? t('toolsAvailable', { count }) : t('noTools')}
          data-ui-guide="tools"
          className={cn(
            'h-7 w-7 shrink-0 cursor-pointer gap-0 rounded-md border px-0 hover:opacity-80 sm:w-auto sm:gap-1.5 sm:px-2.5',
            active
              ? 'border-primary/30'
              : 'border-border bg-secondary/50 text-muted-foreground hover:bg-secondary/50 hover:text-muted-foreground',
          )}
        >
          <Wrench className="h-3 w-3" aria-hidden="true" />
          <span className="hidden font-mono sm:inline" aria-hidden="true">{count}</span>
        </Button>
      </TooltipTrigger>
      <TooltipContent>{t('openTools')}</TooltipContent>
    </Tooltip>
  )
}
