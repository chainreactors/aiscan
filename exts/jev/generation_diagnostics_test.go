package jev

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chainreactors/cyber/agent/provider"
	aop "github.com/chainreactors/cyber/aop"
)

func TestStructuredGenerationUnwrapsArtifactsBeforeValidationAndObservation(t *testing.T) {
	e, cfg, _ := testInstallation(t, Config{Mode: "off"}, nil)
	source := `[{"type":"noul","context":"The requested current native result is available."}]`
	var observed *Generation
	sub := e.stream.Observe(func(event *aop.Event) {
		value := new(RuntimeEvent)
		if event.GetExtension() != nil && event.GetExtension().UnmarshalTo(value) == nil && value.GetGeneration().GetState() == "finished" {
			observed = value.GetGeneration()
		}
	})
	defer sub.Close(t.Context())
	cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		if !req.JSONOutput {
			t.Fatal("Claim generation omitted structured output control")
		}
		data, _ := json.Marshal(map[string]json.RawMessage{"claims": json.RawMessage(source)})
		return reply(provider.TextMessage("assistant", string(data))), nil
	})
	var artifact []Claim
	ctx := traceContext(t.Context(), declaration{session: "s", turn: "t", task: "task"}.trace())
	if err := e.generateClaims(ctx, cfg, map[string]string{}, &artifact); err != nil {
		t.Fatal(err)
	}
	if len(artifact) != 1 || artifact[0].Context != "The requested current native result is available." || observed == nil || observed.Output != source || observed.Error != "" {
		t.Fatal("transport JSON leaked into executable artifact or timeline")
	}
	for _, invalid := range []string{`{"claims": [], "analysis": "extra"}`, `{"observe":"js:({})"}`, `{"code":"js:({})"}`} {
		if _, err := declarationArtifact(invalid); err == nil {
			t.Fatalf("accepted invalid envelope: %s", invalid)
		}
	}
	claims, err := declarationArtifact(`{"claims": []}`)
	if err != nil || claims != "[]" {
		t.Fatalf("claim array changed: %s %v", claims, err)
	}
}

func TestGenerationFailureRetainsEvidenceWithoutPublishingPartialArtifact(t *testing.T) {
	for _, tc := range []struct{ name, output, finish, diagnostic string }{
		{"truncated", "{\"claims\":[", "length", "truncated at 8192 tokens"},
		{"reasoning_only", "", "stop", "returned no artifact"},
		{"tool_markup", "<｜DSML｜function_calls>read</｜DSML｜function_calls>", "stop", "tool-call markup"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, cfg, _ := testInstallation(t, Config{Mode: "off", DeclarationEffort: "none"}, nil)
			cfg.Provider = testProvider(func(_ context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
				if req.ReasoningEffort != "none" {
					t.Fatal("declaration inference control lost")
				}
				resp := reply(provider.TextMessage("assistant", tc.output))
				resp.Choices[0].FinishReason = tc.finish
				return resp, nil
			})
			var finished *Generation
			sub := e.stream.Observe(func(event *aop.Event) {
				value := new(RuntimeEvent)
				if event.GetExtension() != nil && event.GetExtension().UnmarshalTo(value) == nil {
					if g := value.GetGeneration(); g != nil && g.State == "finished" {
						finished = g
					}
				}
			})
			defer sub.Close(t.Context())
			var artifact []Claim
			err := e.generateClaims(traceContext(t.Context(), declaration{session: "session", turn: "turn", task: "task"}.trace()), cfg, map[string]string{"evidence": "recorded task"}, &artifact)
			if err == nil || !strings.Contains(err.Error(), tc.diagnostic) || artifact != nil {
				t.Fatalf("artifact=%v error=%v", artifact, err)
			}
			if finished == nil || finished.Output != tc.output || finished.Error != err.Error() || finished.Usage == nil {
				t.Fatalf("missing real failed-generation evidence: %+v", finished)
			}
		})
	}
}
