package cli

import (
	"os"
	"strings"

	"github.com/phroun/direct-key-handler/keyboard"
)

// InputHandler manages keyboard input from the host terminal
type InputHandler struct {
	term     *Terminal
	keyboard *keyboard.Handler
}

// NewInputHandler creates a new input handler
func NewInputHandler(term *Terminal) *InputHandler {
	return &InputHandler{
		term: term,
	}
}

// InputLoop reads and processes input from stdin using direct-key-handler
func (h *InputHandler) InputLoop() {
	// Create keyboard handler - don't manage terminal since we do that in Start()
	manageTerminal := false
	h.keyboard = keyboard.New(keyboard.Options{
		InputReader:    os.Stdin,
		ManageTerminal: &manageTerminal,
	})

	// Set up key callback
	h.keyboard.OnKey = func(key string) {
		h.handleKey(key)
	}

	// Start the keyboard handler
	if err := h.keyboard.Start(); err != nil {
		return
	}

	// Wait for stop signal
	<-h.term.stopRender

	h.keyboard.Stop()
}

// processInput handles raw input bytes (for embedded mode)
func (h *InputHandler) processInput(data []byte) {
	// In embedded mode, we receive raw bytes from the parent TUI
	// We need to parse them through the keyboard handler
	// For now, just send directly to PTY and let it handle escape sequences
	h.sendToPTY(data)
}

// handleKey processes a parsed key event from direct-key-handler.
// Returns true if the key was consumed.
func (h *InputHandler) handleKey(key string) bool {
	// Check for input callback first
	h.term.mu.Lock()
	callback := h.term.inputCallback
	h.term.mu.Unlock()

	// Convert key to bytes for the callback
	keyBytes := keyToBytes(key)
	if callback != nil && len(keyBytes) > 0 {
		if callback(keyBytes) {
			return true // Consumed by callback
		}
	}

	// Check if this key should be handled locally (scrollback navigation)
	if h.handleLocalKey(key) {
		return true
	}

	// Scroll to bottom on any input (except scrollback keys)
	if h.term.GetScrollOffset() > 0 {
		h.term.ScrollToBottom()
		h.term.renderer.RequestRender()
	}

	// Convert key to bytes and send to PTY
	if len(keyBytes) > 0 {
		h.sendToPTY(keyBytes)
		return true
	}

	return false
}

// handleLocalKey handles keys that are processed by the CLI adapter locally
// Returns true if the key was handled
func (h *InputHandler) handleLocalKey(key string) bool {
	switch key {
	case "S-PageUp":
		// Scroll up one page
		_, rows := h.term.buffer.GetSize()
		h.term.ScrollUp(rows - 1)
		h.term.renderer.RequestRender()
		return true

	case "S-PageDown":
		// Scroll down one page
		_, rows := h.term.buffer.GetSize()
		h.term.ScrollDown(rows - 1)
		h.term.renderer.RequestRender()
		return true

	case "S-Up":
		// Scroll up one line
		h.term.ScrollUp(1)
		h.term.renderer.RequestRender()
		return true

	case "S-Down":
		// Scroll down one line
		h.term.ScrollDown(1)
		h.term.renderer.RequestRender()
		return true

	case "S-Home":
		// Scroll to top
		h.term.ScrollToTop()
		h.term.renderer.RequestRender()
		return true

	case "S-End":
		// Scroll to bottom
		h.term.ScrollToBottom()
		h.term.renderer.RequestRender()
		return true
	}

	return false
}

// sendToPTY sends data to the child process
func (h *InputHandler) sendToPTY(data []byte) {
	h.term.mu.Lock()
	pty := h.term.pty
	h.term.mu.Unlock()

	if pty != nil {
		pty.Write(data)
	}
}

// keyToBytes converts a key name from direct-key-handler to bytes for PTY
func keyToBytes(key string) []byte {
	// Check special keys first
	if bytes, ok := keyToBytesMap[key]; ok {
		return bytes
	}

	// Single character keys (including "-", "+", "=", etc.) - handle before modifier checks
	if len(key) == 1 {
		return []byte(key)
	}

	// Control keys: ^A through ^Z
	if len(key) == 2 && key[0] == '^' {
		ch := key[1]
		if ch >= 'A' && ch <= 'Z' {
			return []byte{ch - 'A' + 1}
		}
		if ch >= 'a' && ch <= 'z' {
			return []byte{ch - 'a' + 1}
		}
		if ch == '@' {
			return []byte{0}
		}
		if ch == '[' {
			return []byte{27}
		}
		if ch == '\\' {
			return []byte{28}
		}
		if ch == ']' {
			return []byte{29}
		}
		if ch == '^' {
			return []byte{30}
		}
		if ch == '_' {
			return []byte{31}
		}
	}

	// Alt+key: M-x
	if strings.HasPrefix(key, "M-") && len(key) == 3 {
		return []byte{0x1b, key[2]}
	}

	// Multi-byte UTF-8 characters (len > 1, no modifiers)
	if len(key) > 1 && key[0] != '^' && !strings.Contains(key, "-") {
		return []byte(key)
	}

	return nil
}

// keyToBytesMap maps key names to their byte sequences
var keyToBytesMap = map[string][]byte{
	// Control keys
	"Enter":     {13},
	"Tab":       {9},
	"Backspace": {127}, // Most terminals send DEL for backspace
	"Escape":    {27},

	// Arrow keys
	"Up":    {0x1b, '[', 'A'},
	"Down":  {0x1b, '[', 'B'},
	"Right": {0x1b, '[', 'C'},
	"Left":  {0x1b, '[', 'D'},

	// Modified arrow keys (for applications that understand them)
	"C-Up":    {0x1b, '[', '1', ';', '5', 'A'},
	"C-Down":  {0x1b, '[', '1', ';', '5', 'B'},
	"C-Right": {0x1b, '[', '1', ';', '5', 'C'},
	"C-Left":  {0x1b, '[', '1', ';', '5', 'D'},
	"M-Up":    {0x1b, '[', '1', ';', '3', 'A'},
	"M-Down":  {0x1b, '[', '1', ';', '3', 'B'},
	"M-Right": {0x1b, '[', '1', ';', '3', 'C'},
	"M-Left":  {0x1b, '[', '1', ';', '3', 'D'},

	// Navigation keys
	"Home":     {0x1b, '[', 'H'},
	"End":      {0x1b, '[', 'F'},
	"Insert":   {0x1b, '[', '2', '~'},
	"Delete":   {0x1b, '[', '3', '~'},
	"PageUp":   {0x1b, '[', '5', '~'},
	"PageDown": {0x1b, '[', '6', '~'},

	// Function keys
	"F1":  {0x1b, 'O', 'P'},
	"F2":  {0x1b, 'O', 'Q'},
	"F3":  {0x1b, 'O', 'R'},
	"F4":  {0x1b, 'O', 'S'},
	"F5":  {0x1b, '[', '1', '5', '~'},
	"F6":  {0x1b, '[', '1', '7', '~'},
	"F7":  {0x1b, '[', '1', '8', '~'},
	"F8":  {0x1b, '[', '1', '9', '~'},
	"F9":  {0x1b, '[', '2', '0', '~'},
	"F10": {0x1b, '[', '2', '1', '~'},
	"F11": {0x1b, '[', '2', '3', '~'},
	"F12": {0x1b, '[', '2', '4', '~'},
}

// HandleMouseInput processes mouse events (if mouse tracking is enabled)
// This is a placeholder for future mouse support
func (h *InputHandler) HandleMouseInput(x, y int, button int, pressed bool) {
	// Mouse support would be implemented here
	// For now, this is a no-op
}
