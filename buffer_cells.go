package purfecterm

// --- Cell Access Methods ---

// GetCell returns the cell at the given screen position
// For positions beyond stored line length, returns the line's default cell
func (b *Buffer) GetCell(x, y int) Cell {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.getCellInternal(x, y)
}

func (b *Buffer) getCellInternal(x, y int) Cell {
	// Check if y is beyond stored lines
	if y < 0 || y >= len(b.screen) {
		// Return screen default for lines beyond stored content
		return b.screenInfo.DefaultCell
	}

	line := b.screen[y]
	// Check if x is beyond this line's stored content
	if x < 0 || x >= len(line) {
		// Return line's default cell
		if y < len(b.lineInfos) {
			cell := b.lineInfos[y].DefaultCell
			cell.Char = ' '
			return cell
		}
		return EmptyCell()
	}

	return line[x]
}

// GetVisibleCell returns the cell accounting for scroll offset (both vertical and horizontal)
func (b *Buffer) GetVisibleCell(x, y int) Cell {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.getVisibleCellInternal(x, y)
}

func (b *Buffer) getVisibleCellInternal(x, y int) Cell {
	// Apply horizontal scroll offset
	actualX := x + b.horizOffset

	if y < 0 || y >= b.rows {
		return b.screenInfo.DefaultCell
	}

	effectiveRows := b.EffectiveRows()
	scrollbackSize := len(b.scrollback)

	// Calculate how much of the logical screen is hidden above
	// (if logical > physical, some logical rows are above the visible area)
	logicalHiddenAbove := 0
	if effectiveRows > b.rows {
		logicalHiddenAbove = effectiveRows - b.rows
	}

	// Use helper function for magnetic zone handling
	effectiveScrollOffset := b.getEffectiveScrollOffset()

	// Total scrollable area above visible: scrollback + hidden logical rows
	totalScrollableAbove := scrollbackSize + logicalHiddenAbove

	if effectiveScrollOffset == 0 {
		// Not scrolled - show bottom of logical screen
		// Map visible y to logical y (bottom-aligned)
		logicalY := logicalHiddenAbove + y
		return b.getLogicalCell(actualX, logicalY)
	}

	// Scrolled up - need to map visible y to either scrollback or logical screen
	// scrollOffset goes from 0 (not scrolled) to totalScrollableAbove (scrolled to top)
	absoluteY := totalScrollableAbove - effectiveScrollOffset + y

	if absoluteY < scrollbackSize {
		// In scrollback
		return b.getScrollbackCell(actualX, absoluteY)
	}

	// In logical screen
	logicalY := absoluteY - scrollbackSize
	return b.getLogicalCell(actualX, logicalY)
}

// getScrollbackCell returns a cell from the scrollback buffer
func (b *Buffer) getScrollbackCell(x, scrollbackY int) Cell {
	if scrollbackY < 0 || scrollbackY >= len(b.scrollback) {
		return b.screenInfo.DefaultCell
	}

	line := b.scrollback[scrollbackY]
	if x < 0 || x >= len(line) {
		// Beyond line content - use line's default
		if scrollbackY < len(b.scrollbackInfo) {
			cell := b.scrollbackInfo[scrollbackY].DefaultCell
			cell.Char = ' '
			return cell
		}
		return EmptyCell()
	}
	return line[x]
}

// getLogicalCell returns a cell from the logical screen
func (b *Buffer) getLogicalCell(x, logicalY int) Cell {
	if logicalY < 0 {
		return b.screenInfo.DefaultCell
	}

	if logicalY >= len(b.screen) {
		// Beyond stored lines - use screen default
		return b.screenInfo.DefaultCell
	}

	line := b.screen[logicalY]
	if x < 0 || x >= len(line) {
		// Beyond line content - use line's default
		if logicalY < len(b.lineInfos) {
			cell := b.lineInfos[logicalY].DefaultCell
			cell.Char = ' '
			return cell
		}
		return EmptyCell()
	}
	return line[x]
}

// --- Dirty Flag ---

// IsDirty returns true if the buffer has changed since last render
func (b *Buffer) IsDirty() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.dirty
}

// ClearDirty clears the dirty flag
func (b *Buffer) ClearDirty() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.dirty = false
}
