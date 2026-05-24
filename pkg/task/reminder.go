package task

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type Reminder struct {
	manager    *Manager
	interval   time.Duration
	onReminder ReminderFunc
	cancel     context.CancelFunc
	done       chan struct{}
}

func newReminder(mgr *Manager, interval time.Duration, fn ReminderFunc) *Reminder {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &Reminder{
		manager:    mgr,
		interval:   interval,
		onReminder: fn,
		done:       make(chan struct{}),
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
	var running []Info
	for _, t := range r.manager.tasks {
		if t.State == StateRunning {
			running = append(running, t.Info)
		}
	}
	r.manager.mu.Unlock()

	if len(running) == 0 {
		return
	}

	content := formatReminder(running)
	fn := r.onReminder
	go func() {
		defer func() { _ = recover() }()
		fn(content)
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
