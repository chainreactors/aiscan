import { useEffect, useMemo, useState, type MouseEvent, type ReactNode } from 'react'
import { AlertCircle, Brain, ChevronRight, File, Fingerprint, Folder, FolderOpen, Globe, Network, Server } from 'lucide-react'
import type { AssetItem, ScanResult } from '../api'
import {
  buildResultModel,
  buildSitemapTree,
  collectSitemapFolderIDs,
  defaultOpenSitemapNodes,
  endpointFileName,
  formatCount,
  itemFactValues,
  itemFacts,
  itemKindTone,
  itemStateTone,
  itemTitle,
  pathIdentity,
  pathSearch,
  sameTarget,
  statusCodeTone,
  tagBadges,
  type BadgeTone,
  type HostGroup,
  type ServiceNode,
  type SitemapNode,
  type ViewAsset,
} from '../lib/scan-result'
import { cn } from '@/lib/utils'

interface AssetResultViewProps {
  result: ScanResult
}

type AssetPanel = {
  id: string
  label: string
  count?: number
  preferred?: boolean
  render: () => ReactNode
}

export default function AssetResultView({ result }: AssetResultViewProps) {
  const model = useMemo(() => buildResultModel(result), [result])

  return (
    <div className="space-y-4 animate-fade-in">
      <div className="rounded-lg border border-border bg-card/50 p-4">
        <div className="grid grid-cols-2 gap-3 text-xs sm:grid-cols-3 lg:grid-cols-9">
          <Metric label="Hosts" value={model.metrics.hosts} />
          <Metric label="Assets" value={model.metrics.assets} />
          <Metric label="Services" value={model.metrics.services} />
          <Metric label="Web" value={model.metrics.web} />
          <Metric label="Probes" value={model.metrics.probes} />
          <Metric label="Fingers" value={model.metrics.fingers} />
          <Metric label="Items" value={model.metrics.items} />
          <Metric label="Errors" value={model.metrics.errors} />
          <Metric label="Duration" value={model.metrics.duration} />
        </div>
      </div>

      <Section title="Hosts">
        {model.hosts.length > 0 ? (
          <HostList hosts={model.hosts} />
        ) : (
          <div className="py-8 text-center text-sm text-muted-foreground">No hosts.</div>
        )}
      </Section>
    </div>
  )
}

function HostList({ hosts }: { hosts: HostGroup[] }) {
  return (
    <div className="divide-y divide-border/70">
      {hosts.map((host) => (
        <HostPanel key={host.id} host={host} />
      ))}
    </div>
  )
}

function HostPanel({ host }: { host: HostGroup }) {
  const [open, setOpen] = useState(true)
  const webCount = host.services.filter((service) => service.web).length

  return (
    <details
      className="group py-3 first:pt-0 last:pb-0"
      open={open}
      onToggle={(event) => setOpen(event.currentTarget.open)}
    >
      <summary className="flex cursor-pointer list-none items-start gap-2 [&::-webkit-details-marker]:hidden">
        <ChevronRight className="mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform group-open:rotate-90" />
        <Network className="mt-0.5 h-3.5 w-3.5 shrink-0 text-cyber-700 dark:text-cyber-300" />
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
            <span className="break-all font-mono text-sm font-semibold text-foreground">{host.host}</span>
            <Badge>{formatCount(host.services.length, 'service')}</Badge>
            {webCount > 0 && <Badge tone="cyan">{webCountLabel(webCount)}</Badge>}
          </div>
        </div>
      </summary>

      <div className="ml-6 mt-3 border-l border-border/70 pl-3">
        <ServiceList services={host.services} />
      </div>
    </details>
  )
}

function ServiceList({ services }: { services: ServiceNode[] }) {
  return (
    <div className="divide-y divide-border/60">
      {services.map((service) => (
        <ServiceRow key={service.id} service={service} />
      ))}
    </div>
  )
}

