package config

import (
	"fmt"
	"net/http"
	"strings"
)

type HeaderFlags []string

func (h *HeaderFlags) UnmarshalFlag(value string) error {
	if _, _, ok, err := parseHeaderAssignment(value); err != nil {
		return err
	} else if ok {
		*h = append(*h, value)
	}
	return nil
}

func ParseHeaderFlags(values HeaderFlags) (map[string]string, error) {
	out := make(map[string]string)
	for _, raw := range values {
		key, value, ok, err := parseHeaderAssignment(raw)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if _, exists := out[key]; exists {
			return nil, fmt.Errorf("duplicate LLM header %q", key)
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func NormalizeHeaderMap(headers map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(headers))
	for key, value := range headers {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		if !validHeaderName(key) {
			return nil, fmt.Errorf("invalid LLM header name %q", key)
		}
		if strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("invalid LLM header %q: value must not contain CR or LF", key)
		}
		canonical := http.CanonicalHeaderKey(key)
		if _, exists := out[canonical]; exists {
			return nil, fmt.Errorf("duplicate LLM header %q", canonical)
		}
		out[canonical] = value
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func parseHeaderAssignment(raw string) (string, string, bool, error) {
	key, value, ok := strings.Cut(raw, "=")
	if !ok {
		return "", "", false, fmt.Errorf("invalid LLM header %q: expected KEY=VALUE", raw)
	}
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" || value == "" {
		return "", "", false, nil
	}
	if !validHeaderName(key) {
		return "", "", false, fmt.Errorf("invalid LLM header name %q", key)
	}
	if strings.ContainsAny(value, "\r\n") {
		return "", "", false, fmt.Errorf("invalid LLM header %q: value must not contain CR or LF", key)
	}
	return http.CanonicalHeaderKey(key), value, true, nil
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			continue
		}
		switch c {
		case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
			continue
		default:
			return false
		}
	}
	return true
}

func mergeHeaderMaps(fallback, override map[string]string) map[string]string {
	if len(fallback) == 0 && len(override) == 0 {
		return nil
	}
	out := make(map[string]string, len(fallback)+len(override))
	for key, value := range fallback {
		out[key] = value
	}
	for key, value := range override {
		canonical := http.CanonicalHeaderKey(strings.TrimSpace(key))
		for existing := range out {
			if http.CanonicalHeaderKey(strings.TrimSpace(existing)) == canonical {
				delete(out, existing)
			}
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeOptionHeaders(dst, src *Option) {
	if len(src.Headers) == 0 {
		return
	}
	dst.Headers = mergeHeaderMaps(src.Headers, dst.Headers)
}
