package cloud

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	ecs "github.com/alibabacloud-go/ecs-20140526/v6/client"
	util "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/alibabacloud-go/tea/tea"
	vpc "github.com/alibabacloud-go/vpc-20160428/v6/client"
)

// aliyunProvider talks to the ECS (2014-05-26) and VPC (2016-04-08) APIs via the
// official modular alibabacloud-go clients.
type aliyunProvider struct {
	cred Credential
	// transport, when set, overrides the SDK client's HTTP transport. Tests inject
	// a RoundTripper here to feed canned responses; production leaves it nil.
	transport http.RoundTripper
}

func (p *aliyunProvider) Name() string { return "aliyun" }

// roundTripHTTPClient adapts an http.RoundTripper to the dara.HttpClient
// interface the aliyun SDK uses, so tests can inject canned responses uniformly.
type roundTripHTTPClient struct{ rt http.RoundTripper }

func (c roundTripHTTPClient) Call(req *http.Request, _ *http.Transport) (*http.Response, error) {
	return c.rt.RoundTrip(req)
}

// config builds an openapi.Config for region, leaving Endpoint unset so each
// product client's Init resolves the regional host from RegionId itself
// (ecs.{region}.aliyuncs.com / vpc.{region}.aliyuncs.com via the SDK's endpoint
// rules). The test transport, if set, is applied.
func (p *aliyunProvider) config(region string) *openapi.Config {
	cfg := &openapi.Config{
		AccessKeyId:     tea.String(p.cred.AccessKeyID),
		AccessKeySecret: tea.String(p.cred.AccessKeySecret),
		RegionId:        tea.String(firstNonEmpty(region, p.cred.Region)),
	}
	if p.transport != nil {
		cfg.HttpClient = roundTripHTTPClient{rt: p.transport}
	}
	return cfg
}

func (p *aliyunProvider) ecsClient(region string) (*ecs.Client, error) {
	return ecs.NewClient(p.config(region))
}

func (p *aliyunProvider) vpcClient(region string) (*vpc.Client, error) {
	return vpc.NewClient(p.config(region))
}

// Aliyun per-call HTTP bounds. The darabonba SDK takes no context.Context, so
// the interface's ctx is otherwise dropped and an unset timeout resolves to 0
// (no timeout) in the vendored client — a stalled endpoint would hang
// deploy/recycle forever and strand billable ECS. Bounding every call via the
// RuntimeOptions is the effective backstop (contrast tencent.go's WithContext).
const (
	aliyunConnectTimeoutMS = 10_000
	aliyunReadTimeoutMS    = 60_000
)

func aliyunRuntime() *util.RuntimeOptions {
	return &util.RuntimeOptions{
		ConnectTimeout: tea.Int(aliyunConnectTimeoutMS),
		ReadTimeout:    tea.Int(aliyunReadTimeoutMS),
	}
}

// ListRegions calls DescribeRegions, a global action needing only AK/SK.
// AcceptLanguage gives Chinese names.
func (p *aliyunProvider) ListRegions(ctx context.Context) ([]Region, error) {
	// DescribeRegions is region-agnostic, but the SDK resolves the request host
	// from RegionId, so pin a valid default when the credential has none.
	cli, err := p.ecsClient(firstNonEmpty(p.cred.Region, "cn-hangzhou"))
	if err != nil {
		return nil, err
	}
	req := &ecs.DescribeRegionsRequest{AcceptLanguage: tea.String("zh-CN")}
	resp, err := cli.DescribeRegionsWithOptions(req, aliyunRuntime())
	if err != nil {
		return nil, err
	}
	if resp.Body == nil || resp.Body.Regions == nil {
		return nil, nil
	}
	regions := make([]Region, 0, len(resp.Body.Regions.Region))
	for _, r := range resp.Body.Regions.Region {
		regions = append(regions, Region{ID: tea.StringValue(r.RegionId), LocalName: tea.StringValue(r.LocalName)})
	}
	return regions, nil
}

