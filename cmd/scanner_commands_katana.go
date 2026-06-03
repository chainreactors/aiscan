//go:build browser || full

package cmd

func init() {
	extraCommands["katana"] = true
	extraUsageEntries = append(extraUsageEntries, "  katana         Run katana web crawler")
	extraSummaryEntries = append(extraSummaryEntries, "katana")
}
