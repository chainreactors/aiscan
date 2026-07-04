import type { PoolAsset } from '../api'

// Columns worth carrying out of the pool: the target plus the provenance and
// scan metadata that make an exported row actionable elsewhere. Internal ids
// (last_scan_id) stay behind.
const COLUMNS: Array<[header: string, pick: (a: PoolAsset) => string]> = [
  ['target', (a) => a.target],
  ['label', (a) => a.label || ''],
  ['source', (a) => a.source || 'manual'],
  ['status', (a) => a.status || ''],
  ['services', (a) => String(a.services ?? 0)],
  ['loots', (a) => String(a.loots ?? 0)],
  ['first_seen', (a) => a.first_seen || ''],
  ['last_seen', (a) => a.last_seen || ''],
]

// UTF-8 byte-order mark: makes Excel open CJK labels/targets without mojibake.
const BOM = '﻿'

/**
 * Export the asset pool as a CSV the user can archive or feed into other tools.
 * Downloads entirely client-side (the pool is already in memory) using the same
 * Blob→anchor pattern the chat file-download uses — no round-trip to the hub.
 */
export function exportAssetsCSV(assets: PoolAsset[], prefix = 'assets'): void {
  const rows = [COLUMNS.map(([h]) => h), ...assets.map((a) => COLUMNS.map(([, pick]) => pick(a)))]
  const csv = rows.map((r) => r.map(csvCell).join(',')).join('\r\n')
  downloadText(BOM + csv, `${prefix}-${stamp()}.csv`, 'text/csv;charset=utf-8')
}

// Quote a cell only when it carries a comma, quote, or newline; embedded quotes
// are doubled per RFC 4180. Cells that begin with =, +, -, @ (or a leading tab/
// CR) are first prefixed with a tab so a spreadsheet treats attacker-influenced
// recon values (agent-surfaced targets/labels) as text rather than executing
// them as a formula (CSV injection).
function csvCell(value: string): string {
  const guarded = /^[=+\-@\t\r]/.test(value) ? `\t${value}` : value
  return /[",\r\n]/.test(guarded) ? `"${guarded.replace(/"/g, '""')}"` : guarded
}

// Compact local date for the filename so successive exports don't clobber.
function stamp(): string {
  const d = new Date()
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}`
}

function downloadText(content: string, filename: string, mime: string): void {
  const blob = new Blob([content], { type: mime })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  // Revoke after the click is processed so the download isn't cancelled.
  setTimeout(() => URL.revokeObjectURL(url), 0)
}