func (p *aliyunProvider) CreateInstances(ctx context.Context, req CreateRequest) ([]Instance, error) {
	region := firstNonEmpty(req.Region, p.cred.Region)
	if region == "" {
		return nil, fmt.Errorf("aliyun: region is required")
	}
	count := req.Count
	if count <= 0 {
		count = 1
	}
	r := &ecs.RunInstancesRequest{
		RegionId:           tea.String(region),
		ImageId:            tea.String(req.ImageID),
		InstanceType:       tea.String(req.InstanceType),
		Amount:             tea.Int32(int32(count)),
		InstanceName:       tea.String(firstNonEmpty(req.Name, "aiscan-agent")),
		InstanceChargeType: tea.String("PostPaid"),
	}
	if req.SecurityGroupID != "" {
		r.SecurityGroupId = tea.String(req.SecurityGroupID)
	}
	if req.VSwitchID != "" {
		r.VSwitchId = tea.String(req.VSwitchID)
	}
	if req.ZoneID != "" {
		r.ZoneId = tea.String(req.ZoneID)
	}
	if req.BandwidthOut > 0 {
		r.InternetMaxBandwidthOut = tea.Int32(int32(req.BandwidthOut))
		r.InternetChargeType = tea.String("PayByTraffic")
	}
	if req.UserData != "" {
		r.UserData = tea.String(base64.StdEncoding.EncodeToString([]byte(req.UserData)))
	}
	for k, v := range ownershipTags(req.Tags) {
		r.Tag = append(r.Tag, &ecs.RunInstancesRequestTag{Key: tea.String(k), Value: tea.String(v)})
	}

	cli, err := p.ecsClient(region)
	if err != nil {
		return nil, err
	}
	resp, err := cli.RunInstancesWithOptions(r, aliyunRuntime())
	if err != nil {
		return nil, err
	}
	var ids []*string
	if resp.Body != nil && resp.Body.InstanceIdSets != nil {
		ids = resp.Body.InstanceIdSets.InstanceIdSet
	}
	insts := make([]Instance, 0, len(ids))
	for _, id := range ids {
		insts = append(insts, Instance{ID: tea.StringValue(id), Status: "Pending"})
	}
	return insts, nil
}

// ListImages returns Available linux system (public) images for the region,
// Ubuntu/LTS ranked first.
func (p *aliyunProvider) ListImages(ctx context.Context, region string) ([]Image, error) {
	region = firstNonEmpty(region, p.cred.Region)
	if region == "" {
		return nil, fmt.Errorf("aliyun: region is required")
	}
	r := &ecs.DescribeImagesRequest{
		RegionId:        tea.String(region),
		Status:          tea.String("Available"),
		ImageOwnerAlias: tea.String("system"),
		OSType:          tea.String("linux"),
		PageSize:        tea.Int32(100),
	}
	cli, err := p.ecsClient(region)
	if err != nil {
		return nil, err
	}
	resp, err := cli.DescribeImagesWithOptions(r, aliyunRuntime())
	if err != nil {
		return nil, err
	}
	if resp.Body == nil || resp.Body.Images == nil {
		return nil, nil
	}
	imgs := make([]Image, 0, len(resp.Body.Images.Image))
	for _, im := range resp.Body.Images.Image {
		imgs = append(imgs, Image{
			ID:       tea.StringValue(im.ImageId),
			Name:     firstNonEmpty(tea.StringValue(im.OSNameEn), tea.StringValue(im.ImageName)),
			OSName:   firstNonEmpty(tea.StringValue(im.OSName), tea.StringValue(im.OSNameEn)),
			Platform: tea.StringValue(im.Platform),
			Arch:     tea.StringValue(im.Architecture),
		})
	}
	sortImages(imgs)
	return capList(imgs, 100), nil
}

