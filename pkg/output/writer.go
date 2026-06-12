package output

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// TimelineWriter writes both scan records and agent events to a single
// JSONL file. It is the unified write-side counterpart to ParseTimelineFile.
type TimelineWriter struct {
	mu   sync.Mutex
	file *os.File
}

func NewTimelineWriter(path string) (*TimelineWriter, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open timeline file %s: %w", path, err)
	}
	return &TimelineWriter{file: f}, nil
}

func (w *TimelineWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

// WriteJSON writes any JSON-marshalable value as one JSONL line.
// Used for both Record (scan) and Event (agent) — they share the file.
func (w *TimelineWriter) WriteJSON(v any) {
	line, err := json.Marshal(v)
	if err != nil {
		return
	}
	line = append(line, '\n')
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return
	}
	_, _ = w.file.Write(line)
}

// WriteRecord is a convenience alias for scan records.
func (w *TimelineWriter) WriteRecord(rec Record) {
	w.WriteJSON(rec)
}
