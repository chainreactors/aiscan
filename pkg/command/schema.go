package command

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/invopop/jsonschema"
)

// SchemaOf generates a JSON Schema (as map[string]any) from a Go struct.
// Struct fields use standard tags:
//
//	json:"name"          → property name; omitempty marks the field as optional
//	jsonschema:"..."     → description, enum, etc. per invopop/jsonschema
//
// Fields without omitempty are automatically added to the "required" list.
func SchemaOf(proto any) map[string]any {
	r := &jsonschema.Reflector{
		DoNotReference: true,
	}
	schema := r.Reflect(proto)

	data, err := json.Marshal(schema)
	if err != nil {
		return map[string]any{"type": "object"}
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return map[string]any{"type": "object"}
	}

	delete(m, "$schema")
	delete(m, "$id")
	delete(m, "$defs")
	return m
}

// ToolDef builds a complete ToolDefinition from a name,
// description, and an args struct prototype.
func ToolDef(name, description string, argsProto any) ToolDefinition {
	return ToolDefinition{
		Type: "function",
		Function: FunctionDefinition{
			Name:        name,
			Description: description,
			Parameters:  SchemaOf(argsProto),
		},
	}
}

// ParseArgs unmarshals the raw JSON arguments string into a typed struct.
func ParseArgs[T any](arguments string) (T, error) {
	var args T
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return args, fmt.Errorf("invalid arguments: %w", err)
	}
	return args, nil
}

// ParseStrictArgs unmarshals JSON arguments and rejects unknown fields.
func ParseStrictArgs[T any](arguments string) (T, error) {
	var args T
	dec := json.NewDecoder(bytes.NewReader([]byte(arguments)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&args); err != nil {
		return args, fmt.Errorf("invalid arguments: %w", err)
	}
	var extra struct{}
	if err := dec.Decode(&extra); err != io.EOF {
		return args, fmt.Errorf("invalid arguments: trailing data")
	}
	return args, nil
}