// ListInstanceTypes returns entry-level instance specs, region/zone-availability
// filtered when DescribeAvailableResource succeeds (otherwise all specs).
func (p *aliyunProvider) ListInstanceTypes(ctx context.Context, region, zone string) ([]InstanceType, error) {
	region = firstNonEmpty(region, p.cred.Region)
	if region == "" {
		return nil, fmt.Errorf("aliyun: region is required")
	}
	specs, err := p.describeInstanceTypeSpecs(ctx, region)
	if err != nil {
		return nil, err
	}
	avail, _ := p.describeAvailableTypes(ctx, region, zone) // best-effort
	out := make([]InstanceType, 0, len(specs))
	for _, t := range specs {
		if avail != nil && !avail[t.ID] {
			continue
		}
		out = append(out, t)
	}
	if len(out) == 0 { // availability filtered everything out (or empty) -> fall back
		out = specs
	}
	sortInstanceTypes(out)
	return capList(out, 80), nil
}

// describeInstanceTypeSpecs lists global instance-type specs, keeping only
// entry-level sizes (<= maxInstanceTypeCPU vCPU).
func (p *aliyunProvider) describeInstanceTypeSpecs(ctx context.Context, region string) ([]InstanceType, error) {
	cli, err := p.ecsClient(region)
	if err != nil {
		return nil, err
	}
	resp, err := cli.DescribeInstanceTypesWithOptions(&ecs.DescribeInstanceTypesRequest{}, aliyunRuntime())
	if err != nil {
		return nil, err
	}
	if resp.Body == nil || resp.Body.InstanceTypes == nil {
		return nil, nil
	}
	types := make([]InstanceType, 0, len(resp.Body.InstanceTypes.InstanceType))
	for _, t := range resp.Body.InstanceTypes.InstanceType {
		cpu := int(tea.Int32Value(t.CpuCoreCount))
		if cpu <= 0 || cpu > maxInstanceTypeCPU {
			continue
		}
		types = append(types, InstanceType{ID: tea.StringValue(t.InstanceTypeId), CPU: cpu, MemoryGiB: float64(tea.Float32Value(t.MemorySize))})
	}
	return types, nil
}

// describeAvailableTypes returns the set of instance types purchasable (PostPaid)
// in the region/zone. A nil map means "availability unknown" (caller keeps all).
func (p *aliyunProvider) describeAvailableTypes(ctx context.Context, region, zone string) (map[string]bool, error) {
	r := &ecs.DescribeAvailableResourceRequest{
		RegionId:            tea.String(region),
		DestinationResource: tea.String("InstanceType"),
		InstanceChargeType:  tea.String("PostPaid"),
	}
	if zone != "" {
		r.ZoneId = tea.String(zone)
	}
	cli, err := p.ecsClient(region)
	if err != nil {
		return nil, err
	}
	resp, err := cli.DescribeAvailableResourceWithOptions(r, aliyunRuntime())
	if err != nil {
		return nil, err
	}
	if resp.Body == nil || resp.Body.AvailableZones == nil {
		return nil, nil
	}
	avail := map[string]bool{}
	for _, z := range resp.Body.AvailableZones.AvailableZone {
		if z.AvailableResources == nil {
			continue
		}
		for _, ar := range z.AvailableResources.AvailableResource {
			if ar.SupportedResources == nil {
				continue
			}
			for _, sr := range ar.SupportedResources.SupportedResource {
				status := tea.StringValue(sr.Status)
				if status == "" || strings.EqualFold(status, "Available") {
					avail[tea.StringValue(sr.Value)] = true
				}
			}
		}
	}
	if len(avail) == 0 {
		return nil, nil // treat "nothing reported" as unknown, not "none available"
	}
	return avail, nil
}

// DefaultNetwork resolves a default VPC + VSwitch + security group so a minimal
// deploy can omit them. Best-effort: any unresolved piece is left blank.
func (p *aliyunProvider) DefaultNetwork(ctx context.Context, region, zone string) (NetworkDefaults, error) {
	region = firstNonEmpty(region, p.cred.Region)
	nd := NetworkDefaults{ZoneID: zone}
	if region == "" {
		return nd, fmt.Errorf("aliyun: region is required")
	}
	vpcID := p.findVPC(ctx, region)
	if vpcID == "" {
		return nd, nil
	}
	nd.VPCID = vpcID
	vsw, vswZone := p.findVSwitch(ctx, region, vpcID, zone)
	nd.VSwitchID = vsw
	if nd.ZoneID == "" {
		nd.ZoneID = vswZone
	}
	nd.SecurityGroupID = p.findSecurityGroup(ctx, region, vpcID)
	return nd, nil
}

