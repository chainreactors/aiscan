import { useTranslation } from 'react-i18next'
import type { WorkflowNode } from '../../lib/workflow-view'
import { ToolCallResult } from './ToolCallResult'
import type { ControlStage } from '../../lib/jev-control-flow'

// A call and its receipt occupy separate anchors in one invocation. Render
// arguments only at dispatch and output only at receipt, without another card.
export function WorkflowToolContent({ node, stage }: { node: WorkflowNode; stage: ControlStage }) {
  const { t } = useTranslation('jev')
  const payload = node.record?.value.payload
  const native = node.item?.kind === 'tool_call' ? node.item.toolCall : undefined
  const resultRecord = node.related?.find(record => record.value.payload.case === 'result')
  const resultPayload = resultRecord?.value.payload
  const result = resultPayload?.case === 'result' ? resultPayload.value.result : native?.toolResult
    || (payload?.case === 'result' ? payload.value.result : node.step?.result)
  const call = payload?.case === 'dispatch' ? payload.value.call : node.step?.call
  const args = native?.toolArgs || (call?.arguments?.data ? new TextDecoder().decode(call.arguments.data) : '')
  const feedback = stage === 'feedback' || payload?.case === 'result'
  const eventId = native?.resultEventId || node.events?.find(event => event.payload.case === 'toolResult')?.id || resultRecord?.event.id
  const observations = node.step?.observations || native?.observations || []
  // Only adapt recorded/native data. Timeline owns all formatting and specialized tool UI.
  const displayedResult = result && resultPayload?.case === 'result' && !result.durationMs
    ? { ...result, durationMs: resultPayload.value.elapsedMs } : result
  return <div data-testid={feedback ? 'workflow-tool-result' : args ? 'workflow-tool-arguments' : undefined}>
    <ToolCallResult id={native?.id || call?.id || result?.callId || node.id}
      sessionId={node.sessionId} events={node.events} bodyOnly content={feedback ? 'result' : 'arguments'}
      toolName={native?.toolName || call?.name || result?.name || node.label} toolArgs={args}
      toolResult={feedback ? displayedResult : undefined} result={feedback && !result ? native?.result : undefined}
      resultEventId={eventId} pending={!feedback && !result && node.state === 'pending'}
      error={feedback && (result?.isError || native?.error || node.state === 'failed')}
      observations={feedback || !result ? observations : undefined} />
    {feedback && !result && native?.result === undefined && <p>{t('missingFeedback')}</p>}
    {node.state === 'interrupted' && !feedback && <p>{t('missingFeedback')}</p>}
  </div>
}
