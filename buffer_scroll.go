package purfecterm

import "time"

// --- Vertical Scroll Methods ---

// GetScrollbackSize returns the number of lines in scrollback
func (b *Buffer) GetScrollbackSize() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.scrollback)
}

// GetMaxScrollOffset returns the maximum vertical scroll offset
// This accounts for scrollback AND any logical rows hidden above the visible area
func (b *Buffer) GetMaxScrollOffset() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.getMaxScrollOffsetInternal()
}

func (b *Buffer) getMaxScrollOffsetInternal() int {
	effectiveRows := b.EffectiveRows()

	// If logical screen is larger than physical, some rows are hidden
	logicalHiddenAbove := 0
	if effectiveRows > b.rows {
		logicalHiddenAbove = effectiveRows - b.rows
	}

	// When scrollback is disabled, only allow scrolling within logical screen
	if b.scrollbackDisabled {
		return logicalHiddenAbove
	}

	scrollbackSize := len(b.scrollback)
	baseMax := scrollbackSize + logicalHiddenAbove

	// Add magnetic threshold to create extra scroll positions for the magnetic zone.
	// This ensures all scrollback content remains accessible - the magnetic zone
	// adds positions rather than consuming them.
	if scrollbackSize > 0 {
		baseMax += b.getMagneticThreshold()
	}

	return baseMax
}

// SetScrollOffset sets how many lines we're scrolled back
func (b *Buffer) SetScrollOffset(offset int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	maxOffset := b.getMaxScrollOffsetInternal()
	if offset < 0 {
		offset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	b.scrollOffset = offset
	b.markDirty()
}

// GetScrollOffset returns current scroll offset
func (b *Buffer) GetScrollOffset() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.scrollOffset
}

// GetEffectiveScrollOffset returns the scroll offset adjusted for the magnetic zone.
// Use this for rendering sprites, splits, and other positioned elements that should
// remain stable during the magnetic zone.
func (b *Buffer) GetEffectiveScrollOffset() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.getEffectiveScrollOffset()
}

// NormalizeScrollOffset snaps the scroll offset back if it's in the magnetic zone.
// The magnetic zone is when the boundary would appear at rows 1-threshold (5% of scrollable content).
// This should be called when scrolling down to create a "sticky" effect at the
// boundary between logical screen and scrollback.
// Returns true if the offset was changed.
func (b *Buffer) NormalizeScrollOffset() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	effectiveRows := b.EffectiveRows()

	// Calculate how much of the logical screen is hidden above
	logicalHiddenAbove := 0
	if effectiveRows > b.rows {
		logicalHiddenAbove = effectiveRows - b.rows
	}

	// Calculate where the boundary would appear
	boundaryRow := b.scrollOffset - logicalHiddenAbove

	// Check if we're in the magnetic zone (boundary would be at rows 1-threshold)
	magneticThreshold := b.getMagneticThreshold()
	if boundaryRow > 0 && boundaryRow <= magneticThreshold {
		// Snap back to where boundary is at row 0 (just off the top of visible area)
		b.scrollOffset = logicalHiddenAbove
		b.markDirty()
		return true
	}
	return false
}

