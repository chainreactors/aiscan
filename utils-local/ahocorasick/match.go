package ahocorasick

// MatchKind specifies the semantics of matching.
type MatchKind int

const (
	// LeftmostFirst implements leftmost-first semantics.
	// When multiple patterns can match at the same position,
	// the pattern that appears first in the pattern list wins.
	// This is Perl-compatible behavior and matches Go's regexp package.
	LeftmostFirst MatchKind = iota

	// LeftmostLongest implements leftmost-longest semantics.
	// When multiple patterns can match at the same position,
	// the longest pattern wins. This is POSIX-compatible behavior.
	LeftmostLongest
)

// Match represents a match found in the haystack.
type Match struct {
	// PatternID is the index of the pattern that matched.
	PatternID int

	// Start is the byte offset of the start of the match.
	Start int

	// End is the byte offset of the end of the match (exclusive).
	End int
}

// Len returns the length of the match in bytes.
func (m Match) Len() int {
	return m.End - m.Start
}

// PatternID is an identifier for a pattern in the automaton.
type PatternID uint32

// StateID is an identifier for a state in the automaton.
type StateID uint32
