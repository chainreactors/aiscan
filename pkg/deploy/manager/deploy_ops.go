package manager

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chainreactors/aiscan/pkg/cloud"
	"github.com/chainreactors/aiscan/pkg/deploy"
)

// deployLaunchConcurrency bounds how many RunInstances calls are in flight at
// once during a deploy. Each call is one count=1 instance (so every node gets a
// unique --ioa-node-name); launching them concurrently removes the serial
// N×(API latency) wait.
const deployLaunchConcurrency = 2

// recycleInFlight reports whether a Recycle/RecycleAll has claimed this deploy
// record. CreateDeploy's persist closures run over many seconds while nodes
// launch, so a concurrent recycle can complete underneath them; without this
// guard a late closure would flip the record back to Active and re-adopt the
// just-launched nodes, defeating the recycle and leaking a billable instance.
func recycleInFlight(rec *deploy.DeployRecord) bool {
	return rec.Status == deploy.StatusRecycled || rec.Phase == deploy.PhaseRecycling
}

var (
	deployConfirmTimeout  = 45 * time.Second
	deployConfirmInterval = 5 * time.Second
)

// DeployRequest is the API payload to launch a batch of agent nodes.
type DeployRequest struct {
	CloudID         string            `json:"cloud_id"`
	Region          string            `json:"region"`
	ZoneID          string            `json:"zone_id"`
	ImageID         string            `json:"image_id"`
	InstanceType    string            `json:"instance_type"`
	SecurityGroupID string            `json:"security_group_id"`
	VSwitchID       string            `json:"vswitch_id"`
	VPCID           string            `json:"vpc_id"`
	Count           int               `json:"count"`
	Space           string            `json:"space"`
	BandwidthOut    int               `json:"bandwidth_out"`
	Overrides       map[string]string `json:"overrides"`
	TTLMinutes      int               `json:"ttl_minutes"`
	RecycleWhenIdle bool              `json:"recycle_when_idle"`
	DryRun          bool              `json:"dry_run"`
}

// DeployResult is returned from CreateDeploy. For a dry run only Script is set.
type DeployResult struct {
	Record *DeployRecordView `json:"record,omitempty"`
	Script string            `json:"script,omitempty"`
	DryRun bool              `json:"dry_run"`
}

// userDataFor builds the cloud-init script for one node.
func (m *DeployManager) userDataFor(publicURL, ioaURL, space, nodeName string, overrides map[string]string) string {
	return deploy.GenerateUserData(deploy.UserDataParams{
		PublicURL:   publicURL,
		IOAURL:      ioaURL,
		Space:       space,
		NodeName:    nodeName,
		ProgressURL: deploy.ProgressURL(publicURL, m.ioaToken, nodeName),
		Overrides:   overrides,
	})
}

