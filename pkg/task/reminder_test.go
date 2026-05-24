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
	"testing"
	"time"
)

func TestPeekSinceIncremental(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	dir := t.TempDir()
	mgr := NewManager(filepath.Join(dir, "tasks"))

	fn := func(ctx context.Context, out io.Writer) error {
		fmt.Fprintln(out, "line1")
		fmt.Fprintln(out, "line2")
		time.Sleep(200 * time.Millisecond)
		fmt.Fprintln(out, "line3")
		fmt.Fprintln(out, "line4")
		return nil
	}
	info, err := mgr.SpawnInProcess("peek-since", "peek-since-cmd", 10*time.Second, fn)
	if err != nil {
		t.Fatalf("SpawnInProcess: %v", err)
	}

	waitUntil(t, 2*time.Second, func() bool {
		data, _ := os.ReadFile(info.StdoutFile)
		return strings.Contains(string(data), "line2")
	})

	out1, off1, err := mgr.PeekSince(info.ID, 0)
	if err != nil {
		t.Fatalf("PeekSince(0): %v", err)
	}
	if !strings.Contains(out1, "line1") || !strings.Contains(out1, "line2") {
		t.Fatalf("PeekSince(0) = %q, want line1+line2", out1)
	}
	if off1 == 0 {
		t.Fatal("offset should advance past 0")
	}

	waitUntil(t, 2*time.Second, func() bool {
		final, _ := mgr.Get(info.ID)
		return final.State == StateCompleted
	})

	out2, off2, err := mgr.PeekSince(info.ID, off1)
	if err != nil {
		t.Fatalf("PeekSince(%d): %v", off1, err)
	}
	if strings.Contains(out2, "line1") || strings.Contains(out2, "line2") {
		t.Fatalf("PeekSince(%d) should not contain old lines: %q", off1, out2)
	}
	if !strings.Contains(out2, "line3") || !strings.Contains(out2, "line4") {
		t.Fatalf("PeekSince(%d) = %q, want line3+line4", off1, out2)
	}
	if off2 <= off1 {
		t.Fatalf("offset did not advance: %d <= %d", off2, off1)
	}

	out3, off3, err := mgr.PeekSince(info.ID, off2)
	if err != nil {
		t.Fatalf("PeekSince(%d): %v", off2, err)
	}
	if out3 != "" {
		t.Fatalf("PeekSince(%d) = %q, want empty", off2, out3)
	}
	if off3 != off2 {
		t.Fatalf("offset changed on empty read: %d != %d", off3, off2)
	}
}

func TestPeekSinceLimitPagesWithoutSkipping(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(filepath.Join(dir, "tasks"))

	payload := strings.Repeat("a", 12)
	fn := func(ctx context.Context, out io.Writer) error {
		_, _ = io.WriteString(out, payload)
		return nil
	}
	info, err := mgr.SpawnInProcess("peek-limit", "peek-limit-cmd", 10*time.Second, fn)
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

	out1, off1, more1, err := mgr.PeekSinceLimit(info.ID, 0, 5)
	if err != nil {
		t.Fatalf("PeekSinceLimit first: %v", err)
	}
	if out1 != strings.Repeat("a", 5) || off1 != 5 || !more1 {
		t.Fatalf("first page = (%q, %d, %t), want 5 bytes, offset 5, more", out1, off1, more1)
	}

	out2, off2, more2, err := mgr.PeekSinceLimit(info.ID, off1, 5)
	if err != nil {
		t.Fatalf("PeekSinceLimit second: %v", err)
	}
	if out2 != strings.Repeat("a", 5) || off2 != 10 || !more2 {
		t.Fatalf("second page = (%q, %d, %t), want 5 bytes, offset 10, more", out2, off2, more2)
	}

	out3, off3, more3, err := mgr.PeekSinceLimit(info.ID, off2, 5)
	if err != nil {
		t.Fatalf("PeekSinceLimit third: %v", err)
	}
	if out3 != strings.Repeat("a", 2) || off3 != 12 || more3 {
		t.Fatalf("third page = (%q, %d, %t), want 2 bytes, offset 12, no more", out3, off3, more3)
	}
}

