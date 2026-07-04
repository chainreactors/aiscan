package manager

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chainreactors/aiscan/pkg/cloud"
	"github.com/chainreactors/aiscan/pkg/deploy"
	"github.com/chainreactors/aiscan/pkg/deploy/local"
	"github.com/chainreactors/aiscan/pkg/deploy/tunnel"
)

// DeployManager orchestrates cloud auto-deployment of aiscan agent nodes.
// Cloud AccessKeySecret stays here on the hub and is never distributed.
type DeployManager struct {
	store      deploy.DeployStore
	pool       deploy.AgentLister // live registered-agent snapshots (adapts *AgentPool)
	binaryPath string             // explicit aiscan binary to serve; empty => os.Executable()
	ioaToken   string             // hub IOA access key, embedded in node IOA URLs
	mu         sync.Mutex

	// SSH reverse tunnel state. The relay is auto-provisioned via a stored cloud
	// credential; the supervisor keeps the tunnel alive. Guarded by mu.
	localURL    string             // hub loopback addr the relay forwards to (empty => unavailable)
	tun         *tunnel.Supervisor // active supervisor (nil when stopped)
	tunBusy     bool               // relay provisioning / connecting in progress
	tunErr      string             // last start/provision error
	tunOpCancel context.CancelFunc // cancels the in-flight provisioning/connect operation
	tunOpDone   chan struct{}      // closed when the in-flight operation exits

	// Locally-spawned agent subprocesses (hub-hosted nodes), launched and tracked
	// by an independent service (its own lock, never touches the deploy store).
	local *local.Service

	// Bootstrap progress self-reported by nodes before they register (keyed by
	// node name). Populated by the ungated /api/agent/progress endpoint and
	// surfaced on orphan nodes. Guarded by progMu, independent of mu.
	progMu sync.Mutex
	prog   map[string]nodeProgress

	// newProvider builds a cloud client; overridable in tests.
	newProvider func(cloud.Credential) (cloud.Provider, error)
}

// NewDeployManager builds a manager. ioaToken is the hub's embedded-IOA access
// key; binaryPath optionally overrides the binary served to new nodes.
// noAgents is a no-op AgentLister used when no pool is wired (e.g. in tests), so
// snapshot lookups are always safe to call.
type noAgents struct{}

func (noAgents) AgentSnapshots() []deploy.AgentSnapshot { return nil }

func NewDeployManager(store deploy.DeployStore, pool deploy.AgentLister, ioaToken, binaryPath string) *DeployManager {
	if pool == nil {
		pool = noAgents{}
	}
	m := &DeployManager{
		store:       store,
		pool:        pool,
		ioaToken:    ioaToken,
		binaryPath:  binaryPath,
		newProvider: cloud.NewProvider,
	}
	m.local = local.NewService(ioaToken, binaryPath, m.hubURL, m.localLookup)
	return m
}

// providerCred maps a stored credential to the cloud.Credential used to build a
// provider client, overriding the region. The AccessKeySecret stays on the hub.
func providerCred(c deploy.CloudCredential, region string) cloud.Credential {
	return cloud.Credential{
		Provider:        c.Provider,
		AccessKeyID:     c.AccessKeyID,
		AccessKeySecret: c.AccessKeySecret,
		Region:          region,
	}
}

// autoNet records the network resources aiscan auto-created, for later teardown.
func autoNet(n cloud.NetworkDefaults) deploy.AutoNetwork {
	return deploy.AutoNetwork{VPCID: n.VPCID, VSwitchID: n.VSwitchID, SecurityGroupID: n.SecurityGroupID}
}

// netDefaults maps recorded auto-network back to the shape a provider teardown wants.
func netDefaults(a deploy.AutoNetwork) cloud.NetworkDefaults {
	return cloud.NetworkDefaults{VPCID: a.VPCID, VSwitchID: a.VSwitchID, SecurityGroupID: a.SecurityGroupID}
}

func (m *DeployManager) BinaryPath() string { return m.binaryPath }

// --- views (masked / API-facing) ---

// CloudCredentialView is the masked public view of a credential.
type CloudCredentialView struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Provider         string `json:"provider"`
	AccessKeyID      string `json:"access_key_id"` // masked
	DefaultRegion    string `json:"default_region"`
	SecretConfigured bool   `json:"secret_configured"`
}

