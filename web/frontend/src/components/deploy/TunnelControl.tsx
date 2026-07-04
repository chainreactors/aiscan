import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Trash2, Server, AlertTriangle, Zap, Copy } from 'lucide-react'
import { getTunnelStatus, startTunnel, stopTunnel, destroyRelay } from '../../api'
import type { CloudCredentialView, TunnelStatus, StartTunnelRequest } from '../../api'
import { Button, Input, Select, SelectTrigger, SelectContent, SelectItem, SelectValue, Badge, Spinner } from '@aspect/ui'
import { useConfirm } from '../ConfirmDialog'

// --- Outbound SSH reverse tunnel (auto-provisioned relay) ---

export function TunnelControl({ creds, onReload, setError }: {
  creds: CloudCredentialView[]
  onReload: () => Promise<void>
  setError: (v: string) => void
}) {
  const { t } = useTranslation('deploy')
  const confirm = useConfirm()
  const [status, setStatus] = useState<TunnelStatus | null>(null)
  const [busy, setBusy] = useState(false)
  const [cloudId, setCloudId] = useState('')
  const [region, setRegion] = useState('')
  const [copied, setCopied] = useState(false)
  const prevConnected = useRef(false)

  // Default the cloud select to the first credential; region follows it. Also
  // reconcile a dangling selection: if the chosen credential was deleted, its id
  // is still truthy so `!cloudId` alone would never re-pick — provisioning would
  // then run against a non-existent cloud_id. Re-home onto a valid credential
  // whenever the current selection isn't in the list.
  useEffect(() => {
    if (creds.length && !creds.some((c) => c.id === cloudId)) setCloudId(creds[0].id)
  }, [creds, cloudId])
  // Reset the region to the selected credential's default whenever the
  // credential changes (mirrors DeployTab.selectCloud). Keying on cloudId only —
  // not creds — preserves a manual region edit (which doesn't change cloudId)
  // while preventing a stale region from a previous account reaching provisioning.
  useEffect(() => {
    const c = creds.find((x) => x.id === cloudId)
    setRegion(c?.default_region || '')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cloudId])

  // Poll status while the panel is open (relay provisioning takes ~1-2 min).
  useEffect(() => {
    let alive = true
    const tick = async () => {
      try {
        const st = await getTunnelStatus()
        if (alive) setStatus(st)
      } catch { /* keep last state */ }
    }
    void tick()
    const id = setInterval(tick, 2500)
    return () => { alive = false; clearInterval(id) }
  }, [])

  // When the tunnel just came up, its public URL was written — refresh the parent.
  useEffect(() => {
    if (status?.connected && !prevConnected.current) void onReload()
    prevConnected.current = !!status?.connected
  }, [status?.connected]) // eslint-disable-line react-hooks/exhaustive-deps

  const doStart = async (req: StartTunnelRequest) => {
    setBusy(true); setError('')
    try { setStatus(await startTunnel(req)) }
    catch (err: any) { setError(err.message || t('failedSave')) }
    finally { setBusy(false) }
  }
  const stop = async () => {
    setBusy(true); setError('')
    try { setStatus(await stopTunnel()); await onReload() }
    catch (err: any) { setError(err.message || t('failedSave')) }
    finally { setBusy(false) }
  }
  const destroy = async () => {
    if (!(await confirm({ description: t('tunnelDestroyConfirm'), destructive: true }))) return
    setBusy(true); setError('')
    try { setStatus(await destroyRelay()); await onReload() }
    catch (err: any) { setError(err.message || t('failedSave')) }
    finally { setBusy(false) }
  }
  const flashCopied = () => { setCopied(true); setTimeout(() => setCopied(false), 1500) }
  const copy = () => {
    if (!status?.public_url) return
    const url = status.public_url
    // navigator.clipboard only exists in secure contexts; the hub is commonly
    // served over plain HTTP (non-secure), where it's undefined. Only flash
    // "Copied" when a write actually succeeds, with an execCommand fallback so
    // copy still works over HTTP.
    if (navigator.clipboard?.writeText) {
      navigator.clipboard.writeText(url).then(flashCopied).catch(() => {})
      return
    }
    try {
      const ta = document.createElement('textarea')
      ta.value = url
      ta.style.position = 'fixed'
      ta.style.opacity = '0'
      document.body.appendChild(ta)
      ta.focus(); ta.select()
      const ok = document.execCommand('copy')
      document.body.removeChild(ta)
      if (ok) flashCopied()
    } catch { /* clipboard unavailable — leave the URL on screen to copy manually */ }
  }

  if (status && !status.available) {
    return (
      <div className="flex items-center gap-2 rounded-md border border-border/60 bg-background/40 px-2.5 py-1.5 text-xs text-muted-foreground">
        <Zap className="h-3.5 w-3.5 shrink-0" /> {t('tunnelUnavailable')}
      </div>
    )
  }

  const hasRelay = !!(status?.relay_ip || status?.provider)
  const running = !!status?.running
  const connected = !!status?.connected
  const phaseLabel = connected ? t('tunnelConnected')
    : status?.phase === 'provisioning' ? t('tunnelProvisioning')
      : running ? t('tunnelConnecting') : ''

  return (
    <div className="space-y-2 rounded-md border border-border/60 bg-background/40 p-2.5">
      <div className="flex items-center gap-1.5 text-xs font-medium text-foreground">
        <Zap className={`h-3.5 w-3.5 ${connected ? 'text-success' : running ? 'text-warning' : 'text-muted-foreground'}`} />
        {t('tunnelTitle')}
        {phaseLabel && (
          <span className={`rounded px-1.5 py-0.5 text-[11px] font-normal ${connected ? 'bg-success/15 text-success' : 'bg-warning/15 text-warning'}`}>
            {busy && <Spinner className="mr-1 inline h-3 w-3" />}{phaseLabel}
          </span>
        )}
      </div>
      <div className="text-xs leading-relaxed text-muted-foreground">{t('tunnelHint')}</div>

      {/* Provision form: no relay yet and nothing running. */}
      {!hasRelay && !running && (
        creds.length === 0 ? (
          <div className="flex items-center gap-1.5 rounded border border-border/60 bg-secondary/40 px-2 py-1.5 text-xs text-muted-foreground">
            <AlertTriangle className="h-3.5 w-3.5 shrink-0 opacity-60" /> {t('tunnelNoCloud')}
          </div>
        ) : (
          <div className="flex flex-wrap items-end gap-2">
            <div className="min-w-[130px] flex-1">
              <div className="mb-1 text-[11px] text-muted-foreground">{t('tunnelCloud')}</div>
              <Select value={cloudId} onValueChange={setCloudId}>
                <SelectTrigger><SelectValue placeholder={t('tunnelSelectCloud')} /></SelectTrigger>
                <SelectContent>
                  {creds.map((c) => <SelectItem key={c.id} value={c.id}>{c.name} · {c.provider}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
            <div className="w-[130px]">
              <div className="mb-1 text-[11px] text-muted-foreground">{t('tunnelRegion')}</div>
              <Input value={region} onChange={(e) => setRegion(e.target.value)} placeholder={t('tunnelRegionPlaceholder')} />
            </div>
            <Button type="button" size="sm" disabled={busy || !cloudId || !region.trim()} onClick={() => void doStart({ cloud_id: cloudId, region: region.trim() })}>
              {busy && <Spinner className="h-4 w-4" />}{t('tunnelStart')}
            </Button>
          </div>
        )
      )}

      {/* Relay status + actions. */}
      {(hasRelay || running) && (
        <div className="space-y-2">
          {status?.relay_ip && (
            <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
              <Server className="h-3.5 w-3.5 shrink-0" />
              <span>{t('tunnelRelayIP')}: <code className="font-mono text-foreground">{status.relay_ip}</code></span>
              {status.provider && <Badge variant="outline" className="text-[11px]">{status.provider}{status.region ? ` · ${status.region}` : ''}</Badge>}
            </div>
          )}
          {connected && status?.public_url && (
            <div className="flex items-center gap-2 rounded border border-success/30 bg-success/10 px-2 py-1.5">
              <code className="flex-1 truncate font-mono text-xs text-success">{status.public_url}</code>
              <button type="button" onClick={copy} className="text-success/80 hover:text-success" title={t('copy')}>
                <Copy className="h-3.5 w-3.5" />
              </button>
              {copied && <span className="text-[11px] text-success">{t('copied')}</span>}
            </div>
          )}
          <div className="flex items-center gap-2">
            {running ? (
              <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => void stop()}>
                {busy && <Spinner className="h-4 w-4" />}{t('tunnelStop')}
              </Button>
            ) : hasRelay ? (
              <Button type="button" size="sm" disabled={busy} onClick={() => void doStart({})}>
                {busy && <Spinner className="h-4 w-4" />}{t('tunnelReconnect')}
              </Button>
            ) : null}
            {hasRelay && (
              <Button type="button" size="sm" variant="ghost" className="text-destructive hover:text-destructive" disabled={busy} onClick={() => void destroy()}>
                <Trash2 className="h-3.5 w-3.5" />{t('tunnelDestroy')}
              </Button>
            )}
          </div>
        </div>
      )}

      {status?.error && (
        <div className="flex items-start gap-1.5 rounded border border-warning/30 bg-warning/10 px-2 py-1.5 text-xs text-warning">
          <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" /> <span className="break-all">{status.error}</span>
        </div>
      )}
    </div>
  )
}
