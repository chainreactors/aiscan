package task

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/chainreactors/aiscan/pkg/provider"
)

type testSink struct {
	mu   sync.Mutex
	msgs []provider.ChatMessage
	ch   chan struct{}
}

func newTestSink() *testSink {
	return &testSink{ch: make(chan struct{}, 8)}
}

func (s *testSink) Push(msg provider.ChatMessage) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, msg)
	select {
	case s.ch <- struct{}{}:
	default:
	}
	return true
}

func (s *testSink) waitOne(t *testing.T, timeout time.Duration) provider.ChatMessage {
	t.Helper()
	select {
	case <-s.ch:
	case <-time.After(timeout):
		t.Fatalf("no completion message received within %s", timeout)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.msgs[len(s.msgs)-1]
}

// rejectSink always returns false, simulating a full/closed inbox.
type rejectSink struct{}

func (rejectSink) Push(provider.ChatMessage) bool { return false }

type panicSink struct {
	called chan struct{}
}

func (s panicSink) Push(provider.ChatMessage) bool {
	if s.called != nil {
		close(s.called)
	}
	panic("boom")
}

type blockingSink struct {
	started chan struct{}
	release <-chan struct{}
}

func (s blockingSink) Push(provider.ChatMessage) bool {
	if s.started != nil {
		close(s.started)
	}
	<-s.release
	return true
}

// waitUntil polls until predicate returns true or timeout elapses. Used in
// place of fixed sleeps so tests stay fast and don't flake on slow CI.
func waitUntil(t *testing.T, timeout time.Duration, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !predicate() {
		if time.Now().After(deadline) {
			t.Fatalf("condition not met within %s", timeout)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSpawnCompletesAndNotifies(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	dir := t.TempDir()
	mgr := NewManager(filepath.Join(dir, "tasks"))
	sink := newTestSink()
	mgr.SetSink(sink)

	info, err := mgr.Spawn(dir, "printf done; sleep 0.05", "demo", 10*time.Second)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if info.State != StateRunning {
		t.Fatalf("initial state = %s, want running", info.State)
	}

	msg := sink.waitOne(t, 5*time.Second)
	if msg.Content == nil || !strings.Contains(*msg.Content, info.ID) {
		t.Fatalf("completion message missing id: %v", msg)
	}
	if !strings.Contains(*msg.Content, "done") {
		t.Fatalf("completion message missing stdout tail: %v", *msg.Content)
	}

	final, ok := mgr.Get(info.ID)
	if !ok {
		t.Fatal("task disappeared after completion")
	}
	if final.State != StateCompleted {
		t.Fatalf("final state = %s, want completed", final.State)
	}
	if final.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", final.ExitCode)
	}

	stdoutBytes, err := os.ReadFile(final.StdoutFile)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if !strings.Contains(string(stdoutBytes), "done") {
		t.Fatalf("stdout file missing 'done': %q", string(stdoutBytes))
	}

	signalPath := filepath.Join(filepath.Dir(final.StdoutFile), "signal")
	sigBytes, err := os.ReadFile(signalPath)
	if err != nil {
		t.Fatalf("read signal: %v", err)
	}
	if strings.TrimSpace(string(sigBytes)) != "0" {
		t.Fatalf("signal = %q, want 0", string(sigBytes))
	}
}

func TestKillCascadesToGrandchild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	dir := t.TempDir()
	mgr := NewManager(filepath.Join(dir, "tasks"))

	// Spawn a sleep, capture its PID via stdout, then kill via Manager and
	// verify the grandchild is also gone (kill -- -pgid should sweep it).
	script := "sh -c 'sleep 30 & echo CHILDPID=$! ; wait'"
	info, err := mgr.Spawn(dir, script, "kill-test", 30*time.Second)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Wait for the child PID to land in stdout.
	var childPID int
	waitUntil(t, 3*time.Second, func() bool {
		data, _ := os.ReadFile(info.StdoutFile)
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.HasPrefix(line, "CHILDPID=") {
				continue
			}
			pid := 0
			for _, c := range line[len("CHILDPID="):] {
				if c < '0' || c > '9' {
					break
				}
				pid = pid*10 + int(c-'0')
			}
			if pid > 0 {
				childPID = pid
				return true
			}
		}
		return false
	})

	if err := syscall.Kill(childPID, 0); err != nil {
		t.Fatalf("grandchild %d already dead before Kill: %v", childPID, err)
	}

	if err := mgr.Kill(info.ID); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	waitUntil(t, 5*time.Second, func() bool {
		final, _ := mgr.Get(info.ID)
		return final.State != StateRunning
	})
	final, _ := mgr.Get(info.ID)
	if final.State != StateKilled {
		t.Fatalf("state after Kill = %s, want killed", final.State)
	}
	signalPath := filepath.Join(filepath.Dir(final.StdoutFile), "signal")
	sigBytes, err := os.ReadFile(signalPath)
	if err != nil {
		t.Fatalf("read signal: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(sigBytes)), "killed:") {
		t.Fatalf("signal = %q, want killed marker", string(sigBytes))
	}

	// Grandchild should have been swept by the SIGTERM-to-process-group.
	waitUntil(t, 3*time.Second, func() bool {
		return syscall.Kill(childPID, 0) != nil
	})
}

func TestPeekReturnsTail(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	dir := t.TempDir()
	mgr := NewManager(filepath.Join(dir, "tasks"))
	info, err := mgr.Spawn(dir, "for i in 1 2 3 4 5; do echo line$i; done", "peek-test", 5*time.Second)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	waitUntil(t, 3*time.Second, func() bool {
		final, _ := mgr.Get(info.ID)
		return final.State == StateCompleted
	})

	out, err := mgr.Peek(info.ID, 3)
	if err != nil {
		t.Fatalf("Peek: %v", err)
	}
	want := "line3\nline4\nline5"
	if out != want {
		t.Fatalf("Peek = %q, want %q", out, want)
	}
}

func TestWaitRespectsTimeoutAndContext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	dir := t.TempDir()
	mgr := NewManager(filepath.Join(dir, "tasks"))
	defer mgr.Shutdown()

	info, err := mgr.Spawn(dir, "sleep 5", "wait-test", 30*time.Second)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// Wait with short timeout — should return with task still running.
	start := time.Now()
	got, err := mgr.Wait(context.Background(), info.ID, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 600*time.Millisecond {
		t.Fatalf("Wait returned after %s, expected ~200ms", elapsed)
	}
	if got.State != StateRunning {
		t.Fatalf("state after short Wait = %s, want running", got.State)
	}

	// Context cancellation unblocks Wait.
	ctx, cancel := context.WithCancel(context.Background())
	cancelDone := make(chan struct{})
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
		close(cancelDone)
	}()
	_, err = mgr.Wait(ctx, info.ID, 10*time.Second)
	if err == nil {
		t.Fatal("Wait did not return error after ctx cancel")
	}
	<-cancelDone
}

func TestShutdownKillsRunning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	dir := t.TempDir()
	mgr := NewManager(filepath.Join(dir, "tasks"))

	info, err := mgr.Spawn(dir, "sleep 30", "shutdown-test", 60*time.Second)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if syscall.Kill(info.PID, 0) != nil {
		t.Fatal("process not alive immediately after spawn")
	}

	mgr.Shutdown()

	waitUntil(t, 3*time.Second, func() bool {
		return syscall.Kill(info.PID, 0) != nil
	})
	final, _ := mgr.Get(info.ID)
	if final.State == StateRunning {
		t.Fatalf("state after Shutdown still running")
	}
}

