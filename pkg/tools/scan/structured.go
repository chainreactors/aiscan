package scan

import (
	"time"

	"github.com/chainreactors/aiscan/pkg/output"
)

func (c *collector) StructuredResult() *output.Result {
	c.mu.Lock()
	defer c.mu.Unlock()

	stats := c.statsSnapshotLocked()
	result := &output.Result{
		Summary: output.Summary{
			Targets:  stats.Inputs,
			Services: len(c.gogoResults),
			Webs:     len(c.seenWeb),
			Probes:   len(c.sprayResults),
			Risks:    len(c.zombieResults),
			Vulns:    len(c.neutronMatches),
			Verified: c.confirmedVerificationCountLocked(),
			Errors:   len(c.errors),
			Tasks:    stats.Tasks,
			Requests: stats.Requests,
			Duration: stats.Duration().Round(time.Millisecond).String(),
			StartedAt:  stats.StartedAt,
			FinishedAt: stats.FinishedAt,
		},
	}

	for _, item := range c.gogoResults {
		if item == nil {
			continue
		}
		result.Services = append(result.Services, item)
	}
	for _, item := range c.sprayResults {
		if item.Result == nil {
			continue
		}
		result.WebProbes = append(result.WebProbes, item.Result)
	}
	for _, item := range c.zombieResults {
		if item == nil {
			continue
		}
		result.Risks = append(result.Risks, item)
	}
	for _, item := range c.neutronMatches {
		if item.Result == nil {
			continue
		}
		result.Vulns = append(result.Vulns, item.Result)
	}
	for _, item := range c.verifications {
		result.AI = append(result.AI, output.AIFinding{
			Kind:         string(item.Finding.Kind()),
			Target:       item.Finding.Target,
			Priority:     string(item.Finding.Priority()),
			Status:       string(item.Finding.Status),
			Summary:      item.Finding.Summary,
			Evidence:     item.Finding.Evidence,
			Source:       item.Source,
			OriginalKind: string(item.Finding.OriginalKind),
			OriginalKey:  item.Finding.OriginalKey,
			Raw:          verificationOutput(item.Finding),
		})
	}
	for _, item := range c.aiSkillResults {
		result.AI = append(result.AI, output.AIFinding{
			Kind:         string(item.Finding.Kind()),
			Target:       item.Finding.Target,
			Priority:     string(item.Finding.Priority()),
			Status:       item.Finding.Status,
			Summary:      item.Finding.Summary,
			Detail:       item.Finding.Detail,
			Skill:        item.Finding.Skill,
			Source:       item.Source,
			OriginalKind: string(item.Finding.OriginalKind),
			OriginalKey:  item.Finding.OriginalKey,
			Raw:          aiSkillOutput(item.Finding),
		})
	}
	for _, item := range c.aiSkillResponses {
		result.AI = append(result.AI, output.AIFinding{
			Kind:         string(item.Response.Kind()),
			Target:       item.Response.Target,
			Priority:     string(item.Response.Priority()),
			Status:       item.Response.Status,
			Summary:      item.Response.Summary,
			Detail:       item.Response.Detail,
			Skill:        item.Response.Skill,
			Source:       item.Source,
			OriginalKind: string(item.Response.OriginalKind),
			OriginalKey:  item.Response.OriginalKey,
			Raw:          aiSkillResponseOutput(item.Response),
		})
	}
	for _, message := range c.errors {
		result.Errors = append(result.Errors, output.Error{Message: message})
	}

	result.Assets = AggregateStructuredResult(result)
	return result
}
