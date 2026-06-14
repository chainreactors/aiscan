package ahocorasick

import "errors"

// Builder configures and builds an Aho-Corasick automaton.
type Builder struct {
	patterns    [][]byte
	matchKind   MatchKind
	ascii       bool // optimize for ASCII-only patterns
	prefilter   bool // enable prefilter optimization
	byteClasses bool // enable byte class compression
}

// NewBuilder creates a new Builder with default configuration.
// Default: LeftmostFirst semantics, byte classes enabled.
func NewBuilder() *Builder {
	return &Builder{
		matchKind:   LeftmostFirst,
		byteClasses: true,
		prefilter:   true,
	}
}

// SetMatchKind sets the match semantics.
// Default is LeftmostFirst (Perl-compatible).
func (b *Builder) SetMatchKind(kind MatchKind) *Builder {
	b.matchKind = kind
	return b
}

// SetASCII enables ASCII-only optimization.
// When true, patterns are assumed to contain only ASCII bytes.
func (b *Builder) SetASCII(ascii bool) *Builder {
	b.ascii = ascii
	return b
}

// SetPrefilter enables or disables prefilter optimization.
// When enabled, a prefilter may be used to accelerate search.
func (b *Builder) SetPrefilter(prefilter bool) *Builder {
	b.prefilter = prefilter
	return b
}

// SetByteClasses enables or disables byte class compression.
// When enabled, reduces memory usage by grouping equivalent bytes.
func (b *Builder) SetByteClasses(enabled bool) *Builder {
	b.byteClasses = enabled
	return b
}

// AddPattern adds a single pattern to the automaton.
func (b *Builder) AddPattern(pattern []byte) *Builder {
	b.patterns = append(b.patterns, pattern)
	return b
}

// AddPatterns adds multiple patterns to the automaton.
func (b *Builder) AddPatterns(patterns [][]byte) *Builder {
	b.patterns = append(b.patterns, patterns...)
	return b
}

// AddStrings adds multiple string patterns to the automaton.
func (b *Builder) AddStrings(patterns []string) *Builder {
	for _, p := range patterns {
		b.patterns = append(b.patterns, []byte(p))
	}
	return b
}

// Build constructs the Aho-Corasick automaton from the configured patterns.
// Returns an error if no patterns were added or if construction fails.
func (b *Builder) Build() (*Automaton, error) {
	if len(b.patterns) == 0 {
		return nil, errors.New("ahocorasick: no patterns provided")
	}

	for i, p := range b.patterns {
		if len(p) == 0 {
			return nil, errors.New("ahocorasick: empty pattern at index " + itoa(i))
		}
	}

	var bc *ByteClasses
	if b.byteClasses {
		bc = NewByteClasses(b.patterns)
	} else {
		bc = NewSingletonByteClasses()
	}

	nfa := buildOptimizedNFA(b.patterns, bc, b.matchKind)
	dfa := buildDFA(nfa, b.patterns, b.matchKind)

	return &Automaton{
		dfa:       dfa,
		patterns:  b.patterns,
		matchKind: b.matchKind,
	}, nil
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
