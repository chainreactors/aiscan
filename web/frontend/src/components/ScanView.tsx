import { useEffect, useState, type ReactNode } from 'react'
import { FileText, TableProperties } from 'lucide-react'
import type { ScanJob, ScanResult } from '../api'
import ScanProgress from './ScanProgress'
import ReportView from './ReportView'
import AssetResultView from './AssetResultView'
import { cn } from '@/lib/utils'

interface ScanViewProps {
  scan: ScanJob
  lines: string[]
  report: string
  result: ScanResult | null
  logCollapsed: boolean
  onToggleLog: () => void
}

export default function ScanView({ scan, lines, report, result, logCollapsed, onToggleLog }: ScanViewProps) {
  const hasReport = !!report
  const hasResult = !!result
  const isRunning = scan.status === 'running'
  const verifyEnabled = !!scan.verify || (!!scan.ai && !scan.sniper)
  const sniperEnabled = !!scan.sniper || (!!scan.ai && !scan.verify)
  const [tab, setTab] = useState<'assets' | 'report'>('assets')

  useEffect(() => {
    if (!hasResult && hasReport) {
      setTab('report')
    } else if (hasResult) {
      setTab('assets')
    }
  }, [hasReport, hasResult, scan.id])

  return (
    <div className="space-y-4">
      {/* Target info */}
      <div className="flex flex-wrap items-center gap-3">
        <span className="font-mono text-sm text-foreground">{scan.target}</span>
        <span className="text-xs text-muted-foreground px-2 py-0.5 rounded bg-secondary">{scan.mode}</span>
        {verifyEnabled && <span className="text-xs text-cyber-700 dark:text-cyber-300 px-2 py-0.5 rounded bg-cyber-500/10">Verify</span>}
        {sniperEnabled && <span className="text-xs text-red-700 dark:text-red-300 px-2 py-0.5 rounded bg-red-400/10">Sniper</span>}
        {scan.deep && <span className="text-xs text-yellow-700 dark:text-yellow-300 px-2 py-0.5 rounded bg-yellow-400/10">Deep</span>}
        <StatusIndicator status={scan.status} />
      </div>

      {/* Progress section (always shown if we have lines or are running) */}
      {(lines.length > 0 || isRunning) && (
        <ScanProgress
          lines={lines}
          status={scan.status}
          collapsed={logCollapsed}
          onToggleCollapse={onToggleLog}
        />
      )}

      {(hasResult || hasReport) && (
        <div className="space-y-3">
          {hasResult && hasReport && (
            <div className="inline-flex items-center rounded-md border border-input bg-secondary/50 p-0.5">
              <ResultTabButton active={tab === 'assets'} onClick={() => setTab('assets')}>
                <TableProperties className="h-3.5 w-3.5" />
                <span>Assets</span>
              </ResultTabButton>
              <ResultTabButton active={tab === 'report'} onClick={() => setTab('report')}>
                <FileText className="h-3.5 w-3.5" />
                <span>Narrative</span>
              </ResultTabButton>
            </div>
          )}

          {hasResult && tab === 'assets' && <AssetResultView result={result} />}

          {hasReport && tab === 'report' && (
            <div className="animate-fade-in">
              <ReportView report={report} />
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function ResultTabButton({
  active,
  children,
  onClick,
}: {
  active: boolean
  children: ReactNode
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'inline-flex items-center gap-1.5 rounded-sm px-3 py-1.5 text-xs font-medium transition-all',
        active ? 'bg-primary text-primary-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground',
      )}
    >
      {children}
    </button>
  )
}

function StatusIndicator({ status }: { status: string }) {
  const config: Record<string, { label: string; className: string }> = {
    queued: { label: 'Queued', className: 'text-gray-600 bg-gray-400/10 dark:text-gray-400' },
    running: { label: 'Running', className: 'text-blue-700 bg-blue-400/10 dark:text-blue-400 animate-pulse' },
    completed: { label: 'Completed', className: 'text-cyber-700 bg-cyber-400/10 dark:text-cyber-400' },
    failed: { label: 'Failed', className: 'text-red-700 bg-red-400/10 dark:text-red-400' },
    cancelled: { label: 'Cancelled', className: 'text-yellow-700 bg-yellow-400/10 dark:text-yellow-400' },
  }
  const { label, className } = config[status] || config.queued
  return (
    <span className={`text-[10px] font-medium px-2 py-0.5 rounded-full ${className}`}>
      {label}
    </span>
  )
}
