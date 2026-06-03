export interface ScanJob {
  id: string;
  target: string;
  mode: string;
  verify?: boolean;
  sniper?: boolean;
  ai?: boolean;
  deep?: boolean;
  status: 'queued' | 'running' | 'completed' | 'failed' | 'cancelled';
  progress?: string;
  report?: string;
  result?: StructuredResult;
  error?: string;
  created_at: string;
  updated_at: string;
}

export interface StructuredResult {
  summary: StructuredSummary;
  assets?: Asset[];
  services?: StructuredService[];
  web_endpoints?: StructuredWebEndpoint[];
  web_probes?: StructuredWebEndpoint[];
  fingerprints?: StructuredFingerprint[];
  risks?: StructuredFinding[];
  vulns?: StructuredFinding[];
  ai?: StructuredFinding[];
  errors?: StructuredError[];
}

export interface StructuredSummary {
  targets: number;
  services: number;
  webs: number;
  probes: number;
  fingerprints: number;
  risks: number;
  vulns: number;
  verified: number;
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

export interface AssetItem {
  kind: string;
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

export interface StructuredService {
  target: string;
  ip?: string;
  port?: string;
  protocol?: string;
  service?: string;
  banner?: string;
  raw?: string;
  is_web?: boolean;
}

export interface StructuredWebEndpoint {
  url: string;
  host_header?: string;
  source?: string;
  status?: number;
  title?: string;
  fingers?: string[];
  raw?: string;
}

export interface StructuredFingerprint {
  target: string;
  name: string;
  source?: string;
  focus?: boolean;
}

export interface StructuredFinding {
  kind: string;
  target?: string;
  priority?: string;
  status?: string;
  summary?: string;
  detail?: string;
  evidence?: string;
  skill?: string;
  source?: string;
  original_kind?: string;
  original_key?: string;
  raw?: string;
}

export interface StructuredError {
  source?: string;
  message: string;
}

export interface ScanEvent {
  type: 'progress' | 'status' | 'complete' | 'error';
  scan_id: string;
  data?: string;
  status?: string;
  error?: string;
  result?: StructuredResult;
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

const isWails = !!(window as any).go;

export async function getStatus(): Promise<ServerStatus> {
  if (isWails) {
    const app = (window as any)['go']?.['main']?.['App'];
    if (app?.GetStatus) {
      return app.GetStatus();
    }
    return { llm_available: true, config_loaded: false };
  }
  const res = await fetch('/api/status');
  if (!res.ok) throw new Error('Failed to load status');
  return res.json();
}

export async function getLLMConfig(): Promise<LLMConfig> {
  if (isWails) {
    const app = (window as any)['go']?.['main']?.['App'];
    if (app?.GetLLMConfig) {
      return app.GetLLMConfig();
    }
    throw new Error('LLM config is not available in this runtime');
  }
  const res = await fetch('/api/config/llm');
  if (!res.ok) {
    const err = await res.json();
    throw new Error(err.error || 'Failed to load LLM config');
  }
  return res.json();
}

export async function saveLLMConfig(config: LLMConfig): Promise<LLMConfig> {
  if (isWails) {
    const app = (window as any)['go']?.['main']?.['App'];
    if (app?.SaveLLMConfig) {
      return app.SaveLLMConfig(config);
    }
    throw new Error('LLM config is not available in this runtime');
  }
  const res = await fetch('/api/config/llm', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  });
  if (!res.ok) {
    const err = await res.json();
    throw new Error(err.error || 'Failed to save LLM config');
  }
  return res.json();
}

export async function submitScan(target: string, mode: string, options: ScanOptions): Promise<ScanJob> {
  if (isWails) {
    return (window as any)['go']['main']['App']['SubmitScan'](
      target,
      mode,
      options.verify,
      options.sniper,
      options.deep,
    );
  }
  const res = await fetch('/api/scans', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ target, mode, ...options }),
  });
  if (!res.ok) {
    const err = await res.json();
    throw new Error(err.error || 'Failed to submit scan');
  }
  return res.json();
}

export async function getScan(id: string): Promise<ScanJob> {
  if (isWails) {
    return (window as any)['go']['main']['App']['GetScan'](id);
  }
  const res = await fetch(`/api/scans/${id}`);
  if (!res.ok) throw new Error('Scan not found');
  return res.json();
}

export async function listScans(): Promise<ScanJob[]> {
  if (isWails) {
    return (window as any)['go']['main']['App']['ListScans']();
  }
  const res = await fetch('/api/scans');
  if (!res.ok) throw new Error('Failed to list scans');
  return res.json();
}

export async function cancelScan(id: string): Promise<void> {
  if (isWails) {
    return (window as any)['go']['main']['App']['CancelScan'](id);
  }
  await fetch(`/api/scans/${id}`, { method: 'DELETE' });
}

export async function getReport(id: string): Promise<string> {
  if (isWails) {
    return (window as any)['go']['main']['App']['GetReport'](id);
  }
  const res = await fetch(`/api/scans/${id}/report`);
  if (!res.ok) throw new Error('Report not ready');
  return res.text();
}

export function subscribeScanEvents(
  id: string,
  onEvent: (event: ScanEvent) => void,
): () => void {
  if (isWails) {
    const runtime = (window as any).runtime;
    if (runtime?.EventsOn) {
      runtime.EventsOn(`scan:${id}`, (event: ScanEvent) => onEvent(event));
      return () => runtime.EventsOff(`scan:${id}`);
    }
  }

  const es = new EventSource(`/api/scans/${id}/events`);
  const handler = (type: ScanEvent['type']) => (e: Event) => {
    const data = 'data' in e ? (e as MessageEvent).data : undefined;
    if (typeof data !== 'string' || data === '') {
      if (type === 'error') {
        void getScan(id)
          .then((job) => {
            if (job.status === 'completed') {
              onEvent({ type: 'complete', scan_id: id, status: job.status });
              es.close();
            } else if (job.status === 'failed' || job.status === 'cancelled') {
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
