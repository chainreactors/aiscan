package jev

import (
	"slices"
	"strings"

	jevapi "github.com/chainreactors/cyber/agent/provider/jev"
)

// Catalog keys are stable choice values. Their meanings are ordinary context,
// not another decision schema. Sorting makes identical catalogs deterministic.
func choiceClaim(context string, labels map[string]string) Claim {
	options := make([]string, 0, len(labels))
	for id := range labels {
		options = append(options, id)
	}
	slices.Sort(options)
	var meanings strings.Builder
	for _, id := range options {
		meanings.WriteString("\n" + id + ": " + labels[id])
	}
	return Claim{Type: jevapi.ClaimChoice, Context: context + meanings.String(), Options: options}
}
