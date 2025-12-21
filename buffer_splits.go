package purfecterm

// --- Screen Split Methods ---

// DeleteAllScreenSplits removes all screen splits.
func (b *Buffer) DeleteAllScreenSplits() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.screenSplits = make(map[int]*ScreenSplit)
	b.markDirty()
}

// DeleteScreenSplit removes a specific screen split by ID.
func (b *Buffer) DeleteScreenSplit(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.screenSplits, id)
	b.markDirty()
}

// SetScreenSplit creates or updates a screen split.
// screenY: Y coordinate in sprite units where this split begins on screen
// bufferRow, bufferCol: 0-indexed logical screen coordinates to draw from
// topFineScroll, leftFineScroll: 0 to (subdivisions-1), higher = more clipped
// charWidthScale: character width multiplier (0 = inherit)
// lineDensity: line density override (0 = inherit)
func (b *Buffer) SetScreenSplit(id int, screenY, bufferRow, bufferCol, topFineScroll, leftFineScroll int, charWidthScale float64, lineDensity int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Clamp fine scroll values
	if topFineScroll < 0 {
		topFineScroll = 0
	}
	if topFineScroll >= b.spriteUnitY {
		topFineScroll = b.spriteUnitY - 1
	}
	if leftFineScroll < 0 {
		leftFineScroll = 0
	}
	if leftFineScroll >= b.spriteUnitX {
		leftFineScroll = b.spriteUnitX - 1
	}

	b.screenSplits[id] = &ScreenSplit{
		ScreenY:        screenY,
		BufferRow:      bufferRow,
		BufferCol:      bufferCol,
		TopFineScroll:  topFineScroll,
		LeftFineScroll: leftFineScroll,
		CharWidthScale: charWidthScale,
		LineDensity:    lineDensity,
	}
	b.markDirty()
}

// GetScreenSplit returns a screen split by ID, or nil if not found.
func (b *Buffer) GetScreenSplit(id int) *ScreenSplit {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.screenSplits[id]
}

// GetScreenSplitsSorted returns all screen splits sorted by ScreenY coordinate.
func (b *Buffer) GetScreenSplitsSorted() []*ScreenSplit {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(b.screenSplits) == 0 {
		return nil
	}

	// Collect all splits
	splits := make([]*ScreenSplit, 0, len(b.screenSplits))
	for _, split := range b.screenSplits {
		splits = append(splits, split)
	}

	// Sort by ScreenY
	for i := 0; i < len(splits)-1; i++ {
		for j := i + 1; j < len(splits); j++ {
			if splits[j].ScreenY < splits[i].ScreenY {
				splits[i], splits[j] = splits[j], splits[i]
			}
		}
	}

	return splits
}

// GetCellForSplit returns a cell for split rendering.
// screenX/screenY: position within the split region (0 = first cell of split)
// bufferRow/bufferCol: buffer offset for this split (0-indexed)
// The cell is fetched from the logical screen at position (screenX + bufferCol, screenY + bufferRow)
// accounting for the current scroll offset.
func (b *Buffer) GetCellForSplit(screenX, screenY, bufferRow, bufferCol int) Cell {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// Calculate actual buffer position
	actualX := screenX + bufferCol
	actualY := screenY + bufferRow

	if actualY < 0 || actualY >= b.rows {
		return b.screenInfo.DefaultCell
	}

	effectiveRows := b.EffectiveRows()
	scrollbackSize := len(b.scrollback)

	// Calculate how much of the logical screen is hidden above
	logicalHiddenAbove := 0
	if effectiveRows > b.rows {
		logicalHiddenAbove = effectiveRows - b.rows
	}

	// Total scrollable area above visible
	totalScrollableAbove := scrollbackSize + logicalHiddenAbove

	if b.scrollOffset == 0 {
		// Not scrolled - show bottom of logical screen
		logicalY := logicalHiddenAbove + actualY
		return b.getLogicalCell(actualX, logicalY)
	}

	// Scrolled up
	absoluteY := totalScrollableAbove - b.scrollOffset + actualY

	if absoluteY < scrollbackSize {
		return b.getScrollbackCell(actualX, absoluteY)
	}

	logicalY := absoluteY - scrollbackSize
	return b.getLogicalCell(actualX, logicalY)
}

// GetLineAttributeForSplit returns the line attribute for split rendering.
func (b *Buffer) GetLineAttributeForSplit(screenY, bufferRow int) LineAttribute {
	b.mu.RLock()
	defer b.mu.RUnlock()

	actualY := screenY + bufferRow

	if actualY < 0 || actualY >= b.rows {
		return LineAttrNormal
	}

	effectiveRows := b.EffectiveRows()
	scrollbackSize := len(b.scrollback)

	logicalHiddenAbove := 0
	if effectiveRows > b.rows {
		logicalHiddenAbove = effectiveRows - b.rows
	}

	totalScrollableAbove := scrollbackSize + logicalHiddenAbove

	if b.scrollOffset == 0 {
		logicalY := logicalHiddenAbove + actualY
		if logicalY >= 0 && logicalY < len(b.lineInfos) {
			return b.lineInfos[logicalY].Attribute
		}
		return LineAttrNormal
	}

	absoluteY := totalScrollableAbove - b.scrollOffset + actualY

	if absoluteY < scrollbackSize {
		// Scrollback lines don't have special attributes
		return LineAttrNormal
	}

	logicalY := absoluteY - scrollbackSize
	if logicalY >= 0 && logicalY < len(b.lineInfos) {
		return b.lineInfos[logicalY].Attribute
	}
	return LineAttrNormal
}

// GetLineLengthForSplit returns the effective content length for a split row.
// This is the line length minus the BufferCol offset (content before BufferCol is excluded).
// Used to know when to stop rendering (no more content on line).
func (b *Buffer) GetLineLengthForSplit(screenY, bufferRow, bufferCol int) int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	actualY := screenY + bufferRow

	if actualY < 0 || actualY >= b.rows {
		return 0
	}

	effectiveRows := b.EffectiveRows()
	scrollbackSize := len(b.scrollback)

	logicalHiddenAbove := 0
	if effectiveRows > b.rows {
		logicalHiddenAbove = effectiveRows - b.rows
	}

	totalScrollableAbove := scrollbackSize + logicalHiddenAbove

	var lineLen int
	if b.scrollOffset == 0 {
		logicalY := logicalHiddenAbove + actualY
		if logicalY >= 0 && logicalY < len(b.screen) {
			lineLen = len(b.screen[logicalY])
		}
	} else {
		absoluteY := totalScrollableAbove - b.scrollOffset + actualY
		if absoluteY < scrollbackSize {
			if absoluteY >= 0 && absoluteY < len(b.scrollback) {
				lineLen = len(b.scrollback[absoluteY])
			}
		} else {
			logicalY := absoluteY - scrollbackSize
			if logicalY >= 0 && logicalY < len(b.screen) {
				lineLen = len(b.screen[logicalY])
			}
		}
	}

	// Subtract the BufferCol offset - content before that is excluded from this split
	effectiveLen := lineLen - bufferCol
	if effectiveLen < 0 {
		return 0
	}
	return effectiveLen
}

// SetSplitContentWidth sets the max content width found across all split regions.
// This is called by the renderer after processing splits and is used for horizontal
// scrollbar calculation independent from scrollback content.
func (b *Buffer) SetSplitContentWidth(width int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.splitContentWidth = width
}

// GetSplitContentWidth returns the max content width found across all split regions.
// Returns 0 if no splits are active or have content.
func (b *Buffer) GetSplitContentWidth() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.splitContentWidth
}
