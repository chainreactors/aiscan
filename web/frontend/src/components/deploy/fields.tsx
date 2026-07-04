import { useEffect, useRef, useState, type ComponentType, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { RefreshCw } from 'lucide-react'
import { Button, Input, Select, SelectTrigger, SelectContent, SelectItem, SelectValue, Spinner } from '@aspect/ui'

// LiveSelect shows a live dropdown once a list is fetched, falling back to a
// free-text input. `load` does the (admin-gated) lookup; when `autoLoadKey`
// changes to a truthy value the list is fetched automatically. `autoSelect`
// optionally picks a default id once the list arrives and nothing valid is
// chosen yet. "Enter manually" keeps a custom value reachable even with a list.
export function LiveSelect<T>({ value, onChange, placeholder, load, autoLoadKey, getId, getLabel, autoSelect, loadLabel }: {
  value: string
  onChange: (v: string) => void
  placeholder?: string
  load: () => Promise<T[]>
  autoLoadKey?: string
  getId: (item: T) => string
  getLabel: (item: T) => string
  autoSelect?: (items: T[]) => string | undefined
  loadLabel: string
}) {
  const { t } = useTranslation('deploy')
  const [items, setItems] = useState<T[] | null>(null)
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState('')
  const [manual, setManual] = useState(false)

  // Monotonic request id: autoLoadKey (region) can change mid-flight, and cloud
  // list APIs have variable latency, so an older region's response can resolve
  // last. Ignore any response that isn't the newest request, otherwise a stale
  // list would replace items and autoSelect would clobber the current selection
  // with an item from the wrong region.
  const reqRef = useRef(0)
  const fetchItems = async () => {
    const req = ++reqRef.current
    setLoading(true)
    setErr('')
    try {
      const rs = await load()
      if (req !== reqRef.current) return
      setItems(rs)
      if (rs.length === 0) {
        setErr(t('listEmpty'))
        return
      }
      // Pre-select a sensible default when nothing valid is chosen yet.
      if (autoSelect && (!value || !rs.some((it) => getId(it) === value))) {
        const def = autoSelect(rs)
        if (def) onChange(def)
      }
    } catch (e: any) {
      if (req !== reqRef.current) return
      setErr(e.message || t('listFailed'))
      setItems(null)
    } finally {
      if (req === reqRef.current) setLoading(false)
    }
  }

  useEffect(() => {
    if (autoLoadKey) {
      // Drop the previous key's list so it can't be shown/selected during the
      // reload, then fetch (which invalidates any in-flight request via reqRef).
      setItems(null)
      void fetchItems()
    } else {
      // Clearing the key invalidates any in-flight request; its finally block is
      // guarded by `req === reqRef.current` and so won't reset loading, so do it
      // here — otherwise the Load button stays disabled with a spinner forever.
      reqRef.current++
      setItems(null)
      setManual(false)
      setLoading(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [autoLoadKey])

  if (items && items.length > 0 && !manual) {
    const known = items.some((it) => getId(it) === value)
    return (
      <div className="space-y-1">
        <div className="flex gap-2">
          <Select value={known ? value : ''} onValueChange={onChange}>
            <SelectTrigger className="h-9 w-full"><SelectValue placeholder={placeholder} /></SelectTrigger>
            <SelectContent>
              {items.map((it) => (
                <SelectItem key={getId(it)} value={getId(it)}>{getLabel(it)}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button type="button" variant="outline" size="icon" disabled={loading} onClick={() => void fetchItems()} title={t('refresh')}>
            <RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
          </Button>
        </div>
        <button type="button" className="text-xs text-muted-foreground/70 underline-offset-2 hover:text-foreground hover:underline" onClick={() => setManual(true)}>
          {t('enterManually')}
        </button>
      </div>
    )
  }

  return (
    <div className="space-y-1">
      <div className="flex gap-2">
        <Input value={value} onChange={(e) => onChange(e.target.value)} placeholder={placeholder} />
        <Button type="button" variant="outline" disabled={loading} onClick={() => void fetchItems()} title={loadLabel}>
          {loading ? <Spinner className="h-4 w-4" /> : <RefreshCw className="h-3.5 w-3.5" />}{loadLabel}
        </Button>
      </div>
      {items && items.length > 0 && manual && (
        <button type="button" className="text-xs text-muted-foreground/70 underline-offset-2 hover:text-foreground hover:underline" onClick={() => setManual(false)}>
          {t('backToList')}
        </button>
      )}
      {err && <span className="block text-xs text-destructive">{err}</span>}
    </div>
  )
}

export function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <label className="block space-y-1.5">
      <span className="text-xs font-medium text-foreground/75">{label}</span>
      {children}
      {hint && <span className="block text-xs leading-relaxed text-muted-foreground">{hint}</span>}
    </label>
  )
}

// SectionTitle — the panel's one section eyebrow. In a CJK UI weight can't carry
// hierarchy (MiSans ships only 330/520, so medium≡semibold for Chinese), so a
// section reads as a section through size + full-ink colour + a blue "system"
// tick (or a leading icon), one clear step above the muted field labels below it.
export function SectionTitle({ children, icon: Icon, hint, className = '' }: {
  children: ReactNode
  icon?: ComponentType<{ className?: string }>
  hint?: string
  className?: string
}) {
  return (
    <div className={`flex items-center gap-2 text-[13px] font-semibold text-foreground ${className}`}>
      {Icon
        ? <Icon className="h-3.5 w-3.5 shrink-0 text-primary" />
        : <span className="h-3.5 w-[3px] shrink-0 rounded-full bg-primary/70" aria-hidden />}
      <span>{children}</span>
      {hint && <span className="text-xs font-normal text-muted-foreground">· {hint}</span>}
    </div>
  )
}
