package output

import (
	"time"

	"github.com/chainreactors/aiscan/pkg/agent/provider"
	sdktypes "github.com/chainreactors/sdk/pkg/types"
)

type Result struct {
	Summary   Summary                  `json:"summary"`
	Assets    []Asset                  `json:"assets,omitempty"`
	Services  []*sdktypes.GOGOResult   `json:"services,omitempty"`
	WebProbes []*sdktypes.SprayResult  `json:"web_probes,omitempty"`
	Risks     []*sdktypes.ZombieResult `json:"risks,omitempty"`
	Vulns     []*sdktypes.VulnResult   `json:"vulns,omitempty"`
	AI        []AIFinding              `json:"ai,omitempty"`
	Errors    []Error                  `json:"errors,omitempty"`
}

type Summary struct {
	Targets  int       `json:"targets"`
	Services int       `json:"services"`
	Webs     int       `json:"webs"`
	Probes   int       `json:"probes"`
	Risks    int       `json:"risks"`
	Vulns    int       `json:"vulns"`
	Verified int       `json:"verified"`
	Errors   int       `json:"errors"`
	Tasks    int64     `json:"tasks"`
	Requests int64     `json:"requests"`
	Duration string    `json:"duration"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
}

type Asset struct {
	ID     string      `json:"id"`
	Key    string      `json:"key"`
	Target string      `json:"target"`
	Title  string      `json:"title,omitempty"`
	Status string      `json:"status,omitempty"`
	Items  []AssetItem `json:"items,omitempty"`
}

const (
	AssetItemService     = "service"
	AssetItemPath        = "path"
	AssetItemFingerprint = "fingerprint"
	AssetItemFinding     = "finding"
	AssetItemNote        = "note"
	AssetItemResponse    = "response"
	AssetItemError       = "error"
)

type AssetItem struct {
	Kind    string         `json:"kind"`
	Source  string         `json:"source,omitempty"`
	Target  string         `json:"target,omitempty"`
	Status  string         `json:"status,omitempty"`
	Title   string         `json:"title,omitempty"`
	Summary string         `json:"summary,omitempty"`
	Detail  string         `json:"detail,omitempty"`
	Tags    []string       `json:"tags,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
	Raw     string         `json:"raw,omitempty"`
}

type AIFinding struct {
	Kind         string `json:"kind"`
	Target       string `json:"target,omitempty"`
	Priority     string `json:"priority,omitempty"`
	Status       string `json:"status,omitempty"`
	Summary      string `json:"summary,omitempty"`
	Detail       string `json:"detail,omitempty"`
	Evidence     string `json:"evidence,omitempty"`
	Skill        string `json:"skill,omitempty"`
	Source       string `json:"source,omitempty"`
	OriginalKind string `json:"original_kind,omitempty"`
	OriginalKey  string `json:"original_key,omitempty"`
	Raw          string `json:"raw,omitempty"`
}

type Error struct {
	Source  string `json:"source,omitempty"`
	Message string `json:"message"`
}

// --- Record payload types (aiscan-specific) ---

type ScanStart struct {
	Targets []string `json:"targets"`
	Mode    string   `json:"mode"`
	Flags   []string `json:"flags"`
}

type ScanEnd struct {
	Duration float64 `json:"duration_s"`
	Targets  int     `json:"targets"`
	Services int     `json:"services"`
	Webs     int     `json:"webs"`
	Findings int     `json:"findings"`
	AISkills int     `json:"ai_skills"`
	Errors   int     `json:"errors"`
}

type AISkill struct {
	Skill    string  `json:"skill"`
	Target   string  `json:"target"`
	Status   string  `json:"status"`
	Summary  string  `json:"summary"`
	Detail   string  `json:"detail,omitempty"`
	Duration float64 `json:"duration_s"`
}

type AITurn struct {
	Skill    string                 `json:"skill"`
	Turn     int                    `json:"turn"`
	Prompt   string                 `json:"prompt,omitempty"`
	Messages []provider.ChatMessage `json:"messages,omitempty"`
	Usage    *provider.Usage        `json:"usage,omitempty"`
	Duration float64                `json:"duration_s"`
}
