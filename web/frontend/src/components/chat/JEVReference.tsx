import type { ElementType } from 'react'
import { ArrowUpRight } from 'lucide-react'
import './JEVReference.css'

export function JEVReference({ label, identity, icon: Icon, disabled = false, onClick, className = '' }: { label: string; identity?: string; icon?: ElementType; disabled?: boolean; onClick: () => void; className?: string }) {
  return <button type="button" className={`jev-reference ${className}`} disabled={disabled} onClick={onClick} title={identity ? `${label}\n${identity}` : label}>
    {Icon && <Icon aria-hidden="true" />}<strong>{label}</strong>{identity && <code className="sr-only">{identity}</code>}<ArrowUpRight aria-hidden="true" />
  </button>
}
