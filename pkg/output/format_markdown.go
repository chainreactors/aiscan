package output

import (
	"fmt"
	"io"
	"strings"

	sdktypes "github.com/chainreactors/sdk/pkg/types"
)

func RenderRecordsMarkdown(w io.Writer, records []Record) error {
	fmt.Fprintln(w, "# Scan Record")
	fmt.Fprintln(w)

	var result Result
	var scanEnd *ScanEnd

	for _, r := range records {
		switch r.Type {
		case TypeLoot:
			d, _ := ParseRecordData[Loot](r)
			result.Loots = append(result.Loots, d)
		case TypeScanEnd:
			d, _ := ParseRecordData[ScanEnd](r)
			scanEnd = &d
		}
	}

	if scanEnd != nil {
		fmt.Fprintf(w, "## Summary\n\n")
		fmt.Fprintf(w, "| Metric | Value |\n|---|---:|\n")
		fmt.Fprintf(w, "| Duration | %.1fs |\n", scanEnd.Duration)
		fmt.Fprintf(w, "| Services | %d |\n", scanEnd.Services)
		fmt.Fprintf(w, "| Web | %d |\n", scanEnd.Webs)
		fmt.Fprintf(w, "| Loots | %d |\n", scanEnd.Loots)
		fmt.Fprintln(w)
	}

	if len(result.Loots) > 0 {
		fmt.Fprintf(w, "## Loots\n\n")
		for _, d := range result.Loots {
			fmt.Fprintf(w, "- **[%s]** `%s` %s\n", d.Kind, d.Target, d.Description)
		}
		fmt.Fprintln(w)
	}

	return nil
}

func writeMarkdownFence(w io.Writer, lang, body string) {
	fence := "```"
	if strings.Contains(body, "```") {
		fence = "~~~"
	}
	fmt.Fprintf(w, "%s%s\n%s\n%s\n\n", fence, lang, body, fence)
}

// RecordsToResult converts parsed records into a Result for asset report rendering.
func RecordsToResult(records []Record) *Result {
	result := &Result{}
	for _, r := range records {
		switch r.Type {
		case TypeService:
			d, _ := ParseRecordData[sdktypes.GOGOResult](r)
			result.Services = append(result.Services, &d)
		case TypeWeb:
			d, _ := ParseRecordData[sdktypes.SprayResult](r)
			if d.UrlString == "" {
				continue
			}
			if d.Status > 0 {
				result.WebProbes = append(result.WebProbes, &d)
			}
		case TypeLoot:
			d, _ := ParseRecordData[Loot](r)
			result.Loots = append(result.Loots, d)
		case TypeScanEnd:
			d, _ := ParseRecordData[ScanEnd](r)
			result.Summary = Summary{
				Targets:  d.Targets,
				Services: d.Services,
				Webs:     d.Webs,
				Loots:    d.Loots,
				Duration: fmt.Sprintf("%.1fs", d.Duration),
			}
		}
	}

	if result.Summary.Probes == 0 {
		result.Summary.Probes = len(result.WebProbes)
	}
	return result
}

// RenderRecordFileAsAsset reads a record JSONL file and renders as an asset report.
func RenderRecordFileAsAsset(path string, color bool, aggregate func(*Result) []Asset) (string, *Result, error) {
	records, err := ParseRecordFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("open record file: %w", err)
	}

	result := RecordsToResult(records)
	if aggregate != nil {
		result.Assets = aggregate(result)
	}

	out := FormatAssetReport(result, color)
	return strings.TrimRight(out, "\n") + "\n", result, nil
}