func TestPeekSinceUnknownTask(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(filepath.Join(dir, "tasks"))

	_, _, err := mgr.PeekSince("nonexistent", 0)
	if err == nil {
		t.Fatal("expected error for unknown task")
	}
}

type testReminderCollector struct {
	mu   sync.Mutex
	msgs []string
	ch   chan struct{}
}

func newTestReminderCollector() *testReminderCollector {
	return &testReminderCollector{ch: make(chan struct{}, 16)}
}

func (c *testReminderCollector) handler() ReminderFunc {
	return func(content string) {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.msgs = append(c.msgs, content)
		select {
		case c.ch <- struct{}{}:
		default:
		}
	}
}

func (c *testReminderCollector) waitOne(t *testing.T, timeout time.Duration) string {
	t.Helper()
	select {
	case <-c.ch:
	case <-time.After(timeout):
		t.Fatalf("no reminder received within %s", timeout)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.msgs[len(c.msgs)-1]
}

func (c *testReminderCollector) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.msgs)
}

func TestReminderPushesWhenTasksRunning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	dir := t.TempDir()
	mgr := NewManager(filepath.Join(dir, "tasks"))
	col := newTestReminderCollector()

	started := make(chan struct{})
	fn := func(ctx context.Context, out io.Writer) error {
		close(started)
		<-ctx.Done()
		return nil
	}
	_, err := mgr.SpawnInProcess("reminder-test", "long-task", 30*time.Second, fn)
	if err != nil {
		t.Fatalf("SpawnInProcess: %v", err)
	}
	<-started

	mgr.StartReminder(200*time.Millisecond, col.handler())
	defer mgr.Shutdown()

	content := col.waitOne(t, 3*time.Second)
	if !strings.Contains(content, "<task_reminder>") {
		t.Fatalf("reminder content missing tag: %q", content)
	}
	if !strings.Contains(content, "peek_new") {
		t.Fatalf("reminder content missing peek_new instruction: %q", content)
	}
}

func TestReminderSkipsWhenNoTasks(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(filepath.Join(dir, "tasks"))
	col := newTestReminderCollector()

	mgr.StartReminder(100*time.Millisecond, col.handler())
	defer mgr.StopReminder()

	time.Sleep(500 * time.Millisecond)

	if count := col.count(); count > 0 {
		t.Fatalf("expected no reminders with no tasks, got %d", count)
	}
}

func TestReminderStopsCleanly(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(filepath.Join(dir, "tasks"))
	col := newTestReminderCollector()

	mgr.StartReminder(100*time.Millisecond, col.handler())
	done := make(chan struct{})
	go func() {
		mgr.StopReminder()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("StopReminder hung")
	}
}

func TestReminderDoubleStartNoop(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(filepath.Join(dir, "tasks"))
	col := newTestReminderCollector()

	mgr.StartReminder(1*time.Second, col.handler())
	mgr.StartReminder(1*time.Second, col.handler()) // should be no-op
	mgr.StopReminder()
}

func TestFormatReminder(t *testing.T) {
	running := []Info{
		{ID: "abc123", Name: "gogo", StartedAt: time.Now().Add(-2 * time.Minute)},
		{ID: "def456", Name: "spray", StartedAt: time.Now().Add(-45 * time.Second)},
	}
	result := formatReminder(running)
	if !strings.Contains(result, "<task_reminder>") {
		t.Fatalf("missing opening tag: %q", result)
	}
	if !strings.Contains(result, "</task_reminder>") {
		t.Fatalf("missing closing tag: %q", result)
	}
	if !strings.Contains(result, "2 running") {
		t.Fatalf("missing task count: %q", result)
	}
	if !strings.Contains(result, "abc123") || !strings.Contains(result, "def456") {
		t.Fatalf("missing task IDs: %q", result)
	}
}
