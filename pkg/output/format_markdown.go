package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	sdktypes "github.com/chainreactors/sdk/pkg/types"
)

func RenderRecordsMarkdown(w io.Writer, records []Record) error {
	fmt.Fprintln(w, "# Scan Record")
	fmt.Fprintln(w)

	var services []*sdktypes.GOGOResult
	var webs []*sdktypes.SprayResult
	var zombieFindings []*sdktypes.ZombieResult
	var vulnFindings []*sdktypes.VulnResult
	var aiSkills []AISkill
	type aiTurnEntry struct {
		AITurn
		parsedMsgs []aiMessageView
	}
	var aiTurns []aiTurnEntry
	var scanEnd *ScanEnd

	for _, r := range records {
		switch r.Type {
		case TypeService:
			d, _ := ParseRecordData[sdktypes.GOGOResult](r)
			services = append(services, &d)
		case TypeWeb:
			d, _ := ParseRecordData[sdktypes.SprayResult](r)
			webs = append(webs, &d)
		case TypeFinding:
			// Try zombie first, then vuln.
			var zr sdktypes.ZombieResult
			if err := json.Unmarshal(r.Data, &zr); err == nil && zr.Service != "" {
				zombieFindings = append(zombieFindings, &zr)
			} else {
				var vr sdktypes.VulnResult
				if err := json.Unmarshal(r.Data, &vr); err == nil && vr.TemplateID != "" {
					vulnFindings = append(vulnFindings, &vr)
				}
			}
		case TypeAISkill:
			d, _ := ParseRecordData[AISkill](r)
			aiSkills = append(aiSkills, d)
		case TypeAITurn:
			d, _ := ParseRecordData[AITurn](r)
			aiTurns = append(aiTurns, aiTurnEntry{AITurn: d, parsedMsgs: parseAIMessages(d.Messages)})
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
		fmt.Fprintf(w, "| Findings | %d |\n", scanEnd.Findings)
		fmt.Fprintf(w, "| AI Skills | %d |\n", scanEnd.AISkills)
		fmt.Fprintln(w)
	}

	if len(services) > 0 {
		fmt.Fprintf(w, "## Services\n\n")
		for _, d := range services {
			fmt.Fprintf(w, "- `%s` %s\n", d.GetTarget(), d.Protocol)
		}
		fmt.Fprintln(w)
	}

	if len(webs) > 0 {
		fmt.Fprintf(w, "## Web Endpoints\n\n")
		for _, d := range webs {
			fmt.Fprintf(w, "- `%s` %d %s\n", d.UrlString, d.Status, d.Title)
		}
		fmt.Fprintln(w)
	}

	if len(zombieFindings) > 0 || len(vulnFindings) > 0 {
		fmt.Fprintf(w, "## Findings\n\n")
		for _, d := range zombieFindings {
			fmt.Fprintf(w, "- **[zombie]** `%s` %s %s/%s\n", d.Address(), d.Service, d.Username, d.Password)
		}
		for _, d := range vulnFindings {
			fmt.Fprintf(w, "- **[%s]** `%s` %s — %s\n", d.Severity, d.Target, d.TemplateID, d.TemplateName)
		}
		fmt.Fprintln(w)
	}

	if len(aiSkills) > 0 {
		fmt.Fprintf(w, "## AI Skill Results\n\n")
		for _, d := range aiSkills {
			fmt.Fprintf(w, "### %s → %s (%.1fs)\n\n", d.Skill, d.Target, d.Duration)
			fmt.Fprintf(w, "**Status:** %s\n\n", d.Status)
			fmt.Fprintf(w, "%s\n\n", d.Summary)
			if d.Detail != "" {
				fmt.Fprintf(w, "> %s\n\n", d.Detail)
			}
		}
	}

	if len(aiTurns) > 0 {
		fmt.Fprintf(w, "## AI Execution Trace\n\n")
		for _, d := range aiTurns {
			fmt.Fprintf(w, "#### [%s] Turn %d (%.1fs)\n\n", d.Skill, d.Turn, d.Duration)
			if d.Prompt != "" {
				fmt.Fprintf(w, "**Request:** %s\n\n", TruncateStr(d.Prompt, 200))
			}
			for _, msg := range d.parsedMsgs {
				if msg.Role == "assistant" && msg.Content != "" {
					fmt.Fprintf(w, "**Response:** %s\n\n", TruncateStr(msg.Content, 300))
				}
				if len(msg.ToolCalls) > 0 {
					fmt.Fprintf(w, "**Tools:**\n")
					for _, tc := range msg.ToolCalls {
						fmt.Fprintf(w, "- `%s` %s\n", tc.Name, TruncateStr(tc.Arguments, 100))
					}
					fmt.Fprintln(w)
				}
			}
		}
	}

	return nil
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
		case TypeFinding:
			// Try zombie result first, then vuln result.
			var zr sdktypes.ZombieResult
			if err := json.Unmarshal(r.Data, &zr); err == nil && zr.Service != "" {
				result.Risks = append(result.Risks, &zr)
				continue
			}
			var vr sdktypes.VulnResult
			if err := json.Unmarshal(r.Data, &vr); err == nil && vr.TemplateID != "" {
				result.Vulns = append(result.Vulns, &vr)
				continue
			}
		case TypeAISkill:
			d, _ := ParseRecordData[AISkill](r)
			result.AI = append(result.AI, AIFinding{
				Kind:    "ai-skill",
				Skill:   d.Skill,
				Target:  d.Target,
				Status:  d.Status,
				Summary: d.Summary,
				Detail:  d.Detail,
			})
		case TypeScanEnd:
			d, _ := ParseRecordData[ScanEnd](r)
			result.Summary = Summary{
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
