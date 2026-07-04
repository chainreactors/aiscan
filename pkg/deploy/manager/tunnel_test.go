package manager

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chainreactors/aiscan/pkg/cloud"
)

func TestTunnelStatusWhenUnavailable(t *testing.T) {
	mgr := NewDeployManager(&memStore{}, nil, "tok", "")
	st := mgr.TunnelStatus()
	if st.Available {
		t.Fatalf("tunnel should be unavailable without a configured local addr")
	}
	if st.Backend != "ssh" {
		t.Fatalf("backend = %q, want ssh", st.Backend)
	}
	if _, err := mgr.StartTunnel(context.Background(), StartTunnelRequest{}); err == nil {
		t.Fatalf("StartTunnel should fail without a configured local addr")
	}
}

func TestConfigureTunnelParsesLocalTarget(t *testing.T) {
	mgr := NewDeployManager(&memStore{}, nil, "tok", "")
	mgr.ConfigureTunnel("http://127.0.0.1:3000/")
	if mgr.localURL != "http://127.0.0.1:3000" {
		t.Fatalf("localURL not trimmed: %q", mgr.localURL)
	}
	target, port, ok := mgr.localTarget()
	if !ok || target != "127.0.0.1:3000" || port != 3000 {
		t.Fatalf("localTarget = %q,%d,%v", target, port, ok)
	}
	if !mgr.TunnelStatus().Available {
		t.Fatalf("tunnel should be available after ConfigureTunnel")
	}
}

func TestStartTunnelRequiresCloudWhenUnprovisioned(t *testing.T) {
	mgr, _ := newMgrWithFake(&fakeProvider{})
	mgr.ConfigureTunnel("http://127.0.0.1:3000")
	if _, err := mgr.StartTunnel(context.Background(), StartTunnelRequest{}); err == nil {
		t.Fatalf("StartTunnel should require cloud_id when no relay exists")
	}
}

