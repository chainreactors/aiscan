import { useEffect, useRef, useState, type FormEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { CheckCircle, Settings, X, Zap, AlertCircle } from 'lucide-react'
import { getConfigStatus, saveConfig, testLLM, testConn } from '../api'
import type { ConfigStatus, ConnCheck, DistributeConfig, LLMTestResult, ServerStatus } from '../api'
import { Button, Input, Select, SelectTrigger, SelectContent, SelectItem, SelectValue, Badge, Spinner } from '@aspect/ui'
import { cn } from '@aspect/theme'
import { useDialogA11y } from '../hooks/useDialogA11y'

interface ConfigPanelProps {
  open: boolean
  status: ServerStatus | null
  onClose: () => void
  onSaved: () => void
}

type TabKey = 'llm' | 'cyberhub' | 'recon' | 'scan' | 'search' | 'ioa' | 'agent'

const TABS: { key: TabKey; label: string }[] = [
  { key: 'llm', label: 'LLM' },
  { key: 'cyberhub', label: 'Cyberhub' },
  { key: 'recon', label: 'Recon' },
  { key: 'scan', label: 'Scan' },
  { key: 'search', label: 'Search' },
  { key: 'ioa', label: 'IOA' },
  { key: 'agent', label: 'Agent' },
]

function emptyForm(): DistributeConfig {
  return {
    llm: { provider: '', base_url: '', api_key: '', model: '', proxy: '' },
    cyberhub: { url: '', key: '', mode: '', proxy: '' },
    recon: { fofa_email: '', fofa_key: '', hunter_token: '', hunter_api_key: '', proxy: '' },
    scan: { verify: '', verify_timeout: 0 },
    search: { tavily_keys: '' },
    ioa: { url: '', token: '', node_name: '', space: '' },
    agent: { tools: [], timeout: 0, save_session: false },
  }
}

function statusToForm(cs: ConfigStatus): DistributeConfig {
  return {
    llm: { provider: cs.llm.provider, base_url: cs.llm.base_url, api_key: '', model: cs.llm.model, proxy: cs.llm.proxy },
    cyberhub: { url: cs.cyberhub.url, key: '', mode: cs.cyberhub.mode, proxy: cs.cyberhub.proxy },
    recon: { fofa_email: cs.recon.fofa_email, fofa_key: '', hunter_token: '', hunter_api_key: '', proxy: cs.recon.proxy, limit: cs.recon.limit },
    scan: { ...cs.scan },
    search: { tavily_keys: '' },
    ioa: { url: cs.ioa.url, token: '', node_name: cs.ioa.node_name, space: cs.ioa.space },
    agent: { ...cs.agent },
  }
}

// sectionStatus returns the readiness badges for the active settings section.
// Each tab surfaces ITS OWN dependencies (Recon → FOFA/Hunter, Search → Tavily,
// IOA → token) instead of repeating the global "LLM ready" badge on every tab.
// Local-only tabs (scan, agent) have no external dependency, so they return none
// and only the global "config loaded" badge shows.
function sectionStatus(
  tab: TabKey,
  cs: ConfigStatus | null,
  status: ServerStatus | null,
  t: (key: string) => string,
): { key: string; label: string; ok: boolean }[] {
  const tag = (name: string, ok: boolean) => ({ key: name, label: `${name} ${ok ? t('configured') : t('notConfigured')}`, ok })
  switch (tab) {
    case 'llm':
      return [{ key: 'llm', label: status?.llm_available ? t('llmReady') : t('llmOffline'), ok: !!status?.llm_available }]
    case 'cyberhub':
      return [tag('Cyberhub', !!(cs?.cyberhub.url && cs?.cyberhub.key_configured))]
    case 'recon':
      return [
        // FOFA's 2023 simplified auth needs only the API key — the email never
        // enters the request URL (see engine/uncover.go). Gate readiness on the
        // key alone so a valid key-only setup doesn't read as "not configured".
        tag('FOFA', !!cs?.recon.fofa_key_configured),
        tag('Hunter', !!(cs?.recon.hunter_api_key_configured || cs?.recon.hunter_token_configured)),
      ]
    case 'search':
      return [tag('Tavily', !!cs?.search.tavily_keys_configured)]
    case 'ioa':
      return [tag('IOA', !!(cs?.ioa.url && cs?.ioa.token_configured))]
    default:
      return [] // scan, agent — local only
  }
}

export default function ConfigPanel({ open, status, onClose, onSaved }: ConfigPanelProps) {
  const { t } = useTranslation('config')
  const [cs, setCs] = useState<ConfigStatus | null>(null)
  const [form, setForm] = useState<DistributeConfig>(emptyForm)
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)
  const [activeTab, setActiveTab] = useState<TabKey>('llm')

  useEffect(() => {
    if (!open) return
    setLoading(true)
    setError('')
    setSaved(false)
    getConfigStatus()
      .then((s) => { setCs(s); setForm(statusToForm(s)) })
      .catch((err: Error) => setError(err.message || t('failedLoad')))
      .finally(() => setLoading(false))
  }, [open])

  // Esc-to-close + focus trap/restore (parity with the Radix-backed ConfirmDialog).
  const panelRef = useRef<HTMLFormElement>(null)
  useDialogA11y(open, onClose, panelRef)

  if (!open) return null

  const handleSave = async (event: FormEvent) => {
    event.preventDefault()
    setSaving(true)
    setError('')
    setSaved(false)
    try {
      const next = await saveConfig(form)
      setCs(next)
      setForm(statusToForm(next))
      setSaved(true)
      onSaved()
    } catch (err: any) {
      setError(err.message || t('failedSave'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-black/50 px-4 py-8 backdrop-blur-md animate-in fade-in duration-200">
      <form ref={panelRef} tabIndex={-1} onSubmit={handleSave} role="dialog" aria-modal="true" aria-labelledby="config-panel-title" className="w-full max-w-3xl overflow-hidden rounded-2xl border border-border/70 bg-card shadow-elevated animate-in fade-in zoom-in-95 duration-200 focus:outline-none">
        <div className="flex items-center justify-between border-b border-border/60 px-4 py-3">
          <div className="flex items-center gap-2">
            <Settings className="h-4 w-4 text-primary" />
            <div>
              <div id="config-panel-title" className="text-sm font-medium text-foreground">{t('settings')}</div>
              <div className="text-xs text-muted-foreground">{cs?.config_path || status?.config_path || 'config.yaml'}</div>
            </div>
          </div>
          <button type="button" onClick={onClose} aria-label={t('close', 'Close')} className="rounded-md p-1 text-muted-foreground hover:bg-accent hover:text-foreground">
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="flex gap-1 overflow-x-auto border-b border-border px-4 py-1">
          {TABS.map((tab) => (
            <button
              key={tab.key} type="button" onClick={() => setActiveTab(tab.key)}
              className={`whitespace-nowrap rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${activeTab === tab.key ? 'bg-primary/10 text-primary' : 'text-muted-foreground hover:bg-accent hover:text-foreground'}`}
            >{tab.label}</button>
          ))}
        </div>

        <div className="p-4">
          {loading ? (
            <div className="flex h-48 flex-col items-center justify-center gap-3 text-muted-foreground">
              <span className="h-8 w-8 shrink-0 animate-spin rounded-full border-2 border-primary/20 border-t-primary" />
              <span className="text-xs">{t('loading')}</span>
            </div>
          ) : (
            <>
              <div className="mb-3 flex flex-wrap items-center gap-2 text-xs">
                {sectionStatus(activeTab, cs, status, t).map((b) => (
                  <Badge key={b.key} variant={b.ok ? 'success' : 'warning'} className="text-xs">{b.label}</Badge>
                ))}
                <Badge variant={cs?.config_loaded ? 'success' : 'warning'} className="text-xs">{cs?.config_loaded ? t('configLoaded') : t('configMissing')}</Badge>
              </div>
              <div className="min-h-[12rem]">
                {activeTab === 'llm' && <LLMTab form={form} setForm={setForm} cs={cs} />}
                {activeTab === 'cyberhub' && <CyberhubTab form={form} setForm={setForm} cs={cs} />}
                {activeTab === 'recon' && <ReconTab form={form} setForm={setForm} cs={cs} />}
                {activeTab === 'scan' && <ScanTab form={form} setForm={setForm} />}
                {activeTab === 'search' && <SearchTab form={form} setForm={setForm} cs={cs} />}
                {activeTab === 'ioa' && <IOATab form={form} setForm={setForm} cs={cs} />}
                {activeTab === 'agent' && <AgentTab form={form} setForm={setForm} />}
              </div>
            </>
          )}

          {error && <div className="mt-3 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">{error}</div>}
          {saved && <div className="mt-3 flex items-center gap-2 rounded-md border border-primary/30 bg-primary/10 px-3 py-2 text-sm text-primary"><CheckCircle className="h-4 w-4" />{t('saved')}</div>}
        </div>

        <div className="flex justify-end gap-2 border-t border-border/60 px-4 py-3">
          <Button type="button" variant="outline" onClick={onClose}>{t('close')}</Button>
          <Button type="submit" disabled={loading || saving}>{saving && <Spinner className="h-4 w-4" />}{t('save')}</Button>
        </div>
      </form>
    </div>
  )
}

type TabProps = { form: DistributeConfig; setForm: React.Dispatch<React.SetStateAction<DistributeConfig>>; cs?: ConfigStatus | null }

function LLMTab({ form, setForm, cs }: TabProps) {
  const { t } = useTranslation('config')
  const u = (k: string, v: string) => setForm((f) => ({ ...f, llm: { ...f.llm, [k]: v } }))
  const [testing, setTesting] = useState(false)
  const [result, setResult] = useState<LLMTestResult | null>(null)

  const handleTest = async () => {
    setTesting(true)
    setResult(null)
    try {
      const res = await testLLM({
        provider: form.llm.provider,
        base_url: form.llm.base_url,
        api_key: form.llm.api_key,
        model: form.llm.model,
        proxy: form.llm.proxy,
      })
      setResult(res)
    } catch (err: any) {
      setResult({ ok: false, provider: form.llm.provider, model: form.llm.model, latency_ms: 0, error: err.message || t('testFailed') })
    } finally {
      setTesting(false)
    }
  }

  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <Field label={t('provider')}>
        <Select value={form.llm.provider} onValueChange={(v) => u('provider', v)}>
          <SelectTrigger className="h-9 w-full"><SelectValue placeholder={t('selectProvider')} /></SelectTrigger>
          <SelectContent>
            {['deepseek','openai','openrouter','ollama','groq','moonshot','anthropic'].map((p) => <SelectItem key={p} value={p}>{p}</SelectItem>)}
          </SelectContent>
        </Select>
      </Field>
      <Field label={t('model')}><Input value={form.llm.model} onChange={(e) => u('model', e.target.value)} placeholder="deepseek-v4-pro / gpt-4.1" /></Field>
      <Field label={t('baseUrl')}><Input value={form.llm.base_url} onChange={(e) => u('base_url', e.target.value)} placeholder={t('providerDefault')} /></Field>
      <Field label={t('proxy')}><Input value={form.llm.proxy} onChange={(e) => u('proxy', e.target.value)} placeholder="http://127.0.0.1:7890" /></Field>
      <div className="sm:col-span-2">
        <Field label={t('apiKey')}>
          <Input type="password" value={form.llm.api_key} onChange={(e) => u('api_key', e.target.value)}
            placeholder={cs?.llm.api_key_configured ? t('configuredKeep') : t('requiredUnlessOllama')} />
        </Field>
      </div>
      <div className="sm:col-span-2 flex items-center gap-3">
        <Button type="button" variant="outline" size="sm" onClick={handleTest} disabled={testing || !form.llm.model}>
          {testing ? <ProbePulse /> : <Zap className="h-4 w-4" />}
          {testing ? t('testing') : t('testConnection')}
        </Button>
        {result && (
          result.ok ? (
            <span className="flex min-w-0 items-center gap-1.5 text-xs text-primary">
              <CheckCircle className="h-3.5 w-3.5 shrink-0" />
              <span>{t('testOk')} · {t('testLatency')} {result.latency_ms}ms{result.reply ? ` · ${t('testReply')}: ${result.reply}` : ''}</span>
            </span>
          ) : (
            <span className="flex min-w-0 items-center gap-1.5 text-xs text-destructive">
              <AlertCircle className="h-3.5 w-3.5 shrink-0" />
              <span className="truncate" title={result.error}>{t('testFailed')}: {result.error}</span>
            </span>
          )
        )}
      </div>
    </div>
  )
}

function CyberhubTab({ form, setForm, cs }: TabProps) {
  const { t } = useTranslation('config')
  const u = (k: string, v: string) => setForm((f) => ({ ...f, cyberhub: { ...f.cyberhub, [k]: v } }))
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <Field label={t('cyberhubUrl')}><Input value={form.cyberhub.url} onChange={(e) => u('url', e.target.value)} placeholder="https://cyberhub.example.com" /></Field>
      <Field label={t('mode')}>
        <Select value={form.cyberhub.mode || 'merge'} onValueChange={(v) => u('mode', v)}>
          <SelectTrigger className="h-9 w-full"><SelectValue placeholder="merge" /></SelectTrigger>
          <SelectContent><SelectItem value="merge">merge</SelectItem><SelectItem value="override">override</SelectItem></SelectContent>
        </Select>
      </Field>
      <Field label={t('proxy')}><Input value={form.cyberhub.proxy} onChange={(e) => u('proxy', e.target.value)} placeholder="socks5://127.0.0.1:1080" /></Field>
      <Field label={t('apiKey')}>
        <Input type="password" value={form.cyberhub.key} onChange={(e) => u('key', e.target.value)}
          placeholder={cs?.cyberhub.key_configured ? t('configuredKeep') : t('cyberhubApiKey')} />
      </Field>
      <ConnTest section="cyberhub" form={form} />
    </div>
  )
}

function ReconTab({ form, setForm, cs }: TabProps) {
  const { t } = useTranslation('config')
  const u = (k: string, v: string) => setForm((f) => ({ ...f, recon: { ...f.recon, [k]: v } }))
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <Field label={t('fofaEmail')}><Input value={form.recon.fofa_email} onChange={(e) => u('fofa_email', e.target.value)} placeholder="account@example.com" /></Field>
      <Field label={t('fofaKey')}><Input type="password" value={form.recon.fofa_key} onChange={(e) => u('fofa_key', e.target.value)} placeholder={cs?.recon.fofa_key_configured ? t('configuredKeep') : t('fofaApiKey')} /></Field>
      <Field label={t('hunterApiKey')}><Input type="password" value={form.recon.hunter_api_key} onChange={(e) => u('hunter_api_key', e.target.value)} placeholder={cs?.recon.hunter_api_key_configured ? t('configuredKeep') : t('hex64')} /></Field>
      <Field label={t('hunterToken')}><Input type="password" value={form.recon.hunter_token} onChange={(e) => u('hunter_token', e.target.value)} placeholder={cs?.recon.hunter_token_configured ? t('configuredKeep') : t('webTokenRare')} /></Field>
      <Field label={t('reconProxy')}><Input value={form.recon.proxy} onChange={(e) => u('proxy', e.target.value)} placeholder="socks5://host:port" /></Field>
      <Field label={t('perQueryLimit')}>
        <Input type="number" value={form.recon.limit ?? ''} onChange={(e) => { const v = e.target.value; setForm((f) => ({ ...f, recon: { ...f.recon, limit: v === '' ? undefined : parseInt(v, 10) } })) }} placeholder={t('unlimited')} />
      </Field>
      <ConnTest section="recon" form={form} />
    </div>
  )
}