// GetScrollbackBoundaryVisibleRow returns the visible row (0-indexed from top of display)
// where the boundary between scrollback and logical screen is located.
// Returns -1 if the boundary is not currently visible (either fully in scrollback or fully in logical screen).
// The magnetic threshold suppresses the boundary for the first few rows after it would appear,
// creating a "sticky" feel when transitioning from logical screen to scrollback.
func (b *Buffer) GetScrollbackBoundaryVisibleRow() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	scrollbackSize := len(b.scrollback)

	// If no scrollback, no boundary to show
	if scrollbackSize == 0 {
		return -1
	}

	effectiveRows := b.EffectiveRows()

	// Calculate how much of the logical screen is hidden above
	logicalHiddenAbove := 0
	if effectiveRows > b.rows {
		logicalHiddenAbove = effectiveRows - b.rows
	}

	// Normal boundary calculation
	boundaryRow := b.scrollOffset - logicalHiddenAbove

	// Boundary at or below row 0 means we're viewing logical screen only
	if boundaryRow <= 0 {
		return -1
	}

	// Magnetic zone: suppress boundary when it would appear in the first few rows
	// This creates the "sticky" feel at the transition from logical screen to scrollback
	magneticThreshold := b.getMagneticThreshold()
	if boundaryRow <= magneticThreshold {
		return -1
	}

	// Past magnetic zone - subtract threshold so boundary position matches content
	effectiveBoundaryRow := boundaryRow - magneticThreshold

	// Check effective boundary is in visible range (1 to rows-1)
	// Row 0 would mean boundary at very top (no scrollback visible)
	// Row >= rows would mean boundary below visible area
	if effectiveBoundaryRow <= 0 || effectiveBoundaryRow >= b.rows {
		return -1
	}

	return effectiveBoundaryRow
}

// GetCursorVisiblePosition returns the visible (x, y) position of the cursor
// accounting for scroll offset and magnetic zone. Returns (-1, -1) if the cursor
// is not currently visible.
func (b *Buffer) GetCursorVisiblePosition() (x, y int) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	effectiveRows := b.EffectiveRows()

	// Calculate how much of the logical screen is hidden above
	logicalHiddenAbove := 0
	if effectiveRows > b.rows {
		logicalHiddenAbove = effectiveRows - b.rows
	}

	// Use effective scroll offset to account for magnetic zone
	effectiveScrollOffset := b.getEffectiveScrollOffset()

	// The cursor is at logical position (cursorX, cursorY)
	// Its visible position is: cursorY - logicalHiddenAbove + effectiveScrollOffset
	visibleY := b.cursorY - logicalHiddenAbove + effectiveScrollOffset

	// Check if cursor is within visible vertical area
	if visibleY < 0 || visibleY >= b.rows {
		return -1, -1
	}

	// X position needs to account for horizontal scroll
	visibleX := b.cursorX - b.horizOffset

	// Check if cursor is within visible horizontal area
	if visibleX < 0 || visibleX >= b.cols {
		return -1, -1
	}

	return visibleX, visibleY
}

// GetCursorVisibleY returns just the cursor's visible Y position (row) on the
// physical screen, even if the cursor is horizontally off-screen. Returns -1
// if the cursor's line is not within the visible vertical area.
// This is useful for cursor tracking: we want to know if the cursor's LINE
// is being rendered, regardless of whether the cursor itself is visible.
func (b *Buffer) GetCursorVisibleY() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	effectiveRows := b.EffectiveRows()

	// Calculate how much of the logical screen is hidden above
	logicalHiddenAbove := 0
	if effectiveRows > b.rows {
		logicalHiddenAbove = effectiveRows - b.rows
	}

	// Use effective scroll offset to account for magnetic zone
	effectiveScrollOffset := b.getEffectiveScrollOffset()

	// The cursor is at logical position (cursorX, cursorY)
	// Its visible position is: cursorY - logicalHiddenAbove + effectiveScrollOffset
	visibleY := b.cursorY - logicalHiddenAbove + effectiveScrollOffset

	// Check if cursor is within visible vertical area
	if visibleY < 0 || visibleY >= b.rows {
		return -1
	}

	return visibleY
}

// --- Horizontal Scroll Methods ---

// SetHorizOffset sets the horizontal scroll offset
func (b *Buffer) SetHorizOffset(offset int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if offset < 0 {
		offset = 0
	}
	b.horizOffset = offset
	b.markDirty()
}

// GetHorizOffset returns current horizontal scroll offset
func (b *Buffer) GetHorizOffset() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.horizOffset
}

// NotifyManualHorizScroll should be called when the user manually scrolls horizontally.
// This temporarily suppresses horizontal auto-scrolling.
func (b *Buffer) NotifyManualHorizScroll() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastManualHorizScroll = time.Now()
}

