package association

import (
	"testing"

	"github.com/chainreactors/fingers/alias"
	"github.com/chainreactors/fingers/common"
	fingersEngine "github.com/chainreactors/fingers/fingers"
	"github.com/chainreactors/neutron/templates"
)

func buildTestIndex(options ...IndexOption) *Index {
	fingers := fingersEngine.Fingers{
		{
			Name:        "nginx",
			Protocol:    "http",
			Description: "Nginx web server",
			Tags:        []string{"webserver", "proxy"},
		},
		{
			Name:        "mysql",
			Protocol:    "tcp",
			Description: "MySQL database",
			Tags:        []string{"database"},
		},
		{
			Name:        "apache-log4j",
			Protocol:    "http",
			Description: "Apache Log4j",
			Tags:        []string{"log4j"},
		},
		{
			Name:        "tomcat",
			Protocol:    "http",
			Description: "Apache Tomcat",
			Tags:        []string{"appserver"},
		},
		nil,
	}

	aliases := []*alias.Alias{
		{
			Name: "nginx",
			Attributes: common.Attributes{
				Vendor:  "nginx",
				Product: "nginx",
			},
			Tags: []string{"webserver", "proxy"},
			Pocs: []string{"CVE-2021-23017"},
			Metadata: map[string]interface{}{
				"service": "http",
			},
		},
		{
			Name: "mysql",
			Attributes: common.Attributes{
				Vendor:  "oracle",
				Product: "mysql",
			},
			Tags: []string{"database"},
			Pocs: []string{"CVE-2012-2122"},
			Metadata: map[string]interface{}{
				"service": "mysql",
			},
		},
		{
			Name: "tomcat",
			Attributes: common.Attributes{
				Vendor:  "apache",
				Product: "tomcat",
			},
			Tags: []string{"appserver"},
			Pocs: []string{"CVE-2022-0001"},
			AliasMap: map[string][]string{
				"fingers": {"Apache Tomcat"},
			},
			Metadata: map[string]interface{}{
				"service":  "http",
				"category": "middleware",
			},
		},
		nil,
	}
	for _, a := range aliases {
		if a != nil {
			a.Compile()
		}
	}

	tpls := []*templates.Template{
		{
			Id:      "CVE-2021-23017",
			Fingers: []string{"nginx"},
			Info: templates.Info{
				Name:     "Nginx DNS Resolver Vuln",
				Severity: "high",
				Tags:     "cve,nginx",
				Classification: &templates.Classification{
					CVEID: "CVE-2021-23017",
				},
			},
		},
		{
			Id:      "CVE-2021-44228",
			Fingers: []string{"apache-log4j"},
			Info: templates.Info{
				Name:     "Log4Shell",
				Severity: "critical",
				Tags:     "cve,rce,log4j",
				Classification: &templates.Classification{
					CVEID: "CVE-2021-44228",
				},
			},
		},
		{
			Id:      "CVE-2012-2122",
			Fingers: []string{"mysql"},
			Info: templates.Info{
				Name:     "MySQL Auth Bypass",
				Severity: "critical",
				Tags:     "cve,mysql,auth-bypass",
				Classification: &templates.Classification{
					CVEID: "CVE-2012-2122",
				},
			},
		},
		{
			Id: "CVE-2022-0001",
			Info: templates.Info{
				Name:     "Tomcat Alias Bridge Vuln",
				Severity: "medium",
				Tags:     "cve,tomcat",
				Classification: &templates.Classification{
					CVEID: "CVE-2022-0001",
				},
			},
		},
		nil,
	}

	idx := NewIndex(options...)
	idx.BuildWithFingers(fingers, aliases, tpls)
	return idx
}

func TestPrimaryGetters(t *testing.T) {
	idx := buildTestIndex()

	if a := idx.Alias("nginx"); a == nil || a.Name != "nginx" {
		t.Fatalf("expected nginx alias, got %v", a)
	}
	if f := idx.Finger("nginx"); f == nil || f.Description != "Nginx web server" {
		t.Fatalf("expected full nginx finger, got %v", f)
	}
	if tpl := idx.Template("CVE-2021-23017"); tpl == nil || tpl.Id != "CVE-2021-23017" {
		t.Fatalf("expected nginx template, got %v", tpl)
	}

	if got := idx.Aliases("nginx", "mysql", "nginx"); len(got) != 2 {
		t.Fatalf("expected 2 aliases, got %d", len(got))
	}
	if got := idx.Fingers("nginx", "mysql", "nginx"); len(got) != 2 {
		t.Fatalf("expected 2 fingers, got %d", len(got))
	}
	if got := idx.Templates("CVE-2021-23017", "CVE-2012-2122", "CVE-2012-2122"); len(got) != 2 {
		t.Fatalf("expected 2 templates, got %d", len(got))
	}
}

