package passive

import (
	"encoding/json"
	"testing"

	anigo "github.com/chainreactors/ani-go"
	inago "github.com/chainreactors/ina-go"
)

func TestAniPythonShape(t *testing.T) {
	b, err := json.Marshal(aniPython([]anigo.CompanyAsset{
		{
			Name: "RootCo", PID: "aqc-1", ICP: "ICP1", Domain: "root.example",
			Title: "Root", Percent: 1, Source: "aqc_unauth",
		},
		{
			Name: "NoICP", PID: "aqc-2", Parent: "RootCo", Percent: 0.8,
			Source: "aqc_unauth",
		},
		{
			Name: "ChildCo", PID: "tyc-1", ICP: "ICP2", Domain: "child.example",
			Title: "Child", Parent: "RootCo", Percent: 0.6, Source: "tyc_unauth",
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["NoICP"]; ok {
		t.Fatalf("company without ICP should be omitted: %v", got)
	}
	root := got["RootCo"]
	if root["name"] != "RootCo" || root["perc"] != float64(1) || root["aqcid"] != "aqc-1" {
		t.Fatalf("root fields mismatch: %v", root)
	}
	if root["parent"] != nil {
		t.Fatalf("root parent = %v, want nil", root["parent"])
	}
	for _, forbidden := range []string{"pid", "source", "percent", "depth"} {
		if _, ok := root[forbidden]; ok {
			t.Fatalf("unexpected Go-only key %q in %v", forbidden, root)
		}
	}
	icps, ok := root["icps"].([]any)
	if !ok || len(icps) != 1 {
		t.Fatalf("root icps = %#v", root["icps"])
	}
	child := got["ChildCo"]
	if child["tycid"] != "tyc-1" || child["parent"] != "RootCo" {
		t.Fatalf("child fields mismatch: %v", child)
	}
}

func TestInaPythonFofaShape(t *testing.T) {
	b, err := json.Marshal(inaPython("fofa", []inago.Asset{{
		IP: "1.2.3.4", Port: "443", URL: "https://example.com",
		Domain: "example.com", Title: "Example", ICP: "ICP1", Source: "fofa",
	}}))
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	for _, key := range []string{"ip", "port", "url", "domain", "title", "icp"} {
		if _, ok := got[0][key]; !ok {
			t.Fatalf("missing key %q in %v", key, got[0])
		}
	}
	if _, ok := got[0]["source"]; ok {
		t.Fatalf("source should not be in Python shape: %v", got[0])
	}
}

func TestInaPythonHunterShape(t *testing.T) {
	b, err := json.Marshal(inaPython("hunter", []inago.Asset{{
		IP: "1.2.3.4", Port: "443", URL: "http://example.com:443",
		Domain: "example.com", Status: "200", Company: "Example Inc",
		Frame: "nginx, spring", Title: "Example", ICP: "ICP1", Source: "hunter",
	}}))
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	for _, key := range []string{"ip", "port", "url", "domain", "status", "company", "frame", "title", "icp"} {
		if _, ok := got[0][key]; !ok {
			t.Fatalf("missing key %q in %v", key, got[0])
		}
	}
	if got[0]["frame"] != "nginx, spring" {
		t.Fatalf("frame = %v", got[0]["frame"])
	}
}

func TestSplitSource(t *testing.T) {
	src, rest, help, err := splitSource([]string{"-s", "fofa", "domain=\"x.com\""})
	if err != nil || help || src != "fofa" || len(rest) != 1 || rest[0] != `domain="x.com"` {
		t.Fatalf("src=%q rest=%v help=%v err=%v", src, rest, help, err)
	}

	_, _, help, _ = splitSource([]string{"-h"})
	if !help {
		t.Fatal("expected help")
	}

	_, _, _, err = splitSource([]string{"-n", "foo"})
	if err == nil {
		t.Fatal("expected error when -s missing")
	}
}

func TestParseAniArgs(t *testing.T) {
	name, depth, pct, dSet, pSet, err := parseAniArgs([]string{"-n", "默安科技", "-d", "2", "-p", "0.6"})
	if err != nil {
		t.Fatal(err)
	}
	if name != "默安科技" || !dSet || depth != 2 || !pSet || pct != 0.6 {
		t.Fatalf("got name=%q d=%d(%v) p=%f(%v)", name, depth, dSet, pct, pSet)
	}

	_, _, _, _, _, err = parseAniArgs([]string{})
	if err == nil {
		t.Fatal("expected error for missing -n")
	}
}

func TestParseInaArgs(t *testing.T) {
	q, err := parseInaArgs([]string{`domain="example.com"`})
	if err != nil || q != `domain="example.com"` {
		t.Fatalf("q=%q err=%v", q, err)
	}

	_, err = parseInaArgs([]string{})
	if err == nil {
		t.Fatal("expected error for missing query")
	}

	_, err = parseInaArgs([]string{"a", "b"})
	if err == nil {
		t.Fatal("expected error for multiple positional args")
	}
}
