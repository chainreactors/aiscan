import { useEffect, useMemo, useState, type MouseEvent, type ReactNode } from 'react'
import { AlertCircle, Brain, ChevronRight, File, Fingerprint, Folder, FolderOpen, Globe, Server } from 'lucide-react'
import type {
  Asset,
  AssetItem,
  StructuredResult,
} from '../api'
import { cn } from '@/lib/utils'

interface StructuredResultViewProps {
  result: StructuredResult
}

type ViewAsset = Asset & {
  items: AssetItem[]
}

type BadgeTone = 'muted' | 'cyan' | 'yellow' | 'green' | 'red'

type BadgeSpec = {
  id: string
  label: ReactNode
  tone?: BadgeTone
}

type AssetPanel = {
  id: string
  label: string
  count?: number
  preferred?: boolean
  render: () => ReactNode
}

type SitemapNode = {
  id: string
  name: string
  path: string
  children: SitemapNode[]
  items: AssetItem[]
}

export default function StructuredResultView({ result }: StructuredResultViewProps) {
  const assets = useMemo(() => normalizeAssets(result.assets || []), [result])
  const itemCount = assets.reduce((sum, asset) => sum + asset.items.filter((item) => item.kind !== 'path').length, 0)

  return (
    <div className="space-y-4 animate-fade-in">
      <div className="rounded-lg border border-border bg-card/50 p-4">
        <div className="grid grid-cols-2 gap-3 text-xs sm:grid-cols-4 lg:grid-cols-8">
          <Metric label="Assets" value={assets.length} />
          <Metric label="Services" value={result.summary.services} />
          <Metric label="Web" value={result.summary.webs} />
          <Metric label="Probes" value={result.summary.probes} />
          <Metric label="Fingers" value={result.summary.fingerprints} />
          <Metric label="Items" value={itemCount} />
          <Metric label="Errors" value={result.summary.errors} />
          <Metric label="Duration" value={result.summary.duration} />
        </div>
      </div>

      <Section title="Assets">
        {assets.length > 0 ? (
          <AssetList assets={assets} />
        ) : (
          <div className="py-8 text-center text-sm text-muted-foreground">No structured assets.</div>
        )}
      </Section>
    </div>
  )
}

function AssetList({ assets }: { assets: ViewAsset[] }) {
  return (
    <div className="divide-y divide-border/70">
      {assets.map((asset) => (
        <AssetRow key={asset.id || asset.key || asset.target} asset={asset} />
      ))}
    </div>
  )
}

function AssetRow({ asset }: { asset: ViewAsset }) {
  const paths = asset.items.filter((item) => item.kind === 'path')
  const aiItems = asset.items.filter(isAIItem)
  const infoItems = asset.items.filter((item) => item.kind !== 'path' && !isAIItem(item))
  const panels = assetPanels(asset, aiItems, paths)
  const [open, setOpen] = useState(false)
  const [activePanelID, setActivePanelID] = useState(() => defaultPanelID(panels))
  const activePanel = panels.find((panel) => panel.id === activePanelID) || panels[0]

  const selectPanel = (panelID: string) => (event: MouseEvent<HTMLButtonElement>) => {
    event.preventDefault()
    event.stopPropagation()
    setActivePanelID(panelID)
    setOpen(true)
  }

  if (panels.length === 0) {
    return (
      <div className="py-3 first:pt-0 last:pb-0">
        <div className="flex items-start gap-2">
          <AssetIcon asset={asset} />
          <div className="min-w-0 flex-1">
            <AssetHeading asset={asset} />
            <AssetInfoLine asset={asset} items={infoItems} paths={paths} aiItems={aiItems} />
            <BadgeList badges={assetBadges(asset, infoItems, paths, aiItems)} />
          </div>
          {asset.status && <StatusBadge status={asset.status} />}
        </div>
      </div>
    )
  }

  return (
    <details
      className="group py-3 first:pt-0 last:pb-0"
      open={open}
      onToggle={(event) => setOpen(event.currentTarget.open)}
    >
      <summary className="flex cursor-pointer list-none items-start justify-between gap-3 [&::-webkit-details-marker]:hidden">
        <div className="flex min-w-0 items-start gap-2">
          <ChevronRight className="mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform group-open:rotate-90" />
          <AssetIcon asset={asset} />
          <div className="min-w-0">
            <AssetHeading asset={asset} />
            <AssetInfoLine asset={asset} items={infoItems} paths={paths} aiItems={aiItems} />
            <BadgeList badges={assetBadges(asset, infoItems, paths, aiItems)} />
          </div>
        </div>

        <div className="flex shrink-0 flex-wrap justify-end gap-1.5">
          {asset.status && <StatusBadge status={asset.status} />}
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

      {activePanel && <div className="ml-6 mt-3">{activePanel.render()}</div>}
    </details>
  )
}

