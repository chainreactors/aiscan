package scan

import (
	"fmt"
	"strings"
)

func formatPlainText(d *collector, fileLines []string) string {
	d.mu.Lock()
	defer d.mu.Unlock()

	var sb strings.Builder
	for _, line := range fileLines {
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	sb.WriteString(formatScanSummaryLine(d, d.statsSnapshotLocked(), false))
	return sb.String()
}

func formatPlainTextWithFindings(d *collector, fileLines []string) string {
	d.mu.Lock()
	defer d.mu.Unlock()

	var sb strings.Builder
	for _, line := range fileLines {
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	sb.WriteString(formatScanSummaryLine(d, d.statsSnapshotLocked(), false))

	if len(d.aiSkillResults) == 0 {
		return sb.String()
	}

	sb.WriteString("\n--- Findings ---\n\n")
	for _, item := range d.aiSkillResults {
		f := item.Finding
		fmt.Fprintf(&sb, "[%s] %s\n", f.Skill, f.Status)
		if f.Target != "" {
			fmt.Fprintf(&sb, "  target: %s\n", f.Target)
		}
		if f.Summary != "" {
			fmt.Fprintf(&sb, "  title:  %s\n", f.Summary)
		}
		if f.Detail != "" {
			for _, line := range strings.Split(strings.TrimSpace(f.Detail), "\n") {
				fmt.Fprintf(&sb, "  | %s\n", line)
			}
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}
