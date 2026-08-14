package purfecterm

import "time"

// Scroll region (DECSTBM) and origin mode (DECOM).
//
// The region is a band [scrollTop, scrollBottom] (0-based, inclusive) within the
// effective rows. LineFeed/IND/RI/wrap and SU/SD/IL/DL confine their scrolling
// to it. Only a scroll of the FULL screen feeds scrollback (session history); an
// interior-region scroll discards the departing line and is kept out of the
// capture transcript, so a hosted full-screen app (which parks a status line
// below the region) never pollutes history.

// scrollRegionLocked resolves the effective region bounds. Caller holds the lock.
func (b *Buffer) scrollRegionLocked() (top, bottom int) {
	rows := b.EffectiveRows()
	if !b.hasScrollRegion {
		return 0, rows - 1
	}
	top = b.scrollTop
	bottom = b.scrollBottom
	if top < 0 {
		top = 0
	}
	if bottom >= rows {
		bottom = rows - 1
	}
	if top >= bottom {
		return 0, rows - 1
	}
	return top, bottom
}

// GetScrollRegion returns the effective 0-based inclusive region bounds.
func (b *Buffer) GetScrollRegion() (top, bottom int) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.scrollRegionLocked()
}

// SetScrollRegion implements DECSTBM (CSI Pt ; Pb r). Arguments are 1-based;
// a bottom of 0 (or omitted) means the last row. An invalid region (top >= bottom)
// resets to the full screen. DECSTBM homes the cursor (to the region origin under
// DECOM, else screen home).
func (b *Buffer) SetScrollRegion(top, bottom int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	rows := b.EffectiveRows()

	t := top - 1
	var bt int
	if bottom <= 0 {
		bt = rows - 1
	} else {
		bt = bottom - 1
	}
	if t < 0 {
		t = 0
	}
	if bt >= rows {
		bt = rows - 1
	}

	if t >= bt || (t == 0 && bt == rows-1) {
		// Invalid or full-screen: no region in effect.
		b.hasScrollRegion = false
		b.scrollTop = 0
		b.scrollBottom = rows - 1
	} else {
		b.hasScrollRegion = true
		b.scrollTop = t
		b.scrollBottom = bt
	}

	b.cursorX = 0
	if b.originMode {
		b.cursorY = b.scrollTop
	} else {
		b.cursorY = 0
	}
	b.setHorizMoveDir(0, true)
	b.markDirty()
}

// ResetScrollRegion clears the scroll region (full screen) and origin mode.
// Used by RIS and DECSTR.
func (b *Buffer) ResetScrollRegion() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.hasScrollRegion = false
	b.scrollTop = 0
	b.scrollBottom = b.EffectiveRows() - 1
	b.originMode = false
	b.hasLRMargins = false
	b.marginLeft = 0
	b.marginRight = b.EffectiveCols() - 1
	b.leftRightMarginMode = false
	b.markDirty()
}

// SetOriginMode implements DECOM (CSI ? 6 h/l). Setting or resetting it homes the
// cursor (to the region top under origin mode, else screen home).
func (b *Buffer) SetOriginMode(on bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.originMode = on
	b.cursorX = 0
	if on {
		top, _ := b.scrollRegionLocked()
		b.cursorY = top
	} else {
		b.cursorY = 0
	}
	b.setHorizMoveDir(0, true)
	b.markDirty()
}

// IsOriginMode reports whether DECOM is active.
func (b *Buffer) IsOriginMode() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.originMode
}

// SetCursorPosition is the CUP/HVP entry point: col and row are 0-based screen
// coordinates (col is a VISUAL column). Under origin mode the row is relative to
// the top margin and confined to the region. The visual column is translated to
// a logical cell index in standard mode, mirroring SetCursorVisual.
func (b *Buffer) SetCursorPosition(col, row int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	left, right, lrActive := b.lrMarginsLocked()
	if b.originMode {
		top, bottom := b.scrollRegionLocked()
		row += top
		if row < top {
			row = top
		}
		if row > bottom {
			row = bottom
		}
	}
	if !b.flexWidthMode {
		r := row
		if r < 0 {
			r = 0
		}
		col = b.visualToLogicalLocked(r, col)
	}
	// Under origin mode the column is relative to the left margin and confined
	// to the margin box.
	if b.originMode && lrActive {
		col += left
		if col < left {
			col = left
		}
		if col > right {
			col = right
		}
	}
	b.setCursorInternal(col, row)
}

