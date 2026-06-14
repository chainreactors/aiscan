package ahocorasick

import (
	"regexp"
	"strings"
)

// ExtractLiterals pulls literal substrings out of a regex pattern that must
// appear in any matching input. Returns nil if no useful literal (>= 3 chars)
// is found.
//
// For alternation groups like (key|password|passwd), all literal alternatives
// are returned. Otherwise the longest contiguous literal run is returned.
func ExtractLiterals(pattern string) []string {
	p := pattern
	if strings.HasPrefix(p, "(?i)") {
		p = p[4:]
	}
	if strings.HasPrefix(p, "^") {
		p = p[1:]
	}
	if strings.HasPrefix(p, `\b`) {
		p = p[2:]
	}

	if idx := strings.Index(p, "("); idx >= 0 {
		end := strings.Index(p[idx:], ")")
		if end > 0 {
			inner := p[idx+1 : idx+end]
			if strings.Contains(inner, "|") && !strings.ContainsAny(inner, "([{\\.*+?^$") {
				alternatives := strings.Split(inner, "|")
				var result []string
				for _, alt := range alternatives {
					alt = strings.TrimSpace(alt)
					if len(alt) >= 3 && regexp.QuoteMeta(alt) == alt {
						result = append(result, alt)
					}
				}
				if len(result) > 0 {
					return result
				}
			}
		}
	}

	var best string
	var current strings.Builder
	for i := 0; i < len(p); i++ {
		ch := p[i]
		if ch == '\\' && i+1 < len(p) {
			next := p[i+1]
			if regexp.QuoteMeta(string(next)) != string(next) || next == '\\' || next == '$' {
				current.WriteByte(next)
				i++
				continue
			}
			if current.Len() > len(best) {
				best = current.String()
			}
			current.Reset()
			i++
			continue
		}
		if ch == '[' {
			if current.Len() > len(best) {
				best = current.String()
			}
			current.Reset()
			for i++; i < len(p); i++ {
				if p[i] == '\\' && i+1 < len(p) {
					i++
				} else if p[i] == ']' {
					break
				}
			}
			continue
		}
		if ch == '{' {
			if current.Len() > len(best) {
				best = current.String()
			}
			current.Reset()
			for i++; i < len(p); i++ {
				if p[i] == '}' {
					break
				}
			}
			continue
		}
		if ch == '(' {
			if current.Len() > len(best) {
				best = current.String()
			}
			current.Reset()
			depth := 1
			hasAlt := false
			start := i
			for i++; i < len(p) && depth > 0; i++ {
				switch p[i] {
				case '\\':
					if i+1 < len(p) {
						i++
					}
				case '(':
					depth++
				case ')':
					depth--
				case '|':
					if depth == 1 {
						hasAlt = true
					}
				}
			}
			i--
			if !hasAlt {
				i = start
			}
			continue
		}
		if strings.ContainsRune(".*+?^$})|]", rune(ch)) {
			if current.Len() > len(best) {
				best = current.String()
			}
			current.Reset()
			continue
		}
		current.WriteByte(ch)
	}
	if current.Len() > len(best) {
		best = current.String()
	}

	if len(best) >= 3 {
		return []string{best}
	}
	return nil
}
