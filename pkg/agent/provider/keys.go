package provider

import (
	"strings"
	"sync/atomic"
)

type apiKeyRing struct {
	keys []string
	next atomic.Uint64
}

func newAPIKeyRing(cfg *ProviderConfig) *apiKeyRing {
	return &apiKeyRing{keys: normalizeAPIKeys(cfg.APIKey, cfg.APIKeys)}
}

func (r *apiKeyRing) Next() string {
	if r == nil || len(r.keys) == 0 {
		return ""
	}
	idx := r.next.Add(1) - 1
	return r.keys[idx%uint64(len(r.keys))]
}

func normalizeAPIKeys(primary string, extra []string) []string {
	seen := make(map[string]struct{})
	keys := make([]string, 0, 1+len(extra))
	add := func(raw string) {
		for _, part := range splitAPIKeyList(raw) {
			if _, ok := seen[part]; ok {
				continue
			}
			seen[part] = struct{}{}
			keys = append(keys, part)
		}
	}
	add(primary)
	for _, raw := range extra {
		add(raw)
	}
	return keys
}

func splitAPIKeyList(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case ',', '\n', '\r', '\t', ' ':
			return true
		default:
			return false
		}
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			out = append(out, field)
		}
	}
	return out
}
