package purfecterm

// --- Line Attribute Methods ---

// SetLineAttribute sets the display attribute for the current line
func (b *Buffer) SetLineAttribute(attr LineAttribute) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cursorY >= 0 && b.cursorY < len(b.lineInfos) {
		b.lineInfos[b.cursorY].Attribute = attr
		b.markDirty()
	}
}

// GetLineAttribute returns the display attribute for the specified line
func (b *Buffer) GetLineAttribute(y int) LineAttribute {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if y >= 0 && y < len(b.lineInfos) {
		return b.lineInfos[y].Attribute
	}
	return LineAttrNormal
}

// GetLineInfo returns the full LineInfo for the specified line
func (b *Buffer) GetLineInfo(y int) LineInfo {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if y >= 0 && y < len(b.lineInfos) {
		return b.lineInfos[y]
	}
	return DefaultLineInfo()
}

// GetVisibleLineAttribute returns the line attribute accounting for scroll offset
func (b *Buffer) GetVisibleLineAttribute(y int) LineAttribute {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.getVisibleLineInfoInternal(y).Attribute
}

// GetVisibleLineInfo returns the full LineInfo accounting for scroll offset
func (b *Buffer) GetVisibleLineInfo(y int) LineInfo {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.getVisibleLineInfoInternal(y)
}

func (b *Buffer) getVisibleLineInfoInternal(y int) LineInfo {
	if y < 0 || y >= b.rows {
		return LineInfo{Attribute: LineAttrNormal, DefaultCell: b.screenInfo.DefaultCell}
	}

	effectiveRows := b.EffectiveRows()
	scrollbackSize := len(b.scrollback)

	// Calculate how much of the logical screen is hidden above
	logicalHiddenAbove := 0
	if effectiveRows > b.rows {
		logicalHiddenAbove = effectiveRows - b.rows
	}

	// Use helper function for magnetic zone handling (same as GetVisibleCell)
	effectiveScrollOffset := b.getEffectiveScrollOffset()

	totalScrollableAbove := scrollbackSize + logicalHiddenAbove

	if effectiveScrollOffset == 0 {
		// Not scrolled - show bottom of logical screen
		logicalY := logicalHiddenAbove + y
		if logicalY >= 0 && logicalY < len(b.lineInfos) {
			return b.lineInfos[logicalY]
		}
		return LineInfo{Attribute: LineAttrNormal, DefaultCell: b.screenInfo.DefaultCell}
	}

	// Scrolled - map to scrollback or logical screen
	absoluteY := totalScrollableAbove - effectiveScrollOffset + y

	if absoluteY < scrollbackSize {
		// In scrollback
		if absoluteY >= 0 && absoluteY < len(b.scrollbackInfo) {
			return b.scrollbackInfo[absoluteY]
		}
		return LineInfo{Attribute: LineAttrNormal, DefaultCell: b.screenInfo.DefaultCell}
	}

	// In logical screen
	logicalY := absoluteY - scrollbackSize
	if logicalY >= 0 && logicalY < len(b.lineInfos) {
		return b.lineInfos[logicalY]
	}
	return LineInfo{Attribute: LineAttrNormal, DefaultCell: b.screenInfo.DefaultCell}
}
