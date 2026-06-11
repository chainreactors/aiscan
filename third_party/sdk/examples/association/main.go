package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/chainreactors/sdk/pkg/association"
	"github.com/chainreactors/sdk/pkg/cyberhub"
	"github.com/chainreactors/sdk/pkg/types"
)

var (
	hubURL       = flag.String("url", "", "Cyberhub URL; empty uses inline demo data")
	apiKey       = flag.String("key", "", "Cyberhub API key")
	fingerName   = flag.String("finger", "", "Finger name query")
	aliasName    = flag.String("alias", "", "Alias name query")
	templateID   = flag.String("template", "", "Template ID query")
	cveID        = flag.String("cve", "", "CVE query")
	tag          = flag.String("tag", "", "Tag query")
	service      = flag.String("service", "", "Service query")
	severity     = flag.String("severity", "", "Template severity query")
	metadataKeys = flag.String("metadata-keys", "", "Comma-separated metadata keys to index")
	timeout      = flag.Duration("timeout", 20*time.Second, "Cyberhub request timeout")
)

func main() {
	flag.Parse()

	if *hubURL == "" && *apiKey == "" {
		idx := buildInlineIndex()
		q := queryFromFlags()
		if isEmptyQuery(q) {
			runInlineDemo(idx)
			return
		}
		printResult("inline lookup", idx.Lookup(q))
		return
	}

	if *hubURL == "" || *apiKey == "" {
		fmt.Fprintln(os.Stderr, "both -url and -key are required for remote Cyberhub mode")
		os.Exit(1)
	}

	ctx := context.Background()
	hub := cyberhub.NewProvider(*hubURL, *apiKey).WithTimeout(*timeout)
	idx, err := association.BuildFromProviderWithOptions(ctx, hub, association.IndexOptions{
		MetadataKeys: splitCSV(*metadataKeys),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "build association index failed: %v\n", err)
		os.Exit(1)
	}

	q := queryFromFlags()
	if isEmptyQuery(q) {
		q.WithFingers("tomcat")
	}
	printResult("remote lookup", idx.Lookup(q))
}

func runInlineDemo(idx *association.Index) {
	printResult("finger -> alias -> template", idx.Lookup(
		association.NewQuery().WithFingers("apache tomcat"),
	))
	printResult("template -> alias -> finger", idx.Lookup(
		association.NewQuery().WithTemplates("CVE-2022-0001"),
	))
	printResult("CVE -> template -> finger", idx.Lookup(
		association.NewQuery().WithCVEs("CVE-2021-44228"),
	))
	printResult("attribute severity=medium", idx.Lookup(
		association.NewQuery().WithAttr("severity", "medium"),
	))
	printResult("metadata category=middleware", idx.Lookup(
		association.NewQuery().WithAttr("category", "middleware"),
	))
}

func buildInlineIndex() *association.Index {
	fingers := types.Fingers{
		{
			Name:        "tomcat",
			Protocol:    "http",
			Description: "Apache Tomcat",
			Tags:        []string{"appserver"},
			Attributes: types.Attributes{
				Vendor:  "apache",
				Product: "tomcat",
			},
		},
		{
			Name:        "apache-log4j",
			Protocol:    "http",
			Description: "Apache Log4j",
			Tags:        []string{"log4j"},
		},
	}

	aliases := []*types.Alias{
		{
			Name: "tomcat",
			Attributes: types.Attributes{
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
	}

	templates := []*types.Template{
		{
			Id:      "CVE-2022-0001",
			Fingers: []string{"apache tomcat"},
			Info: types.TemplateInfo{
				Name:     "Tomcat alias bridge demo",
				Severity: "medium",
				Tags:     "cve,tomcat",
				Classification: &types.Classification{
					CVEID: "CVE-2022-0001",
					CPE:   "apache/tomcat",
				},
			},
		},
		{
			Id:      "CVE-2021-44228",
			Fingers: []string{"apache-log4j"},
			Info: types.TemplateInfo{
				Name:     "Log4Shell demo",
				Severity: "critical",
				Tags:     "cve,rce,log4j",
				Classification: &types.Classification{
					CVEID: "CVE-2021-44228",
				},
			},
		},
	}

	idx := association.NewIndex(association.WithMetadataKeys("category"))
	idx.BuildWithFingers(fingers, aliases, templates)
	return idx
}

func queryFromFlags() *association.Query {
	q := association.NewQuery()
	if *fingerName != "" {
		q.WithFingers(*fingerName)
	}
	if *aliasName != "" {
		q.WithAliases(*aliasName)
	}
	if *templateID != "" {
		q.WithTemplates(*templateID)
	}
	if *cveID != "" {
		q.WithCVEs(*cveID)
	}
	if *tag != "" {
		q.WithTags(*tag)
	}
	if *service != "" {
		q.WithServices(*service)
	}
	if *severity != "" {
		q.WithAttr("severity", *severity)
	}
	return q
}

func isEmptyQuery(q *association.Query) bool {
	return q == nil ||
		(len(q.Fingers) == 0 &&
			len(q.Aliases) == 0 &&
			len(q.Templates) == 0 &&
			len(q.Tags) == 0 &&
			len(q.Services) == 0 &&
			len(q.CPEs) == 0 &&
			len(q.CVEs) == 0 &&
			len(q.Attributes) == 0)
}

func printResult(title string, result *association.QueryResult) {
	fmt.Printf("\n%s\n", title)
	fmt.Printf("  fingers  : %s\n", strings.Join(fingerNames(result.Fingers), ", "))
	fmt.Printf("  aliases  : %s\n", strings.Join(aliasNames(result.Aliases), ", "))
	fmt.Printf("  templates: %s\n", strings.Join(templateIDs(result.Templates), ", "))
}

func fingerNames(fingers types.Fingers) []string {
	names := make([]string, 0, len(fingers))
	for _, finger := range fingers {
		if finger != nil {
			names = append(names, finger.Name)
		}
	}
	sort.Strings(names)
	return names
}

func aliasNames(aliases []*types.Alias) []string {
	names := make([]string, 0, len(aliases))
	for _, item := range aliases {
		if item != nil {
			names = append(names, item.Name)
		}
	}
	sort.Strings(names)
	return names
}

func templateIDs(templates []*types.Template) []string {
	ids := make([]string, 0, len(templates))
	for _, template := range templates {
		if template != nil {
			ids = append(ids, template.Id)
		}
	}
	sort.Strings(ids)
	return ids
}

func splitCSV(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}
