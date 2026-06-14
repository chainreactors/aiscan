package ahocorasick

import "testing"

func TestExtractLiterals(t *testing.T) {
	tests := []struct {
		pattern  string
		expected []string
	}{
		{"(?i)powered by nginx", []string{"powered by nginx"}},
		{"(?i)(apache|nginx|lighttpd)", []string{"apache", "nginx", "lighttpd"}},
		{`server:\s*cloudflare`, []string{"cloudflare"}},
		{`<meta name="generator" content="WordPress"`, []string{`<meta name="generator" content="WordPress"`}},
		{`[a-z]+test`, []string{"test"}},
		{`\d+`, nil},
		{`(?i)^ab`, nil},
		{`(?i)nginx/([\d\.]+)`, []string{"nginx/"}},
		{`^X-Powered-By:\s+Express`, []string{"X-Powered-By:"}},
	}

	for _, tt := range tests {
		result := ExtractLiterals(tt.pattern)
		if tt.expected == nil {
			if result != nil {
				t.Errorf("ExtractLiterals(%q) = %v, want nil", tt.pattern, result)
			}
			continue
		}
		if len(result) != len(tt.expected) {
			t.Errorf("ExtractLiterals(%q) = %v, want %v", tt.pattern, result, tt.expected)
			continue
		}
		for i := range result {
			if result[i] != tt.expected[i] {
				t.Errorf("ExtractLiterals(%q)[%d] = %q, want %q", tt.pattern, i, result[i], tt.expected[i])
			}
		}
	}
}
