package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/chainreactors/aiscan/pkg/provider"
	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/chainreactors/aiscan/pkg/tool"
)

func Run(ctx context.Context, prompt string, tools *tool.ToolRegistry, opts ...Option) (string, error) {
	result, err := RunWithEvents(ctx, prompt, tools, nil, opts...)
	if err != nil {
		return "", err
	}
	return result.Output, nil
}

func RunWithEvents(ctx context.Context, prompt string, tools *tool.ToolRegistry, emit EventHandler, opts ...Option) (*Result, error) {
	allOpts := make([]Option, 0, len(opts)+1)
	allOpts = append(allOpts, opts...)
	if emit != nil {
		allOpts = append(allOpts, WithEventHandler(emit))
	}
	cfg := newConfig(allOpts...)
	ag := New(cfg.Provider, tools, allOpts...)
	return ag.Prompt(ctx, prompt)
}

func RunLoop(ctx context.Context, prompts []provider.ChatMessage, agentCtx Context, cfg Config) (*Result, error) {
	return runLoop(ctx, prompts, agentCtx, normalizeConfig(cfg))
}

func runLoop(ctx context.Context, prompts []provider.ChatMessage, agentCtx Context, cfg Config) (*Result, error) {
	if cfg.Provider == nil {
		return nil, fmt.Errorf("agent provider is nil")
	}
	if agentCtx.Tools == nil {
		agentCtx.Tools = tool.NewToolRegistry()
	}
	if agentCtx.SystemPrompt == "" {
		agentCtx.SystemPrompt = cfg.SystemPrompt
	}

	transcript := newTranscript(agentCtx.Messages, len(prompts)+4)
	turn := 1

	emitFn := cfg.Emit
	if err := emit(ctx, emitFn, Event{Type: EventAgentStart}); err != nil {
		return nil, err
	}
	ended := false
	end := func(result *Result, err error) (*Result, error) {
		if result == nil {
			result = transcript.result("", transcript.completedTurns, err)
		}
		if err != nil && result.Err == nil {
			result.Err = err
		}
		if !ended {
			ended = true
			endEvent := Event{
				Type:        EventAgentEnd,
				Turn:        result.Turns,
				Messages:    append([]provider.ChatMessage(nil), result.Messages...),
				NewMessages: append([]provider.ChatMessage(nil), result.NewMessages...),
				Err:         result.Err,
			}
			if emitErr := emit(ctx, emitFn, endEvent); emitErr != nil && err == nil {
				err = emitErr
				result.Err = emitErr
			}
		}
		return result, err
	}

	for turn = 1; turn <= cfg.MaxTurns; turn++ {
		if err := ctx.Err(); err != nil {
			failure := provider.NewTextMessage("assistant", "")
			transcript.append(failure)
			return end(nil, err)
		}
		if err := emit(ctx, emitFn, Event{Type: EventTurnStart, Turn: turn}); err != nil {
			return end(nil, err)
		}

		if len(prompts) > 0 {
			for _, msg := range prompts {
				transcript.append(msg)
				if err := emitMessage(ctx, emitFn, turn, msg); err != nil {
					return end(nil, err)
				}
			}
			prompts = nil
		}

		reqMessages := requestMessages(agentCtx.SystemPrompt, transcript.messages, cfg.TransformContext)
		cfg.Logger.Debugf("[turn %d/%d] sending %d messages to LLM", turn, cfg.MaxTurns, len(reqMessages))

		assistantMsg, err := requestAssistantMessage(ctx, cfg, reqMessages, agentCtx.Tools.Definitions(), turn)
		if err != nil {
			failure := provider.NewTextMessage("assistant", "")
			transcript.append(failure)
			if emitErr := emitMessage(ctx, emitFn, turn, failure); emitErr != nil {
				return end(nil, emitErr)
			}
			if emitErr := emit(ctx, emitFn, Event{Type: EventTurnEnd, Turn: turn, Message: failure, Err: err}); emitErr != nil {
				return end(nil, emitErr)
			}
			transcript.completedTurns = turn
			return end(nil, err)
		}
		transcript.append(assistantMsg)

		var toolResults []provider.ChatMessage
		terminate := false
		if len(assistantMsg.ToolCalls) > 0 {
			batch, err := executeToolCalls(ctx, agentCtx, assistantMsg, cfg, turn)
			if err != nil {
				return end(nil, err)
			}
			toolResults = batch.messages
			terminate = batch.terminate
			transcript.append(toolResults...)
		}

		if err := emit(ctx, emitFn, Event{Type: EventTurnEnd, Turn: turn, Message: assistantMsg, ToolResults: toolResults}); err != nil {
			return end(nil, err)
		}
		transcript.completedTurns = turn

		if cfg.ShouldStopAfterTurn != nil {
			messages, newMessages := transcript.snapshot()
			stop, err := cfg.ShouldStopAfterTurn(ctx, ShouldStopAfterTurnContext{
				Message:     assistantMsg,
				ToolResults: append([]provider.ChatMessage(nil), toolResults...),
				Context: Context{
					SystemPrompt: agentCtx.SystemPrompt,
					Messages:     messages,
					Tools:        agentCtx.Tools,
				},
				NewMessages: newMessages,
			})
			if err != nil {
				return end(nil, err)
			}
			if stop {
				cfg.Logger.Importantf("agent status=stopped turns=%d", turn)
				return end(transcript.result(messageContent(assistantMsg), turn, nil), nil)
			}
		}

		if len(assistantMsg.ToolCalls) == 0 || terminate {
			cfg.Logger.Importantf("agent status=completed turns=%d", turn)
			return end(transcript.result(messageContent(assistantMsg), turn, nil), nil)
		}
	}

	return end(nil, fmt.Errorf("agent exceeded max turns (%d)", cfg.MaxTurns))
}

