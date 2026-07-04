package cloud

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// cannedTransport returns an http.RoundTripper that answers every request with a
// 200 body produced by handler(r) — letting provider methods (which build real
// SDK requests) be exercised without touching a real cloud endpoint. Providers
// expose a `transport` seam for exactly this.
func cannedTransport(handler func(*http.Request) string) http.RoundTripper {
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(handler(r))),
			Header:     make(http.Header),
		}, nil
	})
}

// aliyunAction extracts the RPC Action from an SDK-built request. The modular SDK
// (ACS3 signing path) carries the action in the lowercase `x-acs-action` header;
// note it stores headers with literal lowercase keys, so http.Header.Get (which
// canonicalizes) misses them — index the map directly. Older RPC paths put it in
// the query string, so fall back to that.
func aliyunAction(r *http.Request) string {
	if v := r.Header["x-acs-action"]; len(v) > 0 { //nolint:staticcheck // mock stores literal lowercase header keys
		return v[0]
	}
	if a := r.URL.Query().Get("Action"); a != "" {
		return a
	}
	return ""
}

// tencentAction reads the action header the SDK sets on every request.
func tencentAction(r *http.Request) string { return r.Header.Get("X-TC-Action") }

// --- aliyun ---

func TestAliyunListImagesRanksUbuntuFirst(t *testing.T) {
	p := &aliyunProvider{cred: Credential{AccessKeyID: "ak", AccessKeySecret: "sk", Region: "cn-hangzhou"}}
	p.transport = cannedTransport(func(r *http.Request) string {
		if a := aliyunAction(r); a != "DescribeImages" {
			t.Errorf("Action = %q, want DescribeImages", a)
		}
		if !strings.HasPrefix(r.URL.Host, "ecs") { // SDK resolves the canonical ECS host
			t.Errorf("host = %q, want ECS endpoint", r.URL.Host)
		}
		return `{"Images":{"Image":[
			{"ImageId":"centos_7","OSName":"CentOS 7.9","OSNameEn":"CentOS 7.9","Platform":"CentOS","Architecture":"x86_64"},
			{"ImageId":"ubuntu_22","OSName":"Ubuntu 22.04","OSNameEn":"Ubuntu 22.04","Platform":"Ubuntu","Architecture":"x86_64"}
		]}}`
	})
	imgs, err := p.ListImages(context.Background(), "cn-hangzhou")
	if err != nil {
		t.Fatal(err)
	}
	if len(imgs) != 2 {
		t.Fatalf("got %d images", len(imgs))
	}
	if imgs[0].ID != "ubuntu_22" {
		t.Errorf("Ubuntu should rank first, got %q", imgs[0].ID)
	}
	if imgs[0].Platform != "Ubuntu" || imgs[0].Arch != "x86_64" {
		t.Errorf("fields not parsed: %+v", imgs[0])
	}
}

func TestAliyunListInstanceTypesFiltersByAvailabilityAndCPU(t *testing.T) {
	p := &aliyunProvider{cred: Credential{AccessKeyID: "ak", AccessKeySecret: "sk", Region: "cn-hangzhou"}}
	p.transport = cannedTransport(func(r *http.Request) string {
		switch aliyunAction(r) {
		case "DescribeInstanceTypes":
			return `{"InstanceTypes":{"InstanceType":[
				{"InstanceTypeId":"ecs.big","CpuCoreCount":32,"MemorySize":128},
				{"InstanceTypeId":"ecs.s2","CpuCoreCount":2,"MemorySize":4},
				{"InstanceTypeId":"ecs.s1","CpuCoreCount":1,"MemorySize":1},
				{"InstanceTypeId":"ecs.unavail","CpuCoreCount":2,"MemorySize":8}
			]}}`
		case "DescribeAvailableResource":
			return `{"AvailableZones":{"AvailableZone":[{"AvailableResources":{"AvailableResource":[{"SupportedResources":{"SupportedResource":[
				{"Value":"ecs.s1","Status":"Available"},
				{"Value":"ecs.s2","Status":"Available"},
				{"Value":"ecs.big","Status":"Available"}
			]}}]}}]}}`
		}
		return `{}`
	})
	ts, err := p.ListInstanceTypes(context.Background(), "cn-hangzhou", "")
	if err != nil {
		t.Fatal(err)
	}
	// ecs.big dropped by CPU cap; ecs.unavail dropped by availability; sorted asc.
	if len(ts) != 2 || ts[0].ID != "ecs.s1" || ts[1].ID != "ecs.s2" {
		t.Fatalf("got %+v, want [ecs.s1 ecs.s2]", ts)
	}
	if ts[1].CPU != 2 || ts[1].MemoryGiB != 4 {
		t.Errorf("specs not parsed: %+v", ts[1])
	}
}

