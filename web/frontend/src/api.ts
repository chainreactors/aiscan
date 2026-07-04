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
  project?: string;
  /** Batch targets dropped by validation on submit (transient, create only). */
  skipped?: { target: string; reason: string }[];
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

// One entry in the shared asset pool (deduplicated by target). Source is
// 'scan' | 'agent' | 'manual'.
export interface PoolAsset {
  id: string;
  project_id?: string;
  target: string;
  label?: string;
  source?: string;
  status?: string;
  note?: string;
  services?: number;
  loots?: number;
  last_scan_id?: string;
  first_seen: string;
  last_seen: string;
}

export interface ScanEvent {
  type: 'progress' | 'status' | 'stats' | 'complete' | 'error';
  scan_id: string;
  data?: string;
  status?: string;
  error?: string;
  result?: ScanResult;
}

type RawScanEventType = ScanEvent['type'] | 'output';

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
  agents: number;
}

export interface AgentInfo {
  id: string;
  name: string;
  commands?: string[];
  busy: boolean;
  connected_at: string;
  identity?: AgentIdentity;
  stats?: AgentStats;
}

export interface AgentIdentity {
  node_id?: string;
  node_name?: string;
  space?: string;
  ioa_url?: string;
  hostname?: string;
  username?: string;
  working_dir?: string;
  os?: string;
  arch?: string;
  pid?: number;
  provider?: string;
  model?: string;
  capabilities?: string[];
  meta?: Record<string, unknown>;
}

export interface AgentStats {
  turns?: number;
  tool_calls?: number;
  running_tools?: number;
  prompt_tokens?: number;
  completion_tokens?: number;
  total_tokens?: number;
  cache_read_tokens?: number;
  cache_write_tokens?: number;
  assets?: number;
  loots?: number;
  last_event?: string;
  current_tool?: string;
  current_detail?: string;
}

// ConfigStatus — GET /api/config response (secrets masked, *_configured flags)
export interface ConfigStatus {
  config_path?: string;
  config_loaded: boolean;
  llm: { provider: string; base_url: string; api_key_configured: boolean; model: string; proxy: string };
  cyberhub: { url: string; key_configured: boolean; mode: string; proxy: string };
  recon: { fofa_email: string; fofa_key_configured: boolean; hunter_token_configured: boolean; hunter_api_key_configured: boolean; proxy: string; limit?: number };
  scan: { verify: string; verify_timeout: number };
  search: { tavily_keys_configured: boolean };
  ioa: { url: string; token_configured: boolean; node_name: string; space: string };
  agent: { tools: string[]; timeout: number; save_session: boolean };
}

// DistributeConfig — PUT /api/config request body (with secret values)
export interface DistributeConfig {
  llm: { provider: string; base_url: string; api_key: string; model: string; proxy: string };
  cyberhub: { url: string; key: string; mode: string; proxy: string };
  recon: { fofa_email: string; fofa_key: string; hunter_token: string; hunter_api_key: string; proxy: string; limit?: number };
  scan: { verify: string; verify_timeout: number };
  search: { tavily_keys: string };
  ioa: { url: string; token: string; node_name: string; space: string };
  agent: { tools: string[]; timeout: number; save_session: boolean };
}

export interface TerminalMessage {
  type: string;
  task_id?: string;
  stream_id?: string;
  data?: string;
  data_b64?: string;
  payload?: Record<string, unknown>;
}

export async function getStatus(): Promise<ServerStatus> {
  return apiJSON('/api/status', 'Failed to load status');
}

export async function listAgents(): Promise<AgentInfo[]> {
  return apiJSON('/api/agents', 'Failed to list agents');
}

export async function getConfigStatus(): Promise<ConfigStatus> {
  return apiJSON('/api/config', 'Failed to load config');
}

