import { useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { CheckCircle, Loader2, Plus, Settings, Trash2, X } from 'lucide-react'
import { getLLMConfig, saveLLMConfig } from '../api'
import type { LLMConfig, ServerStatus } from '../api'
import { Button } from './ui/button'
import { Input } from './ui/input'

interface LLMConfigPanelProps {
  open: boolean
  status: ServerStatus | null
  onClose: () => void
  onSaved: () => void
}

const emptyConfig: LLMConfig = {
  config_loaded: false,
  provider: '',
  base_url: '',
  api_key: '',
  api_key_configured: false,
  model: '',
  proxy: '',
  headers: {},
}

type HeaderRow = {
  id: string
  key: string
  value: string
}

let headerRowSeq = 0

function createHeaderRow(key = '', value = ''): HeaderRow {
  headerRowSeq += 1
  return { id: `header-${headerRowSeq}`, key, value }
}

function rowsFromHeaders(headers?: Record<string, string>): HeaderRow[] {
  return Object.entries(headers || {})
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([key, value]) => createHeaderRow(key, value))
}

function headersFromRows(rows: HeaderRow[]): Record<string, string> {
  const headers: Record<string, string> = {}
  for (const row of rows) {
    const key = row.key.trim()
    const value = row.value.trim()
    if (!key || !value) continue
    headers[key] = value
  }
  return headers
}

