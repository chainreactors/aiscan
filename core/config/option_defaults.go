package config

import (
	"strconv"
	"strings"
)

func ResolveString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func ResolveStringSlice(value, fallback []string) []string {
	if len(value) > 0 {
		return append([]string(nil), value...)
	}
	return append([]string(nil), fallback...)
}

func ParseStringList(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case ',', '\n', '\r', '\t', ' ':
			return true
		default:
			return false
		}
	})
	out := make([]string, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		out = append(out, field)
	}
	return out
}

func DefaultInt(value string, fallback int) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func resolveSpace(space string) string {
	if space != "" && space != "default" {
		return space
	}
	return ResolveString(DefaultSpace, "default")
}

func ApplyDefaults(option *Option) {
	option.CyberhubURL = ResolveString(option.CyberhubURL, DefaultCyberhubURL)
	option.CyberhubKey = ResolveString(option.CyberhubKey, DefaultCyberhubKey)
	mode := ResolveString(option.CyberhubMode, DefaultCyberhubMode)
	option.CyberhubMode = ResolveString(mode, "merge")
	option.Proxy = ResolveString(option.Proxy, DefaultScannerProxy)
	option.IOAURL = ResolveString(option.IOAURL, DefaultIOAURL)
	option.IOANodeID = ResolveString(option.IOANodeID, DefaultIOANodeID)
	option.IOANodeName = ResolveString(option.IOANodeName, DefaultIOANodeName)
	option.Space = resolveSpace(option.Space)
	if option.Model == "" {
		option.Model = ResolveString(DefaultModel, "deepseek-v4-pro")
	}
}