// scrollRegionUp scrolls rows [top,bottom] up by one. The line leaving at top is
// pushed to scrollback (and reported via OnScrollLineOff) only when the region is
// the full screen; an interior-region scroll discards it. Caller holds the lock.
func (b *Buffer) scrollRegionUp(top, bottom int) {
	if len(b.screen) == 0 || top < 0 || bottom >= len(b.screen) || top > bottom {
		return
	}
	if b.liveEnabled() {
		b.flushLiveWriteRun()
	}
	if left, right, active := b.lrMarginsLocked(); active {
		// Left/right margins in effect: scroll only the rectangle, leaving the
		// columns outside untouched. A boxed scroll never reaches scrollback.
		b.scrollRectUp(top, bottom, left, right)
		b.lastScrollCausingEvent = time.Now()
		b.lastCursorMoveDir = 1
		b.markDirty()
		return
	}
	if top == 0 && bottom == b.EffectiveRows()-1 {
		b.pushLineToScrollback(b.screen[top], b.lineInfos[top])
		if b.liveEnabled() {
			b.captureObserver.OnScrollLineOff(1)
		}
	}
	for y := top; y < bottom; y++ {
		b.screen[y] = b.screen[y+1]
		b.lineInfos[y] = b.lineInfos[y+1]
	}
	b.screen[bottom] = b.makeEmptyLine()
	b.lineInfos[bottom] = b.makeDefaultLineInfo()
	b.lastScrollCausingEvent = time.Now()
	b.lastCursorMoveDir = 1 // toward newer content
	b.markDirty()
}

// scrollRegionDown scrolls rows [top,bottom] down by one, blanking the top line.
// Reverse scrolling never reaches scrollback. Caller holds the lock.
func (b *Buffer) scrollRegionDown(top, bottom int) {
	if len(b.screen) == 0 || top < 0 || bottom >= len(b.screen) || top > bottom {
		return
	}
	if b.liveEnabled() {
		b.flushLiveWriteRun()
	}
	if left, right, active := b.lrMarginsLocked(); active {
		b.scrollRectDown(top, bottom, left, right)
		b.lastCursorMoveDir = -1
		b.markDirty()
		return
	}
	for y := bottom; y > top; y-- {
		b.screen[y] = b.screen[y-1]
		b.lineInfos[y] = b.lineInfos[y-1]
	}
	b.screen[top] = b.makeEmptyLine()
	b.lineInfos[top] = b.makeDefaultLineInfo()
	b.lastCursorMoveDir = -1
	b.markDirty()
}

// advanceLineInternal moves the cursor down one row within the scroll region,
// scrolling the region up when the cursor sits on the bottom margin. Used by
// LineFeed/Newline and the auto-wrap paths. Caller holds the lock.
func (b *Buffer) advanceLineInternal() {
	top, bottom := b.scrollRegionLocked()
	if b.cursorY == bottom {
		b.scrollRegionUp(top, bottom)
	} else if b.cursorY < b.EffectiveRows()-1 {
		b.trackCursorYMove(b.cursorY + 1)
		b.cursorY++
	}
}

// reverseLineInternal moves the cursor up one row, reverse-scrolling the region
// when the cursor sits on the top margin. Caller holds the lock.
func (b *Buffer) reverseLineInternal() {
	top, bottom := b.scrollRegionLocked()
	if b.cursorY == top {
		b.scrollRegionDown(top, bottom)
	} else if b.cursorY > 0 {
		b.trackCursorYMove(b.cursorY - 1)
		b.cursorY--
	}
}

// CursorReportPosition returns the 1-based cursor position for CPR/DSR reports:
// the column is VISUAL (the wcwidth contract), and under origin mode the row is
// relative to the top margin.
func (b *Buffer) CursorReportPosition() (row, col int) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	col = b.logicalToVisualLocked(b.cursorY, b.cursorX) + 1
	row = b.cursorY + 1
	if b.originMode {
		top, _ := b.scrollRegionLocked()
		row = b.cursorY - top + 1
	}
	if row < 1 {
		row = 1
	}
	return row, col
}

// ReverseIndex implements RI (ESC M): move up one line within the region,
// reverse-scrolling at the top margin.
func (b *Buffer) ReverseIndex() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.liveEnabled() {
		b.flushLiveWriteRun()
	}
	b.reverseLineInternal()
	b.markDirty()
}

// --- Left/right margins (DECLRMM ?69 + DECSLRM) ---
//
// Margins are stored as 0-based inclusive LOGICAL columns. Under the standard
// (wcwidth) contract narrow content has logical == visual, so for the realistic
// use of DECSLRM — side-by-side ASCII panes, boxes — these are exactly the
// visual columns the app named. Wide glyphs straddling a margin (DECSLRM's
// undefined corner) stay self-consistent rather than perfectly visual.

