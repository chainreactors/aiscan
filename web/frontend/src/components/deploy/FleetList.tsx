import { useTranslation } from 'react-i18next'
import { Trash2, Server } from 'lucide-react'
import { recycleDeploy, recycleAllDeploys } from '../../api'
import type { DeployRecordView, NodeProgress } from '../../api'
import { Button, Badge, Spinner } from '@aspect/ui'
import { SectionTitle } from './fields'
import { useConfirm } from '../ConfirmDialog'

// --- Fleet tab ---

// bootProgressLabel turns a node's pre-registration progress into a short label
// (e.g. "下载 agent 42%"), or null when there's no fresh report — anything older
// than 2 min is treated as stale so we don't freeze a percentage on a dead node.
function bootProgressLabel(p: NodeProgress | undefined, t: (k: string) => string): string | null {
  if (!p || (p.age_sec ?? 0) > 120) return null
  const phase = t(`bootPhase.${p.phase}`)
  if (p.phase === 'downloading') {
    if (p.total && p.bytes != null) {
      const pct = Math.min(100, Math.max(0, Math.floor((p.bytes / p.total) * 100)))
      return `${phase} ${pct}%`
    }
    if (p.bytes) return `${phase} ${(p.bytes / 1048576).toFixed(0)}MB`
  }
  return `${phase}…`
}

