package purfecterm

import (
	"fmt"
	"strconv"
	"strings"
)

// Parser states
type parserState int

const (
	stateGround      parserState = iota
	stateEscape                  // After ESC
	stateCSI                     // After ESC [
	stateCSIParam                // Reading CSI parameters
	stateOSC                     // After ESC ]
	stateOSCString               // Reading OSC string
	stateOSCEsc                  // ESC seen inside an OSC string (expecting the \ of ST)
	stateCharset                 // After ESC ( or ESC )
	stateDECLineAttr             // After ESC # (waiting for line attribute command)
	stateDCS                     // After ESC P (collecting a DCS string)
	stateDCSEsc                  // ESC seen inside a DCS string (expecting the \ of ST)
)

// SGRParam represents an SGR parameter with optional subparameters
// For example, "38:2:255:128:0" becomes {Base: 38, Subs: [2, 255, 128, 0]}
type SGRParam struct {
	Base int   // Primary parameter value
	Subs []int // Subparameters (colon-separated values after the base)
}

// Parser parses ANSI escape sequences and updates a Buffer
type Parser struct {
	buffer *Buffer
	state  parserState

	// CSI sequence accumulator
	csiParams       []int
	csiRawParams    []string // Raw parameter strings for subparameter parsing
	csiPrivate      byte     // For private sequences like ?25h
	csiIntermediate byte     // For sequences with intermediate bytes like DECSCUSR (SP q)
	csiBuf          strings.Builder

	// OSC accumulator
	oscCmd int             // OSC command number (e.g., 7000 for palette, 7001 for glyph)
	oscBuf strings.Builder // OSC command arguments

	// DCS accumulator (ESC P ... ST)
	dcsBuf strings.Builder

	// OSC 52 clipboard (see clipboard.go)
	onClipboard     func(ev ClipboardEvent, reply func([]byte))
	clipboardPolicy ClipboardPolicy
	responseSink    func([]byte)

	// UTF-8 multi-byte handling
	utf8Buf  []byte
	utf8Need int

	// savedModes holds DEC private mode values stashed by XTSAVE (CSI ? Pm s)
	// for XTRESTORE (CSI ? Pm r).
	savedModes map[int]bool
}

// NewParser creates a new ANSI parser for the given buffer
func NewParser(buffer *Buffer) *Parser {
	return &Parser{
		buffer:    buffer,
		state:     stateGround,
		csiParams: make([]int, 0, 16),
		// Writes act, queries do not. The zero ClipboardPolicy would deny
		// both, which is not the documented default.
		clipboardPolicy: DefaultClipboardPolicy(),
	}
}

// CaptureObserver receives a terminal's output as events, for a host that wants
// to mirror or log it beyond what the screen buffer keeps. Registered with
// SetCaptureObserver; nil means no events and no cost. Calls happen on the
// goroutine that feeds the terminal, in feed order. An implementer that wants
// only some events should embed NopCaptureObserver so later additions to this
// interface do not break it.
type CaptureObserver interface {
	// OnOutput reports a chunk of input exactly as the parser received it,
	// before parsing — the literal bytes, in the same chunks they were fed
	// (the `raw` capture rung). The slice is valid only for the duration of the
	// call; copy it to retain.
	OnOutput(data []byte)

	// OnLineOff reports one line leaving the screen into scrollback — a line of
	// the ordered transcript (the `lines` rung), final at this point. It fires
	// from every path that moves a line off: a scroll, a resize that shrinks the
	// screen, and the scrollback-preserving clear/reset. line and info are valid
	// only for the call. SerializeLineANS turns them into a self-contained
	// string.
	OnLineOff(line []Cell, info LineInfo)

	// OnScreenSwitch reports the terminal switching between the primary and the
	// alternate screen (DEC modes ?47 / ?1047 / ?1049): toAlt is true on entering
	// the alternate screen, false on returning to the primary. It fires whenever
	// an observer is set — regardless of SetCaptureScope — so a consumer always
	// knows which screen the events that follow belong to, even for a screen it
	// is not itself capturing.
	OnScreenSwitch(toAlt bool)

	// The live-screen events (the `live` rung) let an observer mirror the screen
	// in place. They fire only while live events are enabled (SetCaptureLive),
	// since they sit on the write/cursor hot path. Positions are 0-based cells.
	// Each is a distinct op so an observer may treat them differently (a wrap is
	// not a newline is not an absolute move), even where a mirror handles several
	// the same way.
	//
	// OnWrite reports a run of printable text placed starting at (x,y) with the
	// pen as a complete absolute SGR (e.g. "\x1b[0;1;31m", empty for default).
	// Runs are batched: one call per contiguous same-pen run, flushed before any
	// other event and at the end of a feed. text/sgr are valid only for the call.
	OnWrite(x, y int, text, sgr string)
	OnCursorMove(x, y int) // absolute positioning (CUP), after clamping
	OnNewline(x, y int)    // line feed / newline, after the move (and any scroll)
	OnLineWrap(x, y int)   // auto-wrap at the right margin, after the move
	OnBackspace(x, y int)  // backspace, after the move
	OnScrollLineOff(n int) // n lines scrolled off the top
	// The erase events. Each carries the current pen (a complete absolute SGR,
	// empty for the default), whose background fills the erased cells
	// (background-colour erase); the directional ones carry the cursor position.
	OnClearScreen(sgr string)                  // ED2: whole screen (cursor homes)
	OnClearEndOfLine(x, y int, sgr string)     // EL0: (x,y) to the right margin
	OnClearBeginOfLine(x, y int, sgr string)   // EL1: line start through (x,y)
	OnClearLine(y int, sgr string)             // EL2: the whole line y
	OnClearEndOfScreen(x, y int, sgr string)   // ED0: (x,y) to end of screen
	OnClearBeginOfScreen(x, y int, sgr string) // ED1: screen start through (x,y)

	// The in-line character ops a curses app uses for horizontal shifts and
	// editing. sgr is the current pen whose background fills the blanks.
	OnDeleteChars(x, y, n int, sgr string) // DCH: delete n cells at (x,y), shifting left
	OnInsertChars(x, y, n int, sgr string) // ICH: insert n blank cells at (x,y), shifting right
	OnEraseChars(x, y, n int, sgr string)  // ECH: blank n cells at (x,y) in place
}

// NopCaptureObserver is a CaptureObserver that ignores every event. Embed it to
// implement only the events you care about and stay forward-compatible as the
// interface grows.
type NopCaptureObserver struct{}

func (NopCaptureObserver) OnOutput([]byte)                       {}
func (NopCaptureObserver) OnLineOff([]Cell, LineInfo)            {}
func (NopCaptureObserver) OnScreenSwitch(bool)                   {}
func (NopCaptureObserver) OnWrite(int, int, string, string)      {}
func (NopCaptureObserver) OnCursorMove(int, int)                 {}
func (NopCaptureObserver) OnNewline(int, int)                    {}
func (NopCaptureObserver) OnLineWrap(int, int)                   {}
func (NopCaptureObserver) OnBackspace(int, int)                  {}
func (NopCaptureObserver) OnScrollLineOff(int)                   {}
func (NopCaptureObserver) OnClearScreen(string)                  {}
func (NopCaptureObserver) OnClearEndOfLine(int, int, string)     {}
func (NopCaptureObserver) OnClearBeginOfLine(int, int, string)   {}
func (NopCaptureObserver) OnClearLine(int, string)               {}
func (NopCaptureObserver) OnClearEndOfScreen(int, int, string)   {}
func (NopCaptureObserver) OnClearBeginOfScreen(int, int, string) {}
func (NopCaptureObserver) OnDeleteChars(int, int, int, string)   {}
func (NopCaptureObserver) OnInsertChars(int, int, int, string)   {}
func (NopCaptureObserver) OnEraseChars(int, int, int, string)    {}

// SetCaptureObserver registers (or clears, with nil) the observer that receives
// this terminal's capture events. See CaptureObserver. It lives on the buffer —
// most events (line-off, and the live-screen events to come) originate there —
// so this delegates; a nil buffer (never, for a NewParser) is a no-op.
func (p *Parser) SetCaptureObserver(o CaptureObserver) {
	if p.buffer != nil {
		p.buffer.SetCaptureObserver(o)
	}
}

