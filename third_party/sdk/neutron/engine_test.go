package neutron

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chainreactors/sdk/pkg/types"
	"gopkg.in/yaml.v3"
)

func TestConfigWithCapacity(t *testing.T) {
	config := NewConfig().WithCapacity(10)
	if config.Capacity != 10 {
		t.Fatalf("config.Capacity = %d, want 10", config.Capacity)
	}
}

func TestSetCapacityPostCreation(t *testing.T) {
	engine := &Engine{config: NewConfig()}
	if engine.Capacity() != nil {
		t.Fatal("engine should have no capacity by default")
	}
	engine.SetCapacity(5)
	if engine.Capacity() == nil {
		t.Fatal("engine should have a capacity after SetCapacity()")
	}
	if engine.Capacity().Total() != 5 {
		t.Fatalf("capacity total = %d, want 5", engine.Capacity().Total())
	}
}

func TestCapacityContextCancellation(t *testing.T) {
	dummyTemplate := &types.Template{Id: "test-capacity"}
	engine := &Engine{
		config:    NewConfig(),
		templates: []*types.Template{dummyTemplate},
	}
	engine.SetCapacity(1)

	// Exhaust capacity
	if err := engine.Capacity().Acquire(context.Background(), 1); err != nil {
		t.Fatal(err)
	}

	// Cancelled context should fail Acquire in executeTemplates
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	nctx := NewContext().WithContext(ctx)
	task := NewExecuteTask("http://127.0.0.1")

	_, err := engine.Execute(nctx, task)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}

	engine.Capacity().Release(1)
}

func TestNoCapacityByDefault(t *testing.T) {
	engine := &Engine{config: NewConfig()}
	if engine.Capacity() != nil {
		t.Fatal("engine should have no capacity by default")
	}
}

func TestCompileTemplatesIsolatesTemplateVariables(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("path=" + r.URL.Path))
	}))
	defer server.Close()

	first := parseTemplateForTest(t, `id: first-template
info:
  name: First Template
  severity: info
variables:
  token: first
http:
  - method: GET
    path:
      - "{{BaseURL}}/{{token}}"
    matchers:
      - type: word
        words:
          - "path=/first"
`)
	second := parseTemplateForTest(t, `id: second-template
info:
  name: Second Template
  severity: info
variables:
  token: second
http:
  - method: GET
    path:
      - "{{BaseURL}}/{{token}}"
    matchers:
      - type: word
        words:
          - "path=/second"
`)

	engine := &Engine{config: NewConfig()}
	compiled := engine.compileTemplates([]*types.Template{first, second})
	if len(compiled) != 2 {
		t.Fatalf("compiled templates = %d, want 2", len(compiled))
	}

	for _, tpl := range compiled {
		result, err := tpl.Execute(server.URL, nil)
		if err != nil {
			t.Fatalf("execute %s: %v", tpl.Id, err)
		}
		if result == nil || !result.Matched {
			t.Fatalf("expected %s to match its own variable-expanded path", tpl.Id)
		}
	}
}

func parseTemplateForTest(t *testing.T, raw string) *types.Template {
	t.Helper()
	var tpl types.Template
	if err := yaml.Unmarshal([]byte(raw), &tpl); err != nil {
		t.Fatalf("parse template: %v", err)
	}
	return &tpl
}
