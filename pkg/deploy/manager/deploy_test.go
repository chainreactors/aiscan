package manager

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chainreactors/aiscan/pkg/cloud"
	"github.com/chainreactors/aiscan/pkg/deploy"
)

// fakeProvider records concurrency and lets a chosen node-name suffix fail.
type fakeProvider struct {
	mu          sync.Mutex
	inFlight    int
	maxInFlight int
	created     []string
	deleted     []string
	failSuffix  string
	delay       time.Duration
	createFn    func(cloud.CreateRequest) ([]cloud.Instance, error)
	listFn      func([]string) ([]cloud.Instance, error)
	owned       []cloud.Instance // returned by ListOwnedInstances (reconcile)

	ensureCreated cloud.NetworkDefaults   // what EnsureNetwork reports as auto-created
	ensureErr     error                   // EnsureNetwork failure
	createdSGs    []string                // CreateSecurityGroup calls, in order
	tornDown      []cloud.NetworkDefaults // TeardownNetwork calls, in order
	teardownErr   error
	openedPorts   []int // OpenPort calls, in order

	images        []cloud.Image        // returned by ListImages
	instanceTypes []cloud.InstanceType // returned by ListInstanceTypes
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) ListRegions(ctx context.Context) ([]cloud.Region, error) {
	return []cloud.Region{{ID: "cn-hangzhou", LocalName: "华东1(杭州)"}}, nil
}

func (f *fakeProvider) ListImages(ctx context.Context, region string) ([]cloud.Image, error) {
	return f.images, nil
}

func (f *fakeProvider) ListInstanceTypes(ctx context.Context, region, zone string) ([]cloud.InstanceType, error) {
	return f.instanceTypes, nil
}

func (f *fakeProvider) DefaultNetwork(ctx context.Context, region, zone string) (cloud.NetworkDefaults, error) {
	return cloud.NetworkDefaults{}, nil
}

func (f *fakeProvider) EnsureNetwork(ctx context.Context, region, zone, label string) (cloud.NetworkDefaults, cloud.NetworkDefaults, error) {
	resolved := cloud.NetworkDefaults{ZoneID: zone, VPCID: "vpc-x", VSwitchID: "vsw-x", SecurityGroupID: "sg-x"}
	if f.ensureCreated.VPCID != "" {
		resolved.VPCID = f.ensureCreated.VPCID
	}
	if f.ensureCreated.VSwitchID != "" {
		resolved.VSwitchID = f.ensureCreated.VSwitchID
	}
	if f.ensureCreated.SecurityGroupID != "" {
		resolved.SecurityGroupID = f.ensureCreated.SecurityGroupID
	}
	return resolved, f.ensureCreated, f.ensureErr
}

func (f *fakeProvider) TeardownNetwork(ctx context.Context, region string, created cloud.NetworkDefaults) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tornDown = append(f.tornDown, created)
	return f.teardownErr
}

func (f *fakeProvider) CreateSecurityGroup(ctx context.Context, region, vpcID, label string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := "sg-relay"
	f.createdSGs = append(f.createdSGs, id)
	return id, nil
}

func (f *fakeProvider) CreateInstances(ctx context.Context, req cloud.CreateRequest) ([]cloud.Instance, error) {
	f.mu.Lock()
	f.inFlight++
	if f.inFlight > f.maxInFlight {
		f.maxInFlight = f.inFlight
	}
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.inFlight--
		f.created = append(f.created, req.Name)
		f.mu.Unlock()
	}()

	time.Sleep(f.delay)

	if f.failSuffix != "" && strings.HasSuffix(req.Name, f.failSuffix) {
		return nil, fmt.Errorf("boom")
	}
	if f.createFn != nil {
		return f.createFn(req)
	}
	return []cloud.Instance{{ID: "i-" + req.Name, Status: "Pending"}}, nil
}

