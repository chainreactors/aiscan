package scan

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/chainreactors/aiscan/pkg/command"
	"github.com/chainreactors/aiscan/pkg/record"
	"github.com/chainreactors/aiscan/pkg/tools/scan/pipeline"
)

type AISkill struct {
	Name      string
	CapName   string
	Flag      string
	Agent     bool
	SkillFile string // embedded skill file path for LLM reference (e.g. "aiscan://skills/scan/verify.md")
	Accept    func(event) bool
	Prompt    func(event) string
	RunKey    func(event) string
	Workers   int
}

var scanAISkills = []AISkill{
	{
		Name: "verify", CapName: capAgentVerify, Flag: "", Agent: true, SkillFile: "aiscan://skills/scan/verify.md",
		Accept: shouldVerifyFinding,
		Prompt: func(e event) string {
			// Sniper findings carry CVE intelligence that needs active probing,
			// not just evidence review. Tailor the prompt accordingly.
			if ai, ok := e.Finding.(aiSkillFinding); ok && ai.Skill == "sniper" {
				return fmt.Sprintf(`CVE intelligence to actively verify against target:
- target: %s
- claimed CVEs/vulnerabilities: %s
- detail: %s

This is CVE lookup intelligence from fingerprint analysis, NOT a confirmed exploit.
Actively probe the target to determine if any of the claimed vulnerabilities are actually exploitable.
Check: service reachability, version confirmation, authentication requirements, WAF presence.
If active probing does not produce direct evidence of exploitability, return status "not_confirmed".
Use "inconclusive" only when probing cannot be completed or evaluated, not merely because access is denied.`,
					ai.Target, ai.Summary, ai.Detail)
			}
			return fmt.Sprintf(`Finding to verify:
- source: %s
- kind: %s
- priority: %s
- target: %s
- evidence: %s

Return "confirmed" only for direct evidence. Return "not_confirmed" when probing runs but does not support the claim.`,
				e.Source, findingKindOf(e.Finding), findingPriority(e.Finding),
				findingTarget(e.Finding), findingEvidence(e.Finding))
		},
		Workers: 3,
	},
	{
		Name: "sniper", CapName: capAgentSniper, Flag: "sniper", Agent: true, SkillFile: "aiscan://skills/scan/sniper.md",
		Accept: func(e event) bool {
			if e.Kind != eventFinding {
				return false
			}
			fp, ok := e.Finding.(fingerprintFinding)
			return ok && fp.Focus
		},
		Prompt: func(e event) string {
			fp, _ := e.Finding.(fingerprintFinding)
			return fmt.Sprintf(`Search for known public vulnerabilities for fingerprints on target %s.

Fingerprints: %s

Identify known CVEs (critical/high), public exploits, and remediation.`,
				fp.Target, strings.Join(fp.Fingers, ", "))
		},
		Workers: 3,
	},
	{
		Name: "deep", CapName: capAgentDeep, Flag: "deep", Agent: true, SkillFile: "aiscan://skills/scan/deep.md",
		Accept: shouldDeepTestAsset,
		Prompt: deepAssetPrompt,
		RunKey: func(e event) string {
			key := deepAssetTargetKey(e)
			if key == "" {
				return ""
			}
			return capAgentDeep + "|" + key
		},
		Workers: 1,
	},
}

func shouldVerifyFinding(e event) bool {
	if e.Kind != eventFinding || e.Finding == nil {
		return false
	}
	if e.Finding.Kind() == findingVerification {
		return false
	}
	if ai, ok := e.Finding.(aiSkillFinding); ok {
		return shouldVerifyAISkillFinding(ai)
	}
	return e.Finding.Priority().atLeast(priorityHigh)
}

func shouldVerifyAISkillFinding(finding aiSkillFinding) bool {
	switch finding.Skill {
	case "verify":
		return false
	case "sniper":
		return strings.TrimSpace(finding.Summary) != "" || strings.TrimSpace(finding.Detail) != ""
	default:
		return finding.Priority().atLeast(priorityHigh)
	}
}

func aiSkillEnabled(skill AISkill, flags flags) bool {
	if skill.Flag == "deep" {
		return flags.Deep
	}
	if flags.AI {
		return true
	}
	switch skill.Flag {
	case "sniper":
		return flags.Sniper
	case "":
		return verificationEnabled(flags.Verify)
	default:
		return false
	}
}

