package scan

import (
	"strings"

	"github.com/chainreactors/parsers"
)

func formatEventLine(event event, color bool) string {
	rc := newRenderColor(color)
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
			return formatOutputLine(outputPrefix(label, rc.Green), target.Result.OutputLine(), color)
		case webTarget:
			if target.URL == "" {
				return ""
			}
			return formatOutputLine(outputPrefix("web", rc.Green), parsers.JoinOutput(target.URL, target.HostHeader), color)
		case webProbeTarget:
			if !reportableSprayResultForCapability(target.Result, target.Capability) {
				return ""
			}
			return formatOutputLine(outputPrefix("web", rc.Green), target.Result.OutputLine(), color)
		}
	case eventFinding:
		switch finding := event.Finding.(type) {
		case fingerprintFinding:
			names := parsers.NormalizeNames(finding.Fingers)
			if len(names) == 0 || !finding.Focus {
				return ""
			}
			return formatOutputLine(outputPrefix("fingerprint", rc.ForPriority(finding.Priority())), parsers.JoinOutput(finding.Target, parsers.NamesOutput(names)), color)
		case weakpassFinding:
			if finding.Result == nil {
				return ""
			}
			return formatOutputLine(outputPrefix("risk", rc.ForPriority(finding.Priority())), finding.Result.OutputLine(), color)
		case vulnFinding:
			if finding.String() == "" {
				return ""
			}
			return formatOutputLine(outputPrefix("vuln", rc.ForPriority(finding.Priority())), finding.String(), color)
		case verificationFinding:
			if !reportableVerificationFinding(finding) {
				return ""
			}
			return formatOutputLine(outputPrefix("ai", rc.ForVerificationStatus(finding.Status)), verificationOutput(finding), color)
		case aiSkillFinding:
			if finding.Summary == "" && finding.Detail == "" {
				return ""
			}
			return formatOutputLine(outputPrefix(aiSkillOutputLabelWithStatus(finding), rc.ForAISkill(finding.Status)), aiSkillOutput(finding), color)
		case aiSkillResponse:
			if finding.Summary == "" && finding.Detail == "" && finding.Raw == "" {
				return ""
			}
			return formatOutputLine(outputPrefix(aiSkillResponseLabel(finding), rc.Dim), aiSkillResponseOutput(finding), color)
		}
	case eventError:
		if event.Error.Message == "" {
			return ""
		}
		return formatOutputLine(outputPrefix("error", rc.Red), parsers.JoinOutput(event.Error.Message), color)
	}
	return ""
}

func outputPrefix(source string, colorFn func(string) string) string {
	return colorFn("[" + source + "]")
}

func formatOutputLine(prefix, output string, color bool) string {
	output = strings.TrimSpace(output)
	parts := []string{prefix}
	if output != "" {
		parts = append(parts, output)
	}
	return sanitizeOutputLine(strings.Join(parts, " "), color)
}

func sanitizeOutputLine(line string, color bool) string {
	line = strings.TrimSpace(line)
	if !color {
		line = stripANSI(line)
	}
	return line
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
