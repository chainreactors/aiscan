import { ClaimType, type Claim, type Evaluation } from '../../gen/decision/claim_pb'
import { Check, CircuitBoard, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { DecisionRequest, DecisionResult } from '../../gen/types/jev_pb'
import type { TokenUsage } from '../../../cyber-ui/packages/aop/src/gen/aop/event_pb'
import { decisionOptions, decisionQuestions, evaluationNumber } from '../../lib/jev-decisions'
import './JEVTimeline.css'

const percent = (value: number) => `${(value * 100).toFixed(1)}%`

export function TokenUsageLine({ source, usage }: { source: string; usage?: TokenUsage }) {
  const { t } = useTranslation('jev')
  return <p className="mt-2 text-[10px] tabular-nums text-muted-foreground" data-testid="jev-token-usage">{source} · {usage && !usage.detail.usage_missing
    ? t('tokenUsage', { input: usage.inputTokens.toString(), output: usage.outputTokens.toString() }) : t('usageUnknown')}</p>
}

export function ChoiceBranches({ question, answer, definition = false }: { question: Claim; answer?: Evaluation; definition?: boolean }) {
  const { t } = useTranslation('jev')
  const options = decisionOptions(question, answer)
  return <><p className="mt-3 text-[10px] text-muted-foreground">{t(!definition && answer ? 'rankedOptions' : 'candidateOptions', { count: options.length })}</p><div className="jev-branches" data-testid="jev-options">
    {options.map((option, rank) => <div key={option.id} className={`jev-option ${option.selected ? 'jev-option-selected' : ''}`}
      data-option-id={option.id} data-selected={option.selected}>
      <div className="flex min-w-0 items-center gap-2 text-xs">
        {!definition && <span className="w-3 shrink-0 text-[10px] tabular-nums text-muted-foreground">{rank + 1}</span>}
        <span className="min-w-0 flex-1 break-words font-medium" title={option.id}>{t(`optionTitles.${option.id}`, { defaultValue: option.description || option.id })}</span>
        {option.selected && <Check aria-label={t('selected')} className="h-3.5 w-3.5 shrink-0 text-emerald-600 dark:text-emerald-400" />}
        {!definition && <span className="shrink-0 tabular-nums" data-testid="jev-probability">{option.probability === undefined ? '—' : percent(option.probability)}</span>}
      </div>
      {option.description && t(`optionTitles.${option.id}`, { defaultValue: option.description }) !== option.description && <p className={`mt-1 break-words text-[11px] leading-relaxed text-muted-foreground ${definition ? '' : 'pl-5'}`}>{option.description}</p>}
      {!definition && <div className="jev-probability-track" aria-hidden="true"><span style={{ width: `${(option.probability ?? 0) * 100}%` }} /></div>}
    </div>)}
    {!options.length && <p className="text-xs text-muted-foreground">{t('noOptions')}</p>}
  </div></>
}

function QuestionView({ id, question, answer, purpose, finished }: { id: string; question: Claim; answer?: Evaluation; purpose: string; finished: boolean }) {
  const { t } = useTranslation('jev')
  const title = t(`questionTitles.${id}`, { defaultValue: /^claim\d+$/.test(id) ? t('claimDeclaration')
    : /^coverage\d+$/.test(id) ? t('coverageReview') : question.type === ClaimType.score ? t('nativeScore') : question.type === ClaimType.noul ? t('booleanJudgment')
      : purpose === 'jev_reflex' ? t('sceneMembership') : t('nextOperation') })
  const levels = question.type === ClaimType.score ? question.options : []
  const scalar = evaluationNumber(answer)
  const maximum = question.type === ClaimType.score ? levels.length ? levels.length - 1 : undefined : 1
  return <section className="jev-question" data-testid="jev-question" data-question-id={id}>
    <header className="flex min-w-0 items-center justify-between gap-2 text-xs">
      <span className="font-medium" title={id}>{title}</span><span className="font-mono text-[10px] text-muted-foreground">{ClaimType[question.type]}</span>
    </header>
    {question.context && <p className="jev-instructions mt-2 whitespace-pre-wrap break-words text-[11px] leading-relaxed text-muted-foreground">{question.context}</p>}
    {question.type === ClaimType.choice ? <ChoiceBranches question={question} answer={answer} />
      : question.type === ClaimType.score || question.type === ClaimType.noul ? <div className="mt-3 space-y-2" data-testid="jev-scale">
        <div className="flex items-baseline justify-between gap-2 text-xs"><span className="text-muted-foreground">{t(question.type === ClaimType.score ? 'nativeScore' : 'trueProbability')}</span>
          <strong className="tabular-nums">{scalar === undefined ? '—' : question.type === ClaimType.noul ? percent(scalar) : scalar.toFixed(2)}</strong></div>
        {maximum !== undefined && <><div className="jev-scalar-track">{scalar !== undefined && <span style={{ left: `${maximum ? Math.max(0, Math.min(1, scalar / maximum)) * 100 : 0}%` }} />}</div>
          <div className="flex justify-between gap-2 text-[10px] text-muted-foreground"><span>{question.type === ClaimType.noul ? `${t('falseValue')} · 0` : '0'}</span><span>{question.type === ClaimType.noul ? `${t('trueValue')} · 1` : maximum}</span></div></>}
        {!!levels.length && <ol className="space-y-1 text-[11px] text-muted-foreground">{levels.map((level, index) => <li key={index} className="flex gap-2"><span className="tabular-nums">{index}</span><span className="break-words">{level}</span></li>)}</ol>}
        <p className="text-[10px] text-muted-foreground">{t(question.type === ClaimType.score ? 'scoreExplanation' : 'noulExplanation')}</p>
      </div> : null}
    <div className="mt-3 border-t border-border/60 pt-2 text-[10px] text-muted-foreground">
      <span>{answer ? `${t('confidence')} ${percent(answer.confidence)}` : t(finished ? 'noAnswer' : 'awaitingAnswer')}</span>
    </div>
  </section>
}

export function DecisionBatch({ request, result, bodyOnly = false }: { request: DecisionRequest; result?: DecisionResult; bodyOnly?: boolean }) {
  const { t } = useTranslation('jev')
  const questions = decisionQuestions(request.claims)
  const header = <div className="flex flex-wrap items-center gap-2 text-xs">
      <CircuitBoard className="h-4 w-4 text-emerald-500" /><span className="font-semibold">JEV</span><span>{t('judgment')}</span>
      <span className="text-[10px] text-muted-foreground">{questions.length > 1 ? t('parallelQuestions', { count: questions.length }) : t('singleQuestion')}</span>
      {result ? <span className="ml-auto text-[10px] tabular-nums text-muted-foreground">{Number(result.elapsedMs)} ms</span> : <Loader2 className="ml-auto h-3 w-3 animate-spin" />}
    </div>
  const body = <>{result && <TokenUsageLine source="JEV" usage={result.usage} />}{result?.error && <p className="mt-2 break-words text-xs text-destructive">{result.error}</p>}
    <div className="jev-question-grid">{questions.map(([id, question]) => <QuestionView key={id} id={id} question={question} answer={result?.evaluations[id]} purpose={request.purpose} finished={!!result} />)}</div></>
  const attributes = { 'data-testid': 'jev-decision', 'data-request-id': request.requestId, 'data-state': result ? result.error ? 'failed' : 'answered' : 'judging' }
  return <div className={`jev-decision ${bodyOnly ? 'jev-decision-body' : ''}`} {...attributes}>{bodyOnly ? <div className="flex items-center justify-between text-[10px] text-muted-foreground">
    <span>{questions.length > 1 ? t('parallelQuestions', { count: questions.length }) : t('singleQuestion')}</span>
    {result ? <span>{Number(result.elapsedMs)} ms</span> : <Loader2 className="h-3 w-3 animate-spin" />}
  </div> : header}{body}</div>
}
