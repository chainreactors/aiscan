package manager

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/chainreactors/aiscan/pkg/cloud"
	"github.com/chainreactors/aiscan/pkg/deploy"
	"github.com/chainreactors/aiscan/pkg/deploy/tunnel"
)

// Relay defaults. The relay is a throwaway forwarder, so it gets the cheapest
// image/type and a small egress allowance; the hub logs in as root over key.
const (
	relayBandwidthMbps = 5
	relaySSHUser       = "root"
	relaySSHPort       = 22
	// relayInstanceName is the fixed cloud instance name given to the relay VM. It
	// is used both when creating the relay and by the reconcile sweep to recognize
	// an orphan relay the hub owns but no longer tracks.
	relayInstanceName = "aiscan-relay"
)

// relay public-IP polling bounds (a fresh instance gets its IP asynchronously).
var (
	relayIPTimeout  = 120 * time.Second
	relayIPInterval = 5 * time.Second
)

// relayURL is the hub-facing URL for a relay forwarding traffic on port at ip.
func relayURL(ip string, port int) string {
	return "http://" + net.JoinHostPort(ip, strconv.Itoa(port))
}

// StartTunnelRequest provisions (first time) and connects the SSH reverse tunnel.
// CloudID/Region are required only when no relay exists yet; the relay image,
// instance type and bandwidth are picked automatically.
type StartTunnelRequest struct {
	CloudID string `json:"cloud_id"`
	Region  string `json:"region"`
}

// ConfigureTunnel sets the hub loopback address the relay forwards to. Empty =>
// the tunnel feature is unavailable (e.g. the listen addr could not be derived).
func (m *DeployManager) ConfigureTunnel(localURL string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.localURL = strings.TrimRight(strings.TrimSpace(localURL), "/")
}

// localTarget parses the configured hub local URL into a dial target "host:port"
// and its port. ok=false when unset/unparseable.
func (m *DeployManager) localTarget() (target string, port int, ok bool) {
	m.mu.Lock()
	raw := m.localURL
	m.mu.Unlock()
	if raw == "" {
		return "", 0, false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", 0, false
	}
	ps := u.Port()
	if ps == "" {
		if u.Scheme == "https" {
			ps = "443"
		} else {
			ps = "80"
		}
	}
	p, err := strconv.Atoi(ps)
	if err != nil {
		return "", 0, false
	}
	host := u.Hostname()
	if host == "0.0.0.0" || host == "::" || host == "" {
		host = "127.0.0.1" // the relay forwards to the hub's own loopback port
	}
	return net.JoinHostPort(host, ps), p, true
}

// StartTunnel provisions a relay (if none) and connects the reverse tunnel. The
// slow work (VM launch + public-IP wait, ~1-2 min) runs in the background; the
// caller polls TunnelStatus for progress. Idempotent while already connected.
func (m *DeployManager) StartTunnel(ctx context.Context, req StartTunnelRequest) (TunnelStatus, error) {
	target, port, ok := m.localTarget()
	if !ok {
		return m.TunnelStatus(), fmt.Errorf("hub local address unknown; tunnel unavailable")
	}

	m.mu.Lock()
	if m.tunBusy {
		m.mu.Unlock()
		return m.TunnelStatus(), fmt.Errorf("a tunnel operation is already in progress")
	}
	if m.tun != nil {
		if running, connected, _, _ := m.tun.Status(); running && connected {
			m.mu.Unlock()
			return m.TunnelStatus(), nil
		}
	}
	state, err := m.store.Load(ctx)
	if err != nil {
		m.mu.Unlock()
		return m.TunnelStatus(), err
	}
	if state.SSHTunnel == nil || !state.SSHTunnel.Provisioned() {
		if req.CloudID == "" {
			m.mu.Unlock()
			return m.TunnelStatus(), fmt.Errorf("cloud_id is required to provision a relay")
		}
		if state.FindCloud(req.CloudID) == nil {
			m.mu.Unlock()
			return m.TunnelStatus(), fmt.Errorf("cloud credential %s not found", req.CloudID)
		}
	}

	m.launchTunnelOp(ctx, func(c context.Context) error {
		return m.doProvisionAndConnect(c, req, target, port)
	})
	return m.TunnelStatus(), nil
}