// EnsureNetwork discovers the region's default network and creates whatever
// piece is missing (VPC, vSwitch, security group) so a deploy always has a
// valid, consistent VPC trio to launch into. See the Provider interface.
func (p *aliyunProvider) EnsureNetwork(ctx context.Context, region, zone, label string) (NetworkDefaults, NetworkDefaults, error) {
	region = firstNonEmpty(region, p.cred.Region)
	var created NetworkDefaults
	if region == "" {
		return NetworkDefaults{}, created, fmt.Errorf("aliyun: region is required")
	}
	nd, _ := p.DefaultNetwork(ctx, region, zone) // best-effort discovery

	vpcCIDR := autoVPCCIDR
	if nd.VPCID == "" {
		id, err := p.createVPC(ctx, region, label)
		if err != nil {
			return nd, created, fmt.Errorf("create vpc: %w", err)
		}
		nd.VPCID, created.VPCID = id, id
	} else {
		// Carve the new vSwitch from the existing VPC's own block, not a fixed one.
		vpcCIDR = firstNonEmpty(p.vpcCIDR(ctx, region, nd.VPCID), autoVPCCIDR)
	}
	if nd.VSwitchID == "" {
		z := firstNonEmpty(nd.ZoneID, zone, p.firstZone(ctx, region))
		id, err := p.createVSwitch(ctx, region, nd.VPCID, z, carveSubnetCIDR(vpcCIDR), label)
		if err != nil {
			return nd, created, fmt.Errorf("create vswitch: %w", err)
		}
		nd.VSwitchID, created.VSwitchID = id, id
		if nd.ZoneID == "" {
			nd.ZoneID = z
		}
	}
	if nd.SecurityGroupID == "" {
		id, err := p.CreateSecurityGroup(ctx, region, nd.VPCID, label)
		if id != "" {
			nd.SecurityGroupID, created.SecurityGroupID = id, id
		}
		if err != nil {
			return nd, created, fmt.Errorf("create security group: %w", err)
		}
	}
	return nd, created, nil
}

// TeardownNetwork deletes the auto-created resources in dependency order
// (security group → vSwitch → VPC). See the Provider interface.
func (p *aliyunProvider) TeardownNetwork(ctx context.Context, region string, created NetworkDefaults) error {
	region = firstNonEmpty(region, p.cred.Region)
	return teardownNetwork(ctx, region, "vswitch", "aliyun", created, p.deleteNetResource)
}

// createVPC creates an auto VPC and returns its id.
func (p *aliyunProvider) createVPC(ctx context.Context, region, label string) (string, error) {
	cli, err := p.vpcClient(region)
	if err != nil {
		return "", err
	}
	r := &vpc.CreateVpcRequest{
		RegionId:  tea.String(region),
		CidrBlock: tea.String(autoVPCCIDR),
		VpcName:   tea.String(netName(label)),
	}
	resp, err := cli.CreateVpcWithOptions(r, aliyunRuntime())
	if err != nil {
		return "", err
	}
	if resp.Body == nil {
		return "", fmt.Errorf("empty vpc id")
	}
	return tea.StringValue(resp.Body.VpcId), nil
}

