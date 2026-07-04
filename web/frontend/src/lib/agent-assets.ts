// A token that is a real network asset we can hand to the scanner: an IP,
// IP:port, hostname (with a dot), host:port, or an http(s) URL. This is an
// allowlist — it deliberately rejects the bash fragments, file paths, and
// search queries that the agent's tool calls also carry, so the pool stays a
// target inventory rather than the raw "触及目标" activity dump.
export function isNetworkAsset(raw: string): boolean {
  const t = raw.trim()
  if (!t || /\s/.test(t)) return false
  if (/^https?:\/\//i.test(t)) return true // url
  if (/^\d{1,3}(\.\d{1,3}){3}(:\d+)?$/.test(t)) return true // ipv4[:port]
  if (/^[a-z0-9-]+(\.[a-z0-9-]+)+(:\d+)?$/i.test(t)) return true // hostname[:port]
  return false
}

// Pull the value(s) a tool call is acting on out of its raw JSON args. Mirrors
// App.tsx's toolTarget but (a) keeps the strings instead of only counting them
// and (b) reads only network-ish keys, so bash `command` / read `path` / search
// `query` args don't leak into the pool.
export function toolArgTargets(toolArgs: string): string[] {
  const raw = (toolArgs || '').trim()
  if (!raw) return []
  try {
    const parsed = JSON.parse(raw)
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      const rec = parsed as Record<string, unknown>
      const out: string[] = []
      for (const k of ['target', 'targets', 'url', 'urls', 'host', 'hosts', 'domain', 'domains', 'ip']) {
        const v = rec[k]
        if (typeof v === 'string' && v.trim()) out.push(v.trim())
        else if (Array.isArray(v)) for (const x of v) if (typeof x === 'string' && x.trim()) out.push(x.trim())
      }
      return out
    }
    return typeof parsed === 'string' ? [parsed] : []
  } catch {
    return [raw] // not JSON — a bare arg string
  }
}

// --- recon-command stdout → assets ------------------------------------------
//
// Recon tools (`passive`, `uncover`) print their *findings* to stdout, not into
// their args, and the agent runs them through the bash tool — so toolArgTargets
// never sees them. These helpers mine that stdout instead, gated on a strict
// tool/command allowlist plus a per-token isNetworkAsset check so only genuine
// assets (not log banners or query strings) reach the pool.

// Shell wrappers/keywords that can sit in front of the actual command word, so
// `sudo passive …` and `for x in …; do uncover …` still register.
const CMD_WRAPPERS = new Set(['do', 'then', 'else', 'elif', 'sudo', 'env', 'time', 'nohup', 'xargs', 'command', 'exec'])

// The shell command a tool call is about to run, pulled from its JSON args.
function commandString(toolArgs: string): string {
  const raw = (toolArgs || '').trim()
  if (!raw) return ''
  try {
    const parsed = JSON.parse(raw)
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      const rec = parsed as Record<string, unknown>
      for (const k of ['command', 'cmd', 'script', 'input']) {
        if (typeof rec[k] === 'string') return rec[k] as string
      }
      return ''
    }
    return typeof parsed === 'string' ? parsed : ''
  } catch {
    return raw
  }
}

// Does this shell command *run* passive/uncover? Split into command segments on
// shell control operators (\x60 == backtick), then check each segment's leading
// word — skipping env-assignments and wrappers — so the tool must be the command
// executed, not merely an argument (`curl https://passive.x`) or a mention
// (`echo passive`).
function runsReconCommand(cmd: string): boolean {
  for (const seg of cmd.split(/[\n;|&()\x60]/)) {
    const words = seg.trim().split(/\s+/)
    let i = 0
    while (i < words.length && (/^[A-Za-z_]\w*=/.test(words[i]) || CMD_WRAPPERS.has(words[i]))) i++
    if (words[i] === 'passive' || words[i] === 'uncover') return true
  }
  return false
}

