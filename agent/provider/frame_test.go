package provider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	aop "github.com/chainreactors/cyber/aop"
)

func TestFrameObserversCompose(t *testing.T) {
	var first, second int
	ctx := WithFrameObserver(context.Background(), func(RawFrame) { first++ })
	ctx = WithFrameObserver(ctx, func(RawFrame) { second++ })
	captureFrame(ctx, RawFrame{Direction: "request", Payload: []byte(`{}`)})
	if first != 1 || second != 1 {
		t.Fatalf("observers lost: %d %d", first, second)
	}
}

func TestEmptyReasoningSurvivesToolContinuation(t *testing.T) {
	for _, field := range []string{"reasoning_content", "reasoning"} {
		for _, reasoning := range []string{`""`, `"brief reasoning"`, `null`} {
			r, err := parseOpenAIResponse([]byte(`{"choices":[{"message":{"role":"assistant","content":null,"` + field + `":` + reasoning + `,"tool_calls":[{"id":"c1","type":"function","function":{"name":"bash","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`))
			if err != nil {
				t.Fatal(err)
			}
			data, err := marshalOpenAIRequest(&ChatCompletionRequest{Model: "deepseek-v4.1-flash", Messages: []*aop.Message{r.Choices[0].Message}})
			if err != nil {
				t.Fatal(err)
			}
			var wire struct {
				Messages []map[string]json.RawMessage `json:"messages"`
			}
			if err = json.Unmarshal(data, &wire); err != nil {
				t.Fatal(err)
			}
			got, present := wire.Messages[0]["reasoning_content"]
			if reasoning == "null" {
				if present {
					t.Fatal("absent reasoning was fabricated")
				}
			} else if !present || string(got) != reasoning {
				t.Fatalf("reasoning field lost in tool continuation: got %s, want %s", got, reasoning)
			}
		}
	}
}

