import './i18n'
import './index.css'
import { AppRuntime } from './runtime/app-runtime'
import { mountBrowserRuntime, unmountBrowserRuntime } from './runtime/bootstrap'

const runtime = new AppRuntime()
void mountBrowserRuntime(runtime, document.getElementById('root')!)
  .catch(error => { console.error('Frontend runtime failed to start', error) })

if (import.meta.hot) {
  import.meta.hot.accept()
  import.meta.hot.dispose(() => { void unmountBrowserRuntime(runtime).catch(error => console.error('Frontend runtime failed to dispose', error)) })
}
