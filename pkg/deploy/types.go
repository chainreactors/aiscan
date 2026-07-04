// Package deploy holds the persisted state for cloud auto-deployment of aiscan
// agent nodes: cloud credentials, deployment records, and the cloud-init
// generator. The cloud AccessKeySecret lives only here on the hub and is never
// part of the agent DistributeConfig.
package deploy

import "time"

// Deployment lifecycle states.
const (
	StatusPending  = "pending"  // instances requested, awaiting cloud
	StatusActive   = "active"   // instances launched
	StatusRecycled = "recycled" // instances terminated and reclaimed
	StatusFailed   = "failed"   // launch failed
)

// Deployment progress phases. These are finer-grained than Status and are safe
// to show directly in the UI as "what the deployment is doing now".
const (
	PhasePreparing           = "preparing"
	PhaseEnsuringNetwork     = "ensuring_network"
	PhaseLaunchingInstances  = "launching_instances"
	PhaseWaitingRegistration = "waiting_registration"
	PhaseReady               = "ready"
	PhaseRecycling           = "recycling"
	PhaseRecycled            = "recycled"
	PhaseFailed              = "failed"
)

// CloudCredential is one cloud account's AK/SK. Secret stays on the hub.
type CloudCredential struct {
	ID              string `yaml:"id" json:"id"`
	Name            string `yaml:"name" json:"name"`
	Provider        string `yaml:"provider" json:"provider"` // aliyun | tencent
	AccessKeyID     string `yaml:"access_key_id" json:"access_key_id"`
	AccessKeySecret string `yaml:"access_key_secret" json:"-"` // never serialized to JSON
	DefaultRegion   string `yaml:"default_region" json:"default_region"`
}

// DeployNode tracks one launched instance and its intended IOA identity.
type DeployNode struct {
	InstanceID string `yaml:"instance_id" json:"instance_id"`
	NodeName   string `yaml:"node_name" json:"node_name"`
	PublicIP   string `yaml:"public_ip,omitempty" json:"public_ip,omitempty"`
	PrivateIP  string `yaml:"private_ip,omitempty" json:"private_ip,omitempty"`
	Status     string `yaml:"status,omitempty" json:"status,omitempty"`
}

// AutoNetwork records network resources aiscan auto-created for a deploy (when
// the target region had none) so recycling can tear down exactly those — never
// the user's pre-existing resources. A field is set only when aiscan created
// that resource; it is cleared once successfully reclaimed.
type AutoNetwork struct {
	VPCID           string `yaml:"vpc_id,omitempty" json:"vpc_id,omitempty"`
	VSwitchID       string `yaml:"vswitch_id,omitempty" json:"vswitch_id,omitempty"`
	SecurityGroupID string `yaml:"security_group_id,omitempty" json:"security_group_id,omitempty"`
}

// Empty reports whether aiscan created no network resource for this deploy.
func (n AutoNetwork) Empty() bool {
	return n.VPCID == "" && n.VSwitchID == "" && n.SecurityGroupID == ""
}

// DeployRecord is one batch deployment.
type DeployRecord struct {
	ID              string       `yaml:"id" json:"id"`
	CloudID         string       `yaml:"cloud_id" json:"cloud_id"`
	Provider        string       `yaml:"provider" json:"provider"`
	Region          string       `yaml:"region" json:"region"`
	Space           string       `yaml:"space" json:"space"`
	Nodes           []DeployNode `yaml:"nodes" json:"nodes"`
	Status          string       `yaml:"status" json:"status"`
	Phase           string       `yaml:"phase,omitempty" json:"phase,omitempty"`
	DesiredCount    int          `yaml:"desired_count,omitempty" json:"desired_count,omitempty"`
	TTLMinutes      int          `yaml:"ttl_minutes,omitempty" json:"ttl_minutes,omitempty"`
	RecycleWhenIdle bool         `yaml:"recycle_when_idle,omitempty" json:"recycle_when_idle,omitempty"`
	CreatedAt       time.Time    `yaml:"created_at" json:"created_at"`
	UpdatedAt       time.Time    `yaml:"updated_at,omitempty" json:"updated_at,omitempty"`
	RecycledAt      *time.Time   `yaml:"recycled_at,omitempty" json:"recycled_at,omitempty"`
	Error           string       `yaml:"error,omitempty" json:"error,omitempty"`
	// AutoNet is the network aiscan provisioned for this deploy (empty when it
	// reused existing resources). Reclaimed on recycle.
	AutoNet AutoNetwork `yaml:"auto_network,omitempty" json:"auto_network,omitempty"`
}