function ServiceRow({ service }: { service: ServiceNode }) {
  const panels = useMemo(() => servicePanels(service), [service])
  const [open, setOpen] = useState(false)
  const [activePanelID, setActivePanelID] = useState(() => defaultPanelID(panels))
  const activePanel = panels.find((panel) => panel.id === activePanelID) || panels[0]

  useEffect(() => {
    if (!panels.some((panel) => panel.id === activePanelID)) {
      setActivePanelID(defaultPanelID(panels))
    }
  }, [activePanelID, panels])

  const selectPanel = (panelID: string) => (event: MouseEvent<HTMLButtonElement>) => {
    event.preventDefault()
    event.stopPropagation()
    setActivePanelID(panelID)
    setOpen(true)
  }

  if (panels.length === 0) {
    return (
      <div className="py-3 first:pt-0 last:pb-0">
        <ServiceLine service={service} />
      </div>
    )
  }

  return (
    <details
      className="group/service py-3 first:pt-0 last:pb-0"
      open={open}
      onToggle={(event) => setOpen(event.currentTarget.open)}
    >
      <summary className="cursor-pointer list-none [&::-webkit-details-marker]:hidden">
        <ServiceLine service={service} expandable />
        <div className="mt-2 flex flex-wrap gap-1.5 pl-7 sm:pl-[7.25rem]">
          {panels.map((panel) => (
            <TabChip
              key={panel.id}
              active={open && activePanel?.id === panel.id}
              label={panel.label}
              count={panel.count}
              onClick={selectPanel(panel.id)}
            />
          ))}
        </div>
      </summary>

      {activePanel && (
        <div className="mt-3 pl-7 sm:pl-[7.25rem]">
          {activePanel.render()}
        </div>
      )}
    </details>
  )
}

function ServiceLine({ service, expandable = false }: { service: ServiceNode; expandable?: boolean }) {
  const displayTarget = service.web ? service.asset.target : service.target

  return (
    <div className="grid min-w-0 gap-2 sm:grid-cols-[minmax(0,1fr)_auto]">
      <div className="flex min-w-0 items-start gap-2">
        {expandable ? (
          <ChevronRight className="mt-1 h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform group-open/service:rotate-90" />
        ) : (
          <span className="h-3.5 w-3.5 shrink-0" />
        )}
        <span className="w-[4.75rem] shrink-0 break-words font-mono text-sm font-semibold leading-5 text-foreground">
          {service.port || '-'}
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
            <ServiceIcon service={service} />
            <span className="font-medium text-foreground">{service.service || service.protocol || 'service'}</span>
            {service.protocol && service.protocol !== service.service && <Badge>{service.protocol}</Badge>}
            {service.web && <Badge tone="cyan">{service.pathCount > 0 ? webCountLabel(service.pathCount) : 'web'}</Badge>}
            {service.title && (
              <span className="min-w-0 break-words text-xs text-muted-foreground">{service.title}</span>
            )}
          </div>
          <div className="mt-1 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-[11px] text-muted-foreground">
            {displayTarget && <span className="break-all font-mono">{displayTarget}</span>}
            {service.summary && <span className="break-words">{service.summary}</span>}
            {service.statuses.slice(0, 5).map((status) => (
              <Badge key={`http:${status}`} tone={statusCodeTone(status)}>{status}</Badge>
            ))}
            {service.states.slice(0, 3).map((state) => (
              <Badge key={`state:${state}`} tone={itemStateTone(state)}>{state}</Badge>
            ))}
            <FingerChips fingers={service.fingers} />
            {service.aiItems.length > 0 && <span className="text-cyber-700 dark:text-cyber-300">{service.aiItems.length} AI</span>}
          </div>
        </div>
      </div>
      <SourceChips sources={service.sources} className="justify-start sm:justify-end" />
    </div>
  )
}

function ServiceIcon({ service }: { service: ServiceNode }) {
  if (service.web) {
    return <Globe className="h-3.5 w-3.5 shrink-0 text-cyber-700 dark:text-cyber-300" />
  }
  if (service.fingers.length > 0) {
    return <Fingerprint className="h-3.5 w-3.5 shrink-0 text-yellow-700 dark:text-yellow-300" />
  }
  return <Server className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
}