// CreateDeploy launches req.Count nodes, one instance per call so each gets a
// unique --ioa-node-name (a single RunInstances shares one UserData across all
// instances). On dry-run it returns the script without touching the cloud.
func (m *DeployManager) CreateDeploy(ctx context.Context, req DeployRequest) (*DeployResult, error) {
	persistCtx := context.WithoutCancel(ctx)
	opCtx := context.WithoutCancel(ctx)

	m.mu.Lock()

	state, err := m.store.Load(ctx)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if state.PublicURL == "" {
		m.mu.Unlock()
		return nil, fmt.Errorf("public_url not set; configure the hub's externally reachable address first")
	}
	cred := state.FindCloud(req.CloudID)
	if cred == nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("cloud credential %s not found", req.CloudID)
	}
	credCopy := *cred
	count := req.Count
	if count <= 0 {
		count = 1
	}
	space := deploy.FirstNonEmpty(req.Space, "default")
	region := deploy.FirstNonEmpty(req.Region, credCopy.DefaultRegion)
	if region == "" {
		m.mu.Unlock()
		return nil, fmt.Errorf("region is required (set on request or as credential default)")
	}
	publicURL := state.PublicURL
	ioaURL, err := deploy.NodeIOAURL(publicURL, m.ioaToken)
	if err != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("build IOA url: %w", err)
	}

	deployID := deploy.NewID("dep-")

	if req.DryRun {
		m.mu.Unlock()
		script := m.userDataFor(publicURL, ioaURL, space, deployID+"-0", req.Overrides)
		return &DeployResult{Script: script, DryRun: true}, nil
	}

	prov, err := m.newProvider(providerCred(credCopy, region))
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	providerType, err := deploy.ProviderKind(credCopy.Provider)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if providerType == "tencent" && req.VSwitchID != "" && req.VPCID == "" {
		m.mu.Unlock()
		return nil, fmt.Errorf("vpc_id is required when vswitch_id is set for tencent")
	}

	now := time.Now().UTC()
	rec := deploy.DeployRecord{
		ID:              deployID,
		CloudID:         credCopy.ID,
		Provider:        credCopy.Provider,
		Region:          region,
		Space:           space,
		Status:          deploy.StatusPending,
		Phase:           deploy.PhasePreparing,
		DesiredCount:    count,
		TTLMinutes:      req.TTLMinutes,
		RecycleWhenIdle: req.RecycleWhenIdle,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	state.Deploys = append(state.Deploys, rec)
	if err := m.store.Save(persistCtx, state); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	m.mu.Unlock()

	// Fill blank network fields by ensuring a usable VPC / vSwitch / security
	// group for the region — discovering existing resources, else creating a
	// minimal set so even a never-used region can launch. User-supplied values
	// always win. Resources aiscan creates are recorded on the deploy up front so
	// a later recycle reclaims them even if the launch (or the ensure) then fails.
	zoneID, vswitchID, vpcID, sgID := req.ZoneID, req.VSwitchID, req.VPCID, req.SecurityGroupID
	if vswitchID == "" || sgID == "" || (providerType == "tencent" && vpcID == "") {
		_, _ = m.updateDeployRecord(persistCtx, deployID, func(r *deploy.DeployRecord) {
			r.Phase = deploy.PhaseEnsuringNetwork
		})
		resolved, created, nerr := prov.EnsureNetwork(opCtx, region, zoneID, deployID)
		if !created.Empty() {
			if _, perr := m.updateDeployRecord(persistCtx, deployID, func(r *deploy.DeployRecord) {
				r.AutoNet = autoNet(created)
			}); perr != nil && nerr == nil {
				nerr = perr
			}
		}
		if nerr != nil {
			finalRec, _ := m.updateDeployRecord(persistCtx, deployID, func(r *deploy.DeployRecord) {
				r.Status = deploy.StatusFailed
				r.Phase = deploy.PhaseFailed
				r.Error = "ensure network: " + nerr.Error()
			})
			finalRec = m.reclaimAutoNetwork(opCtx, persistCtx, prov, finalRec)
			view := m.viewForRecord(finalRec)
			return &DeployResult{Record: &view}, fmt.Errorf("ensure network in %s: %w", region, nerr)
		}
		zoneID = deploy.FirstNonEmpty(zoneID, resolved.ZoneID)
		vswitchID = deploy.FirstNonEmpty(vswitchID, resolved.VSwitchID)
		vpcID = deploy.FirstNonEmpty(vpcID, resolved.VPCID)
		sgID = deploy.FirstNonEmpty(sgID, resolved.SecurityGroupID)
	}
	_, _ = m.updateDeployRecord(persistCtx, deployID, func(r *deploy.DeployRecord) {
		r.Phase = deploy.PhaseLaunchingInstances
	})

	// Launch the nodes concurrently (bounded by deployLaunchConcurrency). Each
	// goroutine creates one instance with a deterministic node name; results are
	// persisted as soon as it returns, so a later crash still leaves recyclable IDs.
	type launchResult struct {
		index int
		nodes []deploy.DeployNode
		err   error
	}
	results := make([]launchResult, count)
	resultCh := make(chan launchResult, count)
	sem := make(chan struct{}, deployLaunchConcurrency)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			nodeName := fmt.Sprintf("%s-%d", deployID, i)
			userData := m.userDataFor(publicURL, ioaURL, space, nodeName, req.Overrides)
			insts, err := prov.CreateInstances(opCtx, cloud.CreateRequest{
				Region:          region,
				ZoneID:          zoneID,
				ImageID:         req.ImageID,
				InstanceType:    req.InstanceType,
				SecurityGroupID: sgID,
				VSwitchID:       vswitchID,
				VPCID:           vpcID,
				Count:           1,
				UserData:        userData,
				Name:            nodeName,
				BandwidthOut:    req.BandwidthOut,
				Tags:            map[string]string{cloud.TagDeployID: deployID},
			})
			if err != nil {
				resultCh <- launchResult{index: i, err: fmt.Errorf("node %d: %w", i, err)}
				return
			}
			nodes := make([]deploy.DeployNode, 0, len(insts))
			for _, inst := range insts {
				nodes = append(nodes, deploy.DeployNode{
					InstanceID: inst.ID,
					NodeName:   nodeName,
					PublicIP:   inst.PublicIP,
					PrivateIP:  inst.PrivateIP,
					Status:     inst.Status,
				})
			}
			resultCh <- launchResult{index: i, nodes: nodes}
		}(i)
	}
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	var persistErr error
	for res := range resultCh {
		results[res.index] = res
		if len(res.nodes) == 0 {
			continue
		}
		if _, err := m.updateDeployRecord(persistCtx, deployID, func(rec *deploy.DeployRecord) {
			if recycleInFlight(rec) {
				// A concurrent Recycle/RecycleAll claimed this deploy mid-launch.
				// Don't append/resurrect it to Active — the just-launched nodes
				// carry the managed-by tag and get swept by Reconcile.
				return
			}
			rec.Nodes = append(rec.Nodes, res.nodes...)
			rec.Status = deploy.StatusActive
			rec.Phase = deploy.PhaseLaunchingInstances
			sortDeployNodes(rec.Nodes)
		}); err != nil && persistErr == nil {
			persistErr = err
		}
	}

	var launchErr error
	created := 0
	for i := 0; i < count; i++ {
		if results[i].err != nil {
			if launchErr == nil {
				launchErr = results[i].err // lowest-index error
			}
			continue
		}
		created += len(results[i].nodes)
	}

	finalRec, finalSaveErr := m.updateDeployRecord(persistCtx, deployID, func(rec *deploy.DeployRecord) {
		if recycleInFlight(rec) {
			return // recycled concurrently; leave the recycled state intact
		}
		sortDeployNodes(rec.Nodes)
		switch {
		case len(rec.Nodes) == 0:
			rec.Status = deploy.StatusFailed
			rec.Phase = deploy.PhaseFailed
			if launchErr != nil {
				rec.Error = launchErr.Error()
			}
		case launchErr != nil:
			rec.Status = deploy.StatusActive
			rec.Phase = deploy.PhaseWaitingRegistration
			rec.Error = "partial: " + launchErr.Error()
		default:
			rec.Status = deploy.StatusActive
			rec.Phase = deploy.PhaseWaitingRegistration
			rec.Error = ""
		}
	})
	if persistErr != nil {
		// The record couldn't be persisted, so record-driven recycle can't be
		// trusted to find these instances. Roll them back now (best-effort); the
		// managed-by-aiscan tag also lets a later Reconcile catch any that survive.
		var orphaned []string
		for i := range results {
			for _, n := range results[i].nodes {
				if n.InstanceID != "" {
					orphaned = append(orphaned, n.InstanceID)
				}
			}
		}
		if len(orphaned) > 0 {
			_ = prov.DeleteInstances(opCtx, orphaned)
		}
		if finalRec.ID == "" {
			finalRec = rec
		}
		view := m.viewForRecord(finalRec)
		return &DeployResult{Record: &view}, fmt.Errorf("persist launched nodes: %w", persistErr)
	}
	if finalSaveErr != nil {
		if finalRec.ID == "" {
			finalRec = rec
		}
		view := m.viewForRecord(finalRec)
		return &DeployResult{Record: &view}, finalSaveErr
	}
	if launchErr != nil && created == 0 {
		finalRec = m.reclaimAutoNetwork(opCtx, persistCtx, prov, finalRec)
	}
	view := m.viewForRecord(finalRec)
	res := &DeployResult{Record: &view}
	if launchErr != nil && created == 0 {
		return res, launchErr
	}
	return res, nil
}

