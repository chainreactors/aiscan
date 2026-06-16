package web

import (
	"strings"
	"testing"

	"github.com/chainreactors/aiscan/core/output"
)

func TestBuildMarkdownReportKeepsAssetDetailAsMarkdown(t *testing.T) {
	report := buildMarkdownReport("http://127.0.0.1:8092", "quick", &output.Result{
		Summary: output.Summary{Targets: 1},
		Assets: []output.Asset{
			{
				Target: "http://127.0.0.1:8092",
				Items: []output.AssetItem{
					{
						Kind:    output.AssetItemResponse,
						Source:  "deep",
						Status:  "response",
						Summary: "manual agent response",
						Detail:  "Let me analyze the collected browser evidence.\n\n## Evidence Analysis\n\n| Asset | Details |\n|---|---|\n| API | GET /api/scans |",
					},
				},
			},
		},
	})

	for _, want := range []string{"## Evidence Analysis", "| Asset | Details |"} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q:\n%s", want, report)
		}
	}
}
