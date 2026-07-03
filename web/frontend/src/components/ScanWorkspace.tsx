import { useState, type ReactNode } from 'react'
import {
  AlertTriangle,
  CheckCircle2,
  Circle,
  History,
  Info,
  Shield,
  X,
} from 'lucide-react'
import type { ScanJob, ScanOptions, ScanResult } from '../api'
import ScanForm from './ScanForm'
import ScanView from './ScanView'
import { cn } from '@aspect/theme'
import { Tooltip, TooltipContent, TooltipTrigger } from '@aspect/ui'
import { DetailGroup, DetailRow, formatDateTime } from '@aspect/terminal'

interface ScanWorkspaceProps {
  scans: ScanJob[]
  activeScan: ScanJob | null
  lines: string[]
  report: string
  result: ScanResult | null
  scanning: boolean
  error: string
  logCollapsed: boolean
  analysisAvailable: boolean
  onSubmit: (target: string, mode: string, options: ScanOptions) => void
  onToggleLog: () => void
  onClearError: () => void
}

export default function ScanWorkspace({
  activeScan,
  analysisAvailable,
  error,
  lines,
  logCollapsed,
  onClearError,
  onSubmit,
  onToggleLog,
  report,
  result,
  scanning,
  scans,
}: ScanWorkspaceProps) {
  const [detailsOpen, setDetailsOpen] = useState(false)

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col bg-card">
      {/* Scan form */}
      <div className="shrink-0 border-b border-border bg-card/85 px-3 py-2">
        <div className="flex items-center gap-2">
          <div className="min-w-0 flex-1">
            <ScanForm
              onSubmit={onSubmit}
              disabled={scanning}
              analysisAvailable={analysisAvailable}
            />
          </div>
          {activeScan && (
            <IconButton
              active={detailsOpen}
              label={detailsOpen ? 'Hide details' : 'Show details'}
              onClick={() => setDetailsOpen((v) => !v)}
            >
              <Info className="h-3.5 w-3.5" />
            </IconButton>
          )}
        </div>
      </div>

      {error && (
        <div
          role="alert"
          className="mx-3 mt-3 flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive"
        >
          <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
          <span className="min-w-0 flex-1 break-words">{error}</span>
          <button
            type="button"
            aria-label="Dismiss error"
            onClick={onClearError}
            className="rounded p-0.5 text-destructive/70 hover:bg-destructive/10 hover:text-destructive"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      )}

      <div className="flex min-h-0 min-w-0 flex-1">
        <section className="flex min-h-0 min-w-0 flex-1 flex-col">
          {activeScan ? (
            <div className="min-h-0 flex-1 overflow-auto p-3">
              <ScanView
                scan={activeScan}
                lines={lines}
                report={report}
                result={result}
                logCollapsed={logCollapsed}
                onToggleLog={onToggleLog}
              />
            </div>
          ) : (
            <EmptyScanConsole
              analysisAvailable={analysisAvailable}
              completed={scans.filter((s) => s.status === 'completed').length}
              running={scans.filter((s) => s.status === 'running' || s.status === 'queued').length}
              total={scans.length}
            />
          )}
        </section>

        {detailsOpen && activeScan && (
          <ScanDetails
            lines={lines.length}
            result={result || activeScan.result || null}
            scan={activeScan}
            onClose={() => setDetailsOpen(false)}
          />
        )}
      </div>
    </div>
  )
}

function EmptyScanConsole({
  analysisAvailable,
  completed,
  running,
  total,
}: {
  analysisAvailable: boolean
  completed: number
  running: number
  total: number
}) {
  return (
    <div className="flex min-h-0 flex-1 items-center justify-center p-6">
      <div className="space-y-4 text-center">
        <Shield className="mx-auto h-14 w-14 text-primary/15" strokeWidth={1.25} />
        <div className="space-y-1">
          <p className="text-sm font-medium text-foreground">No active scan</p>
          <p className="text-xs text-muted-foreground">Enter a target above to start scanning</p>
        </div>
        <div className="flex flex-wrap justify-center gap-2">
          <Metric icon={<History className="h-3.5 w-3.5" />} label="History" value={total} />
          <Metric icon={<Circle className="h-3.5 w-3.5 fill-current" />} label="Running" value={running} tone={running ? 'ready' : 'muted'} />
          <Metric icon={<CheckCircle2 className="h-3.5 w-3.5" />} label="Completed" value={completed} />
          <Metric
            icon={analysisAvailable ? <CheckCircle2 className="h-3.5 w-3.5" /> : <AlertTriangle className="h-3.5 w-3.5" />}
            label="LLM"
            value={analysisAvailable ? 'Ready' : 'Offline'}
            tone={analysisAvailable ? 'ready' : 'warning'}
          />
        </div>
      </div>
    </div>
  )
}

