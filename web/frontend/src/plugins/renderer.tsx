import React from 'react'
import { createRoot } from 'react-dom/client'
import { useTranslation } from 'react-i18next'
import { Button, ConfirmProvider } from '@cyber/ui'
import AuthGate from '../components/AuthGate'
import ErrorBoundary from '../components/ErrorBoundary'
import { RootSlot, useObservable } from '../runtime/react'
import type { Store } from '../runtime/store'
import type { AppPlugin, HubServices } from '../runtime/services'
import type { AuthService } from '../runtime/auth'

function LocalizedConfirmProvider({ children }: { children: React.ReactNode }) {
  const { t } = useTranslation('app')
  return <ConfirmProvider labels={{ title: t('confirmTitle'), confirm: t('confirm'), cancel: t('cancel'), close: t('closeDialog') }}>{children}</ConfirmProvider>
}
type RendererConfig = {
  container: HTMLElement; hub: Store<HubServices | null>; errors: Store<readonly string[]>
  retry: () => Promise<void>
}
function BrowserView({ auth, hub, errors, retry }: Omit<RendererConfig, 'container'> & { auth: AuthService }) {
  const { t } = useTranslation('app')
  const services = useObservable(hub)
  const failures = useObservable(errors)
  const authentication = useObservable(auth)
  if (!services && failures.length && authentication.state === 'authenticated') return (
    <div role="alert" className="aspect-theme-root flex min-h-[100dvh] flex-col items-center justify-center gap-4 bg-background p-6 text-center text-foreground">
      <h1 className="text-lg font-semibold">{t('runtimeStartFailed')}</h1>
      <p className="text-sm text-muted-foreground">{t('runtimeStartFailedHint')}</p>
      <Button onClick={() => { void retry() }}>{t('crashReset')}</Button>
    </div>
  )
  return <ErrorBoundary><LocalizedConfirmProvider><AuthGate auth={auth} ready={services !== null}>
    {services && <RootSlot slots={services.slots} />}
  </AuthGate></LocalizedConfirmProvider></ErrorBoundary>
}
export const ReactRendererPlugin: AppPlugin<RendererConfig> = {
  name: 'react-renderer', inject: ['auth'],
  apply(ctx, { container, hub, errors, retry }) {
    const auth = ctx.auth
    ctx.effect(() => {
      const root = createRoot(container)
      root.render(<React.StrictMode><BrowserView auth={auth} hub={hub} errors={errors} retry={retry} /></React.StrictMode>)
      return () => { root.unmount() }
    })
  },
}
