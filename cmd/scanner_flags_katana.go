//go:build browser || full

package cmd

import katanacmd "github.com/chainreactors/aiscan/pkg/tools/katana"

func init() {
	extraScannerUsage["katana"] = func() string { return katanacmd.New().Usage() }
}
