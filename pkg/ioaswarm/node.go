package ioaswarm

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	ioaclient "github.com/chainreactors/ioa/client"
	"github.com/chainreactors/ioa/protocols"
	"github.com/chainreactors/ioa/protocols/swarm"
)

type Logger interface {
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
	Importantf(format string, args ...any)
}

type nopLogger struct{}

func NopLogger() Logger                     { return nopLogger{} }
func (nopLogger) Debugf(string, ...any)     {}
func (nopLogger) Infof(string, ...any)      {}
func (nopLogger) Warnf(string, ...any)      {}
func (nopLogger) Errorf(string, ...any)     {}
func (nopLogger) Importantf(string, ...any) {}

type Task struct {
	MessageID string
	Sender    string
	Content   string
	Targets   []string
	Meta      map[string]any
	Refs      protocols.Ref
}

type PeerMessage struct {
	MessageID   string
	Sender      string
	ContentType string
	Content     string
	RawContent  map[string]any
	Targets     []string
	Meta        map[string]any
	Refs        protocols.Ref
}

func (t Task) Prompt() string {
	var sb strings.Builder
	sb.WriteString(t.Content)
	if len(t.Targets) > 0 {
		sb.WriteString("\n\nTargets:\n")
		for _, tgt := range t.Targets {
			sb.WriteString("- ")
			sb.WriteString(tgt)
			sb.WriteByte('\n')
		}
	}
	if len(t.Meta) > 0 {
		skip := true
		for k := range t.Meta {
			if k != "kind" {
				skip = false
				break
			}
		}
		if !skip {
			if data, err := json.MarshalIndent(t.Meta, "", "  "); err == nil {
				sb.WriteString("\nContext:\n")
				sb.Write(data)
				sb.WriteByte('\n')
			}
		}
	}
	return sb.String()
}

type TaskHandler func(ctx context.Context, task Task) (string, error)

type HeartbeatFunc func(ctx context.Context, prompt string) (string, error)

type heartbeatConfig struct {
	name         string
	interval     time.Duration
	prompt       string
	contextLimit int
}

type NodeConfig struct {
	Client                ioaclient.StreamAPI
	NodeName              string
	SpaceName             string
	SpaceDescription      string
	PollInterval          time.Duration
	HeartbeatInterval     time.Duration
	HeartbeatContextLimit int
	Prompt                string
	Meta                  map[string]any
	OnTask                TaskHandler
	OnPeer                func(PeerMessage) bool
	OnHeartbeat           HeartbeatFunc
	Logger                Logger
	Head                  string
	ForkDepth             int
}

type Node struct {
	cfg        NodeConfig
	mu         sync.Mutex
	historical map[string]struct{}
	dispatched map[string]struct{}
	lastSeenID string
	spaceID    string
	spaceName  string
	runCtx     context.Context

	pending []pendingTask
}

type pendingTask struct {
	msg protocols.Message
	sm  swarm.SwarmMessage
}

type activeTask struct {
	messageID string
	done      chan taskResult
}

type taskResult struct {
	messageID string
	result    string
	err       error
}

const defaultPeerBufferSize = 64

func NewNode(cfg NodeConfig) *Node {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 2 * time.Second
	}
	if cfg.HeartbeatContextLimit <= 0 {
		cfg.HeartbeatContextLimit = 50
	}
	if cfg.NodeName == "" {
		cfg.NodeName = "ioa-swarm"
	}
	if cfg.SpaceDescription == "" {
		cfg.SpaceDescription = "swarm worker"
	}
	if cfg.Logger == nil {
		cfg.Logger = NopLogger()
	}
	return &Node{
		cfg:        cfg,
		historical: make(map[string]struct{}),
		dispatched: make(map[string]struct{}),
	}
}

func (n *Node) NodeID() string  { return n.cfg.Client.NodeID() }
func (n *Node) SpaceID() string { return n.spaceID }

