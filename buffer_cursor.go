package purfecterm

import "time"

// --- Cursor Position Methods ---

// GetCursor returns the current cursor position
func (b *Buffer) GetCursor() (x, y int) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.cursorX, b.cursorY
}

// SetCursor sets the cursor position (clamped to valid range)
func (b *Buffer) SetCursor(x, y int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.setCursorInternal(x, y)
}

// trackCursorYMove tracks cursor movement direction for auto-scroll.
// Call this before modifying cursorY with the new Y value.
func (b *Buffer) trackCursorYMove(newY int) {
	if newY > b.cursorY {
		b.lastCursorMoveDir = 1 // Moving down
	} else if newY < b.cursorY {
		b.lastCursorMoveDir = -1 // Moving up
	}
	// If equal, keep previous direction
}

// setHorizMoveDir sets the horizontal cursor movement direction for horizontal auto-scroll.
// dir: -1=left, 0=unknown/absolute, 1=right
// isAbsolute: true if this was an absolute positioning command (CSI H/f/G)
func (b *Buffer) setHorizMoveDir(dir int, isAbsolute bool) {
	b.lastHorizCursorMoveDir = dir
	b.isAbsoluteHorizPosition = isAbsolute
}

func (b *Buffer) setCursorInternal(x, y int) {
	// Use effective (logical) dimensions for cursor bounds
	effectiveCols := b.EffectiveCols()
	effectiveRows := b.EffectiveRows()
	if x < 0 {
		x = 0
	}
	if x >= effectiveCols {
		x = effectiveCols - 1
	}
	if y < 0 {
		y = 0
	}
	if y >= effectiveRows {
		y = effectiveRows - 1
	}

	b.trackCursorYMove(y)
	b.setHorizMoveDir(0, true) // Absolute positioning - direction unknown
	b.cursorX = x
	b.cursorY = y
	b.markDirty()
}

// --- Cursor Visibility and Style ---

// SetCursorVisible sets cursor visibility
func (b *Buffer) SetCursorVisible(visible bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cursorVisible = visible
	b.markDirty()
}

// IsCursorVisible returns cursor visibility
func (b *Buffer) IsCursorVisible() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.cursorVisible
}

// SetCursorStyle sets the cursor shape and blink mode
func (b *Buffer) SetCursorStyle(shape, blink int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cursorShape = shape
	b.cursorBlink = blink
	b.markDirty()
}

// GetCursorStyle returns the cursor shape and blink mode
func (b *Buffer) GetCursorStyle() (shape, blink int) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.cursorShape, b.cursorBlink
}

// --- Cursor Save/Restore ---

// SaveCursor saves the current cursor position
func (b *Buffer) SaveCursor() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.savedCursorX = b.cursorX
	b.savedCursorY = b.cursorY
}

// RestoreCursor restores the saved cursor position
func (b *Buffer) RestoreCursor() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cursorX = b.savedCursorX
	b.trackCursorYMove(b.savedCursorY)
	b.cursorY = b.savedCursorY
	b.markDirty()
}

// --- Cursor Auto-Scroll ---

// NotifyKeyboardActivity signals that keyboard input occurred.
// This starts/restarts the auto-scroll timer.
func (b *Buffer) NotifyKeyboardActivity() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastKeyboardActivity = time.Now()
}

// NotifyManualVertScroll signals that the user manually scrolled vertically
// (via mouse wheel, scrollbar, etc). This cancels vertical auto-scroll
// to avoid fighting with user intent.
func (b *Buffer) NotifyManualVertScroll() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastManualVertScroll = time.Now()
}

// isVertAutoScrollActive returns true if vertical auto-scroll should be active.
// It checks keyboard activity and whether manual scroll should take precedence.
// Must be called with lock held.
func (b *Buffer) isVertAutoScrollActive() bool {
	// Must have recent keyboard activity
	if b.lastKeyboardActivity.IsZero() {
		return false
	}
	if time.Since(b.lastKeyboardActivity) >= keyboardAutoScrollDuration {
		return false
	}

	// If user manually scrolled more recently than keyboard activity, defer to user
	if !b.lastManualVertScroll.IsZero() && b.lastManualVertScroll.After(b.lastKeyboardActivity) {
		return false
	}

	return true
}

// extendAutoScrollTimer extends the keyboard activity timer when auto-scroll
// is actively working to bring the cursor into view. This allows longer output
// to keep scrolling without the timer expiring mid-scroll.
// Must be called with lock held.
func (b *Buffer) extendAutoScrollTimer() {
	b.lastKeyboardActivity = time.Now()
}

// isAutoScrollActive returns true if keyboard activity occurred recently
// enough that cursor movements should auto-scroll the view.
// Must be called with lock held.
// DEPRECATED: Use isVertAutoScrollActive instead for more accurate behavior.
func (b *Buffer) isAutoScrollActive() bool {
	if b.lastKeyboardActivity.IsZero() {
		return false
	}
	return time.Since(b.lastKeyboardActivity) < keyboardAutoScrollDuration
}

// SetCursorDrawn is called by the widget after rendering to indicate whether
// the cursor was actually drawn on screen.
func (b *Buffer) SetCursorDrawn(drawn bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cursorDrawnLastFrame = drawn
}

