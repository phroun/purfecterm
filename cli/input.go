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
	switch keyByName[key] {
	case keyboard.KeyUp, keyboard.KeyDown, keyboard.KeyLeft, keyboard.KeyRight,
		keyboard.KeyHome, keyboard.KeyEnd:
		if len(b) == 3 && b[0] == 0x1b && b[1] == '[' {
			return []byte{0x1b, 'O', b[2]}
		}
	}
	return b
}

// keyToBytes converts a key name from direct-key-handler to bytes for PTY.
// Handles all modifier combinations (S-, M-, C-) with all base keys.
func keyToBytes(key string) []byte {
	// Check explicit mappings first. The name is resolved to a Key and the
	// bytes are looked up under that, so this table is indexed by whatever
	// direct-key-handler currently calls the key rather than by a word copied
	// out of it at some point in the past. A key it names but this encoder has
	// no bytes for falls through to unknownKeyBytes below, same as a name from
	// nowhere.
	if bytes, ok := keyBytes[keyByName[key]]; ok {
		return bytes
	}

	// Single character keys (including "-", "+", "=", etc.) - handle before modifier checks
	if len(key) == 1 {
		return []byte(key)
	}

	// Control chords: ^A through ^Z, and the punctuation ones.
	if b, ok := controlByte(key); ok {
		return []byte{b}
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

	// A multi-byte key that is a single rune is a typed character — "é", "€",
	// "日" — and goes out as itself. (Single-BYTE characters were handled
	// above.) This has to be settled before the bracketing below, or every
	// non-ASCII keystroke would arrive at the guest wearing angle brackets.
	if len([]rune(key)) == 1 {
		return []byte(key)
	}

	// Anything still here has no encoding in this package, and it goes out
	// bracketed: "<F13>", "<C-Menu>", "<M-a:Repeat>", "<a:Release>".
	//
	// EVERY unencodable shape lands here, modified chords and suffixed names
	// included. They used to return nil, which sent nothing and left no trace,
	// and the reason given for it was that nil means "not consumed" so an
	// embedding host still gets its chance — but neither caller reads that
	// bool: cli/input.go's OnKey discards it, and mew's trinket discards it and
	// returns true unconditionally. So nil was not preserving anything. It was
	// this encoder concluding, from its own inability to spell a key, that the
	// keypress did not happen.
	//
	// What it cannot spell is a gap in this package and nowhere else. The kitty
	// keyboard protocol is negotiated in full by keyboard_protocol.go —
	// disambiguation, event reporting, alternates, all-keys, associated text —
	// and none of those flags is consulted on the way out. Until they are, the
	// honest report of a key this encoder has no bytes for is a mark at the
	// guest saying so, not silence that reads as a decision.
	//
	// Bare letters, the oldest behavior, were worse than either: a name typed as
	// its own characters is indistinguishable from the user having typed them,
	// so a key that stopped working looked like ordinary text.
	return unknownKeyBytes(key)
}

// unknownKeyBytes is what a key name with no encoding sends: the name in angle
// brackets. Written once so a test can ask "did this key fall through?"
// without restating the bracketing and drifting from it.
func unknownKeyBytes(name string) []byte {
	return []byte("<" + name + ">")
}

// controlByte answers the ASCII control code a caret chord names — "^A" is 1,
// "^[" is 27 — and whether the name is a caret chord at all.
//
// direct-key-handler spells Control with a caret against the keys the caret is
// natural for rather than with a "C-" prefix, so this is the shape Ctrl chords
// arrive in. It is a function rather than an inline block because the caret is
// also the BASE of every Ctrl chord that carries a further modifier: Alt+Ctrl+A
// arrives as "M-^A", and encodeModifiedKey needs the same answer this does.
//
// Note the ok result carries the meaning, not the byte: "^@" is NUL, a real
// encoding whose value is zero.
func controlByte(name string) (byte, bool) {
	if len(name) != 2 || name[0] != '^' {
		return 0, false
	}
	switch ch := name[1]; {
	case ch >= 'A' && ch <= 'Z':
		return ch - 'A' + 1, true
	case ch >= 'a' && ch <= 'z':
		return ch - 'a' + 1, true
	case ch == '@':
		return 0, true
	case ch == '[':
		return 27, true
	case ch == '\\':
		return 28, true
	case ch == ']':
		return 29, true
	case ch == '^':
		return 30, true
	case ch == '_':
		return 31, true
	}
	return 0, false
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

	// The xterm modifier code is 1 + a bitmask (Shift 1, Alt 2, Ctrl 4), so the
	// bits have to be read out of mod-1. Testing them against mod directly made
	// every code with the 2 bit set look like Alt — code 2 is Shift alone, and
	// it was being encoded as Alt.
	const modAlt = 2
	hasAlt := (mod-1)&modAlt != 0

	// A caret chord carrying a further modifier: "M-^A" is Alt+Ctrl+A, "S-^A" is
	// Ctrl+Shift+A, "M-S-^A" is both. The caret already holds the Control, so
	// what is left to encode is whatever rides alongside it.
	//
	// Shift has nowhere to go: an ASCII control code is five bits and has no
	// room for it, which is why a legacy terminal sends plain ^A for
	// Ctrl+Shift+A too. Degrading to ^A is therefore not a loss this introduces
	// — it is what the wire has always done. Alt keeps its ESC prefix.
	//
	// These arrive only under the kitty protocol, and all four of them used to
	// fall out of this function as nil and be dropped without a trace.
	if b, ok := controlByte(baseKey); ok {
		if hasAlt {
			return []byte{0x1b, b}
		}
		return []byte{b}
	}

	// A single printable character with modifiers, measured in RUNES: "M-€" is
	// one character in three bytes, and a byte-length test sent it here to be
	// dropped. Ctrl and Shift do not appear in this shape — Ctrl arrives as a
	// caret chord, handled above, and Shift is carried by the character's own
	// case — so Alt is the only modifier there is anything to encode.
	if r := []rune(baseKey); len(r) == 1 {
		if hasAlt {
			return append([]byte{0x1b}, baseKey...)
		}
		return nil
	}

	// Every comparison below is against a Key rather than a spelling, for the
	// reason keyBytes is: a name this package invents its own opinion about goes
	// stale silently the next time the upstream vocabulary moves. A name with no
	// Key resolves to KeyNone, which matches nothing here and falls through.
	base := keyByName[baseKey]

	// Arrow keys: ESC [ 1 ; <mod> <A-D>
	if code, ok := arrowKeyCode[base]; ok {
		return []byte{0x1b, '[', '1', ';', modChar, code}
	}

	// Home/End: ESC [ 1 ; <mod> <H|F>
	if code, ok := homeEndCode[base]; ok {
		return []byte{0x1b, '[', '1', ';', modChar, code}
	}

	// Tab: S-Tab is ESC [ Z, Alt+Tab is ESC + Tab byte
	if base == keyboard.KeyTab {
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
	// gives "Enter" to the keypad's. Naming the constant is what makes that
	// history irrelevant — the branch follows the key, not the word.
	if base == keyboard.KeyReturn {
		if mod == 3 { // Alt+Return
			return []byte{0x1b, 0x0d}
		}
		// Other modifier combos - just send CR
		return []byte{0x0d}
	}

	// The keypad's Enter with modifiers: its own sequence behind an ESC for
	// Alt, the same shape as the home-row key above. Sending CR here would put
	// the two keys back together, which is what the split exists to prevent.
	if base == keyboard.KeyKeypadEnter {
		if mod == 3 { // Alt+Enter
			return []byte{0x1b, 0x1b, 'O', 'M'}
		}
		return []byte{0x1b, 'O', 'M'}
	}

	// Backspace with modifiers. "Delete" is the same key: direct-key-handler
	// names BS (8) and DEL (127) apart because it cannot tell which one a
	// terminal will send for its backspace, but both erase behind the cursor,
	// so both encode the same way going out. (Forward delete is "FDel".)
	if base == keyboard.KeyBackspace || base == keyboard.KeyDEL {
		if mod == 3 { // Alt+Backspace
			return []byte{0x1b, 0x7f}
		}
		if mod == 5 { // Ctrl+Backspace
			return []byte{0x08} // BS
		}
		return []byte{0x7f}
	}

	// Escape with modifiers
	if base == keyboard.KeyEscape {
		if mod == 3 { // Alt+Escape
			return []byte{0x1b, 0x1b}
		}
		return []byte{0x1b}
	}

	// F1-F4: ESC [ 1 ; <mod> <P-S>
	if code, ok := f1f4Code[base]; ok {
		return []byte{0x1b, '[', '1', ';', modChar, code}
	}

	// F5-F12, Insert, FDel, PageUp, PageDown: ESC [ <code> ; <mod> ~
	if codeStr, ok := tildeKeyCode[base]; ok {
		result := []byte{0x1b, '['}
		result = append(result, []byte(codeStr)...)
		result = append(result, ';', modChar, '~')
		return result
	}

	// Space with modifiers
	if base == keyboard.KeySpace {
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

var arrowKeyCode = map[keyboard.Key]byte{
	keyboard.KeyUp:    'A',
	keyboard.KeyDown:  'B',
	keyboard.KeyRight: 'C',
	keyboard.KeyLeft:  'D',
}

var homeEndCode = map[keyboard.Key]byte{
	keyboard.KeyHome: 'H',
	keyboard.KeyEnd:  'F',
}

var f1f4Code = map[keyboard.Key]byte{
	keyboard.KeyF1: 'P',
	keyboard.KeyF2: 'Q',
	keyboard.KeyF3: 'R',
	keyboard.KeyF4: 'S',
}

var tildeKeyCode = map[keyboard.Key]string{
	keyboard.KeyInsert:   "2",
	keyboard.KeyDelete:   "3", // forward delete; the erase-behind keys are handled above
	keyboard.KeyPageUp:   "5",
	keyboard.KeyPageDown: "6",
	keyboard.KeyF5:       "15",
	keyboard.KeyF6:       "17",
	keyboard.KeyF7:       "18",
	keyboard.KeyF8:       "19",
	keyboard.KeyF9:       "20",
	keyboard.KeyF10:      "21",
	keyboard.KeyF11:      "23",
	keyboard.KeyF12:      "24",
}

// keyBytes maps a key to the bytes a terminal sends for it with no modifiers
// held. Modified keys are built by encodeModifiedKey instead.
//
// It is keyed by direct-key-handler's CONSTANT, never by the name. The names
// belong to that package and it may respell them: when the home-row key was
// renamed from "Enter" to "Return", a table written in words still said
// "Enter", nothing matched the arriving name, and pressing Return typed its own
// six letters into the guest — with no error raised anywhere, because an
// unmatched name is indistinguishable from a key this encoder never knew. A
// constant cannot drift like that. A respelling upstream changes only the
// spelling this table is indexed under, and a key withdrawn upstream stops
// compiling rather than going quiet.
var keyBytes = map[keyboard.Key][]byte{
	// Control keys
	keyboard.KeyTab:    {9},
	keyboard.KeyEscape: {27},
	keyboard.KeySpace:  {32},

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
	keyboard.KeyReturn:      {13},
	keyboard.KeyKeypadEnter: {0x1b, 'O', 'M'},

	// The two erase-behind keys. direct-key-handler names BS (8) and DEL (127)
	// apart on the way IN, because a terminal's backspace sends one or the
	// other by lineage and only the application can decide whether it cares.
	// Going OUT there is nothing to decide: both mean erase behind, and what a
	// terminal sends for that is DEL. Emitting BS here instead would be read as
	// Ctrl-H by any guest that maps input to key events — a browser has no
	// binding for Ctrl-H, so the keystroke would simply vanish.
	keyboard.KeyBackspace: {127},
	keyboard.KeyDEL:       {127},

	// Arrow keys
	keyboard.KeyUp:    {0x1b, '[', 'A'},
	keyboard.KeyDown:  {0x1b, '[', 'B'},
	keyboard.KeyRight: {0x1b, '[', 'C'},
	keyboard.KeyLeft:  {0x1b, '[', 'D'},

	// Navigation keys
	keyboard.KeyHome:     {0x1b, '[', 'H'},
	keyboard.KeyEnd:      {0x1b, '[', 'F'},
	keyboard.KeyInsert:   {0x1b, '[', '2', '~'},
	keyboard.KeyDelete:   {0x1b, '[', '3', '~'}, // forward delete, spelled "FDel"
	keyboard.KeyPageUp:   {0x1b, '[', '5', '~'},
	keyboard.KeyPageDown: {0x1b, '[', '6', '~'},

	// Function keys (F1-F4 use SS3, F5+ use CSI)
	keyboard.KeyF1:  {0x1b, 'O', 'P'},
	keyboard.KeyF2:  {0x1b, 'O', 'Q'},
	keyboard.KeyF3:  {0x1b, 'O', 'R'},
	keyboard.KeyF4:  {0x1b, 'O', 'S'},
	keyboard.KeyF5:  {0x1b, '[', '1', '5', '~'},
	keyboard.KeyF6:  {0x1b, '[', '1', '7', '~'},
	keyboard.KeyF7:  {0x1b, '[', '1', '8', '~'},
	keyboard.KeyF8:  {0x1b, '[', '1', '9', '~'},
	keyboard.KeyF9:  {0x1b, '[', '2', '0', '~'},
	keyboard.KeyF10: {0x1b, '[', '2', '1', '~'},
	keyboard.KeyF11: {0x1b, '[', '2', '3', '~'},
	keyboard.KeyF12: {0x1b, '[', '2', '4', '~'},
}

// keyByName turns a base name direct-key-handler emitted back into its Key, so
// every table and comparison below is written in constants while the lookup
// still starts from the string that actually arrives.
//
// It is built from AllKeys(), which is the point: the list of keys and their
// spellings comes from one place, at the source. A key added upstream appears
// here the moment the dependency is bumped, and TestEveryKeyIsAccountedFor then
// requires this package to say whether it has bytes for it.
var keyByName = func() map[string]keyboard.Key {
	all := keyboard.AllKeys()
	m := make(map[string]keyboard.Key, len(all))
	for _, k := range all {
		m[k.DefaultName()] = k
	}
	return m
}()

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
