import { useEffect, useMemo, useState } from 'react'
import { CircuitBoard, Network, RefreshCw, Search, Bot, Wrench, Repeat2, Library, Layers } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { JEVUsage } from './chat/JEVUsage'
import { ReactFlow, Background, Controls, Handle, Position, useNodesState, type NodeProps, type Edge } from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { Button, Tooltip, TooltipContent, TooltipTrigger } from '@cyber/ui'
import { cn } from '@cyber/theme'
import type { AOPEvent } from '@/viewer'
import { aopClient, getJEVLibrary } from '../api'
import type { JEVLibrary } from '../cyber-proto'
import { projectJEV } from '../lib/jev-view'
import { projectRuntimeNetwork, runtimeTurns, turnKey, type RuntimeNode } from '../lib/jev-network'
import { JEVDefinition } from './chat/JEVDefinition'
import { ToolDrawer } from './layout/ToolDrawer'

function RuntimeNodeView({ data, selected }: NodeProps<RuntimeNode>) {
  const { t } = useTranslation('jev')
  const Icon = data.kind === 'agent' ? Bot : data.kind === 'jev' ? Repeat2 : Wrench
  return <div className={cn('w-[210px] rounded-md border bg-background px-3 py-2.5 text-foreground shadow-sm',
    selected ? 'border-primary ring-1 ring-primary' : 'border-border')}>
    <Handle type="target" position={data.vertical ? Position.Top : Position.Left} className="!bg-muted-foreground" />
    <div className="flex items-center gap-2 text-xs font-medium"><Icon className={cn('h-4 w-4 shrink-0', data.kind === 'jev' ? 'text-emerald-500' : data.kind === 'agent' ? 'text-blue-500' : 'text-amber-500')} />
      <span className="min-w-0 break-all">{data.label}</span></div>
    <div className="mt-1.5 flex items-center gap-1.5 text-xs text-muted-foreground"><span className={cn('h-1.5 w-1.5 shrink-0 rounded-full',
      ['running', 'executing', 'model', 'checking'].includes(data.status) ? 'animate-pulse bg-emerald-500' : 'bg-muted-foreground/50')} />{t(`nodeStatus.${data.status}`)}</div>
    {!!data.detail && <div className="mt-1 truncate font-mono text-xs text-muted-foreground" title={data.detail}>{data.detail}</div>}
    <Handle type="source" position={data.vertical ? Position.Bottom : Position.Right} className="!bg-muted-foreground" />
    <Handle id="feedback-in" type="target" position={data.vertical ? Position.Right : Position.Bottom} style={data.vertical ? { top: '30%' } : { left: '30%' }} className="!bg-muted-foreground" />
    <Handle id="feedback-out" type="source" position={data.vertical ? Position.Right : Position.Bottom} style={data.vertical ? { top: '70%' } : { left: '70%' }} className="!bg-muted-foreground" />
  </div>
}
const nodeTypes = { runtime: RuntimeNodeView }

function RuntimeNetwork({ nodes: projected, edges, vertical, onSelect }: {
  nodes: RuntimeNode[]; edges: Edge[]; vertical: boolean; onSelect: (id: string) => void
}) {
  const [nodes, setNodes, onNodesChange] = useNodesState<RuntimeNode>(projected)
  useEffect(() => setNodes(previous => {
    const measured = new Map(previous.map(node => [node.id, node.measured]))
    return projected.map(node => ({ ...node, measured: measured.get(node.id) }))
  }), [projected, setNodes])
  return <ReactFlow nodes={nodes} edges={edges} onNodesChange={onNodesChange} nodeTypes={nodeTypes} fitView
    fitViewOptions={{ padding: 0.15, minZoom: 0.75, maxZoom: 1, nodes: nodes.slice(0, vertical ? 4 : undefined) }}
    minZoom={0.5} maxZoom={1.5} nodesDraggable={false} nodesConnectable={false} onNodeClick={(_, node) => onSelect(node.id)}
    proOptions={{ hideAttribution: true }} colorMode={document.documentElement.classList.contains('dark') ? 'dark' : 'light'}>
    <Background color="hsl(var(--border))" gap={24} /><Controls showInteractive={false} />
  </ReactFlow>
}


