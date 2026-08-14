package purfecterm

// Alternate screen buffer (DEC private modes ?47 / ?1047 / ?1049) plus the
// capture scope that decides which screen(s) feed the capture events.
//
// The Buffer's live fields are always the ACTIVE screen. Entering the alternate
// screen stashes the primary screen's whole context — content, cursor, margins,
// origin mode, and its own scrollback — and installs a fresh blank alternate
// screen with an independent scrollback. Leaving restores the primary context
// and discards the alternate. A switch fires OnScreenSwitch so a capture
// consumer can attribute the events that follow to the right screen.

// CaptureScope selects which screen(s) feed the capture content events
// (OnLineOff and the live rung). OnScreenSwitch is never scope-gated.
type CaptureScope int

const (
	CaptureMain CaptureScope = iota // only the primary screen (default)
	CaptureAlt                      // only the alternate screen
	CaptureBoth                     // both screens
)

// screenState is the swappable per-screen context.
type screenState struct {
	screen          [][]Cell
	lineInfos       []LineInfo
	scrollback      [][]Cell
	scrollbackInfo  []LineInfo
	scrollOffset    int
	cursorX         int
	cursorY         int
	savedCursorX    int
	savedCursorY    int
	scrollTop       int
	scrollBottom    int
	hasScrollRegion bool
	originMode      bool
	marginLeft      int
	marginRight     int
	hasLRMargins    bool
	screenInfo      ScreenInfo
	// Anchored images belong to the screen they were placed on. Without
	// stashing them, an alternate-screen application's images stayed behind
	// on the restored primary screen, and the primary's images bled into the
	// alternate — each screen redrawing over the other's leftovers.
	images      []*PlacedImage
	logicalCols int
	logicalRows int
	horizOffset int
}

// captureScreenState snapshots the active screen's context. Caller holds the lock.
func (b *Buffer) captureScreenState() screenState {
	return screenState{
		screen:          b.screen,
		lineInfos:       b.lineInfos,
		scrollback:      b.scrollback,
		scrollbackInfo:  b.scrollbackInfo,
		scrollOffset:    b.scrollOffset,
		cursorX:         b.cursorX,
		cursorY:         b.cursorY,
		savedCursorX:    b.savedCursorX,
		savedCursorY:    b.savedCursorY,
		scrollTop:       b.scrollTop,
		scrollBottom:    b.scrollBottom,
		hasScrollRegion: b.hasScrollRegion,
		originMode:      b.originMode,
		marginLeft:      b.marginLeft,
		marginRight:     b.marginRight,
		hasLRMargins:    b.hasLRMargins,
		screenInfo:      b.screenInfo,
		images:          b.images,
		logicalCols:     b.logicalCols,
		logicalRows:     b.logicalRows,
		horizOffset:     b.horizOffset,
	}
}

// restoreScreenState installs a stashed context into the active fields. Caller
// holds the lock.
func (b *Buffer) restoreScreenState(s screenState) {
	b.screen = s.screen
	b.lineInfos = s.lineInfos
	b.scrollback = s.scrollback
	b.scrollbackInfo = s.scrollbackInfo
	b.scrollOffset = s.scrollOffset
	b.cursorX = s.cursorX
	b.cursorY = s.cursorY
	b.savedCursorX = s.savedCursorX
	b.savedCursorY = s.savedCursorY
	b.scrollTop = s.scrollTop
	b.scrollBottom = s.scrollBottom
	b.hasScrollRegion = s.hasScrollRegion
	b.originMode = s.originMode
	b.marginLeft = s.marginLeft
	b.marginRight = s.marginRight
	b.hasLRMargins = s.hasLRMargins
	b.screenInfo = s.screenInfo
	b.images = s.images
	b.logicalCols = s.logicalCols
	b.logicalRows = s.logicalRows
	b.horizOffset = s.horizOffset
}