func sortDeployNodes(nodes []deploy.DeployNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		return nodes[i].NodeName < nodes[j].NodeName
	})
}

func (m *DeployManager) updateDeployRecord(ctx context.Context, id string, update func(*deploy.DeployRecord)) (deploy.DeployRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := m.store.Load(ctx)
	if err != nil {
		return deploy.DeployRecord{}, err
	}
	rec := state.FindDeploy(id)
	if rec == nil {
		return deploy.DeployRecord{}, fmt.Errorf("deploy %s not found", id)
	}
	update(rec)
	rec.UpdatedAt = time.Now().UTC()
	if err := m.store.Save(ctx, state); err != nil {
		return *rec, err
	}
	return *rec, nil
}

// viewForRecord cross-references a record's nodes against the live AgentPool.
func (m *DeployManager) viewForRecord(rec deploy.DeployRecord) DeployRecordView {
	byName := map[string]deploy.AgentSnapshot{}
	for _, a := range m.pool.AgentSnapshots() {
		byName[a.NodeName] = a
	}
	view := DeployRecordView{DeployRecord: rec}
	view.Nodes = make([]DeployNodeView, 0, len(rec.Nodes))
	for _, n := range rec.Nodes {
		nv := DeployNodeView{DeployNode: n}
		if a, ok := byName[n.NodeName]; ok {
			nv.Registered = true
			nv.AgentID = a.AgentID
			nv.Busy = a.Busy
			view.RegisteredCount++
		} else if n.Status != deploy.StatusRecycled {
			view.Orphans++
			nv.Progress = m.nodeProgressView(n.NodeName) // bootstrap progress, if the node reported any
		}
		view.Nodes = append(view.Nodes, nv)
	}
	// Recycled deployments have no live nodes; don't count them as orphans.
	if rec.Status == deploy.StatusRecycled {
		view.Orphans = 0
	}
	applyDeployPhase(&view)
	return view
}

