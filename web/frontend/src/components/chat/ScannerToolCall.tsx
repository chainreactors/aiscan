import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Loader2 } from 'lucide-react'
import { buildSCOModel, type SCONode } from '@cyber/cstx-easm'
import { Badge, Tabs, TabsContent, TabsList, TabsTrigger } from '@cyber/ui'
import { ToolResultDisplay, type ToolResultDisplayProps } from '@/viewer'
import { cstxFailures, listSCONodes, retryCSTXFailures, subscribeCSTXChanges, syncCSTXArtifacts } from '../../lib/cstx-runtime'
import { buildFindingsFromSCO } from '../../lib/scan-result'
import AssetResultView from '../AssetResultView'
import FindingsPanel from '../FindingsPanel'

export interface ScannerToolCallProps extends ToolResultDisplayProps { id: string }

export default function ScannerToolCall({
  id,
  toolName,
  toolArgs = '',
  result,
  pending = false,
  error = false,
  toolResult,
  resultEventId,
  observations,
  observationLabels,
}: ScannerToolCallProps) {
  const { t } = useTranslation('scan')
  const { t: tChat } = useTranslation('chat')
  const { t: tf } = useTranslation('findings')
  const [nodes, setNodes] = useState<SCONode[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [failure, setFailure] = useState('')
  const model = useMemo(() => buildSCOModel(nodes || []), [nodes])
  const findings = useMemo(() => buildFindingsFromSCO(model), [model])

  useEffect(() => {
    if (!id) {
      setNodes(null)
      return
    }
    let disposed = false
    let initializing = true
    const load = () => {
      setLoading(true)
      return Promise.all([listSCONodes({ scanId: id }), cstxFailures(id)]).then(([{ items }, errors]) => {
        if (!disposed) { setNodes(items); setFailure(errors.map((error) => error.error).join('; ')) }
      }).catch((error) => {
        if (!disposed) setFailure(String(error))
      }).finally(() => {
        if (!disposed) setLoading(false)
      })
    }
    const unsubscribe = subscribeCSTXChanges(() => { if (!initializing) void load() })
    void (async () => {
      let syncError = ''
      try { await syncCSTXArtifacts() } catch (error) { syncError = String(error) }
      initializing = false
      if (disposed) return
      await load()
      if (!disposed && syncError) setFailure(syncError)
    })()
    return () => {
      disposed = true
      unsubscribe()
    }
  }, [error, id, pending])

  const labels = {
    arguments: tChat('toolCard.arguments'),
    result: tChat('toolCard.result'),
    failed: tChat('toolCard.failed'),
    running: tChat('toolCard.running'),
    completed: tChat('toolCard.completed'),
  }
  const failureNotice = failure && (
    <div role="alert" className="px-3 py-2 text-xs text-warning">
      {t('resultsIncomplete')}: {failure}
      <button className="ml-2 underline" onClick={() => void syncCSTXArtifacts().then(() => retryCSTXFailures()).catch((error) => setFailure(String(error)))}>{t('retryParsing')}</button>
    </div>
  )

  if (!nodes || nodes.length === 0) {
    return (
      <div>
      <ToolResultDisplay
        toolName={toolName}
        toolArgs={toolArgs}
        result={result}
        pending={pending}
        error={error}
        toolResult={toolResult}
        resultEventId={resultEventId}
        observations={observations}
        observationLabels={observationLabels}
        labels={labels}
      />
      {failureNotice}
      </div>
    )
  }

  return <ToolResultDisplay toolName={toolName} toolArgs={toolArgs} result={result} pending={pending} error={error}
    toolResult={toolResult} resultEventId={resultEventId} observations={observations} observationLabels={observationLabels} labels={{ ...labels, result: tChat('toolCard.rawOutput') }}
    headerExtra={<Badge variant="muted" size="sm" className="shrink-0 rounded-full font-mono tabular-nums">{nodes.length} {t('assets')}</Badge>}>
    {failureNotice}
    <Tabs defaultValue="assets" className="p-3">
      <TabsList>
        <TabsTrigger value="assets">{tf('assets')}</TabsTrigger>
        <TabsTrigger value="findings">{tf('findings')} {findings.length}</TabsTrigger>
      </TabsList>
      <TabsContent value="assets"><AssetResultView model={model} anchorPrefix={id} /></TabsContent>
      <TabsContent value="findings"><FindingsPanel findings={findings} /></TabsContent>
    </Tabs>
    {loading && <div className="flex items-center gap-2 px-3 py-2 text-xs text-muted-foreground">
      <Loader2 className="h-3 w-3 animate-spin" />{tChat('toolCard.loadingResults')}
    </div>}
  </ToolResultDisplay>
}
