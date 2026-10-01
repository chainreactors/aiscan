package jev

import (
	"context"
	"encoding/json"

	"github.com/dop251/goja"
	"mvdan.cc/sh/v3/syntax"
)

// The same pure JSON helpers can be serialized into an ordinary programmable
// read. They contain no tool names, resource access, or execution capability.
const observeHelpersJS = `
function bind(name, arguments, read) {
  if (typeof read !== "boolean") throw new Error("bind requires an explicit boolean read flag: true only for effect-free inspection/polling, false for effects");
  return {name:name, arguments:arguments, read:read};
}
function choices(items) {
  if (!Array.isArray(items) || items.length > 64) throw new Error("invalid finite choices");
  const out = {};
  items.forEach((item, i) => { if (item != null) out["c" + i] = item; });
  return out;
}
function quote(value) {
  if (typeof value !== "string" || value.indexOf("\u0000") !== -1) throw new Error("invalid argument string");
  if (/^[A-Za-z0-9_./:,-]+$/.test(value)) return value;
  return "$'" + value.replace(/\\/g,"\\\\").replace(/'/g,"\\'").replace(/[\x01-\x1f\x7f]/g,
    function(c) { return "\\x" + ("0" + c.charCodeAt(0).toString(16)).slice(-2); }) + "'";
}
`

// Each evaluation owns a new pure-data runtime. Nothing connects this program
// to a browser, filesystem, network, executor, timer, or persistent VM state.
func runObserveJS(ctx context.Context, program *goja.Program, env map[string]any) (any, error) {
	data := map[string]any{}
	for _, name := range []string{"messages", "history", "user", "tools", "commands", "omitted_evidence"} {
		data[name] = env[name]
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	runtime := goja.New()
	stop := context.AfterFunc(ctx, func() { runtime.Interrupt(ctx.Err()) })
	defer stop()
	if err = runtime.Set("encoded", string(encoded)); err != nil {
		return nil, err
	}
	if err = runtime.Set("reader_helpers", observeHelpersJS); err != nil {
		return nil, err
	}
	// Standard JSON objects avoid exporting reflection-visible Go objects.
	_, err = runtime.RunString(`
const input = JSON.parse(encoded);
const user = input.user, history = input.history, messages = input.messages;
const tools = input.tools, commands = input.commands, omitted_evidence = input.omitted_evidence;
` + observeHelpersJS + `
function program(reader, args) {
  if (typeof reader !== "function" || !Array.isArray(args)) throw new Error("program requires a function and JSON argument array");
  return "(function(){" + reader_helpers + "return (" + reader.toString() + ").apply(null," + JSON.stringify(args) + ");})()";
}
Math.random = function() { throw new Error("observation cannot use randomness"); };
globalThis.Date = undefined;
`)
	if err != nil {
		return nil, err
	}
	if err = runtime.Set("quote", func(value string) (string, error) { return syntax.Quote(value, syntax.LangBash) }); err != nil {
		return nil, err
	}
	value, err := runtime.RunProgram(program)
	if err != nil {
		return nil, err
	}
	// A pure function expression is also a valid program entry. Invoke it
	// once in this same data-only runtime and under the same interruption.
	if entry, ok := goja.AssertFunction(value); ok {
		value, err = entry(goja.Undefined())
		if err != nil {
			return nil, err
		}
	}
	return value.Export(), ctx.Err()
}
