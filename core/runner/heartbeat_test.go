package runner

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chainreactors/aiscan/cmd/ioaserve"
	cfg "github.com/chainreactors/aiscan/core/config"
	"github.com/chainreactors/aiscan/pkg/agent"
	"github.com/chainreactors/aiscan/pkg/agent/inbox"
	"github.com/chainreactors/aiscan/pkg/telemetry"
	ioaclient "github.com/chainreactors/ioa/client"
	"github.com/chainreactors/ioa/protocols"
)

func TestScheduleHeartbeatAddsInboxLoop(t *testing.T) {
	scheduler := agent.NewLoopScheduler(inbox.NewBuffered(4), telemetry.NopLogger())
	defer scheduler.Stop()

	opt := &cfg.Option{}
	opt.Heartbeat = 5
	opt.Space = "case-1"
	opt.IOANodeName = "coordinator"
	opt.Prompt = "coordinate scan workers"
	opt.Inputs = []string{"10.0.0.1", "https://example.test"}

	initialTask := "coordinate scan workers\n\nTargets:\n- 10.0.0.1\n- https://example.test"
	client := &fakeHeartbeatClient{
		nodeID: "node-coordinator",
		space: protocols.SpaceInfo{
			ID:           "space-1",
			Name:         "case-1",
			MessageCount: 1,
			Nodes: []protocols.Node{{
				ID:   "node-worker",
				Name: "worker",
				Meta: map[string]any{"role": "scan"},
			}},
		},
		messages: []protocols.Message{{
			ID:        "msg-1",
			Sender:    "node-worker",
			CreatedAt: "2026-06-12T00:00:00Z",
			Content: map[string]any{
				"kind":    "task_dispatch",
				"content": "scan 10.0.0.1",
			},
			Refs: protocols.Ref{Nodes: []string{"node-coordinator"}},
		}},
	}
	if err := scheduleHeartbeat(context.Background(), scheduler, opt, initialTask, client); err != nil {
		t.Fatalf("scheduleHeartbeat() error = %v", err)
	}

	loops := scheduler.List()
	if len(loops) != 1 {
		t.Fatalf("loops = %d, want 1: %#v", len(loops), loops)
	}
	got := loops[0]
	if got.Name != "heartbeat" {
		t.Fatalf("loop name = %q, want heartbeat", got.Name)
	}
	if got.Interval != 5*time.Minute {
		t.Fatalf("interval = %s, want 5m", got.Interval)
	}
	if got.Mode != agent.ModeInbox {
		t.Fatalf("mode = %d, want ModeInbox", got.Mode)
	}
	for _, want := range []string{
		"Heartbeat wake-up",
		"Space:",
		"- name: case-1",
		"Current node:",
		"- name: coordinator",
		"Initial task:",
		"coordinate scan workers",
		"10.0.0.1",
		"https://example.test",
		"Runtime reads IOA SpaceInfo",
	} {
		if !strings.Contains(got.Prompt, want) {
			t.Fatalf("heartbeat prompt missing %q:\n%s", want, got.Prompt)
		}
	}

	prompt, err := heartbeatPrompt(context.Background(), opt, initialTask, client)
	if err != nil {
		t.Fatalf("heartbeatPrompt() error = %v", err)
	}
	if client.spaceName != "case-1" {
		t.Fatalf("spaceName = %q, want case-1", client.spaceName)
	}
	if client.readSpaceID != "space-1" {
		t.Fatalf("readSpaceID = %q, want space-1", client.readSpaceID)
	}
	if !client.readOpts.All {
		t.Fatal("heartbeat read should use All=true")
	}
	if client.readOpts.Limit != heartbeatRecentMessageLimit {
		t.Fatalf("heartbeat read limit = %d, want %d", client.readOpts.Limit, heartbeatRecentMessageLimit)
	}
	for _, want := range []string{
		"- id: space-1",
		"- id: node-coordinator",
		"Space nodes:",
		"worker (node-worker)",
		"Recent messages (latest 50):",
		"msg-1",
		"task_dispatch",
		"scan 10.0.0.1",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("dynamic heartbeat prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestScheduleHeartbeatDisabledAndInvalidValues(t *testing.T) {
	if err := scheduleHeartbeat(context.Background(), nil, &cfg.Option{}, "", nil); err != nil {
		t.Fatalf("disabled heartbeat should not require scheduler: %v", err)
	}
	if err := scheduleHeartbeat(context.Background(), nil, &cfg.Option{AgentOptions: cfg.AgentOptions{Heartbeat: 1}}, "", nil); err == nil {
		t.Fatal("enabled heartbeat with nil scheduler should fail")
	}
	scheduler := agent.NewLoopScheduler(inbox.NewBuffered(1), telemetry.NopLogger())
	defer scheduler.Stop()
	if err := scheduleHeartbeat(
		context.Background(),
		scheduler,
		&cfg.Option{AgentOptions: cfg.AgentOptions{Heartbeat: 1}},
		"",
		nil,
	); err == nil {
		t.Fatal("enabled heartbeat with nil IOA client should fail")
	}
	if err := scheduleHeartbeat(
		context.Background(),
		scheduler,
		&cfg.Option{AgentOptions: cfg.AgentOptions{Heartbeat: -1}},
		"",
		nil,
	); err == nil {
		t.Fatal("negative heartbeat should fail")
	}
}

func TestHeartbeatPromptReadsRealIOAServer(t *testing.T) {
	addr := freeTCPAddr(t)
	baseURL := "http://" + addr

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- ioaserve.RunServe(ctx, ioaserve.Config{URL: baseURL, DB: ""}, telemetry.NopLogger())
	}()
	waitForIOAReady(t, baseURL)

	client, err := ioaclient.NewClient(baseURL, "")
	if err != nil {
		cancel()
		t.Fatalf("NewClient(coordinator) error = %v", err)
	}
	if _, err := client.RegisterNode(ctx, "coordinator", "coordinates workers", map[string]any{"client": "test"}); err != nil {
		cancel()
		t.Fatalf("RegisterNode(coordinator) error = %v", err)
	}
	if _, err := client.Space(ctx, "runtime-heartbeat", "coordinator space"); err != nil {
		cancel()
		t.Fatalf("Space(coordinator) error = %v", err)
	}

	worker, err := ioaclient.NewClient(baseURL, "")
	if err != nil {
		cancel()
		t.Fatalf("NewClient(worker) error = %v", err)
	}
	if _, err := worker.RegisterNode(ctx, "worker-1", "scanner", map[string]any{"skill": "scan"}); err != nil {
		cancel()
		t.Fatalf("RegisterNode(worker) error = %v", err)
	}
	info, err := worker.Space(ctx, "runtime-heartbeat", "worker space")
	if err != nil {
		cancel()
		t.Fatalf("Space(worker) error = %v", err)
	}
	if _, err := worker.Send(ctx, info.ID, protocols.SendMessage{
		Content: map[string]any{
			"kind":    "loot",
			"content": "worker found http://10.0.0.1:8080",
		},
	}); err != nil {
		cancel()
		t.Fatalf("Send(worker) error = %v", err)
	}

	opt := &cfg.Option{}
	opt.Space = "runtime-heartbeat"
	opt.IOANodeName = "coordinator"
	prompt, err := heartbeatPrompt(ctx, opt, "coordinate workers", client)
	if err != nil {
		cancel()
		t.Fatalf("heartbeatPrompt() error = %v", err)
	}
	for _, want := range []string{
		"Space:",
		"- name: runtime-heartbeat",
		"Current node:",
		"- name: coordinator",
		"Space nodes:",
		"worker-1",
		"Recent messages (latest 50):",
		"worker found http://10.0.0.1:8080",
		"coordinate workers",
	} {
		if !strings.Contains(prompt, want) {
			cancel()
			t.Fatalf("real IOA heartbeat prompt missing %q:\n%s", want, prompt)
		}
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("IOA server error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("IOA server did not stop")
	}
}

func freeTCPAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen free tcp addr: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close free tcp addr listener: %v", err)
	}
	return addr
}

func waitForIOAReady(t *testing.T, baseURL string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/ready")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("IOA server not ready at %s", baseURL)
}

type fakeHeartbeatClient struct {
	nodeID      string
	space       protocols.SpaceInfo
	messages    []protocols.Message
	spaceName   string
	readSpaceID string
	readOpts    protocols.ReadOptions
}

func (c *fakeHeartbeatClient) NodeID() string { return c.nodeID }

func (c *fakeHeartbeatClient) RegisterNode(context.Context, string, string, map[string]any) (protocols.Node, error) {
	return protocols.Node{}, fmt.Errorf("not implemented")
}

func (c *fakeHeartbeatClient) Space(_ context.Context, name, _ string, _ ...string) (protocols.SpaceInfo, error) {
	c.spaceName = name
	return c.space, nil
}

func (c *fakeHeartbeatClient) Send(context.Context, string, protocols.SendMessage) (protocols.Message, error) {
	return protocols.Message{}, fmt.Errorf("not implemented")
}

func (c *fakeHeartbeatClient) Read(_ context.Context, spaceID string, opts protocols.ReadOptions) ([]protocols.Message, error) {
	c.readSpaceID = spaceID
	c.readOpts = opts
	return c.messages, nil
}