func buildAISkillCap(c *Command, skill AISkill) pipeline.Capability {
	workers := skill.Workers
	if c.aiConfig.Workers > 0 {
		workers = c.aiConfig.Workers
	}
	if workers <= 0 {
		workers = 3
	}

	cap := wrapCapability(skill.CapName, skill.Accept, workers,
		func(ctx context.Context, e event, emit func(event)) {
			c.runAISkill(ctx, skill, e, emit)
		},
	)
	cap.RunKey = func(pe pipeline.Event) string {
		e, ok := pe.(event)
		if !ok {
			return ""
		}
		if skill.RunKey != nil {
			return skill.RunKey(e)
		}
		if e.Finding == nil {
			return skill.CapName + "|" + e.Key()
		}
		return skill.CapName + "|" + string(e.Finding.Kind()) + "|" + e.Finding.Key()
	}
	return cap
}

func shouldDeepTestAsset(e event) bool {
	switch e.Kind {
	case eventTarget:
		if target, ok := e.Target.(webTarget); ok {
			return strings.TrimSpace(target.URL) != ""
		}
	case eventFinding:
		fp, ok := e.Finding.(fingerprintFinding)
		return ok && strings.TrimSpace(fp.Target) != "" && len(parsersNormalizeNames(fp.Fingers)) > 0
	}
	return false
}

func deepAssetTargetKey(e event) string {
	switch e.Kind {
	case eventTarget:
		if target, ok := e.Target.(webTarget); ok {
			return deepAssetKey(webAssetTarget(target.URL), target.HostHeader)
		}
	case eventFinding:
		fp, ok := e.Finding.(fingerprintFinding)
		if !ok {
			return ""
		}
		if strings.TrimSpace(fp.Target) == "" || len(parsersNormalizeNames(fp.Fingers)) == 0 {
			return ""
		}
		return deepAssetKey(assetTargetFromValues(fp.Target), "")
	}
	return ""
}

func deepAssetKey(target, hostHeader string) string {
	target = canonicalKey(target)
	if target == "" || target == canonicalKey("Scan") {
		return ""
	}
	if hostHeader = strings.ToLower(strings.TrimSpace(hostHeader)); hostHeader != "" {
		return "asset|" + target + "|host=" + hostHeader
	}
	return "asset|" + target
}

func deepAssetPrompt(e event) string {
	if target, ok := e.Target.(webTarget); ok {
		return deepWebPrompt(target, e.Source)
	}
	if fp, ok := e.Finding.(fingerprintFinding); ok {
		return deepFingerprintPrompt(fp, e.Source)
	}
	return ""
}

func deepWebPrompt(target webTarget, source string) string {
	assetURL := webAssetTarget(target.URL)
	return fmt.Sprintf(`Perform a safe deep dynamic web test for this discovered web asset. Use the browser automation tool for rendered-page and interaction testing.

Website:
- url: %s
- evidence_url: %s
- host_header: %s
- source: %s

Focus on rendered-page behavior, forms, SPA routes, client-side endpoints, network activity, and safe canary-based vulnerability checks.
Do not run destructive actions, brute force, credential stuffing, shell uploads, or state-changing workflows unless the page clearly presents a harmless search/login/test interaction.
You must call the checkpoint tool exactly once when finished. If browser testing fails, still call checkpoint with status=inconclusive and include the attempted commands or errors.
Submit one checkpoint summarizing exploitable findings, high-value observations, or why no issue was confirmed.`,
		assetURL, target.URL, target.HostHeader, source)
}

func deepFingerprintPrompt(fp fingerprintFinding, source string) string {
	fingers := parsersNormalizeNames(fp.Fingers)
	sort.Strings(fingers)
	assetTarget := assetTargetFromValues(fp.Target)
	return fmt.Sprintf(`Perform a safe deep assessment for this fingerprinted asset.

Asset:
- target: %s
- evidence_target: %s
- fingerprints: %s
- focus: %t
- source: %s

Use the fingerprint and scan evidence to assess whether the identified technology or service exposes a realistic attack surface.
For web targets, consider framework-specific routes, default panels, version exposure, and safe canary checks.
For non-web services, focus on version-specific risk, authentication surface, known misconfiguration patterns, and safe validation steps that do not brute force or mutate state.
Do not treat a fingerprint alone as a confirmed vulnerability. Return confirmed only with direct evidence. Return info for meaningful exposure, not_confirmed when no issue is supported, and inconclusive when the evidence is insufficient.
You must call the checkpoint tool exactly once with kind="deep".`,
		assetTarget, fp.Target, strings.Join(fingers, ", "), fp.Focus, source)
}

