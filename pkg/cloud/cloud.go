// Package cloud provides a minimal abstraction over cloud provider compute APIs
// (Aliyun ECS/VPC, Tencent CVM/VPC) for auto-provisioning aiscan agent nodes.
// Each provider is implemented on top of the official modular vendor SDK
// (alibabacloud-go, tencentcloud-sdk-go); the Provider interface normalizes the
// handful of operations the deploy flow needs.
package cloud

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"
)

// Ownership tags stamped on every instance aiscan provisions. They let the hub
// reconcile against the cloud — listing what it actually owns — instead of
// trusting only its local records, so an instance whose record was lost (hub
// restart, partial launch, a recycle whose delete never landed) can still be
// found and reaped. TagManagedBy is the ownership marker; TagDeployID records
// the owning deploy. Keys avoid the reserved "aliyun"/"acs:" prefixes and use a
// portable hyphenated form accepted by both Aliyun ECS and Tencent CVM.
const (
	TagManagedBy      = "aiscan-managed"
	TagManagedByValue = "true"
	TagDeployID       = "aiscan-deploy-id"
)

// ownershipTags merges the always-present managed marker with the caller's tags,
// so every provider stamps instances identically and a later reconcile can find
// them even if the local record is lost.
func ownershipTags(extra map[string]string) map[string]string {
	tags := map[string]string{TagManagedBy: TagManagedByValue}
	for k, v := range extra {
		tags[k] = v
	}
	return tags
}

// Credential identifies a cloud account. AccessKeySecret is sensitive and must
// never be distributed to agents — it stays on the hub.
type Credential struct {
	Provider        string // "aliyun" | "tencent"
	AccessKeyID     string
	AccessKeySecret string
	Region          string
}

// CreateRequest describes a batch of instances to launch. UserData is the raw
// (un-encoded) cloud-init script; each provider base64-encodes it as required.
type CreateRequest struct {
	Region          string
	ZoneID          string
	ImageID         string
	InstanceType    string
	SecurityGroupID string
	VSwitchID       string // aliyun VSwitch / tencent SubnetID
	VPCID           string // tencent requires VpcId alongside SubnetId
	Count           int
	UserData        string
	Name            string
	BandwidthOut    int               // public egress bandwidth in Mbps; 0 disables a public IP request
	Tags            map[string]string // provider tags applied to every created instance (ownership reconciliation)
}

// Region is a normalized provider region the UI can offer as a deploy target.
type Region struct {
	ID        string `json:"id"`                   // e.g. "cn-hangzhou", "ap-guangzhou"
	LocalName string `json:"local_name,omitempty"` // human-readable, possibly localized
}

// Instance is the normalized view of a provider instance.
type Instance struct {
	ID        string    `json:"id"`
	Name      string    `json:"name,omitempty"`
	Status    string    `json:"status,omitempty"`
	PublicIP  string    `json:"public_ip,omitempty"`
	PrivateIP string    `json:"private_ip,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"` // provider creation time; zero if unknown
}