function ScanTab({ form, setForm }: Omit<TabProps, 'cs'>) {
  const { t } = useTranslation('config')
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <Field label={t('defaultVerifyMode')}>
        <Select value={form.scan.verify || 'auto'} onValueChange={(v) => setForm((f) => ({ ...f, scan: { ...f.scan, verify: v } }))}>
          <SelectTrigger className="h-9 w-full"><SelectValue placeholder="auto" /></SelectTrigger>
          <SelectContent>{['auto','off','low','high'].map((v) => <SelectItem key={v} value={v}>{v}</SelectItem>)}</SelectContent>
        </Select>
      </Field>
      <Field label={t('verifyTimeout')}>
        <Input type="number" value={form.scan.verify_timeout || ''} onChange={(e) => setForm((f) => ({ ...f, scan: { ...f.scan, verify_timeout: parseInt(e.target.value, 10) || 0 } }))} placeholder="120" />
      </Field>
      <p className="sm:col-span-2 text-xs text-muted-foreground">{t('localOnlyNote')}</p>
    </div>
  )
}

function SearchTab({ form, setForm, cs }: TabProps) {
  const { t } = useTranslation('config')
  return (
    <div className="grid gap-3">
      <Field label={t('tavilyKeys')}>
        <Input type="password" value={form.search.tavily_keys} onChange={(e) => setForm((f) => ({ ...f, search: { tavily_keys: e.target.value } }))}
          placeholder={cs?.search.tavily_keys_configured ? t('configuredKeep') : t('tavilyHint')} />
      </Field>
      <ConnTest section="search" form={form} />
    </div>
  )
}