// CheckCursorAutoScroll checks if the cursor was not drawn last frame and
// auto-scroll is active, then scrolls toward the cursor position to bring
// the cursor back into view. When the cursor is multiple lines away, it
// scrolls multiple lines at once for faster catch-up.
// Returns true if a scroll occurred.
func (b *Buffer) CheckCursorAutoScroll() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Check if auto-scroll is disabled by DEC Private Mode
	if b.autoScrollDisabled {
		return false
	}

	// Only auto-scroll if keyboard activity is recent and user hasn't manually scrolled
	if !b.isVertAutoScrollActive() {
		return false
	}

	effectiveRows := b.EffectiveRows()
	logicalHiddenAbove := 0
	if effectiveRows > b.rows {
		logicalHiddenAbove = effectiveRows - b.rows
	}

	// FIRST: If we're viewing scrollback (scrollOffset > logicalHiddenAbove),
	// instantly snap to the logical screen boundary. The scrollback should be
	// forced off screen before any gradual auto-scrolling happens.
	if b.scrollOffset > logicalHiddenAbove {
		b.scrollOffset = logicalHiddenAbove
		b.extendAutoScrollTimer() // Extend timer since we're actively scrolling
		b.markDirty()
		return true
	}

	// If cursor was drawn, no scroll needed
	if b.cursorDrawnLastFrame {
		return false
	}

	// Calculate cursor's visible Y position to determine how far off-screen it is
	effectiveScrollOffset := b.getEffectiveScrollOffset()
	visibleY := b.cursorY - logicalHiddenAbove + effectiveScrollOffset

	// Determine how many rows to scroll
	var scrollAmount int
	if b.lastCursorMoveDir > 0 {
		// Cursor moved down - need to scroll down (decrease scroll offset)
		// visibleY >= b.rows means cursor is below visible area
		if visibleY >= b.rows {
			scrollAmount = visibleY - b.rows + 1 // How far below the visible area
		} else if b.scrollOffset > 0 {
			scrollAmount = 1 // Just scroll one row if we can
		}

		if scrollAmount > 0 && b.scrollOffset > 0 {
			// Don't scroll more than we have offset
			if scrollAmount > b.scrollOffset {
				scrollAmount = b.scrollOffset
			}
			b.scrollOffset -= scrollAmount
			b.extendAutoScrollTimer() // Extend timer since we're actively scrolling
			b.markDirty()
			return true
		}
	} else if b.lastCursorMoveDir < 0 {
		// Cursor moved up - need to scroll up (increase scroll offset)
		// visibleY < 0 means cursor is above visible area
		if visibleY < 0 {
			scrollAmount = -visibleY // How far above (positive value)
		} else if b.scrollOffset < logicalHiddenAbove {
			scrollAmount = 1 // Just scroll one row if we can
		}

		if scrollAmount > 0 && b.scrollOffset < logicalHiddenAbove {
			// Don't scroll beyond logicalHiddenAbove
			maxScroll := logicalHiddenAbove - b.scrollOffset
			if scrollAmount > maxScroll {
				scrollAmount = maxScroll
			}
			b.scrollOffset += scrollAmount
			b.extendAutoScrollTimer() // Extend timer since we're actively scrolling
			b.markDirty()
			return true
		}
	}
	// If lastCursorMoveDir == 0, no direction known, don't scroll

	return false
}

// SetAutoScrollDisabled enables or disables cursor-following auto-scroll.
// When disabled, tracking still occurs but no automatic scrolling happens.
// This is controlled by a DEC Private Mode sequence.
func (b *Buffer) SetAutoScrollDisabled(disabled bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.autoScrollDisabled = disabled
}

// IsAutoScrollDisabled returns true if auto-scroll is disabled.
func (b *Buffer) IsAutoScrollDisabled() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.autoScrollDisabled
}

// --- Cursor Movement ---

// MoveCursorUp moves cursor up n rows
func (b *Buffer) MoveCursorUp(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	newY := b.cursorY - n
	if newY < 0 {
		newY = 0
	}
	b.trackCursorYMove(newY)
	b.cursorY = newY
	b.markDirty()
}

// MoveCursorDown moves cursor down n rows
func (b *Buffer) MoveCursorDown(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	newY := b.cursorY + n
	effectiveRows := b.EffectiveRows()
	if newY >= effectiveRows {
		newY = effectiveRows - 1
	}
	b.trackCursorYMove(newY)
	b.cursorY = newY
	b.markDirty()
}

// MoveCursorForward moves cursor right n columns (CSI C)
func (b *Buffer) MoveCursorForward(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.setHorizMoveDir(1, false) // Moving right
	b.cursorX += n
	effectiveCols := b.EffectiveCols()
	if b.cursorX >= effectiveCols {
		b.cursorX = effectiveCols - 1
	}
	b.markDirty()
}

// MoveCursorBackward moves cursor left n columns (CSI D)
func (b *Buffer) MoveCursorBackward(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.setHorizMoveDir(-1, false) // Moving left
	b.cursorX -= n
	if b.cursorX < 0 {
		b.cursorX = 0
	}
	b.markDirty()
}