func (n *Node) Run(ctx context.Context) error {
	if n.cfg.Client == nil {
		return fmt.Errorf("ioa client is required")
	}
	if n.cfg.OnTask == nil {
		return fmt.Errorf("task handler is required")
	}
	if n.cfg.Client.NodeID() == "" {
		node, err := n.cfg.Client.RegisterNode(ctx, n.cfg.NodeName, n.cfg.SpaceDescription, n.cfg.Meta)
		if err != nil {
			return err
		}
		n.cfg.Logger.Infof("swarm node=%s name=%q status=registered", node.ID, node.Name)
	}
	if strings.TrimSpace(n.cfg.SpaceName) == "" {
		return fmt.Errorf("space name is required")
	}
	space, err := n.cfg.Client.Space(ctx, n.cfg.SpaceName, n.cfg.SpaceDescription)
	if err != nil {
		return err
	}
	n.spaceID = space.ID
	n.spaceName = space.Name
	n.cfg.Logger.Importantf("swarm status=listening space=%s name=%q node=%s", space.ID, space.Name, n.cfg.Client.NodeID())

	n.runCtx = ctx

	var active *activeTask

	if n.cfg.HeartbeatInterval > 0 && n.cfg.OnHeartbeat != nil {
		if err := n.markExisting(ctx); err != nil {
			return err
		}
		hb := heartbeatConfig{
			name:         "heartbeat",
			interval:     n.cfg.HeartbeatInterval,
			prompt:       n.cfg.Prompt,
			contextLimit: n.cfg.HeartbeatContextLimit,
		}
		if err := n.execHeartbeat(ctx, hb); err != nil {
			n.cfg.Logger.Warnf("swarm heartbeat init failed: %s", err)
		}
	} else {
		if err := n.catchUp(ctx, &active); err != nil {
			return err
		}
	}

	var subOpts []ioaclient.SubscribeOption
	if n.cfg.Head != "" {
		subOpts = append(subOpts, ioaclient.WithHead(n.cfg.Head))
	}
	if n.cfg.ForkDepth > 0 {
		subOpts = append(subOpts, ioaclient.WithForkDepth(n.cfg.ForkDepth))
	}
	messages, errs, cancel, err := n.cfg.Client.Subscribe(ctx, n.spaceID, subOpts...)
	if err != nil {
		return err
	}
	defer cancel()

	ticker := time.NewTicker(n.cfg.PollInterval)
	defer ticker.Stop()

	activeDone := func() <-chan taskResult {
		if active == nil {
			return nil
		}
		return active.done
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case err, ok := <-errs:
			if ok && err != nil {
				return err
			}
		case msg, ok := <-messages:
			if !ok {
				return nil
			}
			if _, err := n.routeIncoming(ctx, msg, &active); err != nil {
				n.cfg.Logger.Warnf("swarm message failed: %s", err)
			}
		case <-ticker.C:
			if err := n.catchUp(ctx, &active); err != nil {
				n.cfg.Logger.Warnf("swarm catch-up failed: %s", err)
			}
		case res := <-activeDone():
			if err := n.completeTask(ctx, res); err != nil {
				n.cfg.Logger.Warnf("swarm task=report failed: %s", err)
			}
			active = nil
			for len(n.pending) > 0 {
				next := n.pending[0]
				n.pending = n.pending[1:]
				n.cfg.Logger.Importantf("swarm task=dequeued msg=%s queue_depth=%d",
					next.msg.ID, len(n.pending))
				state, err := n.startTask(ctx, next.msg, next.sm)
				if err != nil {
					n.cfg.Logger.Warnf("swarm task=dequeue-start failed msg=%s: %s",
						next.msg.ID, err)
					continue
				}
				active = state
				break
			}
		}
	}
}


func (n *Node) catchUp(ctx context.Context, active **activeTask) error {
	messages, err := n.cfg.Client.Read(ctx, n.spaceID, protocols.ReadOptions{
		All:   true,
		After: n.watermark(),
	})
	if err != nil {
		return err
	}
	var lastHandled string
	deferred := false
	for _, msg := range messages {
		handled, routeErr := n.routeIncoming(ctx, msg, active)
		if routeErr != nil {
			n.cfg.Logger.Warnf("swarm catch-up message failed: %s", routeErr)
		}
		if !handled {
			deferred = true
		}
		if !deferred {
			lastHandled = msg.ID
		}
	}
	if lastHandled != "" {
		n.advanceWatermark(lastHandled)
	}
	return nil
}

func (n *Node) markExisting(ctx context.Context) error {
	messages, err := n.cfg.Client.Read(ctx, n.spaceID, protocols.ReadOptions{All: true})
	if err != nil {
		return err
	}
	for _, msg := range messages {
		n.markHistorical(msg.ID)
	}
	if len(messages) > 0 {
		n.mu.Lock()
		n.lastSeenID = messages[len(messages)-1].ID
		n.mu.Unlock()
	}
	return nil
}