func TestSpawnInProcessCompletesAndNotifies(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(filepath.Join(dir, "tasks"))
	sink := newTestSink()
	mgr.SetSink(sink)

	fn := func(ctx context.Context, out io.Writer) error {
		fmt.Fprintln(out, "step 1")
		fmt.Fprintln(out, "step 2")
		return nil
	}
	info, err := mgr.SpawnInProcess("in-proc", "fake-cmd arg1", 5*time.Second, fn)
	if err != nil {
		t.Fatalf("SpawnInProcess: %v", err)
	}

	msg := sink.waitOne(t, 3*time.Second)
	if msg.Content == nil || !strings.Contains(*msg.Content, info.ID) {
		t.Fatalf("completion msg missing id: %v", msg)
	}
	if !strings.Contains(*msg.Content, "step 2") {
		t.Fatalf("completion msg missing stdout: %v", *msg.Content)
	}

	final, _ := mgr.Get(info.ID)
	if final.State != StateCompleted {
		t.Fatalf("state = %s, want completed", final.State)
	}

	stdoutBytes, _ := os.ReadFile(final.StdoutFile)
	if !strings.Contains(string(stdoutBytes), "step 1") {
		t.Fatalf("stdout missing 'step 1': %q", string(stdoutBytes))
	}
}