export default function ReflexPanel({ open, onClose, sessionID, events }: {
  open: boolean; onClose: () => void; sessionID: string | null; events: AOPEvent[]
}) {
  const { t } = useTranslation('jev')
  const [tab, setTab] = useState<'network' | 'library'>('network')
  const [library, setLibrary] = useState<JEVLibrary>()
  const [error, setError] = useState(''), [loading, setLoading] = useState(false), [revision, setRevision] = useState(0)
  const [connected, setConnected] = useState(aopClient.connected)
  const [turn, setTurn] = useState(''), [selected, setSelected] = useState(''), [query, setQuery] = useState('')
  const [node, setNode] = useState('')
  const [networkElement, setNetworkElement] = useState<HTMLDivElement | null>(null)
  const [vertical, setVertical] = useState(false)
  const projection = useMemo(() => projectJEV(events), [events])
  const graph = useMemo(() => projectRuntimeNetwork(events, projection, turn, vertical), [events, projection, turn, vertical])
  const turns = useMemo(() => runtimeTurns(events), [events])
  const latestChange = [...projection.records].reverse().find(r => r.value.payload.case === 'libraryChange')?.event.id
  useEffect(() => aopClient.onConnectionChange(setConnected), [])
  useEffect(() => {
    if (!networkElement) return
    const observer = new ResizeObserver(([entry]) => setVertical(entry.contentRect.width < 950))
    observer.observe(networkElement)
    return () => observer.disconnect()
  }, [networkElement])
  useEffect(() => { setLibrary(undefined); setSelected(''); setTurn(''); setNode(''); setError('') }, [sessionID])
  useEffect(() => {
    if (!open || !sessionID || !connected) return
    let disposed = false
    setLoading(true)
    void getJEVLibrary(sessionID).then(value => {
      if (!disposed) { setLibrary(value); setError('') }
    }).catch(error => { if (!disposed) setError(error instanceof Error ? error.message : String(error)) })
      .finally(() => { if (!disposed) setLoading(false) })
    return () => { disposed = true }
  }, [open, sessionID, connected, revision, latestChange])
  const edges: Edge[] = graph.edges.map(edge => {
    const [key] = String(edge.label).split(' · ')
    return { ...edge, label: `${t(`edges.${key}`)} · ${edge.data?.count}` }
  })
  const definitions = [...(library?.reflexes || []), ...(library?.candidates || []), ...(library?.claims || [])]
  const filtered = definitions.filter(value => `${value.id} ${'context' in value ? value.context : value.when + ' ' + value.decide}`.toLowerCase().includes(query.toLowerCase()))
  const definition = filtered.find(value => value.id === selected) || filtered[0]
  const currentSegments = graph.segments || []
  const selectedNode = graph.nodes.find(n => n.id === node)
  const focused = currentSegments.filter(segment => !selectedNode || selectedNode.id === `agent:${segment.sessionId}` || selectedNode.id === `jev:${segment.sessionId}`
    || selectedNode.data.sessionId === segment.sessionId && segment.steps.some(step => step.call && selectedNode.data.nativeTool === step.call.name
      && (!selectedNode.data.callIds || selectedNode.data.callIds.includes(step.call.id))))
  return <ToolDrawer open={open} onClose={onClose} icon={CircuitBoard} title="Reflex" description={sessionID || t('noSession')}
    titleMeta={<span className="text-xs text-muted-foreground">{!connected ? t('disconnected') : error ? t('unavailable') : library?.mode || ''}</span>}
    actions={<Tooltip><TooltipTrigger asChild><Button variant="ghost" size="icon" aria-label={t('refresh')} disabled={loading || !sessionID || !connected} onClick={() => setRevision(v => v + 1)}>
      <RefreshCw className={cn('h-4 w-4', loading && 'animate-spin')} /></Button></TooltipTrigger><TooltipContent>{t('refresh')}</TooltipContent></Tooltip>}>
    <div className="flex h-full min-h-0 flex-col">
      <JEVUsage events={events} />
      <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 border-b border-border/70 px-4 py-2">
        <div role="tablist" className="flex items-center gap-1">
          {(['network', 'library'] as const).map(value => <button key={value} role="tab" aria-selected={tab === value} className={cn('inline-flex items-center gap-2 rounded-md px-3 py-2 text-xs', tab === value ? 'bg-accent text-foreground' : 'text-muted-foreground hover:bg-accent/50')} onClick={() => setTab(value)}>
            {value === 'network' ? <Network className="h-3.5 w-3.5" /> : <Library className="h-3.5 w-3.5" />}{t(value)}</button>)}
        </div>
        {tab === 'network' && !!turns.length && <select className="max-w-[210px] rounded-md border border-border bg-background p-1.5 text-xs" aria-label={t('turn')} value={turn} onChange={event => { setTurn(event.target.value); setNode('') }}>
          <option value="">{t('currentTurn')}</option>{turns.map((event, index) => <option key={turnKey(event)} value={turnKey(event)}>{t('turnNumber', { count: index + 1 })} · {event.turnId.slice(-8)}</option>)}
        </select>}
      </div>
      {tab === 'network' ? <div className="reflex-network-layout">
        <div ref={setNetworkElement} className="reflex-network-canvas relative min-w-0 shrink-0 border-b border-border/60" data-testid="reflex-network">
          {graph.nodes.length ? <RuntimeNetwork key={`${graph.turn && turnKey(graph.turn)}:${vertical}`} nodes={graph.nodes} edges={edges} vertical={vertical} onSelect={setNode} />
            : <div className="flex h-full min-h-[320px] items-center justify-center text-sm text-muted-foreground">{t('noRuntime')}</div>}
          {!!graph.nodes.length && <div className="pointer-events-none absolute left-3 top-3 flex flex-wrap gap-3 bg-background/90 px-2 py-1.5 text-xs text-muted-foreground">
            <span className="flex items-center gap-1"><Bot className="h-3 w-3 text-blue-500" />Agent</span><span className="flex items-center gap-1"><Repeat2 className="h-3 w-3 text-emerald-500" />JEV</span><span className="flex items-center gap-1"><Wrench className="h-3 w-3 text-amber-500" />{t('tools')}</span>
          </div>}
        </div>
        <div className="reflex-network-detail shrink-0 p-4">
          <h3 className="mb-3 text-sm font-medium">{selectedNode?.data.label || t('currentLoop')}</h3>
          {focused.length ? focused.map(segment => <button key={segment.id} className="mb-2 flex w-full items-center gap-2 rounded-md border border-border px-3 py-2 text-left text-xs hover:bg-accent"
            onClick={() => { onClose(); window.dispatchEvent(new CustomEvent('cyber-workflow-select', { detail: segment.records[0]?.event.id })) }}>
            <Repeat2 className="h-3.5 w-3.5 text-emerald-500" /><span>{t('workflow.openExecution')}</span><span className="ml-auto text-muted-foreground">{t('counts', { calls: segment.steps.filter(step => step.call).length, decisions: segment.records.filter(record => record.value.payload.case === 'decisionResult').length })}</span>
          </button>) : <p className="text-xs text-muted-foreground">{t('noTakeover')}</p>}
        </div>
      </div> : <div className="tool-panel-split">
        <div className="tool-panel-list max-h-64 border-b border-border/60">
          <div className="sticky top-0 border-b border-border/60 bg-background p-3"><label className="flex items-center gap-2 rounded-md border border-border px-2.5 py-2">
            <Search className="h-3.5 w-3.5 shrink-0 text-muted-foreground" /><input value={query} onChange={e => setQuery(e.target.value)} placeholder={t('search')} aria-label={t('search')} className="min-w-0 flex-1 bg-transparent text-xs outline-none" /></label>
            <div className="mt-2 flex gap-3 text-xs text-muted-foreground"><span>Reflex {library?.reflexes.length ?? 0}</span><span>Claim {library?.claims.length ?? 0}</span><span>{t('candidate')} {library?.candidates?.length ?? 0}</span></div>
          </div>
          {!connected && <p className="p-4 text-xs text-muted-foreground">{t('disconnected')}</p>}
          {error && <p role="alert" className="break-words p-4 text-xs text-destructive">{t('unavailable')} · {error}</p>}
          {loading && !library && <p className="p-4 text-xs text-muted-foreground">{t('loading')}</p>}
          {!loading && !error && connected && !filtered.length && <p className="p-4 text-xs text-muted-foreground">{t(!sessionID ? 'noSession' : query ? 'noMatches' : 'emptyLibrary')}</p>}
          {filtered.map(value => <button key={value.id} className={cn('block w-full border-b border-border/40 px-4 py-3 text-left hover:bg-accent/40', definition?.id === value.id && 'bg-accent/60')} onClick={() => setSelected(value.id)}>
            <div className="flex items-center gap-2 text-xs text-muted-foreground">{'observe' in value ? <Repeat2 className="h-3 w-3" /> : <Layers className="h-3 w-3" />}<span>{'observe' in value ? library?.candidates?.some(candidate => candidate.id === value.id) ? t('candidate') : 'Reflex' : 'Claim'}</span><code className="truncate">{value.id}</code></div>
            <p className="mt-1.5 line-clamp-2 break-words text-xs leading-relaxed">{'context' in value ? value.context : value.when}</p>
          </button>)}
        </div>
        <div className="min-h-0 min-w-0 flex-1 overflow-auto p-5">{definition && <JEVDefinition value={definition} />}</div>
      </div>}
    </div>
  </ToolDrawer>
}
