package web

import (
	"encoding/json"
	"time"

	"github.com/chainreactors/aiscan/core/output"
	"github.com/chainreactors/aiscan/pkg/webproto"
)

type ScanStatus string

const (
	StatusQueued    ScanStatus = "queued"
	StatusRunning   ScanStatus = "running"
	StatusCompleted ScanStatus = "completed"
	StatusFailed    ScanStatus = "failed"
	StatusCanceled  ScanStatus = "canceled"
)

type ScanJob struct {
	ID       string         `json:"id"`
	Target   string         `json:"target"`
	Mode     string         `json:"mode"`
	Verify   bool           `json:"verify,omitempty"`
	Sniper   bool           `json:"sniper,omitempty"`
	AI       bool           `json:"ai,omitempty"`
	Deep     bool           `json:"deep,omitempty"`
	Status   ScanStatus     `json:"status"`
	Progress string         `json:"progress,omitempty"`
	Report   string         `json:"report,omitempty"`
	Result   *output.Result `json:"result,omitempty"`
	Error    string         `json:"error,omitempty"`
	// Project scopes the assets this scan ingests into the pool. Scans are still
	// listed globally; this is persisted only so the background runner can re-load
	// the job and ingest discovered assets into the selected project.
	Project string `json:"project,omitempty"`
	// Skipped lists batch targets that failed validation and were dropped from
	// the run. Populated on the create response; transient (not persisted).
	Skipped   []RejectedTarget `json:"skipped,omitempty"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}

type ScanRequest struct {
	Target string `json:"target"`
	Mode   string `json:"mode"`
	Verify bool   `json:"verify,omitempty"`
	Sniper bool   `json:"sniper,omitempty"`
	AI     bool   `json:"ai,omitempty"`
	Deep   bool   `json:"deep,omitempty"`
	// Project scopes the asset pool the scan's discovered assets fold into.
	Project string `json:"project,omitempty"`
}

func (r ScanRequest) AnalysisOptions() (verify, sniper, deep bool) {
	verify, sniper, deep = r.Verify, r.Sniper, r.Deep
	if r.AI && !verify && !sniper {
		verify = true
		sniper = true
	}
	return verify, sniper, deep
}

// PoolAsset is one entry in the shared asset inventory — a target (host / IP /
// host:port / URL) discovered by a scan, surfaced by an AI agent's recon, or
// entered by a human. It is deduplicated by Target and can be re-tested with a
// single click from the scan deck.
type PoolAsset struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"project_id,omitempty"`
	Target     string    `json:"target"`
	Label      string    `json:"label,omitempty"`
	Source     string    `json:"source,omitempty"` // scan | agent | manual
	Status     string    `json:"status,omitempty"`
	Note       string    `json:"note,omitempty"`
	Services   int       `json:"services,omitempty"`
	Loots      int       `json:"loots,omitempty"`
	LastScanID string    `json:"last_scan_id,omitempty"`
	FirstSeen  time.Time `json:"first_seen"`
	LastSeen   time.Time `json:"last_seen"`
}

const (
	AssetSourceScan   = "scan"
	AssetSourceAgent  = "agent"
	AssetSourceManual = "manual"
)

// AssetRequest is the POST /api/assets body. Either Target or Targets may be
// set; both are folded into a single upsert batch.
type AssetRequest struct {
	Target  string   `json:"target,omitempty"`
	Targets []string `json:"targets,omitempty"`
	Source  string   `json:"source,omitempty"`
	Label   string   `json:"label,omitempty"`
	Project string   `json:"project,omitempty"`
}

// DefaultProjectID is the implicit project every asset falls into when no
// project is selected — it holds the pre-project global inventory after
// migration, so existing behavior is preserved for anyone who never switches.
const DefaultProjectID = "default"

// Project scopes the asset pool: a named workspace / engagement. Assets are
// filtered by the active project so separate engagements don't share one global
// inventory. The IOA space name is derived from the project id when deploying
// agents, so operators pick a single thing. (Scan / session scoping may follow.)
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Assets    int       `json:"assets"`
	CreatedAt time.Time `json:"created_at"`
}

