//go:build !windows

package cli

import (
	"os"
	"os/signal"
	"syscall"
)

// handleSIGWINCH listens for terminal resize signals
func (t *Terminal) handleSIGWINCH() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGWINCH)
	defer signal.Stop(sigChan)

	for {
		select {
		case <-sigChan:
			t.handleResize()
		case <-t.done:
			return
		}
	}
}