export async function saveConfig(config: DistributeConfig): Promise<ConfigStatus> {
  return apiJSON('/api/config', 'Failed to save config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  });
}

// LLMTestRequest — POST /api/config/llm/test body. Leave api_key blank to
// reuse the key already stored on the server.
export interface LLMTestRequest {
  provider: string;
  base_url: string;
  api_key: string;
  model: string;
  proxy: string;
}

// LLMTestResult — outcome of a connectivity probe. ok=false carries the
// failure reason in `error`; transport/HTTP errors never reject the promise.
export interface LLMTestResult {
  ok: boolean;
  provider: string;
  model: string;
  latency_ms: number;
  reply?: string;
  error?: string;
}

export async function testLLM(req: LLMTestRequest): Promise<LLMTestResult> {
  return apiJSON('/api/config/llm/test', 'Failed to test LLM', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
}

// ConnCheck — outcome of probing one external dependency within a settings
// section. A section may return several checks (Recon probes FOFA and Hunter
// independently). ok=false carries the reason in `error`.
export interface ConnCheck {
  name: string; // fofa | hunter | cyberhub | tavily | ioa | recon
  ok: boolean;
  latency_ms: number;
  detail?: string;
  error?: string;
}

export interface ConnTestResponse {
  checks: ConnCheck[];
}

// testConn probes the external dependencies of a settings section
// (cyberhub | recon | search | ioa). The current (possibly unsaved) form is
// sent so edits are tested; blank secrets fall back to stored values server-side.
export async function testConn(section: string, config: DistributeConfig): Promise<ConnTestResponse> {
  return apiJSON(`/api/config/${section}/test`, 'Failed to test connection', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(config),
  });
}

// --- Cloud auto-deploy types ---

export interface CloudCredentialView {
  id: string
  name: string
  provider: string
  access_key_id: string // masked
  default_region: string
  secret_configured: boolean
}

export interface SaveCredentialInput {
  id?: string
  name?: string
  provider: string
  access_key_id: string
  access_key_secret: string
  default_region: string
}

export interface CloudRegion {
  id: string
  local_name?: string
}

export interface ListRegionsInput {
  cloud_id?: string
  provider?: string
  access_key_id?: string
  access_key_secret?: string
}

export interface CloudImage {
  id: string
  name?: string
  os_name?: string
  platform?: string
  arch?: string
}

export interface CloudInstanceType {
  id: string
  cpu: number
  memory_gib: number
}

export interface ListImagesInput {
  cloud_id: string
  region?: string
}

export interface ListInstanceTypesInput {
  cloud_id: string
  region?: string
  zone?: string
}

export interface NodeProgress {
  phase: string // booting | downloading | installing | starting
  bytes?: number
  total?: number
  age_sec: number
}

export interface DeployNodeView {
  instance_id: string
  node_name: string
  public_ip?: string
  private_ip?: string
  status?: string
  registered: boolean
  agent_id?: string
  busy?: boolean
  progress?: NodeProgress
}

export interface DeployRecordView {
  id: string
  cloud_id: string
  provider: string
  region: string
  space: string
  nodes: DeployNodeView[]
  status: string
  phase?: string
  desired_count?: number
  ttl_minutes?: number
  recycle_when_idle?: boolean
  created_at: string
  updated_at?: string
  recycled_at?: string
  error?: string
  registered_count: number
  orphans: number
}

export interface DeployRequest {
  cloud_id: string
  region?: string
  zone_id?: string
  image_id: string
  instance_type: string
  security_group_id?: string
  vswitch_id?: string
  vpc_id?: string
  count: number
  space?: string
  bandwidth_out?: number
  overrides?: Record<string, string>
  ttl_minutes?: number
  recycle_when_idle?: boolean
  dry_run?: boolean
}

export interface DeployResult {
  record?: DeployRecordView
  script?: string
  dry_run: boolean
}

export interface PublicURLInfo {
  public_url: string
  providers: string[]
}

export interface TunnelStatus {
  backend: string
  available: boolean
  enabled: boolean
  running: boolean
  connected: boolean
  phase?: string // provisioning | connecting | connected | error
  relay_ip?: string
  provider?: string
  region?: string
  public_url?: string
  error?: string
  started_at?: string
}

export interface StartTunnelRequest {
  cloud_id?: string
  region?: string
  instance_type?: string
  image_id?: string
  bandwidth?: number
}

// --- Admin token (gates cloud/deploy endpoints) ---

const ADMIN_TOKEN_KEY = 'aiscan_admin_token'

export function getAdminToken(): string {
  try {
    return window.localStorage.getItem(ADMIN_TOKEN_KEY) || ''
  } catch {
    return ''
  }
}

export function setAdminToken(token: string): void {
  try {
    if (token) window.localStorage.setItem(ADMIN_TOKEN_KEY, token)
    else window.localStorage.removeItem(ADMIN_TOKEN_KEY)
  } catch {
    /* ignore */
  }
}

function adminHeaders(extra?: Record<string, string>): Record<string, string> {
  const headers: Record<string, string> = { ...(extra || {}) }
  const token = getAdminToken()
  if (token) headers['X-Admin-Token'] = token
  return headers
}

// --- Cloud auto-deploy API ---

export async function getPublicURL(): Promise<PublicURLInfo> {
  return apiJSON('/api/cloud/public-url', 'Failed to load public URL', { headers: adminHeaders() })
}

export async function setPublicURL(publicURL: string): Promise<void> {
  await apiJSON('/api/cloud/public-url', 'Failed to save public URL', {
    method: 'PUT',
    headers: adminHeaders({ 'Content-Type': 'application/json' }),
    body: JSON.stringify({ public_url: publicURL }),
  })
}

export async function getTunnelStatus(): Promise<TunnelStatus> {
  return apiJSON('/api/cloud/tunnel', 'Failed to load tunnel status', { headers: adminHeaders() })
}

export async function startTunnel(req: StartTunnelRequest = {}): Promise<TunnelStatus> {
  return apiJSON('/api/cloud/tunnel', 'Failed to start tunnel', {
    method: 'POST',
    headers: adminHeaders({ 'Content-Type': 'application/json' }),
    body: JSON.stringify(req),
  })
}

export async function stopTunnel(): Promise<TunnelStatus> {
  return apiJSON('/api/cloud/tunnel', 'Failed to stop tunnel', {
    method: 'DELETE',
    headers: adminHeaders(),
  })
}

// destroyRelay terminates the auto-provisioned relay VM and clears the tunnel.
export async function destroyRelay(): Promise<TunnelStatus> {
  return apiJSON('/api/cloud/tunnel/relay', 'Failed to destroy relay', {
    method: 'DELETE',
    headers: adminHeaders(),
  })
}

export async function listCloudCredentials(): Promise<CloudCredentialView[]> {
  return apiJSON('/api/cloud/credentials', 'Failed to list credentials', { headers: adminHeaders() })
}

export async function saveCloudCredential(input: SaveCredentialInput): Promise<CloudCredentialView> {
  return apiJSON('/api/cloud/credentials', 'Failed to save credential', {
    method: 'PUT',
    headers: adminHeaders({ 'Content-Type': 'application/json' }),
    body: JSON.stringify(input),
  })
}

export async function deleteCloudCredential(id: string): Promise<void> {
  await apiJSON(`/api/cloud/credentials/${encodeURIComponent(id)}`, 'Failed to delete credential', {
    method: 'DELETE',
    headers: adminHeaders(),
  })
}

export async function listCloudRegions(input: ListRegionsInput): Promise<CloudRegion[]> {
  return apiJSON('/api/cloud/regions', 'Failed to list regions', {
    method: 'POST',
    headers: adminHeaders({ 'Content-Type': 'application/json' }),
    body: JSON.stringify(input),
  })
}

export async function listCloudImages(input: ListImagesInput): Promise<CloudImage[]> {
  return apiJSON('/api/cloud/images', 'Failed to list images', {
    method: 'POST',
    headers: adminHeaders({ 'Content-Type': 'application/json' }),
    body: JSON.stringify(input),
  })
}

export async function listCloudInstanceTypes(input: ListInstanceTypesInput): Promise<CloudInstanceType[]> {
  return apiJSON('/api/cloud/instance-types', 'Failed to list instance types', {
    method: 'POST',
    headers: adminHeaders({ 'Content-Type': 'application/json' }),
    body: JSON.stringify(input),
  })
}

export async function createDeploy(req: DeployRequest): Promise<DeployResult> {
  const res = await fetch('/api/deploy', {
    method: 'POST',
    headers: adminHeaders({ 'Content-Type': 'application/json' }),
    body: JSON.stringify(req),
  })
  const body = await res.json().catch(() => null)
  // A partial launch returns 207 (StatusMultiStatus) with {result, error}:
  // instances may already be billing while network setup / node persistence
  // failed. 207 is a 2xx, so res.ok is true and apiJSON would report success —
  // detect the wrapper's `error` field and throw so the operator is told.
  const partialError = body && typeof body === 'object' && 'error' in body ? (body as any).error : ''
  if (!res.ok || partialError) {
    throw new Error(partialError || (body && (body as any).error) || 'Failed to create deployment')
  }
  return body as DeployResult
}

export async function listDeploys(): Promise<DeployRecordView[]> {
  return apiJSON('/api/deploy', 'Failed to list deployments', { headers: adminHeaders() })
}

export async function recycleDeploy(id: string, instanceIDs?: string[]): Promise<DeployRecordView> {
  if (instanceIDs && instanceIDs.length > 0) {
    return apiJSON(`/api/deploy/${encodeURIComponent(id)}/recycle`, 'Failed to recycle', {
      method: 'POST',
      headers: adminHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify({ instance_ids: instanceIDs }),
    })
  }
  return apiJSON(`/api/deploy/${encodeURIComponent(id)}`, 'Failed to recycle', {
    method: 'DELETE',
    headers: adminHeaders(),
  })
}

export async function recycleAllDeploys(cloudID?: string, space?: string): Promise<{ recycled: number }> {
  return apiJSON('/api/deploy/recycle-all', 'Failed to recycle all', {
    method: 'POST',
    headers: adminHeaders({ 'Content-Type': 'application/json' }),
    body: JSON.stringify({ cloud_id: cloudID || '', space: space || '' }),
  })
}

// --- Local agents (hub-hosted nodes: one-click launch + delete) ---

export interface LocalAgentView {
  name: string
  pid: number
  registered: boolean
  busy?: boolean
}

export async function launchLocalAgent(): Promise<LocalAgentView> {
  return apiJSON('/api/deploy/local', 'Failed to launch local agent', {
    method: 'POST',
    headers: adminHeaders(),
  })
}

export async function listLocalAgents(): Promise<LocalAgentView[]> {
  return apiJSON('/api/deploy/local', 'Failed to list local agents', { headers: adminHeaders() })
}

export async function stopLocalAgent(name: string): Promise<void> {
  await apiJSON(`/api/deploy/local/${encodeURIComponent(name)}`, 'Failed to delete local agent', {
    method: 'DELETE',
    headers: adminHeaders(),
  })
}

export async function submitScan(target: string, mode: string, options: ScanOptions, project?: string): Promise<ScanJob> {
  return apiJSON('/api/scans', 'Failed to submit scan', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ target, mode, ...options, project }),
  });
}

