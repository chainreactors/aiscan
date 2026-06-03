import { useEffect, useState, type ReactNode } from 'react'
import { FileText, TableProperties } from 'lucide-react'
import type { ScanJob, StructuredResult } from '../api'
import SummaryCards from './SummaryCards'
import ScanProgress from './ScanProgress'
import ReportView from './ReportView'
import StructuredResultView from './StructuredResultView'
import { cn } from '@/lib/utils'

interface ScanViewProps {
  scan: ScanJob
  lines: string[]
  report: string
  result: StructuredResult | null
  logCollapsed: boolean
  onToggleLog: () => void
}

export default function ScanView({ scan, lines, report, result, logCollapsed, onToggleLog }: ScanViewProps) {
  const hasReport = !!report
  const hasResult = !!result
  const isRunning = scan.status === 'running'
  const verifyEnabled = !!scan.verify || (!!scan.ai && !scan.sniper)
  const sniperEnabled = !!scan.sniper || (!!scan.ai && !scan.verify)
  const [tab, setTab] = useState<'structured' | 'report'>('structured')

  useEffect(() => {
    if (!hasResult && hasReport) {
      setTab('report')
    } else if (hasResult) {
      setTab('structured')
    }
  }, [hasReport, hasResult, scan.id])

  return (
    <div className="space-y-4">
      {/* Target info */}
      <div className="flex flex-wrap items-center gap-3">
        <span className="font-mono text-sm text-foreground">{scan.target}</span>
        <span className="text-xs text-muted-foreground px-2 py-0.5 rounded bg-secondary">{scan.mode}</span>
        {verifyEnabled && <span className="text-xs text-cyber-300 px-2 py-0.5 rounded bg-cyber-500/10">Verify</span>}
        {sniperEnabled && <span className="text-xs text-red-300 px-2 py-0.5 rounded bg-red-400/10">Sniper</span>}
        {scan.deep && <span className="text-xs text-yellow-300 px-2 py-0.5 rounded bg-yellow-400/10">Deep</span>}
        <StatusIndicator status={scan.status} />
      </div>

      {/* Summary cards */}
      {(lines.length > 0 || result) && <SummaryCards lines={lines} result={result} />}

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
              <ResultTabButton active={tab === 'structured'} onClick={() => setTab('structured')}>
                <TableProperties className="h-3.5 w-3.5" />
                <span>Structured</span>
              </ResultTabButton>
              <ResultTabButton active={tab === 'report'} onClick={() => setTab('report')}>
                <FileText className="h-3.5 w-3.5" />
                <span>Narrative</span>
              </ResultTabButton>
            </div>
          )}

          {hasResult && tab === 'structured' && <StructuredResultView result={result} />}

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
    queued: { label: 'Queued', className: 'text-gray-400 bg-gray-400/10' },
    running: { label: 'Running', className: 'text-blue-400 bg-blue-400/10 animate-pulse' },
    completed: { label: 'Completed', className: 'text-cyber-400 bg-cyber-400/10' },
    failed: { label: 'Failed', className: 'text-red-400 bg-red-400/10' },
    cancelled: { label: 'Cancelled', className: 'text-yellow-400 bg-yellow-400/10' },
  }
  const { label, className } = config[status] || config.queued
  return (
    <span className={`text-[10px] font-medium px-2 py-0.5 rounded-full ${className}`}>
      {label}
    </span>
  )
}