// ProjectRequest is the POST /api/projects body. ID is optional; when blank it
// is slugified from Name (falling back to a generated id).
type ProjectRequest struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
}

type ServiceStatus struct {
	LLMAvailable        bool   `json:"llm_available"`
	LLMProvider         string `json:"llm_provider,omitempty"`
	LLMModel            string `json:"llm_model,omitempty"`
	LLMAPIKeyConfigured bool   `json:"llm_api_key_configured,omitempty"`
	ConfigPath          string `json:"config_path,omitempty"`
	ConfigLoaded        bool   `json:"config_loaded"`
	Agents              int    `json:"agents"`
}

// ConfigStatus is the response for GET /api/config — secrets masked,
// *_configured booleans indicate whether a secret is set.
type ConfigStatus struct {
	ConfigPath   string `json:"config_path,omitempty"`
	ConfigLoaded bool   `json:"config_loaded"`
	LLM          struct {
		Provider         string `json:"provider"`
		BaseURL          string `json:"base_url"`
		APIKeyConfigured bool   `json:"api_key_configured"`
		Model            string `json:"model"`
		Proxy            string `json:"proxy"`
	} `json:"llm"`
	Cyberhub struct {
		URL           string `json:"url"`
		KeyConfigured bool   `json:"key_configured"`
		Mode          string `json:"mode"`
		Proxy         string `json:"proxy"`
	} `json:"cyberhub"`
	Recon struct {
		FofaEmail              string `json:"fofa_email"`
		FofaKeyConfigured      bool   `json:"fofa_key_configured"`
		HunterTokenConfigured  bool   `json:"hunter_token_configured"`
		HunterAPIKeyConfigured bool   `json:"hunter_api_key_configured"`
		Proxy                  string `json:"proxy"`
		Limit                  *int   `json:"limit,omitempty"`
	} `json:"recon"`
	Scan struct {
		Verify        string `json:"verify"`
		VerifyTimeout int    `json:"verify_timeout"`
	} `json:"scan"`
	Search struct {
		TavilyKeysConfigured bool `json:"tavily_keys_configured"`
	} `json:"search"`
	IOA struct {
		URL             string `json:"url"`
		TokenConfigured bool   `json:"token_configured"`
		NodeName        string `json:"node_name"`
		Space           string `json:"space"`
	} `json:"ioa"`
	Agent struct {
		Tools       []string `json:"tools,omitempty"`
		Timeout     int      `json:"timeout"`
		SaveSession bool     `json:"save_session"`
	} `json:"agent"`
}

// ConfigStatusFromDistribute builds a masked ConfigStatus from raw config.
func ConfigStatusFromDistribute(d *webproto.DistributeConfig, path string, loaded bool) ConfigStatus {
	var cs ConfigStatus
	cs.ConfigPath = path
	cs.ConfigLoaded = loaded
	cs.LLM.Provider = d.LLM.Provider
	cs.LLM.BaseURL = d.LLM.BaseURL
	cs.LLM.APIKeyConfigured = d.LLM.APIKey != ""
	cs.LLM.Model = d.LLM.Model
	cs.LLM.Proxy = d.LLM.Proxy
	cs.Cyberhub.URL = d.Cyberhub.URL
	cs.Cyberhub.KeyConfigured = d.Cyberhub.Key != ""
	cs.Cyberhub.Mode = d.Cyberhub.Mode
	cs.Cyberhub.Proxy = d.Cyberhub.Proxy
	cs.Recon.FofaEmail = d.Recon.FofaEmail
	cs.Recon.FofaKeyConfigured = d.Recon.FofaKey != ""
	cs.Recon.HunterTokenConfigured = d.Recon.HunterToken != ""
	cs.Recon.HunterAPIKeyConfigured = d.Recon.HunterAPIKey != ""
	cs.Recon.Proxy = d.Recon.Proxy
	cs.Recon.Limit = d.Recon.Limit
	cs.Scan.Verify = d.Scan.Verify
	cs.Scan.VerifyTimeout = d.Scan.VerifyTimeout
	cs.Search.TavilyKeysConfigured = d.Search.TavilyKeys != ""
	cs.IOA.URL = d.IOA.URL
	cs.IOA.TokenConfigured = d.IOA.Token != ""
	cs.IOA.NodeName = d.IOA.NodeName
	cs.IOA.Space = d.IOA.Space
	cs.Agent.Tools = d.Agent.Tools
	cs.Agent.Timeout = d.Agent.Timeout
	cs.Agent.SaveSession = d.Agent.SaveSession
	return cs
}