// createVSwitch creates an auto vSwitch (cidr within its VPC) in vpcID/zone. A
// just-created VPC may still be Pending, so the create retries through that
// transient state; the new vSwitch is then polled until Available so the launch
// doesn't race it.
func (p *aliyunProvider) createVSwitch(ctx context.Context, region, vpcID, zone, cidr, label string) (string, error) {
	if zone == "" {
		return "", fmt.Errorf("no available zone for vswitch in %s", region)
	}
	var id string
	err := deleteWithRetry(ctx, func() error {
		cli, err := p.vpcClient(region)
		if err != nil {
			return err
		}
		r := &vpc.CreateVSwitchRequest{
			RegionId:    tea.String(region),
			VpcId:       tea.String(vpcID),
			ZoneId:      tea.String(zone),
			CidrBlock:   tea.String(cidr),
			VSwitchName: tea.String(netName(label)),
		}
		resp, err := cli.CreateVSwitchWithOptions(r, aliyunRuntime())
		if err != nil {
			return err
		}
		if resp.Body != nil {
			id = tea.StringValue(resp.Body.VSwitchId)
		}
		return nil
	}, isAliyunNetPending)
	if err != nil {
		return "", err
	}
	p.waitVSwitchAvailable(ctx, region, id)
	return id, nil
}

// vpcCIDR returns the existing VPC's primary CIDR block, "" if it can't be read.
func (p *aliyunProvider) vpcCIDR(ctx context.Context, region, vpcID string) string {
	cli, err := p.vpcClient(region)
	if err != nil {
		return ""
	}
	r := &vpc.DescribeVpcsRequest{RegionId: tea.String(region), VpcId: tea.String(vpcID)}
	resp, err := cli.DescribeVpcsWithOptions(r, aliyunRuntime())
	if err == nil && resp.Body != nil && resp.Body.Vpcs != nil && len(resp.Body.Vpcs.Vpc) > 0 {
		return tea.StringValue(resp.Body.Vpcs.Vpc[0].CidrBlock)
	}
	return ""
}

// CreateSecurityGroup creates a caller-owned security group in vpcID and opens
// inbound SSH. Egress is allow-all by default on a new aliyun group, so the
// agent's outbound dial to the hub works without an explicit rule.
func (p *aliyunProvider) CreateSecurityGroup(ctx context.Context, region, vpcID, label string) (string, error) {
	region = firstNonEmpty(region, p.cred.Region)
	cli, err := p.ecsClient(region)
	if err != nil {
		return "", err
	}
	r := &ecs.CreateSecurityGroupRequest{
		RegionId:          tea.String(region),
		SecurityGroupName: tea.String(netName(label)),
	}
	if vpcID != "" {
		r.VpcId = tea.String(vpcID)
	}
	resp, err := cli.CreateSecurityGroupWithOptions(r, aliyunRuntime())
	if err != nil {
		return "", err
	}
	sgID := ""
	if resp.Body != nil {
		sgID = tea.StringValue(resp.Body.SecurityGroupId)
	}
	if sgID == "" {
		return "", fmt.Errorf("empty security group id")
	}
	if err := p.authorizeSSH(ctx, region, sgID, 22); err != nil {
		return sgID, fmt.Errorf("authorize security group: %w", err)
	}
	return sgID, nil
}

// authorizeSSH authorizes inbound TCP on port from anywhere in the group.
func (p *aliyunProvider) authorizeSSH(ctx context.Context, region, sgID string, port int) error {
	cli, err := p.ecsClient(region)
	if err != nil {
		return err
	}
	r := &ecs.AuthorizeSecurityGroupRequest{
		RegionId:        tea.String(region),
		SecurityGroupId: tea.String(sgID),
		IpProtocol:      tea.String("tcp"),
		PortRange:       tea.String(fmt.Sprintf("%d/%d", port, port)),
		SourceCidrIp:    tea.String("0.0.0.0/0"),
		Policy:          tea.String("accept"),
	}
	_, err = cli.AuthorizeSecurityGroupWithOptions(r, aliyunRuntime())
	if isAliyunDuplicateRule(err) {
		return nil
	}
	return err
}

// OpenPort authorizes inbound TCP on port (from anywhere) in the security group.
// See the Provider interface. Aliyun's AuthorizeSecurityGroup is idempotent, so a
// repeated identical rule succeeds as a no-op.
func (p *aliyunProvider) OpenPort(ctx context.Context, region, sgID string, port int) error {
	region = firstNonEmpty(region, p.cred.Region)
	if sgID == "" {
		return fmt.Errorf("aliyun: security group id required to open port")
	}
	return p.authorizeSSH(ctx, region, sgID, port)
}

