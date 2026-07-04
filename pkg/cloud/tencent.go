package cloud

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	sdkerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	cvm "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/cvm/v20170312"
	vpc "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/vpc/v20170312"
)

// tencentProvider talks to the CVM (2017-03-12) and VPC (2017-03-12) APIs via the
// official modular tencentcloud-sdk-go clients.
type tencentProvider struct {
	cred Credential
	// transport, when set, overrides the SDK client's HTTP transport. Tests inject
	// a RoundTripper here to feed canned responses; production leaves it nil.
	transport http.RoundTripper
}

func (p *tencentProvider) Name() string { return "tencent" }

// credential builds the SDK credential from the stored AK/SK.
func (p *tencentProvider) credential() *common.Credential {
	return common.NewCredential(p.cred.AccessKeyID, p.cred.AccessKeySecret)
}

// cvmClient constructs a CVM client bound to region, applying the test transport
// if one is set. An empty region is tolerated (region-less actions like
// DescribeRegions still sign correctly — the scope is service-based).
func (p *tencentProvider) cvmClient(region string) (*cvm.Client, error) {
	c, err := cvm.NewClient(p.credential(), region, profile.NewClientProfile())
	if err != nil {
		return nil, err
	}
	if p.transport != nil {
		c.WithHttpTransport(p.transport)
	}
	return c, nil
}

// vpcClient constructs a VPC client bound to region, applying the test transport.
func (p *tencentProvider) vpcClient(region string) (*vpc.Client, error) {
	c, err := vpc.NewClient(p.credential(), region, profile.NewClientProfile())
	if err != nil {
		return nil, err
	}
	if p.transport != nil {
		c.WithHttpTransport(p.transport)
	}
	return c, nil
}

// ListRegions calls DescribeRegions, a region-less CVM action needing only the
// credential. Only regions reported AVAILABLE are returned.
func (p *tencentProvider) ListRegions(ctx context.Context) ([]Region, error) {
	cli, err := p.cvmClient(firstNonEmpty(p.cred.Region, "ap-guangzhou"))
	if err != nil {
		return nil, err
	}
	resp, err := cli.DescribeRegionsWithContext(ctx, cvm.NewDescribeRegionsRequest())
	if err != nil {
		return nil, err
	}
	regions := make([]Region, 0, len(resp.Response.RegionSet))
	for _, r := range resp.Response.RegionSet {
		if state := ptrStr(r.RegionState); state != "" && state != "AVAILABLE" {
			continue
		}
		regions = append(regions, Region{ID: ptrStr(r.Region), LocalName: ptrStr(r.RegionName)})
	}
	return regions, nil
}

func (p *tencentProvider) CreateInstances(ctx context.Context, req CreateRequest) ([]Instance, error) {
	region := firstNonEmpty(req.Region, p.cred.Region)
	if region == "" {
		return nil, fmt.Errorf("tencent: region is required")
	}
	count := req.Count
	if count <= 0 {
		count = 1
	}
	r := cvm.NewRunInstancesRequest()
	r.InstanceChargeType = common.StringPtr("POSTPAID_BY_HOUR")
	r.ImageId = common.StringPtr(req.ImageID)
	r.InstanceType = common.StringPtr(req.InstanceType)
	r.InstanceCount = common.Int64Ptr(int64(count))
	r.InstanceName = common.StringPtr(firstNonEmpty(req.Name, "aiscan-agent"))
	if req.ZoneID != "" {
		r.Placement = &cvm.Placement{Zone: common.StringPtr(req.ZoneID)}
	}
	if req.VPCID != "" && req.VSwitchID != "" {
		r.VirtualPrivateCloud = &cvm.VirtualPrivateCloud{
			VpcId:    common.StringPtr(req.VPCID),
			SubnetId: common.StringPtr(req.VSwitchID),
		}
	}
	if req.SecurityGroupID != "" {
		r.SecurityGroupIds = common.StringPtrs([]string{req.SecurityGroupID})
	}
	if req.BandwidthOut > 0 {
		r.InternetAccessible = &cvm.InternetAccessible{
			InternetMaxBandwidthOut: common.Int64Ptr(int64(req.BandwidthOut)),
			PublicIpAssigned:        common.BoolPtr(true),
			InternetChargeType:      common.StringPtr("TRAFFIC_POSTPAID_BY_HOUR"),
		}
	}
	if req.UserData != "" {
		r.UserData = common.StringPtr(base64.StdEncoding.EncodeToString([]byte(req.UserData)))
	}
	tagSpec := &cvm.TagSpecification{ResourceType: common.StringPtr("instance")}
	for k, v := range ownershipTags(req.Tags) {
		tagSpec.Tags = append(tagSpec.Tags, &cvm.Tag{Key: common.StringPtr(k), Value: common.StringPtr(v)})
	}
	r.TagSpecification = []*cvm.TagSpecification{tagSpec}

	cli, err := p.cvmClient(region)
	if err != nil {
		return nil, err
	}
	resp, err := cli.RunInstancesWithContext(ctx, r)
	if err != nil {
		return nil, err
	}
	insts := make([]Instance, 0, len(resp.Response.InstanceIdSet))
	for _, id := range resp.Response.InstanceIdSet {
		insts = append(insts, Instance{ID: ptrStr(id), Status: "PENDING"})
	}
	return insts, nil
}