// Parse processes input data and updates the terminal buffer
func (p *Parser) Parse(data []byte) {
	// The raw tee: the literal bytes as received, before parsing. This is the
	// `raw` capture rung, and it fires for every consumer of the parser (the
	// standalone CLI's read loop, an embedded host) alike.
	if p.buffer != nil && p.buffer.captureObserver != nil {
		p.buffer.captureObserver.OnOutput(data)
	}
	for _, b := range data {
		p.processByte(b)
	}
	// Flush any accumulated live write-run at the end of the feed so a
	// partial run isn't held across Parse calls.
	if p.buffer != nil && p.buffer.liveEnabled() {
		p.buffer.flushLiveWriteRun()
	}
}

// ParseString processes a string and updates the terminal buffer
func (p *Parser) ParseString(data string) {
	p.Parse([]byte(data))
}

func (p *Parser) processByte(b byte) {
	// Handle UTF-8 continuation bytes
	if p.utf8Need > 0 {
		if b&0xC0 == 0x80 {
			p.utf8Buf = append(p.utf8Buf, b)
			p.utf8Need--
			if p.utf8Need == 0 {
				// Complete UTF-8 sequence
				r := decodeUTF8(p.utf8Buf)
				if p.state == stateGround {
					p.buffer.WriteChar(r)
				}
				p.utf8Buf = p.utf8Buf[:0]
			}
			return
		}
		// Invalid UTF-8, reset
		p.utf8Buf = p.utf8Buf[:0]
		p.utf8Need = 0
	}

	// Check for UTF-8 start bytes in ground state
	if p.state == stateGround {
		if b&0xE0 == 0xC0 {
			// 2-byte sequence
			p.utf8Buf = append(p.utf8Buf[:0], b)
			p.utf8Need = 1
			return
		} else if b&0xF0 == 0xE0 {
			// 3-byte sequence
			p.utf8Buf = append(p.utf8Buf[:0], b)
			p.utf8Need = 2
			return
		} else if b&0xF8 == 0xF0 {
			// 4-byte sequence
			p.utf8Buf = append(p.utf8Buf[:0], b)
			p.utf8Need = 3
			return
		}
	}

	switch p.state {
	case stateGround:
		p.handleGround(b)
	case stateEscape:
		p.handleEscape(b)
	case stateCSI, stateCSIParam:
		p.handleCSI(b)
	case stateOSC:
		p.handleOSC(b)
	case stateOSCString:
		p.handleOSCString(b)
	case stateOSCEsc:
		p.handleOSCEsc(b)
	case stateCharset:
		// Consume one character and return to ground
		p.state = stateGround
	case stateDECLineAttr:
		p.handleDECLineAttr(b)
	case stateDCS:
		p.handleDCS(b)
	case stateDCSEsc:
		p.handleDCSEsc(b)
	}
}

func decodeUTF8(buf []byte) rune {
	if len(buf) == 0 {
		return 0xFFFD
	}
	switch len(buf) {
	case 2:
		return rune(buf[0]&0x1F)<<6 | rune(buf[1]&0x3F)
	case 3:
		return rune(buf[0]&0x0F)<<12 | rune(buf[1]&0x3F)<<6 | rune(buf[2]&0x3F)
	case 4:
		return rune(buf[0]&0x07)<<18 | rune(buf[1]&0x3F)<<12 | rune(buf[2]&0x3F)<<6 | rune(buf[3]&0x3F)
	default:
		return 0xFFFD
	}
}

func (p *Parser) handleGround(b byte) {
	switch b {
	case 0x00: // NUL - ignore
	case 0x07: // BEL - bell (ignore for now)
	case 0x08: // BS - backspace
		p.buffer.Backspace()
	case 0x09: // HT - horizontal tab
		p.buffer.TabForward(1)
	case 0x0A: // LF - line feed
		p.buffer.LineFeed()
	case 0x0B, 0x0C: // VT, FF - treated as line feed
		p.buffer.LineFeed()
	case 0x0D: // CR - carriage return
		p.buffer.CarriageReturn()
	case 0x1B: // ESC
		p.state = stateEscape
	default:
		if b >= 0x20 && b < 0x7F {
			// Printable ASCII
			p.buffer.WriteChar(rune(b))
		}
	}
}

func (p *Parser) handleEscape(b byte) {
	switch b {
	case '[': // CSI - Control Sequence Introducer
		p.state = stateCSI
		p.csiParams = p.csiParams[:0]
		p.csiRawParams = p.csiRawParams[:0]
		p.csiPrivate = 0
		p.csiIntermediate = 0
		p.csiBuf.Reset()
	case ']': // OSC - Operating System Command
		p.state = stateOSC
		p.oscBuf.Reset()
	case 'P': // DCS - Device Control String (ESC P ... ST)
		p.state = stateDCS
		p.dcsBuf.Reset()
	case '(', ')': // Character set designation
		p.state = stateCharset
	case '#': // DEC line attribute commands (DECDHL, DECDWL, DECSWL, DECALN)
		p.state = stateDECLineAttr
	case 'H': // HTS - Horizontal Tab Set (set a tab stop at the cursor)
		p.buffer.SetTabStop()
		p.state = stateGround
	case '7': // DECSC - Save Cursor
		p.buffer.SaveCursor()
		p.state = stateGround
	case '8': // DECRC - Restore Cursor
		p.buffer.RestoreCursor()
		p.state = stateGround
	case 'c': // RIS - Reset to Initial State
		p.buffer.LeaveAltScreen() // a hard reset returns to the primary screen
		p.buffer.ClearScreen()
		p.buffer.SetCursor(0, 0)
		p.buffer.ResetAttributes()
		p.buffer.ResetScrollRegion()
		p.buffer.resetTabStops()
		p.buffer.SetInsertMode(false)
		p.buffer.SetNewLineMode(false)
		p.buffer.resetKeyModes()
		p.buffer.resetOSCColors()
		p.buffer.SetProtectedAttr(false)
		p.state = stateGround
	case 'D': // IND - Index (down one line; scrolls the region at the bottom margin)
		p.buffer.LineFeed()
		p.state = stateGround
	case 'E': // NEL - Next Line
		p.buffer.CarriageReturn()
		p.buffer.LineFeed()
		p.state = stateGround
	case 'M': // RI - Reverse Index (up one line; reverse-scrolls at the top margin)
		p.buffer.ReverseIndex()
		p.state = stateGround
	case '=': // DECKPAM - Keypad Application Mode
		p.buffer.SetApplicationKeypad(true)
		p.state = stateGround
	case '>': // DECKPNM - Keypad Numeric Mode
		p.buffer.SetApplicationKeypad(false)
		p.state = stateGround
	default:
		// Unknown escape sequence, return to ground state
		p.state = stateGround
	}
}

// handleDECLineAttr handles ESC # sequences for line attributes
// ESC#3 - DECDHL: Double-height line, top half
// ESC#4 - DECDHL: Double-height line, bottom half
// ESC#5 - DECSWL: Single-width line (normal)
// ESC#6 - DECDWL: Double-width line
// ESC#8 - DECALN: Screen alignment test (fill screen with 'E')
func (p *Parser) handleDECLineAttr(b byte) {
	switch b {
	case '3': // DECDHL top half
		p.buffer.SetLineAttribute(LineAttrDoubleTop)
	case '4': // DECDHL bottom half
		p.buffer.SetLineAttribute(LineAttrDoubleBottom)
	case '5': // DECSWL - single width (normal)
		p.buffer.SetLineAttribute(LineAttrNormal)
	case '6': // DECDWL - double width
		p.buffer.SetLineAttribute(LineAttrDoubleWidth)
	case '8': // DECALN - Screen alignment test (fill with 'E')
		cols, rows := p.buffer.GetSize()
		for y := 0; y < rows; y++ {
			p.buffer.SetCursor(0, y)
			p.buffer.SetLineAttribute(LineAttrNormal)
			for x := 0; x < cols; x++ {
				p.buffer.WriteChar('E')
			}
		}
		p.buffer.SetCursor(0, 0)
	}
	p.state = stateGround
}

