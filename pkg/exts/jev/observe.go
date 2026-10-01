package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/chainreactors/cyber/agent"
	aop "github.com/chainreactors/cyber/aop"
)

// Observe only transforms interaction data into finite native bindings. Reading
// external state is an ordinary candidate call, selected by JEV and executed
// through the same Executor as any other operation.
type binding struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Read      bool            `json:"read,omitempty"`
}

type observation struct {
	context json.RawMessage
	facts   map[string]json.RawMessage
	choices map[string]*aop.Content
	reads   map[string]bool
}

// Capture the actual controller boundary before the model supplements it. Later
// successful model calls cannot retroactively make a missing binding complete.
func (o *observation) handoffSnapshot() json.RawMessage {
	bindings := map[string]string{}
	for key, content := range o.choices {
		bindings[key] = canonical(content.GetToolCall())
	}
	data, err := json.Marshal(map[string]any{"context": o.context, "observations": o.facts, "candidates": bindings})
	if err != nil || len(data) > 56<<10 {
		return nil
	}
	return data
}

// capabilities is the existing tool surface, not a registry of JEV adapters.
func (e *Extension) capabilities(cfg agent.Config) (map[string]any, error) {
	tools := []map[string]any{}
	if cfg.Tools != nil {
		for _, definition := range cfg.Tools.ToolDefinitions() {
			if definition == nil || !json.Valid(definition.GetInputSchema().GetData()) {
				return nil, errors.New("invalid native tool definition")
			}
			tools = append(tools, map[string]any{"name": definition.Name, "description": definition.Description, "input_schema": json.RawMessage(definition.GetInputSchema().GetData())})
		}
	}
	commands := []map[string]any{}
	if e.commands != nil {
		for _, command := range e.commands.All() {
			commands = append(commands, map[string]any{"name": command.Name, "usage": clip(command.Usage, 16<<10), "description_path": e.commands.DescriptionPath(command.Name)})
		}
	}
	capabilities := map[string]any{"tools": tools, "commands": commands}
	data, err := json.Marshal(capabilities)
	if err != nil || len(data) > 24<<10 {
		return nil, errors.New("tool descriptions exceed observation budget")
	}
	// Pass plain JSON to expressions, never live Go objects or tool methods.
	if err = json.Unmarshal(data, &capabilities); err != nil {
		return nil, err
	}
	return capabilities, nil
}

func observeInput(state json.RawMessage, capabilities map[string]any) (map[string]any, error) {
	var projection struct {
		Messages []map[string]any `json:"messages"`
		Omitted  int              `json:"omitted_evidence"`
	}
	if err := json.Unmarshal(state, &projection); err != nil {
		return nil, err
	}
	// Bind only this user's interaction. Older user/system constraints remain
	// in the separate JEV context and cannot be overwritten by this projection.
	start := 0
	for i, message := range projection.Messages {
		if message["role"] == "user" && message["name"] == nil {
			start = i
		}
	}
	env := map[string]any{"user": ""}
	current := make([]map[string]any, 0, len(projection.Messages)-start)
	for _, message := range projection.Messages[start:] {
		// Named user messages are control receipts, not new user requirements.
		// Actual native evidence is retained as associated calls/results.
		if message["role"] != "user" || message["name"] == nil {
			current = append(current, message)
		}
	}
	env["messages"], env["omitted_evidence"] = current, projection.Omitted
	// Joining native calls and completed results is protocol normalization, not
	// a tool adapter. No generated code has to guess which tool produced text.
	calls := map[string]map[string]any{}
	for _, message := range current {
		if message["role"] == "user" {
			env["user"] = message["text"]
		}
		if items, ok := message["calls"].([]any); ok {
			for _, item := range items {
				call := item.(map[string]any)
				calls[call["id"].(string)] = call
			}
		}
	}
	history := []map[string]any{}
	for _, message := range current {
		id, ok := message["call_id"].(string)
		if !ok || calls[id] == nil {
			continue
		}
		call := calls[id]
		text, _ := message["text"].(string)
		data := resultJSON(text)
		normalized := text
		if data != nil {
			encoded, err := json.Marshal(data)
			if err != nil {
				return nil, err
			}
			normalized = string(encoded)
		}
		history = append(history, map[string]any{"name": call["name"], "arguments": call["arguments"], "text": normalized, "data": data, "is_error": message["is_error"], "terminate": message["terminate"]})
	}
	env["history"] = history
	env["tools"], env["commands"] = capabilities["tools"], capabilities["commands"]
	return env, nil
}