// launchTunnelOp registers the cancel/done handshake, releases m.mu, and runs op
// in the background, reconciling tunBusy/tunErr on exit. The caller must hold
// m.mu; launchTunnelOp unlocks it.
func (m *DeployManager) launchTunnelOp(ctx context.Context, op func(context.Context) error) {
	opCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	m.tunBusy = true
	m.tunErr = ""
	m.tunOpCancel = cancel
	m.tunOpDone = done
	m.mu.Unlock()

	go func() {
		defer close(done)
		err := op(opCtx)
		m.mu.Lock()
		m.finishTunnelOp(done, err)
		m.mu.Unlock()
	}()
}

// finishTunnelOp clears the in-flight handshake when done is still the active
// operation, recording a non-cancel error. Caller must hold m.mu.
func (m *DeployManager) finishTunnelOp(done chan struct{}, err error) {
	if m.tunOpDone != done {
		return
	}
	m.tunBusy = false
	m.tunOpCancel = nil
	m.tunOpDone = nil
	if err != nil && err != context.Canceled {
		m.tunErr = err.Error()
	}
}

// doProvisionAndConnect provisions the relay when absent, then connects.
func (m *DeployManager) doProvisionAndConnect(ctx context.Context, req StartTunnelRequest, localTarget string, localPort int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	state, err := m.store.Load(ctx)
	if err != nil {
		return err
	}
	tun := state.SSHTunnel
	if tun == nil || !tun.Provisioned() {
		tun, err = m.provisionRelay(ctx, req, localPort)
		if err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return m.connectExisting(ctx, tun, localTarget, localPort)
}

// connectExisting builds a supervisor for an already-provisioned relay, swaps it
// in (stopping any prior one), fills the deploy PublicURL, and marks enabled.
func (m *DeployManager) connectExisting(ctx context.Context, tun *deploy.SSHTunnel, localTarget string, localPort int) error {
	remotePort := tun.RemotePort
	if remotePort == 0 {
		remotePort = localPort
	}
	sshPort := tun.SSHPort
	if sshPort == 0 {
		sshPort = relaySSHPort
	}
	user := tun.SSHUser
	if user == "" {
		user = relaySSHUser
	}
	st, err := tunnel.New(
		net.JoinHostPort(tun.PublicIP, strconv.Itoa(sshPort)),
		user,
		tun.PrivateKey,
		net.JoinHostPort("0.0.0.0", strconv.Itoa(remotePort)),
		localTarget,
	)
	if err != nil {
		return fmt.Errorf("parse relay key: %w", err)
	}
	m.mu.Lock()
	old := m.tun
	m.tun = st
	m.mu.Unlock()
	if old != nil {
		old.Stop()
	}
	st.Start()
	cleanupStarted := func() {
		st.Stop()
		m.mu.Lock()
		if m.tun == st {
			m.tun = nil
		}
		m.mu.Unlock()
	}

	publicURL := relayURL(tun.PublicIP, remotePort)
	if err := m.SetPublicURL(ctx, publicURL); err != nil {
		cleanupStarted()
		return err
	}
	if err := m.setTunnelEnabled(ctx, true); err != nil {
		cleanupStarted()
		return err
	}
	return nil
}

// provisionRelay launches a minimal relay VM via the request's credential: it
// ensures a network, opens the forward port, injects a generated key via
// cloud-init, and waits for the public IP. Network is torn down on early
// failure; once the instance exists it is persisted so DestroyRelay can reclaim.
func (m *DeployManager) provisionRelay(ctx context.Context, req StartTunnelRequest, localPort int) (*deploy.SSHTunnel, error) {
	m.mu.Lock()
	state, err := m.store.Load(ctx)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	cred := state.FindCloud(req.CloudID)
	if cred == nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("cloud credential %s not found", req.CloudID)
	}
	credCopy := *cred
	m.mu.Unlock()

	region := deploy.FirstNonEmpty(req.Region, credCopy.DefaultRegion)
	if region == "" {
		return nil, fmt.Errorf("region is required (set on request or as credential default)")
	}
	prov, err := m.newProvider(providerCred(credCopy, region))
	if err != nil {
		return nil, err
	}

	label := deploy.NewID("relay-")
	resolved, created, err := prov.EnsureNetwork(ctx, region, "", label)
	if err != nil {
		// EnsureNetwork returns any partially-created resources even on error;
		// tear them down (best-effort, detached ctx so a cancel can't block the
		// cleanup) so a failed provision does not leak a VPC/vSwitch/SG. This
		// mirrors CreateDeploy's rollback on the agent-node path, which the relay
		// path previously omitted.
		if !created.Empty() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			_ = prov.TeardownNetwork(cleanupCtx, region, created)
			cancel()
		}
		return nil, fmt.Errorf("ensure network: %w", err)
	}
	// From here, any failure tears down the network we just created.
	fail := func(format string, a ...interface{}) (*deploy.SSHTunnel, error) {
		_ = prov.TeardownNetwork(ctx, region, created)
		return nil, fmt.Errorf(format, a...)
	}

	relaySGID := resolved.SecurityGroupID
	if created.SecurityGroupID == "" {
		// Do not mutate a pre-existing user security group for the relay's public
		// forwarding port. Create a caller-owned SG and reclaim it with the relay.
		sgID, err := prov.CreateSecurityGroup(ctx, region, resolved.VPCID, label)
		if err != nil {
			return fail("create relay security group: %w", err)
		}
		created.SecurityGroupID = sgID
		relaySGID = sgID
	}
	if relaySGID == "" {
		return fail("relay security group is empty")
	}

	remotePort := localPort
	ports := []int{relaySSHPort, remotePort}
	opened := map[int]bool{}
	for _, p := range ports {
		if p <= 0 || opened[p] {
			continue
		}
		opened[p] = true
		if err := prov.OpenPort(ctx, region, relaySGID, p); err != nil {
			return fail("open relay port %d: %w", p, err)
		}
	}

	imgs, err := prov.ListImages(ctx, region)
	if err != nil {
		return fail("list images: %w", err)
	}
	if len(imgs) == 0 {
		return fail("no images available in %s", region)
	}
	imageID := imgs[0].ID

	types, err := prov.ListInstanceTypes(ctx, region, resolved.ZoneID)
	if err != nil {
		return fail("list instance types: %w", err)
	}
	if len(types) == 0 {
		return fail("no instance types available in %s", region)
	}
	instanceType := types[0].ID

	privPEM, authKey, err := tunnel.NewRelayKey()
	if err != nil {
		return fail("generate relay key: %w", err)
	}
	insts, err := prov.CreateInstances(ctx, cloud.CreateRequest{
		Region:          region,
		ZoneID:          resolved.ZoneID,
		ImageID:         imageID,
		InstanceType:    instanceType,
		SecurityGroupID: relaySGID,
		VSwitchID:       resolved.VSwitchID,
		VPCID:           resolved.VPCID,
		Count:           1,
		UserData:        deploy.GenerateRelayUserData(authKey),
		Name:            relayInstanceName,
		BandwidthOut:    relayBandwidthMbps,
	})
	if err != nil {
		return fail("create relay: %w", err)
	}
	if len(insts) == 0 {
		return fail("create relay: no instance returned")
	}
	cleanupUnrecordedRelay := func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		_ = prov.DeleteInstances(cleanupCtx, []string{insts[0].ID})
		_ = prov.TeardownNetwork(cleanupCtx, region, created)
	}

	tun := &deploy.SSHTunnel{
		CloudID:    credCopy.ID,
		Provider:   credCopy.Provider,
		Region:     region,
		InstanceID: insts[0].ID,
		SSHUser:    relaySSHUser,
		SSHPort:    relaySSHPort,
		RemotePort: remotePort,
		PrivateKey: privPEM,
		AutoNet:    autoNet(created),
		CreatedAt:  time.Now().UTC(),
	}
	// Persist before polling so a crash still leaves the relay recyclable.
	if err := m.saveTunnel(ctx, tun); err != nil {
		cleanupUnrecordedRelay()
		return nil, err
	}
	ip, err := m.waitRelayIP(ctx, prov, tun.InstanceID)
	if err != nil {
		return nil, err // relay recorded; DestroyRelay reclaims it
	}
	tun.PublicIP = ip
	if err := m.saveTunnel(ctx, tun); err != nil {
		return nil, err
	}
	return tun, nil
}