func (p *Parser) handleCSI(b byte) {
	if p.state == stateCSI {
		// First byte after ESC [. The private parameter prefixes are the
		// whole 0x3C-0x3F range -- '<', '=', '>', '?' -- per ECMA-48. All
		// four have to be RECOGNIZED even where the sequence itself is not
		// supported: an unrecognized prefix falls through to the final-byte
		// branch below, which ends the sequence on the prefix and prints the
		// parameters that follow as text.
		//
		// '=' is the one that bites. A TUI resets the Kitty keyboard protocol
		// on the way out with CSI = 0 ; 1 u, and a terminal that does not know
		// '=' is a prefix leaves "0;1u" sitting on the screen as the program
		// exits.
		//
		// '!' (0x21) is an intermediate byte rather than a parameter prefix,
		// but CSI ! p (DECSTR) carries it in the prefix position, so it stays.
		if b == '!' || (b >= 0x3C && b <= 0x3F) {
			p.csiPrivate = b
			p.state = stateCSIParam
			return
		}
		p.state = stateCSIParam
	}

	// Collect parameter bytes
	if b >= '0' && b <= '9' {
		p.csiBuf.WriteByte(b)
		return
	}

	if b == ';' {
		// Parameter separator
		p.parseCSIParam()
		p.csiBuf.Reset()
		return
	}

	if b == ':' {
		// Sub-parameter separator (used in some SGR sequences)
		p.csiBuf.WriteByte(b)
		return
	}

	// Intermediate bytes (0x20-0x2F) - used in sequences like DECSCUSR (ESC [ Ps SP q)
	if b >= 0x20 && b <= 0x2F {
		p.parseCSIParam() // Parse any parameter before the intermediate
		p.csiIntermediate = b
		return
	}

	// Final byte - execute the sequence
	p.parseCSIParam() // Parse any remaining parameter
	p.executeCSI(b)
	p.state = stateGround
}

func (p *Parser) parseCSIParam() {
	s := p.csiBuf.String()
	if s == "" {
		p.csiParams = append(p.csiParams, 0) // Default value
		p.csiRawParams = append(p.csiRawParams, "")
	} else {
		// Store raw string for subparameter parsing
		p.csiRawParams = append(p.csiRawParams, s)
		// For legacy int params, extract base value (before any colon)
		base := s
		if colonIdx := strings.IndexByte(s, ':'); colonIdx >= 0 {
			base = s[:colonIdx]
		}
		n, _ := strconv.Atoi(base)
		p.csiParams = append(p.csiParams, n)
	}
}

// parseSGRParam parses a raw parameter string into an SGRParam with subparameters
func parseSGRParam(raw string) SGRParam {
	if raw == "" {
		return SGRParam{Base: 0}
	}
	parts := strings.Split(raw, ":")
	base, _ := strconv.Atoi(parts[0])
	var subs []int
	for i := 1; i < len(parts); i++ {
		if parts[i] == "" {
			// Empty subparameter (e.g., "58:2::255:0:0" has empty colorspace)
			subs = append(subs, -1) // Use -1 to indicate empty/default
		} else {
			n, _ := strconv.Atoi(parts[i])
			subs = append(subs, n)
		}
	}
	return SGRParam{Base: base, Subs: subs}
}

func (p *Parser) getParam(idx, defaultVal int) int {
	if idx < len(p.csiParams) && p.csiParams[idx] > 0 {
		return p.csiParams[idx]
	}
	return defaultVal
}

func (p *Parser) executeCSI(finalByte byte) {
	switch finalByte {
	case 'A': // CUU - Cursor Up
		p.buffer.MoveCursorUp(p.getParam(0, 1))

	case 'B': // CUD - Cursor Down
		p.buffer.MoveCursorDown(p.getParam(0, 1))

	case 'C': // CUF - Cursor Forward
		p.buffer.MoveCursorForwardVisual(p.getParam(0, 1))

	case 'D': // CUB - Cursor Backward
		p.buffer.MoveCursorBackwardVisual(p.getParam(0, 1))

	case 'E': // CNL - Cursor Next Line
		p.buffer.MoveCursorDown(p.getParam(0, 1))
		p.buffer.CarriageReturn()

	case 'F': // CPL - Cursor Previous Line
		p.buffer.MoveCursorUp(p.getParam(0, 1))
		p.buffer.CarriageReturn()

	case 'G': // CHA - Cursor Horizontal Absolute
		x := p.getParam(0, 1) - 1 // 1-indexed to 0-indexed
		_, y := p.buffer.GetCursor()
		p.buffer.SetCursorVisual(x, y)

	case 'H', 'f': // CUP/HVP - Cursor Position (origin-mode aware)
		row := p.getParam(0, 1) - 1
		col := p.getParam(1, 1) - 1
		p.buffer.SetCursorPosition(col, row)

	case 'J': // ED - Erase in Display / DECSED (CSI ? Ps J - selective)
		if p.csiPrivate == '?' {
			p.buffer.SelectiveEraseDisplay(p.getParam(0, 0))
			break
		}
		switch p.getParam(0, 0) {
		case 0:
			p.buffer.ClearToEndOfScreen()
		case 1:
			p.buffer.ClearToStartOfScreen()
		case 2, 3:
			p.buffer.ClearScreen()
			p.buffer.SetCursor(0, 0)
		}

	case 'K': // EL - Erase in Line / DECSEL (CSI ? Ps K - selective)
		if p.csiPrivate == '?' {
			p.buffer.SelectiveEraseLine(p.getParam(0, 0))
			break
		}
		switch p.getParam(0, 0) {
		case 0:
			p.buffer.ClearToEndOfLine()
		case 1:
			p.buffer.ClearToStartOfLine()
		case 2:
			p.buffer.ClearLine()
		}

	case 'L': // IL - Insert Lines
		p.buffer.InsertLines(p.getParam(0, 1))

	case 'M': // DL - Delete Lines
		p.buffer.DeleteLines(p.getParam(0, 1))

	case 'P': // DCH - Delete Characters
		p.buffer.DeleteChars(p.getParam(0, 1))

	case '@': // ICH - Insert Characters
		p.buffer.InsertChars(p.getParam(0, 1))

	case 'X': // ECH - Erase Characters
		p.buffer.EraseChars(p.getParam(0, 1))

	case 'S': // SU - Scroll Up
		p.buffer.ScrollUp(p.getParam(0, 1))

	case 'T': // SD - Scroll Down
		p.buffer.ScrollDown(p.getParam(0, 1))

	case 'd': // VPA - Vertical Position Absolute
		y := p.getParam(0, 1) - 1
		x, _ := p.buffer.GetCursor()
		p.buffer.SetCursor(x, y)

	case 'e': // VPR - Vertical Position Relative (down n rows)
		p.buffer.MoveCursorDown(p.getParam(0, 1))

	case '`': // HPA - Horizontal Position Absolute (visual column)
		x := p.getParam(0, 1) - 1
		_, y := p.buffer.GetCursor()
		p.buffer.SetCursorVisual(x, y)

	case 'a': // HPR - Horizontal Position Relative (forward n columns)
		p.buffer.MoveCursorForwardVisual(p.getParam(0, 1))

	case 'b': // REP - Repeat the last printed character n times
		p.buffer.RepeatLastChar(p.getParam(0, 1))

	case 'g': // TBC - Tab Clear (0 = at cursor, 3 = all)
		p.buffer.ClearTabStop(p.getParam(0, 0))

	case 'I': // CHT - Cursor Forward Tabulation (n tab stops)
		p.buffer.TabForward(p.getParam(0, 1))

	case 'Z': // CBT - Cursor Backward Tabulation (n tab stops)
		p.buffer.TabBackward(p.getParam(0, 1))

	case 'm': // SGR - Select Graphic Rendition
		p.executeSGR()

	case 'h': // SM - Set Mode
		if p.csiPrivate == '?' {
			p.executePrivateModeSet(true)
		} else if p.csiPrivate == 0 {
			p.executeModeSet(true)
		}

	case 'l': // RM - Reset Mode
		if p.csiPrivate == '?' {
			p.executePrivateModeSet(false)
		} else if p.csiPrivate == 0 {
			p.executeModeSet(false)
		}

	case 's': // XTSAVE (CSI ? Pm s) / DECSLRM (CSI Pl;Pr s under DECLRMM) / SCP
		if p.csiPrivate == '?' {
			p.executeXTSAVE()
		} else if p.csiPrivate == 0 && p.buffer.IsLeftRightMarginMode() {
			p.buffer.SetLeftRightMargins(p.getParam(0, 1), p.getParam(1, 0))
		} else {
			p.buffer.SaveCursor()
		}

	case 'u': // RCP - Restore Cursor Position
		p.buffer.RestoreCursor()

	case 'n': // DSR - Device Status Report
		p.executeDSR()

	case 'r': // DECSTBM (CSI Pt;Pb r) / XTRESTORE (CSI ? Pm r)
		if p.csiPrivate == '?' {
			p.executeXTRESTORE()
		} else if p.csiPrivate == 0 {
			p.buffer.SetScrollRegion(p.getParam(0, 1), p.getParam(1, 0))
		}

	case 'c': // DA - Device Attributes
		p.executeDA()

	case 't': // Window manipulation
		p.executeWindowManipulation()

	case 'p': // DECRQM (CSI ? Ps $ p) / DECSTR (CSI ! p)
		if p.csiPrivate == '?' && p.csiIntermediate == '$' {
			p.executeDECRQM()
		} else if p.csiPrivate == '!' {
			// DECSTR - Soft Terminal Reset: clear the scroll region and origin
			// mode, reset attributes, and home the cursor (screen not cleared).
			p.buffer.ResetScrollRegion()
			p.buffer.ResetAttributes()
			p.buffer.SetInsertMode(false)
			p.buffer.SetProtectedAttr(false)
			p.buffer.SetCursor(0, 0)
		}

	case 'q': // DECSCUSR (SP q) / DECSCA (" q - select character protection)
		if p.csiIntermediate == ' ' {
			p.executeDECSCUSR()
		} else if p.csiIntermediate == '"' {
			// DECSCA: Ps 1 = protected, 0/2 = not protected.
			p.buffer.SetProtectedAttr(p.getParam(0, 0) == 1)
		}
	}
}

