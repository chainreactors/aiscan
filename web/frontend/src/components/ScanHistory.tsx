import type { ScanJob } from '../api'

interface ScanHistoryProps {
  scans: ScanJob[]
  activeId?: string
  onSelect: (scan: ScanJob) => void
}

export default function ScanHistory({ scans, activeId, onSelect }: ScanHistoryProps) {
  if (scans.length === 0) {
    return (
      <div className="text-muted-foreground/60 text-xs text-center py-6">
        No scans yet.
      </div>
    )
  }

  return (
    <div className="space-y-1">
      {scans.map((scan) => {
        const verifyEnabled = !!scan.verify || (!!scan.ai && !scan.sniper)
        const sniperEnabled = !!scan.sniper || (!!scan.ai && !scan.verify)
        return (
          <button
            key={scan.id}
            onClick={() => onSelect(scan)}
            className={`w-full text-left px-2.5 py-2 rounded-md transition-colors ${
              scan.id === activeId
                ? 'bg-accent border border-cyber-600/30'
                : 'hover:bg-accent/50 border border-transparent'
            }`}
          >
            <div className="flex items-center justify-between gap-2">
              <span className="text-xs font-mono text-foreground truncate">
                {scan.target}
              </span>
              <StatusDot status={scan.status} />
            </div>
            <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5">
              <span className="text-[10px] text-muted-foreground">{scan.mode}</span>
              {verifyEnabled && <span className="text-[10px] text-cyber-300">Verify</span>}
              {sniperEnabled && <span className="text-[10px] text-red-300">Sniper</span>}
              {scan.deep && <span className="text-[10px] text-yellow-300">Deep</span>}
              <span className="text-[10px] text-muted-foreground/60">{formatTime(scan.created_at)}</span>
            </div>
          </button>
        )
      })}
    </div>
  )
}

function StatusDot({ status }: { status: string }) {
  const colors: Record<string, string> = {
    queued: 'bg-gray-500',
    running: 'bg-blue-400 animate-pulse',
    completed: 'bg-green-400',
    failed: 'bg-red-400',
    cancelled: 'bg-yellow-400',
  }
  return (
    <span className={`w-2 h-2 rounded-full shrink-0 ${colors[status] || colors.queued}`} />
  )
}

function formatTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString(undefined, {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    })
  } catch {
    return iso
  }
}