// ListImages returns NORMAL public images for the region, Ubuntu/LTS ranked first.
func (p *tencentProvider) ListImages(ctx context.Context, region string) ([]Image, error) {
	region = firstNonEmpty(region, p.cred.Region)
	if region == "" {
		return nil, fmt.Errorf("tencent: region is required")
	}
	r := cvm.NewDescribeImagesRequest()
	r.Limit = common.Uint64Ptr(100)
	r.Filters = []*cvm.Filter{{Name: common.StringPtr("image-type"), Values: common.StringPtrs([]string{"PUBLIC_IMAGE"})}}

	cli, err := p.cvmClient(region)
	if err != nil {
		return nil, err
	}
	resp, err := cli.DescribeImagesWithContext(ctx, r)
	if err != nil {
		return nil, err
	}
	imgs := make([]Image, 0, len(resp.Response.ImageSet))
	for _, im := range resp.Response.ImageSet {
		if state := ptrStr(im.ImageState); state != "" && !strings.EqualFold(state, "NORMAL") {
			continue
		}
		osName := firstNonEmpty(ptrStr(im.OsName), ptrStr(im.ImageName))
		imgs = append(imgs, Image{
			ID:       ptrStr(im.ImageId),
			Name:     osName,
			OSName:   osName,
			Platform: ptrStr(im.Platform),
			Arch:     ptrStr(im.Architecture),
		})
	}
	sortImages(imgs)
	return capList(imgs, 100), nil
}

// ListInstanceTypes returns entry-level instance specs that are on sale (SELL) in
// the region (and zone if given), cheapest first.
func (p *tencentProvider) ListInstanceTypes(ctx context.Context, region, zone string) ([]InstanceType, error) {
	region = firstNonEmpty(region, p.cred.Region)
	if region == "" {
		return nil, fmt.Errorf("tencent: region is required")
	}
	r := cvm.NewDescribeZoneInstanceConfigInfosRequest()
	r.Filters = []*cvm.Filter{
		{Name: common.StringPtr("instance-charge-type"), Values: common.StringPtrs([]string{"POSTPAID_BY_HOUR"})},
	}
	if zone != "" {
		r.Filters = append(r.Filters, &cvm.Filter{Name: common.StringPtr("zone"), Values: common.StringPtrs([]string{zone})})
	}

	cli, err := p.cvmClient(region)
	if err != nil {
		return nil, err
	}
	resp, err := cli.DescribeZoneInstanceConfigInfosWithContext(ctx, r)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	types := make([]InstanceType, 0, len(resp.Response.InstanceTypeQuotaSet))
	for _, t := range resp.Response.InstanceTypeQuotaSet {
		if status := ptrStr(t.Status); status != "" && !strings.EqualFold(status, "SELL") {
			continue
		}
		id := ptrStr(t.InstanceType)
		cpu := int(ptrI64(t.Cpu))
		if cpu <= 0 || cpu > maxInstanceTypeCPU || seen[id] {
			continue
		}
		seen[id] = true
		types = append(types, InstanceType{ID: id, CPU: cpu, MemoryGiB: float64(ptrI64(t.Memory))})
	}
	sortInstanceTypes(types)
	return capList(types, 80), nil
}

