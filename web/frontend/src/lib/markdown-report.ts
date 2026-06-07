import type { Asset, AssetItem, Loot, ScanJob, ScanResult } from '../api'

type ReportScan = Pick<ScanJob, 'target' | 'mode' | 'created_at' | 'updated_at'>

export function buildMarkdownReport(scan: ReportScan, result: ScanResult): string {
  const lines: string[] = []
  const loots = result.loots || []
  const assets = result.assets || []

  lines.push('# Penetration Test Report', '')
  lines.push(`**Target:** ${markdownCode(scan.target)}`)
  lines.push(`**Mode:** ${scan.mode || 'quick'}`)
  lines.push(`**Date:** ${formatDate(scan.updated_at || scan.created_at)}`, '')
  lines.push('---', '')

  writeSummary(lines, result)
  writeLootAnalysis(lines, loots)
  writeLootTable(lines, loots)
  writeAssets(lines, assets, loots.length > 0)
  writeErrors(lines, result.errors || [])

  return `${lines.join('\n').trim()}\n`
}

function writeSummary(lines: string[], result: ScanResult) {
  const summary = result.summary
  lines.push('## Summary', '')
  lines.push('| Metric | Value |')
  lines.push('|---|---:|')
  lines.push(`| Targets | ${summary.targets || 0} |`)
  lines.push(`| Services | ${summary.services || 0} |`)
  lines.push(`| Web | ${summary.webs || 0} |`)
  lines.push(`| Probes | ${summary.probes || 0} |`)
  lines.push(`| Fingerprints | ${fingerprintCount(result.assets || [])} |`)
  lines.push(`| Loots | ${summary.loots || (result.loots || []).length || assetLootCount(result.assets || [])} |`)
  lines.push(`| Errors | ${summary.errors || 0} |`)
  if (summary.duration) {
    lines.push(`| Duration | ${escapeTableCell(summary.duration)} |`)
  }
  lines.push('')
}

function writeLootAnalysis(lines: string[], loots: Loot[]) {
  const entries = loots
    .map((loot) => ({
      loot,
      summary: lootSummary(loot),
      content: lootContent(loot),
    }))
    .filter((entry) => entry.content)

  if (entries.length === 0) {
    return
  }

  lines.push('## Analysis', '')
  for (const { loot, summary, content } of entries) {
    const title = summary || `${loot.kind || 'loot'} ${loot.target || ''}`.trim()
    lines.push(`### ${markdownHeading(title)}`, '')
    if (loot.kind) lines.push(`**Kind:** ${markdownCode(loot.kind)}`, '')
    if (loot.priority) lines.push(`**Priority:** ${markdownCode(loot.priority)}`, '')
    if (loot.target) lines.push(`**Target:** ${markdownCode(loot.target)}`, '')
    if (content && !sameText(summary, content)) {
      lines.push(content, '')
    } else if (summary) {
      lines.push(summary, '')
    }
  }
}

function writeLootTable(lines: string[], loots: Loot[]) {
  if (loots.length === 0) {
    return
  }

  lines.push('## Loots', '')
  lines.push('| Kind | Target | Priority | Description |')
  lines.push('|---|---|---|---|')
  for (const loot of loots) {
    lines.push([
      tableCell(loot.kind),
      tableCell(loot.target),
      tableCell(loot.priority || ''),
      tableCell(loot.description || firstMarkdownLine(lootContent(loot))),
    ].join(' | ').replace(/^/, '| ').replace(/$/, ' |'))
  }
  lines.push('')
}

function writeAssets(lines: string[], assets: Asset[], skipLootItems: boolean) {
  if (assets.length === 0) {
    return
  }

  lines.push('## Assets', '')
  for (const asset of assets) {
    const title = firstText(asset.title, asset.target, asset.key, 'Asset')
    lines.push(`### ${markdownHeading(title)}`, '')
    if (asset.target && asset.target !== title) {
      lines.push(`- **Target:** ${markdownCode(asset.target)}`)
    }
    if (asset.status) {
      lines.push(`- **State:** ${markdownCode(asset.status)}`)
    }
    writeInlineList(lines, 'Services', assetServiceFacts(asset.items || []))
    writeInlineList(lines, 'HTTP', assetHTTPStatuses(asset.items || []))
    writeInlineList(lines, 'Fingers', assetFingers(asset.items || []))
    writeInlineList(lines, 'Sources', assetSources(asset.items || []))
    const pathCount = (asset.items || []).filter((item) => item.kind === 'path').length
    if (pathCount > 0) {
      lines.push(`- **Paths:** ${pathCount}`)
    }
    lines.push('')
    writeAssetAnalysis(lines, asset.items || [], skipLootItems)
  }
}

