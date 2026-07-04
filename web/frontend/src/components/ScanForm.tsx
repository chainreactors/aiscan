import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { AlertTriangle, Brain, Crosshair, Loader2, Play, Radar } from 'lucide-react'
import type { ScanOptions } from '../api'
import { parseTargets, type InvalidReason } from '../lib/targets'
import { Button, ToggleGroup, ToggleGroupItem } from '@aspect/ui'
import { cn } from '@aspect/theme'

const REASON_KEY: Record<InvalidReason, string> = {
  cidr: 'reasonCidr',
  scheme: 'reasonScheme',
  url: 'reasonUrl',
  format: 'reasonFormat',
}

const MAX_SHOWN_INVALID = 4

interface ScanFormProps {
  onSubmit: (target: string, mode: string, options: ScanOptions) => void
  disabled: boolean
  analysisAvailable: boolean
}

export default function ScanForm({ onSubmit, disabled, analysisAvailable }: ScanFormProps) {
  const { t } = useTranslation('scan')
  const [target, setTarget] = useState('')
  const [mode, setMode] = useState('quick')
  const [options, setOptions] = useState<ScanOptions>({ verify: false, sniper: false, deep: false })
  const [focused, setFocused] = useState(false)
  const targetRef = useRef<HTMLTextAreaElement>(null)

  // Batch is always on: the server splits any comma / whitespace / newline list
  // (see ParseTargets) and skips anything invalid. We mirror that client-side to
  // count what was entered and flag the tokens that will be skipped as you type.
  const parsed = useMemo(() => parseTargets(target), [target])
  const targetCount = parsed.total
  const shownInvalid = parsed.invalid.slice(0, MAX_SHOWN_INVALID)
  const moreInvalid = parsed.invalid.length - shownInvalid.length

  useEffect(() => {
    if (!analysisAvailable) {
      setOptions({ verify: false, sniper: false, deep: false })
    }
  }, [analysisAvailable])

  // Grow the target box to fit its content: a single line by default, taller as
  // targets are pasted or typed, capped so it never eats the deck.
  useEffect(() => {
    const el = targetRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${el.scrollHeight}px`
  }, [target])

  const submitTargets = () => {
    const trimmed = target.trim()
    if (!trimmed || disabled) return
    onSubmit(trimmed, mode, options)
  }

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    submitTargets()
  }

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // Enter scans; Shift+Enter drops to a new line to add another target by hand.
    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault()
      submitTargets()
    }
  }

  const toggleOption = (key: keyof ScanOptions) => {
    setOptions((current) => ({ ...current, [key]: !current[key] }))
  }

  const analysisOptions: Array<{
    key: keyof ScanOptions
    label: string
    icon: ReactNode
    activeClass: string
  }> = [
    {
      key: 'verify',
      label: t('optVerify'),
      icon: <Brain className="h-4 w-4" />,
      activeClass: 'border-primary/40 bg-primary/15 text-primary',
    },
    {
      key: 'sniper',
      label: t('optSniper'),
      icon: <Crosshair className="h-4 w-4" />,
      // On/off option chips carry the single blue signal accent; the icon (not a
      // warm hue) differentiates them. Warm red/amber stays reserved for severity.
      activeClass: 'border-primary/40 bg-primary/15 text-primary',
    },
    {
      key: 'deep',
      label: t('optDeep'),
      icon: <Radar className="h-4 w-4" />,
      activeClass: 'border-primary/40 bg-primary/15 text-primary',
    },
  ]

  return (
    <form
      onSubmit={handleSubmit}
      className="grid w-full grid-cols-[minmax(0,1fr)_auto] items-center gap-2 sm:flex sm:flex-wrap sm:gap-3"
    >
      <div className="reticle col-start-1 row-start-1 min-w-0 rounded-lg bg-secondary/40 p-1 focus-within:bg-secondary/60 sm:min-w-[16rem] sm:flex-1">
        <div className="relative">
          <Crosshair className="pointer-events-none absolute left-2.5 top-2 h-4 w-4 text-primary/80" />
          <textarea
            ref={targetRef}
            value={target}
            onChange={(e) => setTarget(e.target.value)}
            onKeyDown={handleKeyDown}
            onFocus={() => setFocused(true)}
            onBlur={() => setFocused(false)}
            placeholder={t('targetPlaceholder')}
            disabled={disabled}
            autoFocus
            rows={1}
            wrap="off"
            aria-label={t('scanTarget')}
            className={cn(
              'block max-h-40 w-full resize-none overflow-x-hidden overflow-y-auto border-0 bg-transparent py-1.5 pl-9 font-mono text-sm leading-5 shadow-none outline-none placeholder:font-sans placeholder:text-muted-foreground focus-visible:ring-0',
              targetCount > 1 ? 'pr-24' : 'pr-3',
            )}
          />
          {targetCount > 1 && (
            <span className="pointer-events-none absolute bottom-1.5 right-2 rounded bg-secondary px-1 font-mono text-[11px] text-muted-foreground">
              {t('batchCount', { count: targetCount })}
            </span>
          )}
        </div>
      </div>

      <div className="col-span-full row-start-2 flex min-w-0 items-center gap-2 sm:contents">
        <ToggleGroup value={mode} onValueChange={setMode} disabled={disabled} ariaLabel="Scan mode">
          <ToggleGroupItem value="quick" className="ctl-label">{t('quick')}</ToggleGroupItem>
          <ToggleGroupItem value="full" className="ctl-label">{t('full')}</ToggleGroupItem>
        </ToggleGroup>

        <div className="inline-flex min-w-0 items-center gap-1.5 sm:gap-2">
          {analysisOptions.map((item) => {
            const active = options[item.key]
            const optionDisabled = disabled || !analysisAvailable
            return (
              <button
                key={item.key}
                type="button"
                aria-pressed={active}
                aria-label={t('optionAnalysis', { label: item.label })}
                title={analysisAvailable ? item.label : t('llmOffline')}
                disabled={optionDisabled}
                onClick={() => toggleOption(item.key)}
                className={cn(
                  'inline-flex h-10 w-10 shrink-0 items-center justify-center gap-2 rounded-md border px-0 text-xs font-medium transition-colors sm:w-auto sm:px-3',
                  active ? item.activeClass : 'border-input bg-secondary/50 text-muted-foreground hover:text-foreground',
                  optionDisabled && 'cursor-not-allowed opacity-50',
                )}
              >
                {item.icon}
                <span className="hidden sm:inline ctl-label">{item.label}</span>
              </button>
            )
          })}
        </div>

        <Button
          type="submit"
          disabled={disabled || !target.trim()}
          aria-label={disabled ? t('scanningTarget') : t('startScan')}
          className="h-10 w-10 shrink-0 px-0 bg-primary text-primary-foreground hover:bg-primary/90 sm:w-auto sm:px-5"
        >
          {disabled ? (
            <Loader2 className="w-4 h-4 animate-spin" />
          ) : (
            <Play className="w-4 h-4" />
          )}
          <span className="hidden sm:inline">{disabled ? t('scanning') : t('scan')}</span>
        </Button>
      </div>

      {(focused || parsed.invalid.length > 0) && (
        <div className="col-span-full row-start-3 min-w-0 space-y-1 pl-1 pt-0.5 sm:order-last sm:basis-full">
          {parsed.invalid.length > 0 && (
            <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
              {shownInvalid.map((item) => (
                <span key={item.target} className="inline-flex min-w-0 items-center gap-1 text-[11px] text-warning">
                  <AlertTriangle className="h-3 w-3 shrink-0" />
                  <span className="truncate font-mono">{item.target}</span>
                  <span className="shrink-0 text-warning/70">
                    {t(REASON_KEY[item.reason])} · {t('willSkip')}
                  </span>
                </span>
              ))}
              {moreInvalid > 0 && (
                <span className="text-[11px] text-warning/70">{t('moreInvalid', { count: moreInvalid })}</span>
              )}
            </div>
          )}
          {focused && <p className="font-sans text-[11px] text-muted-foreground/70">{t('targetHint')}</p>}
        </div>
      )}
    </form>
  )
}