type transcript struct {
	messages       []provider.ChatMessage
	newMessages    []provider.ChatMessage
	completedTurns int
}

func newTranscript(base []provider.ChatMessage, newCapacity int) *transcript {
	return &transcript{
		messages:    append([]provider.ChatMessage(nil), base...),
		newMessages: make([]provider.ChatMessage, 0, newCapacity),
	}
}

func (t *transcript) append(messages ...provider.ChatMessage) {
	t.messages = append(t.messages, messages...)
	t.newMessages = append(t.newMessages, messages...)
}

func (t *transcript) snapshot() ([]provider.ChatMessage, []provider.ChatMessage) {
	return append([]provider.ChatMessage(nil), t.messages...), append([]provider.ChatMessage(nil), t.newMessages...)
}

func (t *transcript) result(output string, turns int, err error) *Result {
	messages, newMessages := t.snapshot()
	return &Result{
		Output:      output,
		NewMessages: newMessages,
		Messages:    messages,
		Turns:       turns,
		Err:         err,
	}
}

func emitMessage(ctx context.Context, emitFn EventHandler, turn int, msg provider.ChatMessage) error {
	if err := emit(ctx, emitFn, Event{Type: EventMessageStart, Turn: turn, Message: msg}); err != nil {
		return err
	}
	return emit(ctx, emitFn, Event{Type: EventMessageEnd, Turn: turn, Message: msg})
}

func requestAssistantMessage(ctx context.Context, cfg Config, messages []provider.ChatMessage, tools []provider.ToolDefinition, turn int) (provider.ChatMessage, error) {
	req := &provider.ChatCompletionRequest{
		Model:       cfg.Model,
		Messages:    messages,
		Tools:       tools,
		MaxTokens:   cfg.MaxTokens,
		Temperature: cfg.Temperature,
	}
	if cfg.Stream {
		if streaming, ok := cfg.Provider.(provider.StreamingProvider); ok {
			return streamAssistantMessage(ctx, streaming, req, cfg.Emit, cfg.Logger, turn)
		}
	}

	resp, err := cfg.Provider.ChatCompletion(ctx, req)
	if err != nil {
		return provider.ChatMessage{}, fmt.Errorf("LLM call failed at turn %d: %w", turn, err)
	}
	if len(resp.Choices) == 0 {
		return provider.ChatMessage{}, fmt.Errorf("empty response from LLM at turn %d", turn)
	}
	msg := resp.Choices[0].Message
	if err := emit(ctx, cfg.Emit, Event{Type: EventMessageStart, Turn: turn, Message: msg}); err != nil {
		return provider.ChatMessage{}, err
	}
	if err := emit(ctx, cfg.Emit, Event{Type: EventMessageEnd, Turn: turn, Message: msg}); err != nil {
		return provider.ChatMessage{}, err
	}
	logAssistantAndUsage(cfg.Logger, msg, resp.Usage)
	return msg, nil
}