function writeAssetAnalysis(lines: string[], items: AssetItem[], skipLootItems: boolean) {
  const entries = items
    .filter((item) => ['loot', 'note', 'response', 'error'].includes(item.kind))
    .filter((item) => !(skipLootItems && item.kind === 'loot'))
    .map((item) => ({
      item,
      summary: firstText(item.summary, item.title),
      content: itemContent(item),
    }))
    .filter((entry) => entry.summary || entry.content)

  if (entries.length === 0) {
    return
  }

  lines.push('#### Analysis', '')
  for (const { item, summary, content } of entries) {
    const title = summary || firstMarkdownLine(content) || item.kind
    lines.push(`##### ${markdownHeading(title)}`, '')
    const source = [firstText(item.source, item.kind), item.status].filter(Boolean).join(':')
    if (source) {
      lines.push(`**Source:** ${markdownCode(source)}`, '')
    }
    if (content && !sameText(summary, content)) {
      lines.push(content, '')
    } else if (summary) {
      lines.push(summary, '')
    }
  }
}

function writeErrors(lines: string[], errors: ScanResult['errors']) {
  if (!errors || errors.length === 0) {
    return
  }
  lines.push('## Errors', '')
  for (const error of errors) {
    lines.push(`- ${firstText(error.source, 'scan')}: ${error.message}`)
  }
  lines.push('')
}

function writeInlineList(lines: string[], label: string, values: string[]) {
  if (values.length === 0) {
    return
  }
  lines.push(`- **${label}:** ${values.map(markdownCode).join(', ')}`)
}

function lootSummary(loot: Loot) {
  return firstText(loot.description, firstMarkdownLine(lootContent(loot)))
}

function lootContent(loot: Loot) {
  return firstText(
    dataText(loot.data?.content),
    dataText(loot.data?.detail),
    dataText(loot.data?.markdown),
    dataText(loot.data?.narrative),
    dataText(loot.data?.evidence),
    dataText(loot.data?.response),
    dataText(loot.data?.output),
  )
}

function itemContent(item: AssetItem) {
  return firstText(
    item.detail,
    dataText(item.data?.content),
    dataText(item.data?.detail),
    dataText(item.data?.markdown),
    dataText(item.data?.narrative),
    dataText(item.data?.evidence),
    dataText(item.data?.response),
    dataText(item.data?.output),
  )
}

function assetServiceFacts(items: AssetItem[]) {
  return compactStrings(items
    .filter((item) => item.kind === 'service')
    .map((item) => compactStrings([
      dataText(item.data?.protocol),
      dataText(item.data?.service),
      dataText(item.data?.port),
    ]).join(' ')))
}

function assetHTTPStatuses(items: AssetItem[]) {
  return compactStrings(items
    .filter((item) => item.kind === 'path')
    .map((item) => firstText(item.status, dataText(item.data?.status))))
}

function assetFingers(items: AssetItem[]) {
  return compactStrings(items.flatMap((item) => {
    if (item.kind === 'fingerprint') {
      return [firstText(item.title, dataText(item.data?.name))]
    }
    if (item.kind === 'path') {
      return dataList(item.data?.fingers)
    }
    return []
  }))
}

function assetSources(items: AssetItem[]) {
  return compactStrings(items.map((item) => firstText(item.source, dataText(item.data?.source))))
}

function fingerprintCount(assets: Asset[]) {
  return assetFingers(assets.flatMap((asset) => asset.items || [])).length
}

function assetLootCount(assets: Asset[]) {
  return assets
    .flatMap((asset) => asset.items || [])
    .filter((item) => item.kind === 'loot' && dataText(item.data?.kind).toLowerCase() !== 'fingerprint')
    .length
}

function dataText(value: unknown) {
  if (typeof value === 'string') return value
  if (typeof value === 'number' && Number.isFinite(value)) return String(value)
  return ''
}

function dataList(value: unknown) {
  if (Array.isArray(value)) {
    return value.map(dataText).filter(Boolean)
  }
  if (typeof value === 'string') {
    return value.split(/[;,]/).map((part) => part.trim()).filter(Boolean)
  }
  return []
}

function firstMarkdownLine(value: string) {
  return value.split('\n').map((line) => line.trim()).find(Boolean) || ''
}

function firstText(...values: Array<string | undefined>) {
  return values.find((value) => value && value.trim())?.trim() || ''
}

function compactStrings(values: string[]) {
  const seen = new Set<string>()
  const out: string[] = []
  for (const value of values) {
    const trimmed = value.trim()
    const key = trimmed.toLowerCase()
    if (!trimmed || seen.has(key)) {
      continue
    }
    seen.add(key)
    out.push(trimmed)
  }
  return out
}

function sameText(left: string, right: string) {
  return left.trim() === right.trim()
}

function markdownCode(value: string) {
  return `\`${String(value).replace(/`/g, "'")}\``
}

function markdownHeading(value: string) {
  return value.trim().replace(/\s*\n+\s*/g, ' ').replace(/^#+\s*/, '') || 'Analysis'
}

function tableCell(value: string) {
  return escapeTableCell(value.replace(/\s*\n+\s*/g, ' ').trim())
}

function escapeTableCell(value: string) {
  return value.replace(/\|/g, '\\|')
}

function formatDate(value: string) {
  if (!value) {
    return new Date().toLocaleString()
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return date.toLocaleString()
}
