//go:build windows

package tmux

import (
	"os/exec"
	"strconv"
)

func signalProcessGroup(pid int, hard bool) error {
	if pid <= 0 {
		return nil
	}
	args := []string{"/PID", strconv.Itoa(pid), "/T"}
	if hard {
		args = append(args, "/F")
	}
	return exec.Command("taskkill", args...).Run()
}
