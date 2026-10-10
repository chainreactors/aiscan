import { Fragment, useSyncExternalStore, type ComponentType } from 'react'
import type { Observable } from './store'
import type { HubServices } from './services'
import type { Slots, PanelViewProps } from './slots'
import { ContributionBoundary } from './contribution-boundary'

export function useObservable<T>(observable: Observable<T>): T {
  return useSyncExternalStore(observable.subscribe, observable.getSnapshot, observable.getSnapshot)
}
export function SlotHost({ slots, name, view }: { slots: Slots; name: 'shell.header.actions' | 'shell.panels'; view: PanelViewProps }) {
  const entries = useObservable(slots.observe(name))
  return <>{entries.map(entry => {
    if (name === 'shell.header.actions') {
      const Component = entry.value as ComponentType<PanelViewProps>
      return <ContributionBoundary key={entry.id} id={entry.id} resetKey={entry.value}><Component {...view} /></ContributionBoundary>
    }
    const panel = entry.value as import('./slots').SlotTypes['shell.panels']
    if (panel.mount === 'open' && view.selection?.id !== entry.id) return null
    const key = panel.mount === 'session' ? `${entry.id}:${view.workbench.chat.activeSessionID || 'none'}` : entry.id
    return <ContributionBoundary key={key} id={entry.id} resetKey={panel} visible={view.selection?.id === entry.id}><panel.component {...view} /></ContributionBoundary>
  })}</>
}
export function WorkbenchSlots({ services, name }: { services: HubServices; name: 'shell.header.actions' | 'shell.panels' }) {
  const workbench = useObservable(services.workbench)
  const selection = useObservable(services.panels)
  if (!workbench) return null
  return <SlotHost slots={services.slots} name={name} view={{ workbench, panels: services.panels, selection }} />
}
export function RootSlot({ slots }: { slots: Slots }) {
  const entries = useObservable(slots.observe('root'))
  return <>{entries.map(({ id, value: Component }) => <Fragment key={id}><Component /></Fragment>)}</>
}
