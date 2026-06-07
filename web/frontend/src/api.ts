export interface ScanJob {
  id: string;
  target: string;
  mode: string;
  verify?: boolean;
  sniper?: boolean;
  ai?: boolean;
  deep?: boolean;
  status: 'queued' | 'running' | 'completed' | 'failed' | 'canceled';
  progress?: string;
  report?: string;
  result?: ScanResult;
  error?: string;
  created_at: string;
  updated_at: string;
}

export interface ScanResult {
  summary: ScanResultSummary;
  assets?: Asset[];
  services?: unknown[];
  web_probes?: unknown[];
  loots?: Loot[];
  errors?: ResultError[];
}

export interface ScanResultSummary {
  targets: number;
  services: number;
  webs: number;
  probes: number;
  loots: number;
  errors: number;
  tasks: number;
  requests: number;
  duration: string;
  started_at?: string;
  finished_at?: string;
}

export interface Asset {
  id: string;
  key: string;
  target: string;
  title?: string;
  status?: string;
  items?: AssetItem[];
}

export type AssetItemKind = 'service' | 'path' | 'fingerprint' | 'loot' | 'note' | 'response' | 'error';

export interface AssetItem {
  kind: AssetItemKind;
  source?: string;
  target?: string;
  status?: string;
  title?: string;
  summary?: string;
  detail?: string;
  tags?: string[];
  data?: Record<string, unknown>;
  raw?: string;
}

export interface Loot {
  kind: string;
  target: string;
  priority?: string;
  description?: string;
  tags?: string[];
  data?: Record<string, unknown>;
}

export interface ResultError {
  source?: string;
  message: string;
}

export interface ScanEvent {
  type: 'progress' | 'status' | 'complete' | 'error';
  scan_id: string;
  data?: string;
  status?: string;
  error?: string;
  result?: ScanResult;
}

export interface ScanOptions {
  verify: boolean;
  sniper: boolean;
  deep: boolean;
}

export interface ServerStatus {
  llm_available: boolean;
  llm_provider?: string;
  llm_model?: string;
  llm_api_key_configured?: boolean;
  config_path?: string;
  config_loaded: boolean;
}

export interface LLMConfig {
  config_path?: string;
  config_loaded: boolean;
  provider: string;
  base_url: string;
  api_key?: string;
  api_key_configured: boolean;
  model: string;
  proxy: string;
}

export async function getStatus(): Promise<ServerStatus> {
  return apiJSON('/api/status', 'Failed to load status');
}

export async function getLLMConfig(): Promise<LLMConfig> {
  return apiJSON('/api/config/llm', 'Failed to load LLM config');
}

export async function saveLLMConfig(config: LLMConfig): Promise<LLMConfig> {
  return apiJSON('/api/config/llm', 'Failed to save LLM config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  });
}

export async function submitScan(target: string, mode: string, options: ScanOptions): Promise<ScanJob> {
  return apiJSON('/api/scans', 'Failed to submit scan', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ target, mode, ...options }),
  });
}

export async function getScan(id: string): Promise<ScanJob> {
  return apiJSON(`/api/scans/${encodeURIComponent(id)}`, 'Scan not found');
}

export async function listScans(): Promise<ScanJob[]> {
  return apiJSON('/api/scans', 'Failed to list scans');
}

export async function cancelScan(id: string): Promise<void> {
  await apiJSON(`/api/scans/${encodeURIComponent(id)}`, 'Failed to cancel scan', { method: 'DELETE' });
}

export function subscribeScanEvents(
  id: string,
  onEvent: (event: ScanEvent) => void,
): () => void {
  const es = new EventSource(`/api/scans/${encodeURIComponent(id)}/events`);
  const handler = (type: ScanEvent['type']) => (e: Event) => {
    const data = 'data' in e ? (e as MessageEvent).data : undefined;
    if (typeof data !== 'string' || data === '') {
      if (type === 'error') {
        void getScan(id)
          .then((job) => {
            if (job.status === 'completed') {
              onEvent({ type: 'complete', scan_id: id, status: job.status });
              es.close();
            } else if (job.status === 'failed' || job.status === 'canceled') {
              onEvent({
                type: 'error',
                scan_id: id,
                error: job.error || `Scan ${job.status}`,
              });
              es.close();
            }
          })
          .catch(() => {});
      }
      return;
    }

    let event: ScanEvent;
    try {
      event = JSON.parse(data);
    } catch {
      event = { type, scan_id: id, data };
    }

    onEvent(event);
    if (event.type === 'complete' || event.type === 'error') {
      es.close();
    }
  };
  es.addEventListener('progress', handler('progress'));
  es.addEventListener('status', handler('status'));
  es.addEventListener('complete', handler('complete'));
  es.addEventListener('error', handler('error'));

  return () => es.close();
}

async function apiJSON<T>(path: string, fallbackMessage: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, init);
  if (!res.ok) {
    throw new Error(await errorMessage(res, fallbackMessage));
  }
  return res.json();
}

async function errorMessage(res: Response, fallback: string) {
  try {
    const body = await res.json();
    return body?.error || fallback;
  } catch {
    return fallback;
  }
}
