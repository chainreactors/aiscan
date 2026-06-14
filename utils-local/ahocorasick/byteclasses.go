package ahocorasick

// ByteClasses compresses the 256-byte alphabet into equivalence classes.
// Bytes that always transition to the same states can share a class.
// This reduces memory usage and improves cache utilization.
type ByteClasses struct {
	// classes maps each byte to its equivalence class.
	classes [256]byte

	// numClasses is the number of equivalence classes (1-256).
	numClasses int
}

// NewByteClasses creates ByteClasses from a set of patterns.
// It computes the minimal set of equivalence classes.
func NewByteClasses(patterns [][]byte) *ByteClasses {
	bc := &ByteClasses{}

	// Track which bytes appear in patterns
	used := [256]bool{}
	for _, p := range patterns {
		for _, b := range p {
			used[b] = true
		}
	}

	// Assign classes:
	// - Class 0: unused bytes (all transition to dead state)
	// - Class 1+: used bytes get individual classes
	class := byte(1)
	for i := 0; i < 256; i++ {
		if used[i] {
			bc.classes[i] = class
			class++
		}
		// unused bytes stay at class 0
	}

	bc.numClasses = int(class)
	return bc
}

// NewSingletonByteClasses creates ByteClasses where each byte is its own class.
// This is the identity mapping (no compression).
func NewSingletonByteClasses() *ByteClasses {
	bc := &ByteClasses{numClasses: 256}
	for i := 0; i < 256; i++ {
		bc.classes[i] = byte(i)
	}
	return bc
}

// Get returns the equivalence class for the given byte.
func (bc *ByteClasses) Get(b byte) int {
	return int(bc.classes[b])
}

// NumClasses returns the number of equivalence classes.
func (bc *ByteClasses) NumClasses() int {
	return bc.numClasses
}

// AlphabetSize returns the number of equivalence classes.
// This is the effective alphabet size for the automaton.
func (bc *ByteClasses) AlphabetSize() int {
	return bc.numClasses
}

// IsSingleton returns true if every byte is its own class.
func (bc *ByteClasses) IsSingleton() bool {
	return bc.numClasses == 256
}
