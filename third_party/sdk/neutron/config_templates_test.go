package neutron

import (
	"context"
	"testing"

	"github.com/chainreactors/sdk/pkg/cyberhub"
	"github.com/chainreactors/sdk/pkg/types"
)

func TestConfigSourcesAndTemplateFiltering(t *testing.T) {
	tpls := []*types.Template{
		{Id: "low-id", Info: types.TemplateInfo{Name: "Low Template", Severity: "low", Tags: "info"}},
		{Id: "critical-id", Info: types.TemplateInfo{Name: "Critical Template", Severity: "critical", Tags: "rce,cve"}},
		{Id: "critical-id", Info: types.TemplateInfo{Name: "Critical Template", Severity: "critical", Tags: "duplicate"}},
	}

	cfg := NewConfig().
		WithTemplates(tpls).
		WithFilter(func(tpl *types.Template) bool {
			return tpl.Info.Severity == "critical"
		})

	if cfg.Templates.Len() != 1 {
		t.Fatalf("filtered templates len = %d, want 1", cfg.Templates.Len())
	}
	if got := cfg.Templates.Templates()[0].Info.Name; got != "Critical Template" {
		t.Fatalf("expected critical template, got %q", got)
	}

	cfg.WithProvider(cyberhub.NewProvider("https://cyberhub.test", "key"))
	if len(cfg.Providers) == 0 {
		t.Fatalf("Provider assignment did not work: %+v", cfg)
	}
}

func TestTemplatesAppendMergeAndFilter(t *testing.T) {
	first := &types.Template{Id: "first", Info: types.TemplateInfo{Severity: "low"}}
	second := &types.Template{Id: "second", Info: types.TemplateInfo{Name: "second-name", Severity: "high"}}

	collection := (Templates{}).Append(first).Append(nil).Merge([]*types.Template{second})
	if collection.Len() != 2 {
		t.Fatalf("collection len = %d, want 2", collection.Len())
	}
	if collection.Items["first"] != first {
		t.Fatalf("template should be keyed by id when name is empty: %+v", collection.Items)
	}
	if collection.Items["second-name"] != second {
		t.Fatalf("template should prefer info.name key: %+v", collection.Items)
	}

	filtered := collection.Filter(func(tpl *types.Template) bool {
		return tpl.Info.Severity == "high"
	})
	if filtered.Len() != 1 || filtered.Items["second-name"] != second {
		t.Fatalf("unexpected filtered templates: %+v", filtered.Items)
	}
}

func TestExecuteTaskAndResultHelpers(t *testing.T) {
	if err := NewExecuteTask("").Validate(); err == nil {
		t.Fatal("expected empty target to fail validation")
	}
	task := NewExecuteTask("http://127.0.0.1")
	task.Templates = []*types.Template{}
	if err := task.Validate(); err == nil {
		t.Fatal("expected explicit empty templates to fail validation")
	}

	matched := &ExecuteResult{
		success:  true,
		template: &types.Template{Id: "demo"},
		data:     &NeutronResult{Result: &types.OperatorResult{Matched: true}},
	}
	if !matched.Success() || !matched.Matched() || matched.Template().Id != "demo" || matched.Data() != matched.Result() {
		t.Fatalf("unexpected matched result helpers: %+v", matched)
	}
}

func TestExecuteEmptyEngineReturnsClosedChannel(t *testing.T) {
	eng := &Engine{config: NewConfig()}
	ch, err := eng.Execute(NewContext().WithContext(context.Background()), NewExecuteTask("http://127.0.0.1"))
	if err != nil {
		t.Fatal(err)
	}
	if result, ok := <-ch; ok {
		t.Fatalf("expected closed channel, got result: %+v", result)
	}
}

func TestExecuteRejectsUnsupportedContextAndTask(t *testing.T) {
	eng := &Engine{
		config:    NewConfig(),
		templates: []*types.Template{{Id: "demo"}},
	}
	if _, err := eng.Execute(fakeContext{ctx: context.Background()}, NewExecuteTask("http://127.0.0.1")); err == nil {
		t.Fatal("expected unsupported context type")
	}
	if _, err := eng.Execute(NewContext(), fakeTask{typ: "unknown"}); err == nil {
		t.Fatal("expected unsupported task type")
	}
}

type fakeContext struct {
	ctx context.Context
}

func (f fakeContext) Context() context.Context {
	return f.ctx
}

type fakeTask struct {
	typ string
}

func (f fakeTask) Type() string {
	return f.typ
}

func (f fakeTask) Validate() error {
	return nil
}

var _ types.Context = fakeContext{}
var _ types.Task = fakeTask{}