// waitRelayIP polls until the relay instance reports a public IP.
func (m *DeployManager) waitRelayIP(ctx context.Context, prov cloud.Provider, instanceID string) (string, error) {
	deadline := time.Now().Add(relayIPTimeout)
	for {
		if insts, err := prov.ListInstances(ctx, []string{instanceID}); err == nil {
			for _, it := range insts {
				if it.ID == instanceID && it.PublicIP != "" {
					return it.PublicIP, nil
				}
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("relay %s got no public IP within %s", instanceID, relayIPTimeout)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(relayIPInterval):
		}
	}
}

// saveTunnel persists the SSH tunnel config.
func (m *DeployManager) saveTunnel(ctx context.Context, tun *deploy.SSHTunnel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.store.Load(ctx)
	if err != nil {
		return err
	}
	// Preserve the enabled flag across intermediate saves.
	if state.SSHTunnel != nil {
		tun.Enabled = state.SSHTunnel.Enabled
	}
	state.SSHTunnel = tun
	return m.store.Save(ctx, state)
}

// setTunnelEnabled persists the auto-start flag.
func (m *DeployManager) setTunnelEnabled(ctx context.Context, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.store.Load(ctx)
	if err != nil {
		return err
	}
	state.EnsureSSHTunnel().Enabled = enabled
	return m.store.Save(ctx, state)
}

// cancelTunnelOperation cancels an in-flight provision/connect operation and
// waits for the goroutine to exit, so a later stop/destroy cannot race with a
// background connectExisting call.
func (m *DeployManager) cancelTunnelOperation() {
	m.mu.Lock()
	cancel := m.tunOpCancel
	done := m.tunOpDone
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
		m.mu.Lock()
		m.finishTunnelOp(done, nil)
		m.mu.Unlock()
	}
}