func isAliyunDuplicateRule(err error) bool {
	return codeMatches(err, aliyunCode, "Duplicate")
}

// firstZone returns the region's first zone id, "" if none could be listed.
func (p *aliyunProvider) firstZone(ctx context.Context, region string) string {
	cli, err := p.ecsClient(region)
	if err != nil {
		return ""
	}
	r := &ecs.DescribeZonesRequest{RegionId: tea.String(region)}
	resp, err := cli.DescribeZonesWithOptions(r, aliyunRuntime())
	if err != nil || resp.Body == nil || resp.Body.Zones == nil || len(resp.Body.Zones.Zone) == 0 {
		return ""
	}
	return tea.StringValue(resp.Body.Zones.Zone[0].ZoneId)
}

// waitVSwitchAvailable polls (briefly, best-effort) until the vSwitch is
// Available so an immediate RunInstances doesn't hit InvalidVSwitchId.NotFound.
func (p *aliyunProvider) waitVSwitchAvailable(ctx context.Context, region, vswID string) {
	deadline := time.Now().Add(networkReadyTimeout)
	for {
		cli, err := p.vpcClient(region)
		if err == nil {
			r := &vpc.DescribeVSwitchesRequest{RegionId: tea.String(region), VSwitchId: tea.String(vswID)}
			resp, err := cli.DescribeVSwitchesWithOptions(r, aliyunRuntime())
			if err == nil && resp.Body != nil && resp.Body.VSwitches != nil && len(resp.Body.VSwitches.VSwitch) > 0 {
				if strings.EqualFold(tea.StringValue(resp.Body.VSwitches.VSwitch[0].Status), "Available") {
					return
				}
			}
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(networkReadyInterval):
		}
	}
}

// deleteNetResource deletes one network resource, retrying while it is still in
// use (a freshly-terminated instance can pin its vSwitch/group for a few
// seconds) and treating an already-gone resource as success.
func (p *aliyunProvider) deleteNetResource(ctx context.Context, region, kind, id string) error {
	return deleteWithRetry(ctx, func() error {
		var err error
		switch kind {
		case "securitygroup":
			var cli *ecs.Client
			if cli, err = p.ecsClient(region); err == nil {
				r := &ecs.DeleteSecurityGroupRequest{RegionId: tea.String(region), SecurityGroupId: tea.String(id)}
				_, err = cli.DeleteSecurityGroupWithOptions(r, aliyunRuntime())
			}
		case "vswitch":
			var cli *vpc.Client
			if cli, err = p.vpcClient(region); err == nil {
				r := &vpc.DeleteVSwitchRequest{RegionId: tea.String(region), VSwitchId: tea.String(id)}
				_, err = cli.DeleteVSwitchWithOptions(r, aliyunRuntime())
			}
		case "vpc":
			var cli *vpc.Client
			if cli, err = p.vpcClient(region); err == nil {
				r := &vpc.DeleteVpcRequest{RegionId: tea.String(region), VpcId: tea.String(id)}
				_, err = cli.DeleteVpcWithOptions(r, aliyunRuntime())
			}
		default:
			return fmt.Errorf("unknown resource kind %q", kind)
		}
		if err != nil && !isAliyunNotFound(err) {
			return err
		}
		return nil
	}, isAliyunInUse)
}

// aliyunCode extracts the API error code from an SDK error, "" otherwise.
func aliyunCode(err error) string {
	if e, ok := err.(*tea.SDKError); ok {
		return tea.StringValue(e.Code)
	}
	return ""
}

// isAliyunNetPending reports a transient "resource not ready yet" state seen when
// creating a vSwitch against a VPC that is still provisioning.
func isAliyunNetPending(err error) bool {
	return codeMatches(err, aliyunCode,
		"OperationConflict", "IncorrectVpcStatus", "InvalidVpcStatus", "SystemBusy", "Throttling")
}

