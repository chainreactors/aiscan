import { Shield, PanelLeftClose, PanelLeft, History } from 'lucide-react'
import { Button } from './ui/button'
import { Tooltip } from './ui/tooltip'
import { Badge } from './ui/badge'
import ScanHistory from './ScanHistory'
import type { ScanJob } from '../api'

interface SidebarProps {
  open: boolean
  onToggle: () => void
  scans: ScanJob[]
  activeId?: string
  onSelectScan: (scan: ScanJob) => void
}

export default function Sidebar({ open, onToggle, scans, activeId, onSelectScan }: SidebarProps) {
  const runningCount = scans.filter(s => s.status === 'running').length

  return (
    <aside
      className={`flex flex-col border-r border-border bg-card/50 backdrop-blur-sm transition-all duration-200 ease-in-out shrink-0 ${
        open ? 'w-72' : 'w-12'
      }`}
    >
      <div className={`flex items-center border-b border-border ${open ? 'p-3 gap-3' : 'p-2 flex-col gap-2'}`}>
        {open ? (
          <>
            <Shield className="w-5 h-5 text-cyber-400 shrink-0" />
            <div className="flex-1 min-w-0">
              <h1 className="text-sm font-bold text-cyber-400">AIScan</h1>
            </div>
            <Button variant="ghost" size="icon" onClick={onToggle} className="h-7 w-7 text-muted-foreground">
              <PanelLeftClose className="w-4 h-4" />
            </Button>
          </>
        ) : (
          <>
            <Tooltip content="AIScan" side="right">
              <button onClick={onToggle} className="p-1 rounded-md hover:bg-accent transition-colors">
                <Shield className="w-5 h-5 text-cyber-400" />
              </button>
            </Tooltip>
          </>
        )}
      </div>

      {open ? (
        <div className="flex-1 overflow-auto p-3 animate-fade-in">
          <div className="flex items-center justify-between mb-3">
            <h2 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
              History
            </h2>
            {runningCount > 0 && (
              <Badge className="bg-blue-900/50 text-blue-400 border-blue-800 text-[10px] px-1.5">
                {runningCount} running
              </Badge>
            )}
          </div>
          <ScanHistory scans={scans} activeId={activeId} onSelect={onSelectScan} />
        </div>
      ) : (
        <div className="flex flex-col items-center gap-2 pt-3">
          <Tooltip content={`${scans.length} scans`} side="right">
            <button onClick={onToggle} className="p-1.5 rounded-md hover:bg-accent transition-colors relative">
              <History className="w-4 h-4 text-muted-foreground" />
              {scans.length > 0 && (
                <span className="absolute -top-0.5 -right-0.5 w-3.5 h-3.5 bg-cyber-600 rounded-full text-[8px] font-bold flex items-center justify-center text-white">
                  {scans.length > 9 ? '9+' : scans.length}
                </span>
              )}
            </button>
          </Tooltip>
          <Tooltip content="Expand sidebar" side="right">
            <Button variant="ghost" size="icon" onClick={onToggle} className="h-7 w-7 text-muted-foreground">
              <PanelLeft className="w-3.5 h-3.5" />
            </Button>
          </Tooltip>
        </div>
      )}
    </aside>
  )
}
