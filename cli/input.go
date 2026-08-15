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
	// DECCKM: unmodified cursor keys use the SS3 (ESC O) form in application mode.
	if h.term.buffer != nil && h.term.buffer.IsApplicationCursorKeys() {
		keyBytes = applyAppCursorKeys(key, keyBytes)
	}
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

// applyAppCursorKeys rewrites an unmodified cursor key's CSI introducer to SS3
// (ESC [ x -> ESC O x) for DECCKM application cursor mode.
func applyAppCursorKeys(key string, b []byte) []byte {
	switch key {
	case "Up", "Down", "Left", "Right", "Home", "End":
		if len(b) == 3 && b[0] == 0x1b && b[1] == '[' {
			return []byte{0x1b, 'O', b[2]}
		}
	}
	return b
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

	// Glyph modifier (private kitty extension): a "G-"-prefixed key carries an
	// AltGr / ISO_Level3_Shift-composed character (see direct-key-handler's
	// parseKittyProtocol). Encode it as a kitty CSI-u sequence with the Glyph
	// modifier bit (value 256, sent 1-indexed as 257) and the codepoint set to
	// the produced glyph, so a kitty-aware child receives a distinct "G-" chord
	// it can bind. This is the one CSI-u form this legacy encoder emits; the
	// glyph could not otherwise survive (a plain "€" is indistinguishable from
	// a typed one, and the "-" in "G-€" bars the multi-byte fallback below).
	if strings.HasPrefix(key, "G-") {
		if r := []rune(key[2:]); len(r) == 1 {
			return []byte(fmt.Sprintf("\x1b[%d;257u", r[0]))
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

	// Tab: S-Tab is ESC [ Z, Alt+Tab is ESC + Tab byte
	if baseKey == "Tab" {
		switch mod {
		case 2: // Shift
			return []byte{0x1b, '[', 'Z'}
		case 3: // Alt
			return []byte{0x1b, 0x09}
		case 4: // Shift+Alt
			return []byte{0x1b, 0x1b, '[', 'Z'} // ESC + S-Tab
		case 5: // Ctrl
			return []byte{0x09} // Ctrl+Tab = Tab (no standard sequence)
		case 6: // Shift+Ctrl
			return []byte{0x1b, '[', 'Z'} // Treat as S-Tab
		case 7: // Alt+Ctrl
			return []byte{0x1b, 0x09} // ESC + Tab
		default:
			return []byte{0x09}
		}
	}

	// The home-row key with modifiers. This branch was written when the name
	// for it was "Enter"; upstream now calls the home-row key "Return" and
	// gives "Enter" to the keypad's, so the behavior moves to the name it
	// always described rather than staying on the word.
	if baseKey == "Return" {
		if mod == 3 { // Alt+Return
			return []byte{0x1b, 0x0d}
		}
		// Other modifier combos - just send CR
		return []byte{0x0d}
	}

	// The keypad's Enter with modifiers: its own sequence behind an ESC for
	// Alt, the same shape as the home-row key above. Sending CR here would put
	// the two keys back together, which is what the split exists to prevent.
	if baseKey == "Enter" {
		if mod == 3 { // Alt+Enter
			return []byte{0x1b, 0x1b, 'O', 'M'}
		}
		return []byte{0x1b, 'O', 'M'}
	}

	// Backspace with modifiers. "Delete" is the same key: direct-key-handler
	// names BS (8) and DEL (127) apart because it cannot tell which one a
	// terminal will send for its backspace, but both erase behind the cursor,
	// so both encode the same way going out. (Forward delete is "FDel".)
	if baseKey == "Backspace" || baseKey == "Delete" {
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
	"FDel":     "3", // forward delete; the erase-behind keys are handled above
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
	"Tab":    {9},
	"Escape": {27},
	"Space":  {32},

	// The two Enter keys, kept apart on the wire.
	//
	// "Return" is the home-row key and sends CR. "Enter" is the keypad's, and
	// sending CR for it too would erase the difference in the one direction
	// where it can still be expressed: a guest handed CR cannot know which key
	// was struck, and neither can this package when the bytes come back. SS3 M
	// is what a terminal sends for the keypad's Enter in application keypad
	// mode (DECKPAM), and it is the only encoding that round-trips — feed it
	// back to direct-key-handler and "Enter" comes out.
	//
	// The keypad is treated as being in application mode always. Numeric mode
	// would have it send CR, which is the conflation this is avoiding, so the
	// distinction wins over the mode.
	"Return": {13},
	"Enter":  {0x1b, 'O', 'M'},

	// The two erase-behind keys. direct-key-handler names BS (8) and DEL (127)
	// apart on the way IN, because a terminal's backspace sends one or the
	// other by lineage and only the application can decide whether it cares.
	// Going OUT there is nothing to decide: both mean erase behind, and what a
	// terminal sends for that is DEL. Emitting BS here instead would be read as
	// Ctrl-H by any guest that maps input to key events — a browser has no
	// binding for Ctrl-H, so the keystroke would simply vanish.
	"Backspace": {127},
	"Delete":    {127},

	// Arrow keys
	"Up":    {0x1b, '[', 'A'},
	"Down":  {0x1b, '[', 'B'},
	"Right": {0x1b, '[', 'C'},
	"Left":  {0x1b, '[', 'D'},

	// Navigation keys
	"Home":     {0x1b, '[', 'H'},
	"End":      {0x1b, '[', 'F'},
	"Insert":   {0x1b, '[', '2', '~'},
	"FDel":     {0x1b, '[', '3', '~'}, // forward delete
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
