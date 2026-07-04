package webagent

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chainreactors/aiscan/pkg/commands"
	"github.com/chainreactors/utils/pty"
)

// TestNewPTYRouterReplaysFullBufferOnAttach guards the WithAttachBytes wiring in
// newPTYRouter. Re-attaching to a session must replay its whole retained buffer,
// not just the router's 64KB default tail — otherwise re-opening the always-on
// "main-repl" drops the head of the transcript and users see a truncated
// ("残缺") conversation instead of the full one.
func TestNewPTYRouterReplaysFullBufferOnAttach(t *testing.T) {
	reg := commands.NewRegistry()
	commands.BuildGroup("core", &commands.Deps{WorkDir: t.TempDir(), BashTimeout: 5}, reg)
	mgr := registryPTYManager(reg)
	if mgr == nil {
		t.Fatal("bash command did not expose tmux manager")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const head = "HEAD_MARKER_a1b2c3"
	const tail = "TAIL_MARKER_x9y8z7"
	// Push the head well past the 64KB default tail window so it only survives a
	// full-buffer replay, while staying far under the 2MB buffer cap so nothing
	// is legitimately dropped.
	filler := strings.Repeat("filler-line-0123456789abcdef\n", 6000) // ~170KB
	if len(filler) <= pty.DefaultAttachBytes {
		t.Fatalf("filler %d must exceed default attach cap %d", len(filler), pty.DefaultAttachBytes)
	}

	info, err := mgr.CreateFunc(ctx, "big-session", 5*time.Second, func(_ context.Context, w io.Writer) error {
		_, _ = io.WriteString(w, head+"\n")
		_, _ = io.WriteString(w, filler)
		_, _ = io.WriteString(w, tail+"\n")
		return nil
	})
	if err != nil {
		t.Fatalf("CreateFunc: %v", err)
	}
	if _, err := mgr.Wait(ctx, info.ID, 5*time.Second); err != nil {
		t.Fatalf("wait for session: %v", err)
	}

	router := newPTYRouter(reg, nil)

	var mu sync.Mutex
	var out bytes.Buffer
	var errFrame string
	send := func(f pty.Frame) {
		mu.Lock()
		defer mu.Unlock()
		switch f.Type {
		case pty.FrameOutput:
			out.Write(f.Data)
		case pty.FrameError:
			errFrame = f.Error
		}
	}

	// attachExisting emits FrameAttached and the full replay FrameOutput
	// synchronously before Handle returns; only the follow-up monitor is async.
	router.Handle(ctx, pty.Frame{
		Type:      pty.FrameAttach,
		StreamID:  "stream-1",
		SessionID: info.ID,
		Cols:      80,
		Rows:      24,
	}, send)

	// Stop the monitor goroutine, then snapshot what was replayed.
	cancel()
	mu.Lock()
	replayed := out.String()
	gotErr := errFrame
	mu.Unlock()

	if gotErr != "" {
		t.Fatalf("attach returned error frame: %s", gotErr)
	}
	if !strings.Contains(replayed, tail) {
		t.Fatalf("replay missing tail marker (replayed %d bytes)", len(replayed))
	}
	if !strings.Contains(replayed, head) {
		t.Fatalf("replay dropped head marker: only the last ~%d bytes came back (replayed %d bytes) — WithAttachBytes wiring missing?", pty.DefaultAttachBytes, len(replayed))
	}
}
