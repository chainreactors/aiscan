package scan

import (
	"github.com/chainreactors/aiscan/pkg/output"
	"github.com/chainreactors/parsers"
)

func formatEventLine(event event, color bool) string {
	c := output.NewColor(color)
	switch event.Kind {
	case eventTarget:
		switch target := event.Target.(type) {
		case serviceTarget:
			if target.Result == nil {
				return ""
			}
			label := "service"
			if target.Result.IsHttp() {
				label = "web"
			}
			return output.FormatLine(output.OutputPrefix(label, c.Green), target.Result.OutputLine(), c)
		case webTarget:
			if target.URL == "" {
				return ""
			}
			return output.FormatLine(output.OutputPrefix("web", c.Green), parsers.JoinOutput(target.URL, target.HostHeader), c)
		case webProbeTarget:
			if !reportableSprayResultForCapability(target.Result, target.Capability) {
				return ""
			}
			return output.FormatLine(output.OutputPrefix("web", c.Green), target.Result.OutputLine(), c)
		}
	case eventFinding:
		switch finding := event.Finding.(type) {
		case fingerprintFinding:
			names := parsers.NormalizeNames(finding.Fingers)
			if len(names) == 0 || !finding.Focus {
				return ""
			}
			return output.FormatLine(output.OutputPrefix("fingerprint", c.ForPriority(string(finding.Priority()))), parsers.JoinOutput(finding.Target, parsers.NamesOutput(names)), c)
		case weakpassFinding:
			if finding.Result == nil {
				return ""
			}
			return output.FormatLine(output.OutputPrefix("risk", c.ForPriority(string(finding.Priority()))), finding.Result.OutputLine(), c)
		case vulnFinding:
			if finding.String() == "" {
				return ""
			}
			return output.FormatLine(output.OutputPrefix("vuln", c.ForPriority(string(finding.Priority()))), finding.String(), c)
		case verificationFinding:
			if !reportableVerificationFinding(finding) {
				return ""
			}
			return output.FormatLine(output.OutputPrefix("ai", c.ForStatus(string(finding.Status))), verificationOutput(finding), c)
		case aiSkillFinding:
			if finding.Summary == "" && finding.Detail == "" {
				return ""
			}
			return output.FormatLine(output.OutputPrefix(aiSkillOutputLabelWithStatus(finding), c.ForStatus(finding.Status)), aiSkillOutput(finding), c)
		case aiSkillResponse:
			if finding.Summary == "" && finding.Detail == "" && finding.Raw == "" {
				return ""
			}
			return output.FormatLine(output.OutputPrefix(aiSkillResponseLabel(finding), c.Dim), aiSkillResponseOutput(finding), c)
		}
	case eventError:
		if event.Error.Message == "" {
			return ""
		}
		return output.FormatLine(output.OutputPrefix("error", c.Red), parsers.JoinOutput(event.Error.Message), c)
	}
	return ""
}

func verificationOutput(finding verificationFinding) string {
	return parsers.JoinOutput(
		finding.Target,
		string(finding.OriginalKind),
		string(finding.Status),
		finding.Summary,
		finding.Evidence,
	)
}

func aiSkillOutputLabel(skill string) string {
	if skill == "verify" {
		return "ai"
	}
	return skill
}

func aiSkillOutputLabelWithStatus(finding aiSkillFinding) string {
	base := aiSkillOutputLabel(finding.Skill)
	switch finding.Status {
	case "confirmed":
		return base + ":verified"
	case "not_confirmed":
		return base + ":rejected"
	case "info":
		return base + ":info"
	case "inconclusive":
		return base + ":inconclusive"
	default:
		return base
	}
}

func aiSkillOutput(finding aiSkillFinding) string {
	parts := []string{finding.Target}
	if finding.Status != "" {
		parts = append(parts, finding.Status)
	}
	if finding.Summary != "" {
		parts = append(parts, finding.Summary)
	}
	if finding.Detail != "" {
		parts = append(parts, finding.Detail)
	}
	return parsers.JoinOutput(parts...)
}

func aiSkillResponseLabel(response aiSkillResponse) string {
	base := aiSkillOutputLabel(response.Skill)
	if response.Status != "" {
		return base + ":" + response.Status
	}
	return base + ":response"
}

func aiSkillResponseOutput(response aiSkillResponse) string {
	parts := []string{response.Target}
	if response.Status != "" {
		parts = append(parts, response.Status)
	}
	if response.Summary != "" {
		parts = append(parts, response.Summary)
	}
	if response.Detail != "" {
		parts = append(parts, response.Detail)
	} else if response.Raw != "" {
		parts = append(parts, response.Raw)
	}
	return parsers.JoinOutput(parts...)
}