// ClearHorizMemos clears all horizontal scroll memos before a new paint frame.
// Call this at the start of each paint.
func (b *Buffer) ClearHorizMemos() {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Ensure slice is sized to match screen rows
	if len(b.horizMemos) != b.rows {
		b.horizMemos = make([]HorizMemo, b.rows)
	} else {
		// Clear existing entries
		for i := range b.horizMemos {
			b.horizMemos[i] = HorizMemo{}
		}
	}
}

// SetHorizMemo sets the horizontal scroll memo for a specific scanline.
// Call this during paint for each row where the cursor's logical line is being rendered.
func (b *Buffer) SetHorizMemo(scanline int, memo HorizMemo) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if scanline >= 0 && scanline < len(b.horizMemos) {
		b.horizMemos[scanline] = memo
	}
}

// GetHorizMemos returns a copy of all horizontal memos (for debugging/inspection).
func (b *Buffer) GetHorizMemos() []HorizMemo {
	b.mu.RLock()
	defer b.mu.RUnlock()
	result := make([]HorizMemo, len(b.horizMemos))
	copy(result, b.horizMemos)
	return result
}

// isHorizAutoScrollActive returns true if horizontal auto-scroll should be active.
// It checks keyboard activity, manual scroll cooldown, and scrollback viewing state.
func (b *Buffer) isHorizAutoScrollActive() bool {
	// Must have recent keyboard activity
	if b.lastKeyboardActivity.IsZero() {
		return false
	}
	if time.Since(b.lastKeyboardActivity) >= keyboardAutoScrollDuration {
		return false
	}

	// Don't auto-scroll if viewing scrollback
	if b.IsViewingScrollbackInternal() {
		return false
	}

	// Check manual scroll cooldown
	if !b.lastManualHorizScroll.IsZero() {
		timeSinceManualScroll := time.Since(b.lastManualHorizScroll)

		// If keyboard activity occurred after manual scroll, allow auto-scroll
		if b.lastKeyboardActivity.After(b.lastManualHorizScroll) {
			return true
		}

		// If within cooldown period, check if a scroll-causing event occurred after manual scroll
		if timeSinceManualScroll < manualScrollCooldown {
			return false
		}

		// Past cooldown, but need a scroll-causing event to have occurred after manual scroll
		if b.lastScrollCausingEvent.IsZero() || b.lastScrollCausingEvent.Before(b.lastManualHorizScroll) {
			return false
		}
	}

	return true
}

// IsViewingScrollbackInternal returns true if currently viewing scrollback buffer (internal, no lock).
func (b *Buffer) IsViewingScrollbackInternal() bool {
	effectiveRows := b.EffectiveRows()
	logicalHiddenAbove := 0
	if effectiveRows > b.rows {
		logicalHiddenAbove = effectiveRows - b.rows
	}
	// If scroll offset is beyond logicalHiddenAbove, we're viewing actual scrollback
	return b.scrollOffset > logicalHiddenAbove
}

