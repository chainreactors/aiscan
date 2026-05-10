package util

import (
	"strconv"
	"strings"
)

func AppendNonEmpty(parts []string, values ...string) []string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			parts = append(parts, v)
		}
	}
	return parts
}

func NeedsQuoting(value string) bool {
	return strings.ContainsAny(value, " \t\r\n\"")
}

func FormatValue(value string) string {
	value = strings.TrimSpace(value)
	if NeedsQuoting(value) {
		return strconv.Quote(value)
	}
	return value
}

func QuoteFields(parts []string) []string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, FormatValue(part))
	}
	return out
}

func CloneMap[K comparable, V any](m map[K]V) map[K]V {
	if m == nil {
		return nil
	}
	out := make(map[K]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