// captureScreenInScope reports whether the active screen is within the capture
// scope. Caller holds the lock.
func (b *Buffer) captureScreenInScope() bool {
	switch b.captureScope {
	case CaptureBoth:
		return true
	case CaptureAlt:
		return b.onAltScreen
	default: // CaptureMain
		return !b.onAltScreen
	}
}

// SetCaptureScope selects which screen(s) feed the capture content events.
func (b *Buffer) SetCaptureScope(scope CaptureScope) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.captureScope = scope
}

// GetCaptureScope returns the current capture scope.
func (b *Buffer) GetCaptureScope() CaptureScope {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.captureScope
}

// IsAltScreen reports whether the alternate screen is currently active.
func (b *Buffer) IsAltScreen() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.onAltScreen
}

// initAltScreen installs a fresh blank alternate screen at physical size with an
// empty independent scrollback, home cursor, no scroll region, and origin mode
// off. Caller holds the lock.
func (b *Buffer) initAltScreen() {
	b.images = nil // a fresh screen starts with no anchored images
	b.logicalCols = 0
	b.logicalRows = 0
	rows := b.rows
	b.screen = make([][]Cell, rows)
	b.lineInfos = make([]LineInfo, rows)
	for i := range b.screen {
		b.screen[i] = b.makeEmptyLine()
		b.lineInfos[i] = b.makeDefaultLineInfo()
	}
	b.scrollback = nil
	b.scrollbackInfo = nil
	b.scrollOffset = 0
	b.cursorX = 0
	b.cursorY = 0
	b.savedCursorX = 0
	b.savedCursorY = 0
	b.scrollTop = 0
	b.scrollBottom = rows - 1
	b.hasScrollRegion = false
	b.originMode = false
	b.marginLeft = 0
	b.marginRight = b.cols - 1
	b.hasLRMargins = false
	b.horizOffset = 0
}

// fitActiveScreenRows adjusts the active screen's row count to the effective row
// count (handling a resize that happened while the other screen was active) and
// clamps the cursor. Caller holds the lock.
func (b *Buffer) fitActiveScreenRows() {
	want := b.EffectiveRows()
	if want < 1 {
		want = 1
	}
	for len(b.screen) < want {
		b.screen = append(b.screen, b.makeEmptyLine())
		b.lineInfos = append(b.lineInfos, b.makeDefaultLineInfo())
	}
	if len(b.screen) > want {
		b.screen = b.screen[:want]
		b.lineInfos = b.lineInfos[:want]
	}
	if b.cursorY >= want {
		b.cursorY = want - 1
	}
	if b.cursorY < 0 {
		b.cursorY = 0
	}
}

// EnterAltScreen switches to a fresh blank alternate screen (DEC ?47/?1047/?1049
// high). No-op if already on the alternate screen. Fires OnScreenSwitch(true).
func (b *Buffer) EnterAltScreen() {
	b.mu.Lock()
	if b.onAltScreen {
		b.mu.Unlock()
		return
	}
	b.mainStash = b.captureScreenState()
	b.onAltScreen = true
	b.initAltScreen()
	b.markDirty()
	obs := b.captureObserver
	b.mu.Unlock()

	if obs != nil {
		obs.OnScreenSwitch(true)
	}
}

// LeaveAltScreen returns to the primary screen (DEC ?47/?1047/?1049 low),
// discarding the alternate screen and its scrollback. No-op if not on the
// alternate screen. Fires OnScreenSwitch(false).
func (b *Buffer) LeaveAltScreen() {
	b.mu.Lock()
	if !b.onAltScreen {
		b.mu.Unlock()
		return
	}
	b.onAltScreen = false
	b.restoreScreenState(b.mainStash)
	b.mainStash = screenState{} // release the alternate screen + stash refs
	b.fitActiveScreenRows()     // in case the terminal resized while in the alt screen
	b.markDirty()
	obs := b.captureObserver
	b.mu.Unlock()

	if obs != nil {
		obs.OnScreenSwitch(false)
	}
}
