//go:build full

package cmd

func init() {
	extraCommands["passive"] = true
	extraUsageEntries = append(extraUsageEntries, "  passive        Run passive cyberspace recon")
	extraSummaryEntries = append(extraSummaryEntries, "passive")
}
