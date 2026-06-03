package cmd

import "strings"

// extraCommands tracks optional scanner commands registered via build-tagged
// init() functions. Core commands (scan, cyberhub, gogo, spray, zombie,
// neutron) are always available; optional ones (katana, passive) register
// themselves here when their build tag is active.
var extraCommands = map[string]bool{}

// extraUsageLines collects per-command usage lines registered from
// build-tagged init() functions, appended to the base usage block.
var extraUsageEntries []string

// extraSummaryEntries collects command names registered from build-tagged
// init() functions, appended to the CLI summary.
var extraSummaryEntries []string

type scannerCommands struct {
	Scan     struct{} `command:"scan" description:"Run the scan pipeline"`
	Cyberhub struct{} `command:"cyberhub" description:"Search Cyberhub fingerprints and POCs"`
	Gogo     struct{} `command:"gogo" description:"Run gogo scanner"`
	Spray    struct{} `command:"spray" description:"Run spray scanner"`
	Katana   struct{} `command:"katana" description:"Run katana web crawler"`
	Zombie   struct{} `command:"zombie" description:"Run zombie weakpass scanner"`
	Neutron  struct{} `command:"neutron" description:"Run neutron POC scanner"`
	Passive  struct{} `command:"passive" description:"Run passive cyberspace recon"`
}

func scannerCommandAvailable(name string) bool {
	switch name {
	case "scan", "cyberhub", "gogo", "spray", "zombie", "neutron":
		return true
	default:
		return extraCommands[name]
	}
}

func scannerUsageLines() string {
	base := `  gogo           Run gogo directly
  spray          Run spray directly
  zombie         Run zombie directly
  neutron        Run neutron directly`
	if len(extraUsageEntries) == 0 {
		return base
	}
	return base + "\n" + strings.Join(extraUsageEntries, "\n")
}

func cliCommandSummary() string {
	base := "agent, ioa serve, scan, cyberhub, gogo, spray, zombie, neutron"
	if len(extraSummaryEntries) == 0 {
		return base
	}
	return base + ", " + strings.Join(extraSummaryEntries, ", ")
}