func applyDeployPhase(view *DeployRecordView) {
	desired := view.DesiredCount
	if desired <= 0 {
		desired = len(view.Nodes)
	}
	switch view.Status {
	case deploy.StatusFailed:
		view.Phase = deploy.PhaseFailed
	case deploy.StatusRecycled:
		view.Phase = deploy.PhaseRecycled
	case deploy.StatusPending:
		if view.Phase == "" {
			view.Phase = deploy.PhasePreparing
		}
	case deploy.StatusActive:
		if view.Phase != deploy.PhaseRecycling && len(view.Nodes) > 0 && view.RegisteredCount >= len(view.Nodes) {
			view.Phase = deploy.PhaseReady
			return
		}
		if view.Phase == "" {
			if len(view.Nodes) > 0 || desired > 0 {
				view.Phase = deploy.PhaseWaitingRegistration
			} else {
				view.Phase = deploy.PhaseLaunchingInstances
			}
		}
	}
}

func (m *DeployManager) ListDeploys(ctx context.Context) ([]DeployRecordView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]DeployRecordView, 0, len(state.Deploys))
	for _, rec := range state.Deploys {
		views = append(views, m.viewForRecord(rec))
	}
	return views, nil
}

func (m *DeployManager) GetDeploy(ctx context.Context, id string) (*DeployRecordView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state, err := m.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	rec := state.FindDeploy(id)
	if rec == nil {
		return nil, fmt.Errorf("deploy %s not found", id)
	}
	view := m.viewForRecord(*rec)
	return &view, nil
}

