package ahocorasick

// OptimizedNFA represents an optimized Aho-Corasick automaton.
// Key optimizations:
// 1. Dense transitions: []StateID instead of map[byte]StateID
// 2. Precomputed root transitions: no failure link following for root
// 3. ByteClasses: reduced alphabet size
type OptimizedNFA struct {
	states      []optState
	byteClasses *ByteClasses
	alphabetLen int
	startState  StateID
	matchKind   MatchKind
	patternCount int
}

type optState struct {
	trans   []StateID
	fail    StateID
	matches []PatternID
	depth   int
}

func buildOptimizedNFA(patterns [][]byte, bc *ByteClasses, matchKind MatchKind) *OptimizedNFA {
	alphabetLen := bc.NumClasses()

	nfa := &OptimizedNFA{
		byteClasses:  bc,
		alphabetLen:  alphabetLen,
		startState:   0,
		matchKind:    matchKind,
		patternCount: len(patterns),
	}

	nfa.buildTrie(patterns)
	nfa.buildFailureLinks()

	if matchKind == LeftmostFirst || matchKind == LeftmostLongest {
		nfa.propagateMatches()
	}

	nfa.precomputeRootTransitions()

	return nfa
}

func (nfa *OptimizedNFA) buildTrie(patterns [][]byte) {
	nfa.states = append(nfa.states, optState{
		trans: make([]StateID, nfa.alphabetLen),
		fail:  0,
		depth: 0,
	})

	for patternID, pattern := range patterns {
		nfa.addPattern(pattern, PatternID(patternID))
	}
}

func (nfa *OptimizedNFA) addPattern(pattern []byte, patternID PatternID) {
	state := nfa.startState

	for _, b := range pattern {
		class := nfa.byteClasses.Get(b)

		if next := nfa.states[state].trans[class]; next != 0 {
			state = next
		} else {
			newState := StateID(len(nfa.states))
			nfa.states = append(nfa.states, optState{
				trans: make([]StateID, nfa.alphabetLen),
				fail:  0,
				depth: nfa.states[state].depth + 1,
			})
			nfa.states[state].trans[class] = newState
			state = newState
		}
	}

	nfa.states[state].matches = append(nfa.states[state].matches, patternID)
}

func (nfa *OptimizedNFA) buildFailureLinks() {
	queue := make([]StateID, 0, len(nfa.states))

	root := &nfa.states[nfa.startState]
	for class := 0; class < nfa.alphabetLen; class++ {
		if child := root.trans[class]; child != 0 {
			nfa.states[child].fail = nfa.startState
			queue = append(queue, child)
		}
	}

	for len(queue) > 0 {
		state := queue[0]
		queue = queue[1:]

		for class := 0; class < nfa.alphabetLen; class++ {
			child := nfa.states[state].trans[class]
			if child == 0 {
				continue
			}
			queue = append(queue, child)

			fail := nfa.states[state].fail
			for {
				if next := nfa.states[fail].trans[class]; next != 0 {
					nfa.states[child].fail = next
					break
				}
				if fail == nfa.startState {
					nfa.states[child].fail = nfa.startState
					break
				}
				fail = nfa.states[fail].fail
			}
		}
	}
}

func (nfa *OptimizedNFA) propagateMatches() {
	queue := make([]StateID, 0, len(nfa.states))

	root := &nfa.states[nfa.startState]
	for class := 0; class < nfa.alphabetLen; class++ {
		if child := root.trans[class]; child != 0 {
			queue = append(queue, child)
		}
	}

	for len(queue) > 0 {
		stateID := queue[0]
		queue = queue[1:]

		state := &nfa.states[stateID]

		for class := 0; class < nfa.alphabetLen; class++ {
			if child := state.trans[class]; child != 0 {
				queue = append(queue, child)
			}
		}

		if state.fail != nfa.startState {
			failMatches := nfa.states[state.fail].matches
			if len(failMatches) > 0 {
				state.matches = append(state.matches, failMatches...)
			}
		}
	}
}

func (nfa *OptimizedNFA) precomputeRootTransitions() {
	// Root state trans[class] == 0 means "stay at root".
	// No additional work needed; the DFA builder resolves this.
}