func TestQueryBuilder(t *testing.T) {
	q := NewQuery().
		WithFingers("nginx").
		WithAliases("tomcat").
		WithTemplates("CVE-2022-0001").
		WithTags("database").
		WithServices("mysql").
		WithCPEs("oracle/mysql").
		WithCVEs("CVE-2021-44228").
		WithAttr("severity", "medium")

	if len(q.Fingers) != 1 || q.Fingers[0] != "nginx" {
		t.Fatalf("unexpected fingers: %v", q.Fingers)
	}
	if len(q.Aliases) != 1 || q.Aliases[0] != "tomcat" {
		t.Fatalf("unexpected aliases: %v", q.Aliases)
	}
	if len(q.Templates) != 1 || q.Templates[0] != "CVE-2022-0001" {
		t.Fatalf("unexpected templates: %v", q.Templates)
	}
	if len(q.CVEs) != 1 || q.CVEs[0] != "CVE-2021-44228" {
		t.Fatalf("unexpected CVEs: %v", q.CVEs)
	}
	if len(q.Attributes["severity"]) != 1 || q.Attributes["severity"][0] != "medium" {
		t.Fatalf("unexpected attributes: %v", q.Attributes)
	}
}

func TestQueryWithFrameworksAndVulns(t *testing.T) {
	fws := common.Frameworks{
		"nginx": &common.Framework{Name: "nginx"},
	}
	vulns := common.Vulns{
		"CVE-2021-44228": &common.Vuln{
			Name:      "CVE-2021-44228",
			Framework: &common.Framework{Name: "apache-log4j"},
		},
	}

	q := NewQuery().WithFrameworks(fws).WithVulns(vulns)
	if len(q.Fingers) != 2 {
		t.Fatalf("expected 2 fingers from frameworks/vulns, got %d", len(q.Fingers))
	}
	if len(q.CVEs) != 1 || q.CVEs[0] != "CVE-2021-44228" {
		t.Fatalf("unexpected CVEs: %v", q.CVEs)
	}
}

func TestQueryMerge(t *testing.T) {
	q1 := NewQuery().WithFingers("nginx")
	q2 := NewQuery().WithServices("mysql").WithCVEs("CVE-2021-44228")
	q1.Merge(q2)

	if len(q1.Fingers) != 1 || len(q1.Services) != 1 || len(q1.CVEs) != 1 {
		t.Fatalf("merge failed: fingers=%v services=%v cves=%v", q1.Fingers, q1.Services, q1.CVEs)
	}
}

func TestLookupByFinger(t *testing.T) {
	idx := buildTestIndex()

	r := idx.Lookup(NewQuery().WithFingers("nginx"))
	if len(r.Fingers) != 1 || r.Fingers[0].Name != "nginx" {
		t.Fatalf("expected nginx finger, got %v", r.Fingers)
	}
	if len(r.Aliases) != 1 || r.Aliases[0].Name != "nginx" {
		t.Fatalf("expected nginx alias, got %v", r.Aliases)
	}
	if len(r.Templates) != 1 || r.Templates[0].Id != "CVE-2021-23017" {
		t.Fatalf("expected nginx template, got %v", r.Templates)
	}
}

func TestLookupByAliasMappedFinger(t *testing.T) {
	idx := buildTestIndex()

	r := idx.Lookup(NewQuery().WithFingers("apache-tomcat"))
	if len(r.Fingers) != 1 || r.Fingers[0].Name != "tomcat" {
		t.Fatalf("expected tomcat finger via alias map, got %v", r.Fingers)
	}
	if len(r.Aliases) != 1 || r.Aliases[0].Name != "tomcat" {
		t.Fatalf("expected tomcat alias via alias map, got %v", r.Aliases)
	}
	if len(r.Templates) != 1 || r.Templates[0].Id != "CVE-2022-0001" {
		t.Fatalf("expected tomcat template via alias pocs, got %v", r.Templates)
	}
}

func TestLookupByTemplate(t *testing.T) {
	idx := buildTestIndex()

	r := idx.Lookup(NewQuery().WithTemplates("CVE-2022-0001"))
	if len(r.Fingers) != 1 || r.Fingers[0].Name != "tomcat" {
		t.Fatalf("expected tomcat finger from template, got %v", r.Fingers)
	}
	if len(r.Aliases) != 1 || r.Aliases[0].Name != "tomcat" {
		t.Fatalf("expected tomcat alias from template, got %v", r.Aliases)
	}
	if len(r.Templates) != 1 || r.Templates[0].Id != "CVE-2022-0001" {
		t.Fatalf("expected template result, got %v", r.Templates)
	}
}