function assetPanels(asset: ViewAsset, aiItems: AssetItem[], paths: AssetItem[]): AssetPanel[] {
  const panels: AssetPanel[] = []
  if (aiItems.length > 0) {
    panels.push({
      id: 'ai',
      label: 'AI',
      count: aiItems.length,
      preferred: true,
      render: () => <AssetItemsBlock asset={asset} items={aiItems} />,
    })
  }
  if (paths.length > 0) {
    panels.push({
      id: 'sitemap',
      label: 'Sitemap',
      count: paths.length,
      render: () => <SitemapBlock items={paths} />,
    })
  }
  return panels
}

function defaultPanelID(panels: AssetPanel[]) {
  return panels.find((panel) => panel.preferred)?.id || panels[0]?.id || ''
}

function AssetInfoLine({
  asset,
  items,
  paths,
  aiItems,
}: {
  asset: ViewAsset
  items: AssetItem[]
  paths: AssetItem[]
  aiItems: AssetItem[]
}) {
  const serviceText = serviceInfoText(items)
  const fingerText = fingerprintInfoText(items)
  const findingText = firstText(...items.filter((item) => item.kind === 'finding').map(itemTitle))
  const pathText = pathInfoText(paths)
  const aiText = firstText(...aiItems.map(itemTitle))
  const parts = [
    serviceText,
    fingerText,
    pathText,
    findingText && findingText !== asset.title ? findingText : '',
    aiText && aiText !== asset.title ? aiText : '',
  ].filter(Boolean)

  if (parts.length === 0) {
    return null
  }
  return (
    <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
      {parts.slice(0, 4).map((part) => (
        <span key={part} className="break-all">{part}</span>
      ))}
    </div>
  )
}

function AssetHeading({ asset }: { asset: ViewAsset }) {
  const title = asset.title || asset.target || asset.key || 'Asset'
  const subtitle = asset.target && asset.target !== title ? asset.target : ''

  return (
    <div className="min-w-0">
      <div className={cn('break-words text-xs text-foreground', subtitle ? '' : 'font-mono')}>
        {title}
      </div>
      {subtitle && <div className="mt-0.5 break-all font-mono text-[11px] text-muted-foreground">{subtitle}</div>}
    </div>
  )
}

function AssetIcon({ asset }: { asset: ViewAsset }) {
  const kinds = new Set(asset.items.map((item) => item.kind))
  if (kinds.has('path')) {
    return <Globe className="mt-0.5 h-3.5 w-3.5 shrink-0 text-cyber-300" />
  }
  if (kinds.has('fingerprint')) {
    return <Fingerprint className="mt-0.5 h-3.5 w-3.5 shrink-0 text-yellow-300" />
  }
  if (kinds.has('finding') || kinds.has('note') || kinds.has('response')) {
    return <Brain className="mt-0.5 h-3.5 w-3.5 shrink-0 text-cyber-300" />
  }
  return <Server className="mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground" />
}

