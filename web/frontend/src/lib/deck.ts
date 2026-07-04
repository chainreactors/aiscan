import type { ScanJob, ScanResult } from '../api'
import type { FindingItem } from './scan-result'

/* ============================================================================
   Deck data derivations — turns the raw scan stream into the shapes the CORTEX
   operation deck renders: pipeline phase, progress arc, split log channels,
   throughput, and per-finding AI provenance. All real-data, no mocks.
   ============================================================================ */

export const PIPELINE_PHASES = ['Discovery', 'Web probe', 'Weak-cred', 'POC detect', 'AI verify'] as const
export type PipelinePhase = (typeof PIPELINE_PHASES)[number]

const PHASE_PATTERNS: Array<{ phase: PipelinePhase; re: RegExp }> = [
  { phase: 'Discovery', re: /gogo|port|alive|discover|host up|icmp|nmap|tcp/i },
  { phase: 'Web probe', re: /katana|http|web|probe|crawl|title|favicon|fingerprint|tech/i },
  { phase: 'Weak-cred', re: /zombie|spray|brute|cred|password|login|weakpass/i },
  { phase: 'POC detect', re: /neutron|cve-|poc|nuclei|template|exploit|vuln/i },
  { phase: 'AI verify', re: /verif|sniper|deep|reason|llm|\bai\b|analy|escalat/i },
]

/** Highest pipeline phase touched by the log so far (index into PIPELINE_PHASES). */
export function inferPhaseIndex(lines: string[]): number {
  let max = 0
  for (const line of lines) {
    for (let i = PHASE_PATTERNS.length - 1; i >= 0; i--) {
      if (PHASE_PATTERNS[i].re.test(line)) {
        if (i > max) max = i
        break
      }
    }
  }
  return max
}

/** Progress fraction 0..1. Completed = 1; otherwise driven by how far the
 *  pipeline has advanced (each phase entered ≈ a fifth), nudged by log volume. */
export function deckProgress(scan: ScanJob | null, lines: string[], scanning: boolean, precomputedPhase?: number): number {
  if (!scan) return 0
  if (scan.status === 'completed') return 1
  const phase = precomputedPhase ?? inferPhaseIndex(lines)
  if (scan.status === 'failed' || scan.status === 'canceled') {
    return Math.min(0.95, (phase + 1) / PIPELINE_PHASES.length)
  }
  if (!scanning && scan.status === 'queued') return 0.04
  const base = phase / PIPELINE_PHASES.length
  const within = Math.min(1, lines.length / 60) / PIPELINE_PHASES.length
  return Math.max(0.05, Math.min(0.97, base + within))
}

export type DeckState = 'idle' | 'queued' | 'scanning' | 'complete' | 'failed' | 'canceled'

export function deckState(scan: ScanJob | null, scanning: boolean): DeckState {
  if (!scan) return 'idle'
  if (scanning) return 'scanning'
  switch (scan.status) {
    case 'completed':
      return 'complete'
    case 'failed':
      return 'failed'
    case 'canceled':
      return 'canceled'
    case 'queued':
      return 'queued'
    case 'running':
      return 'scanning'
    default:
      return 'idle'
  }
}

/** Split the progress stream into the AI reasoning channel and the deterministic
 *  scanner channel. AI/verify/reason lines feed the Cortex reasoning panel. */
// Classify by the leading [tag], not free-text: a crawl URL like
// http://…/analysis/… or …/verification/… must NOT be mistaken for AI output.
const AI_LINE = /^\s*\[(ai|verified|not\s+confirmed|inconclusive|sniper|deep|cortex|reason|llm)\b/i
export function splitLog(lines: string[]): { reasoning: string[]; scanner: string[] } {
  const reasoning: string[] = []
  const scanner: string[] = []
  for (const line of lines) {
    if (AI_LINE.test(line)) reasoning.push(line)
    else scanner.push(line)
  }
  return { reasoning, scanner }
}

/** A finding's AI provenance, used to badge the card. */
export function findingAI(f: FindingItem): 'verified' | 'sniper' | 'deep' | null {
  if (f.source === 'verify' && f.status === 'confirmed') return 'verified'
  if (f.source === 'sniper') return 'sniper'
  if (f.source === 'deep') return 'deep'
  return null
}

/** mm:ss elapsed. Live tick while scanning (uses `now`); completed scans use the
 *  authoritative duration string from the summary. */
export function formatElapsed(scan: ScanJob | null, result: ScanResult | null, now: number): string {
  if (!scan) return '00:00'
  if (scan.status === 'completed' && result?.summary?.duration) {
    return result.summary.duration
  }
  const startRaw = result?.summary?.started_at || scan.created_at
  const start = startRaw ? Date.parse(startRaw) : NaN
  if (Number.isNaN(start)) return result?.summary?.duration || '00:00'
  const end = scan.status === 'completed' || scan.status === 'failed' || scan.status === 'canceled'
    ? Date.parse(scan.updated_at || '') || now
    : now
  const secs = Math.max(0, Math.floor((end - start) / 1000))
  const s = String(secs % 60).padStart(2, '0')
  const mins = Math.floor(secs / 60)
  // Roll over into h:mm:ss past the hour so a long scan reads "1:02:15", not "62:15".
  if (mins >= 60) {
    const h = Math.floor(mins / 60)
    const m = String(mins % 60).padStart(2, '0')
    return `${h}:${m}:${s}`
  }
  return `${String(mins).padStart(2, '0')}:${s}`
}
