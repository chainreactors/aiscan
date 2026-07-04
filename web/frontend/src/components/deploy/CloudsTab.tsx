import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Trash2, AlertTriangle, Settings2 } from 'lucide-react'
import {
  setPublicURL, deleteCloudCredential, saveCloudCredential, listCloudRegions,
} from '../../api'
import type { CloudCredentialView, CloudRegion } from '../../api'
import { Button, Input, Select, SelectTrigger, SelectContent, SelectItem, SelectValue, Badge, Spinner } from '@aspect/ui'
import { TunnelControl } from './TunnelControl'
import { Field, SectionTitle, LiveSelect } from './fields'
import { useConfirm } from '../ConfirmDialog'

// --- Clouds tab ---

export function CloudsTab(props: {
  adminToken: string; setTok: (v: string) => void; onSaveToken: () => Promise<void>
  publicUrl: string; setUrl: (v: string) => void
  providers: string[]; creds: CloudCredentialView[]; loading: boolean
  onReload: () => Promise<void>; setError: (v: string) => void
}) {
  const { t } = useTranslation('deploy')
  const confirm = useConfirm()
  const { adminToken, setTok, onSaveToken, publicUrl, setUrl, providers, creds, onReload, setError } = props
  const [savingUrl, setSavingUrl] = useState(false)

  const saveUrl = async () => {
    setSavingUrl(true)
    setError('')
    try {
      await setPublicURL(publicUrl.trim())
      await onReload()
    } catch (err: any) {
      setError(err.message || t('failedSave'))
    } finally {
      setSavingUrl(false)
    }
  }

  return (
    <div className="space-y-5">
      <div className="space-y-3 rounded-lg border border-border/60 bg-background/30 p-3">
        <SectionTitle icon={Settings2} hint={t('hubSetupHint')}>{t('hubSetup')}</SectionTitle>
        {!publicUrl && (
          <div className="flex items-center gap-2 rounded-md border border-border/60 bg-secondary/40 px-2.5 py-1.5 text-xs text-muted-foreground">
            <AlertTriangle className="h-3.5 w-3.5 shrink-0 opacity-60" /> {t('publicUrlRequired')}
          </div>
        )}
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label={t('publicUrl')} hint={t('publicUrlHint')}>
            <div className="flex gap-2">
              <Input value={publicUrl} onChange={(e) => setUrl(e.target.value)} placeholder="http://1.2.3.4:3000" />
              <Button type="button" variant="outline" disabled={savingUrl} onClick={() => void saveUrl()}>
                {savingUrl && <Spinner className="h-4 w-4" />}{t('savePublicUrl')}
              </Button>
            </div>
          </Field>
          <Field label={t('adminToken')} hint={t('adminTokenHint')}>
            <div className="flex gap-2">
              <Input type="password" value={adminToken} onChange={(e) => setTok(e.target.value)} placeholder="••••••" />
              <Button type="button" variant="outline" onClick={() => void onSaveToken()}>{t('save')}</Button>
            </div>
          </Field>
        </div>
        <TunnelControl creds={creds} onReload={onReload} setError={setError} />
      </div>

      <div>
        <SectionTitle className="mb-2">{t('credentials')}</SectionTitle>
        <div className="space-y-2">
          {creds.length === 0 && <div className="rounded-md border border-dashed border-border px-3 py-4 text-center text-xs text-muted-foreground">{t('noCredentials')}</div>}
          {creds.map((c) => (
            <div key={c.id} className="flex items-center justify-between rounded-md border border-border/60 bg-background/40 px-3 py-2">
              <div className="flex items-center gap-3 text-sm">
                <Badge variant="secondary" className="text-xs uppercase">{c.provider}</Badge>
                {/* A blank name defaults to the provider (deploy.go), which would
                    just echo the badge — only render a name that adds information.
                    Keep it text-xs so it matches the primary identifier used in the
                    other list rows (FleetList) instead of standing out a size larger. */}
                {c.name && c.name.toLowerCase() !== c.provider.toLowerCase() && (
                  <span className="text-xs font-medium text-foreground">{c.name}</span>
                )}
                <span className="font-mono text-xs text-muted-foreground">{c.access_key_id}</span>
                <span className="text-xs text-muted-foreground">{c.default_region}</span>
              </div>
              <button type="button" aria-label={t('delete')} title={t('delete')} className="rounded-md p-1.5 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                onClick={async () => {
                  if (!(await confirm({ description: t('confirmDeleteCredential'), destructive: true }))) return
                  setError('')
                  try { await deleteCloudCredential(c.id); await onReload() } catch (err: any) { setError(err.message) }
                }}>
                <Trash2 className="h-3.5 w-3.5" />
              </button>
            </div>
          ))}
        </div>
      </div>

      <CredentialForm providers={providers} onSaved={onReload} setError={setError} />
    </div>
  )
}