function IOATab({ form, setForm, cs }: TabProps) {
  const { t } = useTranslation('config')
  const u = (k: string, v: string) => setForm((f) => ({ ...f, ioa: { ...f.ioa, [k]: v } }))
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <Field label={t('ioaServerUrl')}><Input value={form.ioa.url} onChange={(e) => u('url', e.target.value)} placeholder="http://host:port" /></Field>
      <Field label={t('accessToken')}><Input type="password" value={form.ioa.token} onChange={(e) => u('token', e.target.value)} placeholder={cs?.ioa.token_configured ? t('configuredKeep') : t('ioaAccessKey')} /></Field>
      <Field label={t('nodeName')}><Input value={form.ioa.node_name} onChange={(e) => u('node_name', e.target.value)} placeholder={t('autoRegisterNode')} /></Field>
      <Field label={t('space')}><Input value={form.ioa.space} onChange={(e) => u('space', e.target.value)} placeholder="default" /></Field>
      <ConnTest section="ioa" form={form} />
    </div>
  )
}

function AgentTab({ form, setForm }: Omit<TabProps, 'cs'>) {
  const { t } = useTranslation('config')
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <Field label={t('timeout')}>
        <Input type="number" value={form.agent.timeout || ''} onChange={(e) => setForm((f) => ({ ...f, agent: { ...f.agent, timeout: parseInt(e.target.value, 10) || 0 } }))} placeholder="3600" />
      </Field>
      <Field label={t('optionalTools')}>
        <Input value={(form.agent.tools || []).join(', ')} onChange={(e) => { const tools = e.target.value.split(',').map((s) => s.trim()).filter(Boolean); setForm((f) => ({ ...f, agent: { ...f.agent, tools } })) }} placeholder="search, browser" />
      </Field>
      <div className="sm:col-span-2">
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          <input type="checkbox" checked={form.agent.save_session} onChange={(e) => setForm((f) => ({ ...f, agent: { ...f.agent, save_session: e.target.checked } }))} className="rounded border-border" />
          {t('autoSaveSessions')}
        </label>
      </div>
      <p className="sm:col-span-2 text-xs text-muted-foreground">{t('localOnlyNote')}</p>
    </div>
  )
}

