//go:build full

package jev

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	coretool "github.com/chainreactors/cyber/core/tool"
	browserext "github.com/chainreactors/cyber/exts/browser"
	"github.com/chainreactors/cyber/pkg/testutil/hosttest"
	"github.com/go-rod/rod/lib/launcher"
)

// Generalization is measured by changing workflow shape, not only renaming a
// wizard's next button. These fixtures and their oracles never enter production.
func (f *liveFixture) generalBrowserTask(index int) (string, func(string) bool) {
	target := []string{"Cancel", "Open workspace", "Return to list", "Show record"}[(index/5)%4]
	area := []string{"North", "South"}[index%2]
	goal := fmt.Sprintf("Select %s.", target)
	switch index % 5 {
	case 1:
		goal = fmt.Sprintf("Filter the catalog to area %s and open item %d from the filtered results.", area, index)
	case 2:
		value := fmt.Sprintf("code-%d", index)
		if index%10 == 7 {
			value = strconv.Quote(value)
		}
		goal = fmt.Sprintf("Create a draft with Code %s and Area %s, then save it.", value, area)
	case 3:
		goal = fmt.Sprintf("Open the tool picker and select item %d once its entries appear.", index)
	case 4:
		goal = fmt.Sprintf("Select item %d in the embedded workspace.", index)
	}
	prompt := fmt.Sprintf("Use the browser UI at %s/general/%d/ . %s Inspect the resulting page and report its displayed confirmation code.", f.server.URL, index, goal)
	return prompt, func(output string) bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		steps := 1
		if index%5 == 3 {
			steps = 2
		}
		marker := "confirmed-" + digest([]any{f.server.URL, index})[:16]
		return f.step == steps && f.wrong == 0 && strings.Contains(output, marker)
	}
}

func (f *liveFixture) serveGeneralBrowser(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/general/"), "/")
	index, err := strconv.Atoi(parts[0])
	if err != nil || index != f.index || len(parts) != 2 {
		f.wrong++
		w.WriteHeader(http.StatusNotFound)
		return
	}
	area := []string{"North", "South"}[index%2]
	target := fmt.Sprintf("item %d", index)
	if index%5 == 0 {
		target = []string{"Cancel", "Open workspace", "Return to list", "Show record"}[(index/5)%4]
	}
	if parts[1] == "act" {
		q := r.URL.Query()
		valid := r.Method == http.MethodPost && q.Get("target") == target && f.step == 0
		switch index % 5 {
		case 1:
			valid = valid && q.Get("area") == area
		case 2:
			valid = valid && q.Get("area") == area && q.Get("code") == fmt.Sprintf("code-%d", index)
		case 3:
			if q.Get("stage") == "open" && f.step == 0 && r.Method == http.MethodPost {
				f.step++
				fmt.Fprint(w, `{"opened":true}`)
				return
			}
			valid = r.Method == http.MethodPost && q.Get("target") == target && f.step == 1
		}
		if !valid {
			f.wrong++
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, "Wrong target or unsatisfied task prerequisites")
			return
		}
		f.step++
		marker := "confirmed-" + digest([]any{f.server.URL, index})[:16]
		fmt.Fprintf(w, `{"confirmation":%q}`, marker)
		return
	}
	if parts[1] != "" && (parts[1] != "frame" || index%5 != 4) {
		f.wrong++
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if index%5 == 4 && parts[1] == "" {
		fmt.Fprintf(w, `<!doctype html><title>Embedded workspace</title><h1>Workspace</h1><p>The available records are in the embedded workspace.</p><main><iframe title="Embedded workspace" src="frame" style="width:650px;height:300px"></iframe></main>`)
		return
	}
	fmt.Fprintf(w, `<!doctype html><title>Workspace %d</title><h1>Workspace</h1><main></main><script>
const index=%d, kind=index%%5, target=%q, wantedArea=%q, endpoint='/general/'+index+'/act';
const root=document.querySelector('main');
const identify=e=>{if(index%%3!==1)e.id='node-'+crypto.randomUUID();return e};
function control(label,fn){let e=document.createElement(index%%3===0?'button':index%%3===1?'a':'div');if(e.tagName==='A')e.href='#';if(e.tagName==='DIV')e.setAttribute('role','button');e.textContent=label;identify(e);e.onclick=event=>{event.preventDefault();fn()};return e}
function choices(parent,fn){const desired=control(target,fn),other=control(target==='Cancel'?'Continue':'Cancel',()=>act({target:'wrong'}));parent.append(...(index%%2?[other,desired]:[desired,other]))}
async function act(args){const res=await fetch(endpoint+'?'+new URLSearchParams(args),{method:'POST'});if(!res.ok){root.append('Action rejected');return}const data=await res.json();if(data.confirmation){const doc=kind===4?window.parent.document:document;doc.querySelector('main').innerHTML='<h2>Completed</h2><output>'+data.confirmation+'</output>'}return data}
function areaSelect(parent,change){const label=document.createElement('label'),select=identify(document.createElement('select'));label.textContent='Area';for(const text of ['Choose an area','North','South']){let o=document.createElement('option');o.textContent=text;o.value=text==='Choose an area'?'':text;select.append(o)}label.append(select);parent.append(label);if(change)select.onchange=()=>change(select.value);return select}
if(kind===0||kind===4){choices(root,()=>act({target}))}
if(kind===1){const results=document.createElement('section');areaSelect(root,area=>{results.replaceChildren();if(area===wantedArea)choices(results,()=>act({target,area}));else results.textContent='No matching records in this area'});root.append(results)}
if(kind===2){let label=document.createElement('label'),input=identify(document.createElement('input'));label.textContent='Code';label.append(input);root.append(label);let select=areaSelect(root);root.append(control('Save draft',()=>act({target,code:input.value,area:select.value})),control('Discard',()=>act({target:'wrong'})))}
if(kind===3){root.append(control('Open tool picker',async()=>{let data=await act({stage:'open'});if(!data)return;root.textContent='Loading tool entries';setTimeout(()=>{root.replaceChildren();let dialog=document.createElement('section');dialog.setAttribute('role','dialog');dialog.append('Tool picker');choices(dialog,()=>act({target}));root.append(dialog)},150+(index%%4)*90)}))}
</script>`, index, index, target, area)
}

