import type { AppRuntime } from './app-runtime'

declare global {
  interface Window {
    __CYBER_APP_RUNTIME__?: AppRuntime
    __CYBER_RUNTIME_TRANSITION__?: Promise<void>
  }
}

export function mountBrowserRuntime(runtime: AppRuntime, container: HTMLElement, host: Window = window) {
  const previous = host.__CYBER_APP_RUNTIME__
  host.__CYBER_APP_RUNTIME__ = runtime
  // Several replacements may arrive while a previous start/dispose is pending.
  // Superseded runtimes never start; all disposal finishes before the next root.
  host.__CYBER_RUNTIME_TRANSITION__ = (host.__CYBER_RUNTIME_TRANSITION__ ?? Promise.resolve())
    .catch(error => { console.error('Frontend runtime transition failed', error) })
    .then(async () => {
      await previous?.dispose()
      if (host.__CYBER_APP_RUNTIME__ === runtime) await runtime.start(container)
    })
  return host.__CYBER_RUNTIME_TRANSITION__
}

export function unmountBrowserRuntime(runtime: AppRuntime, host: Window = window) {
  return host.__CYBER_RUNTIME_TRANSITION__ = (host.__CYBER_RUNTIME_TRANSITION__ ?? Promise.resolve())
    .catch(error => { console.error('Frontend runtime transition failed', error) })
    .then(() => runtime.dispose())
}
