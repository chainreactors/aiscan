package manager

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chainreactors/aiscan/pkg/cloud"
	"github.com/chainreactors/aiscan/pkg/deploy"
)

// reconcileGrace skips very-young owned instances so a reconcile never races an
// in-flight launch whose node IDs haven't been persisted to a record yet.
const reconcileGrace = 12 * time.Minute

// ReconcileOrphan is one owned-but-untracked instance the reconcile found.
type ReconcileOrphan struct {
	InstanceID string    `json:"instance_id"`
	Name       string    `json:"name,omitempty"`
	Region     string    `json:"region"`
	CloudID    string    `json:"cloud_id"`
	PublicIP   string    `json:"public_ip,omitempty"`
	CreatedAt  time.Time `json:"created_at,omitempty"`
	Released   bool      `json:"released"`
	Skipped    string    `json:"skipped,omitempty"` // reason it was left running (grace/relay/dry-run/error)
}

// ReconcileReport summarizes one reconcile pass.
type ReconcileReport struct {
	DryRun   bool              `json:"dry_run"`
	Scanned  int               `json:"scanned"`  // owned instances seen across all scanned regions
	Released int               `json:"released"` // orphans actually terminated
	Orphans  []ReconcileOrphan `json:"orphans"`
	Errors   []string          `json:"errors,omitempty"` // per-region scan/release errors
}

// reapableNodeName reports whether an owned instance is one reconcile may
// terminate when untracked: an agent node launched with the deploy naming scheme
// ("dep-<id>-<n>") or the throwaway relay VM ("aiscan-relay"). The active relay
// is protected upstream (its InstanceID is in the known set), so only a relay no
// live tunnel references — a crash-leaked or lost-record relay — is reaped here.
// Anything else the hub happens to own is never auto-terminated.
func reapableNodeName(name string) bool {
	return strings.HasPrefix(name, "dep-") || name == relayInstanceName
}

// Reconcile lists every instance this hub owns (by the managed-by-aiscan tag) and
// terminates the agent nodes and relays that no live deploy record or active
// tunnel references — the orphans that record-driven recycle can't see (records
// lost across a hub restart, partial launches, or a "recycled"/destroyed
// resource whose delete never landed). It only ever touches instances the hub
// itself tagged, skips instances younger than reconcileGrace, and protects the
// tracked relay (its InstanceID is in the known set), so unrelated user instances
// and live infrastructure are never at risk. With dryRun set it reports orphans
// without terminating anything.
func (m *DeployManager) Reconcile(ctx context.Context, dryRun bool) (ReconcileReport, error) {
	m.mu.Lock()
	state, err := m.store.Load(ctx)
	if err != nil {
		m.mu.Unlock()
		return ReconcileReport{}, err
	}

	// Instances a live record (or the active relay) legitimately owns — never reap
	// these. Recycled records have already dropped their nodes, so an instance
	// still running under a recycled record is (correctly) treated as an orphan.
	known := map[string]bool{}
	for _, d := range state.Deploys {
		if strings.EqualFold(d.Status, deploy.StatusRecycled) {
			continue
		}
		for _, n := range d.Nodes {
			if n.InstanceID != "" {
				known[n.InstanceID] = true
			}
		}
	}
	if state.SSHTunnel != nil && state.SSHTunnel.InstanceID != "" {
		known[state.SSHTunnel.InstanceID] = true
	}

	// Build the set of (credential, region) pairs to scan. An orphan whose record
	// was lost tells us nothing, so we scan every region the account is known to
	// touch: each credential's default region plus every region that appears in a
	// record or the relay.
	type scanTarget struct {
		cred    cloud.Credential
		cloudID string
	}
	targets := map[string]scanTarget{}
	addRegion := func(c *deploy.CloudCredential, region string) {
		region = strings.TrimSpace(region)
		if c == nil || region == "" {
			return
		}
		key := c.ID + "|" + region
		if _, ok := targets[key]; ok {
			return
		}
		targets[key] = scanTarget{
			cred:    providerCred(*c, region),
			cloudID: c.ID,
		}
	}
	for i := range state.Clouds {
		c := &state.Clouds[i]
		addRegion(c, c.DefaultRegion)
	}
	for _, d := range state.Deploys {
		addRegion(state.FindCloud(d.CloudID), d.Region)
	}
	if t := state.SSHTunnel; t != nil {
		addRegion(state.FindCloud(t.CloudID), t.Region)
	}
	m.mu.Unlock()

	report := ReconcileReport{DryRun: dryRun}
	now := time.Now().UTC()
	for _, tgt := range targets {
		prov, err := m.newProvider(tgt.cred)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s/%s: provider: %v", tgt.cloudID, tgt.cred.Region, err))
			continue
		}
		owned, err := prov.ListOwnedInstances(ctx, tgt.cred.Region)
		if err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s/%s: list: %v", tgt.cloudID, tgt.cred.Region, err))
			continue
		}
		byID := map[string]cloud.Instance{}
		var toRelease []string
		for _, inst := range owned {
			report.Scanned++
			if known[inst.ID] {
				continue // tracked by a live record or the active relay
			}
			o := ReconcileOrphan{
				InstanceID: inst.ID,
				Name:       inst.Name,
				Region:     tgt.cred.Region,
				CloudID:    tgt.cloudID,
				PublicIP:   inst.PublicIP,
				CreatedAt:  inst.CreatedAt,
			}
			switch {
			case !reapableNodeName(inst.Name):
				o.Skipped = "not a managed node or relay — needs manual teardown"
			case inst.CreatedAt.IsZero():
				o.Skipped = "creation time unknown — left running for safety"
			case now.Sub(inst.CreatedAt) < reconcileGrace:
				o.Skipped = "within launch grace window"
			case dryRun:
				o.Skipped = "dry-run"
			default:
				byID[inst.ID] = inst
				toRelease = append(toRelease, inst.ID)
				report.Orphans = append(report.Orphans, o)
				continue
			}
			report.Orphans = append(report.Orphans, o)
		}
		if len(toRelease) == 0 {
			continue
		}
		relErr := prov.DeleteInstances(ctx, toRelease)
		for i := range report.Orphans {
			o := &report.Orphans[i]
			if _, ok := byID[o.InstanceID]; !ok || o.Region != tgt.cred.Region {
				continue
			}
			if relErr != nil {
				o.Skipped = "release failed: " + relErr.Error()
			} else {
				o.Released = true
				report.Released++
			}
		}
		if relErr != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("%s/%s: release: %v", tgt.cloudID, tgt.cred.Region, relErr))
		}
	}
	return report, nil
}