export async function getAssets(project?: string): Promise<PoolAsset[]> {
  const q = project ? `?project=${encodeURIComponent(project)}` : '';
  return apiJSON(`/api/assets${q}`, 'Failed to load assets');
}

export async function addAssets(targets: string[], source?: string, label?: string, project?: string): Promise<PoolAsset[]> {
  return apiJSON('/api/assets', 'Failed to add assets', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ targets, source, label, project }),
  });
}

export async function deleteAsset(id: string, project?: string): Promise<void> {
  const q = project ? `?project=${encodeURIComponent(project)}` : '';
  await apiJSON(`/api/assets/${encodeURIComponent(id)}${q}`, 'Failed to delete asset', { method: 'DELETE' });
}

// --- Projects (asset-pool scope) ---

export interface Project {
  id: string
  name: string
  assets: number
  created_at: string
}

export async function getProjects(): Promise<Project[]> {
  return apiJSON('/api/projects', 'Failed to load projects');
}

export async function createProject(name: string, id?: string): Promise<Project> {
  return apiJSON('/api/projects', 'Failed to create project', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, id }),
  });
}

// Cascades on the server: the project and its entire asset pool are removed.
export async function deleteProject(id: string): Promise<void> {
  await apiJSON(`/api/projects/${encodeURIComponent(id)}`, 'Failed to delete project', { method: 'DELETE' });
}