func TestReasoningAliasSurvivesToolContinuation(t *testing.T) {
	r, err := parseOpenAIResponse([]byte(`{"choices":[{"message":{"role":"assistant","content":null,"reasoning":"gateway reasoning","tool_calls":[{"id":"c1","type":"function","function":{"name":"bash","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	data, err := marshalOpenAIRequest(&ChatCompletionRequest{Model: "deepseek-v4-flash", Messages: []*aop.Message{r.Choices[0].Message}})
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Messages []map[string]json.RawMessage `json:"messages"`
	}
	if err = json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if got := string(wire.Messages[0]["reasoning_content"]); got != `"gateway reasoning"` {
		t.Fatalf("reasoning alias was not canonicalized: %s", got)
	}
	if _, present := wire.Messages[0]["reasoning"]; present {
		t.Fatal("provider alias leaked into the next request")
	}
}

func TestOpenAIResponseUnwrapsOneLevelData(t *testing.T) {
	r, err := parseOpenAIResponse([]byte(`{"id":"outer-id","success":true,"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5},"data":{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "outer-id" || len(r.Choices) != 1 || r.Choices[0].FinishReason != "stop" {
		t.Fatalf("wrapped response was not normalized: %+v", r)
	}
	if r.Usage == nil || r.Usage.InputTokens != 2 || r.Usage.OutputTokens != 3 {
		t.Fatalf("wrapped usage was not preserved: %+v", r.Usage)
	}
}

func TestOpenAIResponseRejectsMissingOrAmbiguousChoices(t *testing.T) {
	for _, tc := range []struct {
		name, wire, want string
	}{
		{"missing", `{"success":true,"data":{"status":"ok"}}`, "no choices"},
		{"empty", `{"choices":[]}`, "no choices"},
		{"recursive wrapper", `{"data":{"data":{"choices":[{"message":{"content":"done"}}]}}}`, "no choices"},
		{"missing message", `{"choices":[{}]}`, "has no message"},
		{"null message", `{"choices":[{"message":null}]}`, "has no message"},
		{"ambiguous", `{"choices":[{"message":{"content":"root"}}],"data":{"choices":[{"message":{"content":"nested"}}]}}`, "ambiguous"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseOpenAIResponse([]byte(tc.wire)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("protocol error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestOpenAIResponseReasoningFieldsAgree(t *testing.T) {
	for _, value := range []string{`""`, `"think"`} {
		r, err := parseOpenAIResponse([]byte(`{"choices":[{"message":{"reasoning_content":` + value + `,"reasoning":` + value + `,"content":"done"}}]}`))
		if err != nil || r == nil {
			t.Fatalf("equal reasoning fields rejected: %v", err)
		}
	}
	if _, err := parseOpenAIResponse([]byte(`{"choices":[{"message":{"reasoning_content":"","reasoning":"different","content":"done"}}]}`)); err == nil || !strings.Contains(err.Error(), "conflicting") {
		t.Fatalf("conflicting reasoning error = %v", err)
	}
}

func TestReasoningAndCacheMissAreDistinctFromCacheWrite(t *testing.T) {
	r, err := parseOpenAIResponse([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":100,"completion_tokens":30,"total_tokens":130,"prompt_cache_hit_tokens":60,"prompt_cache_miss_tokens":40,"completion_tokens_details":{"reasoning_tokens":20}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if r.Usage.Detail["reasoning"] != 20 || r.Usage.Detail["cache_read"] != 60 || r.Usage.Detail["cache_miss"] != 40 || r.Usage.Detail["cache_write"] != 0 {
		t.Fatal(r.Usage)
	}
	r, err = parseOpenAIResponse([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":100,"completion_tokens":30,"total_tokens":130}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, known := r.Usage.Detail["reasoning"]; known {
		t.Fatal("unreported reasoning became measured zero")
	}
}

func TestStreamPreservesExplicitEmptyReasoning(t *testing.T) {
	for _, field := range []string{"reasoning_content", "reasoning"} {
		event, err := parseOpenAIStreamChunk([]byte(`{"choices":[{"delta":{"` + field + `":"","tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"bash","arguments":"{}"}}]}}]}`))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := event.MessageDelta.GetValue().(*aop.MessageDelta_Reasoning); !ok || len(event.ToolDeltas) != 1 {
			t.Fatal("empty reasoning presence or tool call lost")
		}
	}
}

func TestStreamAcceptsReasoningAliasAndDataWrapper(t *testing.T) {
	event, err := parseOpenAIStreamChunk([]byte(`{"data":{"choices":[{"delta":{"reasoning":"think"},"finish_reason":""}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if event.MessageDelta == nil || event.MessageDelta.GetReasoning() != "think" {
		t.Fatalf("stream reasoning alias lost: %+v", event.MessageDelta)
	}
}

func TestStreamNormalizesUsageAndErrors(t *testing.T) {
	event, err := parseOpenAIStreamChunk([]byte(`{"data":{"choices":[],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}}`))
	if err != nil || event.Usage == nil || event.Usage.TotalTokens != 5 {
		t.Fatalf("wrapped usage-only chunk: usage=%+v err=%v", event.Usage, err)
	}
	for _, tc := range []struct {
		wire, want string
	}{
		{`{"data":{"error":{"message":"gateway error"}}}`, "gateway error"},
		{`{"data":{"data":{"choices":[{"delta":{"content":"nested"}}]}}}`, "nested data wrapper"},
		{`{"choices":[{"delta":{"content":"done","reasoning_content":"","reasoning":"different"}}]}`, "conflicting"},
		{`{"choices":[{"delta":{"content":"root"}}],"data":{"choices":[{"delta":{"content":"nested"}}]}}`, "ambiguous"},
	} {
		if _, err := parseOpenAIStreamChunk([]byte(tc.wire)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("stream protocol error = %v, want %q", err, tc.want)
		}
	}
}