// StopTunnel drops the tunnel and clears the auto-start flag (user action — it
// stays off across reboots). The relay VM is kept so restarting is fast and its
// IP stable; use DestroyRelay to reclaim it.
func (m *DeployManager) StopTunnel(ctx context.Context) TunnelStatus {
	m.cancelTunnelOperation()
	m.ShutdownTunnel()
	_ = m.setTunnelEnabled(ctx, false)
	m.mu.Lock()
	m.tunErr = ""
	m.mu.Unlock()
	return m.TunnelStatus()
}

// ShutdownTunnel stops the running supervisor without clearing the auto-start
// flag, so a hub restart reconnects. Used on server shutdown.
func (m *DeployManager) ShutdownTunnel() {
	m.cancelTunnelOperation()
	m.mu.Lock()
	t := m.tun
	m.tun = nil
	m.mu.Unlock()
	if t != nil {
		t.Stop()
	}
}

// DestroyRelay stops the tunnel, terminates the relay VM, tears down its
// auto-created network, and clears the tunnel config (and the PublicURL that
// pointed at it). Idempotent when nothing is provisioned.
func (m *DeployManager) DestroyRelay(ctx context.Context) (TunnelStatus, error) {
	m.cancelTunnelOperation()
	m.ShutdownTunnel()

	m.mu.Lock()
	state, err := m.store.Load(ctx)
	if err != nil {
		m.mu.Unlock()
		return m.TunnelStatus(), err
	}
	tun := state.SSHTunnel
	if tun == nil || tun.InstanceID == "" {
		state.SSHTunnel = nil
		_ = m.store.Save(ctx, state)
		m.tunErr = ""
		m.mu.Unlock()
		return m.TunnelStatus(), nil
	}
	cred := state.FindCloud(tun.CloudID)
	tunCopy := *tun
	var credForProvider cloud.Credential
	if cred != nil {
		credForProvider = providerCred(*cred, tunCopy.Region)
	}
	m.mu.Unlock()

	if cred == nil {
		return m.TunnelStatus(), fmt.Errorf("credential %s for the relay is gone; cannot destroy via API", tunCopy.CloudID)
	}
	prov, err := m.newProvider(credForProvider)
	if err != nil {
		return m.TunnelStatus(), err
	}
	if err := prov.DeleteInstances(ctx, []string{tunCopy.InstanceID}); err != nil {
		return m.TunnelStatus(), fmt.Errorf("terminate relay: %w", err)
	}
	m.confirmGone(ctx, prov, []string{tunCopy.InstanceID})
	if !tunCopy.AutoNet.Empty() {
		_ = prov.TeardownNetwork(ctx, tunCopy.Region, netDefaults(tunCopy.AutoNet))
	}

	prevURL := relayURL(tunCopy.PublicIP, tunCopy.RemotePort)
	m.mu.Lock()
	if state, err := m.store.Load(ctx); err == nil {
		if state.PublicURL == prevURL {
			state.PublicURL = ""
		}
		state.SSHTunnel = nil
		_ = m.store.Save(ctx, state)
	}
	m.tunErr = ""
	m.mu.Unlock()
	return m.TunnelStatus(), nil
}

