import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Cloud, X, RefreshCw, AlertTriangle } from 'lucide-react'
import {
  getAdminToken, setAdminToken,
  getPublicURL, listCloudCredentials, listDeploys,
} from '../api'
import type { CloudCredentialView, DeployRecordView } from '../api'
import { Button } from '@aspect/ui'
import { usePolling } from '../hooks/usePolling'
import { CloudsTab } from './deploy/CloudsTab'
import { DeployTab } from './deploy/DeployForm'
import { FleetTab } from './deploy/FleetList'
import { useDialogA11y } from '../hooks/useDialogA11y'

interface DeployPanelProps {
  open: boolean
  onClose: () => void
  /** Active project id — seeds the IOA space so same-project nodes cooperate. */
  defaultSpace?: string
}

type TabKey = 'clouds' | 'deploy' | 'fleet'

export default function DeployPanel({ open, onClose, defaultSpace }: DeployPanelProps) {
  const { t } = useTranslation('deploy')
  const [tab, setTab] = useState<TabKey>('clouds')
  const [error, setError] = useState('')

  // hub-level config
  const [adminToken, setTok] = useState(getAdminToken())
  const [publicUrl, setUrl] = useState('')
  const [providers, setProviders] = useState<string[]>(['aliyun', 'tencent'])

  // data
  const [creds, setCreds] = useState<CloudCredentialView[]>([])
  const [deploys, setDeploys] = useState<DeployRecordView[]>([])
  const [loading, setLoading] = useState(false)

  const reloadAll = async (silent = false) => {
    if (!silent) {
      setLoading(true)
      setError('')
    }
    try {
      const [info, c, d] = await Promise.all([getPublicURL(), listCloudCredentials(), listDeploys()])
      setUrl(info.public_url)
      if (info.providers?.length) setProviders(info.providers)
      setCreds(c)
      setDeploys(d)
    } catch (err: any) {
      if (!silent) setError(err.message || t('failedLoad'))
    } finally {
      if (!silent) setLoading(false)
    }
  }

  useEffect(() => {
    if (!open) return
    setTok(getAdminToken())
    void reloadAll()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  // Silent fleet poll while watching nodes provision — paused when the tab is
  // hidden, and only while the fleet tab is actually open.
  usePolling(() => void reloadAll(true), 2500, open && tab === 'fleet')

  // Esc-to-close + focus trap/restore (parity with the Radix-backed ConfirmDialog).
  const panelRef = useRef<HTMLDivElement>(null)
  useDialogA11y(open, onClose, panelRef)

  if (!open) return null

  const saveTokenAndReload = async () => {
    setAdminToken(adminToken.trim())
    await reloadAll()
  }

  const TABS: { key: TabKey; label: string }[] = [
    { key: 'clouds', label: t('tabs.clouds') },
    { key: 'deploy', label: t('tabs.deploy') },
    { key: 'fleet', label: t('tabs.fleet') },
  ]

  return (
    <div onClick={onClose} className="fixed inset-0 z-50 flex items-start justify-center bg-black/50 px-4 py-8 backdrop-blur-md animate-in fade-in duration-200">
      <div ref={panelRef} tabIndex={-1} onClick={(e) => e.stopPropagation()} role="dialog" aria-modal="true" aria-labelledby="deploy-panel-title" className="w-full max-w-3xl overflow-hidden rounded-2xl border border-border/70 bg-card shadow-elevated animate-in fade-in zoom-in-95 duration-200 focus:outline-none">
        <div className="flex items-center justify-between border-b border-border/60 px-4 py-3">
          <div className="flex items-center gap-2">
            <Cloud className="h-4 w-4 text-primary" />
            <div>
              <div id="deploy-panel-title" className="text-sm font-medium text-foreground">{t('title')}</div>
              <div className="text-xs text-muted-foreground">{t('subtitle')}</div>
            </div>
          </div>
          <button type="button" onClick={onClose} aria-label={t('close')} title={t('close')} className="rounded-md p-1 text-muted-foreground hover:bg-accent hover:text-foreground">
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="flex gap-1 overflow-x-auto border-b border-border px-4 py-1">
          {TABS.map((tb) => (
            <button
              key={tb.key} type="button" onClick={() => setTab(tb.key)}
              className={`whitespace-nowrap rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${tab === tb.key ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:bg-accent hover:text-foreground'}`}
            >{tb.label}</button>
          ))}
          <div className="ml-auto flex items-center">
            <button type="button" onClick={() => void reloadAll()} className="rounded-md p-1.5 text-muted-foreground hover:bg-accent hover:text-foreground" title={t('refresh')}>
              <RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
            </button>
          </div>
        </div>

        <div className="max-h-[70vh] overflow-y-auto p-4">
          {error && (
            <div className="mb-3 flex items-center gap-2 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              <AlertTriangle className="h-4 w-4" />{error}
            </div>
          )}

          {tab === 'clouds' && (
            <CloudsTab
              adminToken={adminToken} setTok={setTok} onSaveToken={saveTokenAndReload}
              publicUrl={publicUrl} setUrl={setUrl}
              providers={providers} creds={creds} loading={loading}
              onReload={reloadAll} setError={setError}
            />
          )}
          {tab === 'deploy' && (
            <DeployTab creds={creds} onDeployed={reloadAll} setError={setError} onGoFleet={() => setTab('fleet')} defaultSpace={defaultSpace} />
          )}
          {tab === 'fleet' && (
            <FleetTab deploys={deploys} loading={loading} onReload={reloadAll} setError={setError} />
          )}
        </div>

        <div className="flex justify-end gap-2 border-t border-border/60 px-4 py-3">
          <Button type="button" variant="outline" onClick={onClose}>{t('close')}</Button>
        </div>
      </div>
    </div>
  )
}