// TestStartTunnelProvisionsRelay drives the full provision path against the fake:
// it must open the forward port, launch one relay instance, wait for its public
// IP, and fill that IP:port into the deploy PublicURL.
func TestStartTunnelProvisionsRelay(t *testing.T) {
	fake := &fakeProvider{
		images:        []cloud.Image{{ID: "img-ubuntu"}},
		instanceTypes: []cloud.InstanceType{{ID: "ecs.t.small", CPU: 1, MemoryGiB: 1}},
		createFn: func(req cloud.CreateRequest) ([]cloud.Instance, error) {
			if req.BandwidthOut <= 0 {
				t.Errorf("relay must request a public IP (BandwidthOut>0), got %d", req.BandwidthOut)
			}
			return []cloud.Instance{{ID: "i-relay", Status: "Pending"}}, nil
		},
		listFn: func(ids []string) ([]cloud.Instance, error) {
			return []cloud.Instance{{ID: "i-relay", PublicIP: "203.0.113.7", Status: "Running"}}, nil
		},
	}
	mgr, store := newMgrWithFake(fake)
	mgr.ConfigureTunnel("http://127.0.0.1:3000")
	ctx := context.Background()
	cred, err := mgr.SaveCredential(ctx, SaveCredentialInput{
		Provider: "aliyun", AccessKeyID: "ak", AccessKeySecret: "sk", DefaultRegion: "cn-hangzhou",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := mgr.StartTunnel(ctx, StartTunnelRequest{CloudID: cred.ID, Region: "cn-hangzhou"}); err != nil {
		t.Fatalf("StartTunnel: %v", err)
	}
	// Provisioning runs in the background; wait for it to settle.
	deadline := time.Now().Add(3 * time.Second)
	for {
		st := mgr.TunnelStatus()
		if st.Phase != "provisioning" && st.RelayIP != "" {
			break
		}
		if st.Error != "" {
			t.Fatalf("provision error: %s", st.Error)
		}
		if time.Now().After(deadline) {
			t.Fatalf("provisioning did not complete; status=%+v", st)
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer mgr.StopTunnel(ctx) // stop the supervisor goroutine dialing the fake IP

	fake.mu.Lock()
	openedPorts := append([]int(nil), fake.openedPorts...)
	created := append([]string(nil), fake.created...)
	fake.mu.Unlock()
	if len(openedPorts) != 2 || openedPorts[0] != 22 || openedPorts[1] != 3000 {
		t.Fatalf("expected ssh+forward ports opened, got %v", openedPorts)
	}
	if len(created) != 1 || created[0] != "aiscan-relay" {
		t.Fatalf("expected one relay instance, got %v", created)
	}
	fake.mu.Lock()
	createdSGs := append([]string(nil), fake.createdSGs...)
	fake.mu.Unlock()
	if len(createdSGs) != 1 || createdSGs[0] != "sg-relay" {
		t.Fatalf("expected dedicated relay security group, got %v", createdSGs)
	}

	got, _ := mgr.GetPublicURL(ctx)
	if got != "http://203.0.113.7:3000" {
		t.Fatalf("PublicURL = %q, want http://203.0.113.7:3000", got)
	}
	st := mgr.TunnelStatus()
	if st.RelayIP != "203.0.113.7" || !st.Enabled {
		t.Fatalf("status = %+v", st)
	}
	// The relay must be persisted for a later DestroyRelay to reclaim.
	state, _ := store.Load(ctx)
	if !state.SSHTunnel.Provisioned() || state.SSHTunnel.InstanceID != "i-relay" {
		t.Fatalf("relay not persisted: %+v", state.SSHTunnel)
	}
	if state.SSHTunnel.AutoNet.SecurityGroupID != "sg-relay" {
		t.Fatalf("relay SG should be tracked for teardown, got %+v", state.SSHTunnel.AutoNet)
	}
}

func TestDestroyRelayReclaims(t *testing.T) {
	fake := &fakeProvider{
		images:        []cloud.Image{{ID: "img"}},
		instanceTypes: []cloud.InstanceType{{ID: "t"}},
		ensureCreated: cloud.NetworkDefaults{VPCID: "vpc-r", VSwitchID: "vsw-r", SecurityGroupID: "sg-r"},
		createFn: func(req cloud.CreateRequest) ([]cloud.Instance, error) {
			return []cloud.Instance{{ID: "i-relay"}}, nil
		},
	}
	// The relay reports its IP until terminated, then vanishes — so confirmGone
	// returns promptly instead of polling to its timeout.
	fake.listFn = func(ids []string) ([]cloud.Instance, error) {
		fake.mu.Lock()
		gone := len(fake.deleted) > 0
		fake.mu.Unlock()
		if gone {
			return nil, nil
		}
		return []cloud.Instance{{ID: "i-relay", PublicIP: "203.0.113.9"}}, nil
	}
	mgr, store := newMgrWithFake(fake)
	mgr.ConfigureTunnel("http://127.0.0.1:3000")
	ctx := context.Background()
	cred, _ := mgr.SaveCredential(ctx, SaveCredentialInput{
		Provider: "aliyun", AccessKeyID: "ak", AccessKeySecret: "sk", DefaultRegion: "cn-hangzhou",
	})
	if _, err := mgr.StartTunnel(ctx, StartTunnelRequest{CloudID: cred.ID}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for mgr.TunnelStatus().RelayIP == "" {
		if time.Now().After(deadline) {
			t.Fatalf("relay never provisioned: %+v", mgr.TunnelStatus())
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := mgr.DestroyRelay(ctx); err != nil {
		t.Fatalf("DestroyRelay: %v", err)
	}
	fake.mu.Lock()
	deleted := append([]string(nil), fake.deleted...)
	tornDown := append([]cloud.NetworkDefaults(nil), fake.tornDown...)
	fake.mu.Unlock()
	if len(deleted) != 1 || deleted[0] != "i-relay" {
		t.Fatalf("relay instance not terminated: %v", deleted)
	}
	if len(tornDown) != 1 || tornDown[0].SecurityGroupID != "sg-r" {
		t.Fatalf("relay network not torn down: %v", tornDown)
	}
	state, _ := store.Load(ctx)
	if state.SSHTunnel != nil {
		t.Fatalf("tunnel config should be cleared, got %+v", state.SSHTunnel)
	}
	if got, _ := mgr.GetPublicURL(ctx); got != "" {
		t.Fatalf("PublicURL should be cleared, got %q", got)
	}
}

func TestStopTunnelCancelsProvisioningWithoutConnecting(t *testing.T) {
	oldInterval := relayIPInterval
	relayIPInterval = 10 * time.Millisecond
	defer func() { relayIPInterval = oldInterval }()

	fake := &fakeProvider{
		images:        []cloud.Image{{ID: "img"}},
		instanceTypes: []cloud.InstanceType{{ID: "t"}},
		createFn: func(req cloud.CreateRequest) ([]cloud.Instance, error) {
			return []cloud.Instance{{ID: "i-relay"}}, nil
		},
		listFn: func(ids []string) ([]cloud.Instance, error) {
			return []cloud.Instance{{ID: "i-relay", Status: "Running"}}, nil
		},
	}
	mgr, store := newMgrWithFake(fake)
	mgr.ConfigureTunnel("http://127.0.0.1:3000")
	ctx := context.Background()
	cred, _ := mgr.SaveCredential(ctx, SaveCredentialInput{
		Provider: "aliyun", AccessKeyID: "ak", AccessKeySecret: "sk", DefaultRegion: "cn-hangzhou",
	})
	if _, err := mgr.StartTunnel(ctx, StartTunnelRequest{CloudID: cred.ID}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		state, _ := store.Load(ctx)
		if state.SSHTunnel != nil && state.SSHTunnel.InstanceID == "i-relay" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("relay was not persisted before stop: %+v", mgr.TunnelStatus())
		}
		time.Sleep(10 * time.Millisecond)
	}

	st := mgr.StopTunnel(ctx)
	if st.Running || st.Connected || st.Enabled {
		t.Fatalf("tunnel should be stopped and disabled after cancel, got %+v", st)
	}
	if got, _ := mgr.GetPublicURL(ctx); got != "" {
		t.Fatalf("PublicURL should not be filled after canceled provisioning, got %q", got)
	}
}

// TestStartTunnelTearsDownNetworkOnEnsureFailure covers the relay-provision
// rollback: when EnsureNetwork fails after partially creating resources, the
// created VPC/vSwitch/SG must be torn down rather than leaked.
func TestStartTunnelTearsDownNetworkOnEnsureFailure(t *testing.T) {
	fake := &fakeProvider{
		ensureCreated: cloud.NetworkDefaults{VPCID: "vpc-r", VSwitchID: "vsw-r", SecurityGroupID: "sg-r"},
		ensureErr:     errors.New("ensure boom"),
	}
	mgr, _ := newMgrWithFake(fake)
	mgr.ConfigureTunnel("http://127.0.0.1:3000")
	ctx := context.Background()
	cred, _ := mgr.SaveCredential(ctx, SaveCredentialInput{
		Provider: "aliyun", AccessKeyID: "ak", AccessKeySecret: "sk", DefaultRegion: "cn-hangzhou",
	})
	if _, err := mgr.StartTunnel(ctx, StartTunnelRequest{CloudID: cred.ID}); err != nil {
		t.Fatal(err)
	}

	// Provisioning fails in the background; wait for the error to surface.
	deadline := time.Now().Add(3 * time.Second)
	for mgr.TunnelStatus().Error == "" {
		if time.Now().After(deadline) {
			t.Fatalf("provisioning error never surfaced: %+v", mgr.TunnelStatus())
		}
		time.Sleep(10 * time.Millisecond)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.tornDown) != 1 || fake.tornDown[0].VPCID != "vpc-r" {
		t.Fatalf("network from a failed EnsureNetwork was not torn down: %#v", fake.tornDown)
	}
}
