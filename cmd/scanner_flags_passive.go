//go:build full

package cmd

import passivecmd "github.com/chainreactors/aiscan/pkg/tools/passive"

func init() {
	extraScannerUsage["passive"] = func() string { return passivecmd.New(nil).Usage() }
}
