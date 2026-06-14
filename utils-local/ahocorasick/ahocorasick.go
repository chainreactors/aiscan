// Package ahocorasick implements the Aho-Corasick algorithm for
// efficient multi-pattern string matching.
//
// The Aho-Corasick algorithm allows searching for multiple patterns
// simultaneously in O(n + m + z) time, where n is the input length,
// m is the total pattern length, and z is the number of matches.
//
// Vendored from github.com/coregx/ahocorasick v0.2.1 (MIT License)
// with Go 1.22+ syntax downgraded for Go 1.20 compatibility.
package ahocorasick

const Version = "0.2.1"
