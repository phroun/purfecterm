//go:build windows

package cli

// handleSIGWINCH is a no-op on Windows.
// Windows doesn't have SIGWINCH; terminal resize must be handled differently.
func (t *Terminal) handleSIGWINCH() {
	// On Windows, terminal resize detection would need to use Windows Console API
	// or polling. For now, this is a no-op stub to allow compilation.
	<-t.done
}
