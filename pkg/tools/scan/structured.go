package scan

import (
	"strings"
	"time"

	"github.com/chainreactors/parsers"
)

type StructuredResult struct {
	Summary      StructuredSummary       `json:"summary"`
	Assets       []Asset                 `json:"assets,omitempty"`
	Services     []StructuredService     `json:"services,omitempty"`
	WebEndpoints []StructuredWebEndpoint `json:"web_endpoints,omitempty"`
	WebProbes    []StructuredWebEndpoint `json:"web_probes,omitempty"`
	Fingerprints []StructuredFingerprint `json:"fingerprints,omitempty"`
	Risks        []StructuredFinding     `json:"risks,omitempty"`
	Vulns        []StructuredFinding     `json:"vulns,omitempty"`
	AI           []StructuredFinding     `json:"ai,omitempty"`
	Errors       []StructuredError       `json:"errors,omitempty"`
}

type StructuredSummary struct {
	Targets      int       `json:"targets"`
	Services     int       `json:"services"`
	Webs         int       `json:"webs"`
	Probes       int       `json:"probes"`
	Fingerprints int       `json:"fingerprints"`
	Risks        int       `json:"risks"`
	Vulns        int       `json:"vulns"`
	Verified     int       `json:"verified"`
	Errors       int       `json:"errors"`
	Tasks        int64     `json:"tasks"`
	Requests     int64     `json:"requests"`
	Duration     string    `json:"duration"`
	StartedAt    time.Time `json:"started_at,omitempty"`
	FinishedAt   time.Time `json:"finished_at,omitempty"`
}

type Asset struct {
	ID     string      `json:"id"`
	Key    string      `json:"key"`
	Target string      `json:"target"`
	Title  string      `json:"title,omitempty"`
	Status string      `json:"status,omitempty"`
	Items  []AssetItem `json:"items,omitempty"`
}

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

