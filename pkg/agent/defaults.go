package agent

import "github.com/chainreactors/aiscan/pkg/agent/truncate"

const (
	DefaultMaxResultSize         = truncate.DefaultMaxBytes
	DefaultMaxRetries            = 9
	DefaultTokenBudgetWarningPct = 80
	DefaultInboxCapacity         = 64
	SubInboxCapacity             = 16
	DefaultMaxParallelTools      = 16
	// DefaultPersistMaxTurns bounds persist mode when no explicit cap is set,
	// preventing a model that never calls finish from looping forever.
	DefaultPersistMaxTurns = 50
	// DefaultMaxTokens caps model output per turn. The provider-level fallback
	// is only 4096, which a large tool call (e.g. dumping many records inline)
	// easily exhausts; the response then truncates mid-JSON and corrupts the
	// tool-call arguments, which poisons every subsequent turn's request. 8192
	// gives 2x headroom while staying within supported models' output ceilings.
	DefaultMaxTokens = 8192
)