function isAIItem(item: AssetItem) {
  if (item.kind === 'note' || item.kind === 'response') {
    return true
  }
  if (item.kind !== 'finding') {
    return false
  }
  const source = `${item.source || ''} ${dataString(item, 'skill')} ${dataString(item, 'source')}`.toLowerCase()
  return ['ai', 'agent', 'deep', 'sniper', 'verify'].some((marker) => source.includes(marker))
}

function serviceInfoText(items: AssetItem[]) {
  const services = items.filter((item) => item.kind === 'service')
  if (services.length === 0) {
    return ''
  }
  const visible = services.slice(0, 2).map((item) => {
    const name = firstText(dataString(item, 'service'), item.title, dataString(item, 'protocol'))
    const port = dataString(item, 'port')
    const protocol = dataString(item, 'protocol')
    return compactStrings(protocol, name, port).join(' ')
  }).filter(Boolean)
  const suffix = services.length > visible.length ? ` +${services.length - visible.length}` : ''
  return compactStrings(...visible).join(' / ') + suffix
}

function fingerprintInfoText(items: AssetItem[]) {
  const names = uniqueStrings(items
    .filter((item) => item.kind === 'fingerprint')
    .flatMap((item) => [item.title, ...(item.tags || [])]))
    .slice(0, 4)
  if (names.length === 0) {
    return ''
  }
  return names.join(', ')
}

function pathInfoText(paths: AssetItem[]) {
  if (paths.length === 0) {
    return ''
  }
  const primary = paths.find((item) => item.title || item.status) || paths[0]
  const path = dataString(primary, 'path') || webPath(primary.target)
  const title = firstText(primary.title, primary.summary)
  const status = primary.status
  const suffix = paths.length > 1 ? ` +${paths.length - 1}` : ''
  return compactStrings(status, title, path === '/' ? '' : path).join(' ') + suffix
}

function BadgeList({ badges }: { badges: BadgeSpec[] }) {
  return (
    <div className="mt-1 flex flex-wrap gap-1.5">
      {badges.map((badge) => (
        <Badge key={badge.id} tone={badge.tone}>{badge.label}</Badge>
      ))}
    </div>
  )
}