// isAliyunInUse reports that a resource can't be deleted yet because a dependent
// (an instance or its ENI) still references it — a retry can clear it.
func isAliyunInUse(err error) bool {
	return codeMatches(err, aliyunCode, "DependencyViolation", "InUse")
}

// findVPC returns the region's default VPC id (or the first VPC), "" if none.
func (p *aliyunProvider) findVPC(ctx context.Context, region string) string {
	cli, err := p.vpcClient(region)
	if err != nil {
		return ""
	}
	r := &vpc.DescribeVpcsRequest{RegionId: tea.String(region), PageSize: tea.Int32(50)}
	resp, err := cli.DescribeVpcsWithOptions(r, aliyunRuntime())
	if err != nil || resp.Body == nil || resp.Body.Vpcs == nil || len(resp.Body.Vpcs.Vpc) == 0 {
		return ""
	}
	for _, v := range resp.Body.Vpcs.Vpc {
		if tea.BoolValue(v.IsDefault) {
			return tea.StringValue(v.VpcId)
		}
	}
	return tea.StringValue(resp.Body.Vpcs.Vpc[0].VpcId)
}

// findVSwitch returns an Available VSwitch in the VPC (preferring the requested
// zone) and its zone, ("","") if none.
func (p *aliyunProvider) findVSwitch(ctx context.Context, region, vpcID, zone string) (string, string) {
	cli, err := p.vpcClient(region)
	if err != nil {
		return "", ""
	}
	r := &vpc.DescribeVSwitchesRequest{RegionId: tea.String(region), VpcId: tea.String(vpcID), PageSize: tea.Int32(50)}
	if zone != "" {
		r.ZoneId = tea.String(zone)
	}
	resp, err := cli.DescribeVSwitchesWithOptions(r, aliyunRuntime())
	if err != nil || resp.Body == nil || resp.Body.VSwitches == nil || len(resp.Body.VSwitches.VSwitch) == 0 {
		return "", ""
	}
	for _, v := range resp.Body.VSwitches.VSwitch {
		if status := tea.StringValue(v.Status); status == "" || strings.EqualFold(status, "Available") {
			return tea.StringValue(v.VSwitchId), tea.StringValue(v.ZoneId)
		}
	}
	first := resp.Body.VSwitches.VSwitch[0]
	return tea.StringValue(first.VSwitchId), tea.StringValue(first.ZoneId)
}

// findSecurityGroup returns the first security group in the VPC, "" if none.
func (p *aliyunProvider) findSecurityGroup(ctx context.Context, region, vpcID string) string {
	cli, err := p.ecsClient(region)
	if err != nil {
		return ""
	}
	r := &ecs.DescribeSecurityGroupsRequest{RegionId: tea.String(region), PageSize: tea.Int32(50)}
	if vpcID != "" {
		r.VpcId = tea.String(vpcID)
	}
	resp, err := cli.DescribeSecurityGroupsWithOptions(r, aliyunRuntime())
	if err != nil || resp.Body == nil || resp.Body.SecurityGroups == nil || len(resp.Body.SecurityGroups.SecurityGroup) == 0 {
		return ""
	}
	return tea.StringValue(resp.Body.SecurityGroups.SecurityGroup[0].SecurityGroupId)
}

func (p *aliyunProvider) ListInstances(ctx context.Context, ids []string) ([]Instance, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	region := p.cred.Region
	idsJSON, _ := json.Marshal(ids)
	r := &ecs.DescribeInstancesRequest{
		RegionId:    tea.String(region),
		InstanceIds: tea.String(string(idsJSON)),
		PageSize:    tea.Int32(100),
	}
	cli, err := p.ecsClient(region)
	if err != nil {
		return nil, err
	}
	resp, err := cli.DescribeInstancesWithOptions(r, aliyunRuntime())
	if err != nil {
		return nil, err
	}
	if resp.Body == nil || resp.Body.Instances == nil {
		return nil, nil
	}
	insts := make([]Instance, 0, len(resp.Body.Instances.Instance))
	for _, it := range resp.Body.Instances.Instance {
		insts = append(insts, aliyunInstanceView(it))
	}
	return insts, nil
}

