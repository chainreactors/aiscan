import { useEffect, useState, type ReactNode } from 'react'
import { Brain, Crosshair, Loader2, Play, Radar, Search } from 'lucide-react'
import type { ScanOptions } from '../api'
import { Input } from './ui/input'
import { Button } from './ui/button'
import { ToggleGroup, ToggleGroupItem } from './ui/toggle-group'
import { cn } from '@/lib/utils'

interface ScanFormProps {
  onSubmit: (target: string, mode: string, options: ScanOptions) => void
  disabled: boolean
  analysisAvailable: boolean
}

export default function ScanForm({ onSubmit, disabled, analysisAvailable }: ScanFormProps) {
  const [target, setTarget] = useState('')
  const [mode, setMode] = useState('quick')
  const [options, setOptions] = useState<ScanOptions>({ verify: false, sniper: false, deep: false })

  useEffect(() => {
    if (!analysisAvailable) {
      setOptions({ verify: false, sniper: false, deep: false })
    }
  }, [analysisAvailable])

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!target.trim()) return
    onSubmit(target.trim(), mode, options)
  }

  const toggleOption = (key: keyof ScanOptions) => {
    setOptions((current) => ({ ...current, [key]: !current[key] }))
  }

  const analysisOptions: Array<{
    key: keyof ScanOptions
    label: string
    icon: ReactNode
    activeClass: string
  }> = [
    {
      key: 'verify',
      label: 'Verify',
      icon: <Brain className="h-4 w-4" />,
      activeClass: 'border-cyber-500/40 bg-cyber-500/15 text-cyber-300',
    },
    {
      key: 'sniper',
      label: 'Sniper',
      icon: <Crosshair className="h-4 w-4" />,
      activeClass: 'border-red-400/40 bg-red-400/15 text-red-300',
    },
    {
      key: 'deep',
      label: 'Deep',
      icon: <Radar className="h-4 w-4" />,
      activeClass: 'border-yellow-400/40 bg-yellow-400/15 text-yellow-300',
    },
  ]

  return (
    <form onSubmit={handleSubmit} className="flex flex-wrap items-center gap-3">
      <div className="relative min-w-[16rem] flex-1">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
        <Input
          value={target}
          onChange={(e) => setTarget(e.target.value)}
          placeholder="Target — IP, hostname, or URL"
          disabled={disabled}
          className="pl-9 h-10 font-mono text-sm bg-secondary/50 border-border focus-visible:ring-cyber-500/50"
        />
      </div>

      <ToggleGroup value={mode} onValueChange={setMode} disabled={disabled}>
        <ToggleGroupItem value="quick">Quick</ToggleGroupItem>
        <ToggleGroupItem value="full">Full</ToggleGroupItem>
      </ToggleGroup>

      <div className="inline-flex flex-wrap items-center gap-2">
        {analysisOptions.map((item) => {
          const active = options[item.key]
          const optionDisabled = disabled || !analysisAvailable
          return (
            <button
              key={item.key}
              type="button"
              aria-pressed={active}
              title={analysisAvailable ? item.label : 'LLM Offline'}
              disabled={optionDisabled}
              onClick={() => toggleOption(item.key)}
              className={cn(
                'inline-flex h-10 items-center gap-2 rounded-md border px-3 text-xs font-medium transition-colors',
                active ? item.activeClass : 'border-input bg-secondary/50 text-muted-foreground hover:text-foreground',
                optionDisabled && 'cursor-not-allowed opacity-50',
              )}
            >
              {item.icon}
              <span>{item.label}</span>
            </button>
          )
        })}
        {!analysisAvailable && (
          <span className="inline-flex h-10 items-center rounded-md border border-yellow-400/20 bg-yellow-400/10 px-3 text-xs font-medium text-yellow-300">
            LLM Offline
          </span>
        )}
      </div>

      <Button
        type="submit"
        disabled={disabled || !target.trim()}
        className="h-10 px-5 bg-cyber-600 hover:bg-cyber-500 text-white"
      >
        {disabled ? (
          <Loader2 className="w-4 h-4 animate-spin" />
        ) : (
          <Play className="w-4 h-4" />
        )}
        <span className="hidden sm:inline">{disabled ? 'Scanning' : 'Scan'}</span>
      </Button>
    </form>
  )
}