// DefaultNetwork resolves a default VPC + subnet + security group so a minimal
// deploy can omit them. Best-effort: any unresolved piece is left blank.
func (p *tencentProvider) DefaultNetwork(ctx context.Context, region, zone string) (NetworkDefaults, error) {
	region = firstNonEmpty(region, p.cred.Region)
	nd := NetworkDefaults{ZoneID: zone}
	if region == "" {
		return nd, fmt.Errorf("tencent: region is required")
	}
	vpcID := p.findVPC(ctx, region)
	if vpcID != "" {
		nd.VPCID = vpcID
		sub, subZone := p.findSubnet(ctx, region, vpcID, zone)
		nd.VSwitchID = sub
		if nd.ZoneID == "" {
			nd.ZoneID = subZone
		}
	}
	nd.SecurityGroupID = p.findSecurityGroup(ctx, region)
	return nd, nil
}

// EnsureNetwork discovers the region's default network and creates whatever
// piece is missing (VPC+subnet via CreateDefaultVpc, or a lone subnet, plus a
// security group) so a deploy always has a valid VpcId+SubnetId+SG to launch
// into. See the Provider interface.
func (p *tencentProvider) EnsureNetwork(ctx context.Context, region, zone, label string) (NetworkDefaults, NetworkDefaults, error) {
	region = firstNonEmpty(region, p.cred.Region)
	var created NetworkDefaults
	if region == "" {
		return NetworkDefaults{}, created, fmt.Errorf("tencent: region is required")
	}
	nd, _ := p.DefaultNetwork(ctx, region, zone) // best-effort discovery

	switch {
	case nd.VPCID == "":
		// No VPC at all: one call provisions a default VPC and its first subnet.
		z := firstNonEmpty(nd.ZoneID, zone, p.firstZone(ctx, region))
		vpcID, subnet, err := p.createDefaultVPC(ctx, region, z, label)
		if err != nil {
			return nd, created, fmt.Errorf("create vpc: %w", err)
		}
		nd.VPCID, created.VPCID = vpcID, vpcID
		nd.VSwitchID, created.VSwitchID = subnet, subnet
		if nd.ZoneID == "" {
			nd.ZoneID = z
		}
	case nd.VSwitchID == "":
		z := firstNonEmpty(nd.ZoneID, zone, p.firstZone(ctx, region))
		cidr := carveSubnetCIDR(firstNonEmpty(p.vpcCIDR(ctx, region, nd.VPCID), autoVPCCIDR))
		subnet, err := p.createSubnet(ctx, region, nd.VPCID, z, cidr, label)
		if err != nil {
			return nd, created, fmt.Errorf("create subnet: %w", err)
		}
		nd.VSwitchID, created.VSwitchID = subnet, subnet
		if nd.ZoneID == "" {
			nd.ZoneID = z
		}
	}
	if nd.SecurityGroupID == "" {
		sg, err := p.CreateSecurityGroup(ctx, region, nd.VPCID, label)
		if err != nil {
			return nd, created, fmt.Errorf("create security group: %w", err)
		}
		nd.SecurityGroupID, created.SecurityGroupID = sg, sg
	}
	return nd, created, nil
}

// TeardownNetwork deletes the auto-created resources in dependency order
// (security group → subnet → VPC). See the Provider interface.
func (p *tencentProvider) TeardownNetwork(ctx context.Context, region string, created NetworkDefaults) error {
	region = firstNonEmpty(region, p.cred.Region)
	return teardownNetwork(ctx, region, "subnet", "tencent", created, p.deleteNetResource)
}

// createDefaultVPC provisions a default VPC and its first subnet in one call,
// returning (vpcID, subnetID).
func (p *tencentProvider) createDefaultVPC(ctx context.Context, region, zone, label string) (string, string, error) {
	r := vpc.NewCreateDefaultVpcRequest()
	r.Force = common.BoolPtr(true)
	if zone != "" {
		r.Zone = common.StringPtr(zone)
	}
	cli, err := p.vpcClient(region)
	if err != nil {
		return "", "", err
	}
	resp, err := cli.CreateDefaultVpcWithContext(ctx, r)
	if err != nil {
		return "", "", err
	}
	if resp.Response.Vpc == nil {
		return "", "", fmt.Errorf("create default vpc returned no vpc")
	}
	vpcID, subnetID := ptrStr(resp.Response.Vpc.VpcId), ptrStr(resp.Response.Vpc.SubnetId)
	if emptyCloudID(vpcID) || emptyCloudID(subnetID) {
		return "", "", fmt.Errorf("create default vpc returned unusable ids: vpc=%q subnet=%q", vpcID, subnetID)
	}
	return vpcID, subnetID, nil
}