type StructuredService struct {
	Target   string `json:"target"`
	IP       string `json:"ip,omitempty"`
	Port     string `json:"port,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Service  string `json:"service,omitempty"`
	Banner   string `json:"banner,omitempty"`
	Raw      string `json:"raw,omitempty"`
	IsWeb    bool   `json:"is_web,omitempty"`
}

type StructuredWebEndpoint struct {
	URL        string   `json:"url"`
	HostHeader string   `json:"host_header,omitempty"`
	Source     string   `json:"source,omitempty"`
	Status     int      `json:"status,omitempty"`
	Length     int      `json:"length,omitempty"`
	Title      string   `json:"title,omitempty"`
	Fingers    []string `json:"fingers,omitempty"`
	Raw        string   `json:"raw,omitempty"`
}

type StructuredFingerprint struct {
	Target string `json:"target"`
	Name   string `json:"name"`
	Source string `json:"source,omitempty"`
	Focus  bool   `json:"focus,omitempty"`
}

type StructuredFinding struct {
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

type StructuredError struct {
	Source  string `json:"source,omitempty"`
	Message string `json:"message"`
}

func (c *collector) StructuredResult() *StructuredResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	stats := c.statsSnapshotLocked()
	result := &StructuredResult{
		Summary: StructuredSummary{
			Targets:      stats.Inputs,
			Services:     len(c.gogoResults),
			Webs:         len(c.webEndpoints),
			Probes:       len(c.sprayResults),
			Fingerprints: len(c.fingerprints),
			Risks:        len(c.zombieResults),
			Vulns:        len(c.neutronMatches),
			Verified:     c.confirmedVerificationCountLocked(),
			Errors:       len(c.errors),
			Tasks:        stats.Tasks,
			Requests:     stats.Requests,
			Duration:     stats.Duration().Round(time.Millisecond).String(),
			StartedAt:    stats.StartedAt,
			FinishedAt:   stats.FinishedAt,
		},
	}

	for _, item := range c.gogoResults {
		if item == nil {
			continue
		}
		result.Services = append(result.Services, StructuredService{
			Target:   item.GetTarget(),
			IP:       item.Ip,
			Port:     item.Port,
			Protocol: item.Protocol,
			Service:  firstNonEmptyString(item.Protocol, item.Midware),
			Banner:   item.Midware,
			Raw:      item.OutputLine(),
			IsWeb:    item.IsHttp(),
		})
	}
	for _, item := range c.webEndpoints {
		result.WebEndpoints = append(result.WebEndpoints, StructuredWebEndpoint{
			URL:        item.URL,
			HostHeader: item.HostHeader,
			Source:     item.Source,
			Raw:        strings.TrimSpace(parsers.JoinOutput(item.URL, item.HostHeader)),
		})
	}
	for _, item := range c.sprayResults {
		if item.Result == nil {
			continue
		}
		result.WebProbes = append(result.WebProbes, StructuredWebEndpoint{
			URL:     item.Result.UrlString,
			Source:  item.Capability,
			Status:  item.Result.Status,
			Length:  item.Result.BodyLength,
			Title:   item.Result.Title,
			Fingers: parsers.FrameworkNames(item.Result.Frameworks),
			Raw:     item.Result.OutputLine(),
		})
	}
	for _, item := range c.fingerprints {
		result.Fingerprints = append(result.Fingerprints, StructuredFingerprint{
			Target: item.Target,
			Name:   item.Name,
			Source: item.Source,
			Focus:  item.Focus,
		})
	}
	for _, item := range c.zombieResults {
		if item == nil {
			continue
		}
		finding := weakpassFinding{Result: item}
		result.Risks = append(result.Risks, StructuredFinding{
			Kind:     string(finding.Kind()),
			Target:   item.Address(),
			Priority: string(finding.Priority()),
			Source:   capZombieWeakpass,
			Raw:      item.OutputLine(),
		})
	}
	for _, item := range c.neutronMatches {
		raw := item.String()
		if raw == "" {
			continue
		}
		result.Vulns = append(result.Vulns, StructuredFinding{
			Kind:     string(item.Kind()),
			Target:   item.Target,
			Priority: string(item.Priority()),
			Source:   capNeutronPOC,
			Summary:  raw,
			Raw:      raw,
		})
	}
	for _, item := range c.verifications {
		result.AI = append(result.AI, StructuredFinding{
			Kind:         string(item.Finding.Kind()),
			Target:       item.Finding.Target,
			Priority:     string(item.Finding.Priority()),
			Status:       string(item.Finding.Status),
			Summary:      item.Finding.Summary,
			Evidence:     item.Finding.Evidence,
			Source:       item.Source,
			OriginalKind: string(item.Finding.OriginalKind),
			OriginalKey:  item.Finding.OriginalKey,
			Raw:          verificationOutput(item.Finding),
		})
	}
	for _, item := range c.aiSkillResults {
		result.AI = append(result.AI, StructuredFinding{
			Kind:         string(item.Finding.Kind()),
			Target:       item.Finding.Target,
			Priority:     string(item.Finding.Priority()),
			Status:       item.Finding.Status,
			Summary:      item.Finding.Summary,
			Detail:       item.Finding.Detail,
			Skill:        item.Finding.Skill,
			Source:       item.Source,
			OriginalKind: string(item.Finding.OriginalKind),
			OriginalKey:  item.Finding.OriginalKey,
			Raw:          aiSkillOutput(item.Finding),
		})
	}
	for _, item := range c.aiSkillResponses {
		result.AI = append(result.AI, StructuredFinding{
			Kind:         string(item.Response.Kind()),
			Target:       item.Response.Target,
			Priority:     string(item.Response.Priority()),
			Status:       item.Response.Status,
			Summary:      item.Response.Summary,
			Detail:       item.Response.Detail,
			Skill:        item.Response.Skill,
			Source:       item.Source,
			OriginalKind: string(item.Response.OriginalKind),
			OriginalKey:  item.Response.OriginalKey,
			Raw:          aiSkillResponseOutput(item.Response),
		})
	}
	for _, message := range c.errors {
		result.Errors = append(result.Errors, StructuredError{Message: message})
	}

	result.Assets = AggregateStructuredResult(result)
	return result
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
