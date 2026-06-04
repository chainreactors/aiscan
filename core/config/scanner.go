package config

import (
	"strings"

	"github.com/chainreactors/aiscan/pkg/tools/scan"
)

var ExtraCommands = map[string]bool{}

var ExtraUsageEntries []string

var ExtraSummaryEntries []string

var ExtraScannerUsage = map[string]func() string{}

type ScannerCommands struct {
	Scan    struct{} `command:"scan" description:"Run the scan pipeline"`
	Gogo    struct{} `command:"gogo" description:"Run gogo scanner"`
	Spray   struct{} `command:"spray" description:"Run spray scanner"`
	Katana  struct{} `command:"katana" description:"Run katana web crawler"`
	Zombie  struct{} `command:"zombie" description:"Run zombie weakpass scanner"`
	Neutron struct{} `command:"neutron" description:"Run neutron POC scanner"`
	Passive struct{} `command:"passive" description:"Run passive cyberspace recon"`
}

func ScannerCommandAvailable(name string) bool {
	switch name {
	case "scan", "gogo", "spray", "zombie", "neutron":
		return true
	default:
		return ExtraCommands[name]
	}
}

func ScannerUsageLines() string {
	base := `  gogo           Run gogo directly
  spray          Run spray directly
  zombie         Run zombie directly
  neutron        Run neutron directly`
	if len(ExtraUsageEntries) == 0 {
		return base
	}
	return base + "\n" + strings.Join(ExtraUsageEntries, "\n")
}

func CLICommandSummary() string {
	base := "agent, ioa serve, scan, gogo, spray, zombie, neutron"
	if len(ExtraSummaryEntries) == 0 {
		return base
	}
	return base + ", " + strings.Join(ExtraSummaryEntries, ", ")
}

func IsScannerHelpRequest(args []string) bool {
	if len(args) < 2 {
		return false
	}
	for _, arg := range args[1:] {
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

func StaticScannerUsage(name string) (string, bool) {
	switch name {
	case "scan":
		return scan.Usage(), true
	case "gogo":
		return "gogo - host, port, service, and banner discovery\nUsage: gogo [options]\n", true
	case "spray":
		return "spray - web probing, fingerprints, common files, and crawl checks\nUsage: spray [options]\n", true
	case "zombie":
		return "zombie - weak credential checks for supported services\nUsage: zombie [options]\n", true
	case "neutron":
		return "neutron - POC/vulnerability testing with nuclei-style options\nUsage: neutron -u <target> [options]\n", true
	default:
		if fn, ok := ExtraScannerUsage[name]; ok {
			return fn(), true
		}
		return "", false
	}
}
