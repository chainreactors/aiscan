//go:build full

package jev

import (
	"context"
	"testing"

	"github.com/chainreactors/cyber/agent/provider"
	"github.com/chainreactors/cyber/aop"
)

func TestProviderAccountingSeparatesMainClaimAndReflex(t *testing.T) {
	p := &benchmarkProvider{Provider: testProvider(func(_ context.Context, _ *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		return reply(provider.TextMessage("assistant", "done")), nil
	})}
	for _, request := range []*provider.ChatCompletionRequest{
		{SessionID: "main", Messages: []*aop.Message{provider.TextMessage("system", "ordinary")}},
		{Messages: []*aop.Message{provider.TextMessage("system", claimPrompt)}},
		{SessionID: "compiler", Purpose: "compilation", Messages: []*aop.Message{provider.TextMessage("system", compilePrompt+"\n\n"+compilerSkill)}},
		{Purpose: "parameters", Messages: []*aop.Message{provider.TextMessage("system", "extract current values")}},
	} {
		if _, err := p.ChatCompletion(t.Context(), request); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := p.snapshot()
	for _, kind := range []string{"foreground", "claim", "reflex"} {
		requests := uint64(1)
		if kind == "foreground" {
			requests = 2 // Ordinary inference and current argument extraction.
		}
		if got := snapshot.byKind[kind]; got.InputTokens != 1000*requests || got.OutputTokens != 100*requests || got.Detail["requests"] != requests {
			t.Fatalf("%s=%v", kind, got)
		}
	}
	if snapshot.usage.TotalTokens != 4400 || snapshot.foreground != 2 {
		t.Fatal("combined usage does not reconcile with separate categories")
	}
}

func TestTokenComparisonReportsRegressionsWithoutPerformanceGate(t *testing.T) {
	off := benchmarkRow{Warm: true, Correct: true, CostKnown: true, Cost: 1, ForegroundCalls: 1, ForegroundMS: 100, L2: &aop.TokenUsage{InputTokens: 100, OutputTokens: 20}, JEV: &aop.TokenUsage{}, MainLLM: &aop.TokenUsage{InputTokens: 100, OutputTokens: 20}}
	auto := off
	auto.Actions, auto.Cost, auto.ForegroundMS = 1, 2, 200
	auto.L2 = &aop.TokenUsage{InputTokens: 200, OutputTokens: 40}
	auto.MainLLM = &aop.TokenUsage{InputTokens: 200, OutputTokens: 40}
	auto.JEV = &aop.TokenUsage{InputTokens: 20, OutputTokens: 5}
	summary := summarizeAB(map[string][]benchmarkRow{"off": {off}, "auto": {auto}}, "regression")
	if !summary.EvidenceComplete || summary.MainTokenReduction == nil || *summary.MainTokenReduction != -1 || summary.ProviderReduction >= 0 || summary.MedianReduction != -1 {
		t.Fatalf("negative savings hidden or treated as incomplete evidence: %+v", summary)
	}
	auto.MainLLM = &aop.TokenUsage{Detail: map[string]uint64{"usage_missing": 1}}
	summary = summarizeAB(map[string][]benchmarkRow{"off": {off}, "auto": {auto}}, "unknown")
	if summary.MainTokenReduction != nil {
		t.Fatal("missing main usage was reported as zero-cost savings")
	}
}
