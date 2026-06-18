package tmux

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

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

func TestCreateAndCompletion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	mgr := NewManager()

	var completed Info
	done := make(chan struct{})
	mgr.SetOnDone(func(info Info) {
		completed = info
		close(done)
	})

	dir := t.TempDir()
	info, err := mgr.Create(dir, "printf done; sleep 0.05", "demo", 10*time.Second, nil, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if info.State != StateRunning {
		t.Fatalf("initial state = %s, want running", info.State)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("OnDone not called within 5s")
	}

	if completed.State != StateCompleted {
		t.Fatalf("completed state = %s, want completed", completed.State)
	}
	if completed.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", completed.ExitCode)
	}

	formatted := FormatCompletion(completed, mgr.PeekOrEmpty(info.ID, 20))
	if !strings.Contains(formatted, "done") {
		t.Fatalf("completion missing stdout: %v", formatted)
	}
}

func TestKillCascadesToGrandchild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	mgr := NewManager()
	dir := t.TempDir()

	info, err := mgr.Create(dir, "sh -c 'sleep 30 & echo CHILDPID=$! ; wait'", "kill-test", 30*time.Second, nil, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	var childPID int
	waitUntil(t, 3*time.Second, func() bool {
		output, _ := mgr.Peek(info.ID, 30)
		for _, line := range strings.Split(output, "\n") {
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

	if !processAlive(childPID) {
		t.Fatalf("grandchild %d already dead", childPID)
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

	waitUntil(t, 3*time.Second, func() bool {
		return !processAlive(childPID)
	})
}

func TestPeekReturnsTail(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	mgr := NewManager()
	dir := t.TempDir()
	info, err := mgr.Create(dir, "for i in 1 2 3 4 5; do echo line$i; done", "peek-test", 5*time.Second, nil, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	<-mgr.Done(info.ID)

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
	mgr := NewManager()
	defer mgr.Shutdown()

	dir := t.TempDir()
	info, err := mgr.Create(dir, "sleep 5", "wait-test", 30*time.Second, nil, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	start := time.Now()
	got, err := mgr.Wait(context.Background(), info.ID, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("Wait error: %v", err)
	}
	if time.Since(start) > 600*time.Millisecond {
		t.Fatalf("Wait took too long")
	}
	if got.State != StateRunning {
		t.Fatalf("state after short Wait = %s, want running", got.State)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	_, err = mgr.Wait(ctx, info.ID, 10*time.Second)
	if err == nil {
		t.Fatal("Wait did not return error after ctx cancel")
	}
}

func TestShutdownKillsRunning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	mgr := NewManager()
	dir := t.TempDir()

	info, err := mgr.Create(dir, "sleep 30", "shutdown-test", 60*time.Second, nil, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !processAlive(info.PID) {
		t.Fatal("process not alive after Create")
	}

	mgr.Shutdown()

	waitUntil(t, 3*time.Second, func() bool {
		return !processAlive(info.PID)
	})
}

func TestWriteInput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	mgr := NewManager()
	var completed Info
	ch := make(chan struct{})
	mgr.SetOnDone(func(info Info) {
		completed = info
		close(ch)
	})

	dir := t.TempDir()
	info, err := mgr.Create(dir, "head -1", "write-test", 10*time.Second, nil, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	if err := mgr.Write(info.ID, []byte("hello world\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("OnDone not called")
	}

	if completed.State != StateCompleted {
		t.Fatalf("state = %s, want completed", completed.State)
	}
	output := mgr.PeekOrEmpty(info.ID, 30)
	if !strings.Contains(output, "hello world") {
		t.Fatalf("expected 'hello world' in output, got: %q", output)
	}
}

func TestCreateCmd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	mgr := NewManager()
	dir := t.TempDir()

	info, err := mgr.CreateCmd(dir, "/bin/sh", []string{"-c", "echo from-createcmd"}, "cmd-test", 10*time.Second, nil, "")
	if err != nil {
		t.Fatalf("CreateCmd: %v", err)
	}
	<-mgr.Done(info.ID)

	output := mgr.PeekOrEmpty(info.ID, 30)
	if !strings.Contains(output, "from-createcmd") {
		t.Fatalf("expected 'from-createcmd', got: %q", output)
	}
}

func TestCreateCmdWithEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	mgr := NewManager()
	dir := t.TempDir()

	info, err := mgr.CreateCmd(dir, "/bin/sh", []string{"-c", "echo $TEST_MAGIC"}, "env-test", 10*time.Second, []string{"TEST_MAGIC=pty_works"}, "")
	if err != nil {
		t.Fatalf("CreateCmd: %v", err)
	}
	<-mgr.Done(info.ID)

	output := mgr.PeekOrEmpty(info.ID, 30)
	if !strings.Contains(output, "pty_works") {
		t.Fatalf("expected 'pty_works', got: %q", output)
	}
}

func TestDoneChannel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	mgr := NewManager()
	dir := t.TempDir()

	info, err := mgr.Create(dir, "echo fast", "done-test", 5*time.Second, nil, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	select {
	case <-mgr.Done(info.ID):
	case <-time.After(5 * time.Second):
		t.Fatal("Done channel not closed")
	}

	// Done on unknown ID returns closed channel
	select {
	case <-mgr.Done("nonexistent"):
	default:
		t.Fatal("Done for unknown ID should return closed channel")
	}
}

func TestPeekNew(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	mgr := NewManager()
	dir := t.TempDir()

	payload := strings.Repeat("x", 100)
	info, err := mgr.Create(dir, "printf '"+payload+"'", "peeknew-test", 10*time.Second, nil, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	<-mgr.Done(info.ID)

	out1, more1, err := mgr.PeekNew(info.ID, 50)
	if err != nil {
		t.Fatalf("PeekNew first: %v", err)
	}
	if out1 != strings.Repeat("x", 50) || !more1 {
		t.Fatalf("first = (%q, %t), want 50 x's + more", out1, more1)
	}

	out2, more2, err := mgr.PeekNew(info.ID, 50)
	if err != nil {
		t.Fatalf("PeekNew second: %v", err)
	}
	if out2 != strings.Repeat("x", 50) || more2 {
		t.Fatalf("second = (%q, %t), want 50 x's + no more", out2, more2)
	}

	out3, _, err := mgr.PeekNew(info.ID, 0)
	if err != nil {
		t.Fatalf("PeekNew third: %v", err)
	}
	if out3 != "" {
		t.Fatalf("third = %q, want empty", out3)
	}
}

func TestObserverPanicDoesNotCrash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	mgr := NewManager()
	called := make(chan struct{})
	mgr.SetOnDone(func(_ Info) {
		close(called)
		panic("boom")
	})

	dir := t.TempDir()
	mgr.Create(dir, "echo ok", "panic-test", 5*time.Second, nil, "")

	select {
	case <-called:
	case <-time.After(3 * time.Second):
		t.Fatal("OnDone not called")
	}
}

func TestOnDoneReceivesEvents(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	mgr := NewManager()
	var mu sync.Mutex
	var infos []Info
	mgr.SetOnDone(func(info Info) {
		mu.Lock()
		infos = append(infos, info)
		mu.Unlock()
	})

	dir := t.TempDir()
	mgr.Create(dir, "echo hello", "events-test", 5*time.Second, nil, "")
	time.Sleep(1 * time.Second)

	mu.Lock()
	defer mu.Unlock()
	if len(infos) != 1 {
		t.Fatalf("expected 1 OnDone call, got %d", len(infos))
	}
	if infos[0].State != StateCompleted {
		t.Fatalf("state = %s, want completed", infos[0].State)
	}
}

func TestNilOnDoneDoesNotPanic(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	mgr := NewManager()
	dir := t.TempDir()
	info, err := mgr.Create(dir, "echo done", "nil-test", 5*time.Second, nil, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	<-mgr.Done(info.ID)
	final, _ := mgr.Get(info.ID)
	if final.State != StateCompleted {
		t.Fatalf("state = %s, want completed", final.State)
	}
}

func TestExecCommandDirect(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	mgr := NewManager()
	dir := t.TempDir()

	info, err := mgr.CreateCmd(dir, "/bin/sh", []string{"-c", "echo direct"}, "", 5*time.Second, nil, "")
	if err != nil {
		t.Fatalf("CreateCmd: %v", err)
	}
	<-mgr.Done(info.ID)
	output := mgr.PeekOrEmpty(info.ID, 30)
	if !strings.Contains(output, "direct") {
		t.Fatalf("expected 'direct', got: %q", output)
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

func TestReadFromIndependentOffset(t *testing.T) {
	mgr := NewManager()
	buf := NewOutputBuffer(1024)
	id := "readfrom-test"
	s := &session{
		Info:   Info{ID: id, State: StateRunning},
		output: buf,
		done:   make(chan struct{}),
	}
	mgr.mu.Lock()
	mgr.sessions[id] = s
	mgr.mu.Unlock()

	buf.Write([]byte("line1\nline2\nline3\n"))

	// ReadFrom with offset 0 — returns everything
	text, off, err := mgr.ReadFrom(id, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "line1") || !strings.Contains(text, "line3") {
		t.Fatalf("ReadFrom(0) = %q, want all lines", text)
	}

	// ReadFrom with the returned offset — returns nothing (no new data)
	text2, off2, err := mgr.ReadFrom(id, off, 0)
	if err != nil {
		t.Fatal(err)
	}
	if text2 != "" {
		t.Fatalf("ReadFrom(%d) should be empty, got %q", off, text2)
	}

	// Write more, ReadFrom should only return new content
	buf.Write([]byte("line4\n"))
	text3, _, err := mgr.ReadFrom(id, off2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if text3 != "line4\n" {
		t.Fatalf("ReadFrom after new write = %q, want %q", text3, "line4\n")
	}

	// PeekNew should still work from its own offset (starts at 0)
	peekText, _, err := mgr.PeekNew(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(peekText, "line1") {
		t.Fatalf("PeekNew should return from offset 0, got %q", peekText)
	}
}

func TestPeekBytes(t *testing.T) {
	mgr := NewManager()
	buf := NewOutputBuffer(1024)
	id := "peekbytes-test"
	s := &session{
		Info:   Info{ID: id, State: StateRunning},
		output: buf,
		done:   make(chan struct{}),
	}
	mgr.mu.Lock()
	mgr.sessions[id] = s
	mgr.mu.Unlock()

	buf.Write([]byte("0123456789"))

	text, err := mgr.PeekBytes(id, 4)
	if err != nil {
		t.Fatal(err)
	}
	if text != "6789" {
		t.Fatalf("PeekBytes(4) = %q, want %q", text, "6789")
	}

	_, err = mgr.PeekBytes("nonexistent", 4)
	if err == nil {
		t.Fatal("PeekBytes on nonexistent session should error")
	}
}

func TestMonitorDeliversIncrementalOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only")
	}
	mgr := NewManager()

	info, err := mgr.Create("", "echo PART1; sleep 1; echo PART2; sleep 1; echo PART3", "", 10*time.Second, nil, "")
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var chunks []string

	mgr.Monitor(info.ID, 200*time.Millisecond, func(output string) {
		mu.Lock()
		chunks = append(chunks, output)
		mu.Unlock()
	})

	<-mgr.Done(info.ID)
	// Give final drain a moment to fire
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	allOutput := strings.Join(chunks, "")
	chunkCount := len(chunks)
	mu.Unlock()

	if !strings.Contains(allOutput, "PART1") {
		t.Fatalf("monitor output missing PART1, got: %q", allOutput)
	}
	if !strings.Contains(allOutput, "PART3") {
		t.Fatalf("monitor output missing PART3, got: %q", allOutput)
	}
	if chunkCount < 2 {
		t.Fatalf("expected multiple incremental chunks, got %d", chunkCount)
	}
	// Verify no duplication: each PART should appear exactly once across all chunks
	if strings.Count(allOutput, "PART1") != 1 {
		t.Fatalf("PART1 duplicated in monitor output: %q", allOutput)
	}
	if strings.Count(allOutput, "PART3") != 1 {
		t.Fatalf("PART3 duplicated in monitor output: %q", allOutput)
	}
}

func TestMonitorStopsOnSessionEnd(t *testing.T) {
	mgr := NewManager()
	buf := NewOutputBuffer(1024)
	id := "monitor-stop"
	done := make(chan struct{})
	s := &session{
		Info:   Info{ID: id, State: StateRunning},
		output: buf,
		done:   done,
	}
	mgr.mu.Lock()
	mgr.sessions[id] = s
	mgr.mu.Unlock()

	var pushCount int32
	var mu sync.Mutex

	mgr.Monitor(id, 50*time.Millisecond, func(output string) {
		mu.Lock()
		pushCount++
		mu.Unlock()
	})

	buf.Write([]byte("data"))
	time.Sleep(150 * time.Millisecond) // let a few ticks fire

	// Close the session
	close(done)
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	countAtStop := pushCount
	mu.Unlock()

	// Write more after done — should not be delivered
	buf.Write([]byte("after-close"))
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	countAfter := pushCount
	mu.Unlock()

	if countAtStop == 0 {
		t.Fatal("monitor should have delivered at least one push")
	}
	if countAfter != countAtStop {
		t.Fatalf("monitor delivered pushes after session done: before=%d after=%d", countAtStop, countAfter)
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