// ProbePulse — the indicator shown while a connection test is in flight. A radar
// "ping": concentric signal rings emanate from a luminous source dot, echoing the
// deck's radar/sonar motion signature. Deliberately NOT a rotating ring — probing
// a remote endpoint reads as a pulse outward, not a spinner. Under reduced-motion
// the rings hold static, settling into a quiet reticle.
function ProbePulse({ className }: { className?: string }) {
  return (
    <span
      role="status"
      aria-label="probing"
      className={cn('relative inline-flex h-4 w-4 shrink-0 items-center justify-center', className)}
    >
      <span className="absolute h-2.5 w-2.5 rounded-full border border-primary/60 motion-safe:animate-ping" />
      <span className="absolute h-2.5 w-2.5 rounded-full border border-primary/40 motion-safe:animate-ping [animation-delay:0.5s]" />
      <span className="h-1.5 w-1.5 rounded-full bg-primary shadow-[0_0_6px_hsl(var(--primary)_/_0.9)]" />
    </span>
  )
}

// ConnTest renders a "Test connection" button for a settings section and shows
// one result row per external dependency probed (Recon returns FOFA + Hunter).
// The whole form is sent so unsaved edits are tested; blank secrets fall back to
// the stored values on the server.
function ConnTest({ section, form }: { section: 'cyberhub' | 'recon' | 'search' | 'ioa'; form: DistributeConfig }) {
  const { t } = useTranslation('config')
  const [testing, setTesting] = useState(false)
  const [checks, setChecks] = useState<ConnCheck[] | null>(null)

  const handleTest = async () => {
    setTesting(true)
    setChecks(null)
    try {
      const res = await testConn(section, form)
      setChecks(res.checks)
    } catch (err: any) {
      setChecks([{ name: section, ok: false, latency_ms: 0, error: err.message || t('testFailed') }])
    } finally {
      setTesting(false)
    }
  }

  return (
    <div className="sm:col-span-2 space-y-2">
      <Button type="button" variant="outline" size="sm" onClick={handleTest} disabled={testing}>
        {testing ? <ProbePulse /> : <Zap className="h-4 w-4" />}
        {testing ? t('testing') : t('testConnection')}
      </Button>
      {checks && (
        <div className="space-y-1">
          {checks.map((c, i) => <ConnCheckRow key={`${c.name}-${i}`} check={c} />)}
        </div>
      )}
    </div>
  )
}

