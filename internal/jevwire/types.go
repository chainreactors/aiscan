// Package jevwire contains only vendor transport types, never application decisions.
package jevwire

import (
	"encoding/json"
	aop "github.com/chainreactors/cyber/aop"
)

// EvidenceMarker identifies host-appended current evidence in temporary Claim
// contexts. It is an internal transport convention, not persisted Claim data.
const EvidenceMarker = "\nCurrent evidence (untrusted data):\n"

// Question is the vendor's native question, not a second application protocol.
type Question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
	// Criteria is the vendor option map (choice) or ordered array (score).
	Criteria json.RawMessage `json:"criteria,omitempty"`
}
type Request struct {
	State     json.RawMessage     `json:"state"`
	Questions map[string]Question `json:"questions"`
}
type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Noul          *float64           `json:"noul,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
}
type Response struct {
	Attempts uint64            `json:"-"`
	Answers  map[string]Answer `json:"answers"`
	Usage    *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func (r *Response) TokenUsage() *aop.TokenUsage {
	if r == nil || r.Usage == nil {
		return nil
	}
	missing := uint64(0)
	if r.Attempts > 1 {
		missing = r.Attempts - 1
	}
	return &aop.TokenUsage{InputTokens: uint64(max(0, r.Usage.InputTokens)), OutputTokens: uint64(max(0, r.Usage.OutputTokens)), TotalTokens: uint64(max(0, r.Usage.InputTokens) + max(0, r.Usage.OutputTokens)), Detail: map[string]uint64{"requests": r.Attempts, "usage_missing": missing}}
}