// --- Chat types ---

const (
	SessionActive   = "active"
	SessionArchived = "archived"
)

type ChatSession struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	AgentName string    `json:"agent_name,omitempty"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	ScanIDs   []string  `json:"scan_ids,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ChatMessage struct {
	ID        string          `json:"id"`
	SessionID string          `json:"session_id"`
	Role      string          `json:"role"`
	AgentID   string          `json:"agent_id,omitempty"`
	AgentName string          `json:"agent_name,omitempty"`
	Content   string          `json:"content"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

const (
	ChatEventMessage      = "message"
	ChatEventMessageStart = "message_start"
	ChatEventMessageDelta = "message_delta"
	ChatEventMessageEnd   = "message_end"
	ChatEventToolCall     = "tool_call"
	ChatEventToolResult   = "tool_result"
	ChatEventThinking     = "thinking"
	ChatEventScanStarted  = "scan_started"
	ChatEventScanProgress = "scan_progress"
	ChatEventScanComplete = "scan_complete"
	ChatEventScanError    = "scan_error"
	ChatEventAgentJoined  = "agent_joined"
	ChatEventEval         = "eval"
	ChatEventError        = "error"
)

type ChatEvent struct {
	Type       string         `json:"type"`
	SessionID  string         `json:"session_id"`
	MessageID  string         `json:"message_id,omitempty"`
	Role       string         `json:"role,omitempty"`
	AgentID    string         `json:"agent_id,omitempty"`
	AgentName  string         `json:"agent_name,omitempty"`
	Turn       int            `json:"turn,omitempty"`
	Content    string         `json:"content,omitempty"`
	Delta      string         `json:"delta,omitempty"`
	ToolName   string         `json:"tool_name,omitempty"`
	ToolArgs   string         `json:"tool_args,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ScanID     string         `json:"scan_id,omitempty"`
	Result     *output.Result `json:"result,omitempty"`
	Data       string         `json:"data,omitempty"`
	Error      string         `json:"error,omitempty"`
	// Eval fields carry goal-evaluation verdicts (persist mode with criteria).
	EvalRound  int    `json:"eval_round,omitempty"`
	EvalPass   bool   `json:"eval_pass,omitempty"`
	EvalReason string `json:"eval_reason,omitempty"`
	Transient  bool   `json:"-"`
}

type SendMessageRequest struct {
	Content string `json:"content"`
	// Persist enables "keep going until the goal is reached" mode for this
	// message: the agent is nudged to continue until it calls finish or hits the
	// turn cap below.
	Persist bool `json:"persist,omitempty"`
	// PersistMaxTurns bounds persist mode (0 = use the server default).
	PersistMaxTurns int `json:"persist_max_turns,omitempty"`
	// EvalCriteria, when set, upgrades persist mode to evaluator-judged completion:
	// an independent LLM decides whether this natural-language goal is achieved,
	// re-running with feedback until it passes or EvalMaxRounds is reached.
	EvalCriteria string `json:"eval_criteria,omitempty"`
	// EvalMaxRounds bounds evaluator rounds (0 = engine default).
	EvalMaxRounds int `json:"eval_max_rounds,omitempty"`
}

// ChatOptions carries per-message agent behavior flags through the dispatch path.
type ChatOptions struct {
	Persist       bool
	MaxTurns      int
	EvalCriteria  string
	EvalMaxRounds int
}

type CreateSessionRequest struct {
	AgentID string `json:"agent_id"`
	Title   string `json:"title,omitempty"`
}