func TestLookupByCVE(t *testing.T) {
	idx := buildTestIndex()

	r := idx.Lookup(NewQuery().WithCVEs("CVE-2021-44228"))
	if len(r.Fingers) != 1 || r.Fingers[0].Name != "apache-log4j" {
		t.Fatalf("expected apache-log4j finger via cve, got %v", r.Fingers)
	}
	if len(r.Templates) != 1 || r.Templates[0].Id != "CVE-2021-44228" {
		t.Fatalf("expected log4j template, got %v", r.Templates)
	}
}

func TestLookupByTagServiceAndCPE(t *testing.T) {
	idx := buildTestIndex()

	byTag := idx.Lookup(NewQuery().WithTags("database"))
	if len(byTag.Fingers) != 1 || byTag.Fingers[0].Name != "mysql" {
		t.Fatalf("expected mysql finger by tag, got %v", byTag.Fingers)
	}
	if len(byTag.Templates) != 1 || byTag.Templates[0].Id != "CVE-2012-2122" {
		t.Fatalf("expected mysql template by tag, got %v", byTag.Templates)
	}

	byService := idx.Lookup(NewQuery().WithServices("mysql"))
	if len(byService.Aliases) != 1 || byService.Aliases[0].Name != "mysql" {
		t.Fatalf("expected mysql alias by service, got %v", byService.Aliases)
	}
	if len(byService.Templates) != 1 || byService.Templates[0].Id != "CVE-2012-2122" {
		t.Fatalf("expected mysql template by service, got %v", byService.Templates)
	}

	byCPE := idx.Lookup(NewQuery().WithCPEs("oracle/mysql"))
	if len(byCPE.Aliases) != 1 || byCPE.Aliases[0].Name != "mysql" {
		t.Fatalf("expected mysql alias by cpe, got %v", byCPE.Aliases)
	}
}

func TestLookupByFixedAttribute(t *testing.T) {
	idx := buildTestIndex()

	r := idx.Lookup(NewQuery().WithAttr("severity", "medium"))
	if len(r.Fingers) != 1 || r.Fingers[0].Name != "tomcat" {
		t.Fatalf("expected tomcat finger by severity, got %v", r.Fingers)
	}
	if len(r.Templates) != 1 || r.Templates[0].Id != "CVE-2022-0001" {
		t.Fatalf("expected medium template, got %v", r.Templates)
	}
}

func TestLookupMetadataWhitelist(t *testing.T) {
	defaultIdx := buildTestIndex()
	if r := defaultIdx.Lookup(NewQuery().WithAttr("category", "middleware")); len(r.Aliases) != 0 {
		t.Fatalf("category metadata should not be indexed by default, got %v", r.Aliases)
	}

	idx := buildTestIndex(WithMetadataKeys("category"))
	r := idx.Lookup(NewQuery().WithAttr("category", "middleware"))
	if len(r.Fingers) != 1 || r.Fingers[0].Name != "tomcat" {
		t.Fatalf("expected tomcat finger by whitelisted metadata, got %v", r.Fingers)
	}
	if len(r.Aliases) != 1 || r.Aliases[0].Name != "tomcat" {
		t.Fatalf("expected tomcat alias by whitelisted metadata, got %v", r.Aliases)
	}
	if len(r.Templates) != 1 || r.Templates[0].Id != "CVE-2022-0001" {
		t.Fatalf("expected tomcat template by whitelisted metadata, got %v", r.Templates)
	}
}

func TestLookupComposed(t *testing.T) {
	idx := buildTestIndex()

	q := NewQuery().
		WithFingers("nginx").
		WithCVEs("CVE-2021-44228")
	r := idx.Lookup(q)

	if len(r.Fingers) != 2 {
		t.Fatalf("expected nginx and log4j fingers, got %v", r.Fingers)
	}
	if len(r.Aliases) != 1 || r.Aliases[0].Name != "nginx" {
		t.Fatalf("expected nginx alias, got %v", r.Aliases)
	}
	if len(r.Templates) != 2 {
		t.Fatalf("expected nginx and log4j templates, got %v", r.Templates)
	}
}

func TestLookupNil(t *testing.T) {
	idx := buildTestIndex()

	r := idx.Lookup(nil)
	if r == nil || len(r.Fingers) != 0 || len(r.Aliases) != 0 || len(r.Templates) != 0 {
		t.Fatal("expected empty result for nil query")
	}
}