// createSubnet creates an auto subnet (cidr within its VPC) in an existing VPC.
func (p *tencentProvider) createSubnet(ctx context.Context, region, vpcID, zone, cidr, label string) (string, error) {
	if zone == "" {
		return "", fmt.Errorf("no available zone for subnet in %s", region)
	}
	r := vpc.NewCreateSubnetRequest()
	r.VpcId = common.StringPtr(vpcID)
	r.SubnetName = common.StringPtr(netName(label))
	r.CidrBlock = common.StringPtr(cidr)
	r.Zone = common.StringPtr(zone)

	cli, err := p.vpcClient(region)
	if err != nil {
		return "", err
	}
	resp, err := cli.CreateSubnetWithContext(ctx, r)
	if err != nil {
		return "", err
	}
	if resp.Response.Subnet == nil || emptyCloudID(ptrStr(resp.Response.Subnet.SubnetId)) {
		return "", fmt.Errorf("empty subnet id")
	}
	return ptrStr(resp.Response.Subnet.SubnetId), nil
}

// CreateSecurityGroup creates a caller-owned security group with allow-all egress
// and inbound SSH in a single call. Tencent security groups are regional rather
// than VPC-scoped, so vpcID is ignored.
func (p *tencentProvider) CreateSecurityGroup(ctx context.Context, region, vpcID, label string) (string, error) {
	region = firstNonEmpty(region, p.cred.Region)
	r := vpc.NewCreateSecurityGroupWithPoliciesRequest()
	r.GroupName = common.StringPtr(netName(label))
	r.GroupDescription = common.StringPtr("aiscan auto")
	r.SecurityGroupPolicySet = &vpc.SecurityGroupPolicySet{
		Egress: []*vpc.SecurityGroupPolicy{{
			Protocol: common.StringPtr("ALL"), CidrBlock: common.StringPtr("0.0.0.0/0"),
			Action: common.StringPtr("ACCEPT"), PolicyDescription: common.StringPtr("aiscan egress"),
		}},
		Ingress: []*vpc.SecurityGroupPolicy{{
			Protocol: common.StringPtr("TCP"), Port: common.StringPtr("22"), CidrBlock: common.StringPtr("0.0.0.0/0"),
			Action: common.StringPtr("ACCEPT"), PolicyDescription: common.StringPtr("aiscan ssh"),
		}},
	}
	cli, err := p.vpcClient(region)
	if err != nil {
		return "", err
	}
	resp, err := cli.CreateSecurityGroupWithPoliciesWithContext(ctx, r)
	if err != nil {
		return "", err
	}
	if resp.Response.SecurityGroup == nil || emptyCloudID(ptrStr(resp.Response.SecurityGroup.SecurityGroupId)) {
		return "", fmt.Errorf("empty security group id")
	}
	return ptrStr(resp.Response.SecurityGroup.SecurityGroupId), nil
}

// OpenPort authorizes inbound TCP on port (from anywhere) in the security group.
// See the Provider interface. A duplicate rule (re-run) is tolerated.
func (p *tencentProvider) OpenPort(ctx context.Context, region, sgID string, port int) error {
	region = firstNonEmpty(region, p.cred.Region)
	if sgID == "" {
		return fmt.Errorf("tencent: security group id required to open port")
	}
	r := vpc.NewCreateSecurityGroupPoliciesRequest()
	r.SecurityGroupId = common.StringPtr(sgID)
	r.SecurityGroupPolicySet = &vpc.SecurityGroupPolicySet{
		Ingress: []*vpc.SecurityGroupPolicy{{
			Protocol: common.StringPtr("TCP"), Port: common.StringPtr(fmt.Sprintf("%d", port)),
			CidrBlock: common.StringPtr("0.0.0.0/0"), Action: common.StringPtr("ACCEPT"),
			PolicyDescription: common.StringPtr("aiscan relay"),
		}},
	}
	cli, err := p.vpcClient(region)
	if err != nil {
		return err
	}
	if _, err := cli.CreateSecurityGroupPoliciesWithContext(ctx, r); err != nil {
		if isTencentDuplicate(err) {
			return nil
		}
		return err
	}
	return nil
}