// executeWindowManipulation handles ESC [ Ps ; Ps ; Ps t - Window manipulation
// We specifically handle ESC [ 8 ; rows ; cols t to set logical screen size
// Custom extensions:
//
//	ESC [ 9 ; 40 ; 0 t - Disable 40-column mode
//	ESC [ 9 ; 40 ; 1 t - Enable 40-column mode
//	ESC [ 9 ; 25 t - Set line density to 25 (also: 30, 43, 50, 60)
func (p *Parser) executeWindowManipulation() {
	if len(p.csiParams) == 0 {
		return
	}

	cmd := p.csiParams[0]
	switch cmd {
	case 14: // Report text area size in pixels: CSI 4 ; height ; width t
		if p.responseSink != nil {
			cw, ch := p.buffer.GetCellPixelSize()
			cols, rows := p.buffer.GetEffectiveSize()
			p.responseSink([]byte(fmt.Sprintf("\x1b[4;%d;%dt", rows*ch, cols*cw)))
		}
	case 16: // Report cell size in pixels: CSI 6 ; height ; width t
		if p.responseSink != nil {
			cw, ch := p.buffer.GetCellPixelSize()
			p.responseSink([]byte(fmt.Sprintf("\x1b[6;%d;%dt", ch, cw)))
		}
	case 18: // Report text area size in chars: CSI 8 ; rows ; cols t
		if p.responseSink != nil {
			cols, rows := p.buffer.GetEffectiveSize()
			p.responseSink([]byte(fmt.Sprintf("\x1b[8;%d;%dt", rows, cols)))
		}
	case 8: // ESC [ 8 ; rows ; cols t - Set terminal size
		// Get parameters (0 or omitted means "use physical/current")
		rows := 0
		cols := 0
		if len(p.csiParams) > 1 {
			rows = p.csiParams[1]
		}
		if len(p.csiParams) > 2 {
			cols = p.csiParams[2]
		}
		p.buffer.SetLogicalSize(rows, cols)

	case 9: // Custom PurfecTerm extensions
		if len(p.csiParams) < 2 {
			return
		}
		subCmd := p.csiParams[1]
		switch subCmd {
		case 40: // 40-column mode toggle
			// ESC [ 9 ; 40 ; 0 t = disable, ESC [ 9 ; 40 ; 1 t = enable
			enabled := false
			if len(p.csiParams) > 2 && p.csiParams[2] != 0 {
				enabled = true
			}
			p.buffer.Set40ColumnMode(enabled)
		case 25, 30, 43, 50, 60: // Line density
			// ESC [ 9 ; density t
			p.buffer.SetLineDensity(subCmd)
		}

		// Other window manipulation commands could be added here
		// case 1: De-iconify window
		// case 2: Iconify window
		// case 3: Move window
		// case 4: Resize window in pixels
		// etc.
	}
}

// executeDECRQM answers a DEC Request Mode query (CSI ? Ps $ p) with a report
// (CSI ? Ps ; status $ y). status: 0 = not recognized, 1 = set, 2 = reset,
// 3 = permanently set, 4 = permanently reset. Only modes PurfecTerm actually
// implements report a non-zero status; the set is easily extended.
func (p *Parser) executeDECRQM() {
	if p.responseSink == nil || len(p.csiParams) == 0 {
		return
	}
	mode := p.csiParams[0]
	p.responseSink([]byte(fmt.Sprintf("\x1b[?%d;%d$y", mode, p.decrqmStatus(mode))))
}

func (p *Parser) decrqmStatus(mode int) int {
	set := func(b bool) int {
		if b {
			return 1
		}
		return 2
	}
	switch mode {
	case 1: // DECCKM - Application cursor keys
		return set(p.buffer.IsApplicationCursorKeys())
	case 3: // DECCOLM - 132-column mode
		return set(p.buffer.Get132ColumnMode())
	case 5: // DECSCNM - reverse video (light mode)
		return set(!p.buffer.IsDarkTheme())
	case 6: // DECOM - Origin Mode
		return set(p.buffer.IsOriginMode())
	case 7: // DECAWM - Auto-wrap mode
		return set(p.buffer.IsAutoWrapModeEnabled())
	case 25: // DECTCEM - Cursor visibility
		return set(p.buffer.IsCursorVisible())
	case 69: // DECLRMM - Left/Right Margin Mode
		return set(p.buffer.IsLeftRightMarginMode())
	case 80: // DECSDM - Sixel Display Mode
		return set(p.buffer.IsSixelDisplayMode())
	case 8452: // Sixel scrolling
		return set(p.buffer.IsSixelScrolling())
	case 1004: // Focus reporting
		return set(p.buffer.IsFocusReporting())
	case 1007: // Alternate scroll mode
		return set(p.buffer.IsAltScrollMode())
	case 47, 1047, 1049: // Alternate screen buffer
		return set(p.buffer.IsAltScreen())
	case 1000, 1002, 1003:
		return set(p.buffer.GetMouseTrackingMode() == mode)
	case 1006:
		enc := p.buffer.GetMouseEncodingMode()
		return set(enc == 1006 || enc == 1016) // ?1016 implies SGR
	case 1016:
		return set(p.buffer.GetMouseEncodingMode() == 1016)
	case 2027:
		return 3 // permanently set: PurfecTerm always grapheme-clusters
	default:
		return 0 // not recognized
	}
}

// executeDECSCUSR handles ESC [ Ps SP q - Set Cursor Style
func (p *Parser) executeDECSCUSR() {
	style := p.getParam(0, 1)
	// Ps = 0, 1: Blinking block
	// Ps = 2: Steady block
	// Ps = 3: Blinking underline
	// Ps = 4: Steady underline
	// Ps = 5: Blinking bar
	// Ps = 6: Steady bar
	var shape, blink int
	switch style {
	case 0, 1: // Blinking block (default)
		shape, blink = 0, 1
	case 2: // Steady block
		shape, blink = 0, 0
	case 3: // Blinking underline
		shape, blink = 1, 1
	case 4: // Steady underline
		shape, blink = 1, 0
	case 5: // Blinking bar
		shape, blink = 2, 1
	case 6: // Steady bar
		shape, blink = 2, 0
	default:
		shape, blink = 0, 1 // Default to blinking block
	}
	p.buffer.SetCursorStyle(shape, blink)
}