function servicePanels(service: ServiceNode): AssetPanel[] {
  const panels: AssetPanel[] = []
  if (service.paths.length > 0) {
    panels.push({
      id: 'sitemap',
      label: 'Sitemap',
      count: service.paths.length,
      preferred: true,
      render: () => <SitemapBlock items={service.paths} />,
    })
  }
  if (service.aiItems.length > 0) {
    panels.push({
      id: 'ai',
      label: 'AI',
      count: service.aiItems.length,
      render: () => <AssetItemsBlock asset={service.asset} items={service.aiItems} />,
    })
  }
  if (service.detailItems.length > 0) {
    panels.push({
      id: 'details',
      label: 'Details',
      count: service.detailItems.length,
      render: () => <AssetItemsBlock asset={service.asset} items={service.detailItems} />,
    })
  }
  return panels
}

function defaultPanelID(panels: AssetPanel[]) {
  return panels.find((panel) => panel.preferred)?.id || panels[0]?.id || ''
}

function webCountLabel(count: number) {
  return `${count} web`
}

function ItemFactLine({ item, search, className }: { item: AssetItem; search?: string; className?: string }) {
  const facts = itemFacts(item)
  if (facts.statuses.length === 0 && facts.states.length === 0 && facts.fingers.length === 0 && facts.sources.length === 0 && !search) {
    return null
  }
  return (
    <div className={cn('flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-[11px]', className)}>
      {facts.statuses.map((status) => (
        <Badge key={`http:${status}`} tone={statusCodeTone(status)}>{status}</Badge>
      ))}
      {facts.states.map((state) => (
        <Badge key={`state:${state}`} tone={itemStateTone(state)}>{state}</Badge>
      ))}
      <FingerChips fingers={facts.fingers} />
      <SourceChips sources={facts.sources} />
      {search && <span className="break-all font-mono text-muted-foreground">{search}</span>}
    </div>
  )
}

function AssetItemsBlock({ asset, items }: { asset: ViewAsset; items: AssetItem[] }) {
  return (
    <div className="space-y-2">
      {items.map((item, idx) => (
        <AssetItemRow key={`${item.kind}-${item.source}-${item.target}-${item.title}-${idx}`} item={item} asset={asset} />
      ))}
    </div>
  )
}

function AssetItemRow({ item, asset }: { item: AssetItem; asset: ViewAsset }) {
  const title = itemTitle(item)
  const detail = item.detail
  const showTarget = item.target && !sameTarget(item.target, asset.target)
  const headerBadges = [
    { id: `kind:${item.kind}`, label: item.kind, tone: itemKindTone(item.kind) },
  ]
  const tags = tagBadges(item.tags, [...headerBadges.map((badge) => badge.label), ...itemFactValues(item)])

  return (
    <div className={cn(
      'rounded-md border p-3 text-xs',
      item.kind === 'error'
        ? 'border-red-400/20 bg-red-400/10'
        : item.kind === 'finding'
          ? 'border-red-400/20 bg-red-400/5'
          : 'border-border/70 bg-background/30',
    )}>
      <div className="flex flex-wrap items-center gap-2">
        <ItemIcon kind={item.kind} />
        {headerBadges.map((badge) => (
          <Badge key={badge.id} tone={badge.tone}>{badge.label}</Badge>
        ))}
        {showTarget && <span className="break-all font-mono text-muted-foreground">{item.target}</span>}
      </div>
      {title && <div className="mt-1 break-words text-foreground">{title}</div>}
      <ItemFactLine item={item} className="mt-2" />
      {detail && (
        <div className="mt-2 max-h-96 overflow-auto whitespace-pre-wrap rounded-md border border-border bg-background/50 p-3 text-muted-foreground">
          {detail}
        </div>
      )}
      {tags.length > 0 && (
        <div className="mt-2 flex flex-wrap gap-1.5">
          {tags.map((badge) => (
            <Badge key={badge.id} tone={badge.tone}>{badge.label}</Badge>
          ))}
        </div>
      )}
    </div>
  )
}

