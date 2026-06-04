package output

import (
	"regexp"
	"strings"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

func StripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

func OutputPrefix(source string, colorFn func(string) string) string {
	return colorFn("[" + source + "]")
}

func FormatLine(prefix, body string, color Color) string {
	body = strings.TrimSpace(body)
	parts := []string{prefix}
	if body != "" {
		parts = append(parts, body)
	}
	return SanitizeLine(strings.Join(parts, " "), color)
}

func SanitizeLine(line string, color Color) string {
	line = strings.TrimSpace(line)
	if !color.Enabled {
		line = StripANSI(line)
	}
	return line
}

func TruncateStr(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func FirstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