// Image is a normalized OS image the UI can offer as a deploy target. Providers
// return the list best-candidate-first (Ubuntu LTS x86_64 ranked highest) so the
// UI can pre-select images[0] as a sane default.
type Image struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`     // human-readable OS name
	OSName   string `json:"os_name,omitempty"`  // localized OS name when available
	Platform string `json:"platform,omitempty"` // e.g. "Ubuntu", "CentOS"
	Arch     string `json:"arch,omitempty"`     // e.g. "x86_64", "arm64"
}

// InstanceType is a normalized, purchasable instance spec. Providers return the
// list cheapest-first (by vCPU then memory) and region/zone-availability filtered
// where the API allows it.
type InstanceType struct {
	ID        string  `json:"id"`
	CPU       int     `json:"cpu"`        // vCPU count
	MemoryGiB float64 `json:"memory_gib"` // RAM in GiB
}

// NetworkDefaults are best-effort default network resources discovered for a
// region, used to fill blank deploy fields so a minimal request still launches.
// Any piece that can't be resolved is left blank.
type NetworkDefaults struct {
	ZoneID          string `json:"zone_id,omitempty"`
	VPCID           string `json:"vpc_id,omitempty"`
	VSwitchID       string `json:"vswitch_id,omitempty"`
	SecurityGroupID string `json:"security_group_id,omitempty"`
}

// Empty reports whether no VPC / vSwitch / security group is set. ZoneID alone
// does not count — as an EnsureNetwork "created" result, a blank Empty()==true
// value means nothing was provisioned and so nothing needs tearing down.
func (n NetworkDefaults) Empty() bool {
	return n.VPCID == "" && n.VSwitchID == "" && n.SecurityGroupID == ""
}

func emptyCloudID(id string) bool {
	id = strings.TrimSpace(id)
	return id == "" || id == "0"
}

// Auto-provisioned network shape. A fresh region gets one /16 VPC and a single
// /24 vSwitch (subnet) — ample for the handful of agent nodes a deploy launches.
const (
	autoVPCCIDR     = "192.168.0.0/16"
	autoVSwitchCIDR = "192.168.0.0/24"
)

// netName derives a recognizable name for an auto-created network resource so
// it's easy to spot (and the deploy id ties it back to its owner).
func netName(label string) string {
	if strings.TrimSpace(label) == "" {
		return "aiscan-auto"
	}
	return "aiscan-" + label
}

// carveSubnetCIDR returns a /24 carved at the base of vpcCIDR for an auto
// vSwitch/subnet created inside a pre-existing VPC (whose address space we don't
// control — it may be 10/8, 172.16/12, etc., so a fixed block would be rejected).
// A VPC already /24-or-smaller is used whole; an unparseable block falls back to
// the default auto subnet.
func carveSubnetCIDR(vpcCIDR string) string {
	_, ipnet, err := net.ParseCIDR(strings.TrimSpace(vpcCIDR))
	if err != nil {
		return autoVSwitchCIDR
	}
	if ones, _ := ipnet.Mask.Size(); ones >= 24 {
		return ipnet.String()
	}
	return ipnet.IP.String() + "/24"
}

// networkReady* bound how long a just-created resource is polled for an
// Available state before launch proceeds anyway. Tunable in tests.
var (
	networkReadyInterval = 2 * time.Second
	networkReadyTimeout  = 60 * time.Second
)

// Provider is the common compute interface implemented per cloud.
type Provider interface {
	Name() string
	// ListRegions returns the regions available to this account. It is a global
	// (region-less) call that needs only the credential's AK/SK, so the UI can
	// populate a region picker before anything is deployed.
	ListRegions(ctx context.Context) ([]Region, error)
	// ListImages returns selectable OS images for region (Ubuntu/LTS biased,
	// best candidate first). region falls back to the credential default.
	ListImages(ctx context.Context, region string) ([]Image, error)
	// ListInstanceTypes returns purchasable instance specs for region (and zone
	// if given), cheapest first.
	ListInstanceTypes(ctx context.Context, region, zone string) ([]InstanceType, error)
	// EnsureNetwork resolves a usable VPC + vSwitch + security group for region,
	// creating (and opening inbound on) any piece DefaultNetwork couldn't find, so
	// a deploy can land even in a region the account has never touched. label
	// names the created resources (typically the deploy id). The first return is
	// the resolved network to launch into; the second lists ONLY the resources
	// EnsureNetwork created, so the caller can tear exactly those down on recycle
	// without touching pre-existing user resources. On failure it still reports
	// what it created up to that point, so a later recycle reclaims it.
	EnsureNetwork(ctx context.Context, region, zone, label string) (resolved, created NetworkDefaults, err error)
	// TeardownNetwork deletes resources previously reported as EnsureNetwork's
	// created result (security group, then vSwitch, then VPC). It is best-effort
	// and idempotent: already-gone pieces are skipped and still-in-use pieces are
	// retried briefly (a just-terminated instance can pin its vSwitch for seconds).
	TeardownNetwork(ctx context.Context, region string, created NetworkDefaults) error
	// CreateSecurityGroup creates a dedicated security group for a caller-owned
	// resource and opens SSH (22). The returned group should be recorded in
	// created network state and later passed to TeardownNetwork.
	CreateSecurityGroup(ctx context.Context, region, vpcID, label string) (string, error)
	// CreateInstances launches req.Count instances and returns their IDs.
	CreateInstances(ctx context.Context, req CreateRequest) ([]Instance, error)
	// ListInstances returns the current state of the given instance IDs.
	// IDs that no longer exist are simply omitted (treated as already gone).
	ListInstances(ctx context.Context, ids []string) ([]Instance, error)
	// ListOwnedInstances returns every non-terminated instance in region that
	// carries the TagManagedBy ownership tag, regardless of whether the hub still
	// has a record of it. It is the reconciliation counterpart to record-driven
	// recycle: the hub diffs this against its records to find and reap orphans.
	// It must only ever surface instances the hub itself tagged, so unrelated user
	// instances are never returned.
	ListOwnedInstances(ctx context.Context, region string) ([]Instance, error)
	// DeleteInstances terminates the given instances. It must be idempotent:
	// already-deleted instances are not an error.
	DeleteInstances(ctx context.Context, ids []string) error
	// OpenPort authorizes inbound TCP on the given port (from 0.0.0.0/0) in the
	// security group. EnsureNetwork opens only SSH (22) because agent nodes just
	// dial out; the relay additionally needs its forwarded hub port reachable from
	// the nodes. Idempotent: re-authorizing an existing rule is not an error.
	OpenPort(ctx context.Context, region, securityGroupID string, port int) error
}

// NewProvider builds a Provider from a credential. The region on the credential
// is the default; per-request CreateRequest.Region overrides it.
func NewProvider(cred Credential) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(cred.Provider)) {
	case "aliyun", "alibaba", "ali":
		if cred.AccessKeyID == "" || cred.AccessKeySecret == "" {
			return nil, fmt.Errorf("aliyun: access key id/secret required")
		}
		return &aliyunProvider{cred: cred}, nil
	case "tencent", "qcloud", "tencentcloud":
		if cred.AccessKeyID == "" || cred.AccessKeySecret == "" {
			return nil, fmt.Errorf("tencent: secret id/key required")
		}
		return &tencentProvider{cred: cred}, nil
	default:
		return nil, fmt.Errorf("unsupported cloud provider: %q (want aliyun|tencent)", cred.Provider)
	}
}

// SupportedProviders lists the provider identifiers the UI can offer.
func SupportedProviders() []string { return []string{"aliyun", "tencent"} }

// deleteWithRetry runs a terminate operation, retrying while it returns a
// transient (retryable) error such as "instance still initializing". A freshly
// launched instance cannot be force-deleted for ~30-60s after creation, so an
// immediate recycle must wait that window out rather than fail.
// Tunable in tests.
var (
	deleteRetryInterval = 8 * time.Second
	deleteRetryTimeout  = 150 * time.Second
)

func deleteWithRetry(ctx context.Context, do func() error, retryable func(error) bool) error {
	deadline := time.Now().Add(deleteRetryTimeout)
	for {
		err := do()
		if err == nil || !retryable(err) {
			return err
		}
		if time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(deleteRetryInterval):
		}
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// codeMatches reports whether the API error code extracted by extract contains
// any of subs. The extract func stays provider-specific (SDK error types
// differ); the substring sets are the per-provider classification. Returns
// false for a nil/unrecognized error (empty code).
func codeMatches(err error, extract func(error) string, subs ...string) bool {
	c := extract(err)
	if c == "" {
		return false
	}
	for _, s := range subs {
		if strings.Contains(c, s) {
			return true
		}
	}
	return false
}

// teardownNetwork deletes the SG, subnet/vSwitch, and VPC recorded in created,
// collecting per-resource failures rather than aborting on the first. subnetKind
// names the mid-tier resource (aliyun "vswitch" vs tencent "subnet") and
// provider labels the aggregate error. del performs one provider-specific
// delete. Shared by both providers.
func teardownNetwork(ctx context.Context, region, subnetKind, provider string, created NetworkDefaults, del func(ctx context.Context, region, kind, id string) error) error {
	var errs []string
	do := func(kind, id string) {
		if emptyCloudID(id) {
			return
		}
		if err := del(ctx, region, kind, id); err != nil {
			errs = append(errs, err.Error())
		}
	}
	do("securitygroup", created.SecurityGroupID)
	do(subnetKind, created.VSwitchID)
	do("vpc", created.VPCID)
	if len(errs) > 0 {
		return fmt.Errorf("%s teardown: %s", provider, strings.Join(errs, "; "))
	}
	return nil
}

// maxInstanceTypeCPU bounds the instance types offered in the picker to
// entry-level sizes; the deploy form's "custom" escape covers anything bigger.
const maxInstanceTypeCPU = 8

// imageScore ranks an image for the deploy picker: prefer Ubuntu, then newer
// LTS, then x86_64. Higher is better. Shared by both providers.
func imageScore(im Image) int {
	s := 0
	name := strings.ToLower(im.OSName + " " + im.Name + " " + im.Platform)
	switch {
	case strings.Contains(name, "ubuntu"):
		s += 100
	case strings.Contains(name, "debian"):
		s += 80
	case strings.Contains(name, "centos"), strings.Contains(name, "anolis"),
		strings.Contains(name, "almalinux"), strings.Contains(name, "rocky"):
		s += 60
	}
	switch {
	case strings.Contains(name, "24.04"), strings.Contains(name, "24_04"):
		s += 8
	case strings.Contains(name, "22.04"), strings.Contains(name, "22_04"):
		s += 6
	case strings.Contains(name, "20.04"), strings.Contains(name, "20_04"):
		s += 4
	}
	if a := strings.ToLower(im.Arch); a == "x86_64" || a == "amd64" {
		s += 2
	}
	// Penalize pre-loaded heavyweight images (GPU/CUDA/driver bundles). The agent
	// embeds katana and crawls with the standard engine, so it needs no browser or
	// GPU — a bare OS image is always preferable and cheaper. Without this, a
	// "Ubuntu 24.04 + NVIDIA GPU/CUDA" image ties a plain "Ubuntu 24.04" and can
	// win the default slot purely on API ordering.
	if strings.Contains(name, "gpu") || strings.Contains(name, "cuda") ||
		strings.Contains(name, "nvidia") {
		s -= 50
	}
	return s
}

// sortImages orders images best-default-first so the UI can pre-select [0].
func sortImages(imgs []Image) {
	sort.SliceStable(imgs, func(i, j int) bool { return imageScore(imgs[i]) > imageScore(imgs[j]) })
}

// sortInstanceTypes orders specs cheapest-first (by vCPU, then memory).
func sortInstanceTypes(ts []InstanceType) {
	sort.SliceStable(ts, func(i, j int) bool {
		if ts[i].CPU != ts[j].CPU {
			return ts[i].CPU < ts[j].CPU
		}
		return ts[i].MemoryGiB < ts[j].MemoryGiB
	})
}

// capList truncates a slice to at most n elements.
func capList[T any](s []T, n int) []T {
	if len(s) > n {
		return s[:n]
	}
	return s
}
