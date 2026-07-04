import type { AgentInfo, PoolAsset, ScanJob, ScanOptions, ScanResult } from '../api'
import OperationDeck from './deck/OperationDeck'

interface ScanWorkspaceProps {
  activeScan: ScanJob | null
  lines: string[]
  result: ScanResult | null
  scanning: boolean
  error: string
  analysisAvailable: boolean
  agents: AgentInfo[]
  llmModel?: string
  llmProvider?: string
  now: number
  onSubmit: (target: string, mode: string, options: ScanOptions) => void
  onClearError: () => void
  onCommandCortex: (text: string) => void
  onOpenNode: (agentID: string) => void
  assets: PoolAsset[]
  onAddAsset: (raw: string) => void
  onRemoveAsset: (id: string) => void
  onDispatchAgent: (target: string) => void
}

/**
 * The scan workspace is the CORTEX operation deck body: a three-column recon
 * console (rail · main · intel) built on real scan-session + agent data. The
 * SCAN/AGENT switch lives in the shared DeckTopBar above it. See [[aiscan-web-redesign-direction]].
 */
export default function ScanWorkspace({
  activeScan,
  lines,
  result,
  scanning,
  error,
  analysisAvailable,
  agents,
  llmModel,
  llmProvider,
  now,
  onSubmit,
  onClearError,
  onCommandCortex,
  onOpenNode,
  assets,
  onAddAsset,
  onRemoveAsset,
  onDispatchAgent,
}: ScanWorkspaceProps) {
  return (
    <OperationDeck
      scan={activeScan}
      result={result}
      lines={lines}
      scanning={scanning}
      error={error}
      analysisAvailable={analysisAvailable}
      agents={agents}
      llmModel={llmModel}
      llmProvider={llmProvider}
      now={now}
      onSubmit={onSubmit}
      onClearError={onClearError}
      onCommandCortex={onCommandCortex}
      onOpenNode={onOpenNode}
      assets={assets}
      onAddAsset={onAddAsset}
      onRemoveAsset={onRemoveAsset}
      onDispatchAgent={onDispatchAgent}
    />
  )
}