// Recycle terminates instances of a deploy. If instanceIDs is empty, all of the
// deploy's instances are reclaimed; otherwise only the listed ones (scale-in).
// It is idempotent and confirms termination by polling the provider.
func (m *DeployManager) Recycle(ctx context.Context, id string, instanceIDs []string) (*DeployRecordView, error) {
	persistCtx := context.WithoutCancel(ctx)
	opCtx := context.WithoutCancel(ctx)

	m.mu.Lock()
	state, err := m.store.Load(ctx)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	rec := state.FindDeploy(id)
	if rec == nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("deploy %s not found", id)
	}
	cred := state.FindCloud(rec.CloudID)
	var credForProvider cloud.Credential
	if cred != nil {
		credForProvider = providerCred(*cred, rec.Region)
	}

	// Already recycled: idempotent, but still retry a previously-failed network
	// teardown so leaked auto-created resources eventually get cleaned up.
	if rec.Status == deploy.StatusRecycled {
		recCopy := *rec
		m.mu.Unlock()
		if !recCopy.AutoNet.Empty() && cred != nil {
			if prov, perr := m.newProvider(credForProvider); perr == nil {
				recCopy = m.reclaimAutoNetwork(opCtx, persistCtx, prov, recCopy)
			}
		}
		view := m.viewForRecord(recCopy)
		return &view, nil
	}
	if cred == nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("credential %s for deploy %s is gone; cannot recycle via API", rec.CloudID, id)
	}
	prov, err := m.newProvider(credForProvider)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}

	known := map[string]bool{}
	for _, n := range rec.Nodes {
		if n.InstanceID != "" {
			known[n.InstanceID] = true
		}
	}
	var target []string
	if len(instanceIDs) == 0 {
		target = rec.InstanceIDs()
	} else {
		seen := map[string]bool{}
		for _, raw := range instanceIDs {
			id := strings.TrimSpace(raw)
			if id == "" {
				continue
			}
			if !known[id] {
				m.mu.Unlock()
				return nil, fmt.Errorf("instance %s is not part of deploy %s", id, rec.ID)
			}
			if !seen[id] {
				target = append(target, id)
				seen[id] = true
			}
		}
	}
	if len(target) == 0 {
		rec.Status = deploy.StatusRecycled
		rec.Phase = deploy.PhaseRecycled
		now := time.Now().UTC()
		rec.RecycledAt = &now
		rec.UpdatedAt = now
		if err := m.store.Save(persistCtx, state); err != nil {
			m.mu.Unlock()
			return nil, err
		}
		recCopy := *rec
		m.mu.Unlock()
		recCopy = m.reclaimAutoNetwork(opCtx, persistCtx, prov, recCopy)
		view := m.viewForRecord(recCopy)
		return &view, nil
	}
	rec.Phase = deploy.PhaseRecycling
	rec.UpdatedAt = time.Now().UTC()
	if err := m.store.Save(persistCtx, state); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	m.mu.Unlock()

	if err := prov.DeleteInstances(opCtx, target); err != nil {
		_, _ = m.updateDeployRecord(persistCtx, id, func(rec *deploy.DeployRecord) {
			if rec.Status != deploy.StatusRecycled {
				rec.Phase = deploy.PhaseWaitingRegistration
				rec.Error = "recycle: " + err.Error()
			}
		})
		return nil, fmt.Errorf("terminate instances: %w", err)
	}
	// Confirm they actually disappear before marking recycled (don't trust the
	// delete call alone).
	gone := m.confirmGone(opCtx, prov, target)
	targetSet := map[string]bool{}
	for _, t := range target {
		targetSet[t] = true
	}

	var recycledNames []string
	finalRec, err := m.updateDeployRecord(persistCtx, id, func(rec *deploy.DeployRecord) {
		if rec.Status == deploy.StatusRecycled {
			return
		}
		recycledNames = recycledNames[:0]
		remaining := rec.Nodes[:0]
		for _, n := range rec.Nodes {
			if targetSet[n.InstanceID] {
				recycledNames = append(recycledNames, n.NodeName)
			}
			if targetSet[n.InstanceID] && gone[n.InstanceID] {
				continue // dropped
			}
			if targetSet[n.InstanceID] {
				n.Status = "recycling" // delete issued but not yet confirmed gone
			}
			remaining = append(remaining, n)
		}
		rec.Nodes = remaining
		stillRecycling := false
		for _, n := range rec.Nodes {
			if n.Status == "recycling" {
				stillRecycling = true
				break
			}
		}
		switch {
		case len(rec.Nodes) == 0:
			rec.Status = deploy.StatusRecycled
			rec.Phase = deploy.PhaseRecycled
			now := time.Now().UTC()
			rec.RecycledAt = &now
		case stillRecycling:
			// Some targeted instances were deleted but not yet confirmed gone.
			rec.Phase = deploy.PhaseRecycling
		default:
			// A scale-in that left only healthy, untargeted nodes: the deploy is
			// still active, not recycling. Let applyDeployPhase recompute ready.
			rec.Phase = deploy.PhaseWaitingRegistration
		}
	})
	m.clearNodeProgress(recycledNames...)
	if err != nil {
		return nil, err
	}
	finalRec = m.reclaimAutoNetwork(opCtx, persistCtx, prov, finalRec)
	view := m.viewForRecord(finalRec)
	return &view, nil
}