function CredentialForm({ providers, onSaved, setError }: { providers: string[]; onSaved: () => Promise<void>; setError: (v: string) => void }) {
  const { t } = useTranslation('deploy')
  const [provider, setProvider] = useState(providers[0] || 'aliyun')
  const [name, setName] = useState('')
  const [ak, setAk] = useState('')
  const [sk, setSk] = useState('')
  const [region, setRegion] = useState('')
  const [saving, setSaving] = useState(false)
  // Provider always has a value; the rest are required. Gate the button on them
  // so an empty credential can't be posted (matches DeployForm's required gating).
  const canSave = !!name.trim() && !!ak.trim() && !!sk.trim() && !!region.trim()

  const submit = async () => {
    if (!canSave) return
    setSaving(true)
    setError('')
    try {
      await saveCloudCredential({ provider, name, access_key_id: ak, access_key_secret: sk, default_region: region })
      setName(''); setAk(''); setSk(''); setRegion('')
      await onSaved()
    } catch (err: any) {
      setError(err.message || t('failedSave'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="rounded-lg border border-border/60 p-3">
      <SectionTitle className="mb-3">{t('addCredential')}</SectionTitle>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label={t('provider')}>
          {/* Switching provider must drop the previously picked region and the
              cached region list — both belong to the old cloud. Clear region and
              remount the LiveSelect (key=provider) so a stale region can't be
              saved against the wrong provider. */}
          <Select value={provider} onValueChange={(p) => { setProvider(p); setRegion('') }}>
            <SelectTrigger className="h-9 w-full"><SelectValue /></SelectTrigger>
            <SelectContent>{providers.map((p) => <SelectItem key={p} value={p}>{p}</SelectItem>)}</SelectContent>
          </Select>
        </Field>
        <Field label={t('name')}><Input value={name} onChange={(e) => setName(e.target.value)} placeholder="prod" /></Field>
        <Field label={t('accessKeyId')}><Input value={ak} onChange={(e) => setAk(e.target.value)} placeholder="LTAI… / AKID…" /></Field>
        <Field label={t('accessKeySecret')}><Input type="password" value={sk} onChange={(e) => setSk(e.target.value)} placeholder={t('accessKeySecretPlaceholder')} /></Field>
        <Field label={t('defaultRegion')} hint={t('credentialPullHint')}>
          <LiveSelect<CloudRegion>
            key={provider}
            value={region} onChange={setRegion} placeholder="cn-hangzhou / ap-guangzhou"
            load={() => listCloudRegions({ provider, access_key_id: ak, access_key_secret: sk })}
            getId={(r) => r.id} getLabel={(r) => (r.local_name ? `${r.local_name} · ${r.id}` : r.id)}
            loadLabel={t('loadRegions')}
          />
        </Field>
      </div>
      <div className="mt-3 flex justify-end">
        <Button type="button" disabled={saving || !canSave} onClick={() => void submit()}>{saving && <Spinner className="h-4 w-4" />}{t('save')}</Button>
      </div>
    </div>
  )
}