func (c *Command) runAISkill(ctx context.Context, skill AISkill, e event, emit func(event)) {
	if c.agentFunc == nil {
		c.logger.Debugf("scan capability=%s status=skipped reason=agent_func_unconfigured", skill.CapName)
		return
	}

	timeout := c.aiConfig.Timeout
	if timeout <= 0 {
		timeout = 300
	}
	skillCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	prompt := skill.Prompt(e)
	if skill.SkillFile != "" {
		prompt = fmt.Sprintf("Read %s for instructions before starting.\n\n%s", skill.SkillFile, prompt)
	}
	browserEvidence := ""
	if skill.Name == "deep" && c.deepBrowser != nil {
		targetURL := deepWebURL(e)
		if targetURL != "" {
			evidence, err := c.deepBrowser(skillCtx, targetURL)
			if err != nil {
				browserEvidence = fmt.Sprintf("Browser collection error: %v", err)
			} else {
				browserEvidence = strings.TrimSpace(evidence)
			}
			if browserEvidence != "" {
				prompt = appendDeepBrowserEvidence(prompt, browserEvidence)
			}
		}
	}
	start := time.Now()

	agentResult, agentErr := c.agentFunc(skillCtx, prompt, "", c.aiConfig.Model, 1600)
	duration := time.Since(start)
	if agentErr != nil {
		c.logger.Debugf("scan capability=%s status=failed error=%q", skill.CapName, agentErr)
		emitAISkillResponse(skill, e, "failed", "LLM call failed", agentErr.Error(), emit)
		return
	}
	if agentResult == nil || agentResult.Checkpoint == nil || isMissingAISkillCheckpoint(agentResult.Checkpoint) {
		c.logger.Debugf("scan capability=%s status=failed error=no_checkpoint", skill.CapName)
		raw := ""
		if agentResult != nil {
			raw = strings.TrimSpace(agentResult.Raw)
		}
		if c.recorder != nil {
			c.recorder.AITurn(skill.Name, 1, prompt, raw, nil, duration, record.TokenUsage{})
		}
		emitAISkillResponse(skill, e, "response", "agent response without checkpoint", raw, emit)
		return
	}
	cp := agentResult.Checkpoint
	c.hydrateAISkillCheckpoint(skill, e, cp)

	if cp.Title == "" && cp.Content == "" {
		return
	}

	if c.recorder != nil {
		c.recorder.AITurn(skill.Name, 1, prompt, agentResult.Raw, nil, duration, record.TokenUsage{})
	}

	status := checkpointStatus(cp)
	target := checkpointTarget(cp)

	originalKey := ""
	originalKind := findingKind("")
	if e.Finding != nil {
		originalKey = e.Finding.Key()
		originalKind = e.Finding.Kind()
	}

	if c.recorder != nil {
		c.recorder.AISkill(skill.Name, target, status, cp.Title, cp.Content, duration)
	}

	emit(findingEvent(skill.CapName, aiSkillFinding{
		Skill:        skill.Name,
		Target:       target,
		Status:       status,
		Summary:      cp.Title,
		Detail:       cp.Content,
		OriginalKey:  originalKey,
		OriginalKind: originalKind,
	}))
}

func isMissingAISkillCheckpoint(cp *command.CheckpointResult) bool {
	if cp == nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(cp.Title), "agent did not submit checkpoint") &&
		strings.TrimSpace(cp.Content) == "" &&
		strings.TrimSpace(cp.Kind) == "" &&
		strings.TrimSpace(cp.Target) == ""
}

