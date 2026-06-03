package web

import (
	"time"

	scanpkg "github.com/chainreactors/aiscan/pkg/tools/scan"
)

type ScanStatus string

const (
	StatusQueued    ScanStatus = "queued"
	StatusRunning   ScanStatus = "running"
	StatusCompleted ScanStatus = "completed"
	StatusFailed    ScanStatus = "failed"
	StatusCancelled ScanStatus = "cancelled"
)

type ScanJob struct {
	ID        string                    `json:"id"`
	Target    string                    `json:"target"`
	Mode      string                    `json:"mode"`
	Verify    bool                      `json:"verify,omitempty"`
	Sniper    bool                      `json:"sniper,omitempty"`
	AI        bool                      `json:"ai,omitempty"`
	Deep      bool                      `json:"deep,omitempty"`
	Status    ScanStatus                `json:"status"`
	Progress  string                    `json:"progress,omitempty"`
	Report    string                    `json:"report,omitempty"`
	Result    *scanpkg.StructuredResult `json:"result,omitempty"`
	Error     string                    `json:"error,omitempty"`
	CreatedAt time.Time                 `json:"created_at"`
	UpdatedAt time.Time                 `json:"updated_at"`
}

type ScanRequest struct {
	Target string `json:"target"`
	Mode   string `json:"mode"`
	Verify bool   `json:"verify,omitempty"`
	Sniper bool   `json:"sniper,omitempty"`
	AI     bool   `json:"ai,omitempty"`
	Deep   bool   `json:"deep,omitempty"`
}

func (r ScanRequest) AnalysisOptions() (verify, sniper, deep bool) {
	verify, sniper, deep = r.Verify, r.Sniper, r.Deep
	if r.AI && !verify && !sniper {
		verify = true
		sniper = true
	}
	return verify, sniper, deep
}

type ServiceStatus struct {
	LLMAvailable        bool   `json:"llm_available"`
	LLMProvider         string `json:"llm_provider,omitempty"`
	LLMModel            string `json:"llm_model,omitempty"`
	LLMAPIKeyConfigured bool   `json:"llm_api_key_configured,omitempty"`
	ConfigPath          string `json:"config_path,omitempty"`
	ConfigLoaded        bool   `json:"config_loaded"`
}

type LLMConfig struct {
	ConfigPath       string `json:"config_path,omitempty"`
	ConfigLoaded     bool   `json:"config_loaded"`
	Provider         string `json:"provider"`
	BaseURL          string `json:"base_url"`
	APIKey           string `json:"api_key,omitempty"`
	APIKeyConfigured bool   `json:"api_key_configured"`
	Model            string `json:"model"`
	Proxy            string `json:"proxy"`
}

type ScanEvent struct {
	Type   string                    `json:"type"`
	ScanID string                    `json:"scan_id"`
	Data   string                    `json:"data,omitempty"`
	Status string                    `json:"status,omitempty"`
	Error  string                    `json:"error,omitempty"`
	Result *scanpkg.StructuredResult `json:"result,omitempty"`
}
