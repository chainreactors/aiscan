import React from 'react'
import { createRoot } from 'react-dom/client'
import { create } from '@bufbuild/protobuf'
import { PtyProtocolMessageSchema } from '@cyber/aop'
import { TerminalView, writeTerminalData } from '@cyber/terminal'
import '../../src/index.css'

createRoot(document.getElementById('root')!).render(
  <div style={{ display: 'flex', height: 420, width: 320 }}>
    <TerminalView onReady={(terminal, fit) => {
      ;(window as any).terminalFixture = {
        terminal, readyCols: terminal.cols, measuredCols: fit.proposeDimensions()?.cols,
        write: (bytes: number[]) => writeTerminalData(terminal, create(PtyProtocolMessageSchema, {
          message: { case: 'output', value: { data: Uint8Array.from(bytes) } },
        })),
      }
    }} />
  </div>,
)
