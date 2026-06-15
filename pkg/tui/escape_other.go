//go:build !unix

package tui

import "time"

func readPendingTerminalBytes(_ time.Duration) string {
	return ""
}