func (f *fakeProvider) ListInstances(ctx context.Context, ids []string) ([]cloud.Instance, error) {
	if f.listFn != nil {
		return f.listFn(ids)
	}
	return nil, nil
}
func (f *fakeProvider) DeleteInstances(ctx context.Context, ids []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, ids...)
	return nil
}
func (f *fakeProvider) ListOwnedInstances(ctx context.Context, region string) ([]cloud.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]cloud.Instance(nil), f.owned...), nil
}
func (f *fakeProvider) OpenPort(ctx context.Context, region, sgID string, port int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.openedPorts = append(f.openedPorts, port)
	return nil
}

func newMgrWithFake(fake *fakeProvider) (*DeployManager, *memStore) {
	store := &memStore{}
	mgr := NewDeployManager(store, nil, "ioaTOK", "")
	mgr.newProvider = func(cloud.Credential) (cloud.Provider, error) { return fake, nil }
	return mgr, store
}

func seedCloudAndURL(t *testing.T, mgr *DeployManager) string {
	t.Helper()
	ctx := context.Background()
	if err := mgr.SetPublicURL(ctx, "http://1.2.3.4:3000"); err != nil {
		t.Fatal(err)
	}
	c, err := mgr.SaveCredential(ctx, SaveCredentialInput{
		Provider: "aliyun", AccessKeyID: "ak", AccessKeySecret: "sk", DefaultRegion: "cn-hangzhou",
	})
	if err != nil {
		t.Fatal(err)
	}
	return c.ID
}

func TestCreateDeployConcurrencyBounded(t *testing.T) {
	fake := &fakeProvider{delay: 40 * time.Millisecond}
	mgr, _ := newMgrWithFake(fake)
	cloudID := seedCloudAndURL(t, mgr)

	res, err := mgr.CreateDeploy(context.Background(), DeployRequest{
		CloudID: cloudID, ImageID: "img", InstanceType: "ecs.t6", Count: 5, Space: "s",
	})
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if res.Record == nil || len(res.Record.Nodes) != 5 {
		t.Fatalf("want 5 nodes, got %v", res.Record)
	}
	if fake.maxInFlight > deployLaunchConcurrency {
		t.Fatalf("concurrency exceeded bound: maxInFlight=%d > %d", fake.maxInFlight, deployLaunchConcurrency)
	}
	if fake.maxInFlight < 2 {
		t.Fatalf("expected parallelism, maxInFlight=%d", fake.maxInFlight)
	}
	// Unique, deterministic node names.
	seen := map[string]bool{}
	for _, n := range res.Record.Nodes {
		if seen[n.NodeName] {
			t.Fatalf("duplicate node name %s", n.NodeName)
		}
		seen[n.NodeName] = true
		if n.InstanceID != "i-"+n.NodeName {
			t.Fatalf("instance/node mismatch: %s vs %s", n.InstanceID, n.NodeName)
		}
	}
}

func TestCreateDeployPartialFailure(t *testing.T) {
	fake := &fakeProvider{failSuffix: "-1"} // node index 1 fails
	mgr, _ := newMgrWithFake(fake)
	cloudID := seedCloudAndURL(t, mgr)

	res, err := mgr.CreateDeploy(context.Background(), DeployRequest{
		CloudID: cloudID, ImageID: "img", InstanceType: "ecs.t6", Count: 3, Space: "s",
	})
	if err != nil {
		t.Fatalf("partial deploy should not hard-error: %v", err)
	}
	if res.Record == nil || len(res.Record.Nodes) != 2 {
		t.Fatalf("want 2 surviving nodes, got %v", res.Record)
	}
	if !strings.Contains(res.Record.Status, "active") {
		t.Fatalf("want active status, got %q", res.Record.Status)
	}
	if !strings.Contains(res.Record.Error, "partial") {
		t.Fatalf("want partial error note, got %q", res.Record.Error)
	}
}