func (n *Node) RunHeartbeat(ctx context.Context) error {
	return n.execHeartbeat(ctx, heartbeatConfig{
		name:         "heartbeat",
		interval:     n.cfg.HeartbeatInterval,
		prompt:       n.cfg.Prompt,
		contextLimit: n.cfg.HeartbeatContextLimit,
	})
}

func (n *Node) execHeartbeat(ctx context.Context, hb heartbeatConfig) error {
	messages, err := n.cfg.Client.Read(ctx, n.spaceID, protocols.ReadOptions{All: true, Limit: hb.contextLimit})
	if err != nil {
		return err
	}
	n.cfg.Logger.Importantf("swarm heartbeat=%s running space=%s", hb.name, n.spaceID)

	prompt := n.heartbeatPrompt(hb, messages)
	result, runErr := n.cfg.OnHeartbeat(ctx, prompt)

	report := swarm.SwarmMessage{Content: result}
	if runErr != nil {
		report.Content = fmt.Sprintf("Heartbeat %s error: %s", hb.name, runErr.Error())
	}
	_, sendErr := n.cfg.Client.Send(ctx, n.spaceID, protocols.SendMessage{
		ContentType: "swarm",
		Content:     swarm.SwarmContent(report),
	})
	if runErr != nil {
		return runErr
	}
	if sendErr == nil {
		n.cfg.Logger.Importantf("swarm heartbeat=%s completed space=%s", hb.name, n.spaceID)
	}
	return sendErr
}

func (n *Node) heartbeatPrompt(hb heartbeatConfig, messages []protocols.Message) string {
	contextView := SlimMessageContext(messages, 32<<10)
	intent := strings.TrimSpace(hb.prompt)
	if intent == "" {
		intent = strings.TrimSpace(n.cfg.Prompt)
	}
	if intent == "" {
		intent = "No explicit worker intent was configured."
	}
	return fmt.Sprintf(`This is a Swarm heartbeat turn.

Heartbeat: %s (every %s)

Space:
- id: %s
- name: %s

This node:
- id: %s
- name: %s
- intent: %s

Review the recent messages below and decide the next useful step.
If this worker should act now, use the available local tools directly.
If no action is needed, say that briefly and do not repeat completed work.

Recent messages (oldest to newest):
%s`, hb.name, hb.interval, n.spaceID, n.spaceName, n.cfg.Client.NodeID(), n.cfg.NodeName, intent, contextView)
}

type slimEntry struct {
	ID       string   `json:"id"`
	Sender   string   `json:"sender"`
	Kind     string   `json:"kind,omitempty"`
	Preview  string   `json:"preview"`
	RefMsgs  []string `json:"ref_msgs,omitempty"`
	RefNodes []string `json:"ref_nodes,omitempty"`
}