func streamAssistantMessage(ctx context.Context, p provider.StreamingProvider, req *provider.ChatCompletionRequest, emitFn EventHandler, logger telemetry.Logger, turn int) (provider.ChatMessage, error) {
	events, err := p.ChatCompletionStream(ctx, req)
	if err != nil {
		return provider.ChatMessage{}, fmt.Errorf("LLM stream failed at turn %d: %w", turn, err)
	}

	builder := newMessageBuilder()
	started := false
	var usage *provider.Usage
	for event := range events {
		if event.Err != nil {
			return provider.ChatMessage{}, fmt.Errorf("LLM stream failed at turn %d: %w", turn, event.Err)
		}
		if event.Usage != nil {
			usage = event.Usage
		}
		if event.Done {
			break
		}
		updated := builder.Apply(event.Delta)
		if !started {
			started = true
			if err := emit(ctx, emitFn, Event{Type: EventMessageStart, Turn: turn, Message: updated}); err != nil {
				return provider.ChatMessage{}, err
			}
		}
		if err := emit(ctx, emitFn, Event{Type: EventMessageUpdate, Turn: turn, Message: updated}); err != nil {
			return provider.ChatMessage{}, err
		}
	}

	msg := builder.Message()
	if !started {
		if err := emit(ctx, emitFn, Event{Type: EventMessageStart, Turn: turn, Message: msg}); err != nil {
			return provider.ChatMessage{}, err
		}
	}
	if err := emit(ctx, emitFn, Event{Type: EventMessageEnd, Turn: turn, Message: msg}); err != nil {
		return provider.ChatMessage{}, err
	}
	logAssistantAndUsage(logger, msg, usage)
	return msg, nil
}

type toolBatchResult struct {
	messages  []provider.ChatMessage
	terminate bool
}

func executeToolCalls(ctx context.Context, agentCtx Context, assistantMsg provider.ChatMessage, cfg Config, turn int) (toolBatchResult, error) {
	results := make([]provider.ChatMessage, 0, len(assistantMsg.ToolCalls))
	terminations := 0
	for _, tc := range assistantMsg.ToolCalls {
		cfg.Logger.Infof("tool_call name=%s args=%q", tc.Function.Name, preview(tc.Function.Arguments, 200))

		if err := emit(ctx, cfg.Emit, Event{
			Type:       EventToolExecutionStart,
			Turn:       turn,
			ToolCallID: tc.ID,
			ToolName:   tc.Function.Name,
			Arguments:  tc.Function.Arguments,
		}); err != nil {
			return toolBatchResult{}, err
		}

		execution := runToolCall(ctx, agentCtx, assistantMsg, tc, cfg)

		if err := emit(ctx, cfg.Emit, Event{
			Type:       EventToolExecutionEnd,
			Turn:       turn,
			ToolCallID: tc.ID,
			ToolName:   tc.Function.Name,
			Arguments:  tc.Function.Arguments,
			Result:     execution.result,
			IsError:    execution.isError,
			Err:        execution.err,
		}); err != nil {
			return toolBatchResult{}, err
		}
		cfg.Logger.Debugf("tool_result name=%s bytes=%d", tc.Function.Name, len(execution.result))
		toolMsg := provider.NewToolResultMessage(tc.ID, execution.result)
		if err := emitMessage(ctx, cfg.Emit, turn, toolMsg); err != nil {
			return toolBatchResult{}, err
		}
		results = append(results, toolMsg)
		if execution.terminate {
			terminations++
		}
	}
	return toolBatchResult{
		messages:  results,
		terminate: len(results) > 0 && terminations == len(results),
	}, nil
}

type toolExecution struct {
	result    string
	isError   bool
	err       error
	terminate bool
}

func runToolCall(ctx context.Context, agentCtx Context, assistantMsg provider.ChatMessage, tc provider.ToolCall, cfg Config) toolExecution {
	execution := beforeToolCall(ctx, agentCtx, assistantMsg, tc, cfg)
	if execution.result == "" && !execution.isError {
		result, execErr := agentCtx.Tools.Execute(ctx, tc.Function.Name, tc.Function.Arguments)
		execution.result = result
		execution.err = execErr
		execution.isError = execErr != nil
		if execErr != nil {
			execution.result = fmt.Sprintf("error: %s", execErr.Error())
			cfg.Logger.Warnf("tool_error name=%s error=%q", tc.Function.Name, execErr.Error())
		}
	}
	execution.result = truncateResult(execution.result)
	return afterToolCall(ctx, agentCtx, assistantMsg, tc, cfg, execution)
}

func beforeToolCall(ctx context.Context, agentCtx Context, assistantMsg provider.ChatMessage, tc provider.ToolCall, cfg Config) toolExecution {
	if cfg.BeforeToolCall == nil {
		return toolExecution{}
	}
	before, err := cfg.BeforeToolCall(ctx, BeforeToolCallContext{
		AssistantMessage: assistantMsg,
		ToolCall:         tc,
		Context:          agentCtx,
	})
	if err != nil {
		return toolExecution{result: fmt.Sprintf("error: %s", err.Error()), isError: true, err: err}
	}
	if before == nil || !before.Block {
		return toolExecution{}
	}
	result := before.Reason
	if result == "" {
		result = "tool execution was blocked"
	}
	return toolExecution{result: result, isError: true}
}