// reclaimAutoNetwork tears down a recycled deploy's auto-created network, or a
// failed deploy that never launched instances, and clears the record so it isn't
// retried. Instances must already be gone; the provider retries any piece still
// pinned by a lingering ENI. On failure AutoNet is left recorded so a later
// recycle attempt retries.
func (m *DeployManager) reclaimAutoNetwork(ctx, persistCtx context.Context, prov cloud.Provider, rec deploy.DeployRecord) deploy.DeployRecord {
	reclaimableFailed := rec.Status == deploy.StatusFailed && len(rec.Nodes) == 0
	if (rec.Status != deploy.StatusRecycled && !reclaimableFailed) || rec.AutoNet.Empty() {
		return rec
	}
	if err := prov.TeardownNetwork(ctx, rec.Region, netDefaults(rec.AutoNet)); err != nil {
		return rec // leave AutoNet set; a later recycle retries
	}
	if updated, err := m.updateDeployRecord(persistCtx, rec.ID, func(r *deploy.DeployRecord) {
		r.AutoNet = deploy.AutoNetwork{}
		if r.Status == deploy.StatusRecycled {
			r.Phase = deploy.PhaseRecycled
		} else if r.Status == deploy.StatusFailed {
			r.Phase = deploy.PhaseFailed
		}
	}); err == nil {
		return updated
	}
	return rec
}

// confirmGone polls the provider (briefly) and returns which instance IDs are
// no longer present.
func (m *DeployManager) confirmGone(ctx context.Context, prov cloud.Provider, ids []string) map[string]bool {
	requested := map[string]bool{}
	for _, id := range ids {
		requested[id] = true
	}
	deadline := time.Now().Add(deployConfirmTimeout)
	for {
		gone := map[string]bool{}
		for id := range requested {
			gone[id] = true
		}
		insts, err := prov.ListInstances(ctx, ids)
		if err != nil {
			// On query error, optimistically assume the delete will take effect.
			return gone
		}
		stillThere := map[string]bool{}
		for _, it := range insts {
			if !requested[it.ID] {
				continue
			}
			// Some providers report a transient "Stopping"/"Terminating" state.
			st := strings.ToLower(it.Status)
			if strings.Contains(st, "terminat") || strings.Contains(st, "deleted") || strings.Contains(st, "released") {
				continue
			}
			stillThere[it.ID] = true
			gone[it.ID] = false
		}
		if len(stillThere) == 0 {
			return gone
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return gone
		}
		select {
		case <-ctx.Done():
			return gone
		case <-time.After(deployConfirmInterval):
		}
	}
}

