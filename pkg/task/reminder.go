package task

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Reminder periodically pushes reminder messages to the Manager's sink
// when there are running background tasks. This nudges the agent to call
// `task peek_new` and check for new output.
type Reminder struct {
	manager  *Manager
	interval time.Duration
	cancel   context.CancelFunc
	done     chan struct{}
}

func newReminder(mgr *Manager, interval time.Duration) *Reminder {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &Reminder{
		manager:  mgr,
		interval: interval,
		done:     make(chan struct{}),
	}
}

func (r *Reminder) start() {
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	go r.run(ctx)
}

func (r *Reminder) stop() {
	if r.cancel != nil {
		r.cancel()
	}
	<-r.done
}

func (r *Reminder) run(ctx context.Context) {
	defer close(r.done)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.check()
		}
	}
}

func (r *Reminder) check() {
	r.manager.mu.Lock()
	sink := r.manager.sink
	var running []Info
	for _, t := range r.manager.tasks {
		if t.State == StateRunning {
			running = append(running, t.Info)
		}
	}
	r.manager.mu.Unlock()

	if len(running) == 0 || sink == nil {
		return
	}

	content := formatReminder(running)
	go func() {
		defer func() { _ = recover() }()
		sink.PushMessage("task", "reminder", content, nil)
	}()
}

func formatReminder(running []Info) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<task_reminder>\nYou have %d running background task(s). Use `task peek_new` with the task id to check for new output.\nRunning:", len(running))
	for _, info := range running {
		elapsed := time.Since(info.StartedAt).Round(time.Second)
		fmt.Fprintf(&sb, " [%s] %s (%s)", info.ID, info.Name, elapsed)
	}
	sb.WriteString("\n</task_reminder>")
	return sb.String()
}
