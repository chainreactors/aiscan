import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronRight } from 'lucide-react'
import {
  createDeploy, listCloudRegions, listCloudImages, listCloudInstanceTypes,
} from '../../api'
import type {
  CloudCredentialView, DeployRequest, CloudRegion, CloudImage, CloudInstanceType,
} from '../../api'
import { Button, Input, Select, SelectTrigger, SelectContent, SelectItem, SelectValue, Spinner } from '@aspect/ui'
import { Field, SectionTitle, LiveSelect } from './fields'

// --- Deploy tab ---

export function DeployTab({ creds, onDeployed, setError, onGoFleet, defaultSpace }: {
  creds: CloudCredentialView[]; onDeployed: () => Promise<void>; setError: (v: string) => void; onGoFleet: () => void; defaultSpace?: string
}) {
  const { t } = useTranslation('deploy')
  const [form, setForm] = useState<DeployRequest>({ cloud_id: '', image_id: '', instance_type: '', count: 1, space: defaultSpace || 'default', bandwidth_out: 5 })
  const [overridesText, setOverridesText] = useState('')
  const [busy, setBusy] = useState(false)
  const [script, setScript] = useState('')
  const [showAdvanced, setShowAdvanced] = useState(false)

  // Pick the first credential (and its default region) once creds load.
  useEffect(() => {
    if (!form.cloud_id && creds[0]) {
      setForm((f) => ({ ...f, cloud_id: creds[0].id, region: f.region || creds[0].default_region || '' }))
    }
  }, [creds, form.cloud_id])

  const u = <K extends keyof DeployRequest>(k: K, v: DeployRequest[K]) => setForm((f) => ({ ...f, [k]: v }))

  // Switching credential resets the region to the new account's default and
  // clears image/type so their pickers re-resolve a fresh default for it.
  const selectCloud = (id: string) => {
    const c = creds.find((x) => x.id === id)
    setForm((f) => ({ ...f, cloud_id: id, region: c?.default_region || '', image_id: '', instance_type: '' }))
  }

  // Images/types depend on both credential and region; only auto-load once a
  // region is known, otherwise the lookup would fail with "region required".
  const imgKey = form.cloud_id && form.region ? `${form.cloud_id}|${form.region}` : ''

  const parseOverrides = (): Record<string, string> => {
    const out: Record<string, string> = {}
    overridesText.split(',').map((s) => s.trim()).filter(Boolean).forEach((pair) => {
      const i = pair.indexOf('=')
      if (i > 0) out[pair.slice(0, i).trim()] = pair.slice(i + 1).trim()
    })
    return out
  }

  const run = async (dryRun: boolean) => {
    setBusy(true)
    setError('')
    setScript('')
    try {
      const req = { ...form, overrides: parseOverrides(), dry_run: dryRun }
      const deployPromise = createDeploy(req)
      if (!dryRun) {
        onGoFleet()
        void onDeployed()
      }
      const res = await deployPromise
      if (dryRun) {
        setScript(res.script || '')
      } else {
        await onDeployed()
      }
    } catch (err: any) {
      setError(err.message || t('failedSave'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="space-y-4">
      {/* Essentials: enough to launch a working node. */}
      <div className="grid gap-3 sm:grid-cols-2">
        <Field label={t('selectCloud')}>
          <Select value={form.cloud_id} onValueChange={selectCloud}>
            <SelectTrigger className="h-9 w-full"><SelectValue placeholder="—" /></SelectTrigger>
            <SelectContent>{creds.map((c) => <SelectItem key={c.id} value={c.id}>{c.name} ({c.provider})</SelectItem>)}</SelectContent>
          </Select>
        </Field>
        <Field label={t('region')} hint={t('regionHint')}>
          <LiveSelect<CloudRegion>
            value={form.region || ''} onChange={(v) => u('region', v)} placeholder="cn-hangzhou"
            autoLoadKey={form.cloud_id}
            load={() => listCloudRegions({ cloud_id: form.cloud_id })}
            getId={(r) => r.id} getLabel={(r) => (r.local_name ? `${r.local_name} · ${r.id}` : r.id)}
            autoSelect={(items) => {
              const def = creds.find((c) => c.id === form.cloud_id)?.default_region
              return def && items.some((r) => r.id === def) ? def : items[0]?.id
            }}
            loadLabel={t('loadRegions')}
          />
        </Field>
        <div className="sm:col-span-2">
          <Field label={t('imageId')} hint={t('imageHint')}>
            <LiveSelect<CloudImage>
              value={form.image_id} onChange={(v) => u('image_id', v)} placeholder="ubuntu_22_04_x64 / img-xxxx"
              autoLoadKey={imgKey}
              load={() => listCloudImages({ cloud_id: form.cloud_id, region: form.region })}
              getId={(im) => im.id} getLabel={imageLabel} autoSelect={(items) => items[0]?.id}
              loadLabel={t('loadImages')}
            />
          </Field>
        </div>
        <div className="sm:col-span-2">
          <Field label={t('instanceType')} hint={t('instanceTypeHint')}>
            <LiveSelect<CloudInstanceType>
              value={form.instance_type} onChange={(v) => u('instance_type', v)} placeholder="ecs.t6-c1m1.large / S5.MEDIUM2"
              autoLoadKey={imgKey}
              load={() => listCloudInstanceTypes({ cloud_id: form.cloud_id, region: form.region })}
              getId={(tp) => tp.id} getLabel={typeLabel}
              autoSelect={(items) => (items.find((x) => x.cpu >= 2 && x.memory_gib >= 4) ?? items.find((x) => x.cpu >= 2) ?? items[0])?.id}
              loadLabel={t('loadInstanceTypes')}
            />
          </Field>
        </div>
        <Field label={t('count')}><Input type="number" min={1} value={form.count} onChange={(e) => u('count', parseInt(e.target.value, 10) || 1)} /></Field>
      </div>

      {/* Advanced: optional networking, lifecycle and per-node overrides. */}
      <button
        type="button" onClick={() => setShowAdvanced((v) => !v)}
        className="flex items-center gap-1 text-xs font-medium text-muted-foreground hover:text-foreground"
      >
        <ChevronRight className={`h-3.5 w-3.5 transition-transform ${showAdvanced ? 'rotate-90' : ''}`} />
        {t('advanced')}
      </button>

      {showAdvanced && (
        <div className="space-y-3 rounded-lg border border-border/50 bg-background/30 p-3">
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label={t('bandwidth')} hint={t('bandwidthHint')}><Input type="number" min={0} value={form.bandwidth_out ?? ''} onChange={(e) => u('bandwidth_out', parseInt(e.target.value, 10) || 0)} placeholder="5" /></Field>
            <Field label={t('space')} hint={t('spaceHint')}><Input value={form.space || ''} onChange={(e) => u('space', e.target.value)} placeholder="default" /></Field>
            <Field label={t('zone')}><Input value={form.zone_id || ''} onChange={(e) => u('zone_id', e.target.value)} placeholder="cn-hangzhou-i" /></Field>
            <Field label={t('securityGroup')} hint={t('networkAutoHint')}><Input value={form.security_group_id || ''} onChange={(e) => u('security_group_id', e.target.value)} placeholder="sg-xxxx" /></Field>
            <Field label={t('vswitch')}><Input value={form.vswitch_id || ''} onChange={(e) => u('vswitch_id', e.target.value)} placeholder="vsw-xxxx / subnet-xxxx" /></Field>
            <Field label={t('vpc')}><Input value={form.vpc_id || ''} onChange={(e) => u('vpc_id', e.target.value)} placeholder="vpc-xxxx" /></Field>
            <Field label={t('ttl')} hint={t('ttlHint')}><Input type="number" min={0} value={form.ttl_minutes ?? ''} onChange={(e) => u('ttl_minutes', parseInt(e.target.value, 10) || 0)} placeholder="0" /></Field>
          </div>
          <Field label={t('overrides')} hint={t('overridesHint')}>
            <Input value={overridesText} onChange={(e) => setOverridesText(e.target.value)} placeholder="provider=openai, model=gpt-4.1" />
          </Field>
          <label className="flex items-center gap-2 text-xs text-muted-foreground">
            <input type="checkbox" checked={!!form.recycle_when_idle} onChange={(e) => u('recycle_when_idle', e.target.checked)} className="rounded border-border" />
            {t('recycleWhenIdle')}
          </label>
        </div>
      )}

      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" disabled={busy || !form.cloud_id} onClick={() => void run(true)}>{t('dryRun')}</Button>
        <Button type="button" disabled={busy || !form.cloud_id || !form.image_id || !form.instance_type} onClick={() => void run(false)}>
          {busy ? <><Spinner className="h-4 w-4" />{t('launching')}</> : t('launch')}
        </Button>
      </div>

      {script && (
        <div>
          <SectionTitle className="mb-1.5">{t('scriptPreview')}</SectionTitle>
          <pre className="max-h-64 overflow-auto rounded-md border border-border/60 bg-background/60 p-3 text-xs text-foreground">{script}</pre>
        </div>
      )}
    </div>
  )
}

function imageLabel(im: CloudImage): string {
  const os = im.os_name || im.name || im.id
  return im.arch ? `${os} · ${im.arch}` : os
}

function typeLabel(tp: CloudInstanceType): string {
  return `${tp.id} · ${tp.cpu}C${tp.memory_gib}G`
}