func afterToolCall(ctx context.Context, agentCtx Context, assistantMsg provider.ChatMessage, tc provider.ToolCall, cfg Config, execution toolExecution) toolExecution {
	if cfg.AfterToolCall == nil {
		return execution
	}
	after, err := cfg.AfterToolCall(ctx, AfterToolCallContext{
		AssistantMessage: assistantMsg,
		ToolCall:         tc,
		Result:           execution.result,
		IsError:          execution.isError,
		Context:          agentCtx,
	})
	if err != nil {
		execution.result = fmt.Sprintf("error: %s", err.Error())
		execution.isError = true
		execution.err = err
		return execution
	}
	if after == nil {
		return execution
	}
	if after.Result != nil {
		execution.result = *after.Result
	}
	if after.IsError != nil {
		execution.isError = *after.IsError
		if !execution.isError {
			execution.err = nil
		}
	}
	execution.terminate = after.Terminate
	return execution
}

func truncateResult(result string) string {
	if len(result) <= maxResultSize {
		return result
	}
	return result[:maxResultSize] + "\n... (truncated)"
}

func preview(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func requestMessages(systemPrompt string, messages []provider.ChatMessage, transform TransformContextFunc) []provider.ChatMessage {
	out := append([]provider.ChatMessage(nil), messages...)
	if transform != nil {
		out = transform(out)
	}
	if systemPrompt != "" {
		out = append([]provider.ChatMessage{provider.NewTextMessage("system", systemPrompt)}, out...)
	}
	return out
}

func emit(ctx context.Context, fn EventHandler, event Event) error {
	if fn == nil {
		return nil
	}
	return fn(ctx, event)
}

func messageContent(msg provider.ChatMessage) string {
	if msg.Content == nil {
		return ""
	}
	return *msg.Content
}

func logAssistantAndUsage(logger telemetry.Logger, msg provider.ChatMessage, usage *provider.Usage) {
	if content := messageContent(msg); content != "" {
		logger.Infof("assistant output=%q", preview(compactLogContent(content), 500))
	}
	if usage != nil {
		logger.Debugf("usage prompt=%d completion=%d total=%d",
			usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)
	}
}

func compactLogContent(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func newConfig(opts ...Option) Config {
	cfg := normalizeConfig(Config{})
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return normalizeConfig(cfg)
}

func normalizeConfig(cfg Config) Config {
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = 50
	}
	if cfg.Logger == nil {
		cfg.Logger = telemetry.NopLogger()
	}
	return cfg
}

type messageBuilder struct {
	role             string
	content          strings.Builder
	reasoningContent strings.Builder
	toolCalls        map[int]*provider.ToolCall
}

func newMessageBuilder() *messageBuilder {
	return &messageBuilder{
		role:      "assistant",
		toolCalls: make(map[int]*provider.ToolCall),
	}
}

func (b *messageBuilder) Apply(delta provider.ChatMessageDelta) provider.ChatMessage {
	if delta.Role != "" {
		b.role = delta.Role
	}
	if delta.Content != nil {
		b.content.WriteString(*delta.Content)
	}
	if delta.ReasoningContent != nil {
		b.reasoningContent.WriteString(*delta.ReasoningContent)
	}
	for _, tcDelta := range delta.ToolCalls {
		tc := b.toolCalls[tcDelta.Index]
		if tc == nil {
			tc = &provider.ToolCall{Type: "function"}
			b.toolCalls[tcDelta.Index] = tc
		}
		if tcDelta.ID != "" {
			tc.ID = tcDelta.ID
		}
		if tcDelta.Type != "" {
			tc.Type = tcDelta.Type
		}
		if tcDelta.Function.Name != "" {
			tc.Function.Name = tcDelta.Function.Name
		}
		if tcDelta.Function.Arguments != "" {
			tc.Function.Arguments += tcDelta.Function.Arguments
		}
	}
	return b.Message()
}

func (b *messageBuilder) Message() provider.ChatMessage {
	content := b.content.String()
	msg := provider.ChatMessage{Role: b.role}
	if content != "" {
		msg.Content = &content
	}
	if reasoningContent := b.reasoningContent.String(); reasoningContent != "" {
		msg.ReasoningContent = &reasoningContent
	}
	if len(b.toolCalls) > 0 {
		indexes := make([]int, 0, len(b.toolCalls))
		for index := range b.toolCalls {
			indexes = append(indexes, index)
		}
		sort.Ints(indexes)
		msg.ToolCalls = make([]provider.ToolCall, 0, len(indexes))
		for _, index := range indexes {
			msg.ToolCalls = append(msg.ToolCalls, *b.toolCalls[index])
		}
	}
	return msg
}
