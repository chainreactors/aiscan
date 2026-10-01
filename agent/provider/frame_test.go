package provider

import (
	"context"
	"encoding/json"
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
	for _, reasoning := range []string{`""`, `"brief reasoning"`, `null`} {
		r, err := parseOpenAIResponse([]byte(`{"choices":[{"message":{"role":"assistant","content":null,"reasoning_content":` + reasoning + `,"tool_calls":[{"id":"c1","type":"function","function":{"name":"bash","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`))
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
	event, err := parseOpenAIStreamChunk([]byte(`{"choices":[{"delta":{"reasoning_content":"","tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"bash","arguments":"{}"}}]}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := event.MessageDelta.GetValue().(*aop.MessageDelta_Reasoning); !ok || len(event.ToolDeltas) != 1 {
		t.Fatal("empty reasoning presence or tool call lost")
	}
}