// memStore is an in-memory DeployStore for tests.
type memStore struct {
	mu     sync.Mutex
	state  deploy.State
	onSave func(deploy.State)
}

func (m *memStore) Load(ctx context.Context) (*deploy.State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Return a deep-ish copy so the manager can't mutate our backing store
	// outside of Save (mirrors the file store semantics).
	cp := m.state
	cp.Clouds = append([]deploy.CloudCredential(nil), m.state.Clouds...)
	cp.Deploys = append([]deploy.DeployRecord(nil), m.state.Deploys...)
	return &cp, nil
}

func (m *memStore) Save(ctx context.Context, s *deploy.State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = *s
	if m.onSave != nil {
		m.onSave(m.state)
	}
	return nil
}

func TestSaveCredentialMasksAndPreserves(t *testing.T) {
	mgr := NewDeployManager(&memStore{}, nil, "tok", "")
	ctx := context.Background()

	view, err := mgr.SaveCredential(ctx, SaveCredentialInput{
		Name:            "prod",
		Provider:        "aliyun",
		AccessKeyID:     "akfakeXXXXXXXXXXXXXXabcd",
		AccessKeySecret: "supersecret",
		DefaultRegion:   "cn-hangzhou",
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if strings.Contains(view.AccessKeyID, "XXXX") || !strings.Contains(view.AccessKeyID, "****") {
		t.Fatalf("access key id not masked: %q", view.AccessKeyID)
	}
	if !view.SecretConfigured {
		t.Fatal("secret should be configured")
	}

	// Empty secret on update must preserve the stored one.
	_, err = mgr.SaveCredential(ctx, SaveCredentialInput{
		ID: view.ID, Provider: "aliyun", AccessKeyID: view.AccessKeyID, DefaultRegion: "cn-beijing",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	creds, _ := mgr.ListCredentials(ctx)
	if len(creds) != 1 {
		t.Fatalf("want 1 cred, got %d", len(creds))
	}
	if creds[0].DefaultRegion != "cn-beijing" {
		t.Fatalf("region not updated: %q", creds[0].DefaultRegion)
	}
}

func TestSetPublicURLValidation(t *testing.T) {
	mgr := NewDeployManager(&memStore{}, nil, "tok", "")
	ctx := context.Background()
	if err := mgr.SetPublicURL(ctx, "not-a-url"); err == nil {
		t.Fatal("expected validation error for bad url")
	}
	if err := mgr.SetPublicURL(ctx, "http://1.2.3.4:3000/"); err != nil {
		t.Fatalf("valid url rejected: %v", err)
	}
	got, _ := mgr.GetPublicURL(ctx)
	if got != "http://1.2.3.4:3000" {
		t.Fatalf("trailing slash not trimmed: %q", got)
	}
}

func TestCreateDeployDryRun(t *testing.T) {
	store := &memStore{}
	mgr := NewDeployManager(store, nil, "ioaTOKEN", "")
	ctx := context.Background()

	if err := mgr.SetPublicURL(ctx, "http://38.76.191.84:3000"); err != nil {
		t.Fatal(err)
	}
	cred, err := mgr.SaveCredential(ctx, SaveCredentialInput{
		Provider: "aliyun", AccessKeyID: "akfakexxxxxxxxxx", AccessKeySecret: "sk", DefaultRegion: "cn-hangzhou",
	})
	if err != nil {
		t.Fatal(err)
	}

	res, err := mgr.CreateDeploy(ctx, DeployRequest{
		CloudID: cred.ID, ImageID: "img-1", InstanceType: "ecs.t6", Count: 3, Space: "redteam", DryRun: true,
	})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !res.DryRun || res.Script == "" {
		t.Fatal("expected dry-run script")
	}
	for _, want := range []string{
		"--web-url 'http://38.76.191.84:3000'",
		"--ioa-url 'http://ioaTOKEN@38.76.191.84:3000/ioa'",
		"--space 'redteam'",
		"/api/agent/binary?os=linux&arch=${ARCH}",
		"systemctl enable --now aiscan-agent",
	} {
		if !strings.Contains(res.Script, want) {
			t.Errorf("script missing %q\n---\n%s", want, res.Script)
		}
	}
	// Dry run must not persist a deploy record.
	deploys, _ := mgr.ListDeploys(ctx)
	if len(deploys) != 0 {
		t.Fatalf("dry run should not persist, got %d deploys", len(deploys))
	}
}

func TestCreateDeployPersistsPendingAndNodesDuringLaunch(t *testing.T) {
	firstNodePersisted := make(chan struct{})
	var closeFirst sync.Once
	pendingSeen := false
	var phases []string

	fake := &fakeProvider{}
	fake.createFn = func(req cloud.CreateRequest) ([]cloud.Instance, error) {
		if strings.HasSuffix(req.Name, "-1") {
			select {
			case <-firstNodePersisted:
			case <-time.After(2 * time.Second):
				return nil, fmt.Errorf("first node was not persisted before second node completed")
			}
		}
		return []cloud.Instance{{ID: "i-" + req.Name, Status: "Pending"}}, nil
	}
	mgr, store := newMgrWithFake(fake)
	store.onSave = func(s deploy.State) {
		if len(s.Deploys) != 1 {
			return
		}
		rec := s.Deploys[0]
		if rec.Status == deploy.StatusPending && len(rec.Nodes) == 0 {
			pendingSeen = true
		}
		phases = append(phases, rec.Phase)
		if len(rec.Nodes) > 0 {
			closeFirst.Do(func() { close(firstNodePersisted) })
		}
	}
	cloudID := seedCloudAndURL(t, mgr)

	res, err := mgr.CreateDeploy(context.Background(), DeployRequest{
		CloudID: cloudID, ImageID: "img", InstanceType: "ecs.t6", Count: 2, Space: "s",
	})
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if !pendingSeen {
		t.Fatal("deploy was not persisted as pending before cloud launch")
	}
	if res.Record == nil || res.Record.Status != deploy.StatusActive || len(res.Record.Nodes) != 2 {
		t.Fatalf("unexpected deploy result: %#v", res.Record)
	}
	if res.Record.DesiredCount != 2 {
		t.Fatalf("desired count not persisted: %d", res.Record.DesiredCount)
	}
	for _, want := range []string{deploy.PhasePreparing, deploy.PhaseEnsuringNetwork, deploy.PhaseLaunchingInstances, deploy.PhaseWaitingRegistration} {
		if !stringSliceContains(phases, want) {
			t.Fatalf("phase %q not observed in %#v", want, phases)
		}
	}
}

func stringSliceContains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestConfirmGoneCanFlipBackToGone(t *testing.T) {
	oldTimeout, oldInterval := deployConfirmTimeout, deployConfirmInterval
	deployConfirmTimeout = 200 * time.Millisecond
	deployConfirmInterval = time.Millisecond
	defer func() {
		deployConfirmTimeout = oldTimeout
		deployConfirmInterval = oldInterval
	}()

	calls := 0
	fake := &fakeProvider{}
	fake.listFn = func(ids []string) ([]cloud.Instance, error) {
		calls++
		if calls == 1 {
			return []cloud.Instance{{ID: "i-1", Status: "Running"}}, nil
		}
		return nil, nil
	}
	mgr := NewDeployManager(&memStore{}, nil, "tok", "")

	gone := mgr.confirmGone(context.Background(), fake, []string{"i-1"})
	if !gone["i-1"] {
		t.Fatalf("instance should be marked gone after later poll, got %#v", gone)
	}
}

func TestRecycleRejectsInstanceOutsideDeploy(t *testing.T) {
	store := &memStore{state: deploy.State{
		Clouds: []deploy.CloudCredential{{
			ID: "cloud-1", Provider: "aliyun", AccessKeyID: "ak", AccessKeySecret: "sk", DefaultRegion: "cn-hangzhou",
		}},
		Deploys: []deploy.DeployRecord{{
			ID: "dep-1", CloudID: "cloud-1", Provider: "aliyun", Region: "cn-hangzhou", Status: deploy.StatusActive,
			Nodes: []deploy.DeployNode{{InstanceID: "i-owned", NodeName: "dep-1-0"}},
		}},
	}}
	fake := &fakeProvider{}
	mgr := NewDeployManager(store, nil, "tok", "")
	mgr.newProvider = func(cloud.Credential) (cloud.Provider, error) { return fake, nil }

	if _, err := mgr.Recycle(context.Background(), "dep-1", []string{"i-foreign"}); err == nil {
		t.Fatal("expected foreign instance id to be rejected")
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.deleted) != 0 {
		t.Fatalf("foreign instance should not be deleted, got %#v", fake.deleted)
	}
}

func TestRecycleIdempotentNoCloud(t *testing.T) {
	store := &memStore{state: deploy.State{
		Deploys: []deploy.DeployRecord{{ID: "dep-x", Status: deploy.StatusRecycled}},
	}}
	mgr := NewDeployManager(store, nil, "tok", "")
	view, err := mgr.Recycle(context.Background(), "dep-x", nil)
	if err != nil {
		t.Fatalf("recycle already-recycled should be idempotent: %v", err)
	}
	if view.Status != deploy.StatusRecycled {
		t.Fatalf("want recycled, got %q", view.Status)
	}
}

func TestDeleteCredentialBlocksFailedDeployWithAutoNetwork(t *testing.T) {
	store := &memStore{state: deploy.State{
		Clouds: []deploy.CloudCredential{{
			ID: "cloud-1", Provider: "aliyun", AccessKeyID: "ak", AccessKeySecret: "sk", DefaultRegion: "cn-hangzhou",
		}},
		Deploys: []deploy.DeployRecord{{
			ID: "dep-1", CloudID: "cloud-1", Provider: "aliyun", Region: "cn-hangzhou", Status: deploy.StatusFailed,
			AutoNet: deploy.AutoNetwork{VPCID: "vpc-leaked"},
		}},
	}}
	mgr := NewDeployManager(store, nil, "tok", "")

	err := mgr.DeleteCredential(context.Background(), "cloud-1")
	if err == nil || !strings.Contains(err.Error(), "auto-created network") {
		t.Fatalf("expected auto-network delete guard, got %v", err)
	}
}

func TestDeleteCredentialBlocksRelay(t *testing.T) {
	store := &memStore{state: deploy.State{
		Clouds: []deploy.CloudCredential{{
			ID: "cloud-1", Provider: "aliyun", AccessKeyID: "ak", AccessKeySecret: "sk", DefaultRegion: "cn-hangzhou",
		}},
		SSHTunnel: &deploy.SSHTunnel{CloudID: "cloud-1", InstanceID: "i-relay"},
	}}
	mgr := NewDeployManager(store, nil, "tok", "")

	err := mgr.DeleteCredential(context.Background(), "cloud-1")
	if err == nil || !strings.Contains(err.Error(), "destroy the relay first") {
		t.Fatalf("expected relay credential block, got %v", err)
	}
}

func TestCreateDeployRejectsTencentSubnetWithoutVPC(t *testing.T) {
	fake := &fakeProvider{}
	mgr, _ := newMgrWithFake(fake)
	ctx := context.Background()
	if err := mgr.SetPublicURL(ctx, "http://1.2.3.4:3000"); err != nil {
		t.Fatal(err)
	}
	cred, err := mgr.SaveCredential(ctx, SaveCredentialInput{
		Provider: "tencent", AccessKeyID: "ak", AccessKeySecret: "sk", DefaultRegion: "ap-guangzhou",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = mgr.CreateDeploy(ctx, DeployRequest{
		CloudID: cred.ID, ImageID: "img", InstanceType: "S5.MEDIUM2",
		VSwitchID: "subnet-1", SecurityGroupID: "sg-1",
	})
	if err == nil || !strings.Contains(err.Error(), "vpc_id is required") {
		t.Fatalf("expected missing vpc_id error, got %v", err)
	}
	deploys, _ := mgr.ListDeploys(ctx)
	if len(deploys) != 0 {
		t.Fatalf("invalid request should not persist a deploy, got %d", len(deploys))
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.created) != 0 {
		t.Fatalf("invalid request should not launch instances, got %#v", fake.created)
	}
}

// A deploy into a region with no network records the resources aiscan created,
// then tears exactly those down (and clears the record) on recycle.
func TestRecycleTearsDownAutoNetwork(t *testing.T) {
	ctx := context.Background()
	autoNet := cloud.NetworkDefaults{VPCID: "vpc-auto", VSwitchID: "vsw-auto", SecurityGroupID: "sg-auto"}
	fake := &fakeProvider{ensureCreated: autoNet}
	mgr, _ := newMgrWithFake(fake)
	cloudID := seedCloudAndURL(t, mgr)

	res, err := mgr.CreateDeploy(ctx, DeployRequest{CloudID: cloudID, ImageID: "img", InstanceType: "ecs.t6", Count: 1})
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	dep := res.Record.ID

	// The created network must be recorded so recycle can reclaim it.
	got, err := mgr.GetDeploy(ctx, dep)
	if err != nil {
		t.Fatal(err)
	}
	if got.AutoNet != (deploy.AutoNetwork{VPCID: "vpc-auto", VSwitchID: "vsw-auto", SecurityGroupID: "sg-auto"}) {
		t.Fatalf("auto network not recorded: %+v", got.AutoNet)
	}

	if _, err := mgr.Recycle(ctx, dep, nil); err != nil {
		t.Fatalf("recycle: %v", err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.tornDown) != 1 || fake.tornDown[0] != autoNet {
		t.Fatalf("auto network not torn down: %+v", fake.tornDown)
	}
	after, err := mgr.GetDeploy(ctx, dep)
	if err != nil {
		t.Fatal(err)
	}
	if !after.AutoNet.Empty() {
		t.Fatalf("auto network should be cleared after teardown, got %+v", after.AutoNet)
	}
}

// A network-ensure failure fails the deploy fast (no half-built launch), records
// anything created, and immediately tries to reclaim it.
func TestEnsureNetworkFailureRecordsAndReclaims(t *testing.T) {
	ctx := context.Background()
	partial := cloud.NetworkDefaults{VPCID: "vpc-partial"}
	fake := &fakeProvider{ensureCreated: partial, ensureErr: fmt.Errorf("quota exceeded")}
	mgr, _ := newMgrWithFake(fake)
	cloudID := seedCloudAndURL(t, mgr)

	res, err := mgr.CreateDeploy(ctx, DeployRequest{CloudID: cloudID, ImageID: "img", InstanceType: "ecs.t6", Count: 1})
	if err == nil {
		t.Fatal("expected ensure-network failure to fail the deploy")
	}
	if res == nil || res.Record == nil || res.Record.Status != deploy.StatusFailed {
		t.Fatalf("deploy should be marked failed, got %#v", res)
	}
	fake.mu.Lock()
	created := len(fake.created)
	fake.mu.Unlock()
	if created != 0 {
		t.Fatalf("no instances should be launched on ensure-network failure, got %d", created)
	}

	got, err := mgr.GetDeploy(ctx, res.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.AutoNet.Empty() {
		t.Fatalf("auto network should be cleared after immediate reclaim, got %+v", got.AutoNet)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.tornDown) != 1 || fake.tornDown[0] != partial {
		t.Fatalf("partial network not torn down immediately: %+v", fake.tornDown)
	}
}

func TestLaunchFailureTearsDownAutoNetwork(t *testing.T) {
	ctx := context.Background()
	autoNet := cloud.NetworkDefaults{VPCID: "vpc-auto", VSwitchID: "vsw-auto", SecurityGroupID: "sg-auto"}
	fake := &fakeProvider{ensureCreated: autoNet, failSuffix: "-0"}
	mgr, _ := newMgrWithFake(fake)
	cloudID := seedCloudAndURL(t, mgr)

	res, err := mgr.CreateDeploy(ctx, DeployRequest{CloudID: cloudID, ImageID: "img", InstanceType: "ecs.t6", Count: 1})
	if err == nil {
		t.Fatal("expected launch failure")
	}
	if res == nil || res.Record == nil || res.Record.Status != deploy.StatusFailed {
		t.Fatalf("deploy should be marked failed, got %#v", res)
	}
	got, err := mgr.GetDeploy(ctx, res.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.AutoNet.Empty() {
		t.Fatalf("auto network should be cleared after failed launch cleanup, got %+v", got.AutoNet)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.tornDown) != 1 || fake.tornDown[0] != autoNet {
		t.Fatalf("auto network not torn down after failed launch: %+v", fake.tornDown)
	}
}

func TestFailedDeployRetainsAutoNetworkWhenImmediateReclaimFails(t *testing.T) {
	ctx := context.Background()
	autoNet := cloud.NetworkDefaults{VPCID: "vpc-auto"}
	fake := &fakeProvider{
		ensureCreated: autoNet,
		ensureErr:     fmt.Errorf("quota exceeded"),
		teardownErr:   fmt.Errorf("busy"),
	}
	mgr, _ := newMgrWithFake(fake)
	cloudID := seedCloudAndURL(t, mgr)

	res, err := mgr.CreateDeploy(ctx, DeployRequest{CloudID: cloudID, ImageID: "img", InstanceType: "ecs.t6", Count: 1})
	if err == nil {
		t.Fatal("expected ensure-network failure")
	}
	got, err := mgr.GetDeploy(ctx, res.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AutoNet.VPCID != "vpc-auto" {
		t.Fatalf("auto network should remain recorded after teardown failure, got %+v", got.AutoNet)
	}

	fake.mu.Lock()
	fake.teardownErr = nil
	fake.mu.Unlock()
	if _, err := mgr.Recycle(ctx, res.Record.ID, nil); err != nil {
		t.Fatalf("retry recycle: %v", err)
	}
	after, err := mgr.GetDeploy(ctx, res.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.AutoNet.Empty() {
		t.Fatalf("auto network should be cleared after retry, got %+v", after.AutoNet)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.tornDown) != 2 {
		t.Fatalf("expected immediate cleanup plus retry, got %+v", fake.tornDown)
	}
}

// TestReconcileReleasesTaggedOrphans covers the reconcile sweep: a tracked node
// is kept, an old untracked agent node is released, an in-grace node is spared,
// and a relay is never auto-released.
func TestReconcileReleasesTaggedOrphans(t *testing.T) {
	old := time.Now().UTC().Add(-time.Hour)
	young := time.Now().UTC().Add(-2 * time.Minute)
	store := &memStore{state: deploy.State{
		Clouds: []deploy.CloudCredential{{
			ID: "cloud-1", Provider: "aliyun", AccessKeyID: "ak", AccessKeySecret: "sk", DefaultRegion: "cn-hangzhou",
		}},
		Deploys: []deploy.DeployRecord{{
			ID: "dep-live", CloudID: "cloud-1", Provider: "aliyun", Region: "cn-hangzhou", Status: deploy.StatusActive,
			Nodes: []deploy.DeployNode{{InstanceID: "i-tracked", NodeName: "dep-live-0"}},
		}},
	}}
	fake := &fakeProvider{owned: []cloud.Instance{
		{ID: "i-tracked", Name: "dep-live-0", CreatedAt: old}, // tracked by a live record → keep
		{ID: "i-orphan", Name: "dep-gone-0", CreatedAt: old},  // untracked agent, old → release
		{ID: "i-young", Name: "dep-new-0", CreatedAt: young},  // untracked but within grace → keep
		{ID: "i-relay", Name: "aiscan-relay", CreatedAt: old}, // untracked relay, old → release (leaked-billing backstop)
	}}
	mgr := NewDeployManager(store, nil, "tok", "")
	mgr.newProvider = func(cloud.Credential) (cloud.Provider, error) { return fake, nil }

	rep, err := mgr.Reconcile(context.Background(), false)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if rep.Released != 2 {
		t.Fatalf("want 2 released, got %d orphans=%#v", rep.Released, rep.Orphans)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	deleted := map[string]bool{}
	for _, id := range fake.deleted {
		deleted[id] = true
	}
	if len(fake.deleted) != 2 || !deleted["i-orphan"] || !deleted["i-relay"] {
		t.Fatalf("want i-orphan and i-relay released, got %#v", fake.deleted)
	}
}

// TestReconcileKeepsTrackedRelay locks in the safety property that makes reaping
// relays safe: the active relay (its InstanceID is in state.SSHTunnel) is never
// terminated, while a second untracked "aiscan-relay" the hub leaked is.
func TestReconcileKeepsTrackedRelay(t *testing.T) {
	old := time.Now().UTC().Add(-time.Hour)
	store := &memStore{state: deploy.State{
		Clouds: []deploy.CloudCredential{{
			ID: "cloud-1", Provider: "aliyun", AccessKeyID: "ak", AccessKeySecret: "sk", DefaultRegion: "cn-hangzhou",
		}},
		SSHTunnel: &deploy.SSHTunnel{
			CloudID: "cloud-1", Provider: "aliyun", Region: "cn-hangzhou", InstanceID: "i-relay-live",
		},
	}}
	fake := &fakeProvider{owned: []cloud.Instance{
		{ID: "i-relay-live", Name: "aiscan-relay", CreatedAt: old}, // active relay → keep (in known set)
		{ID: "i-relay-dead", Name: "aiscan-relay", CreatedAt: old}, // leaked relay → release
	}}
	mgr := NewDeployManager(store, nil, "tok", "")
	mgr.newProvider = func(cloud.Credential) (cloud.Provider, error) { return fake, nil }

	rep, err := mgr.Reconcile(context.Background(), false)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.deleted) != 1 || fake.deleted[0] != "i-relay-dead" {
		t.Fatalf("want only the untracked relay released, got deleted=%#v released=%d", fake.deleted, rep.Released)
	}
}

// TestReconcileDryRunReleasesNothing ensures dry-run reports orphans without
// terminating anything.
func TestReconcileDryRunReleasesNothing(t *testing.T) {
	old := time.Now().UTC().Add(-time.Hour)
	store := &memStore{state: deploy.State{
		Clouds: []deploy.CloudCredential{{
			ID: "cloud-1", Provider: "aliyun", AccessKeyID: "ak", AccessKeySecret: "sk", DefaultRegion: "cn-hangzhou",
		}},
	}}
	fake := &fakeProvider{owned: []cloud.Instance{
		{ID: "i-orphan", Name: "dep-gone-0", CreatedAt: old},
	}}
	mgr := NewDeployManager(store, nil, "tok", "")
	mgr.newProvider = func(cloud.Credential) (cloud.Provider, error) { return fake, nil }

	rep, err := mgr.Reconcile(context.Background(), true)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if rep.Released != 0 || len(rep.Orphans) != 1 {
		t.Fatalf("dry-run want 0 released / 1 orphan, got released=%d orphans=%d", rep.Released, len(rep.Orphans))
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.deleted) != 0 {
		t.Fatalf("dry-run must not delete, got %#v", fake.deleted)
	}
}
