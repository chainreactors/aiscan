import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, ShieldCheck, ShieldOff, Loader2, Settings } from 'lucide-react'
import { Button, DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuRadioGroup, DropdownMenuRadioItem, Tooltip, TooltipContent, TooltipTrigger } from '@cyber/ui'
import { cn } from '@cyber/theme'
import { CONFIG_CHANGED_EVENT, getConfigStatus, setGuardrailMode } from '../api'

export function GuardrailToggle({ disabled = false, onConfigure }: { disabled?: boolean; onConfigure: () => void }) {
  const { t } = useTranslation('app')
  const [available, setAvailable] = useState(true)
  const [enabled, setEnabled] = useState<boolean | null>(null)
  const [mode, setMode] = useState<'safe' | 'auto'>('auto')
  const [open, setOpen] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  useEffect(() => {
    let disposed = false
    const refresh = () => {
      void getConfigStatus().then(config => {
        if (!disposed) {
          setAvailable(!!config.extensions.guardrail)
          setError('')
          setEnabled(config.extensions.guardrail?.values?.provider === 'jev' && config.extensions.jev?.configuredSecrets.includes('api_key') === true)
          setMode(config.extensions.guardrail?.values?.mode === 'safe' ? 'safe' : 'auto')
        }
      }).catch(cause => { if (!disposed) setError(cause instanceof Error ? cause.message : String(cause)) })
    }
    refresh()
    window.addEventListener(CONFIG_CHANGED_EVENT, refresh)
    return () => { disposed = true; window.removeEventListener(CONFIG_CHANGED_EVENT, refresh) }
  }, [])
  const update = async (nextMode: 'safe' | 'auto') => {
    if (!enabled || saving || nextMode === mode) return
    setOpen(false)
    setSaving(true)
    setError('')
    try {
      const config = await setGuardrailMode(nextMode)
      setEnabled(config.extensions.guardrail?.values?.provider === 'jev' && config.extensions.jev?.configuredSecrets.includes('api_key') === true)
      setMode(config.extensions.guardrail?.values?.mode === 'safe' ? 'safe' : 'auto')
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) }
    finally { setSaving(false) }
  }
  if (!available) return null
  const value = enabled ? mode : 'unconfigured'
  const label = t(saving ? 'guardrailSaving' : enabled === null ? 'guardrailLoading' : 'guardrailChoice_' + value)
  const Icon = enabled ? ShieldCheck : ShieldOff
  return <div className="relative flex shrink-0 items-center">
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <Tooltip>
        <TooltipTrigger asChild>
          <DropdownMenuTrigger asChild>
            <Button type="button" aria-label={t('guardrailControl', { mode: label })} aria-busy={saving}
              data-guardrail-control variant="ghost" size="xs" disabled={disabled || saving || enabled === null}
              className={cn('h-7 gap-1.5 rounded-md border px-2', enabled ? 'border-emerald-600/40 text-emerald-700 dark:text-emerald-400' : 'border-border text-muted-foreground')}>
              {saving || enabled === null ? <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden="true" /> : <Icon className="h-3.5 w-3.5" aria-hidden="true" />}<span className="hidden text-xs sm:inline">{label}</span><ChevronDown className="h-3 w-3" aria-hidden="true" />
            </Button>
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <TooltipContent className="max-w-xs text-wrap">{t('guardrailSwitchHint')}</TooltipContent>
      </Tooltip>
      <DropdownMenuContent align="end" className="w-44">
        {!enabled && <p className="px-2 py-1.5 text-xs text-muted-foreground">{t('guardrailConfigureHint')}</p>}
        <DropdownMenuRadioGroup value={mode} onValueChange={next => { void update(next as 'safe' | 'auto') }}>
          <DropdownMenuRadioItem value="auto" disabled={!enabled}>{t('guardrailMode_auto')}</DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="safe" disabled={!enabled}>{t('guardrailMode_safe')}</DropdownMenuRadioItem>
        </DropdownMenuRadioGroup>
        <DropdownMenuSeparator />
        <DropdownMenuItem onSelect={() => { setOpen(false); onConfigure() }}>
          <Settings className="mr-2 h-3.5 w-3.5" aria-hidden="true" />{t('guardrailConfigureAction')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
    {error && <div role="alert" className="absolute right-0 top-9 z-[80] w-80 rounded-md border border-destructive/40 bg-background p-3 text-xs text-destructive shadow-lg">
      {error}<button type="button" className="ml-2 underline" onClick={() => setError('')}>{t('closePanel')}</button>
    </div>}
  </div>
}
