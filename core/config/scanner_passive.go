//go:build full

package config

import passivecmd "github.com/chainreactors/aiscan/pkg/tools/passive"

func init() {
	ExtraCommands["passive"] = true
	ExtraUsageEntries = append(ExtraUsageEntries, "  passive        Run passive cyberspace recon")
	ExtraSummaryEntries = append(ExtraSummaryEntries, "passive")
	ExtraScannerUsage["passive"] = func() string { return passivecmd.New(nil).Usage() }
}