// lrMarginsLocked resolves the effective left/right margin columns. active is
// false when the margins span the full width. Caller holds the lock.
func (b *Buffer) lrMarginsLocked() (left, right int, active bool) {
	cols := b.EffectiveCols()
	if !b.hasLRMargins {
		return 0, cols - 1, false
	}
	left = b.marginLeft
	right = b.marginRight
	if left < 0 {
		left = 0
	}
	if right >= cols {
		right = cols - 1
	}
	if left >= right {
		return 0, cols - 1, false
	}
	return left, right, true
}

// GetLeftRightMargins returns the effective 0-based inclusive margin columns.
func (b *Buffer) GetLeftRightMargins() (left, right int) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	left, right, _ = b.lrMarginsLocked()
	return left, right
}

// SetLeftRightMarginMode implements DECLRMM (CSI ? 69 h/l). Disabling it also
// clears any left/right margins (they cannot exist without the mode).
func (b *Buffer) SetLeftRightMarginMode(on bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.leftRightMarginMode = on
	if !on {
		b.hasLRMargins = false
		b.marginLeft = 0
		b.marginRight = b.EffectiveCols() - 1
	}
	b.markDirty()
}

// IsLeftRightMarginMode reports whether DECLRMM is active.
func (b *Buffer) IsLeftRightMarginMode() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.leftRightMarginMode
}

// SetLeftRightMargins implements DECSLRM (CSI Pl ; Pr s). Arguments are 1-based;
// a right of 0 (or omitted) means the last column. Ignored unless DECLRMM is
// enabled or the region is invalid (left >= right). DECSLRM homes the cursor.
func (b *Buffer) SetLeftRightMargins(left, right int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.leftRightMarginMode {
		return
	}
	cols := b.EffectiveCols()
	l := left - 1
	var r int
	if right <= 0 {
		r = cols - 1
	} else {
		r = right - 1
	}
	if l < 0 {
		l = 0
	}
	if r >= cols {
		r = cols - 1
	}
	if l >= r || (l == 0 && r == cols-1) {
		b.hasLRMargins = false
		b.marginLeft = 0
		b.marginRight = cols - 1
	} else {
		b.hasLRMargins = true
		b.marginLeft = l
		b.marginRight = r
	}

	// DECSLRM homes the cursor (region origin under DECOM, else screen home).
	if b.originMode {
		top, _ := b.scrollRegionLocked()
		b.cursorY = top
		b.cursorX = b.marginLeft
	} else {
		b.cursorX = 0
		b.cursorY = 0
	}
	b.setHorizMoveDir(0, true)
	b.markDirty()
}

// copyRowWindow copies the logical column window [left,right] from row src into
// row dst, padding dst first. Caller holds the lock.
func (b *Buffer) copyRowWindow(src, dst, left, right int) {
	b.ensureLineLength(dst, right+1)
	srcLine := b.screen[src]
	dstLine := b.screen[dst]
	blank := b.currentDefaultCell()
	for x := left; x <= right; x++ {
		if x < len(srcLine) {
			dstLine[x] = srcLine[x]
		} else {
			dstLine[x] = blank
		}
	}
}

// blankRowWindow blanks the logical column window [left,right] of row y with the
// current pen. Caller holds the lock.
func (b *Buffer) blankRowWindow(y, left, right int) {
	b.ensureLineLength(y, right+1)
	line := b.screen[y]
	blank := b.currentDefaultCell()
	for x := left; x <= right; x++ {
		line[x] = blank
	}
}

// scrollRectUp scrolls the rectangle [top,bottom] x [left,right] up by one; the
// top row's window is discarded, the bottom row's window blanked. Columns
// outside the margins are untouched, and nothing reaches scrollback. Caller
// holds the lock.
func (b *Buffer) scrollRectUp(top, bottom, left, right int) {
	for y := top; y < bottom; y++ {
		b.copyRowWindow(y+1, y, left, right)
	}
	b.blankRowWindow(bottom, left, right)
}

// scrollRectDown scrolls the rectangle down by one, blanking the top row's
// window. Caller holds the lock.
func (b *Buffer) scrollRectDown(top, bottom, left, right int) {
	for y := bottom; y > top; y-- {
		b.copyRowWindow(y-1, y, left, right)
	}
	b.blankRowWindow(top, left, right)
}