func (p *Parser) executeSGR() {
	if len(p.csiParams) == 0 {
		p.buffer.ResetAttributes()
		return
	}

	i := 0
	for i < len(p.csiParams) {
		param := p.csiParams[i]
		switch param {
		case 0: // Reset
			p.buffer.ResetAttributes()
		case 1: // Bold
			p.buffer.SetBold(true)
		case 2: // Dim (treat as not bold)
			p.buffer.SetBold(false)
		case 3: // Italic
			p.buffer.SetItalic(true)
		case 4: // Underline (with optional subparameter for style)
			// Check for subparameters: 4:0=off, 4:1=single, 4:2=double, 4:3=curly, 4:4=dotted, 4:5=dashed
			if i < len(p.csiRawParams) {
				sgr := parseSGRParam(p.csiRawParams[i])
				if len(sgr.Subs) > 0 {
					switch sgr.Subs[0] {
					case 0:
						p.buffer.SetUnderlineStyle(UnderlineNone)
					case 1:
						p.buffer.SetUnderlineStyle(UnderlineSingle)
					case 2:
						p.buffer.SetUnderlineStyle(UnderlineDouble)
					case 3:
						p.buffer.SetUnderlineStyle(UnderlineCurly)
					case 4:
						p.buffer.SetUnderlineStyle(UnderlineDotted)
					case 5:
						p.buffer.SetUnderlineStyle(UnderlineDashed)
					default:
						p.buffer.SetUnderlineStyle(UnderlineSingle)
					}
				} else {
					// Plain SGR 4 = single underline
					p.buffer.SetUnderlineStyle(UnderlineSingle)
				}
			} else {
				p.buffer.SetUnderlineStyle(UnderlineSingle)
			}
		case 5, 6: // Blink (slow=5, rapid=6) - rendered as bobbing wave animation
			p.buffer.SetBlink(true)
		case 7: // Reverse video
			p.buffer.SetReverse(true)
		case 9: // Strikethrough
			p.buffer.SetStrikethrough(true)
		case 10: // Primary font (font slot 0)
			p.buffer.SetFont(0)
		case 11, 12, 13, 14, 15, 16, 17, 18, 19: // Alternate fonts 1..9 (slots 1..9)
			p.buffer.SetFont(int(param) - 10)
		case 20: // Fraktur / gothic (font slot 10)
			p.buffer.SetFont(10)
		case 21: // Bold off (double underline in some terminals)
			p.buffer.SetBold(false)
		case 22: // Normal intensity
			p.buffer.SetBold(false)
		case 23: // Italic off
			p.buffer.SetItalic(false)
		case 24: // Underline off
			p.buffer.SetUnderlineStyle(UnderlineNone)
		case 25: // Blink off
			p.buffer.SetBlink(false)
		case 27: // Reverse off
			p.buffer.SetReverse(false)
		case 29: // Strikethrough off
			p.buffer.SetStrikethrough(false)

		// Foreground colors (30-37)
		case 30, 31, 32, 33, 34, 35, 36, 37:
			p.buffer.SetForeground(StandardColor(param - 30))

		// Bright foreground colors (90-97)
		case 90, 91, 92, 93, 94, 95, 96, 97:
			p.buffer.SetForeground(StandardColor(param - 90 + 8))

		// Background colors (40-47)
		case 40, 41, 42, 43, 44, 45, 46, 47:
			p.buffer.SetBackground(StandardColor(param - 40))

		// Bright background colors (100-107)
		case 100, 101, 102, 103, 104, 105, 106, 107:
			p.buffer.SetBackground(StandardColor(param - 100 + 8))

		case 38: // Extended foreground color
			// Check for subparameter format first: 38:5:N or 38:2::R:G:B
			if i < len(p.csiRawParams) {
				sgr := parseSGRParam(p.csiRawParams[i])
				if len(sgr.Subs) >= 2 && sgr.Subs[0] == 5 {
					// Subparam format: 38:5:N
					p.buffer.SetForeground(PaletteColor(sgr.Subs[1]))
				} else if len(sgr.Subs) >= 4 && sgr.Subs[0] == 2 {
					// Subparam format: 38:2:[colorspace]:R:G:B (colorspace is often empty/-1)
					// subs[0]=2, subs[1]=colorspace/-1, subs[2]=R, subs[3]=G, subs[4]=B
					r, g, b := 0, 0, 0
					if len(sgr.Subs) >= 5 {
						r, g, b = sgr.Subs[2], sgr.Subs[3], sgr.Subs[4]
					} else {
						// No colorspace: 38:2:R:G:B
						r, g, b = sgr.Subs[1], sgr.Subs[2], sgr.Subs[3]
					}
					p.buffer.SetForeground(TrueColor(uint8(r), uint8(g), uint8(b)))
				} else if i+2 < len(p.csiParams) && p.csiParams[i+1] == 5 {
					// Semicolon format: 38;5;N
					p.buffer.SetForeground(PaletteColor(p.csiParams[i+2]))
					i += 2
				} else if i+4 < len(p.csiParams) && p.csiParams[i+1] == 2 {
					// Semicolon format: 38;2;R;G;B
					p.buffer.SetForeground(TrueColor(
						uint8(p.csiParams[i+2]),
						uint8(p.csiParams[i+3]),
						uint8(p.csiParams[i+4]),
					))
					i += 4
				}
			} else if i+2 < len(p.csiParams) && p.csiParams[i+1] == 5 {
				// Fallback semicolon format: 38;5;N
				p.buffer.SetForeground(PaletteColor(p.csiParams[i+2]))
				i += 2
			} else if i+4 < len(p.csiParams) && p.csiParams[i+1] == 2 {
				// Fallback semicolon format: 38;2;R;G;B
				p.buffer.SetForeground(TrueColor(
					uint8(p.csiParams[i+2]),
					uint8(p.csiParams[i+3]),
					uint8(p.csiParams[i+4]),
				))
				i += 4
			}

		case 39: // Default foreground
			p.buffer.SetForeground(DefaultForeground)

		case 48: // Extended background color
			// Check for subparameter format first: 48:5:N or 48:2::R:G:B
			if i < len(p.csiRawParams) {
				sgr := parseSGRParam(p.csiRawParams[i])
				if len(sgr.Subs) >= 2 && sgr.Subs[0] == 5 {
					// Subparam format: 48:5:N
					p.buffer.SetBackground(PaletteColor(sgr.Subs[1]))
				} else if len(sgr.Subs) >= 4 && sgr.Subs[0] == 2 {
					// Subparam format: 48:2:[colorspace]:R:G:B
					r, g, b := 0, 0, 0
					if len(sgr.Subs) >= 5 {
						r, g, b = sgr.Subs[2], sgr.Subs[3], sgr.Subs[4]
					} else {
						r, g, b = sgr.Subs[1], sgr.Subs[2], sgr.Subs[3]
					}
					p.buffer.SetBackground(TrueColor(uint8(r), uint8(g), uint8(b)))
				} else if i+2 < len(p.csiParams) && p.csiParams[i+1] == 5 {
					// Semicolon format: 48;5;N
					p.buffer.SetBackground(PaletteColor(p.csiParams[i+2]))
					i += 2
				} else if i+4 < len(p.csiParams) && p.csiParams[i+1] == 2 {
					// Semicolon format: 48;2;R;G;B
					p.buffer.SetBackground(TrueColor(
						uint8(p.csiParams[i+2]),
						uint8(p.csiParams[i+3]),
						uint8(p.csiParams[i+4]),
					))
					i += 4
				}
			} else if i+2 < len(p.csiParams) && p.csiParams[i+1] == 5 {
				// Fallback semicolon format: 48;5;N
				p.buffer.SetBackground(PaletteColor(p.csiParams[i+2]))
				i += 2
			} else if i+4 < len(p.csiParams) && p.csiParams[i+1] == 2 {
				// Fallback semicolon format: 48;2;R;G;B
				p.buffer.SetBackground(TrueColor(
					uint8(p.csiParams[i+2]),
					uint8(p.csiParams[i+3]),
					uint8(p.csiParams[i+4]),
				))
				i += 4
			}

		case 49: // Default background
			p.buffer.SetBackground(DefaultBackground)

		case 58: // Underline color
			// Check for subparameter format: 58:5:N or 58:2::R:G:B
			if i < len(p.csiRawParams) {
				sgr := parseSGRParam(p.csiRawParams[i])
				if len(sgr.Subs) >= 2 && sgr.Subs[0] == 5 {
					// Subparam format: 58:5:N (256-color)
					p.buffer.SetUnderlineColor(PaletteColor(sgr.Subs[1]))
				} else if len(sgr.Subs) >= 4 && sgr.Subs[0] == 2 {
					// Subparam format: 58:2:[colorspace]:R:G:B
					r, g, b := 0, 0, 0
					if len(sgr.Subs) >= 5 {
						r, g, b = sgr.Subs[2], sgr.Subs[3], sgr.Subs[4]
					} else {
						r, g, b = sgr.Subs[1], sgr.Subs[2], sgr.Subs[3]
					}
					p.buffer.SetUnderlineColor(TrueColor(uint8(r), uint8(g), uint8(b)))
				}
			}

		case 59: // Reset underline color (use foreground color)
			p.buffer.ResetUnderlineColor()

		// Custom glyph system - flip attributes
		case 150: // Reset XFlip
			p.buffer.SetXFlip(false)
		case 151: // XFlip on
			p.buffer.SetXFlip(true)
		case 152: // Reset YFlip
			p.buffer.SetYFlip(false)
		case 153: // YFlip on
			p.buffer.SetYFlip(true)

		// Base Glyph Palette (BGP)
		case 158: // Set BGP: ESC[158;Nm
			if i+1 < len(p.csiParams) {
				p.buffer.SetBGP(p.csiParams[i+1])
				i++
			}
		case 159: // Reset BGP to default
			p.buffer.ResetBGP()
		}
		i++
	}
}