function ItemIcon({ kind }: { kind: string }) {
  if (kind === 'finding') {
    return <AlertCircle className="h-3.5 w-3.5 text-red-700 dark:text-red-300" />
  }
  if (kind === 'note' || kind === 'response') {
    return <Brain className="h-3.5 w-3.5 text-cyber-700 dark:text-cyber-300" />
  }
  if (kind === 'fingerprint') {
    return <Fingerprint className="h-3.5 w-3.5 text-yellow-700 dark:text-yellow-300" />
  }
  return <Server className="h-3.5 w-3.5 text-muted-foreground" />
}

function SitemapBlock({ items }: { items: AssetItem[] }) {
  const tree = useMemo(() => buildSitemapTree(items), [items])
  const folderIDs = useMemo(() => collectSitemapFolderIDs(tree), [tree])
  const [openIDs, setOpenIDs] = useState<Set<string>>(() => defaultOpenSitemapNodes(tree))

  useEffect(() => {
    setOpenIDs(defaultOpenSitemapNodes(tree))
  }, [tree])

  const toggleNode = (id: string) => {
    setOpenIDs((current) => {
      const next = new Set(current)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }

  return (
    <div className="overflow-hidden rounded-md border border-border/70 bg-background/30">
      {folderIDs.length > 0 && (
        <div className="flex items-center justify-end gap-1 border-b border-border/70 px-2 py-1">
          <IconButton label="Expand all" onClick={() => setOpenIDs(new Set(folderIDs))}>
            <FolderOpen className="h-3.5 w-3.5" />
          </IconButton>
          <IconButton label="Collapse all" onClick={() => setOpenIDs(new Set())}>
            <Folder className="h-3.5 w-3.5" />
          </IconButton>
        </div>
      )}
      <div role="tree" aria-label="Sitemap">
        {tree.map((node) => (
          <SitemapTreeNode
            key={node.id}
            node={node}
            depth={0}
            openIDs={openIDs}
            onToggle={toggleNode}
          />
        ))}
      </div>
    </div>
  )
}

function SitemapTreeNode({
  node,
  depth,
  openIDs,
  onToggle,
}: {
  node: SitemapNode
  depth: number
  openIDs: Set<string>
  onToggle: (id: string) => void
}) {
  const isFolder = node.children.length > 0
  const isOpen = openIDs.has(node.id)
  const paddingLeft = `${0.6 + depth * 1.15}rem`
  const count = node.children.length + node.items.length

  if (isFolder) {
    return (
      <div role="treeitem" aria-expanded={isOpen}>
        <button
          type="button"
          className="flex w-full items-center gap-2 py-1.5 pr-3 text-left text-xs hover:bg-secondary/40"
          style={{ paddingLeft }}
          onClick={() => onToggle(node.id)}
        >
          <ChevronRight className={cn(
            'h-3 w-3 shrink-0 text-muted-foreground transition-transform',
            isOpen && 'rotate-90',
          )} />
          {isOpen ? (
            <FolderOpen className="h-3.5 w-3.5 shrink-0 text-cyber-700 dark:text-cyber-300" />
          ) : (
            <Folder className="h-3.5 w-3.5 shrink-0 text-cyber-700 dark:text-cyber-300" />
          )}
          <span className="min-w-0 flex-1 truncate font-mono text-foreground">{node.name}</span>
          <span className="shrink-0 text-muted-foreground">{count}</span>
        </button>
        {isOpen && (
          <div role="group">
            {node.items.map((item, idx) => (
              <EndpointFile key={`${pathIdentity(item)}:${idx}`} item={item} depth={depth + 1} />
            ))}
            {node.children.map((child) => (
              <SitemapTreeNode
                key={child.id}
                node={child}
                depth={depth + 1}
                openIDs={openIDs}
                onToggle={onToggle}
              />
            ))}
          </div>
        )}
      </div>
    )
  }

  return (
    <>
      {node.items.map((item, idx) => (
        <EndpointFile key={`${pathIdentity(item)}:${idx}`} item={item} depth={depth} />
      ))}
    </>
  )
}

function EndpointFile({ item, depth }: { item: AssetItem; depth: number }) {
  const paddingLeft = `${0.6 + depth * 1.15}rem`
  const filename = endpointFileName(item)
  const search = pathSearch(item)

  return (
    <div role="treeitem" className="py-1.5 pr-3 text-xs hover:bg-secondary/30" style={{ paddingLeft }}>
      <div className="flex flex-wrap items-center gap-2">
        <File className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <span className="break-all font-mono text-foreground">{filename}</span>
        {item.title && <span className="text-muted-foreground">{item.title}</span>}
      </div>
      <ItemFactLine item={item} search={search} className="mt-1 pl-5" />
    </div>
  )
}

function SourceChips({ sources, className }: { sources: string[]; className?: string }) {
  if (sources.length === 0) {
    return null
  }

  const visible = sources.slice(0, 5)
  const hidden = sources.length - visible.length

  return (
    <span className={cn('inline-flex min-w-0 flex-wrap items-center gap-1 text-cyber-700 dark:text-cyber-300', className)} title="Sources">
      <Server className="h-3 w-3 shrink-0" />
      {visible.map((source) => (
        <span key={`source:${source}`} className="rounded bg-cyber-500/10 px-1.5 py-0.5 text-[10px]">{source}</span>
      ))}
      {hidden > 0 && <span className="rounded bg-cyber-500/10 px-1.5 py-0.5 text-[10px]">+{hidden}</span>}
    </span>
  )
}

function FingerChips({ fingers }: { fingers: string[] }) {
  if (fingers.length === 0) {
    return null
  }

  const visible = fingers.slice(0, 5)
  const hidden = fingers.length - visible.length

  return (
    <span className="inline-flex min-w-0 flex-wrap items-center gap-1 text-yellow-700 dark:text-yellow-300" title="Fingerprints">
      <Fingerprint className="h-3 w-3 shrink-0" />
      {visible.map((finger) => (
        <span key={`finger:${finger}`} className="rounded bg-yellow-400/10 px-1.5 py-0.5 text-[10px]">{finger}</span>
      ))}
      {hidden > 0 && <span className="rounded bg-yellow-400/10 px-1.5 py-0.5 text-[10px]">+{hidden}</span>}
    </span>
  )
}

function IconButton({
  children,
  label,
  onClick,
}: {
  children: ReactNode
  label: string
  onClick: () => void
}) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={onClick}
      className="inline-flex h-6 w-6 items-center justify-center rounded border border-border bg-background text-muted-foreground hover:border-cyber-400/30 hover:text-foreground"
    >
      {children}
    </button>
  )
}