func SlimMessageContext(messages []protocols.Message, budgetBytes int) string {
	var entries []slimEntry
	errorCounts := make(map[string]int)
	var latestError string

	for _, msg := range messages {
		sm, _ := swarm.SwarmFromIOA(msg)
		kind := swarm.MessageKind(msg, sm)

		content := sm.Content
		if content == "" {
			keys := make([]string, 0, len(msg.Content))
			for k := range msg.Content {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			content = "{" + strings.Join(keys, ", ") + "}"
		}

		if cat := classifyError(content); cat != "" {
			errorCounts[cat]++
			latestError = truncate(content, 200)
			continue
		}

		maxPreview := 300
		preview := content
		if len(preview) > maxPreview {
			preview = preview[:maxPreview] + "..."
		}

		entry := slimEntry{
			ID:      msg.ID,
			Sender:  msg.Sender,
			Kind:    kind,
			Preview: preview,
		}
		if len(msg.Refs.Messages) > 0 {
			entry.RefMsgs = msg.Refs.Messages
		}
		if len(msg.Refs.Nodes) > 0 {
			entry.RefNodes = msg.Refs.Nodes
		}
		entries = append(entries, entry)
	}

	return renderSlimMessageContext(entries, formatErrorSuffix(errorCounts, latestError), budgetBytes)
}

func renderSlimMessageContext(entries []slimEntry, errSuffix string, budgetBytes int) string {
	if budgetBytes <= 0 {
		return formatSlimMessageContext(entries, errSuffix, 0)
	}
	out := formatSlimMessageContext(entries, errSuffix, 0)
	if len(out) <= budgetBytes {
		return out
	}
	if len(entries) == 0 {
		return truncateToBytes(out, budgetBytes)
	}
	lo, hi := 0, len(entries)
	for lo < hi {
		mid := (lo + hi) / 2
		candidate := formatSlimMessageContext(entries[mid:], errSuffix, mid)
		if len(candidate) <= budgetBytes {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	if lo >= len(entries) {
		return truncateToBytes(formatSlimMessageContext(nil, errSuffix, len(entries)), budgetBytes)
	}
	return formatSlimMessageContext(entries[lo:], errSuffix, lo)
}

func formatSlimMessageContext(entries []slimEntry, errSuffix string, trimmed int) string {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		data = []byte("[]")
	}
	var sb strings.Builder
	if trimmed > 0 {
		fmt.Fprintf(&sb, "[%d older messages trimmed for context budget]\n\n", trimmed)
	}
	sb.Write(data)
	sb.WriteString(errSuffix)
	return sb.String()
}

func truncateToBytes(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	if cut == 0 {
		return ""
	}
	return s[:cut]
}

func classifyError(content string) string {
	lower := strings.ToLower(strings.TrimSpace(content))
	if !looksLikeGeneratedError(lower) {
		return ""
	}
	switch {
	case strings.Contains(lower, "quota") ||
		strings.Contains(lower, "rate_limit") ||
		strings.Contains(lower, "rate limit"):
		return "provider_quota"
	case strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "timed out"):
		return "timeout"
	case strings.HasPrefix(lower, "error:") ||
		strings.HasPrefix(lower, "provider error:") ||
		(strings.HasPrefix(lower, "heartbeat") && strings.Contains(lower, "error:")):
		return "provider_error"
	default:
		return ""
	}
}

func looksLikeGeneratedError(lower string) bool {
	return strings.HasPrefix(lower, "error:") ||
		strings.HasPrefix(lower, "provider error:") ||
		(strings.HasPrefix(lower, "heartbeat ") && strings.Contains(lower, " error:"))
}

func formatErrorSuffix(counts map[string]int, latest string) string {
	if len(counts) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\nAggregated errors:\n")
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&sb, "  %s: %d\n", k, counts[k])
	}
	if latest != "" {
		sb.WriteString("  latest: ")
		sb.WriteString(latest)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func (n *Node) routeIncoming(ctx context.Context, msg protocols.Message, active **activeTask) (bool, error) {
	if protocols.MessageContentType(msg) == "ioa/fork" {
		if *active != nil && n.cfg.OnPeer != nil {
			peer := peerMessageFromIOA(msg, swarm.SwarmMessage{})
			n.cfg.OnPeer(peer)
		}
		n.markDispatched(msg.ID)
		return true, nil
	}

	sm, ok := swarm.SwarmFromIOA(msg)
	if msg.Sender == n.cfg.Client.NodeID() {
		return true, nil
	}

	nodeID := n.cfg.Client.NodeID()
	if len(msg.Refs.Nodes) > 0 && !slices.Contains(msg.Refs.Nodes, nodeID) {
		n.markDispatched(msg.ID)
		return true, nil
	}

	if n.isKnown(msg.ID) {
		return true, nil
	}

	isActive := *active != nil
	if ok && isTaskMessage(msg, sm, nodeID, isActive) {
		if *active != nil {
			n.markDispatched(msg.ID)
			n.pending = append(n.pending, pendingTask{msg: msg, sm: sm})
			n.cfg.Logger.Infof("swarm task=queued msg=%s active=%s queue_depth=%d",
				msg.ID, (*active).messageID, len(n.pending))
			return true, nil
		}
		n.markDispatched(msg.ID)
		state, err := n.startTask(ctx, msg, sm)
		if err != nil {
			return true, err
		}
		*active = state
		return true, nil
	}

	if *active != nil {
		peer := peerMessageFromIOA(msg, sm)
		if n.cfg.OnPeer != nil && n.cfg.OnPeer(peer) {
			n.markDispatched(msg.ID)
			n.cfg.Logger.Debugf("swarm peer=forwarded msg=%s sender=%s active=%s",
				msg.ID, msg.Sender, (*active).messageID)
			return true, nil
		}
		n.cfg.Logger.Warnf("swarm peer=deferred (buffer full) msg=%s active=%s",
			msg.ID, (*active).messageID)
		return false, nil
	}

	n.markDispatched(msg.ID)
	return true, nil
}

func (n *Node) startTask(ctx context.Context, msg protocols.Message, sm swarm.SwarmMessage) (*activeTask, error) {
	n.cfg.Logger.Importantf("swarm task=received message=%s", msg.ID)
	running := swarm.SwarmMessage{Content: fmt.Sprintf("Accepted task. Executing: %s", truncate(sm.Content, 100))}
	if _, err := n.cfg.Client.Send(ctx, n.spaceID, protocols.SendMessage{
		ContentType: "swarm",
		Content:     swarm.SwarmContent(running),
		Refs:        &protocols.Ref{Messages: []string{msg.ID}},
	}); err != nil {
		return nil, err
	}

	done := make(chan taskResult, 1)
	state := &activeTask{
		messageID: msg.ID,
		done:      done,
	}

	task := Task{
		MessageID: msg.ID,
		Sender:    msg.Sender,
		Content:   sm.Content,
		Targets:   sm.Targets,
		Meta:      sm.Meta,
		Refs:      msg.Refs,
	}

	go func() {
		result, err := n.cfg.OnTask(ctx, task)
		done <- taskResult{messageID: msg.ID, result: result, err: err}
	}()

	return state, nil
}

func (n *Node) completeTask(ctx context.Context, res taskResult) error {
	report := swarm.SwarmMessage{Content: res.result}
	if res.err != nil {
		report.Content = fmt.Sprintf("Error: %s\n\nPartial output:\n%s", res.err.Error(), res.result)
	}
	_, sendErr := n.cfg.Client.Send(ctx, n.spaceID, protocols.SendMessage{
		ContentType: "swarm",
		Content:     swarm.SwarmContent(report),
		Refs:        &protocols.Ref{Messages: []string{res.messageID}},
	})
	if res.err != nil {
		n.cfg.Logger.Warnf("swarm task=failed message=%s error=%s", res.messageID, res.err)
		if sendErr != nil {
			return sendErr
		}
		return res.err
	}
	if sendErr == nil {
		n.cfg.Logger.Importantf("swarm task=completed message=%s", res.messageID)
	}
	return sendErr
}

func (n *Node) markHistorical(messageID string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.historical[messageID] = struct{}{}
}

func (n *Node) isKnown(messageID string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, ok := n.historical[messageID]; ok {
		return true
	}
	_, ok := n.dispatched[messageID]
	return ok
}

func (n *Node) markDispatched(messageID string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.dispatched[messageID] = struct{}{}
}

func (n *Node) advanceWatermark(messageID string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.lastSeenID = messageID
}

func (n *Node) watermark() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.lastSeenID
}

