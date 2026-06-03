package scan

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/chainreactors/aiscan/pkg/record"
)

func renderRecordFile(path string, noColor bool) (string, *StructuredResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", nil, fmt.Errorf("scan: open record file: %w", err)
	}
	defer f.Close()

	result := &StructuredResult{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		r, err := record.Parse(line)
		if err != nil {
			continue
		}
		switch r.Type {
		case record.TypeService:
			d, _ := record.ParseData[record.Service](r)
			result.Services = append(result.Services, StructuredService{
				Target:   d.Target,
				Port:     fmt.Sprintf("%d", d.Port),
				Protocol: d.Protocol,
				Service:  d.Protocol,
				Banner:   d.Banner,
			})
		case record.TypeWeb:
			d, _ := record.ParseData[record.Web](r)
			if d.URL == "" {
				continue
			}
			ep := StructuredWebEndpoint{
				URL:     d.URL,
				Status:  d.Status,
				Length:  d.ContentLen,
				Title:   d.Title,
				Fingers: d.Fingers,
			}
			if d.Status > 0 {
				result.WebProbes = append(result.WebProbes, ep)
			} else {
				result.WebEndpoints = append(result.WebEndpoints, ep)
			}
		case record.TypeFinding:
			d, _ := record.ParseData[record.Finding](r)
			f := StructuredFinding{
				Kind:     d.Kind,
				Target:   d.Target,
				Priority: d.Priority,
				Summary:  d.Summary,
				Detail:   d.Detail,
			}
			switch d.Kind {
			case "fingerprint":
				result.Fingerprints = append(result.Fingerprints, StructuredFingerprint{
					Target: d.Target,
					Name:   d.Summary,
					Focus:  d.Priority == "high" || d.Priority == "critical",
				})
			case "vuln-finding":
				result.Vulns = append(result.Vulns, f)
			default:
				result.Risks = append(result.Risks, f)
			}
		case record.TypeAISkill:
			d, _ := record.ParseData[record.AISkill](r)
			result.AI = append(result.AI, StructuredFinding{
				Kind:    "ai-skill",
				Skill:   d.Skill,
				Target:  d.Target,
				Status:  d.Status,
				Summary: d.Summary,
				Detail:  d.Detail,
			})
		case record.TypeScanEnd:
			d, _ := record.ParseData[record.ScanEnd](r)
			result.Summary = StructuredSummary{
				Targets:  d.Targets,
				Services: d.Services,
				Webs:     d.Webs,
				Risks:    d.Findings,
				Duration: fmt.Sprintf("%.1fs", d.Duration),
			}
		}
	}

	if result.Summary.Probes == 0 {
		result.Summary.Probes = len(result.WebProbes)
	}
	if result.Summary.Fingerprints == 0 {
		result.Summary.Fingerprints = len(result.Fingerprints)
	}
	result.Assets = AggregateStructuredResult(result)

	out := formatAssetReport(result, !noColor)
	return strings.TrimRight(out, "\n") + "\n", result, nil
}
