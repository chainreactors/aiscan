package jev

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/chainreactors/cyber/agent"
	aop "github.com/chainreactors/cyber/aop"
	coretool "github.com/chainreactors/cyber/core/tool"
	"mvdan.cc/sh/v3/syntax"
)

// A binding is a native Executor call emitted by the generated function.
type NativeCall coretool.NativeCall

type binding = NativeCall

func (b binding) call() *aop.ToolCall {
	return &aop.ToolCall{Name: b.Name, Arguments: &aop.EncodedValue{Data: b.Arguments, MediaType: aop.JSONMediaType}}
}

func (b binding) canonical() string { return canonical(b.call()) }

// Shell quoting is transport encoding. Compare decoded literal argv when both
// calls are a single expansion-free command, retaining every other argument.
// Compound scripts and opaque tools still require exact recorded arguments.
func (b binding) replayKey() string {
	prepared, err := prepareBinding(b)
	if err != nil || len(prepared.Argv) == 0 {
		return b.canonical()
	}
	var args map[string]any
	if json.Unmarshal(prepared.Arguments, &args) != nil {
		return b.canonical()
	}
	args["command"] = prepared.Argv
	return jsonText([]any{b.Name, args})
}

// capabilities is the existing tool surface, not a registry of JEV adapters.
func (e *Extension) capabilities(cfg agent.Config, states ...json.RawMessage) (map[string]any, error) {
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
			commands = append(commands, map[string]any{"name": command.Name, "usage": clip(command.Usage, 16<<10), "contract_hash": digest(command.Usage), "description_path": e.commands.DescriptionPath(command.Name)})
		}
	}
	capabilities := map[string]any{"tools": tools, "commands": commands, "native_contracts": e.contracts.Catalog()}
	data, err := json.Marshal(capabilities)
	if err == nil && len(data) > 32<<10 {
		// Full installations carry large scanner manuals unrelated to this
		// interaction. Keep exact native schemas and documentation for observed
		// commands; retain discoverable catalog entries for every other command.
		used := interactionCommands(states)
		for _, command := range commands {
			if used[command["name"].(string)] {
				continue
			}
			usage := command["usage"].(string)
			if first, _, found := strings.Cut(usage, "\n"); found {
				command["usage"], command["usage_complete"] = first, false
			}
		}
		data, err = json.Marshal(capabilities)
	}
	if err != nil || len(data) > 32<<10 {
		return nil, errors.New("tool descriptions exceed observation budget")
	}
	// Pass plain JSON to expressions, never live Go objects or tool methods.
	if err = json.Unmarshal(data, &capabilities); err != nil {
		return nil, err
	}
	return capabilities, nil
}

// Semantic authorization needs the proposed operation's protocol, not every
// installed tool's manual or the host's deterministic verification catalog.
// Keep current constraints and evidence intact while avoiding catalog overflow.
func bindingCapabilities(candidate binding, capabilities map[string]any) map[string]any {
	tools, commands := []any{}, []any{}
	definitions, _ := capabilities["tools"].([]any)
	for _, value := range definitions {
		if tool, ok := value.(map[string]any); ok && tool["name"] == candidate.Name {
			tools = append(tools, tool)
		}
	}
	if prepared, err := prepareBinding(candidate); err == nil && len(prepared.Argv) > 0 {
		if catalog, ok := capabilities["commands"].([]any); ok {
			for _, value := range catalog {
				if command, ok := value.(map[string]any); ok && command["name"] == prepared.Argv[0] {
					commands = append(commands, command)
				}
			}
		}
	}
	return map[string]any{"tools": tools, "commands": commands}
}

// Parse recorded native shell calls without evaluating expansions or executing
// user content. Documentation selection does not authorize tool dispatch.
func interactionCommands(states []json.RawMessage) map[string]bool {
	used := map[string]bool{}
	for _, state := range states {
		var input struct {
			Messages []struct {
				Calls []struct {
					Name      string `json:"name"`
					Arguments struct {
						Command string `json:"command"`
					} `json:"arguments"`
				} `json:"calls"`
			} `json:"messages"`
		}
		if json.Unmarshal(state, &input) != nil {
			continue
		}
		for _, message := range input.Messages {
			for _, call := range message.Calls {
				if call.Name != "bash" {
					continue
				}
				file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(call.Arguments.Command), "")
				if err != nil {
					continue
				}
				syntax.Walk(file, func(node syntax.Node) bool {
					if command, ok := node.(*syntax.CallExpr); ok && len(command.Args) > 0 {
						if name := command.Args[0].Lit(); name != "" {
							used[name] = true
						}
					}
					return true
				})
			}
		}
	}
	return used
}

func observeInput(state json.RawMessage, capabilities map[string]any) (map[string]any, error) {
	var projection struct {
		Messages []map[string]any `json:"messages"`
		Omitted  int              `json:"omitted_evidence"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(state)))
	decoder.UseNumber()
	if err := decoder.Decode(&projection); err != nil {
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
			if message["call_id"] != nil && message["data"] != nil && message["text"] == nil {
				// Keep the JS history/messages ABI while serializing native JSON
				// only once in the bounded host evidence projection.
				message["text"] = jsonText(message["data"])
			}
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
		normalized, data := normalizedResult(text)
		row := map[string]any{"call_id": id, "name": call["name"], "arguments": call["arguments"], "text": normalized, "data": data, "is_error": message["is_error"], "terminate": message["terminate"]}
		if native, err := prepareBinding(NativeCall{Name: fmt.Sprint(call["name"]), Arguments: json.RawMessage(jsonText(call["arguments"]))}); err == nil && len(native.Argv) > 0 {
			row["decoded_argv"] = native.Argv
		}
		history = append(history, row)
	}
	env["history"] = history
	env["tools"], env["commands"] = capabilities["tools"], capabilities["commands"]
	return env, nil
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
	text, _ = normalizedResult(text)
	return clip(text, 2048)
}

func normalizedResult(text string) (string, any) {
	if data := resultJSON(text); data != nil {
		if encoded, err := json.Marshal(data); err == nil {
			return string(encoded), data
		}
	}
	return text, nil
}