// executeModeSet handles the non-private ANSI modes (SM/RM without '?').
func (p *Parser) executeModeSet(set bool) {
	for _, param := range p.csiParams {
		switch param {
		case 4: // IRM - Insert/Replace Mode
			p.buffer.SetInsertMode(set)
		case 20: // LNM - Line Feed/New Line Mode
			p.buffer.SetNewLineMode(set)
		}
	}
}

func (p *Parser) executePrivateModeSet(set bool) {
	for _, param := range p.csiParams {
		switch param {
		case 3: // DECCOLM - 132 Column Mode (horizontal scale 0.6060)
			p.buffer.Set132ColumnMode(set)
		case 5: // DECSCNM - Screen Mode (reverse video)
			// h = reverse video (light mode), l = normal video (dark mode)
			p.buffer.SetDarkTheme(!set)
		case 6: // DECOM - Origin Mode (cursor addressing relative to scroll region)
			p.buffer.SetOriginMode(set)
		case 69: // DECLRMM - Left/Right Margin Mode (enables DECSLRM)
			p.buffer.SetLeftRightMarginMode(set)
		case 80: // DECSDM - Sixel Display Mode
			p.buffer.SetSixelDisplayMode(set)
		case 8452: // Sixel scrolling (cursor lands below the image)
			p.buffer.SetSixelScrolling(set)
		case 25: // DECTCEM - Cursor visibility
			p.buffer.SetCursorVisible(set)
		case 47, 1047, 1049: // Alternate screen buffer
			// All three enter/leave the alternate screen. In this model the alt
			// screen is fresh-on-entry and discarded on leave, and the primary
			// context (including its cursor) is stashed and restored — so the
			// ?1049 save/restore-cursor semantics fall out, and ?47/?1047 get the
			// same clean isolation.
			if set {
				p.buffer.EnterAltScreen()
			} else {
				p.buffer.LeaveAltScreen()
			}
		case 1000: // X11 Normal Mouse Tracking (button press/release)
			if set {
				p.buffer.SetMouseTrackingMode(1000)
			} else {
				p.buffer.SetMouseTrackingMode(0)
			}
		case 1002: // Cell Motion Mouse Tracking (press/release + motion while button down)
			if set {
				p.buffer.SetMouseTrackingMode(1002)
			} else {
				p.buffer.SetMouseTrackingMode(0)
			}
		case 1003: // All Motion Mouse Tracking (all motion events)
			if set {
				p.buffer.SetMouseTrackingMode(1003)
			} else {
				p.buffer.SetMouseTrackingMode(0)
			}
		case 1006: // SGR Extended Mouse Encoding (cell coordinates)
			// Don't clobber the pixel refinement (?1016) if it is active; a
			// bare ?1006 reset only clears SGR cells.
			if set {
				if p.buffer.GetMouseEncodingMode() != 1016 {
					p.buffer.SetMouseEncodingMode(1006)
				}
			} else if p.buffer.GetMouseEncodingMode() == 1006 {
				p.buffer.SetMouseEncodingMode(0)
			}
		case 1016: // SGR-Pixels Mouse Encoding (coordinates in pixels)
			// A refinement of ?1006: same wire format, pixel coordinates.
			// Resetting it falls back to SGR cells (?1006 semantics), the sane
			// default since a program that asked for pixels wanted SGR at least.
			if set {
				p.buffer.SetMouseEncodingMode(1016)
			} else if p.buffer.GetMouseEncodingMode() == 1016 {
				p.buffer.SetMouseEncodingMode(1006)
			}
		case 2004: // Bracketed paste mode
			p.buffer.SetBracketedPasteMode(set)
		case 2027: // terminal-wg grapheme clustering: accepted, inherently satisfied.
			// PurfecTerm always clusters combining marks (appendCombiningMark) and
			// the default STANDARD contract already advances the cursor by visual
			// column width — exactly what a mode-2027 probe asks for. There is no
			// state to toggle; report it set under DECRQM when that lands. Flex
			// mode moved to the private ?7027 to avoid colliding with this.
		case 7027: // PurfecTerm: Flexible East Asian Width mode (Contract B opt-in)
			p.buffer.SetFlexWidthMode(set)
		case 7028: // PurfecTerm: Visual width-based line wrapping
			p.buffer.SetVisualWidthWrap(set)
		case 7029: // PurfecTerm: Ambiguous width: narrow (1.0)
			if set {
				p.buffer.SetAmbiguousWidthMode(AmbiguousWidthNarrow)
			} else {
				// Turning off narrow - check if wide is set, otherwise auto
				if p.buffer.GetAmbiguousWidthMode() == AmbiguousWidthNarrow {
					p.buffer.SetAmbiguousWidthMode(AmbiguousWidthAuto)
				}
			}
		case 7030: // PurfecTerm: Ambiguous width: wide (2.0)
			if set {
				p.buffer.SetAmbiguousWidthMode(AmbiguousWidthWide)
			} else {
				// Turning off wide - check if narrow is set, otherwise auto
				if p.buffer.GetAmbiguousWidthMode() == AmbiguousWidthWide {
					p.buffer.SetAmbiguousWidthMode(AmbiguousWidthAuto)
				}
			}
		case 1: // DECCKM - Application cursor keys
			p.buffer.SetApplicationCursorKeys(set)
		case 1004: // Focus reporting
			p.buffer.SetFocusReporting(set)
		case 1007: // Alternate scroll mode (wheel -> arrows on the alt screen)
			p.buffer.SetAltScrollMode(set)
		case 7: // DECAWM - Auto-wrap mode
			// h = enable auto-wrap (cursor wraps to next line), l = disable (stay at last column)
			p.buffer.SetAutoWrapMode(set)
		case 12: // Cursor blink rate: h=fast, l=slow
			shape, _ := p.buffer.GetCursorStyle()
			if set {
				p.buffer.SetCursorStyle(shape, 2) // Fast blink
			} else {
				p.buffer.SetCursorStyle(shape, 1) // Slow blink
			}
		case 7700: // PurfecTerm: Disable scrollback buffer (for games)
			// h = disable scrollback accumulation, l = re-enable
			p.buffer.SetScrollbackDisabled(set)
		case 7701: // PurfecTerm: Disable cursor-following auto-scroll
			// h = disable auto-scroll, l = re-enable
			// When disabled, tracking still occurs but no automatic scrolling happens
			p.buffer.SetAutoScrollDisabled(set)
		case 7702: // PurfecTerm: Smart word wrap
			// h = enable smart word wrap (wrap at word boundaries), l = disable
			p.buffer.SetSmartWordWrap(set)
		}
	}
}

func (p *Parser) handleOSC(b byte) {
	if b >= '0' && b <= '9' {
		p.oscBuf.WriteByte(b)
		return
	}
	if b == ';' {
		// Parse and save OSC command number
		cmdStr := p.oscBuf.String()
		p.oscCmd, _ = strconv.Atoi(cmdStr)
		p.oscBuf.Reset()
		p.state = stateOSCString
		return
	}
	// Invalid OSC, return to ground
	p.state = stateGround
}

func (p *Parser) handleOSCString(b byte) {
	if b == 0x07 { // BEL terminates OSC
		p.executeOSC()
		p.state = stateGround
		return
	}
	if b == 0x1B {
		// ESC begins ST (ESC \), so wait for the second byte instead of
		// ending here. Ending on the ESC left the '\' to be processed from
		// ground state and PRINTED -- every ST-terminated OSC dropped a
		// stray backslash on the screen. It went unnoticed because the
		// 7000-series are BEL-terminated in practice; OSC 52 traffic from
		// real programs is overwhelmingly ST-terminated.
		p.state = stateOSCEsc
		return
	}
	p.oscBuf.WriteByte(b)
}

// handleOSCEsc resolves the byte after an ESC inside an OSC string. '\'
// completes ST and executes; anything else means the OSC was interrupted, so
// abandon it and reprocess the byte from the escape state it actually
// started.
func (p *Parser) handleOSCEsc(b byte) {
	if b == '\\' {
		p.executeOSC()
		p.state = stateGround
		return
	}
	p.oscBuf.Reset()
	p.state = stateEscape
	p.handleEscape(b)
}

