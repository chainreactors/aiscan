package agent

import (
	"context"

	"github.com/chainreactors/cyber/agent/hooks"
	"github.com/chainreactors/cyber/agent/provider"
	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/operation"
	"google.golang.org/protobuf/proto"
)

func afterModelHook(ctx context.Context, cfg Config, messages []*aop.Message, turn int) {
	if !hooks.AfterModel.Has(cfg.Hooks) {
		return
	}
	snapshot := make([]*aop.Message, len(messages))
	for i, message := range messages {
		snapshot[i] = proto.CloneOf(message)
	}
	cfg.Messages = snapshot
	ctx = ContextWithToolAgentConfig(ctx, cfg)
	_, err := hooks.AfterModel.Emit(ctx, cfg.Hooks, hooks.ContextEvent{
		SessionID: cfg.SessionID, TurnID: cfg.TurnID, Turn: turn, Messages: snapshot,
	})
	if err != nil {
		cfg.Logger.Warnf("after model: %v", err)
	}
}

func appendModelHook(ctx context.Context, cfg Config, messages []*aop.Message, turn int) []*aop.Message {
	if !hooks.BeforeModel.Has(cfg.Hooks) {
		return nil
	}
	snapshot := make([]*aop.Message, len(messages))
	for i, message := range messages {
		snapshot[i] = proto.CloneOf(message)
	}
	cfg.Messages = snapshot
	ctx = ContextWithToolAgentConfig(ctx, cfg)
	ctx = operation.ContextWithInvocation(ctx, operation.Invocation{
		SessionID: cfg.SessionID, TurnID: cfg.TurnID, Emitter: cfg.AgentName,
	})
	result, err := hooks.BeforeModel.Emit(ctx, cfg.Hooks, hooks.ContextEvent{
		SessionID: cfg.SessionID, TurnID: cfg.TurnID, Turn: turn, Messages: snapshot,
	})
	if err != nil {
		cfg.Logger.Warnf("before model: %v", err)
	}
	var appended []*aop.Message
	for _, message := range result {
		// A controller contributes observations, never fabricated assistant calls
		// or tool results without a corresponding model-issued call.
		if message == nil || message.Role != "user" || len(provider.MessageToolCalls(message)) != 0 || provider.MessageToolResult(message) != nil {
			continue
		}
		message = proto.CloneOf(message)
		message.Id = cfg.emitter.allocMessageID()
		cfg.emitter.messageProto(message)
		appended = append(appended, message)
	}
	return appended
}

// The kernel reaches the typed hook registry only through these helpers. Each
// helper preserves the zero-handler fast path exposed by hooks.Registry.

func runStartHook(ctx context.Context, cfg Config, systemPrompt string) (string, []*aop.Message) {
	if !hooks.BeforeRun.Has(cfg.Hooks) {
		return systemPrompt, nil
	}
	result, _ := hooks.BeforeRun.Emit(ctx, cfg.Hooks, hooks.RunStartEvent{
		SessionID:    cfg.SessionID,
		TurnID:       cfg.TurnID,
		AgentName:    cfg.AgentName,
		Model:        cfg.Model,
		SystemPrompt: systemPrompt,
		ToolNames:    toolNames(cfg),
	})
	if result.SystemPrompt != nil {
		systemPrompt = *result.SystemPrompt
	}
	return systemPrompt, result.Prepend
}

func toolNames(cfg Config) []string {
	if cfg.Tools == nil {
		return nil
	}
	definitions := cfg.Tools.ToolDefinitions()
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Name)
	}
	return names
}

func transformContextHook(ctx context.Context, cfg Config, messages []*aop.Message, turn int) []*aop.Message {
	if !hooks.Context.Has(cfg.Hooks) {
		return messages
	}
	result, _ := hooks.Context.Emit(ctx, cfg.Hooks, hooks.ContextEvent{
		SessionID: cfg.SessionID,
		Turn:      turn,
		Messages:  messages,
	})
	if result.Messages != nil {
		return result.Messages
	}
	return messages
}

func compactCanceled(ctx context.Context, cfg Config, trigger string, contextTokens int) (bool, string) {
	if !hooks.BeforeCompact.Has(cfg.Hooks) {
		return false, ""
	}
	result, _ := hooks.BeforeCompact.Emit(ctx, cfg.Hooks, hooks.CompactEvent{
		SessionID:     cfg.SessionID,
		Trigger:       trigger,
		ContextTokens: contextTokens,
		ContextWindow: cfg.ContextWindow,
	})
	return result.Cancel, result.Reason
}

func emitRunEnd(ctx context.Context, cfg Config, result *Result) {
	if result == nil || !hooks.RunEnd.Has(cfg.Hooks) {
		return
	}
	_, _ = hooks.RunEnd.Emit(ctx, cfg.Hooks, hooks.RunEndEvent{
		SessionID:      cfg.SessionID,
		TurnID:         cfg.TurnID,
		Stop:           result.Stop,
		Output:         result.Output,
		Messages:       result.Messages,
		MessageCounter: result.MessageCounter,
		Usage:          result.TotalUsage,
		Err:            result.Err,
	})
}