export async function getScan(id: string): Promise<ScanJob> {
  return apiJSON(`/api/scans/${encodeURIComponent(id)}`, 'Scan not found');
}

export async function listScans(): Promise<ScanJob[]> {
  return apiJSON('/api/scans', 'Failed to list scans');
}

export async function deleteScan(id: string): Promise<void> {
  await apiJSON(`/api/scans/${encodeURIComponent(id)}`, 'Failed to delete scan', { method: 'DELETE' });
}

export function subscribeScanEvents(
  id: string,
  onEvent: (event: ScanEvent) => void,
): () => void {
  const es = new EventSource(`/api/scans/${encodeURIComponent(id)}/events`);
  const handler = (type: RawScanEventType) => (e: Event) => {
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
      const parsed = JSON.parse(data);
      const normalizedType = type === 'output' ? 'progress' : type;
      const parsedType = parsed?.type === 'output' ? 'progress' : parsed?.type || normalizedType;
      event = {
        scan_id: id,
        ...parsed,
        type: parsedType,
      };
    } catch {
      event = { type: type === 'output' ? 'progress' : type, scan_id: id, data };
    }

    onEvent(event);
    if (event.type === 'complete' || event.type === 'error') {
      es.close();
    }
  };
  es.addEventListener('progress', handler('progress'));
  es.addEventListener('status', handler('status'));
  es.addEventListener('stats', handler('stats'));
  es.addEventListener('complete', handler('complete'));
  es.addEventListener('error', handler('error'));
  es.addEventListener('output', handler('output'));

  return () => es.close();
}

