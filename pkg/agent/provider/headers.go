package provider

import (
	"fmt"
	"net/http"
	"strings"
)

func normalizeHeaders(headers map[string]string) (map[string]string, error) {
	if len(headers) == 0 {
		return nil, nil
	}
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

func applyCustomHeaders(req *http.Request, headers map[string]string) {
	for key, value := range headers {
		req.Header.Set(key, value)
	}
}

func cloneConfigWithNormalizedHeaders(cfg *ProviderConfig) (*ProviderConfig, error) {
	if cfg == nil {
		return nil, fmt.Errorf("provider config is nil")
	}
	clone := *cfg
	headers, err := normalizeHeaders(clone.Headers)
	if err != nil {
		return nil, err
	}
	clone.Headers = headers
	return &clone, nil
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
