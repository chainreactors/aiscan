package commands

import (
	"context"
	"testing"
)

func TestCheckpointExecuteStoresResult(t *testing.T) {
	cp := NewCheckpointTool()
	if cp.Result() != nil {
		t.Fatal("Result() should be nil before Execute")
	}

	result, err := cp.Execute(context.Background(), `{
		"kind": "verify",
		"title": "SSH weak creds on 10.0.0.1:22",
		"content": "Evidence: ssh root@10.0.0.1 login success",
		"target": "10.0.0.1:22",
		"status": "confirmed",
		"labels": ["high"]
	}`)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.Terminate {
		t.Fatal("expected Terminate=true")
	}

	got := cp.Result()
	if got == nil {
		t.Fatal("Result() should not be nil after Execute")
	}
	if got.Kind != "verify" {
		t.Fatalf("Kind = %q, want verify", got.Kind)
	}
	if got.Target != "10.0.0.1:22" {
		t.Fatalf("Target = %q, want 10.0.0.1:22", got.Target)
	}
	if got.Status != "confirmed" {
		t.Fatalf("Status = %q, want confirmed", got.Status)
	}
	if got.Title != "SSH weak creds on 10.0.0.1:22" {
		t.Fatalf("Title = %q", got.Title)
	}
	if len(got.Labels) != 1 || got.Labels[0] != "high" {
		t.Fatalf("Labels = %v, want [high]", got.Labels)
	}
}

func TestCheckpointStatusNormalization(t *testing.T) {
	cp := NewCheckpointTool()
	_, err := cp.Execute(context.Background(), `{
		"kind": "verify",
		"title": "test",
		"content": "test",
		"status": "false_positive"
	}`)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if got := cp.Result().Status; got != "not_confirmed" {
		t.Fatalf("Status = %q, want not_confirmed (normalized from false_positive)", got)
	}
}

func TestCheckpointExecuteInvalidJSON(t *testing.T) {
	cp := NewCheckpointTool()
	_, err := cp.Execute(context.Background(), `{invalid`)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if cp.Result() != nil {
		t.Fatal("Result() should be nil after failed Execute")
	}
}

func TestCheckpointDefinitionHasCorrectName(t *testing.T) {
	cp := NewCheckpointTool()
	if cp.Name() != "checkpoint" {
		t.Fatalf("Name = %q, want checkpoint", cp.Name())
	}
	def := cp.Definition()
	if def.Function.Name != "checkpoint" {
		t.Fatalf("Definition name = %q", def.Function.Name)
	}
}

func TestCheckpointDefinitionAllowsDeepKind(t *testing.T) {
	def := NewCheckpointTool().Definition()
	props, ok := def.Function.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties = %T, want map[string]any", def.Function.Parameters["properties"])
	}
	kind, ok := props["kind"].(map[string]any)
	if !ok {
		t.Fatalf("kind property = %T, want map[string]any", props["kind"])
	}
	values, ok := kind["enum"].([]any)
	if !ok {
		t.Fatalf("kind enum = %T, want []any", kind["enum"])
	}
	for _, value := range values {
		if value == "deep" {
			return
		}
	}
	t.Fatalf("kind enum = %v, want deep", values)
}

func TestCheckpointTerminateResultContainsInfo(t *testing.T) {
	cp := NewCheckpointTool()
	result, _ := cp.Execute(context.Background(), `{
		"kind": "sniper",
		"title": "CVE-2021-44228 Log4Shell",
		"content": "Found Log4j vulnerability",
		"target": "10.0.0.5:8080",
		"status": "info"
	}`)
	text := result.Text()
	if text == "" {
		t.Fatal("TerminateResult text should not be empty")
	}
}

