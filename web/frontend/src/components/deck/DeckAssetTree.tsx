import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Boxes, ChevronRight } from 'lucide-react'
import { cn } from '@aspect/theme'
import type { HostGroup, ServiceNode } from '../../lib/scan-result'
import { statusCodeTone } from '../../lib/scan-result'
import { badgeToneClass } from '../../lib/tones'
import { SectionHead } from './FindingsStream'
import DeckEmpty from './DeckEmpty'

const INITIAL_OPEN = 3
const MAX_HOSTS = 8

export default function DeckAssetTree({ hosts }: { hosts: HostGroup[] }) {
  const { t } = useTranslation('deck')
  const [open, setOpen] = useState<Set<string>>(() => new Set(hosts.slice(0, INITIAL_OPEN).map((h) => h.id)))
  const [expanded, setExpanded] = useState(false)

  // The lazy initializer above only runs at mount — and this tree mounts on the
  // empty scan view before any result exists, so `hosts` is [] then and the
  // "first three expanded" default would never apply. Seed the open set ONCE,
  // when a scan's hosts first populate. Deliberately do not re-seed as more hosts
  // stream in mid-scan: the host id signature grows on every incremental SSE
  // snapshot, and re-seeding would slam the operator's manual expand/collapse and
  // the "+N more hosts" toggle shut on each update. Switching scans remounts this
  // component (keyed on scan id in OperationDeck), so a fresh scan resets cleanly
  // without a signature-driven re-seed here.
  const seededRef = useRef(false)
  useEffect(() => {
    if (seededRef.current || hosts.length === 0) return
    seededRef.current = true
    setOpen(new Set(hosts.slice(0, INITIAL_OPEN).map((h) => h.id)))
  }, [hosts])

  const totalServices = hosts.reduce((n, h) => n + h.services.length, 0)
  const shown = expanded ? hosts : hosts.slice(0, MAX_HOSTS)
  const hiddenHosts = hosts.length - shown.length
  const hiddenServices = hosts.slice(shown.length).reduce((n, h) => n + h.services.length, 0)

  const toggle = (id: string) =>
    setOpen((prev) => {
      const next = new Set(prev)
      next.has(id) ? next.delete(id) : next.add(id)
      return next
    })

  if (hosts.length === 0) {
    return (
      <section className="min-w-0">
        <SectionHead title={t('assetTree')} count={t('assetCount', { hosts: 0, svc: 0 })} />
        <DeckEmpty glyph={<Boxes className="h-6 w-6" />} title={t('assetTreeIdle')} />
      </section>
    )
  }

  return (
    <section className="min-w-0">
      <SectionHead title={t('assetTree')} count={t('assetCount', { hosts: hosts.length, svc: totalServices })} />
      <div className="overflow-hidden rounded-xl border border-border bg-card/70 shadow-soft backdrop-blur">
        {shown.map((host) => {
          const isOpen = open.has(host.id)
          return (
            <div key={host.id} className="border-b border-border last:border-b-0">
              <button
                type="button"
                onClick={() => toggle(host.id)}
                className="flex w-full items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-secondary/40"
              >
                <ChevronRight className={cn('h-3.5 w-3.5 shrink-0 text-muted-foreground/70 transition-transform', isOpen && 'rotate-90')} />
                <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-success shadow-[0_0_7px_hsl(var(--success))]" />
                <span className="min-w-0 max-w-[220px] truncate font-mono text-[13px] font-semibold text-foreground">{host.host}</span>
                <span className="ml-auto font-mono text-[11px] text-muted-foreground/70">{t('servicesCount', { count: host.services.length })}</span>
              </button>
              {isOpen && (
                <div className="pb-2 pl-[42px] pr-4">
                  {host.services.map((svc) => (
                    <ServiceRow key={svc.id} svc={svc} />
                  ))}
                </div>
              )}
            </div>
          )
        })}

        {hiddenHosts > 0 && (
          <button
            type="button"
            onClick={() => setExpanded(true)}
            className="flex w-full items-center gap-2 border-t border-border px-4 py-3 font-mono text-[11.5px] font-semibold text-muted-foreground transition-colors hover:bg-secondary/40 hover:text-foreground"
          >
            <span className="w-3.5 shrink-0" />{t('moreHosts', { count: hiddenHosts })}
            <span className="font-normal text-muted-foreground/60">· {t('servicesCount', { count: hiddenServices })}</span>
          </button>
        )}
      </div>
    </section>
  )
}

function ServiceRow({ svc }: { svc: ServiceNode }) {
  const { t } = useTranslation('deck')
  const status = svc.statuses[0] || svc.states[0] || (svc.web ? '200' : 'open')
  const tone = badgeToneClass[statusCodeTone(status)]

  return (
    <div className="grid grid-cols-[78px_1fr_auto] items-center gap-3 border-t border-border/70 px-3 py-2 text-xs first:border-t-0">
      <span className="font-mono font-semibold text-foreground">
        {svc.port || '—'}
        {svc.protocol && <span className="font-normal text-muted-foreground/60">/{svc.protocol.toLowerCase()}</span>}
      </span>
      <span className="min-w-0 truncate font-mono text-muted-foreground">
        <b className="font-medium text-foreground">{svc.service || svc.title || t('serviceFallback')}</b>
        {svc.service && svc.title && svc.title !== svc.service ? ` ${svc.title}` : ''}
      </span>
      <span className={cn('justify-self-end rounded-[5px] px-2 py-px font-mono text-[11px] font-semibold uppercase', tone)}>{status}</span>
    </div>
  )
}
