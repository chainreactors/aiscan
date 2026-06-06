package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/chainreactors/aiscan/pkg/agent"
)

type eventsFileSubscriber struct {
	mu   sync.Mutex
	file *os.File
}

func newEventsFileSubscriber(path string) (*eventsFileSubscriber, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open events file %s: %w", path, err)
	}
	return &eventsFileSubscriber{file: f}, nil
}

func (w *eventsFileSubscriber) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
}

func (w *eventsFileSubscriber) HandleEvent(event agent.Event) {
	line, err := json.Marshal(agent.SerializableEvent(event))
	if err != nil {
		return
	}
	line = append(line, '\n')
	w.mu.Lock()
	defer w.mu.Unlock()
	_, _ = w.file.Write(line)
	_ = w.file.Sync()
}