func (r *Reflex) observe(ctx context.Context, state json.RawMessage, capabilities map[string]any) (json.RawMessage, map[string]binding, error) {
	if r.program == nil {
		return nil, nil, errors.New("scene has no compiled observation")
	}
	input, err := observeInput(state, capabilities)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	var output any
	// A runtime-generated native reader may already return the ordinary
	// observation protocol. Preserve its actual alternatives instead of
	// asking each generated program to implement the consumer again.
	history := input["history"].([]map[string]any)
	if len(history) > 0 {
		latest := history[len(history)-1]
		if latest["is_error"] != true {
			if data, ok := nativeObservation(latest["data"]); ok {
				output = data
			}
		}
	}
	if output == nil {
		output, err = runObserveJS(ctx, r.program, input)
	}
	if err != nil {
		return nil, nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, nil, err
	}
	if normalized, ok := nativeObservation(output); ok {
		output = normalized
	}
	data, err := json.Marshal(output)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid observation output: %w", err)
	}
	if len(data) > 32<<10 {
		return nil, nil, errors.New("observation output exceeds budget")
	}
	var observed struct {
		State      json.RawMessage    `json:"state"`
		Candidates map[string]binding `json:"candidates"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&observed); err != nil {
		return nil, nil, err
	}
	if len(observed.State) == 0 || string(observed.State) == "null" || observed.Candidates == nil || len(observed.Candidates) > maxCandidates {
		return nil, nil, errors.New("invalid observation state or candidates")
	}
	var explicit struct {
		Candidates map[string]struct{ Read *bool } `json:"candidates"`
	}
	if err := json.Unmarshal(data, &explicit); err != nil {
		return nil, nil, err
	}
	for id, candidate := range explicit.Candidates {
		if candidate.Read == nil {
			return nil, nil, fmt.Errorf("binding %q requires an explicit boolean read flag", id)
		}
	}
	for id, candidate := range observed.Candidates {
		var arguments map[string]any
		if strings.TrimSpace(id) == "" || len(id) > 128 || id == Defer || id == report || strings.TrimSpace(candidate.Name) == "" || len(candidate.Arguments) > 16<<10 || json.Unmarshal(candidate.Arguments, &arguments) != nil || arguments == nil {
			return nil, nil, fmt.Errorf("invalid observation binding %q", id)
		}
		known := false
		for _, tool := range capabilities["tools"].([]any) {
			if tool.(map[string]any)["name"] == candidate.Name {
				known = true
				break
			}
		}
		if !known {
			return nil, nil, fmt.Errorf("unknown native tool %q", candidate.Name)
		}
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, nil, errors.New("extra observation output")
	}
	return observed.State, observed.Candidates, nil
}

// Native readers may enumerate bindings as an array or an identified map.
// Recognize only complete native binding objects, so ordinary business results
// with similarly named fields still go through their generated Observe code.
func nativeObservation(value any) (map[string]any, bool) {
	data, ok := value.(map[string]any)
	if !ok || len(data) != 2 || data["state"] == nil {
		return nil, false
	}
	valid := func(value any) bool {
		item, ok := value.(map[string]any)
		if !ok {
			return false
		}
		_, name := item["name"].(string)
		_, arguments := item["arguments"].(map[string]any)
		_, read := item["read"].(bool)
		return name && arguments && read
	}
	bindings := map[string]any{}
	switch candidates := data["candidates"].(type) {
	case []any:
		if len(candidates) > maxCandidates {
			return nil, false
		}
		for i, candidate := range candidates {
			if !valid(candidate) {
				return nil, false
			}
			bindings["c"+strconv.Itoa(i)] = candidate
		}
	case map[string]any:
		for id, candidate := range candidates {
			if !valid(candidate) {
				return nil, false
			}
			bindings[id] = candidate
		}
	default:
		return nil, false
	}
	return map[string]any{"state": data["state"], "candidates": bindings}, true
}

// Decode a JSON result, including an otherwise arbitrary textual envelope.
// Only a complete JSON suffix is accepted; absence is nil, not invented facts.
func resultJSON(text string) any {
	var data any
	parse := func(text string, output *any) bool {
		decoder := json.NewDecoder(strings.NewReader(text))
		decoder.UseNumber()
		if decoder.Decode(output) != nil {
			return false
		}
		var extra any
		return decoder.Decode(&extra) == io.EOF
	}
	decode := func() any {
		// A JSON-producing ordinary operation can return a JSON string whose
		// contents are another JSON object/array. Preserve original text as well.
		for depth := 0; depth < 2; depth++ {
			value, ok := data.(string)
			if !ok {
				break
			}
			value = strings.TrimSpace(value)
			if len(value) == 0 || (value[0] != '{' && value[0] != '[') {
				break
			}
			var nested any
			if !parse(value, &nested) {
				break
			}
			data = nested
		}
		return data
	}
	if parse(text, &data) {
		return decode()
	}
	for _, line := range strings.SplitAfter(text, "\n") {
		text = strings.TrimPrefix(text, line)
		trimmed := strings.TrimSpace(text)
		if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[' || trimmed[0] == '"') && parse(trimmed, &data) {
			return decode()
		}
	}
	return nil
}

// Prefer the actual structured result over an arbitrary envelope/program echo.
// Raw native output remains unchanged in the evidence log and private history.
func resultSummary(text string) string {
	if data := resultJSON(text); data != nil {
		if encoded, err := json.Marshal(data); err == nil {
			return clip(string(encoded), 2048)
		}
	}
	return clip(text, 2048)
}