// CheckCursorAutoScrollHoriz checks horizontal memos and auto-scrolls if needed.
// Returns true if a scroll occurred.
// Call this after paint, after memos have been populated.
func (b *Buffer) CheckCursorAutoScrollHoriz() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Check if auto-scroll is disabled by DEC Private Mode
	if b.autoScrollDisabled {
		return false
	}

	// Check if horizontal auto-scroll is active
	if !b.isHorizAutoScrollActive() {
		return false
	}

	// FIRST: If we're viewing scrollback, snap to logical screen boundary first.
	// The scrollback should be forced off screen before any horizontal auto-scrolling.
	// (Vertical auto-scroll handles this too, but in case horizontal is called first
	// or vertical didn't trigger, we ensure scrollback is off-screen here too.)
	effectiveRows := b.EffectiveRows()
	logicalHiddenAbove := 0
	if effectiveRows > b.rows {
		logicalHiddenAbove = effectiveRows - b.rows
	}
	if b.scrollOffset > logicalHiddenAbove {
		b.scrollOffset = logicalHiddenAbove
		b.markDirty()
		return true
	}

	// Check if cursor line was rendered (from cursorDrawnLastFrame set by vertical tracking)
	// If cursor line isn't visible vertically, don't do horizontal auto-scroll
	if !b.cursorDrawnLastFrame {
		return false
	}

	// Analyze memos to find the nearest distance to cursor
	minDistLeft := -1
	minDistRight := -1
	cursorFound := false

	// For absolute positioning check
	cursorX := b.cursorX

	for _, memo := range b.horizMemos {
		if !memo.Valid {
			continue
		}

		if memo.CursorLocated {
			cursorFound = true
			break
		}

		if memo.DistanceToLeft > 0 {
			if minDistLeft < 0 || memo.DistanceToLeft < minDistLeft {
				minDistLeft = memo.DistanceToLeft
			}
		}

		if memo.DistanceToRight > 0 {
			if minDistRight < 0 || memo.DistanceToRight < minDistRight {
				minDistRight = memo.DistanceToRight
			}
		}
	}

	// If cursor was found in rendered area, no scroll needed
	if cursorFound {
		return false
	}

	// If no distances found, no scroll possible
	if minDistLeft < 0 && minDistRight < 0 {
		return false
	}

	// Determine scroll direction and amount
	var scrollAmount int
	var scrollLeft bool

	// Check for absolute positioning - look for cursor just outside rendered bounds
	if b.isAbsoluteHorizPosition {
		for _, memo := range b.horizMemos {
			if !memo.Valid {
				continue
			}
			// Cursor just one position to the left of rendered area
			if cursorX == memo.LeftmostCell-1 && memo.DistanceToLeft == 1 {
				scrollAmount = 1
				scrollLeft = true
				break
			}
			// Cursor just one position to the right of rendered area
			if cursorX == memo.RightmostCell+1 && memo.DistanceToRight == 1 {
				scrollAmount = 1
				scrollLeft = false
				break
			}
		}
	}

	// If absolute positioning didn't set a scroll, use direction-based or nearest algorithm
	if scrollAmount == 0 {
		if b.lastHorizCursorMoveDir < 0 && minDistLeft > 0 {
			// Known direction: left
			scrollAmount = minDistLeft
			scrollLeft = true
		} else if b.lastHorizCursorMoveDir > 0 && minDistRight > 0 {
			// Known direction: right
			scrollAmount = minDistRight
			scrollLeft = false
		} else {
			// Direction unknown - use nearest, favor left on tie
			if minDistLeft > 0 && minDistRight > 0 {
				if minDistLeft <= minDistRight {
					scrollAmount = minDistLeft
					scrollLeft = true
				} else {
					scrollAmount = minDistRight
					scrollLeft = false
				}
			} else if minDistLeft > 0 {
				scrollAmount = minDistLeft
				scrollLeft = true
			} else if minDistRight > 0 {
				scrollAmount = minDistRight
				scrollLeft = false
			}
		}
	}

	// Apply the scroll
	if scrollAmount > 0 {
		if scrollLeft {
			// Always scroll one extra position left to keep the cursor away from
			// the edge. This provides a visual buffer so the character isn't at
			// the very edge of the visible area.
			scrollAmount++
			b.horizOffset -= scrollAmount
			if b.horizOffset < 0 {
				b.horizOffset = 0
			}
		} else {
			b.horizOffset += scrollAmount
			// Cap at max offset
			maxOffset := b.getMaxHorizOffsetInternal()
			if b.horizOffset > maxOffset {
				b.horizOffset = maxOffset
			}
		}
		b.markDirty()
		return true
	}

	return false
}

// getMaxHorizOffsetInternal calculates max horizontal offset (internal, no lock).
func (b *Buffer) getMaxHorizOffsetInternal() int {
	// Similar to GetMaxHorizOffset but without locking
	cols := b.cols
	splitWidth := b.splitContentWidth
	currentOffset := b.horizOffset

	// Calculate longest line - simplified version for internal use
	longest := 0
	for _, line := range b.screen {
		if len(line) > longest {
			longest = len(line)
		}
	}
	if splitWidth > longest {
		longest = splitWidth
	}

	contentBasedMax := 0
	if longest > cols {
		contentBasedMax = longest - cols
	}

	if currentOffset > contentBasedMax {
		return currentOffset
	}
	return contentBasedMax
}