export function FleetTab({ deploys, loading, onReload, setError }: {
  deploys: DeployRecordView[]; loading: boolean; onReload: () => Promise<void>; setError: (v: string) => void
}) {
  const { t } = useTranslation('deploy')
  const confirm = useConfirm()

  const doRecycle = async (id: string) => {
    if (!(await confirm({ description: t('confirmRecycle'), destructive: true }))) return
    setError('')
    try { await recycleDeploy(id); await onReload() } catch (err: any) { setError(err.message) }
  }
  const doRecycleAll = async () => {
    if (!(await confirm({ description: t('confirmRecycleAll'), destructive: true }))) return
    setError('')
    try { await recycleAllDeploys(); await onReload() } catch (err: any) { setError(err.message) }
  }
  const doRecycleNode = async (deployID: string, instanceID: string) => {
    if (!(await confirm({ description: t('confirmRecycleNode', { id: instanceID }), destructive: true }))) return
    setError('')
    try { await recycleDeploy(deployID, [instanceID]); await onReload() } catch (err: any) { setError(err.message) }
  }

  const active = deploys.filter((d) => d.status !== 'recycled')

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <SectionTitle>{t('fleet')}</SectionTitle>
        <Button type="button" variant="outline" size="sm" disabled={active.length === 0} onClick={() => void doRecycleAll()}>
          <Trash2 className="h-3.5 w-3.5" />{t('recycleAll')}
        </Button>
      </div>

      {loading && deploys.length === 0 && <div className="flex h-24 items-center justify-center text-muted-foreground"><Spinner className="mr-2 h-4 w-4" />{t('loading')}</div>}
      {!loading && deploys.length === 0 && <div className="rounded-md border border-dashed border-border px-3 py-6 text-center text-xs text-muted-foreground">{t('noDeploys')}</div>}

      {deploys.map((d) => (
        <div key={d.id} className="rounded-lg border border-border/60 bg-background/40">
          <div className="border-b border-border/40 px-3 py-2">
            <div className="flex items-center justify-between gap-3">
              <div className="flex min-w-0 items-center gap-2 text-sm">
                <Badge variant="secondary" className="text-xs uppercase">{d.provider}</Badge>
                <span className="truncate font-mono text-xs text-foreground">{d.id}</span>
                <StatusBadge status={d.status} />
                <span className="whitespace-nowrap text-xs text-muted-foreground">{d.region} · {d.space}</span>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <span className="text-xs text-muted-foreground">
                  {d.nodes.length} {t('nodes')} · {d.registered_count} {t('registered')}
                  {d.orphans > 0 && <span className="ml-1 text-warning">· {d.orphans} {t('orphans')}</span>}
                </span>
                {d.status !== 'recycled' && (
                  <button type="button" className="rounded-md p-1.5 text-muted-foreground hover:bg-destructive/10 hover:text-destructive" onClick={() => void doRecycle(d.id)} title={t('recycle')}>
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                )}
              </div>
            </div>
            <DeployPhaseLine deploy={d} />
          </div>
          {d.error && <div className="px-3 py-1 text-xs text-warning">{d.error}</div>}
          <div className="divide-y divide-border/30">
            {d.nodes.map((n) => (
              <div key={n.instance_id} className="flex items-center justify-between px-3 py-1.5 text-xs">
                <div className="flex items-center gap-2">
                  <Server className={`h-3 w-3 ${n.registered ? 'text-success' : 'text-warning'}`} />
                  <span className="font-mono text-foreground">{n.node_name}</span>
                  <span className="font-mono text-muted-foreground">{n.instance_id}</span>
                  {n.public_ip && <span className="text-muted-foreground">{n.public_ip}</span>}
                </div>
                <div className="flex items-center gap-2">
                  <span className={n.registered ? 'text-success' : 'text-warning'}>
                    {n.registered ? t('nodeRegistered') : (bootProgressLabel(n.progress, t) ?? t('nodeOrphan'))}{n.busy ? ' · busy' : ''}
                  </span>
                  {d.status !== 'recycled' && n.instance_id && (
                    <button
                      type="button"
                      className="rounded-md p-1 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                      onClick={() => void doRecycleNode(d.id, n.instance_id)}
                      title={t('recycleNode')}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}

function StatusBadge({ status }: { status: string }) {
  const variant = status === 'active' ? 'success' : status === 'failed' ? 'destructive' : status === 'recycled' ? 'secondary' : 'warning'
  return <Badge variant={variant} className="text-xs">{status}</Badge>
}

function DeployPhaseLine({ deploy }: { deploy: DeployRecordView }) {
  const { t } = useTranslation('deploy')
  const phase = effectiveDeployPhase(deploy)
  const total = deploy.desired_count || deploy.nodes.length
  const target = Math.max(total, deploy.nodes.length)
  const label = t(`phase.${phase}`, { defaultValue: phase || deploy.status })
  let detail = ''

  switch (phase) {
    case 'preparing':
      detail = t('phaseDetail.preparing', { total: target })
      break
    case 'ensuring_network':
      detail = t('phaseDetail.ensuringNetwork')
      break
    case 'launching_instances':
      detail = t('phaseDetail.launching', { created: deploy.nodes.length, total: target })
      break
    case 'waiting_registration':
      detail = t('phaseDetail.waitingRegistration', { registered: deploy.registered_count, total: deploy.nodes.length || target })
      break
    case 'ready':
      detail = t('phaseDetail.ready', { total: deploy.nodes.length })
      break
    case 'recycling':
      detail = t('phaseDetail.recycling')
      break
    case 'recycled':
      detail = t('phaseDetail.recycled')
      break
    case 'failed':
      detail = deploy.error || t('phaseDetail.failed')
      break
    default:
      detail = deploy.error || t('phaseDetail.unknown')
  }

  return (
    <div className="mt-1 flex min-w-0 items-center gap-2 text-xs text-muted-foreground">
      <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${phaseDotClass(phase)}`} />
      <span className="shrink-0 font-medium text-foreground/80">{label}</span>
      <span className="min-w-0 truncate">{detail}</span>
    </div>
  )
}

function effectiveDeployPhase(deploy: DeployRecordView): string {
  if (deploy.phase) return deploy.phase
  if (deploy.status === 'failed') return 'failed'
  if (deploy.status === 'recycled') return 'recycled'
  if (deploy.status === 'pending') return 'preparing'
  if (deploy.status === 'active') {
    if (deploy.nodes.length > 0 && deploy.registered_count >= deploy.nodes.length) return 'ready'
    return deploy.nodes.length > 0 ? 'waiting_registration' : 'launching_instances'
  }
  return deploy.status || 'unknown'
}

function phaseDotClass(phase: string): string {
  if (phase === 'ready') return 'bg-success'
  if (phase === 'failed') return 'bg-destructive'
  if (phase === 'recycled') return 'bg-muted-foreground/50'
  if (phase === 'recycling' || phase === 'ensuring_network' || phase === 'launching_instances') return 'bg-warning'
  return 'bg-primary'
}