func emitAISkillResponse(skill AISkill, e event, status, summary, detail string, emit func(event)) {
	target := aiSkillEventTarget(e)
	originalKey := ""
	originalKind := findingKind("")
	if e.Finding != nil {
		originalKey = e.Finding.Key()
		originalKind = e.Finding.Kind()
	}
	raw := strings.TrimSpace(detail)
	if strings.TrimSpace(summary) == "" && raw != "" {
		summary = firstLine(raw)
	}
	emit(findingEvent(skill.CapName, aiSkillResponse{
		Skill:        skill.Name,
		Target:       target,
		Status:       status,
		Summary:      summary,
		Detail:       raw,
		Raw:          raw,
		OriginalKey:  originalKey,
		OriginalKind: originalKind,
	}))
}

func (c *Command) hydrateAISkillCheckpoint(skill AISkill, e event, cp *command.CheckpointResult) {
	if cp == nil {
		return
	}
	if strings.TrimSpace(cp.Kind) == "" {
		cp.Kind = skill.Name
	}
	if strings.TrimSpace(cp.Target) == "" {
		cp.Target = aiSkillEventTarget(e)
	}
}

func aiSkillEventTarget(e event) string {
	if e.Target != nil {
		switch target := e.Target.(type) {
		case webTarget:
			if strings.TrimSpace(target.URL) != "" {
				return strings.TrimSpace(target.URL)
			}
		case webProbeTarget:
			if target.Result != nil && strings.TrimSpace(target.Result.UrlString) != "" {
				return strings.TrimSpace(target.Result.UrlString)
			}
		case serviceTarget:
			if target.Result != nil {
				return target.Result.GetTarget()
			}
		}
		if e.Target.RawInput() != "" {
			return e.Target.RawInput()
		}
		return e.Target.Key()
	}
	if e.Finding != nil {
		if target := findingTarget(e.Finding); target != "" {
			return target
		}
		return e.Finding.Key()
	}
	return ""
}

func deepWebURL(e event) string {
	if target, ok := e.Target.(webTarget); ok {
		return strings.TrimSpace(webAssetTarget(target.URL))
	}
	return ""
}

func parsersNormalizeNames(values []string) []string {
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			name := strings.ToLower(strings.TrimSpace(item))
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			normalized = append(normalized, name)
		}
	}
	return normalized
}

func appendDeepBrowserEvidence(prompt, evidence string) string {
	return fmt.Sprintf(`%s

aiscan has already run browser automation for this target. Analyze the collected Playwright evidence below; do not say you need to inspect the site before producing a result.

Browser evidence:
%s

Based on this evidence, call checkpoint with kind="deep" only when you have a clear final assessment. If you cannot call the tool, write a concise natural-language response; aiscan will render it as agent response, not as a checkpoint finding.`, prompt, evidence)
}

func checkpointStatus(cp *command.CheckpointResult) string {
	if cp.Status != "" {
		return cp.Status
	}
	return "inconclusive"
}

func checkpointTarget(cp *command.CheckpointResult) string {
	return cp.Target
}

func (c *Command) generateAIReport(ctx context.Context, coll *collector) string {
	if c.reportFunc == nil {
		return coll.ReportMarkdown()
	}

	// Use ReportMarkdown so the LLM sees verification annotations:
	// ~~strikethrough~~ for not_confirmed, **[verified]** for confirmed.
	// PlainText lacks these annotations, causing the LLM to treat
	// unverified fingerprints/sniper CVE lookups as confirmed vulns.
	scanData := coll.ReportMarkdown()
	if scanData == "" {
		scanData = coll.TerminalString(false)
	}

	prompt := fmt.Sprintf(`Read aiscan://skills/report/SKILL.md for report formatting instructions before starting.

Generate a security scan report from the following scan results and write it to a file with a descriptive name (e.g., report_<target>_<date>.md).

After writing the file, output the report content to the user as well.

Scan results:
%s`, scanData)

	timeout := c.aiConfig.Timeout
	if timeout <= 0 {
		timeout = 300
	}
	reportCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	result, err := c.reportFunc(reportCtx, prompt, "", c.aiConfig.Model, 4000)
	if err != nil {
		c.logger.Debugf("scan report skill failed: %v", err)
		return coll.ReportMarkdown()
	}

	result = strings.TrimSpace(result)
	if result == "" {
		return coll.ReportMarkdown()
	}
	return result + "\n"
}