func TestSpawnInProcessKillCancelsContext(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(filepath.Join(dir, "tasks"))

	started := make(chan struct{})
	fn := func(ctx context.Context, out io.Writer) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	info, err := mgr.SpawnInProcess("in-proc-kill", "blocker", 30*time.Second, fn)
	if err != nil {
		t.Fatalf("SpawnInProcess: %v", err)
	}
	<-started

	if err := mgr.Kill(info.ID); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	waitUntil(t, 3*time.Second, func() bool {
		final, _ := mgr.Get(info.ID)
		return final.State != StateRunning
	})
	final, _ := mgr.Get(info.ID)
	if final.State != StateKilled {
		t.Fatalf("state after Kill = %s, want killed", final.State)
	}
}

func TestCompletionToFullSinkDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(filepath.Join(dir, "tasks"))
	mgr.SetSink(rejectSink{})

	fn := func(ctx context.Context, out io.Writer) error {
		fmt.Fprintln(out, "done")
		return nil
	}
	info, err := mgr.SpawnInProcess("full-sink", "full-sink", 5*time.Second, fn)
	if err != nil {
		t.Fatalf("SpawnInProcess: %v", err)
	}
	final, err := mgr.Wait(context.Background(), info.ID, 3*time.Second)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if final.State != StateCompleted {
		t.Fatalf("state = %s, want completed", final.State)
	}
}

func TestCompletionToPanicSinkDoesNotPanic(t *testing.T) {
	called := make(chan struct{})
	sendCompletion(panicSink{called: called}, Info{
		ID:        "panic-sink",
		Name:      "panic-sink",
		StartedAt: time.Now(),
		EndedAt:   time.Now(),
		State:     StateCompleted,
	}, false, "")

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("panic sink was not called")
	}
}

func TestCompletionToBlockingSinkDoesNotBlock(t *testing.T) {
	release := make(chan struct{})
	defer close(release)

	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		sendCompletion(blockingSink{started: started, release: release}, Info{
			ID:        "blocking-sink",
			Name:      "blocking-sink",
			StartedAt: time.Now(),
			EndedAt:   time.Now(),
			State:     StateCompleted,
		}, false, "")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("sendCompletion blocked on sink.Push")
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("blocking sink was not called")
	}
}

func TestTailLines(t *testing.T) {
	got := tailLines("a\nb\n\n\nc\n", 2)
	if got != "b\nc" {
		t.Fatalf("tailLines = %q, want %q", got, "b\nc")
	}
	got = tailLines("a", 5)
	if got != "a" {
		t.Fatalf("tailLines short = %q", got)
	}
}

func TestLabelFromCommand(t *testing.T) {
	cases := map[string]string{
		"scan -i fjbdg.com.cn --mode quick": "scan",
		"/usr/bin/python3 foo.py":           "python3",
		"   ":                               "shell",
	}
	for in, want := range cases {
		if got := labelFromCommand(in); got != want {
			t.Errorf("labelFromCommand(%q) = %q, want %q", in, got, want)
		}
	}
}
