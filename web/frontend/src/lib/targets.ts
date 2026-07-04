/**
 * Client-side target parsing — an advisory mirror of the Go validator in
 * pkg/web/validation.go (validateOneTarget). It powers the scan bar's live
 * count and the "will be skipped" hints as you type.
 *
 * The server stays the authority: it re-validates and now *skips* (rather than
 * aborts on) anything it rejects. So this errs toward "valid when unsure" — a
 * false "looks invalid" here would wrongly scare the user off a target the
 * backend would happily accept, which is worse than letting a dud slip through
 * to the server's own skip list. Keep the rules in sync with validation.go.
 */

export type InvalidReason = 'cidr' | 'scheme' | 'url' | 'format'

export interface InvalidTarget {
  target: string
  reason: InvalidReason
}

export interface ParsedTargets {
  /** Accepted tokens, deduplicated, original order preserved. */
  valid: string[]
  /** Tokens the backend will reject, with a best-effort reason. */
  invalid: InvalidTarget[]
  /** Distinct tokens entered (valid + invalid) — drives the batch badge. */
  total: number
}

const IPV4 = /^\d{1,3}(\.\d{1,3}){3}$/

function isIPv4(s: string): boolean {
  return IPV4.test(s) && s.split('.').every((o) => Number(o) <= 255)
}

/** Classify a single already-trimmed, non-empty token. null == looks valid. */
function classify(token: string): InvalidReason | null {
  // CIDR: an IP (or ip:port) followed by "/prefix". A bare "host/path" without a
  // scheme is also rejected by the backend (isValidHostname bans '/').
  const slash = token.indexOf('/')
  if (slash >= 0 && !token.includes('://')) {
    const prefix = token.slice(0, slash)
    const colon = prefix.lastIndexOf(':')
    const host = colon >= 0 ? prefix.slice(0, colon) : prefix
    if (isIPv4(host)) return 'cidr'
    // A scheme-less "host:port/path" is VALID server-side: Go's net.SplitHostPort
    // accepts it (it doesn't require a numeric port), so validateOneTarget scans
    // it. Only a "host/path" with NO port is rejected (isValidHostname bans '/').
    return colon >= 0 ? null : 'format'
  }

  if (token.includes('://')) {
    let u: URL
    try {
      u = new URL(token)
    } catch {
      return 'url'
    }
    if (!u.hostname) return 'url'
    if (u.protocol !== 'http:' && u.protocol !== 'https:') return 'scheme'
    return null
  }

  // Bare IP, ip:port / host:port, or a dotted hostname. The backend accepts
  // host:port for any host, so anything with a colon is left to it. A bare
  // single-label word (no port, no dot) fails the hostname rule.
  if (!token.includes(':') && !token.includes('.')) return 'format'
  return null
}

export function parseTargets(raw: string): ParsedTargets {
  const seen = new Set<string>()
  const valid: string[] = []
  const invalid: InvalidTarget[] = []
  for (const token of raw.split(/[\s,]+/)) {
    const t = token.trim()
    if (!t || seen.has(t)) continue
    seen.add(t)
    const reason = classify(t)
    if (reason) invalid.push({ target: t, reason })
    else valid.push(t)
  }
  return { valid, invalid, total: valid.length + invalid.length }
}