// aliyunCreationLayouts are the timestamp formats Aliyun ECS returns for
// CreationTime (minute precision, always UTC "Z"), tried in order.
var aliyunCreationLayouts = []string{"2006-01-02T15:04Z", time.RFC3339, "2006-01-02T15:04:05Z"}

func parseAliyunTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range aliyunCreationLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func aliyunInstanceView(it *ecs.DescribeInstancesResponseBodyInstancesInstance) Instance {
	inst := Instance{
		ID:        tea.StringValue(it.InstanceId),
		Name:      tea.StringValue(it.InstanceName),
		Status:    tea.StringValue(it.Status),
		CreatedAt: parseAliyunTime(tea.StringValue(it.CreationTime)),
	}
	if it.PublicIpAddress != nil && len(it.PublicIpAddress.IpAddress) > 0 {
		inst.PublicIP = tea.StringValue(it.PublicIpAddress.IpAddress[0])
	}
	if it.VpcAttributes != nil && it.VpcAttributes.PrivateIpAddress != nil && len(it.VpcAttributes.PrivateIpAddress.IpAddress) > 0 {
		inst.PrivateIP = tea.StringValue(it.VpcAttributes.PrivateIpAddress.IpAddress[0])
	}
	return inst
}

// ListOwnedInstances returns every instance in region tagged managed-by-aiscan
// that isn't already terminated, paging through all results. Only tagged
// instances are ever returned, so unrelated user instances stay untouched.
func (p *aliyunProvider) ListOwnedInstances(ctx context.Context, region string) ([]Instance, error) {
	region = firstNonEmpty(region, p.cred.Region)
	if region == "" {
		return nil, fmt.Errorf("aliyun: region is required")
	}
	cli, err := p.ecsClient(region)
	if err != nil {
		return nil, err
	}
	var out []Instance
	for page := int32(1); ; page++ {
		r := &ecs.DescribeInstancesRequest{
			RegionId:   tea.String(region),
			PageNumber: tea.Int32(page),
			PageSize:   tea.Int32(100),
			Tag: []*ecs.DescribeInstancesRequestTag{
				{Key: tea.String(TagManagedBy), Value: tea.String(TagManagedByValue)},
			},
		}
		resp, err := cli.DescribeInstancesWithOptions(r, aliyunRuntime())
		if err != nil {
			return nil, err
		}
		if resp.Body == nil || resp.Body.Instances == nil {
			break
		}
		batch := resp.Body.Instances.Instance
		for _, it := range batch {
			out = append(out, aliyunInstanceView(it))
		}
		total := tea.Int32Value(resp.Body.TotalCount)
		if len(batch) < 100 || int32(len(out)) >= total {
			break
		}
	}
	return out, nil
}

func (p *aliyunProvider) DeleteInstances(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	region := p.cred.Region
	return deleteWithRetry(ctx, func() error {
		cli, err := p.ecsClient(region)
		if err != nil {
			return err
		}
		r := &ecs.DeleteInstancesRequest{
			RegionId:   tea.String(region),
			Force:      tea.Bool(true), // terminate even if running
			InstanceId: tea.StringSlice(ids),
		}
		if _, err := cli.DeleteInstancesWithOptions(r, aliyunRuntime()); err != nil {
			// Treat "already gone" as success for idempotent recycling.
			if isAliyunNotFound(err) {
				return nil
			}
			return err
		}
		return nil
	}, isAliyunTransient)
}

// isAliyunTransient reports whether an error is a temporary state that a retry
// can clear (e.g. a just-created instance still initializing, or throttling).
func isAliyunTransient(err error) bool {
	return codeMatches(err, aliyunCode,
		"IncorrectInstanceStatus", "Initializing", "Creating",
		"OperationConflict", "LastOrderProcessing", "Throttling")
}

func isAliyunNotFound(err error) bool {
	return codeMatches(err, aliyunCode, "NotFound", "InvalidInstanceId")
}
