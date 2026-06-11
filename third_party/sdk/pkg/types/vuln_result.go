package types

import "github.com/chainreactors/neutron/templates"

// VulnResult is the JSON-serializable form of a neutron vulnerability finding.
type VulnResult struct {
	Target         string                 `json:"target"`
	TemplateID     string                 `json:"template_id"`
	TemplateName   string                 `json:"template_name,omitempty"`
	Severity       string                 `json:"severity,omitempty"`
	Description    string                 `json:"description,omitempty"`
	References     []string               `json:"references,omitempty"`
	Tags           string                 `json:"tags,omitempty"`
	Classification *templates.Classification `json:"classification,omitempty"`
	Matched        bool                   `json:"matched"`
	Matches        map[string][]string    `json:"matches,omitempty"`
	Extracts       map[string][]string    `json:"extracts,omitempty"`
	OutputExtracts []string               `json:"output_extracts,omitempty"`
	Events         []*ResultEvent         `json:"events,omitempty"`
	Request        string                 `json:"request,omitempty"`
	Response       string                 `json:"response,omitempty"`
	PayloadValues  map[string]interface{} `json:"payload_values,omitempty"`
}
