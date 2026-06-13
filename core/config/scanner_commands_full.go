//go:build full

package config

type ScannerCommands struct {
	Scan     struct{} `command:"scan" description:"Run the scan pipeline"`
	Gogo     struct{} `command:"gogo" description:"Run gogo scanner"`
	Spray    struct{} `command:"spray" description:"Run spray scanner"`
	Katana   struct{} `command:"katana" description:"Run katana web crawler"`
	Zombie   struct{} `command:"zombie" description:"Run zombie weakpass scanner"`
	Neutron  struct{} `command:"neutron" description:"Run neutron POC scanner"`
	Cyberhub struct{} `command:"cyberhub" description:"Search Cyberhub fingerprints and POCs"`
	Passive  struct{} `command:"passive" description:"Run passive cyberspace recon"`
}
