//go:build browser || full

package config

import katanacmd "github.com/chainreactors/aiscan/pkg/tools/katana"

func init() {
	ExtraCommands["katana"] = true
	ExtraUsageEntries = append(ExtraUsageEntries, "  katana         Run katana web crawler")
	ExtraSummaryEntries = append(ExtraSummaryEntries, "katana")
	ExtraScannerUsage["katana"] = func() string { return katanacmd.New().Usage() }
}
