package guardrail

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/chainreactors/cyber/aop"
	"google.golang.org/protobuf/proto"
)

var secretField = regexp.MustCompile("(?i)(password|passwd|secret|token|api.?key|authorization|cookie|credential|private.?key)")
var redactions = []*regexp.Regexp{
	regexp.MustCompile("(?is)-----BEGIN [^-]*PRIVATE KEY-----.*?-----END [^-]*PRIVATE KEY-----"),
	regexp.MustCompile("(?i)\\b(?:bearer|basic)\\s+[A-Za-z0-9._~+/=-]+"),
	regexp.MustCompile("(?i)\\b(?:apikey_|sk-)[A-Za-z0-9_-]+"),
	regexp.MustCompile("(?i)(?:https?|ftp)://[^\\s/@]+:[^\\s/@]+@"),
}
var secretAssignment = regexp.MustCompile("(?i)((?:[A-Za-z0-9_]*)(?:password|passwd|secret|token|api[_-]?key|authorization|cookie|credential)(?:[A-Za-z0-9_]*)(?:[\\\"']?\\s*[:=]\\s*|\\s+))(?:\\\"[^\\\"]*\\\"|'[^']*'|[^\\s,;\\\"']+)")
var curlAuth = regexp.MustCompile("(?i)((?:--user|-u)\\s+)(?:\\\"[^\\\"]*\\\"|'[^']*'|[^\\s;]+)")

// RedactText preserves command structure while removing recognizable secrets.
// This is a best-effort disclosure boundary, not an arbitrary-data DLP engine.
func RedactText(s string) string {
	for _, re := range redactions {
		s = re.ReplaceAllString(s, "[REDACTED]")
	}
	s = secretAssignment.ReplaceAllString(s, "1[REDACTED]")
	return curlAuth.ReplaceAllString(s, "1[REDACTED]")
}

func sanitizeValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for k, item := range v {
			if secretField.MatchString(k) {
				v[k] = "[REDACTED]"
			} else {
				v[k] = sanitizeValue(item)
			}
		}
	case []any:
		for i, item := range v {
			v[i] = sanitizeValue(item)
		}
	case string:
		return RedactText(v)
	}
	return value
}

// SanitizeCall returns a private presentation/provider snapshot. The executable
// invocation always retains its original arguments in the tool boundary.
func SanitizeCall(call *aop.ToolCall) *aop.ToolCall {
	if call == nil {
		return nil
	}
	out := proto.Clone(call).(*aop.ToolCall)
	out.WorkingDirectory = RedactText(out.WorkingDirectory)
	if out.Arguments != nil {
		decoder := json.NewDecoder(strings.NewReader(string(out.Arguments.Data)))
		decoder.UseNumber()
		var value any
		if json.Valid(out.Arguments.Data) && decoder.Decode(&value) == nil {
			out.Arguments.Data, _ = json.Marshal(sanitizeValue(value))
		} else {
			out.Arguments.Data = []byte(RedactText(string(out.Arguments.Data)))
		}
	}
	return out
}