function TabChip({
  active,
  count,
  label,
  onClick,
}: {
  active: boolean
  count?: number
  label: string
  onClick: (event: MouseEvent<HTMLButtonElement>) => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        'rounded border px-2 py-1 text-[10px] font-medium transition-colors',
        active
          ? 'border-cyber-400/40 bg-cyber-500/15 text-cyber-800 dark:text-cyber-200'
          : 'border-border bg-background text-muted-foreground hover:border-cyber-400/30 hover:text-foreground',
      )}
    >
      {label}
      {typeof count === 'number' && count > 0 && (
        <>
          {' '}
          <span className="opacity-70">{count}</span>
        </>
      )}
    </button>
  )
}

function Metric({ label, value }: { label: string; value: string | number }) {
  return (
    <div>
      <div className="text-[10px] uppercase text-muted-foreground">{label}</div>
      <div className="mt-1 font-mono text-sm text-foreground">{value}</div>
    </div>
  )
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="rounded-lg border border-border bg-card/50">
      <div className="border-b border-border px-4 py-2 text-sm font-medium text-cyber-700 dark:text-cyber-400">{title}</div>
      <div className="p-4">{children}</div>
    </div>
  )
}

function Badge({ children, tone = 'muted' }: { children: ReactNode; tone?: BadgeTone }) {
  return (
    <span
      className={cn(
        'inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-medium',
        tone === 'cyan' && 'bg-cyber-500/10 text-cyber-700 dark:text-cyber-300',
        tone === 'yellow' && 'bg-yellow-400/10 text-yellow-700 dark:text-yellow-300',
        tone === 'green' && 'bg-green-400/10 text-green-700 dark:text-green-300',
        tone === 'red' && 'bg-red-400/10 text-red-700 dark:text-red-300',
        tone === 'muted' && 'bg-background text-muted-foreground',
      )}
    >
      {children}
    </span>
  )
}
