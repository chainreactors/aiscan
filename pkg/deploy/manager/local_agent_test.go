package manager

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestLocalAgentLifecycle exercises spawn → track → delete end-to-end using a
// stand-in "agent" binary that ignores its args and stays alive, so no real
// agent or LLM is needed.
func TestLocalAgentLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell stub")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-agent.sh")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec sleep 60\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := NewDeployManager(&memStore{}, nil, "tok", bin)
	m.ConfigureTunnel("http://127.0.0.1:9")

	view, err := m.LaunchLocal(context.Background())
	if err != nil {
		t.Fatalf("LaunchLocal: %v", err)
	}
	if view.PID == 0 || view.Name != "local-1" {
		t.Fatalf("want local-1 with a pid, got %+v", view)
	}
	if got := m.ListLocal(); len(got) != 1 || got[0].Name != view.Name {
		t.Fatalf("ListLocal = %+v, want the one launched agent", got)
	}
	if err := m.StopLocal(view.Name); err != nil {
		t.Fatalf("StopLocal: %v", err)
	}
	if got := m.ListLocal(); len(got) != 0 {
		t.Fatalf("after delete ListLocal = %+v, want empty", got)
	}
	if err := m.StopLocal("does-not-exist"); err == nil {
		t.Fatal("StopLocal(unknown) = nil, want error")
	}
}

// TestLaunchLocalRequiresHubURL guards the precondition that the hub loopback
// address is known (otherwise the child would have nothing to dial).
func TestLaunchLocalRequiresHubURL(t *testing.T) {
	m := NewDeployManager(&memStore{}, nil, "tok", "") // localURL never configured
	if _, err := m.LaunchLocal(context.Background()); err == nil {
		t.Fatal("LaunchLocal with no hub URL = nil error, want failure")
	}
}