// vpcCIDR returns the existing VPC's primary CIDR block, "" if it can't be read.
func (p *tencentProvider) vpcCIDR(ctx context.Context, region, vpcID string) string {
	r := vpc.NewDescribeVpcsRequest()
	r.VpcIds = common.StringPtrs([]string{vpcID})
	cli, err := p.vpcClient(region)
	if err != nil {
		return ""
	}
	resp, err := cli.DescribeVpcsWithContext(ctx, r)
	if err == nil && len(resp.Response.VpcSet) > 0 {
		return ptrStr(resp.Response.VpcSet[0].CidrBlock)
	}
	return ""
}

// firstZone returns the region's first AVAILABLE zone, "" if none could be listed.
func (p *tencentProvider) firstZone(ctx context.Context, region string) string {
	cli, err := p.cvmClient(region)
	if err != nil {
		return ""
	}
	resp, err := cli.DescribeZonesWithContext(ctx, cvm.NewDescribeZonesRequest())
	if err != nil {
		return ""
	}
	for _, z := range resp.Response.ZoneSet {
		if state := ptrStr(z.ZoneState); state == "" || state == "AVAILABLE" {
			return ptrStr(z.Zone)
		}
	}
	return ""
}

// deleteNetResource deletes one VPC-service resource, retrying while it is still
// in use and treating an already-gone resource as success.
func (p *tencentProvider) deleteNetResource(ctx context.Context, region, kind, id string) error {
	return deleteWithRetry(ctx, func() error {
		cli, err := p.vpcClient(region)
		if err != nil {
			return err
		}
		switch kind {
		case "securitygroup":
			r := vpc.NewDeleteSecurityGroupRequest()
			r.SecurityGroupId = common.StringPtr(id)
			_, err = cli.DeleteSecurityGroupWithContext(ctx, r)
		case "subnet":
			r := vpc.NewDeleteSubnetRequest()
			r.SubnetId = common.StringPtr(id)
			_, err = cli.DeleteSubnetWithContext(ctx, r)
		case "vpc":
			r := vpc.NewDeleteVpcRequest()
			r.VpcId = common.StringPtr(id)
			_, err = cli.DeleteVpcWithContext(ctx, r)
		default:
			return fmt.Errorf("unknown resource kind %q", kind)
		}
		if err != nil {
			if isTencentNotFound(err) {
				return nil
			}
			return err
		}
		return nil
	}, isTencentInUse)
}

// tencentCode extracts the API error code from an SDK error, "" otherwise.
func tencentCode(err error) string {
	if e, ok := err.(*sdkerrors.TencentCloudSDKError); ok {
		return e.GetCode()
	}
	return ""
}

// isTencentInUse reports that a resource still has a dependent referencing it.
func isTencentInUse(err error) bool {
	return codeMatches(err, tencentCode, "InUse", "ResourceInUse", "DependencyViolation")
}

func isTencentNotFound(err error) bool  { return codeMatches(err, tencentCode, "NotFound") }
func isTencentDuplicate(err error) bool { return codeMatches(err, tencentCode, "Duplicate") }

// findVPC returns the region's default VPC id (or the first VPC), "" if none.
func (p *tencentProvider) findVPC(ctx context.Context, region string) string {
	r := vpc.NewDescribeVpcsRequest()
	r.Limit = common.StringPtr("50")
	cli, err := p.vpcClient(region)
	if err != nil {
		return ""
	}
	resp, err := cli.DescribeVpcsWithContext(ctx, r)
	if err != nil || len(resp.Response.VpcSet) == 0 {
		return ""
	}
	for _, v := range resp.Response.VpcSet {
		if v.IsDefault != nil && *v.IsDefault {
			return ptrStr(v.VpcId)
		}
	}
	return ptrStr(resp.Response.VpcSet[0].VpcId)
}

// findSubnet returns a subnet in the VPC (preferring the requested zone) and its
// zone, ("","") if none.
func (p *tencentProvider) findSubnet(ctx context.Context, region, vpcID, zone string) (string, string) {
	r := vpc.NewDescribeSubnetsRequest()
	r.Limit = common.StringPtr("50")
	r.Filters = []*vpc.Filter{{Name: common.StringPtr("vpc-id"), Values: common.StringPtrs([]string{vpcID})}}
	cli, err := p.vpcClient(region)
	if err != nil {
		return "", ""
	}
	resp, err := cli.DescribeSubnetsWithContext(ctx, r)
	if err != nil || len(resp.Response.SubnetSet) == 0 {
		return "", ""
	}
	if zone != "" {
		for _, s := range resp.Response.SubnetSet {
			if ptrStr(s.Zone) == zone {
				return ptrStr(s.SubnetId), ptrStr(s.Zone)
			}
		}
	}
	return ptrStr(resp.Response.SubnetSet[0].SubnetId), ptrStr(resp.Response.SubnetSet[0].Zone)
}

