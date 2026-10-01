//go:build full

package jev

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestBrowserExpressionBindsRecordedInspection(t *testing.T) {
	r := Reflex{When: "Browser interaction", Decide: "Select current bindings", Observe: browserObserveExpression()}
	if err := r.validate(); err != nil {
		t.Fatal(err)
	}
	state := json.RawMessage(`{"messages":[{"role":"user","text":"Use http://localhost/page"},{"role":"assistant","calls":[{"id":"open","name":"bash","arguments":{"command":"playwright open http://localhost/page --session current"}}]},{"role":"assistant","calls":[{"id":"inspect","name":"bash","arguments":{"command":"playwright evaluate current script"}}]},{"role":"tool","call_id":"inspect","text":"Script: x\n---\n{\"url\":\"http://localhost/page\",\"text\":\"Archive\",\"elements\":[{\"label\":\"Archive\",\"selector\":\"#dynamic-id\",\"disabled\":false}],\"inputs\":[]}"}]}`)
	_, candidates, err := r.observe(t.Context(), state, map[string]any{"tools": []any{map[string]any{"name": "bash"}}, "commands": []any{}})
	if err != nil || len(candidates) != 1 {
		t.Fatalf("candidates=%v error=%v", candidates, err)
	}
	for _, candidate := range candidates {
		if !strings.Contains(string(candidate.Arguments), "#dynamic-id") {
			t.Fatalf("selector was not bound from inspection: %s", candidate.Arguments)
		}
	}
}

// A fake compiler supplies this expression as runtime data. Production has no
// browser parser, DOM adapter or built-in candidate generator.
func browserObserveExpression() string {
	const inspect = `(() => {
const selector = e => {
  if (e.id) return '#' + CSS.escape(e.id);
  let parts = [];
  while (e && e.nodeType === 1) {
    const tag = e.tagName.toLowerCase();
	if (!e.parentElement) { parts.unshift(tag); break; }
    const peers = Array.from(e.parentElement?.children || []).filter(p => p.tagName === e.tagName);
    parts.unshift(tag + ':nth-of-type(' + (peers.indexOf(e) + 1) + ')');
    e = e.parentElement;
  }
  return parts.join(' > ');
};
return {
  url: location.href, text: document.body.innerText,
  elements: Array.from(document.querySelectorAll('button,a,[role="button"]')).map(e => ({label:e.innerText, selector:selector(e), disabled:!!e.disabled})),
  inputs: Array.from(document.querySelectorAll('input,textarea,select')).map(e => ({selector:selector(e), value:e.value, required:e.required, invalid:!e.validity.valid, disabled:e.disabled, readonly:e.readOnly}))
};
})()`
	return `js:(() => {
const urls = user.match(/https?:\/\/[^ ]+/g) || [];
const calls = messages.flatMap(message => message.calls || []);
const opened = calls.filter(call => call.name === "bash" && (call.arguments.command || "").startsWith("playwright open "));
const sessions = opened.length ? opened[opened.length - 1].arguments.command.match(/--session ([^ ]+)/) : null;
const session = sessions ? sessions[1] : "jev-browser";
const recent = history.length ? history[history.length - 1] : null;
const page = recent && (recent.arguments.command || "").startsWith("playwright evaluate ") ? recent.data : null;
return {state: page || {opened: opened.length > 0, needs_read: opened.length > 0},
 candidates: opened.length === 0 ? (urls.length === 0 ? {} : {open: bind("bash", {command:"playwright open " + quote(urls[0]) + " --session jev-browser"}, false)}) :
 page === null ? {inspect: bind("bash", {command:"playwright evaluate " + quote(session) + " " + quote(` + strconv.Quote(inspect) + `)}, true)} :
 Object.fromEntries(page.elements.filter(element => !element.disabled).map((element, i) => ["click-" + i, bind("bash", {command:"playwright click " + quote(session) + " " + quote(element.selector)}, false)]))};
})()`
}