func TestAliyunListInstanceTypesFallsBackWhenAvailabilityEmpty(t *testing.T) {
	p := &aliyunProvider{cred: Credential{AccessKeyID: "ak", AccessKeySecret: "sk", Region: "cn-hangzhou"}}
	p.transport = cannedTransport(func(r *http.Request) string {
		switch aliyunAction(r) {
		case "DescribeInstanceTypes":
			return `{"InstanceTypes":{"InstanceType":[{"InstanceTypeId":"ecs.s2","CpuCoreCount":2,"MemorySize":4}]}}`
		case "DescribeAvailableResource":
			return `{"AvailableZones":{"AvailableZone":[]}}` // availability unknown
		}
		return `{}`
	})
	ts, err := p.ListInstanceTypes(context.Background(), "cn-hangzhou", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 1 || ts[0].ID != "ecs.s2" {
		t.Fatalf("expected fallback to all specs, got %+v", ts)
	}
}

func TestAliyunDefaultNetworkPrefersDefaultVPC(t *testing.T) {
	p := &aliyunProvider{cred: Credential{AccessKeyID: "ak", AccessKeySecret: "sk", Region: "cn-hangzhou"}}
	p.transport = cannedTransport(func(r *http.Request) string {
		switch aliyunAction(r) {
		case "DescribeVpcs":
			if !strings.HasPrefix(r.URL.Host, "vpc") { // VPC actions go to the VPC product client
				t.Errorf("DescribeVpcs host = %q, want VPC endpoint", r.URL.Host)
			}
			return `{"Vpcs":{"Vpc":[{"VpcId":"vpc-a","IsDefault":false},{"VpcId":"vpc-def","IsDefault":true}]}}`
		case "DescribeVSwitches":
			return `{"VSwitches":{"VSwitch":[{"VSwitchId":"vsw-1","ZoneId":"cn-hangzhou-i","Status":"Available"}]}}`
		case "DescribeSecurityGroups":
			if !strings.HasPrefix(r.URL.Host, "ecs") { // SG actions go to the ECS product client
				t.Errorf("DescribeSecurityGroups host = %q, want ECS endpoint", r.URL.Host)
			}
			return `{"SecurityGroups":{"SecurityGroup":[{"SecurityGroupId":"sg-1"}]}}`
		}
		return `{}`
	})
	nd, err := p.DefaultNetwork(context.Background(), "cn-hangzhou", "")
	if err != nil {
		t.Fatal(err)
	}
	if nd.VPCID != "vpc-def" || nd.VSwitchID != "vsw-1" || nd.SecurityGroupID != "sg-1" || nd.ZoneID != "cn-hangzhou-i" {
		t.Fatalf("got %+v", nd)
	}
}

// --- tencent ---

func TestTencentListImagesRanksUbuntuFirst(t *testing.T) {
	p := &tencentProvider{cred: Credential{AccessKeyID: "ak", AccessKeySecret: "sk", Region: "ap-guangzhou"}}
	p.transport = cannedTransport(func(r *http.Request) string {
		if a := tencentAction(r); a != "DescribeImages" {
			t.Errorf("X-TC-Action = %q, want DescribeImages", a)
		}
		return `{"Response":{"ImageSet":[
			{"ImageId":"img-centos","OsName":"CentOS 7.9 64bit","Platform":"CentOS","Architecture":"x86_64","ImageState":"NORMAL"},
			{"ImageId":"img-ubuntu","OsName":"Ubuntu Server 22.04 LTS 64bit","Platform":"Ubuntu","Architecture":"x86_64","ImageState":"NORMAL"}
		]}}`
	})
	imgs, err := p.ListImages(context.Background(), "ap-guangzhou")
	if err != nil {
		t.Fatal(err)
	}
	if len(imgs) != 2 || imgs[0].ID != "img-ubuntu" {
		t.Fatalf("got %+v, want Ubuntu first", imgs)
	}
}

func TestTencentListInstanceTypesDedupAndSell(t *testing.T) {
	p := &tencentProvider{cred: Credential{AccessKeyID: "ak", AccessKeySecret: "sk", Region: "ap-guangzhou"}}
	p.transport = cannedTransport(func(r *http.Request) string {
		if a := tencentAction(r); a != "DescribeZoneInstanceConfigInfos" {
			t.Errorf("X-TC-Action = %q", a)
		}
		return `{"Response":{"InstanceTypeQuotaSet":[
			{"InstanceType":"S5.LARGE16","Cpu":4,"Memory":16,"Status":"SELL"},
			{"InstanceType":"S5.MEDIUM2","Cpu":2,"Memory":2,"Status":"SELL"},
			{"InstanceType":"S5.MEDIUM2","Cpu":2,"Memory":2,"Status":"SELL"},
			{"InstanceType":"BIG","Cpu":48,"Memory":128,"Status":"SELL"},
			{"InstanceType":"SOLD","Cpu":2,"Memory":4,"Status":"SOLD_OUT"}
		]}}`
	})
	ts, err := p.ListInstanceTypes(context.Background(), "ap-guangzhou", "")
	if err != nil {
		t.Fatal(err)
	}
	// BIG dropped (CPU cap), SOLD dropped (status), MEDIUM2 deduped; sorted asc.
	if len(ts) != 2 || ts[0].ID != "S5.MEDIUM2" || ts[1].ID != "S5.LARGE16" {
		t.Fatalf("got %+v, want [S5.MEDIUM2 S5.LARGE16]", ts)
	}
}

func TestTencentDefaultNetwork(t *testing.T) {
	p := &tencentProvider{cred: Credential{AccessKeyID: "ak", AccessKeySecret: "sk", Region: "ap-guangzhou"}}
	p.transport = cannedTransport(func(r *http.Request) string {
		switch tencentAction(r) {
		case "DescribeVpcs":
			if r.URL.Host != "vpc.tencentcloudapi.com" {
				t.Errorf("DescribeVpcs host = %q, want vpc service", r.URL.Host)
			}
			return `{"Response":{"VpcSet":[{"VpcId":"vpc-1","IsDefault":true}]}}`
		case "DescribeSubnets":
			return `{"Response":{"SubnetSet":[{"SubnetId":"subnet-1","Zone":"ap-guangzhou-3"}]}}`
		case "DescribeSecurityGroups":
			return `{"Response":{"SecurityGroupSet":[{"SecurityGroupId":"sg-1"}]}}`
		}
		return `{"Response":{}}`
	})
	nd, err := p.DefaultNetwork(context.Background(), "ap-guangzhou", "")
	if err != nil {
		t.Fatal(err)
	}
	if nd.VPCID != "vpc-1" || nd.VSwitchID != "subnet-1" || nd.SecurityGroupID != "sg-1" || nd.ZoneID != "ap-guangzhou-3" {
		t.Fatalf("got %+v", nd)
	}
}

// --- EnsureNetwork / TeardownNetwork ---

func sawAction(actions []string, want string) bool {
	for _, a := range actions {
		if a == want {
			return true
		}
	}
	return false
}

// A never-used aliyun region (no VPC) gets a full VPC+vSwitch+SG provisioned,
// with inbound opened, and the created ids reported for later teardown.
func TestAliyunEnsureNetworkCreatesWhenEmpty(t *testing.T) {
	old := networkReadyInterval
	networkReadyInterval = time.Millisecond
	defer func() { networkReadyInterval = old }()

	p := &aliyunProvider{cred: Credential{AccessKeyID: "ak", AccessKeySecret: "sk", Region: "cn-wulanchabu"}}
	var actions []string
	p.transport = cannedTransport(func(r *http.Request) string {
		a := aliyunAction(r)
		actions = append(actions, a)
		switch a {
		case "DescribeVpcs":
			return `{"Vpcs":{"Vpc":[]}}` // region is empty → must create
		case "CreateVpc":
			return `{"VpcId":"vpc-new"}`
		case "DescribeZones":
			return `{"Zones":{"Zone":[{"ZoneId":"cn-wulanchabu-a"}]}}`
		case "CreateVSwitch":
			return `{"VSwitchId":"vsw-new"}`
		case "DescribeVSwitches":
			return `{"VSwitches":{"VSwitch":[{"VSwitchId":"vsw-new","Status":"Available"}]}}`
		case "CreateSecurityGroup":
			return `{"SecurityGroupId":"sg-new"}`
		}
		return `{}`
	})

	resolved, created, err := p.EnsureNetwork(context.Background(), "cn-wulanchabu", "", "dep-1")
	if err != nil {
		t.Fatal(err)
	}
	want := NetworkDefaults{ZoneID: "cn-wulanchabu-a", VPCID: "vpc-new", VSwitchID: "vsw-new", SecurityGroupID: "sg-new"}
	if resolved != want {
		t.Fatalf("resolved = %+v, want %+v", resolved, want)
	}
	if created.VPCID != "vpc-new" || created.VSwitchID != "vsw-new" || created.SecurityGroupID != "sg-new" {
		t.Fatalf("created = %+v, want all three new ids", created)
	}
	if !sawAction(actions, "AuthorizeSecurityGroup") {
		t.Fatalf("inbound rule was not opened; actions = %v", actions)
	}
}

// An existing, complete aliyun network is reused untouched — nothing created.
func TestAliyunEnsureNetworkReusesExisting(t *testing.T) {
	p := &aliyunProvider{cred: Credential{AccessKeyID: "ak", AccessKeySecret: "sk", Region: "cn-hangzhou"}}
	p.transport = cannedTransport(func(r *http.Request) string {
		switch aliyunAction(r) {
		case "DescribeVpcs":
			return `{"Vpcs":{"Vpc":[{"VpcId":"vpc-1","IsDefault":true}]}}`
		case "DescribeVSwitches":
			return `{"VSwitches":{"VSwitch":[{"VSwitchId":"vsw-1","ZoneId":"cn-hangzhou-i","Status":"Available"}]}}`
		case "DescribeSecurityGroups":
			return `{"SecurityGroups":{"SecurityGroup":[{"SecurityGroupId":"sg-1"}]}}`
		case "CreateVpc", "CreateVSwitch", "CreateSecurityGroup":
			t.Errorf("must not create when a network already exists (%s)", aliyunAction(r))
		}
		return `{}`
	})
	resolved, created, err := p.EnsureNetwork(context.Background(), "cn-hangzhou", "", "dep-1")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.VPCID != "vpc-1" || resolved.VSwitchID != "vsw-1" || resolved.SecurityGroupID != "sg-1" {
		t.Fatalf("resolved = %+v", resolved)
	}
	if !created.Empty() {
		t.Fatalf("nothing should be created, got %+v", created)
	}
}

// The reported bug: a region with a VPC + security group but no vSwitch. The
// missing vSwitch must be created inside that VPC, carved from the VPC's own
// CIDR (here 172.16/12) — not a fixed block the VPC wouldn't contain.
func TestAliyunEnsureNetworkCreatesVSwitchInExistingVPC(t *testing.T) {
	old := networkReadyInterval
	networkReadyInterval = time.Millisecond
	defer func() { networkReadyInterval = old }()

	p := &aliyunProvider{cred: Credential{AccessKeyID: "ak", AccessKeySecret: "sk", Region: "cn-wulanchabu"}}
	createdCIDR := ""
	p.transport = cannedTransport(func(r *http.Request) string {
		switch aliyunAction(r) {
		case "DescribeVpcs":
			return `{"Vpcs":{"Vpc":[{"VpcId":"vpc-1","IsDefault":true,"CidrBlock":"172.16.0.0/12"}]}}`
		case "DescribeVSwitches":
			if aliyunReqParam(r, "VSwitchId") != "" { // waitVSwitchAvailable
				return `{"VSwitches":{"VSwitch":[{"VSwitchId":"vsw-new","Status":"Available"}]}}`
			}
			return `{"VSwitches":{"VSwitch":[]}}` // discovery: none yet
		case "DescribeSecurityGroups":
			return `{"SecurityGroups":{"SecurityGroup":[{"SecurityGroupId":"sg-1"}]}}`
		case "DescribeZones":
			return `{"Zones":{"Zone":[{"ZoneId":"cn-wulanchabu-a"}]}}`
		case "CreateVSwitch":
			createdCIDR = aliyunReqParam(r, "CidrBlock")
			return `{"VSwitchId":"vsw-new"}`
		case "CreateVpc", "CreateSecurityGroup":
			t.Errorf("VPC and SG already exist; must not create %s", aliyunAction(r))
		}
		return `{}`
	})

	resolved, created, err := p.EnsureNetwork(context.Background(), "cn-wulanchabu", "", "dep-1")
	if err != nil {
		t.Fatal(err)
	}
	if createdCIDR != "172.16.0.0/24" {
		t.Fatalf("vswitch carved CIDR = %q, want 172.16.0.0/24", createdCIDR)
	}
	if resolved.VPCID != "vpc-1" || resolved.VSwitchID != "vsw-new" || resolved.SecurityGroupID != "sg-1" {
		t.Fatalf("resolved = %+v", resolved)
	}
	// Only the vSwitch was created — the user's VPC and SG must not be reclaimed.
	if created.VPCID != "" || created.SecurityGroupID != "" || created.VSwitchID != "vsw-new" {
		t.Fatalf("created = %+v, want only vsw-new", created)
	}
}

func TestAliyunTeardownNetworkDeletesInOrder(t *testing.T) {
	p := &aliyunProvider{cred: Credential{AccessKeyID: "ak", AccessKeySecret: "sk", Region: "cn-wulanchabu"}}
	var actions []string
	p.transport = cannedTransport(func(r *http.Request) string {
		actions = append(actions, aliyunAction(r))
		return `{}`
	})
	err := p.TeardownNetwork(context.Background(), "cn-wulanchabu", NetworkDefaults{VPCID: "vpc-1", VSwitchID: "vsw-1", SecurityGroupID: "sg-1"})
	if err != nil {
		t.Fatal(err)
	}
	// security group, then vSwitch, then VPC.
	if len(actions) != 3 || actions[0] != "DeleteSecurityGroup" || actions[1] != "DeleteVSwitch" || actions[2] != "DeleteVpc" {
		t.Fatalf("teardown order = %v", actions)
	}
}

// A never-used tencent region provisions a default VPC+subnet and a security group.
func TestTencentEnsureNetworkCreatesWhenEmpty(t *testing.T) {
	p := &tencentProvider{cred: Credential{AccessKeyID: "ak", AccessKeySecret: "sk", Region: "ap-guangzhou"}}
	p.transport = cannedTransport(func(r *http.Request) string {
		switch tencentAction(r) {
		case "DescribeVpcs":
			return `{"Response":{"VpcSet":[]}}`
		case "DescribeSecurityGroups":
			return `{"Response":{"SecurityGroupSet":[]}}`
		case "DescribeZones":
			return `{"Response":{"ZoneSet":[{"Zone":"ap-guangzhou-3","ZoneState":"AVAILABLE"}]}}`
		case "CreateDefaultVpc":
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"Force":true`) {
				t.Fatalf("CreateDefaultVpc payload should force creation, got %s", string(body))
			}
			return `{"Response":{"Vpc":{"VpcId":"vpc-1","SubnetId":"subnet-1"}}}`
		case "CreateSecurityGroupWithPolicies":
			return `{"Response":{"SecurityGroup":{"SecurityGroupId":"sg-1"}}}`
		}
		return `{"Response":{}}`
	})
	resolved, created, err := p.EnsureNetwork(context.Background(), "ap-guangzhou", "", "dep-1")
	if err != nil {
		t.Fatal(err)
	}
	want := NetworkDefaults{ZoneID: "ap-guangzhou-3", VPCID: "vpc-1", VSwitchID: "subnet-1", SecurityGroupID: "sg-1"}
	if resolved != want {
		t.Fatalf("resolved = %+v, want %+v", resolved, want)
	}
	if created.VPCID != "vpc-1" || created.VSwitchID != "subnet-1" || created.SecurityGroupID != "sg-1" {
		t.Fatalf("created = %+v", created)
	}
}

func TestTencentCreateDefaultVPCRejectsZeroIDs(t *testing.T) {
	p := &tencentProvider{cred: Credential{AccessKeyID: "ak", AccessKeySecret: "sk", Region: "ap-guangzhou"}}
	p.transport = cannedTransport(func(r *http.Request) string {
		if tencentAction(r) != "CreateDefaultVpc" {
			t.Errorf("X-TC-Action = %q, want CreateDefaultVpc", tencentAction(r))
		}
		return `{"Response":{"Vpc":{"VpcId":"0","SubnetId":"0"}}}`
	})
	_, _, err := p.createDefaultVPC(context.Background(), "ap-guangzhou", "ap-guangzhou-3", "dep-1")
	if err == nil || !strings.Contains(err.Error(), "unusable ids") {
		t.Fatalf("expected unusable ids error, got %v", err)
	}
}

func TestAliyunEnsureNetworkRecordsSGWhenAuthorizeFails(t *testing.T) {
	old := networkReadyInterval
	networkReadyInterval = time.Millisecond
	defer func() { networkReadyInterval = old }()

	p := &aliyunProvider{cred: Credential{AccessKeyID: "ak", AccessKeySecret: "sk", Region: "cn-wulanchabu"}}
	p.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		status := 200
		body := `{}`
		switch aliyunAction(r) {
		case "DescribeVpcs":
			body = `{"Vpcs":{"Vpc":[]}}`
		case "CreateVpc":
			body = `{"VpcId":"vpc-new"}`
		case "DescribeZones":
			body = `{"Zones":{"Zone":[{"ZoneId":"cn-wulanchabu-a"}]}}`
		case "CreateVSwitch":
			body = `{"VSwitchId":"vsw-new"}`
		case "DescribeVSwitches":
			body = `{"VSwitches":{"VSwitch":[{"VSwitchId":"vsw-new","Status":"Available"}]}}`
		case "CreateSecurityGroup":
			body = `{"SecurityGroupId":"sg-new"}`
		case "AuthorizeSecurityGroup":
			status = 400
			body = `{"Code":"Forbidden","Message":"denied"}`
		}
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})

	_, created, err := p.EnsureNetwork(context.Background(), "cn-wulanchabu", "", "dep-1")
	if err == nil || !strings.Contains(err.Error(), "authorize security group") {
		t.Fatalf("expected authorize error, got %v", err)
	}
	if created.VPCID != "vpc-new" || created.VSwitchID != "vsw-new" || created.SecurityGroupID != "sg-new" {
		t.Fatalf("created resources should be reported for reclaim, got %+v", created)
	}
}

// aliyunReqParam reads a single RPC parameter from an SDK-built request, checking
// the query string then the form-encoded body.
func aliyunReqParam(r *http.Request, key string) string {
	if v := r.URL.Query().Get(key); v != "" {
		return v
	}
	if r.Body != nil {
		b, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(b))
		if vals, err := url.ParseQuery(string(b)); err == nil {
			return vals.Get(key)
		}
	}
	return ""
}