// SetScrollbackDisabled enables or disables scrollback accumulation.
// When disabled, lines scrolling off the top are discarded instead of saved.
// Existing scrollback is preserved but inaccessible until re-enabled.
func (b *Buffer) SetScrollbackDisabled(disabled bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.scrollbackDisabled = disabled
	// Reset scroll offset when disabling to prevent viewing hidden scrollback
	if disabled && b.scrollOffset > 0 {
		b.scrollOffset = 0
	}
	b.markDirty()
}

// IsScrollbackDisabled returns true if scrollback accumulation is disabled
func (b *Buffer) IsScrollbackDisabled() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.scrollbackDisabled
}

// GetLongestLineOnScreen returns the length of the longest line currently on the logical screen
func (b *Buffer) GetLongestLineOnScreen() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	longest := 0
	for _, line := range b.screen {
		if len(line) > longest {
			longest = len(line)
		}
	}
	return longest
}

// GetLongestLineInScrollback returns the length of the longest line in scrollback
func (b *Buffer) GetLongestLineInScrollback() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	longest := 0
	for _, line := range b.scrollback {
		if len(line) > longest {
			longest = len(line)
		}
	}
	return longest
}

// GetLongestLineVisible returns the longest line width currently visible.
// Only includes scrollback width when the yellow dashed boundary line is visible.
func (b *Buffer) GetLongestLineVisible() int {
	// Check if scrollback boundary is visible (don't hold lock during this call)
	boundaryVisible := b.GetScrollbackBoundaryVisibleRow() > 0

	b.mu.RLock()
	defer b.mu.RUnlock()

	longest := 0

	// Only include scrollback width if the boundary is visible
	// (meaning we can actually see scrollback content)
	if boundaryVisible {
		for _, line := range b.scrollback {
			if len(line) > longest {
				longest = len(line)
			}
		}
	}

	// Always include screen content width
	for _, line := range b.screen {
		if len(line) > longest {
			longest = len(line)
		}
	}
	return longest
}

// NeedsHorizScrollbar returns true if there's content beyond the visible width
func (b *Buffer) NeedsHorizScrollbar() bool {
	b.mu.RLock()
	cols := b.cols
	splitWidth := b.splitContentWidth
	currentOffset := b.horizOffset
	b.mu.RUnlock()

	// If already scrolled right, show scrollbar so user can scroll back
	if currentOffset > 0 {
		return true
	}

	// GetLongestLineVisible handles the scrollOffset logic internally:
	// - If scrollOffset == 0: returns logical screen content width only
	// - If scrollOffset > 0: returns max of scrollback and screen content width
	longest := b.GetLongestLineVisible()

	// Also consider split content width (for split regions)
	if splitWidth > longest {
		longest = splitWidth
	}
	return longest > cols
}

// GetMaxHorizOffset returns the maximum horizontal scroll offset
func (b *Buffer) GetMaxHorizOffset() int {
	b.mu.RLock()
	cols := b.cols
	splitWidth := b.splitContentWidth
	currentOffset := b.horizOffset
	b.mu.RUnlock()

	// GetLongestLineVisible handles the scrollOffset logic internally:
	// - If scrollOffset == 0: returns logical screen content width only
	// - If scrollOffset > 0: returns max of scrollback and screen content width
	longest := b.GetLongestLineVisible()

	// Also consider split content width (for split regions)
	if splitWidth > longest {
		longest = splitWidth
	}

	contentBasedMax := 0
	if longest > cols {
		contentBasedMax = longest - cols
	}

	// Preserve current scroll position as valid - don't snap left when
	// scrolling vertically from wide scrollback to narrower logical screen.
	// Once user scrolls left past contentBasedMax, they can't scroll right again.
	if currentOffset > contentBasedMax {
		return currentOffset
	}
	return contentBasedMax
}
