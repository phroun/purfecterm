package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/phroun/direct-key-handler/keyboard"
	"github.com/phroun/purfecterm"
)

// InputHandler manages keyboard input from the host terminal
type InputHandler struct {
	term     *Terminal
	keyboard *keyboard.Handler

	// Mouse state for coordinate tracking
	lastMouseX int // Last mouse X from Mouse@x,y position key (1-based host coords)
	lastMouseY int // Last mouse Y from Mouse@x,y position key (1-based host coords)
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
	// Handle mouse events first
	if h.handleMouseKey(key) {
		return true
	}

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

// keyToBytes converts a key name from direct-key-handler to bytes for PTY.
// Handles all modifier combinations (S-, M-, C-) with all base keys.
func keyToBytes(key string) []byte {
	// Check explicit mappings first
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

	// Parse modifier prefixes and base key
	mods, baseKey := parseModifiers(key)
	if mods > 0 {
		// Try to encode with modifiers
		if result := encodeModifiedKey(mods, baseKey); result != nil {
			return result
		}
	}

	// Multi-byte UTF-8 characters (len > 1, no modifiers, no hyphens)
	if len(key) > 1 && key[0] != '^' && !strings.Contains(key, "-") {
		return []byte(key)
	}

	return nil
}

// parseModifiers extracts modifier flags and base key from a key string.
// Returns xterm modifier code (2=Shift, 3=Alt, 4=Shift+Alt, 5=Ctrl, etc.) and base key.
// Returns 0 if no modifiers.
func parseModifiers(key string) (int, string) {
	mods := 0
	remaining := key

	for {
		if strings.HasPrefix(remaining, "S-") {
			mods |= 1 // Shift
			remaining = remaining[2:]
		} else if strings.HasPrefix(remaining, "M-") {
			mods |= 2 // Alt/Meta
			remaining = remaining[2:]
		} else if strings.HasPrefix(remaining, "C-") {
			mods |= 4 // Control
			remaining = remaining[2:]
		} else {
			break
		}
	}

	if mods == 0 {
		return 0, key
	}
	// Convert to xterm modifier code (add 1)
	return mods + 1, remaining
}

// encodeModifiedKey creates the escape sequence for a modified key.
// mod is the xterm modifier code (2=Shift, 3=Alt, etc.)
func encodeModifiedKey(mod int, baseKey string) []byte {
	modChar := byte('0' + mod)

	// Handle single character with Alt (M-x)
	if len(baseKey) == 1 {
		if mod == 3 { // Just Alt
			return []byte{0x1b, baseKey[0]}
		}
		// Alt+Shift+char or other combos - send ESC then char
		// (terminals vary in how they handle this)
		if mod&2 != 0 { // Has Alt
			return []byte{0x1b, baseKey[0]}
		}
		return nil
	}

	// Arrow keys: ESC [ 1 ; <mod> <A-D>
	if code, ok := arrowKeyCode[baseKey]; ok {
		return []byte{0x1b, '[', '1', ';', modChar, code}
	}

	// Home/End: ESC [ 1 ; <mod> <H|F>
	if code, ok := homeEndCode[baseKey]; ok {
		return []byte{0x1b, '[', '1', ';', modChar, code}
	}

	// Tab: ESC [ 1 ; <mod> Z (or ESC [ Z for just Shift)
	if baseKey == "Tab" {
		if mod == 2 { // Just Shift
			return []byte{0x1b, '[', 'Z'}
		}
		return []byte{0x1b, '[', '1', ';', modChar, 'Z'}
	}

	// Enter with modifiers
	if baseKey == "Enter" {
		if mod == 3 { // Alt+Enter
			return []byte{0x1b, 0x0d}
		}
		// Other modifier combos - just send CR
		return []byte{0x0d}
	}

	// Backspace with modifiers
	if baseKey == "Backspace" {
		if mod == 3 { // Alt+Backspace
			return []byte{0x1b, 0x7f}
		}
		if mod == 5 { // Ctrl+Backspace
			return []byte{0x08} // BS
		}
		return []byte{0x7f}
	}

	// Escape with modifiers
	if baseKey == "Escape" {
		if mod == 3 { // Alt+Escape
			return []byte{0x1b, 0x1b}
		}
		return []byte{0x1b}
	}

	// F1-F4: ESC [ 1 ; <mod> <P-S>
	if code, ok := f1f4Code[baseKey]; ok {
		return []byte{0x1b, '[', '1', ';', modChar, code}
	}

	// F5-F12, Insert, Delete, PageUp, PageDown: ESC [ <code> ; <mod> ~
	if codeStr, ok := tildeKeyCode[baseKey]; ok {
		result := []byte{0x1b, '['}
		result = append(result, []byte(codeStr)...)
		result = append(result, ';', modChar, '~')
		return result
	}

	// Space with modifiers
	if baseKey == "Space" {
		if mod == 3 { // Alt+Space
			return []byte{0x1b, ' '}
		}
		if mod == 5 { // Ctrl+Space
			return []byte{0x00}
		}
		return []byte{' '}
	}

	return nil
}

var arrowKeyCode = map[string]byte{
	"Up":    'A',
	"Down":  'B',
	"Right": 'C',
	"Left":  'D',
}

var homeEndCode = map[string]byte{
	"Home": 'H',
	"End":  'F',
}

var f1f4Code = map[string]byte{
	"F1": 'P',
	"F2": 'Q',
	"F3": 'R',
	"F4": 'S',
}

var tildeKeyCode = map[string]string{
	"Insert":   "2",
	"Delete":   "3",
	"PageUp":   "5",
	"PageDown": "6",
	"F5":       "15",
	"F6":       "17",
	"F7":       "18",
	"F8":       "19",
	"F9":       "20",
	"F10":      "21",
	"F11":      "23",
	"F12":      "24",
}

// keyToBytesMap maps base key names (without modifiers) to their byte sequences.
// Modified keys are handled dynamically by encodeModifiedKey.
var keyToBytesMap = map[string][]byte{
	// Control keys
	"Enter":     {13},
	"Tab":       {9},
	"Backspace": {127}, // Most terminals send DEL for backspace
	"Escape":    {27},
	"Space":     {32},

	// Arrow keys
	"Up":    {0x1b, '[', 'A'},
	"Down":  {0x1b, '[', 'B'},
	"Right": {0x1b, '[', 'C'},
	"Left":  {0x1b, '[', 'D'},

	// Navigation keys
	"Home":     {0x1b, '[', 'H'},
	"End":      {0x1b, '[', 'F'},
	"Insert":   {0x1b, '[', '2', '~'},
	"Delete":   {0x1b, '[', '3', '~'},
	"PageUp":   {0x1b, '[', '5', '~'},
	"PageDown": {0x1b, '[', '6', '~'},

	// Function keys (F1-F4 use SS3, F5+ use CSI)
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

// handleMouseKey processes mouse key events from direct-key-handler.
// The library emits mouse events as:
//   - "Mouse@x,y" - position key (emitted before action for press/release/scroll)
//   - "MouseLeftPress", "MouseLeftRelease", "MouseScrollUp", etc. - action keys
//   - "MouseLeftDrag@x,y", "MouseRightDrag@x,y" - drag events (position in key)
//
// Returns true if the key was a mouse event and was handled.
func (h *InputHandler) handleMouseKey(key string) bool {
	if !strings.HasPrefix(key, "Mouse") {
		return false
	}

	// Check if mouse reporting is enabled
	if h.term.options.DisableMouseReporting {
		return true // Consume but don't forward
	}

	trackingMode := h.term.buffer.GetMouseTrackingMode()
	if trackingMode == 0 {
		return true // Consume but don't forward (no app tracking active)
	}

	// Handle position key: "Mouse@x,y"
	if strings.HasPrefix(key, "Mouse@") {
		var x, y int
		if _, err := fmt.Sscanf(key, "Mouse@%d,%d", &x, &y); err == nil {
			h.lastMouseX = x
			h.lastMouseY = y
		}
		return true // Position key consumed, wait for action key
	}

	// Handle drag events: "MouseLeftDrag@x,y" etc.
	if strings.Contains(key, "Drag@") {
		if trackingMode < 1002 {
			return true // Mode 1000 doesn't report motion
		}
		var x, y int
		atIdx := strings.LastIndex(key, "@")
		if atIdx >= 0 {
			fmt.Sscanf(key[atIdx:], "@%d,%d", &x, &y)
		}
		innerX, innerY, ok := h.hostToInnerCoords(x, y)
		if !ok {
			return true // Outside terminal area
		}
		// Determine button from key name
		btn := purfecterm.MouseButtonNone
		actionPart := key[:atIdx]
		actionPart = stripMouseModifiers(actionPart)
		switch {
		case strings.Contains(actionPart, "Left"):
			btn = purfecterm.MouseButtonLeft
		case strings.Contains(actionPart, "Middle"):
			btn = purfecterm.MouseButtonMiddle
		case strings.Contains(actionPart, "Right"):
			btn = purfecterm.MouseButtonRight
		}
		btn |= purfecterm.MouseMotionFlag
		btn |= mouseModsFromKey(key)
		encodingMode := h.term.buffer.GetMouseEncodingMode()
		data := purfecterm.EncodeMouseEvent(btn, innerX, innerY, true, encodingMode)
		if data != nil {
			h.sendToPTY(data)
		}
		return true
	}

	// Handle action keys using last stored position
	innerX, innerY, ok := h.hostToInnerCoords(h.lastMouseX, h.lastMouseY)
	if !ok {
		return true // Outside terminal area
	}

	// Strip modifier prefixes to get base action
	baseKey := stripMouseModifiers(key)
	mods := mouseModsFromKey(key)

	var btn int
	press := true

	switch baseKey {
	case "MouseLeftPress":
		btn = purfecterm.MouseButtonLeft
	case "MouseMiddlePress":
		btn = purfecterm.MouseButtonMiddle
	case "MouseRightPress":
		btn = purfecterm.MouseButtonRight
	case "MousePress":
		btn = purfecterm.MouseButtonLeft
	case "MouseLeftRelease":
		btn = purfecterm.MouseButtonLeft
		press = false
	case "MouseMiddleRelease":
		btn = purfecterm.MouseButtonMiddle
		press = false
	case "MouseRightRelease":
		btn = purfecterm.MouseButtonRight
		press = false
	case "MouseRelease":
		btn = purfecterm.MouseButtonLeft
		press = false
	case "MouseScrollUp":
		btn = purfecterm.MouseScrollUp
	case "MouseScrollDown":
		btn = purfecterm.MouseScrollDown
	default:
		return true // Unknown mouse event, consume
	}

	btn |= mods
	encodingMode := h.term.buffer.GetMouseEncodingMode()
	data := purfecterm.EncodeMouseEvent(btn, innerX, innerY, press, encodingMode)
	if data != nil {
		h.sendToPTY(data)
	}
	return true
}

// hostToInnerCoords converts host terminal coordinates (1-based) to inner terminal coordinates (1-based).
// Returns false if the position is outside the terminal content area.
func (h *InputHandler) hostToInnerCoords(hostX, hostY int) (int, int, bool) {
	borderOffset := 0
	if h.term.options.BorderStyle != BorderNone {
		borderOffset = 1
	}

	// Inner content starts at (OffsetX + borderOffset, OffsetY + borderOffset) in 0-based coords
	contentStartX := h.term.options.OffsetX + borderOffset
	contentStartY := h.term.options.OffsetY + borderOffset

	// Convert from 1-based host to 0-based, subtract offset, convert back to 1-based
	innerX := hostX - contentStartX // Now 1-based relative to content area
	innerY := hostY - contentStartY

	cols, rows := h.term.buffer.GetSize()
	if innerX < 1 || innerX > cols || innerY < 1 || innerY > rows {
		return 0, 0, false
	}

	return innerX, innerY, true
}

// stripMouseModifiers removes "S-", "M-", "C-" prefixes from a mouse key name
func stripMouseModifiers(key string) string {
	for strings.HasPrefix(key, "S-") || strings.HasPrefix(key, "M-") || strings.HasPrefix(key, "C-") {
		key = key[2:]
	}
	return key
}

// mouseModsFromKey extracts xterm mouse modifier flags from a key name
func mouseModsFromKey(key string) int {
	mods := 0
	if strings.HasPrefix(key, "S-") || strings.Contains(key, "-S-") {
		mods |= purfecterm.MouseModShift
	}
	if strings.HasPrefix(key, "M-") || strings.Contains(key, "-M-") {
		mods |= purfecterm.MouseModAlt
	}
	if strings.HasPrefix(key, "C-") || strings.Contains(key, "-C-") {
		mods |= purfecterm.MouseModControl
	}
	return mods
}