function ScanDetails({
  lines,
  onClose,
  result,
  scan,
}: {
  lines: number
  onClose: () => void
  result: ScanResult | null
  scan: ScanJob
}) {
  return (
    <aside className="flex max-h-72 w-full shrink-0 flex-col border-t border-border bg-card lg:max-h-none lg:w-80 lg:border-l lg:border-t-0">
      <div className="flex h-10 shrink-0 items-center justify-between border-b border-border px-3">
        <span className="text-xs font-medium uppercase text-muted-foreground">Details</span>
        <IconButton label="Close details" onClick={onClose}>
          <X className="h-3.5 w-3.5" />
        </IconButton>
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-3 text-xs">
        <DetailGroup title="Scan">
          <DetailRow label="Target" value={scan.target} mono />
          <DetailRow label="ID" value={scan.id} mono />
          <DetailRow label="State" value={scanStatusLabel(scan.status)} />
          <DetailRow label="Mode" value={scan.mode} />
          <DetailRow label="Created" value={formatDateTime(scan.created_at)} />
          <DetailRow label="Updated" value={formatDateTime(scan.updated_at)} />
          <DetailRow label="Output" value={lines ? `${lines} lines` : undefined} />
        </DetailGroup>
        <DetailGroup title="Options">
          <DetailRow label="Verify" value={scan.verify ? 'enabled' : undefined} />
          <DetailRow label="Sniper" value={scan.sniper ? 'enabled' : undefined} />
          <DetailRow label="Deep" value={scan.deep ? 'enabled' : undefined} />
          <DetailRow label="AI" value={scan.ai ? 'enabled' : undefined} />
        </DetailGroup>
        <DetailGroup title="Result">
          <DetailRow label="Assets" value={result?.assets?.length} />
          <DetailRow label="Loots" value={scanLootCount(result)} />
          <DetailRow label="Services" value={result?.summary?.services} />
          <DetailRow label="Webs" value={result?.summary?.webs} />
          <DetailRow label="Requests" value={result?.summary?.requests} />
          <DetailRow label="Duration" value={result?.summary?.duration} />
        </DetailGroup>
      </div>
    </aside>
  )
}

function IconButton({
  active,
  children,
  disabled,
  label,
  onClick,
}: {
  active?: boolean
  children: ReactNode
  disabled?: boolean
  label: string
  onClick: () => void
}) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          aria-label={label}
          title={label}
          disabled={disabled}
          onClick={onClick}
          className={cn(
            'inline-flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground disabled:cursor-not-allowed disabled:opacity-40',
            active && 'bg-primary/10 text-primary',
          )}
        >
          {children}
        </button>
      </TooltipTrigger>
      <TooltipContent side="bottom">{label}</TooltipContent>
    </Tooltip>
  )
}

function Metric({
  icon,
  label,
  tone = 'muted',
  value,
}: {
  icon: ReactNode
  label: string
  tone?: 'muted' | 'ready' | 'warning'
  value: ReactNode
}) {
  return (
    <div
      className={cn(
        'inline-flex items-center gap-2 rounded-md border px-2.5 py-1.5 text-xs',
        tone === 'ready' && 'border-primary/25 bg-primary/10 text-primary',
        tone === 'warning' && 'border-yellow-400/25 bg-yellow-400/10 text-yellow-300',
        tone === 'muted' && 'border-border bg-secondary/50 text-muted-foreground',
      )}
    >
      {icon}
      <span className="text-muted-foreground">{label}</span>
      <span className="font-mono text-foreground">{value}</span>
    </div>
  )
}

function scanLootCount(result?: ScanResult | null) {
  if (!result) return 0
  if (result.loots && result.loots.length > 0) {
    return result.loots.filter((loot) => loot.kind.toLowerCase() !== 'fingerprint').length
  }
  return (result.assets || []).reduce((sum, asset) => (
    sum + (asset.items || []).filter((item) => (
      item.kind === 'loot' && dataKind(item.data).toLowerCase() !== 'fingerprint'
    )).length
  ), 0)
}

function dataKind(data?: Record<string, unknown>) {
  const kind = data?.kind
  return typeof kind === 'string' ? kind : ''
}

function scanStatusLabel(status: string) {
  const labels: Record<string, string> = { queued: 'queued', running: 'running', completed: 'done', failed: 'failed', canceled: 'canceled', ready: 'ready' }
  return labels[status] || status || 'ready'
}
