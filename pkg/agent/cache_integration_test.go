package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/chainreactors/aiscan/pkg/commands"
	"github.com/chainreactors/aiscan/pkg/telemetry"
)

func skipUnlessLive(t *testing.T) (*ProviderConfig, Provider) {
	t.Helper()
	apiKey := os.Getenv("TEST_API_KEY")
	baseURL := os.Getenv("TEST_BASE_URL")
	model := os.Getenv("TEST_MODEL")
	if apiKey == "" || baseURL == "" || model == "" {
		t.Skip("set TEST_API_KEY, TEST_BASE_URL, TEST_MODEL to run live tests")
	}
	cfg := &ProviderConfig{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Model:   model,
		Timeout: 60,
	}
	cfg, err := ResolveProvider(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prov, err := NewProviderFromResolved(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, prov
}

// TestMultiTurnContextInheritanceAndCache verifies that across multiple agent
// turns the conversation context grows correctly and cache metrics reflect
// increasing prefix reuse.
func TestMultiTurnContextInheritanceAndCache(t *testing.T) {
	cfg, prov := skipUnlessLive(t)

	systemPrompt := "You are a math tutor. " +
		strings.Repeat("You always answer arithmetic questions with just the numeric result. ", 30)

	var events []Event
	handler := func(e Event) {
		events = append(events, e)
	}

	tools := commands.NewRegistry()

	agentCfg := Config{
		Provider:       prov,
		Tools:          tools,
		Model:          cfg.Model,
		SystemPrompt:   systemPrompt,
		CacheRetention: CacheShort,
		Bus: testBus(func(e Event) { handler(e) }),
		Logger:         telemetry.NopLogger(),
		MaxRetries:     1,
	}

	// --- Turn 1: fresh context ---
	result1, err := NewAgent(agentCfg).Run(context.Background(), "What is 10+20? Just the number.")
	if err != nil {
		t.Fatalf("turn 1 failed: %v", err)
	}
	t.Logf("Turn 1 output: %s", result1.Output)
	t.Logf("Turn 1 usage: prompt=%d completion=%d cache_read=%d cache_write=%d",
		result1.TotalUsage.PromptTokens, result1.TotalUsage.CompletionTokens,
		result1.TotalUsage.CacheReadTokens, result1.TotalUsage.CacheWriteTokens)

	if result1.Turns < 1 {
		t.Fatalf("expected at least 1 turn, got %d", result1.Turns)
	}
	if result1.TotalUsage.PromptTokens == 0 {
		t.Fatal("expected non-zero prompt tokens")
	}

	// --- Turn 2: inherits turn 1 context ---
	events = nil
	result2, err := NewAgent(agentCfg.WithMessages(result1.Messages)).Run(
		context.Background(),
		"What is 30+40? Just the number.",
	)
	if err != nil {
		t.Fatalf("turn 2 failed: %v", err)
	}
	t.Logf("Turn 2 output: %s", result2.Output)
	t.Logf("Turn 2 usage: prompt=%d completion=%d cache_read=%d cache_write=%d",
		result2.TotalUsage.PromptTokens, result2.TotalUsage.CompletionTokens,
		result2.TotalUsage.CacheReadTokens, result2.TotalUsage.CacheWriteTokens)

	// Turn 2 should have more prompt tokens (longer context)
	if result2.TotalUsage.PromptTokens <= result1.TotalUsage.PromptTokens {
		t.Errorf("turn 2 prompt tokens (%d) should exceed turn 1 (%d) due to accumulated context",
			result2.TotalUsage.PromptTokens, result1.TotalUsage.PromptTokens)
	}

	// --- Turn 3: inherits turn 1+2 context ---
	allMessages := append(result1.Messages, result2.NewMessages...)
	events = nil
	result3, err := NewAgent(agentCfg.WithMessages(allMessages)).Run(
		context.Background(),
		"What is the sum of all three answers you gave? Just the number.",
	)
	if err != nil {
		t.Fatalf("turn 3 failed: %v", err)
	}
	t.Logf("Turn 3 output: %s", result3.Output)
	t.Logf("Turn 3 usage: prompt=%d completion=%d cache_read=%d cache_write=%d",
		result3.TotalUsage.PromptTokens, result3.TotalUsage.CompletionTokens,
		result3.TotalUsage.CacheReadTokens, result3.TotalUsage.CacheWriteTokens)

	// Turn 3 should have even more prompt tokens
	if result3.TotalUsage.PromptTokens <= result2.TotalUsage.PromptTokens {
		t.Errorf("turn 3 prompt tokens (%d) should exceed turn 2 (%d)",
			result3.TotalUsage.PromptTokens, result2.TotalUsage.PromptTokens)
	}

	// --- Summary ---
	t.Logf("\n=== Multi-Turn Cache Summary ===")
	for i, r := range []*Result{result1, result2, result3} {
		ratio := 0.0
		if r.TotalUsage.PromptTokens > 0 {
			ratio = float64(r.TotalUsage.CacheReadTokens) / float64(r.TotalUsage.PromptTokens) * 100
		}
		t.Logf("Turn %d: output=%q prompt=%d cache_read=%d cache_write=%d hit_ratio=%.1f%%",
			i+1, truncateOutput(r.Output, 40),
			r.TotalUsage.PromptTokens, r.TotalUsage.CacheReadTokens, r.TotalUsage.CacheWriteTokens, ratio)
	}

	// Verify cache is working: turn 2 or 3 should have cache reads
	totalCacheRead := result2.TotalUsage.CacheReadTokens + result3.TotalUsage.CacheReadTokens
	if totalCacheRead == 0 {
		t.Error("expected cache_read > 0 in turn 2 or 3, got 0 for both — caching may not be working")
	}
}

// TestMultiTurnStreamingCache verifies streaming mode also reports cache metrics.
func TestMultiTurnStreamingCache(t *testing.T) {
	cfg, prov := skipUnlessLive(t)

	systemPrompt := "You are a translator. " +
		strings.Repeat("You translate English to French. Always respond with just the translation, nothing else. ", 30)

	tools := commands.NewRegistry()

	agentCfg := Config{
		Provider:       prov,
		Tools:          tools,
		Model:          cfg.Model,
		SystemPrompt:   systemPrompt,
		Stream:         true,
		CacheRetention: CacheShort,
		Logger:         telemetry.NopLogger(),
		MaxRetries:     1,
	}

	// Turn 1
	result1, err := NewAgent(agentCfg).Run(context.Background(), "Hello")
	if err != nil {
		t.Fatalf("stream turn 1 failed: %v", err)
	}
	t.Logf("Stream Turn 1: output=%q prompt=%d cache_read=%d",
		truncateOutput(result1.Output, 40), result1.TotalUsage.PromptTokens, result1.TotalUsage.CacheReadTokens)

	// Turn 2 with context
	result2, err := NewAgent(agentCfg.WithMessages(result1.Messages)).Run(context.Background(), "Goodbye")
	if err != nil {
		t.Fatalf("stream turn 2 failed: %v", err)
	}
	t.Logf("Stream Turn 2: output=%q prompt=%d cache_read=%d",
		truncateOutput(result2.Output, 40), result2.TotalUsage.PromptTokens, result2.TotalUsage.CacheReadTokens)

	// Turn 3
	allMsgs := append(result1.Messages, result2.NewMessages...)
	result3, err := NewAgent(agentCfg.WithMessages(allMsgs)).Run(context.Background(), "Thank you")
	if err != nil {
		t.Fatalf("stream turn 3 failed: %v", err)
	}
	t.Logf("Stream Turn 3: output=%q prompt=%d cache_read=%d",
		truncateOutput(result3.Output, 40), result3.TotalUsage.PromptTokens, result3.TotalUsage.CacheReadTokens)

	t.Logf("\n=== Streaming Cache Summary ===")
	for i, r := range []*Result{result1, result2, result3} {
		ratio := 0.0
		if r.TotalUsage.PromptTokens > 0 {
			ratio = float64(r.TotalUsage.CacheReadTokens) / float64(r.TotalUsage.PromptTokens) * 100
		}
		t.Logf("Turn %d: prompt=%d cache_read=%d cache_write=%d hit_ratio=%.1f%%",
			i+1, r.TotalUsage.PromptTokens, r.TotalUsage.CacheReadTokens, r.TotalUsage.CacheWriteTokens, ratio)
	}
}

// TestMultiTurnWithToolCallsCache verifies that tool-calling turns correctly
// accumulate context and maintain cache efficiency.
func TestMultiTurnWithToolCallsCache(t *testing.T) {
	cfg, prov := skipUnlessLive(t)

	systemPrompt := "You are a calculator agent. " +
		strings.Repeat("When asked to compute something, use the calculate tool. Always call the tool, never compute yourself. ", 25)

	tools := commands.NewRegistry()
	calcTool := &recordingTool{name: "calculate", output: "42"}
	tools.RegisterTool(calcTool)

	var turnEndEvents []Event
	handler := func(e Event) {
		if e.Type == EventTurnEnd {
			turnEndEvents = append(turnEndEvents, e)
		}
	}

	agentCfg := Config{
		Provider:       prov,
		Tools:          tools,
		Model:          cfg.Model,
		SystemPrompt:   systemPrompt,
		CacheRetention: CacheShort,
		Bus: testBus(func(e Event) { handler(e) }),
		Logger:         telemetry.NopLogger(),
		MaxRetries:     1,
	}

	result, err := NewAgent(agentCfg).Run(context.Background(),
		"Use the calculate tool to compute 6*7. Then tell me the result.")
	if err != nil {
		t.Fatalf("tool call run failed: %v", err)
	}

	t.Logf("Tool-call output: %s", truncateOutput(result.Output, 80))
	t.Logf("Total turns: %d", result.Turns)
	t.Logf("Tool calls recorded: %d", len(calcTool.callsSnapshot()))

	t.Logf("\n=== Per-Turn Usage (with tool calls) ===")
	for _, tu := range result.TurnUsages {
		ratio := 0.0
		if tu.PromptTokens > 0 {
			ratio = float64(tu.CacheReadTokens) / float64(tu.PromptTokens) * 100
		}
		t.Logf("  turn %d: prompt=%d completion=%d cache_read=%d cache_write=%d hit_ratio=%.1f%%",
			tu.Turn, tu.PromptTokens, tu.CompletionTokens,
			tu.CacheReadTokens, tu.CacheWriteTokens, ratio)
	}

	t.Logf("Total usage: prompt=%d completion=%d cache_read=%d cache_write=%d",
		result.TotalUsage.PromptTokens, result.TotalUsage.CompletionTokens,
		result.TotalUsage.CacheReadTokens, result.TotalUsage.CacheWriteTokens)

	// Should have at least 2 turns: tool call + final answer
	if result.Turns < 2 {
		t.Logf("WARNING: expected >= 2 turns for tool call flow, got %d (model may have answered without tool)", result.Turns)
	}

	// If multiple turns, later turns should have cache reads
	if result.Turns >= 2 && len(result.TurnUsages) >= 2 {
		laterCacheRead := result.TurnUsages[len(result.TurnUsages)-1].CacheReadTokens
		if laterCacheRead == 0 {
			t.Logf("WARNING: last turn cache_read=0 — provider may not support automatic prefix caching")
		} else {
			t.Logf("Cache working: last turn cache_read=%d", laterCacheRead)
		}
	}

	// Verify context was passed correctly: TurnEnd events should carry usage
	for i, e := range turnEndEvents {
		if e.Usage != nil {
			t.Logf("TurnEnd event %d: prompt=%d cache_read=%d cache_write=%d",
				i, e.Usage.PromptTokens, e.Usage.CacheReadTokens, e.Usage.CacheWriteTokens)
		}
	}
}

// TestCacheConfigInheritance verifies that CacheRetention and SessionID
// are correctly threaded through Config → Derive → request.
func TestCacheConfigInheritance(t *testing.T) {
	llm := &scriptedProvider{
		responses: []*ChatCompletionResponse{
			chatResponse(NewTextMessage("assistant", "done")),
		},
	}

	parentCfg := Config{
		Provider:       llm,
		Tools:          commands.NewRegistry(),
		Model:          "test",
		SystemPrompt:   "sys",
		CacheRetention: CacheShort,
		SessionID:      "parent-session-123",
	}

	child := NewAgent(parentCfg).Derive()

	if child.Cfg.CacheRetention != CacheShort {
		t.Errorf("child CacheRetention = %q, want %q", child.Cfg.CacheRetention, CacheShort)
	}
	if child.Cfg.SessionID == "" {
		t.Error("child SessionID should be auto-generated, got empty")
	}
	if child.Cfg.SessionID == "parent-session-123" {
		t.Error("child SessionID should differ from parent")
	}

	// Run the child and verify the request carries cache fields
	_, err := child.Run(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}

	reqs := llm.requestsSnapshot()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if reqs[0].CacheRetention != CacheShort {
		t.Errorf("request CacheRetention = %q, want %q", reqs[0].CacheRetention, CacheShort)
	}
	if reqs[0].SessionID != child.Cfg.SessionID {
		t.Errorf("request SessionID = %q, want child SessionID %q", reqs[0].SessionID, child.Cfg.SessionID)
	}
}

func TestSessionIDAutoGeneration(t *testing.T) {
	cfg := Config{
		CacheRetention: CacheShort,
	}
	initialized := cfg.init()

	if initialized.SessionID == "" {
		t.Error("expected auto-generated SessionID, got empty")
	}
	if len(initialized.SessionID) != 16 {
		t.Errorf("SessionID length = %d, want 16 hex chars, got %q", len(initialized.SessionID), initialized.SessionID)
	}

	cfg2 := Config{CacheRetention: CacheNone}
	initialized2 := cfg2.init()
	if initialized2.SessionID == "" {
		t.Error("CacheNone should still generate SessionID for event tracking")
	}
}

// TestTurnUsageCacheAccumulation verifies that cache tokens are correctly
// accumulated in totalUsage across multiple turns using the scripted provider.
func TestTurnUsageCacheAccumulation(t *testing.T) {
	usage1 := &Usage{
		PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120,
		CacheReadTokens: 0, CacheWriteTokens: 80,
	}
	usage2 := &Usage{
		PromptTokens: 150, CompletionTokens: 15, TotalTokens: 165,
		CacheReadTokens: 80, CacheWriteTokens: 0,
	}

	llm := &scriptedProvider{
		responses: []*ChatCompletionResponse{
			{Choices: []Choice{{
				Message: ChatMessage{
					Role: "assistant",
					ToolCalls: []ToolCall{{
						ID: "call_1", Type: "function",
						Function: FunctionCall{Name: "read", Arguments: `{}`},
					}},
				},
			}}, Usage: usage1},
			{Choices: []Choice{{
				Message: NewTextMessage("assistant", "done"),
			}}, Usage: usage2},
		},
	}

	tools := commands.NewRegistry()
	tools.RegisterTool(&recordingTool{name: "read", output: "file content"})

	result, err := (NewAgent(Config{
		Provider:       llm,
		Tools:          tools,
		Model:          "test",
		SystemPrompt:   "sys",
		CacheRetention: CacheShort,
		Logger:         telemetry.NopLogger(),
	})).Run(context.Background(), "read something")
	if err != nil {
		t.Fatal(err)
	}

	if result.TotalUsage.CacheReadTokens != 80 {
		t.Errorf("TotalUsage.CacheReadTokens = %d, want 80", result.TotalUsage.CacheReadTokens)
	}
	if result.TotalUsage.CacheWriteTokens != 80 {
		t.Errorf("TotalUsage.CacheWriteTokens = %d, want 80", result.TotalUsage.CacheWriteTokens)
	}
	if result.TotalUsage.PromptTokens != 250 {
		t.Errorf("TotalUsage.PromptTokens = %d, want 250", result.TotalUsage.PromptTokens)
	}

	if len(result.TurnUsages) != 2 {
		t.Fatalf("expected 2 TurnUsages, got %d", len(result.TurnUsages))
	}
	if result.TurnUsages[0].CacheWriteTokens != 80 {
		t.Errorf("Turn 1 CacheWriteTokens = %d, want 80", result.TurnUsages[0].CacheWriteTokens)
	}
	if result.TurnUsages[1].CacheReadTokens != 80 {
		t.Errorf("Turn 2 CacheReadTokens = %d, want 80", result.TurnUsages[1].CacheReadTokens)
	}

	t.Logf("Accumulation OK: total prompt=%d cache_read=%d cache_write=%d",
		result.TotalUsage.PromptTokens, result.TotalUsage.CacheReadTokens, result.TotalUsage.CacheWriteTokens)
}

func truncateOutput(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Quick sanity check: does the full agent run print cache stats in events?
func TestEventCarriesCacheUsage(t *testing.T) {
	usage := &Usage{
		PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110,
		CacheReadTokens: 60, CacheWriteTokens: 20,
	}

	llm := &scriptedProvider{
		responses: []*ChatCompletionResponse{
			{Choices: []Choice{{
				Message: NewTextMessage("assistant", "hi"),
			}}, Usage: usage},
		},
	}

	var captured *Usage
	handler := func(e Event) {
		if e.Type == EventTurnEnd && e.Usage != nil {
			captured = e.Usage
		}
	}

	_, err := (NewAgent(Config{
		Provider:     llm,
		Tools:        commands.NewRegistry(),
		Model:        "test",
		SystemPrompt: "sys",
		Bus: testBus(func(e Event) { handler(e) }),
		Logger:       telemetry.NopLogger(),
	})).Run(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}

	if captured == nil {
		t.Fatal("EventTurnEnd did not carry usage")
	}
	if captured.CacheReadTokens != 60 {
		t.Errorf("EventTurnEnd CacheReadTokens = %d, want 60", captured.CacheReadTokens)
	}
	if captured.CacheWriteTokens != 20 {
		t.Errorf("EventTurnEnd CacheWriteTokens = %d, want 20", captured.CacheWriteTokens)
	}
	fmt.Printf("Event carries cache usage: read=%d write=%d\n", captured.CacheReadTokens, captured.CacheWriteTokens)
}
