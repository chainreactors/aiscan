package web

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// RejectedTarget is one target token that failed validation, paired with a
// human-readable reason. A batch scan collects these and skips them instead of
// aborting, so a single typo in a pasted list no longer sinks the whole run.
type RejectedTarget struct {
	Target string `json:"target"`
	Reason string `json:"reason"`
}

// ParseTargets splits a raw batch of targets (separated by newlines, commas, or
// whitespace) and validates each one. It returns the accepted targets
// (deduplicated, order preserved) alongside any rejected tokens. Unlike a strict
// parse it never aborts on the first bad token — the caller scans the valid ones
// and can surface the skipped ones. It errors only when not a single target is
// valid.
func ParseTargets(raw string) (valid []string, rejected []RejectedTarget, err error) {
	seen := make(map[string]struct{})
	for _, field := range splitTargets(raw) {
		t, verr := validateOneTarget(field)
		if verr != nil {
			rejected = append(rejected, RejectedTarget{Target: field, Reason: verr.Error()})
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		valid = append(valid, t)
	}
	if len(valid) == 0 {
		if len(rejected) > 0 {
			return nil, rejected, fmt.Errorf("no valid targets (%s: %s)", rejected[0].Target, rejected[0].Reason)
		}
		return nil, nil, fmt.Errorf("target is required")
	}
	return valid, rejected, nil
}

// splitTargets breaks a raw batch string into individual, trimmed target tokens
// on commas, whitespace, and newlines. Empty tokens are discarded.
func splitTargets(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
}

func validateOneTarget(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("target is required")
	}

	if idx := strings.Index(raw, "/"); idx >= 0 {
		prefix := raw[:idx]
		if net.ParseIP(prefix) != nil {
			return "", fmt.Errorf("CIDR ranges are not allowed; provide a single IP or URL")
		}
		if host, _, err := net.SplitHostPort(prefix); err == nil && net.ParseIP(host) != nil {
			return "", fmt.Errorf("CIDR ranges are not allowed; provide a single IP or URL")
		}
	}

	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Hostname() == "" {
			return "", fmt.Errorf("invalid URL: %s", raw)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return "", fmt.Errorf("only http and https URLs are allowed")
		}
		return raw, nil
	}

	if _, _, err := net.SplitHostPort(raw); err == nil {
		return raw, nil
	}

	if net.ParseIP(raw) != nil {
		return raw, nil
	}

	if isValidHostname(raw) {
		return raw, nil
	}

	return "", fmt.Errorf("invalid target: %s (expected IP, IP:port, hostname, or URL)", raw)
}

func ValidateMode(mode string) (string, error) {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == "" {
		return "quick", nil
	}
	switch mode {
	case "quick", "full":
		return mode, nil
	default:
		return "", fmt.Errorf("invalid mode %q: must be quick or full", mode)
	}
}

func isValidHostname(s string) bool {
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	if !strings.Contains(s, ".") {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '.') {
			return false
		}
	}
	return true
}
