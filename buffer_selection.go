package purfecterm

// --- Text Selection Methods ---

// screenToBufferY converts a screen Y coordinate to a buffer-absolute Y coordinate
// Buffer-absolute coordinates: Y=0 is the oldest scrollback line, increasing toward current
func (b *Buffer) screenToBufferY(screenY int) int {
	scrollbackSize := len(b.scrollback)
	effectiveRows := b.EffectiveRows()

	// Calculate how much of the logical screen is hidden above
	logicalHiddenAbove := 0
	if effectiveRows > b.rows {
		logicalHiddenAbove = effectiveRows - b.rows
	}

	// Total scrollable area above visible
	totalScrollableAbove := scrollbackSize + logicalHiddenAbove

	// Use effective scroll offset to account for magnetic zone
	effectiveScrollOffset := b.getEffectiveScrollOffset()

	// Convert screen Y to buffer-absolute Y
	return totalScrollableAbove - effectiveScrollOffset + screenY
}

// bufferToScreenY converts a buffer-absolute Y coordinate to a screen Y coordinate
// Returns -1 if the buffer Y is not currently visible on screen
func (b *Buffer) bufferToScreenY(bufferY int) int {
	scrollbackSize := len(b.scrollback)
	effectiveRows := b.EffectiveRows()

	// Calculate how much of the logical screen is hidden above
	logicalHiddenAbove := 0
	if effectiveRows > b.rows {
		logicalHiddenAbove = effectiveRows - b.rows
	}

	// Total scrollable area above visible
	totalScrollableAbove := scrollbackSize + logicalHiddenAbove

	// Use effective scroll offset to account for magnetic zone
	effectiveScrollOffset := b.getEffectiveScrollOffset()

	// Convert buffer-absolute Y to screen Y
	screenY := bufferY - totalScrollableAbove + effectiveScrollOffset

	// Check if visible
	if screenY < 0 || screenY >= b.rows {
		return -1
	}
	return screenY
}

// StartSelection begins a text selection (coordinates are screen-relative)
func (b *Buffer) StartSelection(x, y int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.selectionActive = true
	// Convert to buffer-absolute coordinates for stable selection
	bufferY := b.screenToBufferY(y)
	b.selStartX = x
	b.selStartY = bufferY
	b.selEndX = x
	b.selEndY = bufferY
	b.markDirty()
}

// UpdateSelection updates the end point of the selection (coordinates are screen-relative)
func (b *Buffer) UpdateSelection(x, y int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.selectionActive {
		return
	}
	// Convert to buffer-absolute coordinates
	bufferY := b.screenToBufferY(y)
	b.selEndX = x
	b.selEndY = bufferY
	b.markDirty()
}

// EndSelection finalizes the selection
func (b *Buffer) EndSelection() {
	// Selection remains active until cleared
}

// ClearSelection clears any active selection
func (b *Buffer) ClearSelection() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.selectionActive = false
	b.markDirty()
}

// HasSelection returns true if there's an active selection
func (b *Buffer) HasSelection() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.selectionActive
}

// GetSelection returns the normalized selection bounds in buffer-absolute coordinates
func (b *Buffer) GetSelection() (startX, startY, endX, endY int, active bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if !b.selectionActive {
		return 0, 0, 0, 0, false
	}
	sx, sy := b.selStartX, b.selStartY
	ex, ey := b.selEndX, b.selEndY
	if sy > ey || (sy == ey && sx > ex) {
		sx, sy, ex, ey = ex, ey, sx, sy
	}
	return sx, sy, ex, ey, true
}

// IsCellInSelection checks if a cell at screen coordinates is within the selection
func (b *Buffer) IsCellInSelection(screenX, screenY int) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if !b.selectionActive {
		return false
	}

	// Convert screen Y to buffer-absolute Y
	bufferY := b.screenToBufferY(screenY)

	// Get normalized selection bounds
	sx, sy := b.selStartX, b.selStartY
	ex, ey := b.selEndX, b.selEndY
	if sy > ey || (sy == ey && sx > ex) {
		sx, sy, ex, ey = ex, ey, sx, sy
	}

	// Check if the cell is within the selection
	if bufferY < sy || bufferY > ey {
		return false
	}
	if bufferY == sy && screenX < sx {
		return false
	}
	if bufferY == ey && screenX > ex {
		return false
	}
	return true
}

// getCellByAbsoluteY gets a cell using buffer-absolute Y coordinate
func (b *Buffer) getCellByAbsoluteY(x, bufferY int) Cell {
	scrollbackSize := len(b.scrollback)

	if bufferY < 0 {
		return b.screenInfo.DefaultCell
	}

	if bufferY < scrollbackSize {
		// In scrollback
		return b.getScrollbackCell(x, bufferY)
	}

	// In logical screen
	logicalY := bufferY - scrollbackSize
	return b.getLogicalCell(x, logicalY)
}

// GetSelectedText returns the text in the current selection
func (b *Buffer) GetSelectedText() string {
	sx, sy, ex, ey, active := b.GetSelection()
	if !active {
		return ""
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	// Calculate total buffer height for bounds checking
	scrollbackSize := len(b.scrollback)
	effectiveRows := b.EffectiveRows()
	totalBufferHeight := scrollbackSize + effectiveRows

	var lines []string
	for bufferY := sy; bufferY <= ey && bufferY < totalBufferHeight; bufferY++ {
		startX := 0
		endX := b.cols
		if bufferY == sy {
			startX = sx
		}
		if bufferY == ey {
			endX = ex + 1
		}
		var lineRunes []rune
		for x := startX; x < endX && x < b.cols; x++ {
			cell := b.getCellByAbsoluteY(x, bufferY)
			lineRunes = append(lineRunes, cell.Char)
		}
		line := string(lineRunes)
		for len(line) > 0 && (line[len(line)-1] == ' ' || line[len(line)-1] == 0) {
			line = line[:len(line)-1]
		}
		lines = append(lines, line)
	}

	result := ""
	for i, line := range lines {
		result += line
		if i < len(lines)-1 {
			result += "\n"
		}
	}
	return result
}

// IsInSelection returns true if the given screen position is within the selection
// Deprecated: Use IsCellInSelection for clearer semantics
func (b *Buffer) IsInSelection(x, y int) bool {
	return b.IsCellInSelection(x, y)
}

// SelectAll selects all text in the terminal (including scrollback)
func (b *Buffer) SelectAll() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.selectionActive = true
	b.selStartX = 0
	b.selStartY = 0 // Buffer-absolute 0 = oldest scrollback line
	b.selEndX = b.cols - 1
	// End at the last line of the logical screen
	scrollbackSize := len(b.scrollback)
	effectiveRows := b.EffectiveRows()
	b.selEndY = scrollbackSize + effectiveRows - 1
	b.markDirty()
}
