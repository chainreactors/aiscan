import { useMemo } from 'react'
import { Server, Globe, Fingerprint, Brain } from 'lucide-react'
import type { StructuredResult } from '../api'

interface SummaryCardsProps {
  lines: string[]
  result?: StructuredResult | null
}

interface SummaryCard {
  key: string
  label: string
  count: number
  icon: React.ReactNode
  color: string
}

export default function SummaryCards({ lines, result }: SummaryCardsProps) {
  const findings = useMemo(() => {
    if (result) {
      const assetItems = (result.assets || []).reduce((sum, asset) => sum + (asset.items || []).filter((item) => item.kind !== 'path').length, 0)
      return {
        assets: result.assets?.length || 0,
        ports: result.summary.services,
        web: result.summary.webs || result.summary.probes,
        fingerprints: result.summary.fingerprints,
        items: assetItems || result.summary.risks + result.summary.vulns + (result.ai?.length || 0) + result.summary.errors,
      }
    }

    let assets = 0, ports = 0, web = 0, fingerprints = 0, items = 0
    for (const line of lines) {
      const l = line.toLowerCase()
      if (l.includes('[asset')) assets++
      if (l.includes('[service')) ports++
      else if (l.includes('[web')) web++
      else if (l.includes('[fingerprint')) fingerprints++
      else if (l.includes('[risk') || l.includes('weakpass') || l.includes('[vuln') || l.includes('[ai') || l.includes('[sniper') || l.includes('[deep') || l.includes('verified')) items++
    }
    return { assets, ports, web, fingerprints, items }
  }, [lines, result])

  const cards: SummaryCard[] = [
    { key: 'assets', label: 'Assets', count: findings.assets, icon: <Server className="w-3.5 h-3.5" />, color: 'text-cyber-300 bg-cyber-500/10 border-cyber-400/20' },
    { key: 'ports', label: 'Ports', count: findings.ports, icon: <Server className="w-3.5 h-3.5" />, color: 'text-green-400 bg-green-400/10 border-green-400/20' },
    { key: 'web', label: 'Web', count: findings.web, icon: <Globe className="w-3.5 h-3.5" />, color: 'text-green-400 bg-green-400/10 border-green-400/20' },
    { key: 'fingerprints', label: 'Fingerprints', count: findings.fingerprints, icon: <Fingerprint className="w-3.5 h-3.5" />, color: 'text-yellow-400 bg-yellow-400/10 border-yellow-400/20' },
    { key: 'items', label: 'Items', count: findings.items, icon: <Brain className="w-3.5 h-3.5" />, color: 'text-cyber-300 bg-cyber-500/10 border-cyber-400/20' },
  ]

  const visible = cards.filter(c => c.count > 0)
  if (visible.length === 0) return null

  return (
    <div className="flex flex-wrap gap-2 animate-fade-in">
      {visible.map(card => (
        <div
          key={card.key}
          className={`inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-xs font-medium ${card.color}`}
        >
          {card.icon}
          <span>{card.count}</span>
          <span className="opacity-70">{card.label}</span>
        </div>
      ))}
    </div>
  )
}