// DeployNodeView augments a node with its live registration/instance state.
type DeployNodeView struct {
	deploy.DeployNode
	Registered bool              `json:"registered"` // currently connected to the web AgentPool
	AgentID    string            `json:"agent_id,omitempty"`
	Busy       bool              `json:"busy,omitempty"`
	Progress   *NodeProgressView `json:"progress,omitempty"` // bootstrap progress, orphan nodes only
}

// nodeProgress is a node's last self-reported bootstrap step (pre-registration).
type nodeProgress struct {
	phase string
	bytes int64
	total int64
	at    time.Time
}

// NodeProgressView is the API-facing form of a node's bootstrap progress.
type NodeProgressView struct {
	Phase  string `json:"phase"`           // booting|downloading|installing|starting
	Bytes  int64  `json:"bytes,omitempty"` // agent binary bytes downloaded so far
	Total  int64  `json:"total,omitempty"` // agent binary total size (0 if unknown)
	AgeSec int    `json:"age_sec"`         // seconds since this update (staleness)
}

// RecordNodeProgress stores a node's self-reported bootstrap phase. token must
// match the hub's embedded IOA key (which every node carries); returns false on
// a token mismatch or missing node/phase so the handler can reject it.
func (m *DeployManager) RecordNodeProgress(token, node, phase string, bytes, total int64) bool {
	if m.ioaToken != "" && token != m.ioaToken {
		return false
	}
	node = strings.TrimSpace(node)
	if node == "" || strings.TrimSpace(phase) == "" {
		return false
	}
	m.progMu.Lock()
	if m.prog == nil {
		m.prog = map[string]nodeProgress{}
	}
	m.prog[node] = nodeProgress{phase: phase, bytes: bytes, total: total, at: time.Now()}
	m.progMu.Unlock()
	return true
}

// nodeProgressView returns a node's last self-reported progress, or nil if none.
func (m *DeployManager) nodeProgressView(node string) *NodeProgressView {
	m.progMu.Lock()
	defer m.progMu.Unlock()
	p, ok := m.prog[node]
	if !ok {
		return nil
	}
	return &NodeProgressView{
		Phase:  p.phase,
		Bytes:  p.bytes,
		Total:  p.total,
		AgeSec: int(time.Since(p.at) / time.Second),
	}
}

// clearNodeProgress drops stored progress for the given nodes (called on recycle
// so a redeploy that reuses a node name starts clean and the map stays bounded).
func (m *DeployManager) clearNodeProgress(nodes ...string) {
	m.progMu.Lock()
	for _, n := range nodes {
		delete(m.prog, n)
	}
	m.progMu.Unlock()
}

// DeployRecordView augments a record with cross-referenced live state.
type DeployRecordView struct {
	deploy.DeployRecord
	Nodes           []DeployNodeView `json:"nodes"`
	RegisteredCount int              `json:"registered_count"`
	Orphans         int              `json:"orphans"` // launched but never registered
}

func maskAK(ak string) string {
	if len(ak) <= 8 {
		if ak == "" {
			return ""
		}
		return ak[:1] + "****"
	}
	return ak[:4] + "****" + ak[len(ak)-4:]
}

// credentialView is the masked, API-facing view of a stored credential.
func credentialView(c deploy.CloudCredential) CloudCredentialView {
	return CloudCredentialView{
		ID:               c.ID,
		Name:             c.Name,
		Provider:         c.Provider,
		AccessKeyID:      maskAK(c.AccessKeyID),
		DefaultRegion:    c.DefaultRegion,
		SecretConfigured: c.AccessKeySecret != "",
	}
}

// --- credentials ---

func (m *DeployManager) ListCredentials(ctx context.Context) ([]CloudCredentialView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]CloudCredentialView, 0, len(state.Clouds))
	for _, c := range state.Clouds {
		views = append(views, credentialView(c))
	}
	return views, nil
}

// SaveCredentialInput is the upsert payload from the API.
type SaveCredentialInput struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Provider        string `json:"provider"`
	AccessKeyID     string `json:"access_key_id"`
	AccessKeySecret string `json:"access_key_secret"`
	DefaultRegion   string `json:"default_region"`
}