const CHECK_LABELS: Record<string, string> = {
  fofa: 'FOFA', hunter: 'Hunter', cyberhub: 'Cyberhub', tavily: 'Tavily', ioa: 'IOA',
}

function ConnCheckRow({ check }: { check: ConnCheck }) {
  const { t } = useTranslation('config')
  const label = CHECK_LABELS[check.name] ?? check.name
  return check.ok ? (
    <span className="flex min-w-0 items-center gap-1.5 text-xs text-primary">
      <CheckCircle className="h-3.5 w-3.5 shrink-0" />
      <span className="truncate">
        {label} · {t('testOk')} · {t('testLatency')} {check.latency_ms}ms{check.detail ? ` · ${check.detail}` : ''}
      </span>
    </span>
  ) : (
    <span className="flex min-w-0 items-center gap-1.5 text-xs text-destructive">
      <AlertCircle className="h-3.5 w-3.5 shrink-0" />
      <span className="truncate" title={check.error}>{label} · {t('testFailed')}: {check.error}</span>
    </span>
  )
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  // Labels sit at near-ink foreground/75 (not muted grey) so the form reads
  // crisp rather than washed-out — matching DeployPanel's Field. See the CJK
  // type notes: for Chinese, weight can't vary (MiSans has two weights), so
  // label legibility comes from ink colour, not a heavier face.
  return <label className="block space-y-1.5"><span className="text-xs font-medium text-foreground/75">{label}</span>{children}</label>
}