func isTaskMessage(msg protocols.Message, sm swarm.SwarmMessage, nodeID string, active bool) bool {
	if kind := swarm.MessageKind(msg, sm); kind == "task_dispatch" {
		return true
	} else if kind != "" || active {
		return false
	}
	if len(msg.Refs.Nodes) > 0 {
		return slices.Contains(msg.Refs.Nodes, nodeID)
	}
	return len(msg.Refs.Messages) == 0
}

func peerMessageFromIOA(msg protocols.Message, sm swarm.SwarmMessage) PeerMessage {
	peer := PeerMessage{
		MessageID:   msg.ID,
		Sender:      msg.Sender,
		ContentType: msg.ContentType,
		Content:     sm.Content,
		RawContent:  cloneContent(msg.Content),
		Targets:     sm.Targets,
		Meta:        sm.Meta,
		Refs:        msg.Refs,
	}
	if len(peer.Targets) == 0 {
		peer.Targets = parseTargets(msg.Content["targets"])
	}
	if len(peer.Meta) == 0 {
		if meta, ok := msg.Content["meta"].(map[string]any); ok {
			peer.Meta = cloneContent(meta)
		}
	}
	return peer
}

func cloneContent(content map[string]any) map[string]any {
	if len(content) == 0 {
		return nil
	}
	out := make(map[string]any, len(content))
	for k, v := range content {
		out[k] = v
	}
	return out
}

func parseTargets(raw any) []string {
	if raw == nil {
		return nil
	}
	var targets []string
	if data, err := json.Marshal(raw); err == nil {
		_ = json.Unmarshal(data, &targets)
	}
	return targets
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}


