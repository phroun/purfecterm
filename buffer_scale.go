package purfecterm

// --- Screen Scaling Mode Methods ---

// Set132ColumnMode enables or disables 132-column mode (horizontal scale 0.6060)
// This corresponds to DECCOLM (ESC [ ? 3 h / ESC [ ? 3 l)
func (b *Buffer) Set132ColumnMode(enabled bool) {
	b.mu.Lock()
	b.columnMode132 = enabled
	b.markDirty()
	b.mu.Unlock()
	b.notifyScaleChange()
}

// Get132ColumnMode returns whether 132-column mode is enabled
func (b *Buffer) Get132ColumnMode() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.columnMode132
}

// Set40ColumnMode enables or disables 40-column mode (horizontal scale 2.0)
// This is a custom extension
func (b *Buffer) Set40ColumnMode(enabled bool) {
	b.mu.Lock()
	b.columnMode40 = enabled
	b.markDirty()
	b.mu.Unlock()
	b.notifyScaleChange()
}

// Get40ColumnMode returns whether 40-column mode is enabled
func (b *Buffer) Get40ColumnMode() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.columnMode40
}

// SetLineDensity sets the line density (vertical scaling)
// Valid values: 25 (default), 30, 43, 50, 60
// Higher density = more lines in same space = smaller vertical scale
func (b *Buffer) SetLineDensity(density int) {
	b.mu.Lock()
	// Validate density
	switch density {
	case 25, 30, 43, 50, 60:
		b.lineDensity = density
	default:
		b.lineDensity = 25 // Default to 25 if invalid
	}
	b.markDirty()
	b.mu.Unlock()
	b.notifyScaleChange()
}

// GetLineDensity returns the current line density
func (b *Buffer) GetLineDensity() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.lineDensity == 0 {
		return 25 // Default
	}
	return b.lineDensity
}

// GetHorizontalScale returns the combined horizontal scaling factor
// 132-column mode: 0.6060, 40-column mode: 2.0
// If both enabled: 0.6060 * 2.0 = 1.212
func (b *Buffer) GetHorizontalScale() float64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	scale := 1.0
	if b.columnMode132 {
		scale *= 0.6060
	}
	if b.columnMode40 {
		scale *= 2.0
	}
	return scale
}

// GetVerticalScale returns the vertical scaling factor based on line density
// density 25 (default) = scale 1.0
// density 30 = scale 25/30 = 0.8333
// density 43 = scale 25/43 = 0.5814
// density 50 = scale 25/50 = 0.5
// density 60 = scale 25/60 = 0.4167
func (b *Buffer) GetVerticalScale() float64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	density := b.lineDensity
	if density == 0 || density == 25 {
		return 1.0
	}
	return 25.0 / float64(density)
}

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
