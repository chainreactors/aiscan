import { startFixtureRuntime } from './runtime'
import React from 'react'
import { createRoot } from 'react-dom/client'
import { TooltipProvider } from '../../cyber-ui/packages/ui/src'
import '../../src/i18n'
import ScannerToolCall from '../../src/components/chat/ScannerToolCall'
import ScanSummaryCard from '../../src/components/chat/ScanSummaryCard'

const runtime = await startFixtureRuntime()
const original = IDBObjectStore.prototype.getAll
;(window as any).operationReads = 0
IDBObjectStore.prototype.getAll = function (...args: any[]) {
  if (this.name === 'operations') (window as any).operationReads++
  return Reflect.apply(original, this, args)
}
createRoot(document.getElementById('root')!).render(<TooltipProvider>
  <ScannerToolCall id="outer-call" toolName="scan" result="completed" />
  <ScanSummaryCard scanID="outer-call" />
</TooltipProvider>)