// InstanceIDs returns the cloud instance IDs that are still considered live.
func (r *DeployRecord) InstanceIDs() []string {
	ids := make([]string, 0, len(r.Nodes))
	for _, n := range r.Nodes {
		if n.InstanceID != "" {
			ids = append(ids, n.InstanceID)
		}
	}
	return ids
}

// SSHTunnel is the persisted config + state of the hub's outbound SSH reverse
// tunnel. The hub auto-provisions a small relay VM (via one of the stored cloud
// credentials), injects a generated public key, and keeps a reverse tunnel alive
// so nodes can dial a NAT'd hub back through the relay's PublicIP:RemotePort.
// PrivateKey/AutoNet stay on the hub and are never serialized to the API (json:"-").
type SSHTunnel struct {
	Enabled    bool        `yaml:"enabled" json:"enabled"`                             // desired-up; auto-reconnect on hub boot
	CloudID    string      `yaml:"cloud_id" json:"cloud_id,omitempty"`                 // credential that owns the relay
	Provider   string      `yaml:"provider,omitempty" json:"provider,omitempty"`       // provider of that credential
	Region     string      `yaml:"region,omitempty" json:"region,omitempty"`           // region the relay lives in
	InstanceID string      `yaml:"instance_id,omitempty" json:"instance_id,omitempty"` // relay instance id
	PublicIP   string      `yaml:"public_ip,omitempty" json:"public_ip,omitempty"`     // relay public IP nodes dial
	SSHUser    string      `yaml:"ssh_user,omitempty" json:"ssh_user,omitempty"`       // relay login user (default root)
	SSHPort    int         `yaml:"ssh_port,omitempty" json:"ssh_port,omitempty"`       // relay sshd port (default 22)
	RemotePort int         `yaml:"remote_port,omitempty" json:"remote_port,omitempty"` // port bound on the relay
	PrivateKey string      `yaml:"private_key,omitempty" json:"-"`                     // PEM key the hub authenticates with (secret)
	AutoNet    AutoNetwork `yaml:"auto_network,omitempty" json:"-"`                    // network auto-created for the relay
	CreatedAt  time.Time   `yaml:"created_at,omitempty" json:"created_at,omitempty"`
}

// Provisioned reports whether a relay instance has been created for this tunnel.
func (t *SSHTunnel) Provisioned() bool { return t != nil && t.InstanceID != "" }

// State is the full persisted deploy state for the hub.
type State struct {
	PublicURL string `yaml:"public_url" json:"public_url"`
	// SSHTunnel is the hub's outbound SSH reverse tunnel and its auto-provisioned
	// relay (nil until the tunnel is first enabled). It lets a hub behind NAT be
	// reached by cloud nodes through the relay's public IP.
	SSHTunnel *SSHTunnel        `yaml:"ssh_tunnel,omitempty" json:"ssh_tunnel,omitempty"`
	Clouds    []CloudCredential `yaml:"clouds" json:"clouds"`
	Deploys   []DeployRecord    `yaml:"deploys" json:"deploys"`
}

// EnsureSSHTunnel returns the SSH tunnel config, creating an empty one if unset.
func (s *State) EnsureSSHTunnel() *SSHTunnel {
	if s.SSHTunnel == nil {
		s.SSHTunnel = &SSHTunnel{}
	}
	return s.SSHTunnel
}

// FindCloud returns the credential with the given id, or nil.
func (s *State) FindCloud(id string) *CloudCredential {
	for i := range s.Clouds {
		if s.Clouds[i].ID == id {
			return &s.Clouds[i]
		}
	}
	return nil
}

// FindDeploy returns the deploy record with the given id, or nil.
func (s *State) FindDeploy(id string) *DeployRecord {
	for i := range s.Deploys {
		if s.Deploys[i].ID == id {
			return &s.Deploys[i]
		}
	}
	return nil
}