// executeOSC processes a complete OSC command
func (p *Parser) executeOSC() {
	args := p.oscBuf.String()

	switch p.oscCmd {
	case 0, 1, 2: // Set window/icon title
		p.executeOSCTitle(args)
	case 4: // Set/query 256-color palette entry
		p.executeOSCPaletteColor(args)
	case 10, 11, 12: // Set/query default foreground / background / cursor color
		p.executeOSCColorFgBg(p.oscCmd, args)
	case 52: // Clipboard (OSC 52)
		p.executeOSCClipboard(args)
	case 1337: // iTerm2 inline images
		p.executeOSCImage(args)
	case 7000: // Palette management
		p.executeOSCPalette(args)
	case 7001: // Glyph management
		p.executeOSCGlyph(args)
	case 7002: // Sprite management
		p.executeOSCSprite(args)
	case 7004: // Font-slot configuration
		p.executeOSCFont(args)
	case 7005: // Script-class font configuration
		p.executeOSCScriptFont(args)
	case 7003: // Screen crop and splits
		p.executeOSCScreenCrop(args)
		// Other OSC commands (title, etc.) could be added here
	}
}

// executeOSCPalette handles OSC 7000 palette commands
// Format: ESC ] 7000 ; cmd BEL
// Commands:
//
//	da           - delete all palettes
//	d;N          - delete palette N
//	i;N;LEN      - init palette N with LEN entries
//	s;N;IDX;COL  - set palette N index IDX to color COL
//	s;N;IDX;2;COL - set palette N index IDX to dim color COL
func (p *Parser) executeOSCPalette(args string) {
	parts := strings.Split(args, ";")
	if len(parts) == 0 {
		return
	}

	cmd := parts[0]
	switch cmd {
	case "da": // Delete all palettes
		p.buffer.DeleteAllPalettes()

	case "d": // Delete single palette
		if len(parts) >= 2 {
			n, _ := strconv.Atoi(parts[1])
			p.buffer.DeletePalette(n)
		}

	case "i": // Init palette
		if len(parts) >= 3 {
			n, _ := strconv.Atoi(parts[1])
			length, _ := strconv.Atoi(parts[2])
			p.buffer.InitPalette(n, length)
		}

	case "s": // Set palette entry
		// Formats:
		//   s;N;IDX;COL           - SGR-style color (30-37, 90-97)
		//   s;N;IDX;2;COL         - SGR-style color, dim
		//   s;N;IDX;5;N256        - 256-color palette index
		//   s;N;IDX;5;2;N256      - 256-color, dim
		//   s;N;IDX;r;R;G;B       - True color RGB
		//   s;N;IDX;r;2;R;G;B     - True color RGB, dim
		if len(parts) >= 4 {
			n, _ := strconv.Atoi(parts[1])
			idx, _ := strconv.Atoi(parts[2])
			mode := parts[3]

			switch mode {
			case "5": // 256-color mode
				dim := false
				colorIdx := 4
				if len(parts) > 4 && parts[4] == "2" {
					dim = true
					colorIdx = 5
				}
				if colorIdx < len(parts) {
					colorNum, _ := strconv.Atoi(parts[colorIdx])
					color := Get256Color(colorNum)
					p.buffer.SetPaletteEntryColor(n, idx, color, dim)
				}

			case "r": // True color RGB mode
				dim := false
				rgbStart := 4
				if len(parts) > 4 && parts[4] == "2" {
					dim = true
					rgbStart = 5
				}
				if rgbStart+2 < len(parts) {
					r, _ := strconv.Atoi(parts[rgbStart])
					g, _ := strconv.Atoi(parts[rgbStart+1])
					b, _ := strconv.Atoi(parts[rgbStart+2])
					color := TrueColor(uint8(r), uint8(g), uint8(b))
					p.buffer.SetPaletteEntryColor(n, idx, color, dim)
				}

			case "2": // Dim modifier for SGR-style (legacy format)
				if len(parts) >= 5 {
					colorCode, _ := strconv.Atoi(parts[4])
					p.buffer.SetPaletteEntry(n, idx, colorCode, true)
				}

			default: // SGR-style color code
				colorCode, _ := strconv.Atoi(mode)
				p.buffer.SetPaletteEntry(n, idx, colorCode, false)
			}
		}
	}
}

// executeOSCGlyph handles OSC 7001 glyph commands
// Format: ESC ] 7001 ; cmd BEL
// Commands:
//
//	da                    - delete all glyphs
//	d;RUNE                - delete glyph for rune
//	s;RUNE;W;P1;P2;...    - set glyph for rune (W=width, P=pixels)
func (p *Parser) executeOSCGlyph(args string) {
	parts := strings.Split(args, ";")
	if len(parts) == 0 {
		return
	}

	cmd := parts[0]
	switch cmd {
	case "da": // Delete all glyphs
		p.buffer.DeleteAllGlyphs()

	case "d": // Delete single glyph
		if len(parts) >= 2 {
			runeCode, _ := strconv.Atoi(parts[1])
			p.buffer.DeleteGlyph(rune(runeCode))
		}

	case "s": // Set glyph
		// Format: s;RUNE;WIDTH;P1;P2;P3;...
		if len(parts) >= 4 {
			runeCode, _ := strconv.Atoi(parts[1])
			width, _ := strconv.Atoi(parts[2])
			pixels := make([]int, 0, len(parts)-3)
			for i := 3; i < len(parts); i++ {
				px, _ := strconv.Atoi(parts[i])
				pixels = append(pixels, px)
			}
			if width > 0 && len(pixels) > 0 {
				p.buffer.SetGlyph(rune(runeCode), width, pixels)
			}
		}
	}
}