// --- Chat session types ---

export interface ChatSession {
  id: string
  agent_id: string
  agent_name?: string
  title: string
  status: 'active' | 'archived'
  scan_ids?: string[]
  created_at: string
  updated_at: string
}

export interface ChatMessage {
  id: string
  session_id: string
  role: 'user' | 'assistant' | 'system' | 'tool_call' | 'tool_result'
  agent_id?: string
  agent_name?: string
  content: string
  metadata?: Record<string, unknown>
  created_at: string
}

export type ChatEventType =
  | 'message' | 'message_start' | 'message_delta' | 'message_end'
  | 'tool_call' | 'tool_result' | 'thinking'
  | 'scan_started' | 'scan_progress' | 'scan_complete' | 'scan_error'
  | 'agent_joined' | 'eval' | 'error'

export interface ChatEvent {
  type: ChatEventType
  session_id: string
  message_id?: string
  role?: ChatMessage['role']
  agent_id?: string
  agent_name?: string
  turn?: number
  content?: string
  delta?: string
  tool_name?: string
  tool_args?: string
  tool_call_id?: string
  scan_id?: string
  result?: ScanResult
  data?: string
  error?: string
  eval_round?: number
  eval_pass?: boolean
  eval_reason?: string
}

// --- Chat session API ---

export async function createChatSession(agentID: string, title?: string): Promise<ChatSession> {
  return apiJSON('/api/chat/sessions', 'Failed to create session', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ agent_id: agentID, title: title || '' }),
  })
}

export async function listChatSessions(): Promise<ChatSession[]> {
  return apiJSON('/api/chat/sessions', 'Failed to list sessions')
}

export async function getChatSession(id: string): Promise<ChatSession> {
  return apiJSON(`/api/chat/sessions/${encodeURIComponent(id)}`, 'Session not found')
}

