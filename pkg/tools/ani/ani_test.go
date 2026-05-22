package ani

import (
	"encoding/json"
	"testing"

	anigo "github.com/chainreactors/ani-go"
)

func TestPythonCompaniesShape(t *testing.T) {
	b, err := json.Marshal(pythonCompanies([]anigo.CompanyAsset{
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
		t.Fatalf("company without ICP should match Python traverse_all default and be omitted: %v", got)
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
