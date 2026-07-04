package manager

// TunnelStatus is the API-facing view of the hub's outbound SSH reverse tunnel.
// The tunnel lets a hub behind NAT (no public IP) still be reached by cloud
// nodes: the hub auto-provisions a small relay VM, dials out to it over SSH, and
// binds a remote-forward port on the relay's public IP that nodes call back
// through. That public IP:port is filled into the deploy PublicURL.
type TunnelStatus struct {
	Backend   string `json:"backend"`         // "ssh"
	Available bool   `json:"available"`       // hub local addr known => tunnel usable
	Enabled   bool   `json:"enabled"`         // desired-up (persisted, auto-reconnects on boot)
	Running   bool   `json:"running"`         // supervisor active or relay provisioning
	Connected bool   `json:"connected"`       // ssh session currently established
	Phase     string `json:"phase,omitempty"` // provisioning | connecting | connected | error
	RelayIP   string `json:"relay_ip,omitempty"`
	Provider  string `json:"provider,omitempty"`
	Region    string `json:"region,omitempty"`
	PublicURL string `json:"public_url,omitempty"`
	Error     string `json:"error,omitempty"`
	StartedAt string `json:"started_at,omitempty"` // RFC3339, when the supervisor started
}
