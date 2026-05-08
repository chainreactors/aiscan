package scan

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/telemetry"
	"github.com/chainreactors/aiscan/pkg/tool"
	"github.com/chainreactors/parsers"
)

func (c *Command) agentVerifyCapability(flags flags) (capability, bool) {
	minPriority, err := parsePriority(flags.Verify)
	if err != nil {
		minPriority = priorityHigh
	}
	return capability{
		Name:   capAgentVerify,
		Worker: 1,
		AcceptEvents: func(event event) bool {
			if event.Kind != eventFinding || event.Finding == nil {
				return false
			}
			if event.Finding.Kind() == findingVerification {
				return false
			}
			return event.Finding.Priority().atLeast(minPriority)
		},
		RunEventKey: func(event event) string {
			if event.Finding == nil {
				return ""
			}
			return capAgentVerify + "|" + string(event.Finding.Kind()) + "|" + event.Finding.Key()
		},
		RunEvent: func(ctx context.Context, event event, emit emitFunc) {
			c.runAgentVerifyCapability(ctx, flags, event, emit)
		},
	}, true
}

func (c *Command) runAgentVerifyCapability(ctx context.Context, flags flags, event event, emit emitFunc) {
	if c.verification.Provider == nil {
		emit(findingEvent(capAgentVerify, verificationFinding{
			OriginalKey:      findingKey(event.Finding),
			OriginalKind:     findingKindOf(event.Finding),
			OriginalPriority: findingPriority(event.Finding),
			Status:           verificationFailed,
			Target:           findingTarget(event.Finding),
			Summary:          "AI verification skipped: provider is not configured",
		}))
		return
	}

	timeout := flags.VerifyTimeout
	if timeout <= 0 {
		timeout = c.verification.Timeout
	}
	if timeout <= 0 {
		timeout = 120
	}
	verifyCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	maxTurns := flags.VerifyTurns
	if maxTurns <= 0 {
		maxTurns = c.verification.MaxTurns
	}
	if maxTurns <= 0 {
		maxTurns = 3
	}

	model := strings.TrimSpace(c.verification.Model)
	prompt := buildVerificationPrompt(event)
	systemPrompt := strings.TrimSpace(c.verification.SystemPrompt)
	if systemPrompt == "" {
		systemPrompt = defaultVerificationSystemPrompt()
	}

	result, err := agent.Run(verifyCtx, prompt, tool.NewToolRegistry(),
		agent.WithProvider(c.verification.Provider),
		agent.WithModel(model),
		agent.WithMaxTurns(maxTurns),
		agent.WithMaxTokens(1200),
		agent.WithSystemPrompt(systemPrompt),
		agent.WithLogger(telemetry.NopLogger()),
	)
	if err != nil {
		c.logger.Warnf("scan capability=%s status=failed error=%q", capAgentVerify, err)
		emit(findingEvent(capAgentVerify, verificationFinding{
			OriginalKey:      findingKey(event.Finding),
			OriginalKind:     findingKindOf(event.Finding),
			OriginalPriority: findingPriority(event.Finding),
			Status:           verificationFailed,
			Target:           findingTarget(event.Finding),
			Summary:          err.Error(),
		}))
		return
	}

	status, summary, evidence := parseVerificationOutput(result)
	emit(findingEvent(capAgentVerify, verificationFinding{
		OriginalKey:      findingKey(event.Finding),
		OriginalKind:     findingKindOf(event.Finding),
		OriginalPriority: findingPriority(event.Finding),
		Status:           status,
		Target:           findingTarget(event.Finding),
		Summary:          summary,
		Evidence:         evidence,
	}))
}

func buildVerificationPrompt(event event) string {
	finding := event.Finding
	return fmt.Sprintf(`Verify this scan finding from already-collected scanner evidence.

Finding:
- source: %s
- kind: %s
- priority: %s
- key: %s
- target: %s
- evidence: %s

Return only this plain text format:
status: confirmed|not_confirmed|inconclusive
summary: one concise sentence
evidence: short evidence from the provided finding or why it is insufficient`,
		event.Source,
		findingKindOf(finding),
		findingPriority(finding),
		findingKey(finding),
		findingTarget(finding),
		findingEvidence(finding),
	)
}

func defaultVerificationSystemPrompt() string {
	return `You are aiscan's verification reviewer. Validate only the supplied scanner finding and evidence. Do not invent external facts, do not request tools, and do not perform additional scanning. Mark confirmed only when the evidence directly supports the risk.`
}

func parseVerificationOutput(output string) (verificationStatus, string, string) {
	output = strings.TrimSpace(output)
	if output == "" {
		return verificationInconclusive, "empty verification response", ""
	}
	status := verificationInconclusive
	summary := output
	evidence := ""
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		switch key {
		case "status":
			status = normalizeVerificationStatus(value)
		case "summary":
			if value != "" {
				summary = value
			}
		case "evidence":
			evidence = value
		}
	}
	return status, oneLine(summary), oneLine(evidence)
}

func normalizeVerificationStatus(value string) verificationStatus {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(verificationConfirmed):
		return verificationConfirmed
	case string(verificationNotConfirmed), "not confirmed", "false_positive", "false-positive":
		return verificationNotConfirmed
	case string(verificationFailed), "error":
		return verificationFailed
	default:
		return verificationInconclusive
	}
}

func findingKindOf(finding finding) findingKind {
	if finding == nil {
		return ""
	}
	return finding.Kind()
}

func findingKey(finding finding) string {
	if finding == nil {
		return ""
	}
	return finding.Key()
}

func findingPriority(finding finding) priority {
	if finding == nil {
		return priorityLow
	}
	return finding.Priority()
}

func findingTarget(finding finding) string {
	switch f := finding.(type) {
	case fingerprintFinding:
		return f.Target
	case weakpassFinding:
		if f.Result != nil {
			return f.Result.URI()
		}
	case vulnFinding:
		return vulnTarget(f.Message)
	case verificationFinding:
		return f.Target
	}
	return ""
}

func findingEvidence(finding finding) string {
	switch f := finding.(type) {
	case fingerprintFinding:
		return fmt.Sprintf("fingerprints=%s", strings.Join(parsers.NormalizeNames(f.Fingers), ","))
	case weakpassFinding:
		if f.Result != nil {
			return strings.TrimSpace(f.Result.Format(parsers.ZombieFormatWeakpassFinding))
		}
	case vulnFinding:
		return f.Message
	case verificationFinding:
		return f.Summary
	}
	return ""
}

func vulnTarget(message string) string {
	fields := strings.Fields(message)
	for i, field := range fields {
		if field == "[vuln]" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}

func oneLine(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.Join(strings.Fields(value), " ")
}