// SaveCredential upserts a credential. An empty AccessKeySecret on an existing
// credential preserves the stored secret (same pattern as config secrets). A
// masked AccessKeyID (containing "****") is treated as "unchanged".
func (m *DeployManager) SaveCredential(ctx context.Context, in SaveCredentialInput) (CloudCredentialView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.store.Load(ctx)
	if err != nil {
		return CloudCredentialView{}, err
	}
	if strings.TrimSpace(in.Provider) == "" {
		return CloudCredentialView{}, fmt.Errorf("provider is required")
	}
	if _, err := deploy.ProviderKind(in.Provider); err != nil {
		return CloudCredentialView{}, err
	}

	var cred *deploy.CloudCredential
	if in.ID != "" {
		cred = state.FindCloud(in.ID)
	}
	if cred == nil {
		state.Clouds = append(state.Clouds, deploy.CloudCredential{ID: deploy.NewID("cloud-")})
		cred = &state.Clouds[len(state.Clouds)-1]
	}
	cred.Name = deploy.FirstNonEmpty(in.Name, cred.Name, in.Provider)
	cred.Provider = strings.ToLower(strings.TrimSpace(in.Provider))
	cred.DefaultRegion = deploy.FirstNonEmpty(in.DefaultRegion, cred.DefaultRegion)
	if in.AccessKeyID != "" && !strings.Contains(in.AccessKeyID, "****") {
		cred.AccessKeyID = in.AccessKeyID
	}
	if strings.TrimSpace(in.AccessKeySecret) != "" {
		cred.AccessKeySecret = in.AccessKeySecret
	}
	if cred.AccessKeyID == "" {
		return CloudCredentialView{}, fmt.Errorf("access_key_id is required")
	}
	if cred.AccessKeySecret == "" {
		return CloudCredentialView{}, fmt.Errorf("access_key_secret is required")
	}
	if err := m.store.Save(ctx, state); err != nil {
		return CloudCredentialView{}, err
	}
	return credentialView(*cred), nil
}

// ListRegionsInput selects how to authenticate the DescribeRegions call: either
// against a saved credential (CloudID) or against AK/SK typed into the add form
// before the credential is persisted.
type ListRegionsInput struct {
	CloudID         string `json:"cloud_id"`
	Provider        string `json:"provider"`
	AccessKeyID     string `json:"access_key_id"`
	AccessKeySecret string `json:"access_key_secret"`
}

// ListRegions returns the regions selectable for a credential. The network call
// runs outside the lock; only credential resolution is guarded.
func (m *DeployManager) ListRegions(ctx context.Context, in ListRegionsInput) ([]cloud.Region, error) {
	cred, err := m.resolveRegionsCredential(ctx, in)
	if err != nil {
		return nil, err
	}
	prov, err := m.newProvider(cred)
	if err != nil {
		return nil, err
	}
	return prov.ListRegions(ctx)
}

// resolveRegionsCredential builds the cloud.Credential to authenticate a region
// lookup. A saved CloudID supplies stored AK/SK (and provider) unless the caller
// passed fresh, unmasked values; the add-credential form passes provider+AK/SK
// directly. The stored secret is never returned to the caller.
func (m *DeployManager) resolveRegionsCredential(ctx context.Context, in ListRegionsInput) (cloud.Credential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.store.Load(ctx)
	if err != nil {
		return cloud.Credential{}, err
	}
	provider := strings.TrimSpace(in.Provider)
	ak := strings.TrimSpace(in.AccessKeyID)
	sk := strings.TrimSpace(in.AccessKeySecret)
	region := ""
	if in.CloudID != "" {
		stored := state.FindCloud(in.CloudID)
		if stored == nil {
			return cloud.Credential{}, fmt.Errorf("cloud credential %s not found", in.CloudID)
		}
		if provider == "" {
			provider = stored.Provider
		}
		region = stored.DefaultRegion
		if ak == "" || strings.Contains(ak, "****") {
			ak = stored.AccessKeyID
		}
		if sk == "" {
			sk = stored.AccessKeySecret
		}
	}
	if provider == "" {
		return cloud.Credential{}, fmt.Errorf("provider is required")
	}
	if _, err := deploy.ProviderKind(provider); err != nil {
		return cloud.Credential{}, err
	}
	if ak == "" || sk == "" {
		return cloud.Credential{}, fmt.Errorf("access key id/secret required to list regions")
	}
	return cloud.Credential{Provider: provider, AccessKeyID: ak, AccessKeySecret: sk, Region: region}, nil
}

