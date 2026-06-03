import { useEffect, useRef } from 'react'
import { ChevronDown, ChevronRight, Terminal } from 'lucide-react'

interface ScanProgressProps {
  lines: string[]
  status: string
  collapsed: boolean
  onToggleCollapse: () => void
}

const STAGES = ['Port Scan', 'Web Probe', 'Credentials', 'Vulns', 'Analysis']

export function detectStage(lines: string[]): number {
  for (let i = lines.length - 1; i >= 0; i--) {
    const line = lines[i].toLowerCase()
    if (line.includes('[ai') || line.includes('sniper') || line.includes('verify') || line.includes('[deep')) return 4
    if (line.includes('[vuln') || line.includes('neutron')) return 3
    if (line.includes('[risk') || line.includes('zombie') || line.includes('weakpass')) return 2
    if (line.includes('[web') || line.includes('[fingerprint') || line.includes('spray')) return 1
    if (line.includes('[service') || line.includes('gogo')) return 0
  }
  return 0
}

export default function ScanProgress({ lines, status, collapsed, onToggleCollapse }: ScanProgressProps) {
  const logRef = useRef<HTMLDivElement>(null)
  const activeStage = detectStage(lines)
  const isRunning = status === 'running'

  useEffect(() => {
    if (logRef.current && !collapsed) {
      logRef.current.scrollTop = logRef.current.scrollHeight
    }
  }, [lines, collapsed])

  return (
    <div className="space-y-3">
      {/* Thin progress bar */}
      <div className="space-y-1.5">
        <div className="flex h-1.5 rounded-full overflow-hidden bg-secondary">
          {STAGES.map((_, i) => (
            <div
              key={i}
              className={`flex-1 transition-all duration-500 ${
                i < activeStage
                  ? 'bg-cyber-500'
                  : i === activeStage && isRunning
                  ? 'bg-cyber-400 animate-pulse'
                  : ''
              } ${i > 0 ? 'ml-px' : ''}`}
            />
          ))}
        </div>
        <div className="flex justify-between px-0.5">
          {STAGES.map((label, i) => (
            <span
              key={i}
              className={`text-[10px] transition-colors ${
                i <= activeStage ? 'text-muted-foreground' : 'text-muted-foreground/40'
              } ${i === activeStage && isRunning ? 'text-cyber-400' : ''}`}
            >
              {label}
            </span>
          ))}
        </div>
      </div>

      {/* Collapsible log output */}
      <div className="rounded-lg border border-border bg-card/50 overflow-hidden">
        <button
          onClick={onToggleCollapse}
          className="w-full flex items-center gap-2 px-3 py-2 text-xs text-muted-foreground hover:bg-accent/50 transition-colors"
        >
          {collapsed ? <ChevronRight className="w-3.5 h-3.5" /> : <ChevronDown className="w-3.5 h-3.5" />}
          <Terminal className="w-3.5 h-3.5" />
          <span>Scan Output</span>
          {lines.length > 0 && (
            <span className="text-muted-foreground/60 ml-auto">{lines.length} lines</span>
          )}
        </button>

        {!collapsed && (
          <div
            ref={logRef}
            className="px-3 pb-3 max-h-64 overflow-y-auto font-mono text-xs leading-relaxed"
          >
            {lines.length === 0 ? (
              <div className="text-muted-foreground/60 flex items-center gap-2 py-2">
                <span className="animate-pulse">Initializing scan...</span>
              </div>
            ) : (
              lines.map((line, i) => (
                <div key={i} className={lineColor(line)}>
                  {line}
                </div>
              ))
            )}
          </div>
        )}
      </div>
    </div>
  )
}

function lineColor(line: string): string {
  const l = line.toLowerCase()
  if (l.includes('[vuln') || l.includes('critical')) return 'text-red-400'
  if (l.includes('[risk') || l.includes('weakpass')) return 'text-orange-400'
  if (l.includes('[ai') || l.includes('verified') || l.includes('sniper') || l.includes('[deep')) return 'text-purple-400'
  if (l.includes('[fingerprint')) return 'text-yellow-400'
  if (l.includes('[web') || l.includes('[service')) return 'text-green-400'
  if (l.includes('[summary') || l.includes('completed')) return 'text-cyan-400'
  if (l.includes('error') || l.includes('failed')) return 'text-red-500'
  return 'text-muted-foreground/80'
}
