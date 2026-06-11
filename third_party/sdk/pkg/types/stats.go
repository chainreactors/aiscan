package types

import "time"

// Stats is a compact, engine-neutral execution counter emitted by SDK engines
// through context callbacks.
type Stats struct {
	Engine   string
	Task     string
	Targets  int64
	Tasks    int64
	Requests int64
	Results  int64
	Errors   int64
	Duration time.Duration
}