// Hosts embedded in a shell command's URLs, e.g. the `desk.redhaze.top` in
// `curl -s https://desk.redhaze.top/desk/login`. The agent does most of its
// recon by running curl/wget/httpx through the bash tool, so its args carry a
// `command` string, not a network-keyed field — without this both the 触及目标
// stat and the asset pool would never see those targets. We take only
// http(s):// URLs (unambiguous — unlike bare tokens, which collide with
// filenames like `report.txt`) and reduce each to host[:port], so N paths on
// one host count as a single touched target.
function commandURLHosts(cmd: string): string[] {
  const out: string[] = []
  for (const m of cmd.match(/\bhttps?:\/\/[^\s"'`)>|\\]+/gi) ?? []) {
    try {
      const u = new URL(m)
      const host = u.port ? `${u.hostname}:${u.port}` : u.hostname
      if (isNetworkAsset(host)) out.push(host)
    } catch {
      // malformed URL fragment — skip
    }
  }
  return out
}

// Is this tool call a recon command whose stdout we should mine for assets?
// Matches a structured `passive`/`uncover` tool by name, or a bash call that
// runs one.
export function isReconToolCall(toolName: string, toolArgs: string): boolean {
  const name = (toolName || '').trim().toLowerCase()
  if (name === 'passive' || name === 'uncover') return true
  return runsReconCommand(commandString(toolArgs))
}

// The single best scannable target for one uncover/passive record. passive emits
// {ip,port,url,domain,...} (fofa/hunter) or {ip,port,url,host,source} (generic);
// prefer the most specific named target (url), then host/domain[:port], then
// ip[:port]. Every candidate is re-checked so blank/garbage fields drop out.
function recordAsset(rec: Record<string, unknown>): string | null {
  const str = (k: string): string => {
    const v = rec[k]
    return typeof v === 'string' ? v.trim() : typeof v === 'number' ? String(v) : ''
  }
  const port = str('port')
  const withPort = (h: string) => (port && port !== '0' ? `${h}:${port}` : h)

  const url = str('url')
  if (url && isNetworkAsset(url)) return url

  const host = str('host') || str('domain')
  if (host) {
    if (isNetworkAsset(withPort(host))) return withPort(host)
    if (isNetworkAsset(host)) return host
  }

  const ip = str('ip')
  if (ip) {
    if (isNetworkAsset(withPort(ip))) return withPort(ip)
    if (isNetworkAsset(ip)) return ip
  }
  return null
}

// Parse a recon tool's stdout into distinct network assets. passive prints a
// single JSON array of records; uncover (called directly, `-json`) prints one
// JSON record per line; ad-hoc/default output prints one bare token per line.
// All three are handled, and isNetworkAsset gates the bare tokens so progress
// lines / banners never leak in.
export function resultAssets(result: string | undefined): string[] {
  const text = (result || '').trim()
  if (!text) return []
  const out: string[] = []

  const harvest = (value: unknown): void => {
    const rows = Array.isArray(value) ? value : [value]
    for (const row of rows) {
      if (row && typeof row === 'object' && !Array.isArray(row)) {
        const cand = recordAsset(row as Record<string, unknown>)
        if (cand) out.push(cand)
      } else if (typeof row === 'string' && isNetworkAsset(row.trim())) {
        out.push(row.trim())
      }
    }
  }
  const parseJSON = (s: string): unknown | undefined => {
    try {
      return JSON.parse(s)
    } catch {
      return undefined
    }
  }

  // passive's whole output is one JSON value — harvest it and we're done.
  const whole = parseJSON(text)
  if (whole !== undefined) {
    harvest(whole)
    return out
  }

  // Otherwise line by line: a bare host/ip/url token, or a JSON record per line.
  for (const line of text.split('\n')) {
    const tok = line.trim()
    if (!tok) continue
    if (isNetworkAsset(tok)) {
      out.push(tok)
      continue
    }
    if (tok[0] === '{' || tok[0] === '[') {
      const rec = parseJSON(tok)
      if (rec !== undefined) harvest(rec)
    }
  }
  return out
}

// Every network asset a single tool call contributes: from its structured args
// (scan/recon tools that carry target/url/host), from any http(s):// URL in its
// shell command (curl/wget/httpx via the bash tool), and, for recon commands,
// from its stdout listing. Shared by the asset pool and the deck's "触及目标"
// stat so the two never disagree.
export function toolCallAssets(toolName: string, toolArgs: string, result?: string): string[] {
  const out: string[] = []
  for (const cand of toolArgTargets(toolArgs)) if (isNetworkAsset(cand)) out.push(cand)
  out.push(...commandURLHosts(commandString(toolArgs)))
  if (isReconToolCall(toolName, toolArgs)) out.push(...resultAssets(result))
  return out
}
