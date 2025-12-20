package purfecterm

import "sync"

// TerminalCapabilities holds terminal capabilities that can be associated with a channel.
// This allows different channels (e.g., system stdout vs gui_console) to report
// their own capabilities independently.
type TerminalCapabilities struct {
	mu sync.RWMutex

	// Terminal type and detection
	TermType     string // e.g., "xterm-256color", "gui-console"
	IsTerminal   bool   // true if this is an interactive terminal
	IsRedirected bool   // true if output is being redirected (piped/file)
	SupportsANSI bool   // true if ANSI escape codes are supported
	SupportsColor bool  // true if color output is supported
	ColorDepth   int    // 0=none, 8=basic, 16=extended, 256=256color, 24=truecolor

	// Screen dimensions
	Width  int // columns
	Height int // rows

	// Input capabilities
	SupportsInput bool // true if this channel can receive input
	EchoEnabled   bool // true if input should be echoed (duplex mode)
	LineMode      bool // true if input is line-buffered, false for raw/char mode

	// Custom metadata (for host-provided channels)
	Metadata map[string]interface{}
}

// NewTerminalCapabilities creates a new capabilities struct with defaults
func NewTerminalCapabilities() *TerminalCapabilities {
	return &TerminalCapabilities{
		TermType:      "unknown",
		IsTerminal:    false,
		IsRedirected:  false,
		SupportsANSI:  false,
		SupportsColor: false,
		ColorDepth:    0,
		Width:         80,
		Height:        24,
		SupportsInput: false,
		EchoEnabled:   true,
		LineMode:      true,
		Metadata:      make(map[string]interface{}),
	}
}

// SetSize updates the terminal dimensions
func (tc *TerminalCapabilities) SetSize(width, height int) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.Width = width
	tc.Height = height
}

// GetSize returns the terminal dimensions
func (tc *TerminalCapabilities) GetSize() (width, height int) {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	return tc.Width, tc.Height
}
