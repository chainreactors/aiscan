//go:build race

package proxy

// raceEnabled is true when the test binary is built with -race. The mitm
// server-first fallback path is pathologically slow under the race detector
// (loopback interception starves for tens of seconds), so that one test skips
// under -race while still running in a normal `go test`.
const raceEnabled = true