// executeOSCSprite handles OSC 7002 sprite commands
// Format: ESC ] 7002 ; cmd BEL
// Commands:
//
//	da                                         - delete all sprites
//	d;ID                                       - delete sprite by ID
//	s;ID;X;Y;Z;FGP;FLIP;XS;YS;CROP;R1;R2;...   - set sprite (rune codes, 10=newline)
//	t;ID;X;Y;Z;FGP;FLIP;XS;YS;CROP;text        - set sprite (text string)
//	m;ID;X;Y                                   - move sprite (position only)
//	mr;ID;X;Y;R1;R2;...                        - move and update runes (rune codes)
//	mrt;ID;X;Y;text                            - move and update runes (text)
//	u;UX;UY                                    - set coordinate units
//	cda                                        - delete all crop rectangles
//	cd;ID                                      - delete crop rectangle
//	cs;ID;MINX;MINY;MAXX;MAXY                  - set crop rectangle
func (p *Parser) executeOSCSprite(args string) {
	parts := strings.Split(args, ";")
	if len(parts) == 0 {
		return
	}

	cmd := parts[0]
	switch cmd {
	case "da": // Delete all sprites
		p.buffer.DeleteAllSprites()

	case "d": // Delete single sprite
		if len(parts) >= 2 {
			id, _ := strconv.Atoi(parts[1])
			p.buffer.DeleteSprite(id)
		}

	case "s": // Set sprite with rune codes
		// Format: s;ID;X;Y;Z;FGP;FLIP;XS;YS;CROP;R1;R2;...
		if len(parts) >= 11 {
			id, _ := strconv.Atoi(parts[1])
			x, _ := strconv.ParseFloat(parts[2], 64)
			y, _ := strconv.ParseFloat(parts[3], 64)
			z, _ := strconv.Atoi(parts[4])
			fgp, _ := strconv.Atoi(parts[5])
			flipCode, _ := strconv.Atoi(parts[6])
			xScale, _ := strconv.ParseFloat(parts[7], 64)
			yScale, _ := strconv.ParseFloat(parts[8], 64)
			cropRect, _ := strconv.Atoi(parts[9])

			// Collect runes from remaining parts
			runes := make([]rune, 0, len(parts)-10)
			for i := 10; i < len(parts); i++ {
				code, _ := strconv.Atoi(parts[i])
				runes = append(runes, rune(code))
			}

			// Default scales if zero
			if xScale == 0 {
				xScale = 1.0
			}
			if yScale == 0 {
				yScale = 1.0
			}

			p.buffer.SetSprite(id, x, y, z, fgp, flipCode, xScale, yScale, cropRect, runes)
		}

	case "t": // Set sprite with text string
		// Format: t;ID;X;Y;Z;FGP;FLIP;XS;YS;CROP;text
		if len(parts) >= 11 {
			id, _ := strconv.Atoi(parts[1])
			x, _ := strconv.ParseFloat(parts[2], 64)
			y, _ := strconv.ParseFloat(parts[3], 64)
			z, _ := strconv.Atoi(parts[4])
			fgp, _ := strconv.Atoi(parts[5])
			flipCode, _ := strconv.Atoi(parts[6])
			xScale, _ := strconv.ParseFloat(parts[7], 64)
			yScale, _ := strconv.ParseFloat(parts[8], 64)
			cropRect, _ := strconv.Atoi(parts[9])

			// Text is everything after the 9th semicolon (may contain semicolons)
			text := strings.Join(parts[10:], ";")
			runes := []rune(text)

			// Default scales if zero
			if xScale == 0 {
				xScale = 1.0
			}
			if yScale == 0 {
				yScale = 1.0
			}

			p.buffer.SetSprite(id, x, y, z, fgp, flipCode, xScale, yScale, cropRect, runes)
		}

	case "m": // Move sprite (position only)
		// Format: m;ID;X;Y
		if len(parts) >= 4 {
			id, _ := strconv.Atoi(parts[1])
			x, _ := strconv.ParseFloat(parts[2], 64)
			y, _ := strconv.ParseFloat(parts[3], 64)
			p.buffer.MoveSprite(id, x, y)
		}

	case "mr": // Move and update runes (rune codes)
		// Format: mr;ID;X;Y;R1;R2;...
		if len(parts) >= 5 {
			id, _ := strconv.Atoi(parts[1])
			x, _ := strconv.ParseFloat(parts[2], 64)
			y, _ := strconv.ParseFloat(parts[3], 64)

			// Collect runes from remaining parts
			runes := make([]rune, 0, len(parts)-4)
			for i := 4; i < len(parts); i++ {
				code, _ := strconv.Atoi(parts[i])
				runes = append(runes, rune(code))
			}

			p.buffer.MoveSpriteAndRunes(id, x, y, runes)
		}

	case "mrt": // Move and update runes (text)
		// Format: mrt;ID;X;Y;text
		if len(parts) >= 5 {
			id, _ := strconv.Atoi(parts[1])
			x, _ := strconv.ParseFloat(parts[2], 64)
			y, _ := strconv.ParseFloat(parts[3], 64)

			// Text is everything after the 3rd semicolon (may contain semicolons)
			text := strings.Join(parts[4:], ";")
			runes := []rune(text)

			p.buffer.MoveSpriteAndRunes(id, x, y, runes)
		}

	case "u": // Set coordinate units (subdivisions per cell)
		// Format: u;UX;UY
		if len(parts) >= 3 {
			ux, _ := strconv.Atoi(parts[1])
			uy, _ := strconv.Atoi(parts[2])
			p.buffer.SetSpriteUnits(ux, uy)
		}

	case "cda": // Delete all crop rectangles
		p.buffer.DeleteAllCropRects()

	case "cd": // Delete crop rectangle
		if len(parts) >= 2 {
			id, _ := strconv.Atoi(parts[1])
			p.buffer.DeleteCropRect(id)
		}

	case "cs": // Set crop rectangle
		// Format: cs;ID;MINX;MINY;MAXX;MAXY
		if len(parts) >= 6 {
			id, _ := strconv.Atoi(parts[1])
			minX, _ := strconv.ParseFloat(parts[2], 64)
			minY, _ := strconv.ParseFloat(parts[3], 64)
			maxX, _ := strconv.ParseFloat(parts[4], 64)
			maxY, _ := strconv.ParseFloat(parts[5], 64)
			p.buffer.SetCropRect(id, minX, minY, maxX, maxY)
		}
	}
}

// executeOSCScreenCrop handles OSC 7003 screen crop and split commands
// Format: ESC ] 7003 ; cmd BEL
// Commands:
//
//	c                                                       - clear both crops
//	c;WIDTH                                                 - set width crop only (in sprite units, -1 = no crop)
//	c;;HEIGHT                                               - set height crop only
//	c;WIDTH;HEIGHT                                          - set both crops
//	sda                                                     - delete all screen splits
//	sd;ID                                                   - delete screen split by ID
//	ss;ID;SCREENY;BUFROW;BUFCOL;TOPFINE;LEFTFINE;CWS;LD     - set screen split
//	    ID: split identifier
//	    SCREENY: Y coordinate in sprite units where split begins on screen
//	    BUFROW, BUFCOL: 1-indexed logical screen coordinates (0 = inherit/default)
//	    TOPFINE, LEFTFINE: fine scroll (0 to subdivisions-1, higher = more clipped)
//	    CWS: character width scale (0 = inherit)
//	    LD: line density override (0 = inherit)
func (p *Parser) executeOSCScreenCrop(args string) {
	parts := strings.Split(args, ";")
	if len(parts) == 0 {
		return
	}

	cmd := parts[0]
	switch cmd {
	case "c": // Screen crop: c (clear), c;W, c;;H, or c;W;H
		widthCrop := -1
		heightCrop := -1
		if len(parts) >= 2 && parts[1] != "" {
			widthCrop, _ = strconv.Atoi(parts[1])
		}
		if len(parts) >= 3 && parts[2] != "" {
			heightCrop, _ = strconv.Atoi(parts[2])
		}
		p.buffer.SetScreenCrop(widthCrop, heightCrop)

	case "sda": // Delete all screen splits
		p.buffer.DeleteAllScreenSplits()

	case "sd": // Delete screen split
		// Format: sd;ID
		if len(parts) >= 2 {
			id, _ := strconv.Atoi(parts[1])
			p.buffer.DeleteScreenSplit(id)
		}

	case "ss": // Set screen split
		// Format: ss;ID;SCREENY;BUFROW;BUFCOL;TOPFINE;LEFTFINE;CWS;LD
		if len(parts) >= 9 {
			id, _ := strconv.Atoi(parts[1])
			screenY, _ := strconv.Atoi(parts[2])
			bufRow, _ := strconv.Atoi(parts[3])
			bufCol, _ := strconv.Atoi(parts[4])
			topFine, _ := strconv.Atoi(parts[5])
			leftFine, _ := strconv.Atoi(parts[6])
			charWidthScale, _ := strconv.ParseFloat(parts[7], 64)
			lineDensity, _ := strconv.Atoi(parts[8])

			// Convert 1-indexed buffer coordinates to 0-indexed
			// (0 in escape means inherit/default, maps to 0 internally)
			if bufRow > 0 {
				bufRow--
			}
			if bufCol > 0 {
				bufCol--
			}

			p.buffer.SetScreenSplit(id, screenY, bufRow, bufCol, topFine, leftFine, charWidthScale, lineDensity)
		}
	}
}

// executeOSCFont handles OSC 7004 font-slot commands, configuring the
// per-terminal slot -> family map an app selects between with SGR 10-20.
// Format: ESC ] 7004 ; cmd BEL
// Commands:
//
//	f;SLOT;FAMILY  - map font slot SLOT (0..10) to family name FAMILY
//	fd;SLOT        - clear slot SLOT (it then inherits slot 0)
//	fda            - clear all slot mappings
func (p *Parser) executeOSCFont(args string) {
	parts := strings.SplitN(args, ";", 3)
	if len(parts) == 0 {
		return
	}
	switch parts[0] {
	case "fda": // clear all
		for s := 0; s <= 10; s++ {
			p.buffer.SetFontSlot(s, "")
		}
	case "fd": // clear one
		if len(parts) >= 2 {
			if slot, err := strconv.Atoi(parts[1]); err == nil {
				p.buffer.SetFontSlot(slot, "")
			}
		}
	case "f": // set slot -> family
		if len(parts) >= 3 {
			if slot, err := strconv.Atoi(parts[1]); err == nil {
				p.buffer.SetFontSlot(slot, parts[2])
			}
		}
	}
}

// executeOSCScriptFont handles OSC 7005 script-class font commands, configuring
// the per-terminal script-class -> family map a renderer consults (via
// ScriptClass) when the primary font can't cover a glyph.
// Format: ESC ] 7005 ; cmd BEL
// Commands:
//
//	s;CLASS;FAMILY - map script class CLASS (hebrew/arabic/cjk) to family FAMILY
//	sd;CLASS       - clear class CLASS (renderer falls back to its default)
//	sda            - clear all script-class mappings
func (p *Parser) executeOSCScriptFont(args string) {
	parts := strings.SplitN(args, ";", 3)
	if len(parts) == 0 {
		return
	}
	switch parts[0] {
	case "sda": // clear all
		p.buffer.ClearScriptFonts()
	case "sd": // clear one
		if len(parts) >= 2 {
			p.buffer.SetScriptFont(parts[1], "")
		}
	case "s": // set class -> family
		if len(parts) >= 3 {
			p.buffer.SetScriptFont(parts[1], parts[2])
		}
	}
}
