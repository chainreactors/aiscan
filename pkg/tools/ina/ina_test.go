package ina

import (
	"encoding/json"
	"testing"

	inago "github.com/chainreactors/ina-go"
)

func TestPythonAssetsFofaShape(t *testing.T) {
	b, err := json.Marshal(pythonAssets("fofa", []inago.Asset{{
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

func TestPythonAssetsHunterShape(t *testing.T) {
	b, err := json.Marshal(pythonAssets("hunter", []inago.Asset{{
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
	if _, ok := got[0]["source"]; ok {
		t.Fatalf("source should not be in Python shape: %v", got[0])
	}
}