// AutoStartTunnel reconnects to an existing relay at hub boot if the tunnel was
// enabled. It never re-provisions; a missing relay surfaces via TunnelStatus.
func (m *DeployManager) AutoStartTunnel(ctx context.Context) {
	target, port, ok := m.localTarget()
	if !ok {
		return
	}
	state, err := m.store.Load(ctx)
	if err != nil || state.SSHTunnel == nil {
		return
	}
	tun := state.SSHTunnel
	if !tun.Enabled || !tun.Provisioned() || tun.PublicIP == "" || tun.PrivateKey == "" {
		return
	}
	m.mu.Lock()
	if m.tunBusy || m.tun != nil {
		m.mu.Unlock()
		return
	}
	// Register the same cancel/done handshake StartTunnel uses so a StopTunnel/
	// DestroyRelay/ShutdownTunnel racing this boot-time connect can cancel it and
	// wait it out. Without this the boot goroutine is invisible to those calls: it
	// could swap in m.tun and re-enable the tunnel after the user stopped it, race
	// a relay teardown, and leak past the shutdown barrier.
	m.launchTunnelOp(ctx, func(c context.Context) error {
		return m.connectExisting(c, tun, target, port)
	})
}

// TunnelStatus reports the current tunnel state (persisted relay info + live
// supervisor + in-flight provisioning).
func (m *DeployManager) TunnelStatus() TunnelStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	st := TunnelStatus{Backend: "ssh", Available: m.localURL != ""}
	if state, err := m.store.Load(context.Background()); err == nil && state.SSHTunnel != nil {
		s := state.SSHTunnel
		st.Enabled = s.Enabled
		st.RelayIP = s.PublicIP
		st.Provider = s.Provider
		st.Region = s.Region
		if s.PublicIP != "" && s.RemotePort != 0 {
			st.PublicURL = relayURL(s.PublicIP, s.RemotePort)
		}
	}

	tunErr := m.tunErr
	if m.tun != nil {
		running, connected, lastErr, startedAt := m.tun.Status()
		st.Running = running
		st.Connected = connected
		if lastErr != "" && tunErr == "" {
			tunErr = lastErr
		}
		if !startedAt.IsZero() {
			st.StartedAt = startedAt.UTC().Format(time.RFC3339)
		}
	}
	if m.tunBusy {
		st.Running = true
	}
	st.Error = tunErr

	switch {
	case st.Connected:
		st.Phase = "connected"
	case m.tunBusy:
		st.Phase = "provisioning"
	case st.Running:
		st.Phase = "connecting"
	case st.Error != "":
		st.Phase = "error"
	}
	return st
}
