package ahocorasick

const matchFlag uint32 = 1 << 31
const matchMask uint32 = matchFlag - 1

// DFA represents a fully compiled deterministic finite automaton.
type DFA struct {
	trans         []uint32
	matchIndex    []uint32
	matchData     []PatternID
	matchOverflow map[uint32][]PatternID
	byteClasses   *ByteClasses
	alphabetLen   int
	stride        int
	stride2       uint
	stateCount    int
	patternLens   []int
	matchKind     MatchKind
	startID       uint32
	startBytes    []byte
	patternBytes  [4]uint64
}

func nextPow2(n int) int {
	if n <= 1 {
		return 1
	}
	n--
	n |= n >> 1
	n |= n >> 2
	n |= n >> 4
	n |= n >> 8
	n |= n >> 16
	return n + 1
}

func log2(n int) uint {
	var r uint
	for n >>= 1; n > 0; n >>= 1 {
		r++
	}
	return r
}

func buildDFA(nfa *OptimizedNFA, patterns [][]byte, matchKind MatchKind) *DFA {
	numStates := len(nfa.states)
	alphabetLen := nfa.alphabetLen
	stride := nextPow2(alphabetLen)
	stride2 := log2(stride)

	d := &DFA{
		byteClasses: nfa.byteClasses,
		alphabetLen: alphabetLen,
		stride:      stride,
		stride2:     stride2,
		stateCount:  numStates,
		matchKind:   matchKind,
		startID:     uint32(nfa.startState) << stride2,
	}

	d.patternLens = make([]int, len(patterns))
	startByteSet := [256]bool{}
	for i, p := range patterns {
		d.patternLens[i] = len(p)
		if len(p) > 0 {
			startByteSet[p[0]] = true
		}
		for _, b := range p {
			d.patternBytes[b/64] |= 1 << (b % 64)
		}
	}

	for b := 0; b < 256; b++ {
		if startByteSet[b] {
			d.startBytes = append(d.startBytes, byte(b))
		}
	}

	isMatch := make([]bool, numStates)
	for si := 0; si < numStates; si++ {
		isMatch[si] = len(nfa.states[si].matches) > 0
	}

	tableSize := numStates * stride
	d.trans = make([]uint32, tableSize)

	for si := 0; si < numStates; si++ {
		rowOffset := si << stride2
		for class := 0; class < alphabetLen; class++ {
			next := resolveTransition(nfa, StateID(si), class)
			premultiplied := uint32(next) << stride2
			if isMatch[next] {
				premultiplied |= matchFlag
			}
			d.trans[rowOffset+class] = premultiplied
		}
	}

	var totalMatches int
	for si := 0; si < numStates; si++ {
		totalMatches += len(nfa.states[si].matches)
	}

	d.matchData = make([]PatternID, 0, totalMatches)
	d.matchIndex = make([]uint32, numStates)

	for si := 0; si < numStates; si++ {
		matches := nfa.states[si].matches
		if len(matches) == 0 {
			continue
		}

		offset := len(d.matchData)
		count := len(matches)
		d.matchData = append(d.matchData, matches...)

		if offset <= 0xFFFF && count <= 0xFFFF {
			d.matchIndex[si] = uint32(offset<<16) | uint32(count)
		} else {
			d.matchIndex[si] = 0xFFFFFFFF
			if d.matchOverflow == nil {
				d.matchOverflow = make(map[uint32][]PatternID)
			}
			d.matchOverflow[uint32(si)] = matches
		}
	}

	return d
}

func resolveTransition(nfa *OptimizedNFA, s StateID, class int) StateID {
	for {
		if next := nfa.states[s].trans[class]; next != 0 {
			return next
		}
		if s == nfa.startState {
			return nfa.startState
		}
		s = nfa.states[s].fail
	}
}

func (d *DFA) getMatches(sid uint32) []PatternID {
	idx := sid >> d.stride2
	packed := d.matchIndex[idx]
	if packed == 0 {
		return nil
	}
	if packed == 0xFFFFFFFF {
		return d.matchOverflow[idx]
	}
	offset := int(packed >> 16)
	count := int(packed & 0xFFFF)
	return d.matchData[offset : offset+count]
}

// MemoryUsage returns the approximate heap memory used by this DFA in bytes.
func (d *DFA) MemoryUsage() int {
	return len(d.trans)*4 +
		len(d.matchIndex)*4 +
		len(d.matchData)*4 + len(d.patternLens)*8
}
