import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Check, ChevronDown, FolderKanban, Plus, Trash2, X } from 'lucide-react'
import { cn } from '@aspect/theme'
import type { Project } from '../../api'

interface ProjectSelectorProps {
  project: string
  projects: Project[]
  onSelect: (id: string) => void
  onCreate: (name: string) => Promise<void>
  onDelete: (id: string) => Promise<void>
}

/**
 * The active-project switcher in the deck top bar. A project scopes the asset
 * pool (separate engagements keep separate inventories), and its id doubles as
 * the derived IOA space name on deploy — so this one control is the single
 * "which engagement am I on" selector. See [[aiscan-asset-pool]].
 */
export default function ProjectSelector({ project, projects, onSelect, onCreate, onDelete }: ProjectSelectorProps) {
  const { t } = useTranslation('app')
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  // Two-step delete: a row's trash icon arms it (confirmId), a second click on
  // the red confirm commits. Guards against a stray click nuking an engagement.
  const [confirmId, setConfirmId] = useState('')
  const [deleting, setDeleting] = useState(false)
  const [delErr, setDelErr] = useState('')
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onDoc = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [open])

  // Reopening the dropdown should never resume a half-armed delete or a stale error.
  useEffect(() => {
    if (!open) {
      setConfirmId('')
      setDelErr('')
    }
  }, [open])

  const nameOf = (p: Project) => p.name || (p.id === 'default' ? t('projectDefault') : p.id)
  const current = projects.find((p) => p.id === project)
  const label = current ? nameOf(current) : project === 'default' ? t('projectDefault') : project

  const submit = async () => {
    const name = draft.trim()
    if (!name || busy) return
    setBusy(true)
    setErr('')
    try {
      await onCreate(name)
      setDraft('')
      setOpen(false)
    } catch (e: any) {
      // onCreate (useProject.createProject) rejects on a network/HTTP error
      // (e.g. duplicate name). Without this catch the rejection is unhandled and
      // the user gets no feedback — surface it and keep the dropdown open to retry.
      setErr(e?.message || t('projectCreateFailed'))
    } finally {
      setBusy(false)
    }
  }

  const remove = async (id: string) => {
    if (deleting) return
    setDeleting(true)
    setDelErr('')
    try {
      await onDelete(id)
      setConfirmId('')
    } catch (e: any) {
      setDelErr(e?.message || t('projectDeleteFailed'))
    } finally {
      setDeleting(false)
    }
  }

  return (
    <div ref={ref} className="relative shrink-0">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        title={t('projectSwitch')}
        className="inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md border border-border bg-secondary/50 px-2 text-[11px] font-medium text-foreground transition-colors hover:bg-secondary"
      >
        <FolderKanban className="h-3.5 w-3.5 text-ai" />
        <span className="max-w-[120px] truncate">{label}</span>
        <ChevronDown className={cn('h-3 w-3 text-muted-foreground/60 transition-transform', open && 'rotate-180')} />
      </button>
      {open && (
        <div className="absolute right-0 top-[calc(100%+6px)] z-50 w-[240px] overflow-hidden rounded-xl border border-border bg-popover p-1.5 shadow-elevated">
          <div className="px-2 pb-1.5 pt-1 font-display text-[10px] font-semibold uppercase tracking-[0.08em] text-muted-foreground">
            {t('projectTitle')}
          </div>
          <div className="max-h-[40vh] overflow-y-auto">
            {projects.map((p) => {
              const active = p.id === project
              const deletable = p.id !== 'default'
              const arming = confirmId === p.id
              return (
                <div
                  key={p.id}
                  className={cn(
                    'group flex items-center rounded-lg pr-1 transition-colors hover:bg-secondary/60',
                    active && 'bg-secondary/60',
                  )}
                >
                  <button
                    type="button"
                    onClick={() => {
                      onSelect(p.id)
                      setOpen(false)
                    }}
                    className="flex min-w-0 flex-1 items-center gap-2 rounded-lg px-2.5 py-2 text-left"
                  >
                    <span className="min-w-0 flex-1 truncate font-mono text-xs text-foreground">{nameOf(p)}</span>
                    {!arming && (
                      <span className="shrink-0 font-mono text-[10px] text-muted-foreground">{p.assets}</span>
                    )}
                    {active && !arming && <Check className="h-3.5 w-3.5 shrink-0 text-ai" />}
                  </button>
                  {deletable &&
                    (arming ? (
                      <div className="flex shrink-0 items-center gap-0.5">
                        <button
                          type="button"
                          onClick={() => void remove(p.id)}
                          disabled={deleting}
                          title={t('projectDeleteConfirm', { n: p.assets })}
                          aria-label={t('projectDeleteConfirm', { n: p.assets })}
                          className="grid h-6 w-6 place-items-center rounded-md bg-destructive/15 text-destructive transition-colors hover:bg-destructive/25 disabled:opacity-50"
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </button>
                        <button
                          type="button"
                          onClick={() => {
                            setConfirmId('')
                            setDelErr('')
                          }}
                          title={t('projectDeleteCancel')}
                          aria-label={t('projectDeleteCancel')}
                          className="grid h-6 w-6 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-secondary"
                        >
                          <X className="h-3.5 w-3.5" />
                        </button>
                      </div>
                    ) : (
                      <button
                        type="button"
                        onClick={() => {
                          setConfirmId(p.id)
                          setDelErr('')
                        }}
                        title={t('projectDelete')}
                        aria-label={t('projectDelete')}
                        className="grid h-6 w-6 shrink-0 place-items-center rounded-md text-muted-foreground/40 opacity-0 transition-colors hover:bg-destructive/15 hover:text-destructive focus:opacity-100 group-hover:opacity-100"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    ))}
                </div>
              )
            })}
          </div>
          {delErr && <div className="px-2 pt-1 text-[10px] leading-snug text-destructive">{delErr}</div>}
          <div className="mt-1 flex items-center gap-1.5 border-t border-border pt-1.5">
            <input
              value={draft}
              onChange={(e) => { setDraft(e.target.value); if (err) setErr('') }}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  void submit()
                }
              }}
              placeholder={t('projectNewPlaceholder')}
              aria-label={t('projectNewPlaceholder')}
              className="min-w-0 flex-1 rounded-md border border-input bg-card px-2 py-1.5 font-mono text-[11px] text-foreground outline-none placeholder:font-sans placeholder:text-muted-foreground/55 focus:border-ai/50"
            />
            <button
              type="button"
              onClick={() => void submit()}
              disabled={!draft.trim() || busy}
              title={t('projectCreate')}
              aria-label={t('projectCreate')}
              className="grid h-7 w-7 shrink-0 place-items-center rounded-md border border-border bg-secondary/50 text-foreground transition-colors hover:bg-secondary disabled:cursor-not-allowed disabled:opacity-40"
            >
              <Plus className="h-3.5 w-3.5" />
            </button>
          </div>
          {err && (
            <div className="px-2 pt-1 text-[10px] leading-snug text-destructive">{err}</div>
          )}
        </div>
      )}
    </div>
  )
}
