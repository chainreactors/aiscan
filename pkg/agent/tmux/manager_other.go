//go:build !unix && !windows

package tmux

func signalProcessGroup(_ int, _ bool) error {
	return nil
}