// ListImagesInput / ListInstanceTypesInput select a saved credential and the
// region (and zone) to enumerate deploy targets for. Unlike ListRegions these
// only authenticate against a stored CloudID (the deploy form always has one).
type ListImagesInput struct {
	CloudID string `json:"cloud_id"`
	Region  string `json:"region"`
}

type ListInstanceTypesInput struct {
	CloudID string `json:"cloud_id"`
	Region  string `json:"region"`
	Zone    string `json:"zone"`
}

// resolveCloudCredential loads a stored credential by id, overriding its region
// with the requested one (falling back to the credential default). The secret
// stays internal — it is only used to build the provider client.
func (m *DeployManager) resolveCloudCredential(ctx context.Context, cloudID, region string) (cloud.Credential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.store.Load(ctx)
	if err != nil {
		return cloud.Credential{}, err
	}
	stored := state.FindCloud(cloudID)
	if stored == nil {
		return cloud.Credential{}, fmt.Errorf("cloud credential %s not found", cloudID)
	}
	region = deploy.FirstNonEmpty(region, stored.DefaultRegion)
	if region == "" {
		return cloud.Credential{}, fmt.Errorf("region is required (select one or set a credential default)")
	}
	return providerCred(*stored, region), nil
}

// ListImages enumerates selectable OS images for a credential+region.
func (m *DeployManager) ListImages(ctx context.Context, in ListImagesInput) ([]cloud.Image, error) {
	cred, err := m.resolveCloudCredential(ctx, in.CloudID, in.Region)
	if err != nil {
		return nil, err
	}
	prov, err := m.newProvider(cred)
	if err != nil {
		return nil, err
	}
	return prov.ListImages(ctx, cred.Region)
}

// ListInstanceTypes enumerates selectable instance specs for a credential+region.
func (m *DeployManager) ListInstanceTypes(ctx context.Context, in ListInstanceTypesInput) ([]cloud.InstanceType, error) {
	cred, err := m.resolveCloudCredential(ctx, in.CloudID, in.Region)
	if err != nil {
		return nil, err
	}
	prov, err := m.newProvider(cred)
	if err != nil {
		return nil, err
	}
	return prov.ListInstanceTypes(ctx, cred.Region, in.Zone)
}

// DeleteCredential removes a credential, refusing while it has live deployments
// (so their instances remain recyclable).
func (m *DeployManager) DeleteCredential(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.store.Load(ctx)
	if err != nil {
		return err
	}
	for _, d := range state.Deploys {
		if d.CloudID != id {
			continue
		}
		if !d.AutoNet.Empty() {
			return fmt.Errorf("credential has auto-created network on deployment %s; recycle it first", d.ID)
		}
		if d.Status != deploy.StatusRecycled && d.Status != deploy.StatusFailed {
			return fmt.Errorf("credential has active deployment %s; recycle it first", d.ID)
		}
	}
	if state.SSHTunnel != nil && state.SSHTunnel.CloudID == id && state.SSHTunnel.InstanceID != "" {
		return fmt.Errorf("credential owns relay %s; destroy the relay first", state.SSHTunnel.InstanceID)
	}
	out := state.Clouds[:0]
	found := false
	for _, c := range state.Clouds {
		if c.ID == id {
			found = true
			continue
		}
		out = append(out, c)
	}
	if !found {
		return fmt.Errorf("credential %s not found", id)
	}
	state.Clouds = out
	return m.store.Save(ctx, state)
}

// --- public URL ---

func (m *DeployManager) GetPublicURL(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.store.Load(ctx)
	if err != nil {
		return "", err
	}
	return state.PublicURL, nil
}

func (m *DeployManager) SetPublicURL(ctx context.Context, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		if u, err := url.Parse(raw); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("public_url must be an absolute http(s) URL, e.g. http://1.2.3.4:3000")
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.store.Load(ctx)
	if err != nil {
		return err
	}
	state.PublicURL = strings.TrimRight(raw, "/")
	return m.store.Save(ctx, state)
}

// The outbound SSH reverse tunnel lifecycle (ConfigureTunnel, StartTunnel,
// StopTunnel, DestroyRelay, AutoStartTunnel, ShutdownTunnel, TunnelStatus) and
// relay auto-provisioning live in tunnel_relay.go.
