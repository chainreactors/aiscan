package web

import (
	"strings"
	"testing"

	"github.com/chainreactors/aiscan/core/output"
)

func TestBuildMarkdownReportKeepsAssetDetailAsMarkdown(t *testing.T) {
	report := buildMarkdownReport("http://127.0.0.1:8092", "quick", &output.Result{
		// lang "en" preserves the original English structural labels.
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
	}, "en")

	for _, want := range []string{"## Evidence Analysis", "| Asset | Details |"} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q:\n%s", want, report)
		}
	}
}

func TestBuildMarkdownReportLocalizesLabels(t *testing.T) {
	result := &output.Result{
		Summary: output.Summary{Targets: 1},
		Assets: []output.Asset{
			{Target: "http://127.0.0.1:8092", Status: "high"},
		},
	}

	en := buildMarkdownReport("http://127.0.0.1:8092", "quick", result, "en")
	for _, want := range []string{"# Penetration Test Report", "## Summary", "| Metric | Value |", "- **State:** `high`"} {
		if !strings.Contains(en, want) {
			t.Fatalf("en report missing %q:\n%s", want, en)
		}
	}

	zh := buildMarkdownReport("http://127.0.0.1:8092", "quick", result, "zh")
	for _, want := range []string{"# 渗透测试报告", "## 摘要", "| 指标 | 数值 |", "- **状态:** `高`"} {
		if !strings.Contains(zh, want) {
			t.Fatalf("zh report missing %q:\n%s", want, zh)
		}
	}
	// Scan data (the target URL) must stay verbatim regardless of language.
	if !strings.Contains(zh, "http://127.0.0.1:8092") {
		t.Fatalf("zh report dropped raw target data:\n%s", zh)
	}
}
