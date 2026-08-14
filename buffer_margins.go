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
