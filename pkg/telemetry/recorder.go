package telemetry

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chainreactors/aiscan/pkg/util"
)

type Recorder interface {
	Logger
	Inc(name string, labels map[string]string, delta int)
	ObserveDuration(name string, labels map[string]string, duration time.Duration)
	SetGauge(name string, labels map[string]string, value int)
	Event(ctx context.Context, event Event)
	Snapshot() Snapshot
}

type Event struct {
	Name      string
	Labels    map[string]string
	Message   string
	Timestamp time.Time
}

type Snapshot struct {
	Counters  map[string]int
	Gauges    map[string]int
	Durations map[string][]time.Duration
	Events    []Event
}

type MemoryRecorder struct {
	logger Logger

	mu        sync.Mutex
	counters  map[string]int
	gauges    map[string]int
	durations map[string][]time.Duration
	events    []Event
}

func NewRecorder(logger Logger) *MemoryRecorder {
	if logger == nil {
		logger = NopLogger()
	}
	return &MemoryRecorder{
		logger:    logger,
		counters:  make(map[string]int),
		gauges:    make(map[string]int),
		durations: make(map[string][]time.Duration),
	}
}

func NoopRecorder() Recorder {
	return noopRecorder{}
}

func (r *MemoryRecorder) Debugf(format string, args ...any) {
	r.logger.Debugf(format, args...)
}

func (r *MemoryRecorder) Infof(format string, args ...any) {
	r.logger.Infof(format, args...)
}

func (r *MemoryRecorder) Warnf(format string, args ...any) {
	r.logger.Warnf(format, args...)
}

func (r *MemoryRecorder) Errorf(format string, args ...any) {
	r.logger.Errorf(format, args...)
}

func (r *MemoryRecorder) Importantf(format string, args ...any) {
	r.logger.Importantf(format, args...)
}

func (r *MemoryRecorder) Inc(name string, labels map[string]string, delta int) {
	if delta == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters[metricKey(name, labels)] += delta
}

func (r *MemoryRecorder) ObserveDuration(name string, labels map[string]string, duration time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := metricKey(name, labels)
	r.durations[key] = append(r.durations[key], duration)
}

func (r *MemoryRecorder) SetGauge(name string, labels map[string]string, value int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gauges[metricKey(name, labels)] = value
}

func (r *MemoryRecorder) Event(_ context.Context, event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	event.Labels = util.CloneMap(event.Labels)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *MemoryRecorder) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	return Snapshot{
		Counters:  util.CloneMap(r.counters),
		Gauges:    util.CloneMap(r.gauges),
		Durations: cloneDurations(r.durations),
		Events:    cloneEvents(r.events),
	}
}

type noopRecorder struct{}

func (noopRecorder) Debugf(string, ...any)     {}
func (noopRecorder) Infof(string, ...any)      {}
func (noopRecorder) Warnf(string, ...any)      {}
func (noopRecorder) Errorf(string, ...any)     {}
func (noopRecorder) Importantf(string, ...any) {}
func (noopRecorder) Inc(string, map[string]string, int) {
}
func (noopRecorder) ObserveDuration(string, map[string]string, time.Duration) {
}
func (noopRecorder) SetGauge(string, map[string]string, int) {
}
func (noopRecorder) Event(context.Context, Event) {
}
func (noopRecorder) Snapshot() Snapshot {
	return Snapshot{
		Counters:  make(map[string]int),
		Gauges:    make(map[string]int),
		Durations: make(map[string][]time.Duration),
	}
}

func metricKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		if key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys)+1)
	parts = append(parts, name)
	for _, key := range keys {
		parts = append(parts, key+"="+labels[key])
	}
	return strings.Join(parts, "|")
}

func cloneDurations(values map[string][]time.Duration) map[string][]time.Duration {
	out := make(map[string][]time.Duration, len(values))
	for key, value := range values {
		out[key] = append([]time.Duration(nil), value...)
	}
	return out
}

func cloneEvents(events []Event) []Event {
	out := append([]Event(nil), events...)
	for i := range out {
		out[i].Labels = util.CloneMap(out[i].Labels)
	}
	return out
}