export async function deleteChatSession(id: string): Promise<void> {
  await apiJSON(`/api/chat/sessions/${encodeURIComponent(id)}`, 'Failed to delete session', {
    method: 'DELETE',
  })
}

export async function sendChatMessage(
  sessionID: string,
  content: string,
  opts?: { persist?: boolean; maxTurns?: number; evalCriteria?: string; evalMaxRounds?: number },
): Promise<ChatMessage> {
  const body: Record<string, unknown> = { content }
  if (opts?.persist) {
    body.persist = true
    const criteria = opts.evalCriteria?.trim()
    if (criteria) {
      body.eval_criteria = criteria
      if (opts.evalMaxRounds && opts.evalMaxRounds > 0) body.eval_max_rounds = opts.evalMaxRounds
    } else if (opts.maxTurns && opts.maxTurns > 0) {
      body.persist_max_turns = opts.maxTurns
    }
  }
  return apiJSON(`/api/chat/sessions/${encodeURIComponent(sessionID)}/messages`, 'Failed to send message', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

export async function cancelChatSession(sessionID: string): Promise<void> {
  await apiJSON(`/api/chat/sessions/${encodeURIComponent(sessionID)}/cancel`, 'Failed to pause response', {
    method: 'POST',
  })
}

export interface FileUploadResult {
  filename: string
  path: string
  size: number
  error?: string
}

export async function uploadChatFile(sessionID: string, file: File): Promise<FileUploadResult> {
  const form = new FormData()
  form.append('file', file)
  const resp = await fetch(`/api/chat/sessions/${encodeURIComponent(sessionID)}/upload`, {
    method: 'POST',
    body: form,
  })
  if (!resp.ok) {
    const body = await resp.text()
    throw new Error(body || `Upload failed: ${resp.status}`)
  }
  return resp.json()
}

export async function listChatMessages(sessionID: string): Promise<ChatMessage[]> {
  return apiJSON(`/api/chat/sessions/${encodeURIComponent(sessionID)}/messages`, 'Failed to list messages')
}

// Fetch a scan's markdown report, re-rendered server-side in the given language
// ('en' | 'zh'). Returns '' when the report isn't ready yet (404) so callers can
// just show a placeholder.
export async function fetchScanReport(scanID: string, lang: string): Promise<string> {
  const res = await fetch(`/api/scans/${encodeURIComponent(scanID)}/report?lang=${encodeURIComponent(lang)}`)
  if (!res.ok) return ''
  return res.text()
}

export function subscribeChatEvents(
  sessionID: string,
  onEvent: (event: ChatEvent) => void,
  onReconnect?: () => void,
): () => void {
  const url = `/api/chat/sessions/${encodeURIComponent(sessionID)}/events`
  const es = new EventSource(url)

  const eventTypes: ChatEventType[] = [
    'message', 'message_start', 'message_delta', 'message_end',
    'tool_call', 'tool_result', 'thinking',
    'scan_started', 'scan_progress', 'scan_complete', 'scan_error',
    'agent_joined', 'eval', 'error',
  ]

  for (const type of eventTypes) {
    es.addEventListener(type, (e: Event) => {
      const data = 'data' in e ? (e as MessageEvent).data : undefined
      if (typeof data !== 'string' || data === '') return
      try {
        const parsed = JSON.parse(data)
        onEvent({ ...parsed, type })
      } catch {
        onEvent({ type, session_id: sessionID, data } as ChatEvent)
      }
    })
  }

  es.addEventListener('error', () => {
    // EventSource auto-reconnects, but the chat SSE topic keeps no backlog, so a
    // terminal event (message_end / tool_result / the aggregate 'message')
    // broadcast during the drop is lost — which strands the composer in a
    // permanent "thinking" state. Reconcile from REST truth on each connection
    // error (idempotent), mirroring the scan path's getScan-on-error recovery.
    onReconnect?.()
  })

  return () => es.close()
}

export function agentTerminalWebSocketURL(agentID: string): string {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${protocol}//${window.location.host}/api/agents/${encodeURIComponent(agentID)}/terminal/ws`;
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
