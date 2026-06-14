package pool

import (
	"net/url"
	"strings"

	"github.com/chainreactors/spray/pkg"
)

func urlInScope(raw string, scope []string) bool {
	if len(scope) == 0 {
		return true
	}
	for _, item := range scope {
		if strings.TrimSpace(item) == "*" {
			return true
		}
	}

	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return true
	}
	return pkg.MatchWithGlobs(strings.ToLower(parsed.Host), lowerScope(scope))
}

func lowerScope(scope []string) []string {
	out := make([]string, 0, len(scope))
	for _, item := range scope {
		item = strings.ToLower(strings.TrimSpace(item))
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}
