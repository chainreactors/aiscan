import React from 'react'
import { createRoot } from 'react-dom/client'
import { TooltipProvider } from '@cyber/ui'
import { create } from '@bufbuild/protobuf'
import { EventSchema, AOPProtocolMessageSchema } from '@cyber/aop'
import { Context } from 'cordis'
import { AppRuntime } from '../../src/runtime/app-runtime'
import { mountBrowserRuntime, unmountBrowserRuntime } from '../../src/runtime/bootstrap'
import { AuthService } from '../../src/runtime/auth'
import { CapabilityService } from '../../src/runtime/capabilities'
import { createConnection } from '../../src/runtime/connection'
import { Slots } from '../../src/runtime/slots'
import { PanelNavigation, WorkbenchService } from '../../src/runtime/workbench'
import { useObservable, SlotHost } from '../../src/runtime/react'
import { aopClient } from '../../src/api'
import { ScanAdvertisementPlugin, ScanProtocolPlugin } from '../../src/plugins/scan'
import { ScanRendererPlugin, AgentRendererPlugin, JEVRendererPlugin } from '../../src/plugins/conversation'
import { startFixtureRuntime } from './runtime'
import ChatPanel from '../../src/components/ChatPanel'
import AgentTerminal from '../../src/components/terminal/AgentTerminal'
import { withWorkflows } from '../../src/lib/workflow-view'
import '../../src/i18n'
import '../../src/index.css'

// The example plugin is a real Cordis plugin. It only knows application service
// contracts; neither App nor cyber-ui participates in its installation.
const samplePlugin = {
  name: 'sample-ui', inject: ['slots', 'panels', 'slot.shell.header.actions', 'slot.shell.panels', 'slot.conversation.extensions'], apply(ctx: any) {
    const slots = ctx.slots, panels = ctx.panels
    ctx.effect(() => slots.contribute('shell.header.actions', 'sample', () => <button onClick={() => panels.open('sample')}>Sample tool</button>, 60))
    ctx.effect(() => slots.contribute('shell.panels', 'sample', {
      mount: 'open', component: ({ selection }: any) => selection?.id === 'sample' ? <div data-testid="sample-panel">Sample panel</div> : null,
    }))
    ctx.effect(() => slots.contribute('conversation.extensions', 'sample_event', {
      renderer: () => <div data-testid="sample-event">Recorded sample</div>, mark: { label: 'Sample' },
    }))
  },
}
function SampleHost({ services }: any) {
  const selection = useObservable(services.panels)
  const revision = useObservable(services.slots.revision)
  const workbench = { chat: { activeSessionID: 'archive' } } as any
  return <React.StrictMode><TooltipProvider>
    <SlotHost slots={services.slots} name="shell.header.actions" view={{ workbench, panels: services.panels, selection }} />
    <SlotHost slots={services.slots} name="shell.panels" view={{ workbench, panels: services.panels, selection }} />
    <div className="h-[650px]"><ChatPanel resolveExtension={services.slots.resolve} extensionRevision={revision as number}
      timeline={[{ id: 'sample', kind: 'agent_joined', timestamp: Date.now(), agentName: 'Archive agent' }] as any}
      aopEvents={[create(EventSchema, { id: 'recorded-sample', sessionId: 'archive', emitter: 'plugin', payload: { case: 'extension', value: { typeUrl: 'sample_event' } } })]} guardrailReviews={[]} onResolveGuardrail={async () => {}} isThinking={false} isBusy={false} canPause={false}
      error="" hasActiveSession activeSessionID="archive" onSend={async () => true} ensureSession={async () => 'archive'} onPause={() => {}} onClearError={() => {}} />
    </div>
  </TooltipProvider></React.StrictMode>
}
const TerminalFixtureUiPlugin = {
  name: 'terminal-fixture-ui', inject: ['slots'], apply(ctx: any) {
    const slots = ctx.slots
    ctx.effect(() => slots.declare(ctx, 'root'))
    ctx.effect(() => slots.contribute('root', 'terminal', () => <TooltipProvider>
      <div className="h-[600px]"><AgentTerminal agent={{ hello: { nodeId: 'fixture-node' } } as any} /></div>
    </TooltipProvider>))
  },
}
Object.assign(window, { runtimeFixture: {
  Context, AppRuntime, mountBrowserRuntime, unmountBrowserRuntime, AuthService, CapabilityService, createConnection, Slots, PanelNavigation, WorkbenchService,
  ScanAdvertisementPlugin, ScanProtocolPlugin, ScanRendererPlugin, AgentRendererPlugin, JEVRendererPlugin, startFixtureRuntime,
  samplePlugin, TerminalFixtureUiPlugin, aopClient, AOPProtocolMessageSchema, create, withWorkflows,
  mountSample(services: any) { const root = createRoot(document.getElementById('root')!); root.render(<SampleHost services={services} />); return root },
} })