// findSecurityGroup returns the first security group, "" if none.
func (p *tencentProvider) findSecurityGroup(ctx context.Context, region string) string {
	r := vpc.NewDescribeSecurityGroupsRequest()
	r.Limit = common.StringPtr("50")
	cli, err := p.vpcClient(region)
	if err != nil {
		return ""
	}
	resp, err := cli.DescribeSecurityGroupsWithContext(ctx, r)
	if err != nil || len(resp.Response.SecurityGroupSet) == 0 {
		return ""
	}
	return ptrStr(resp.Response.SecurityGroupSet[0].SecurityGroupId)
}

func (p *tencentProvider) ListInstances(ctx context.Context, ids []string) ([]Instance, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	region := p.cred.Region
	r := cvm.NewDescribeInstancesRequest()
	r.InstanceIds = common.StringPtrs(ids)
	r.Limit = common.Int64Ptr(100)
	cli, err := p.cvmClient(region)
	if err != nil {
		return nil, err
	}
	resp, err := cli.DescribeInstancesWithContext(ctx, r)
	if err != nil {
		return nil, err
	}
	insts := make([]Instance, 0, len(resp.Response.InstanceSet))
	for _, it := range resp.Response.InstanceSet {
		insts = append(insts, tencentInstanceView(it))
	}
	return insts, nil
}

func tencentInstanceView(it *cvm.Instance) Instance {
	inst := Instance{ID: ptrStr(it.InstanceId), Name: ptrStr(it.InstanceName), Status: ptrStr(it.InstanceState)}
	if t, err := time.Parse(time.RFC3339, ptrStr(it.CreatedTime)); err == nil {
		inst.CreatedAt = t.UTC()
	}
	if len(it.PublicIpAddresses) > 0 {
		inst.PublicIP = ptrStr(it.PublicIpAddresses[0])
	}
	if len(it.PrivateIpAddresses) > 0 {
		inst.PrivateIP = ptrStr(it.PrivateIpAddresses[0])
	}
	return inst
}

// ListOwnedInstances returns every instance in region tagged managed-by-aiscan,
// paging through all results. Only tagged instances are returned.
func (p *tencentProvider) ListOwnedInstances(ctx context.Context, region string) ([]Instance, error) {
	region = firstNonEmpty(region, p.cred.Region)
	if region == "" {
		return nil, fmt.Errorf("tencent: region is required")
	}
	cli, err := p.cvmClient(region)
	if err != nil {
		return nil, err
	}
	var out []Instance
	for offset := int64(0); ; offset += 100 {
		r := cvm.NewDescribeInstancesRequest()
		r.Offset = common.Int64Ptr(offset)
		r.Limit = common.Int64Ptr(100)
		r.Filters = []*cvm.Filter{{
			Name:   common.StringPtr("tag:" + TagManagedBy),
			Values: common.StringPtrs([]string{TagManagedByValue}),
		}}
		resp, err := cli.DescribeInstancesWithContext(ctx, r)
		if err != nil {
			return nil, err
		}
		batch := resp.Response.InstanceSet
		for _, it := range batch {
			out = append(out, tencentInstanceView(it))
		}
		total := int64(0)
		if resp.Response.TotalCount != nil {
			total = *resp.Response.TotalCount
		}
		if int64(len(batch)) < 100 || int64(len(out)) >= total {
			break
		}
	}
	return out, nil
}

func (p *tencentProvider) DeleteInstances(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	region := p.cred.Region
	return deleteWithRetry(ctx, func() error {
		r := cvm.NewTerminateInstancesRequest()
		r.InstanceIds = common.StringPtrs(ids)
		cli, err := p.cvmClient(region)
		if err != nil {
			return err
		}
		if _, err := cli.TerminateInstancesWithContext(ctx, r); err != nil {
			if isTencentNotFound(err) {
				return nil
			}
			return err
		}
		return nil
	}, isTencentTransient)
}

// isTencentTransient reports whether a CVM error is a temporary operating state.
func isTencentTransient(err error) bool {
	return codeMatches(err, tencentCode,
		"Operating", "InProcess", "InProgress", "InvalidInstanceState", "RequestLimitExceeded")
}

// ptrStr / ptrI64 safely dereference SDK pointer fields.
func ptrStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func ptrI64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