// RecycleAll reclaims every active deploy, optionally filtered by cloud and/or
// space. Returns the number of deploys recycled.
func (m *DeployManager) RecycleAll(ctx context.Context, cloudID, space string) (int, error) {
	m.mu.Lock()
	ids := []string{}
	state, err := m.store.Load(ctx)
	if err != nil {
		m.mu.Unlock()
		return 0, err
	}
	for _, d := range state.Deploys {
		if d.Status == deploy.StatusRecycled {
			continue
		}
		if cloudID != "" && d.CloudID != cloudID {
			continue
		}
		if space != "" && d.Space != space {
			continue
		}
		ids = append(ids, d.ID)
	}
	m.mu.Unlock()

	n := 0
	for _, id := range ids {
		if _, err := m.Recycle(ctx, id, nil); err != nil {
			return n, fmt.Errorf("recycle %s: %w", id, err)
		}
		n++
	}
	return n, nil
}

// StartReaper runs the TTL / idle auto-recycle loop until ctx is canceled. Every
// reconcileEveryTicks it also runs a cloud reconcile to terminate tagged orphan
// nodes that record-driven recycle can't see.
func (m *DeployManager) StartReaper(ctx context.Context, logf func(string, ...interface{})) {
	const reconcileEveryTicks = 10 // ticker fires per minute → reconcile ~every 10 min
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	ticks := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.reapOnce(ctx, logf)
			ticks++
			if ticks%reconcileEveryTicks == 0 {
				if rep, err := m.Reconcile(ctx, false); err != nil {
					if logf != nil {
						logf("reconcile failed: %s", err)
					}
				} else if logf != nil && (rep.Released > 0 || len(rep.Errors) > 0) {
					logf("reconcile: scanned=%d released=%d errors=%d", rep.Scanned, rep.Released, len(rep.Errors))
				}
			}
		}
	}
}

func (m *DeployManager) reapOnce(ctx context.Context, logf func(string, ...interface{})) {
	m.mu.Lock()
	state, err := m.store.Load(ctx)
	if err != nil {
		m.mu.Unlock()
		return
	}
	now := time.Now().UTC()
	const idleGrace = 10 * time.Minute
	// A node that boots but never dials home just bills forever; reap the deploy
	// once it has clearly failed to register (no node connected past this window).
	const registerTimeout = 20 * time.Minute
	var toRecycle []string
	busyByName := map[string]bool{}
	registeredByName := map[string]bool{}
	for _, a := range m.pool.AgentSnapshots() {
		busyByName[a.NodeName] = a.Busy
		registeredByName[a.NodeName] = true
	}
	for _, d := range state.Deploys {
		if d.Status == deploy.StatusRecycled || d.Status == deploy.StatusFailed {
			continue
		}
		age := now.Sub(d.CreatedAt)
		if d.TTLMinutes > 0 && age >= time.Duration(d.TTLMinutes)*time.Minute {
			toRecycle = append(toRecycle, d.ID)
			continue
		}
		if age >= registerTimeout && d.Phase == deploy.PhaseWaitingRegistration {
			anyRegistered := false
			for _, n := range d.Nodes {
				if registeredByName[n.NodeName] {
					anyRegistered = true
					break
				}
			}
			if !anyRegistered {
				toRecycle = append(toRecycle, d.ID)
				continue
			}
		}
		if d.RecycleWhenIdle && age >= idleGrace {
			anyBusy := false
			for _, n := range d.Nodes {
				if busyByName[n.NodeName] {
					anyBusy = true
					break
				}
			}
			if !anyBusy {
				toRecycle = append(toRecycle, d.ID)
			}
		}
	}
	m.mu.Unlock()

	for _, id := range toRecycle {
		if _, err := m.Recycle(ctx, id, nil); err != nil {
			if logf != nil {
				logf("auto-recycle %s failed: %s", id, err)
			}
		} else if logf != nil {
			logf("auto-recycled deploy %s (ttl/idle)", id)
		}
	}
}
