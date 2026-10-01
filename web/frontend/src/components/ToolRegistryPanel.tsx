import { useMemo, useState } from 'react'
import { Search, Wrench } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { EmptyState, Input } from '@cyber/ui'
import type { AgentView } from '../api'
import { ToolDefinitionCard } from '@/viewer'

/** Original tool definitions and commands from the selected agent. */
export default function ToolRegistryPanel({ agent }: { agent: AgentView }) {
  const { t } = useTranslation('tools')
  const [query, setQuery] = useState('')
  const tools = useMemo(() => [...(agent.hello?.tools ?? []), ...agent.commands.filter(tool => tool.name.startsWith('!') && tool.name.length > 1)], [agent.hello?.tools, agent.commands])
  const visible = useMemo(() => {
    const needle = query.trim().toLocaleLowerCase()
    return needle ? tools.filter(tool => [tool.name, tool.description, ...('usage' in tool ? [tool.usage, ...tool.aliases] : [])]
      .some(value => value.toLocaleLowerCase().includes(needle))) : tools
  }, [tools, query])

  return <div className="flex h-full min-h-0 flex-col" data-testid="tool-registry" data-agent-id={agent.hello?.nodeId}>
    <div className="shrink-0 space-y-2 border-b border-border px-4 py-3">
      <div className="relative">
        <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" aria-hidden="true" />
        <Input value={query} onChange={event => setQuery(event.target.value)} placeholder={t('searchPlaceholder')} aria-label={t('searchPlaceholder')} className="h-8 pl-9 text-xs" />
      </div>
      <div className="flex items-center justify-between gap-3 text-[11px] text-muted-foreground">
        <span>{t('resultCount', { count: visible.length })}</span>
        <span className="flex items-center gap-1.5"><Wrench className="h-3 w-3" aria-hidden="true" />{t('transportHint')}</span>
      </div>
    </div>
    <div className="min-h-0 flex-1 overflow-auto p-4">
      {visible.length === 0 ? <EmptyState compact icon={Wrench} title={query.trim() ? t('noMatches') : t('empty')} description={query.trim() ? t('noMatchesDescription') : t('agentEmpty')} /> :
        <div className="mx-auto max-w-5xl space-y-2">{visible.map((tool, index) =>
          <ToolDefinitionCard key={`${tool.$typeName}:${tool.name}:${index}`} {...tool} labels={{ fallbackDescription: t('fallbackDescription'), usage: t('usage'), descriptionLabel: t('descriptionLabel'), aliases: t('aliases'), inputSchema: t('inputSchema') }} />,
        )}</div>}
    </div>
  </div>
}
