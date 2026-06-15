//go:build !unix

package runner

import "time"

func readPendingTerminalBytes(_ time.Duration) string {
	return ""
}