function assetBadges(asset: ViewAsset, items: AssetItem[], paths: AssetItem[], aiItems: AssetItem[]): BadgeSpec[] {
  const badges: BadgeSpec[] = []
  const serviceFacts = uniqueStrings(asset.items
    .filter((item) => item.kind === 'service')
    .flatMap((item) => [
      dataString(item, 'protocol'),
      dataString(item, 'service'),
      dataString(item, 'port'),
    ]))
  badges.push(...serviceFacts.slice(0, 4).map((fact) => ({ id: `service:${fact}`, label: fact })))

  const statuses = uniqueStrings(paths.map((item) => item.status)).slice(0, 3)
  badges.push(...statuses.map((status) => ({
    id: `status:${status}`,
    label: status,
    tone: statusCodeTone(status),
  })))

  const fingers = uniqueStrings(asset.items
    .filter((item) => item.kind === 'fingerprint' || item.kind === 'path')
    .flatMap((item) => [
      item.title,
      ...((item.tags || []).filter((tag) => tag !== item.source && tag !== item.status)),
    ]))
    .slice(0, 4)
  badges.push(...fingers.map((finger) => ({ id: `finger:${finger}`, label: finger, tone: 'yellow' as const })))

  const sources = uniqueStrings(items.map((item) => item.source).filter((source) => source && source !== 'gogo_portscan')).slice(0, 4)
  badges.push(...sources.map((source) => ({ id: `source:${source}`, label: source, tone: 'cyan' as const })))

  if (paths.length > 0) {
    badges.push({ id: 'paths', label: formatCount(paths.length, 'path') })
  }
  if (aiItems.length > 0) {
    badges.push({ id: 'ai', label: `${aiItems.length} AI`, tone: 'cyan' as const })
  }
  return dedupeBadges(badges)
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
  const headerBadges = dedupeBadges([
    { id: `kind:${item.kind}`, label: item.kind, tone: itemKindTone(item.kind) },
    ...(item.source ? [{ id: `source:${item.source}`, label: item.source, tone: 'cyan' as const }] : []),
    ...(item.status ? [{ id: `status:${item.status}`, label: item.status, tone: statusCodeTone(item.status) }] : []),
  ])
  const tags = tagBadges(item.tags, headerBadges.map((badge) => String(badge.label)))

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
    return <AlertCircle className="h-3.5 w-3.5 text-red-300" />
  }
  if (kind === 'note') {
    return <Brain className="h-3.5 w-3.5 text-cyber-300" />
  }
  if (kind === 'fingerprint') {
    return <Fingerprint className="h-3.5 w-3.5 text-yellow-300" />
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
            <FolderOpen className="h-3.5 w-3.5 shrink-0 text-cyber-300" />
          ) : (
            <Folder className="h-3.5 w-3.5 shrink-0 text-cyber-300" />
          )}
          <span className="min-w-0 flex-1 truncate font-mono text-foreground">{node.name}</span>
          <span className="shrink-0 text-muted-foreground">{count}</span>
        </button>
        {isOpen && (
          <div role="group">
            {node.items.map((item) => (
              <EndpointFile key={pathIdentity(item)} item={item} depth={depth + 1} />
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
      {node.items.map((item) => (
        <EndpointFile key={pathIdentity(item)} item={item} depth={depth} />
      ))}
    </>
  )
}

function EndpointFile({ item, depth }: { item: AssetItem; depth: number }) {
  const paddingLeft = `${0.6 + depth * 1.15}rem`
  const filename = endpointFileName(item)
  const search = pathSearch(item)
  const metaBadges = dedupeBadges([
    ...(search ? [{ id: `search:${search}`, label: search }] : []),
    ...(item.source ? [{ id: `source:${item.source}`, label: item.source }] : []),
    ...tagBadges(item.tags, [search, item.source], 'yellow'),
  ])

  return (
    <div role="treeitem" className="py-1.5 pr-3 text-xs hover:bg-secondary/30" style={{ paddingLeft }}>
      <div className="flex flex-wrap items-center gap-2">
        <File className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <span className="break-all font-mono text-foreground">{filename}</span>
        {item.status ? <Badge tone={statusCodeTone(item.status)}>{item.status}</Badge> : <Badge>seen</Badge>}
        {item.title && <span className="text-muted-foreground">{item.title}</span>}
      </div>
      {metaBadges.length > 0 && (
        <div className="mt-1 flex flex-wrap gap-1.5 pl-5">
          {metaBadges.map((badge) => (
            <Badge key={badge.id} tone={badge.tone}>{badge.label}</Badge>
          ))}
        </div>
      )}
    </div>
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
          ? 'border-cyber-400/40 bg-cyber-500/15 text-cyber-200'
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
      <div className="border-b border-border px-4 py-2 text-sm font-medium text-cyber-400">{title}</div>
      <div className="p-4">{children}</div>
    </div>
  )
}

function StatusBadge({ status }: { status: string }) {
  const tone =
    status === 'confirmed' || status === 'critical' || status === 'high' || status === 'finding'
      ? 'red'
      : status === 'not_confirmed'
        ? 'muted'
        : status === 'inconclusive' || status === 'medium'
          ? 'yellow'
          : status === 'low'
            ? 'green'
            : statusCodeTone(status)
  return <Badge tone={tone}>{status}</Badge>
}

function Badge({ children, tone = 'muted' }: { children: ReactNode; tone?: BadgeTone }) {
  return (
    <span
      className={cn(
        'inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-medium',
        tone === 'cyan' && 'bg-cyber-500/10 text-cyber-300',
        tone === 'yellow' && 'bg-yellow-400/10 text-yellow-300',
        tone === 'green' && 'bg-green-400/10 text-green-300',
        tone === 'red' && 'bg-red-400/10 text-red-300',
        tone === 'muted' && 'bg-background text-muted-foreground',
      )}
    >
      {children}
    </span>
  )
}

function normalizeAssets(assets: Asset[]): ViewAsset[] {
  return assets
    .map((asset) => {
      const items = uniqueBy(asset.items || [], itemIdentity).sort(itemSort)
      return {
        ...asset,
        id: asset.id || `asset:${asset.key || asset.target}`,
        key: asset.key || canonicalKey(asset.target),
        target: asset.target || asset.key || 'Scan',
        title: asset.title,
        status: asset.status,
        items,
      }
    })
    .sort((a, b) => (a.key || a.target).localeCompare(b.key || b.target))
}

function buildSitemapTree(items: AssetItem[]): SitemapNode[] {
  const root: SitemapNode = { id: 'root', name: 'root', path: '/', children: [], items: [] }
  for (const item of items) {
    insertPathItem(root, item)
  }
  return sortSitemapNodes(root.children)
}

function insertPathItem(root: SitemapNode, item: AssetItem) {
  const path = dataString(item, 'path') || webPath(item.target)
  const pathname = path.split('?')[0] || '/'
  if (pathname === '/') {
    getOrCreateSitemapChild(root, '/', '/').items.push(item)
    return
  }

  const parts = pathname.split('/').filter(Boolean)
  let current = root
  let currentPath = ''
  parts.forEach((part, index) => {
    currentPath += `/${part}`
    const node = getOrCreateSitemapChild(current, part, currentPath)
    if (index === parts.length - 1) {
      node.items.push(item)
      return
    }
    current = node
  })
}

function getOrCreateSitemapChild(parent: SitemapNode, name: string, path: string) {
  let child = parent.children.find((item) => item.path === path)
  if (!child) {
    child = { id: path, name, path, children: [], items: [] }
    parent.children.push(child)
  }
  return child
}

function sortSitemapNodes(nodes: SitemapNode[]): SitemapNode[] {
  return nodes
    .map((node) => ({ ...node, children: sortSitemapNodes(node.children) }))
    .sort((a, b) => {
      const aFolder = a.children.length > 0 ? 0 : 1
      const bFolder = b.children.length > 0 ? 0 : 1
      return `${aFolder}|${a.name}`.localeCompare(`${bFolder}|${b.name}`)
    })
}

function defaultOpenSitemapNodes(nodes: SitemapNode[]) {
  const open = new Set<string>()
  visitSitemapFolders(nodes, (node, depth) => {
    if (depth < 1) {
      open.add(node.id)
    }
  })
  return open
}

function collectSitemapFolderIDs(nodes: SitemapNode[]) {
  const ids: string[] = []
  visitSitemapFolders(nodes, (node) => ids.push(node.id))
  return ids
}

function visitSitemapFolders(nodes: SitemapNode[], visit: (node: SitemapNode, depth: number) => void, depth = 0) {
  for (const node of nodes) {
    if (node.children.length === 0) {
      continue
    }
    visit(node, depth)
    visitSitemapFolders(node.children, visit, depth + 1)
  }
}

function endpointFileName(item: AssetItem) {
  const path = dataString(item, 'path') || webPath(item.target)
  const pathname = path.split('?')[0] || '/'
  if (pathname === '/') {
    return '/'
  }
  const parts = pathname.split('/').filter(Boolean)
  const last = parts[parts.length - 1] || '/'
  return path.includes('?') ? `${last}?${path.split('?').slice(1).join('?')}` : last
}

function pathSearch(item: AssetItem) {
  const path = dataString(item, 'path') || webPath(item.target)
  const idx = path.indexOf('?')
  return idx >= 0 ? path.slice(idx) : ''
}

function pathIdentity(item: AssetItem) {
  return `${canonicalKey(dataString(item, 'url') || item.target || dataString(item, 'path'))}|host=${dataString(item, 'host_header')}`
}

function itemTitle(item: AssetItem) {
  return firstText(item.summary, item.title, item.raw)
}

function itemSort(a: AssetItem, b: AssetItem) {
  const rank = (item: AssetItem) => {
    if (item.kind === 'service') return 10
    if (item.kind === 'fingerprint') return 20
    if (item.kind === 'finding') return 30
    if (item.kind === 'note') return 40
    if (item.kind === 'response') return 45
    if (item.kind === 'path') return 50
    if (item.kind === 'error') return 60
    return 90
  }
  return `${rank(a)}|${a.target || ''}|${a.title || ''}|${a.summary || ''}`
    .localeCompare(`${rank(b)}|${b.target || ''}|${b.title || ''}|${b.summary || ''}`)
}

function itemIdentity(item: AssetItem) {
  return [
    item.kind,
    item.source,
    item.target,
    item.status,
    item.title,
    item.summary,
    item.raw,
  ].filter(Boolean).join('|')
}

function dataString(item: AssetItem, key: string) {
  const value = item.data?.[key]
  if (typeof value === 'string') return value
  if (typeof value === 'number' && value > 0) return String(value)
  return ''
}

function sameTarget(left?: string, right?: string) {
  return canonicalKey(urlOrigin(left) || left) === canonicalKey(urlOrigin(right) || right)
}

function webPath(rawURL?: string) {
  const parsed = parseURL(rawURL)
  if (!parsed) {
    return rawURL || '/'
  }
  return `${parsed.pathname || '/'}${parsed.search || ''}`
}

function urlOrigin(rawURL?: string) {
  const parsed = parseURL(rawURL)
  return parsed ? parsed.origin.toLowerCase() : ''
}

function parseURL(value?: string) {
  if (!value) {
    return null
  }
  try {
    return new URL(value)
  } catch {
    return null
  }
}

function canonicalKey(value?: string) {
  return (value || '').trim().replace(/\/+$/, '').toLowerCase()
}

function firstText(...values: Array<string | undefined>) {
  return values.find((value) => value && value.trim())?.trim() || ''
}

function compactStrings(...values: Array<string | undefined>) {
  return uniqueStrings(values.map((value) => value?.trim() || '').filter(Boolean))
}

function uniqueStrings(values: Array<string | undefined>) {
  return Array.from(new Set(values.filter((value): value is string => !!value)))
}

function uniqueBy<T>(values: T[], keyFn: (value: T) => string) {
  const seen = new Set<string>()
  const result: T[] = []
  for (const value of values) {
    const key = keyFn(value)
    if (seen.has(key)) {
      continue
    }
    seen.add(key)
    result.push(value)
  }
  return result
}

function dedupeBadges(badges: BadgeSpec[]) {
  const seen = new Set<string>()
  const result: BadgeSpec[] = []
  for (const badge of badges) {
    const key = labelKey(badge.label)
    if (!key || seen.has(key)) {
      continue
    }
    seen.add(key)
    result.push(badge)
  }
  return result
}

function tagBadges(tags: string[] | undefined, excluded: Array<string | undefined>, tone?: BadgeTone): BadgeSpec[] {
  const hidden = new Set(excluded.map(labelKey).filter(Boolean))
  return dedupeBadges((tags || [])
    .filter((tag) => !hidden.has(labelKey(tag)))
    .map((tag) => ({ id: `tag:${tag}`, label: tag, tone })))
}

function labelKey(value?: ReactNode) {
  return String(value || '').trim().toLowerCase()
}

function itemKindTone(kind: string): BadgeTone {
  if (kind === 'finding') return 'red'
  if (kind === 'note' || kind === 'response') return 'cyan'
  return 'muted'
}

function statusCodeTone(status?: string): BadgeTone {
  const code = Number(status)
  if (!Number.isFinite(code)) {
    return 'muted'
  }
  if (code >= 500) return 'red'
  if (code >= 400) return 'yellow'
  if (code >= 200 && code < 400) return 'green'
  return 'muted'
}

function formatCount(count: number, singular: string) {
  return `${count} ${count === 1 ? singular : `${singular}s`}`
}