export default function LLMConfigPanel({ open, status, onClose, onSaved }: LLMConfigPanelProps) {
  const [config, setConfig] = useState<LLMConfig>(emptyConfig)
  const [headerRows, setHeaderRows] = useState<HeaderRow[]>([])
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    if (!open) return
    setLoading(true)
    setError('')
    setSaved(false)
    getLLMConfig()
      .then((cfg) => {
        setConfig({ ...cfg, api_key: '', headers: cfg.headers || {} })
        setHeaderRows(rowsFromHeaders(cfg.headers))
      })
      .catch((err: Error) => setError(err.message || 'Failed to load LLM config'))
      .finally(() => setLoading(false))
  }, [open])

  if (!open) return null

  const update = (key: keyof LLMConfig, value: string) => {
    setConfig((current) => ({ ...current, [key]: value }))
  }

  const updateHeaderRow = (id: string, key: keyof Omit<HeaderRow, 'id'>, value: string) => {
    setHeaderRows((rows) => rows.map((row) => (row.id === id ? { ...row, [key]: value } : row)))
  }

  const addHeaderRow = () => {
    setHeaderRows((rows) => [...rows, createHeaderRow()])
  }

  const removeHeaderRow = (id: string) => {
    setHeaderRows((rows) => rows.filter((row) => row.id !== id))
  }

  const handleSave = async (event: FormEvent) => {
    event.preventDefault()
    setSaving(true)
    setError('')
    setSaved(false)
    try {
      const next = await saveLLMConfig({ ...config, headers: headersFromRows(headerRows) })
      setConfig({ ...next, api_key: '', headers: next.headers || {} })
      setHeaderRows(rowsFromHeaders(next.headers))
      setSaved(true)
      onSaved()
    } catch (err: any) {
      setError(err.message || 'Failed to save LLM config')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-background/80 px-4 py-8 backdrop-blur-sm">
      <form
        onSubmit={handleSave}
        className="w-full max-w-2xl rounded-lg border border-border bg-card shadow-xl"
      >
        <div className="flex items-center justify-between border-b border-border px-4 py-3">
          <div className="flex items-center gap-2">
            <Settings className="h-4 w-4 text-cyber-400" />
            <div>
              <div className="text-sm font-medium text-foreground">LLM Config</div>
              <div className="text-xs text-muted-foreground">
                {config.config_path || status?.config_path || 'config.yaml'}
              </div>
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="rounded-md p-1 text-muted-foreground hover:bg-accent hover:text-foreground"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        <div className="space-y-4 p-4">
          <div className="flex flex-wrap items-center gap-2 text-xs">
            <StatusPill active={!!status?.llm_available} label={status?.llm_available ? 'LLM Ready' : 'LLM Offline'} />
            <StatusPill active={!!config.config_loaded} label={config.config_loaded ? 'Config Loaded' : 'Config Missing'} />
            <StatusPill active={!!config.api_key_configured} label={config.api_key_configured ? 'API Key Set' : 'API Key Empty'} />
          </div>

          {loading ? (
            <div className="flex h-48 items-center justify-center text-muted-foreground">
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              Loading
            </div>
          ) : (
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="Provider">
                <select
                  value={config.provider}
                  onChange={(event) => update('provider', event.target.value)}
                  className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm text-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                >
                  <option value="">Select provider</option>
                  <option value="deepseek">deepseek</option>
                  <option value="openai">openai</option>
                  <option value="openrouter">openrouter</option>
                  <option value="ollama">ollama</option>
                  <option value="groq">groq</option>
                  <option value="moonshot">moonshot</option>
                  <option value="anthropic">anthropic</option>
                </select>
              </Field>

              <Field label="Model">
                <Input
                  value={config.model}
                  onChange={(event) => update('model', event.target.value)}
                  placeholder="deepseek-v4-pro / gpt-4.1 / qwen2.5"
                />
              </Field>

              <Field label="Base URL">
                <Input
                  value={config.base_url}
                  onChange={(event) => update('base_url', event.target.value)}
                  placeholder="leave empty for provider default"
                />
              </Field>

              <Field label="Proxy">
                <Input
                  value={config.proxy}
                  onChange={(event) => update('proxy', event.target.value)}
                  placeholder="http://127.0.0.1:7890"
                />
              </Field>

              <div className="sm:col-span-2">
                <Field label="API Key">
                  <Input
                    type="password"
                    value={config.api_key || ''}
                    onChange={(event) => update('api_key', event.target.value)}
                    placeholder={config.api_key_configured ? 'configured; leave blank to keep current key' : 'required unless provider is ollama'}
                  />
                </Field>
              </div>

              <div className="sm:col-span-2">
                <div className="space-y-2">
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-xs font-medium text-muted-foreground">Custom Headers</span>
                    <Button type="button" variant="outline" size="sm" onClick={addHeaderRow}>
                      <Plus className="h-3.5 w-3.5" />
                      Add
                    </Button>
                  </div>
                  {headerRows.length > 0 && (
                    <div className="space-y-2">
                      {headerRows.map((row) => (
                        <div
                          key={row.id}
                          className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.4fr)_2.25rem]"
                        >
                          <Input
                            value={row.key}
                            onChange={(event) => updateHeaderRow(row.id, 'key', event.target.value)}
                            placeholder="User-Agent"
                          />
                          <Input
                            value={row.value}
                            onChange={(event) => updateHeaderRow(row.id, 'value', event.target.value)}
                            placeholder="Version: 5.10.0 openwarp"
                          />
                          <Button
                            type="button"
                            variant="outline"
                            size="icon"
                            onClick={() => removeHeaderRow(row.id)}
                            aria-label="Remove header"
                          >
                            <Trash2 className="h-4 w-4" />
                          </Button>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              </div>
            </div>
          )}

          {error && (
            <div className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {error}
            </div>
          )}
          {saved && (
            <div className="flex items-center gap-2 rounded-md border border-cyber-400/30 bg-cyber-400/10 px-3 py-2 text-sm text-cyber-700 dark:text-cyber-300">
              <CheckCircle className="h-4 w-4" />
              Saved and runtime reloaded
            </div>
          )}
        </div>

        <div className="flex justify-end gap-2 border-t border-border px-4 py-3">
          <Button type="button" variant="outline" onClick={onClose}>
            Close
          </Button>
          <Button type="submit" disabled={loading || saving} className="bg-cyber-600 text-white hover:bg-cyber-500">
            {saving && <Loader2 className="h-4 w-4 animate-spin" />}
            Save
          </Button>
        </div>
      </form>
    </div>
  )
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="block space-y-1.5">
      <span className="text-xs font-medium text-muted-foreground">{label}</span>
      {children}
    </label>
  )
}

function StatusPill({ active, label }: { active: boolean; label: string }) {
  return (
    <span
      className={`rounded-full border px-2.5 py-1 ${
        active
          ? 'border-cyber-400/30 bg-cyber-400/10 text-cyber-700 dark:text-cyber-300'
          : 'border-yellow-400/30 bg-yellow-400/10 text-yellow-700 dark:text-yellow-300'
      }`}
    >
      {label}
    </span>
  )
}
