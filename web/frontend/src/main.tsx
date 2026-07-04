import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import './i18n'
import '@fontsource-variable/inter'
import '@fontsource-variable/jetbrains-mono'
import '@fontsource-variable/space-grotesk'
// MiSans (Xiaomi) — the primary Chinese face: a refined modern UI sans that
// pairs with Inter and reads far more elegantly than the generic Noto fallback.
// Subsetted by unicode-range (lazy per-glyph), free for commercial use.
// Two weights give a clean hierarchy via CSS nearest-weight matching: body 400/500
// → Regular(330, optically matched to Inter 400), all emphasis 600/700 → Semibold(520).
// No clumsy heavy bold on dense small CJK. Noto Sans SC stays as the deep fallback.
// (NB: the 400–500 band rule prefers a face in [400,500] first, so we deliberately
//  omit Demibold(450) — otherwise body 400 would resolve to it and flatten the
//  body/heading weight contrast.)
import 'misans/lib/Normal/MiSans-Regular.min.css'
import 'misans/lib/Normal/MiSans-Semibold.min.css'
import { registerChatExtensions } from './lib/chat-extensions'
import ErrorBoundary from './components/ErrorBoundary'
import { ConfirmProvider } from './components/ConfirmDialog'
import './index.css'

registerChatExtensions()

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ErrorBoundary>
      <ConfirmProvider>
        <App />
      </ConfirmProvider>
    </ErrorBoundary>
  </React.StrictMode>,
)