func TestGeneralBrowserFixtureRejectsWrongOrUnexecutedOutcomes(t *testing.T) {
	f := newLiveFixture(t)
	for index := 0; index < 5; index++ {
		_, oracle := f.task("playwright-general", index)
		marker := "confirmed-" + digest([]any{f.server.URL, index})[:16]
		if oracle(marker) {
			t.Fatal("reported marker without action was accepted")
		}
		q := url.Values{"target": {fmt.Sprintf("item %d", index)}, "area": {[]string{"North", "South"}[index%2]}, "code": {fmt.Sprintf("code-%d", index)}}
		if index == 0 {
			q.Set("target", "Cancel")
		}
		post := func(values url.Values) int {
			t.Helper()
			resp, err := http.Post(fmt.Sprintf("%s/general/%d/act?%s", f.server.URL, index, values.Encode()), "text/plain", nil)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			return resp.StatusCode
		}
		if post(url.Values{"target": {"wrong"}}) != http.StatusConflict {
			t.Fatal("wrong action was accepted")
		}
		_, oracle = f.task("playwright-general", index)
		if index == 3 && post(url.Values{"stage": {"open"}}) != http.StatusOK {
			t.Fatal("picker did not open")
		}
		if post(q) != http.StatusOK || !oracle(marker) || oracle("invented") {
			t.Fatalf("invalid correctness oracle for variant %d", index)
		}
	}
}

// Validate the benchmark pages independently of either inference provider.
// Fixture-driving scripts below are test oracles, never Reflex candidates.
func TestGeneralBrowserFixtureDOM(t *testing.T) {
	if _, ok := launcher.LookPath(); !ok {
		t.Skip("local Chromium unavailable")
	}
	f := newLiveFixture(t)
	browser, err := browserext.New(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	commands := coretool.NewCommandRegistry()
	hosttest.Load(t, t.Context(), hosttest.Capabilities(), commands, browser)
	run := func(args ...string) string {
		t.Helper()
		var out bytes.Buffer
		if _, err := commands.Execute(t.Context(), "playwright", &coretool.Execution{Args: args, Stdout: &out, Stderr: &out}); err != nil {
			t.Fatalf("%s: %v %s", args[0], err, out.String())
		}
		return out.String()
	}
	for index := 0; index < 5; index++ {
		_, oracle := f.task("playwright-general", index)
		run("open", fmt.Sprintf("%s/general/%d/", f.server.URL, index), "--session", "fixture")
		script := `document.querySelector('button,a,[role=button]').click()`
		switch index {
		case 1:
			script = `(()=>{const s=document.querySelector('select');s.value='South';s.dispatchEvent(new Event('change'));[...document.querySelectorAll('button,a,[role=button]')].find(e=>e.textContent==='item 1').click()})()`
		case 2:
			script = `(()=>{document.querySelector('input').value='code-2';document.querySelector('select').value='North';[...document.querySelectorAll('button,a,[role=button]')].find(e=>e.textContent==='Save draft').click()})()`
		case 4:
			script = `document.querySelector('iframe').contentDocument.querySelector('button,a,[role=button]').click()`
		}
		run("evaluate", "fixture", script)
		run("wait-for", "fixture", "--stable")
		if index == 3 {
			run("evaluate", "fixture", `[...document.querySelectorAll('button,a,[role=button]')].find(e=>e.textContent==='item 3').click()`)
			run("wait-for", "fixture", "--stable")
		}
		if output := run("evaluate", "fixture", "document.body.innerText"); !oracle(output) {
			t.Fatalf("fixture %d did not complete: %s", index, output)
		}
		run("close", "fixture")
	}
}
