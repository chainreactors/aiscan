import { startFixtureRuntime } from './runtime'
import React from 'react'
import { createRoot } from 'react-dom/client'
import '../../src/i18n'
import { aopClient } from '../../src/api'
import { useChatSession } from '../../src/hooks/useChatSession'

const runtime = await startFixtureRuntime()
;(aopClient as any).subscribe = () => () => {}
function Fixture() {
  const session = useChatSession()
  ;(window as any).session = session
  return <div data-testid="scan-ids">{session.timeline.filter(item => item.kind === 'scan_complete').map(item => item.scanID).join(',')}</div>
}
createRoot(document.getElementById('root')!).render(<Fixture />)
