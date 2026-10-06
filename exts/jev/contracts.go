package jev

import (
	"encoding/json"
)

func (e *Extension) contractsAvailable(r Reflex) bool {
	contracts := e.contracts.Snapshot()
	for _, step := range r.Steps {
		if _, ok := contracts[step.Contract]; !ok {
			return false
		}
	}
	return true
}

// Contracts describe executable dependencies, never task parameters or progress.
func nativeContracts(capabilities map[string]any) map[string]string {
	contracts := map[string]string{"helpers": digest([]string{observeHelpersJS})}
	for _, value := range capabilities["tools"].([]any) {
		tool := value.(map[string]any)
		contracts["tool:"+tool["name"].(string)] = digest(tool)
	}
	for _, value := range capabilities["commands"].([]any) {
		command := value.(map[string]any)
		checksum, _ := command["contract_hash"].(string)
		if checksum == "" {
			checksum = digest(command["usage"])
		}
		contracts["command:"+command["name"].(string)] = checksum
	}
	return contracts
}

func compatibleReflex(record reflexRecord, contracts map[string]string) bool {
	for key, expected := range record.Contracts {
		if contracts[key] != expected {
			return false
		}
	}
	return true
}

func reflexContracts(capabilities map[string]any, witnesses []map[string]any) map[string]string {
	catalog := nativeContracts(capabilities)
	selected := map[string]string{"helpers": catalog["helpers"]}
	var calls []any
	for _, witness := range witnesses {
		// Later effects may only be materialized by a native reader at runtime;
		// the recorded trajectory still proves their tool/command dependencies.
		if next, ok := witness["next_calls"].([]any); ok {
			for _, value := range next {
				if call, ok := value.(map[string]any); ok {
					if name, ok := call["name"].(string); ok && catalog["tool:"+name] != "" {
						selected["tool:"+name] = catalog["tool:"+name]
						calls = append(calls, call)
					}
				}
			}
		}
		for _, candidate := range witness["candidates"].(map[string]binding) {
			selected["tool:"+candidate.Name] = catalog["tool:"+candidate.Name]
			var arguments any
			_ = json.Unmarshal(candidate.Arguments, &arguments)
			calls = append(calls, map[string]any{"name": candidate.Name, "arguments": arguments})
		}
	}
	state, _ := json.Marshal(map[string]any{"messages": []any{map[string]any{"calls": calls}}})
	for command := range interactionCommands([]json.RawMessage{state}) {
		if value, exists := catalog["command:"+command]; exists {
			selected["command:"+command] = value
		}
	}
	return selected
}

// Send applicability and policy once; source is needed only by compilation.
func reflexCatalog(records map[string]reflexRecord) map[string]any {
	catalog := map[string]any{}
	for id, record := range records {
		catalog[id] = map[string]any{"when": record.When, "decide": record.Decide, "claims": record.Claims}
	}
	return catalog
}
