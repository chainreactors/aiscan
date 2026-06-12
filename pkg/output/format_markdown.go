package output

import (
	"fmt"
	"strings"

	sdktypes "github.com/chainreactors/sdk/pkg/types"
)

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
