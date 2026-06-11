package cyberhub

import "github.com/chainreactors/sdk/pkg/types"

// FingerprintExport is one fingerprint export row returned by CyberHub.
// RawContent is the approved/effective YAML; RawContentDraft is the pending
// draft YAML returned when ExportFilter.WithDraft(true) is set.
type FingerprintExport struct {
	*types.Finger   `json:",inline" yaml:",inline"`
	Alias           *types.Alias `json:"alias,omitempty" yaml:"alias,omitempty"`
	Engine          string       `json:"engine,omitempty" yaml:"engine,omitempty"`
	Source          string       `json:"source,omitempty" yaml:"source,omitempty"`
	SourceNames     []string     `json:"source_names,omitempty" yaml:"source_names,omitempty"`
	RawContent      string       `json:"raw_content,omitempty" yaml:"raw_content,omitempty"`
	RawContentDraft string       `json:"raw_content_draft,omitempty" yaml:"raw_content_draft,omitempty"`
}

type fingerprintListResponse struct {
	Fingerprints []FingerprintExport `json:"fingerprints"`
	Total        int                 `json:"total"`
	Page         int                 `json:"page"`
	PageSize     int                 `json:"page_size"`
}

type pocResponse struct {
	*types.Template `json:",inline" yaml:",inline"`
	RawContent      string `json:"raw_content,omitempty" yaml:"raw_content,omitempty"`
	RawContentDraft string `json:"raw_content_draft,omitempty" yaml:"raw_content_draft,omitempty"`
}

type pocListResponse struct {
	POCs     []pocResponse `json:"pocs"`
	Total    int           `json:"total"`
	Exported int           `json:"exported"`
}

type apiResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}
