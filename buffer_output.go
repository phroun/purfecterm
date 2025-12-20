package purfecterm

import "time"

// --- Character Writing ---

// WriteChar writes a character at the current cursor position
func (b *Buffer) WriteChar(ch rune) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.writeCharInternal(ch)
}

// getPreviousCellWidth returns the width of the previous cell for ambiguous auto-matching.
// If there's no previous cell or it doesn't have FlexWidth set, returns 1.0.
func (b *Buffer) getPreviousCellWidth() float64 {
	// Find the previous cell
	prevX := b.cursorX - 1
	prevY := b.cursorY

	// If we're at the start of a line, try the end of the previous line
	if prevX < 0 {
		if prevY > 0 {
			prevY--
			if prevY < len(b.screen) && len(b.screen[prevY]) > 0 {
				prevX = len(b.screen[prevY]) - 1
			} else {
				return 1.0 // No previous cell, default to 1.0
			}
		} else {
			return 1.0 // At start of buffer, default to 1.0
		}
	}

	// Get the previous cell
	if prevY < len(b.screen) && prevX < len(b.screen[prevY]) {
		prevCell := b.screen[prevY][prevX]
		if prevCell.FlexWidth && prevCell.CellWidth > 0 {
			return prevCell.CellWidth
		}
	}

	return 1.0 // Default to 1.0 if no valid previous cell
}

// getLineVisualWidth calculates the accumulated visual width of a line up to (but not including) col.
// Returns the sum of CellWidth values for cells 0 to col-1.
func (b *Buffer) getLineVisualWidth(row, col int) float64 {
	if row < 0 || row >= len(b.screen) {
		return 0
	}
	line := b.screen[row]
	width := 0.0
	for i := 0; i < col && i < len(line); i++ {
		if line[i].CellWidth > 0 {
			width += line[i].CellWidth
		} else {
			width += 1.0 // Default for cells without width set
		}
	}
	return width
}

// GetLineVisualWidth returns the visual width of a line up to (but not including) col.
// This is the public thread-safe version.
func (b *Buffer) GetLineVisualWidth(row, col int) float64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.getLineVisualWidth(row, col)
}

// GetTotalLineVisualWidth returns the total visual width of a line.
func (b *Buffer) GetTotalLineVisualWidth(row int) float64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if row < 0 || row >= len(b.screen) {
		return 0
	}
	return b.getLineVisualWidth(row, len(b.screen[row]))
}

func (b *Buffer) writeCharInternal(ch rune) {
	// Handle combining characters (Hebrew vowel points, diacritics, etc.)
	// These should be appended to the previous cell, not placed in a new cell
	if IsCombiningMark(ch) {
		b.appendCombiningMark(ch)
		return
	}

	effectiveCols := b.EffectiveCols()
	effectiveRows := b.EffectiveRows()

	// Check if this character has a custom glyph defined
	hasCustomGlyph := b.customGlyphs[ch] != nil

	// Calculate the width this character will take
	var charWidth float64
	if b.currentFlexWidth {
		if hasCustomGlyph {
			// Custom glyphs: check ambiguous width mode first for explicit overrides
			switch b.ambiguousWidthMode {
			case AmbiguousWidthNarrow:
				charWidth = 1.0
			case AmbiguousWidthWide:
				charWidth = 2.0
			default: // AmbiguousWidthAuto
				// Auto mode: use the underlying character's width category
				charWidth = GetEastAsianWidth(ch)
				// If the underlying character is ambiguous, match previous cell
				if charWidth < 0 {
					charWidth = b.getPreviousCellWidth()
				}
			}
		} else {
			charWidth = GetEastAsianWidth(ch)
			// Handle ambiguous width characters (-1.0 means ambiguous)
			if charWidth < 0 {
				switch b.ambiguousWidthMode {
				case AmbiguousWidthNarrow:
					charWidth = 1.0
				case AmbiguousWidthWide:
					charWidth = 2.0
				default: // AmbiguousWidthAuto
					// Match width of previous character
					charWidth = b.getPreviousCellWidth()
				}
			}
		}
	} else {
		charWidth = 1.0
	}

	// Handle line wrap (DECAWM mode 7)
	// If visual width wrap is enabled, wrap based on accumulated visual width
	// Otherwise, wrap based on cell count (traditional behavior)
	shouldWrap := false
	if b.visualWidthWrap && b.currentFlexWidth {
		// Visual width wrap: wrap when adding this char would exceed column limit
		currentVisualWidth := b.getLineVisualWidth(b.cursorY, b.cursorX)
		shouldWrap = (currentVisualWidth + charWidth) > float64(effectiveCols)
	} else {
		// Traditional cell-count wrap
		shouldWrap = b.cursorX >= effectiveCols
	}

	if shouldWrap {
		if b.autoWrapMode {
			// Check for smart word wrap
			if b.smartWordWrap && b.cursorY < len(b.screen) {
				line := b.screen[b.cursorY]

				// Count leading spaces for indentation preservation
				leadingSpaces := 0
				for _, cell := range line {
					if cell.Char == ' ' {
						leadingSpaces++
					} else {
						break
					}
				}

				// Look backwards for a word boundary character AFTER the leading indent
				// Word boundaries: space, hyphen, comma, semicolon, emdash (U+2014)
				wrapPoint := -1
				for i := len(line) - 1; i > leadingSpaces; i-- {
					ch := line[i].Char
					if ch == ' ' || ch == '-' || ch == ',' || ch == ';' || ch == '—' {
						wrapPoint = i
						break
					}
				}

				// Move to next line
				b.setHorizMoveDir(-1, false)
				b.trackCursorYMove(b.cursorY + 1)
				b.cursorY++
				if b.cursorY >= effectiveRows {
					b.scrollUpInternal()
					b.cursorY = effectiveRows - 1
				}

				// Ensure screen has enough rows
				for b.cursorY >= len(b.screen) {
					b.screen = append(b.screen, b.makeEmptyLine())
					b.lineInfos = append(b.lineInfos, b.makeDefaultLineInfo())
				}

				// Create indent cells (spaces with default attributes)
				indentCells := make([]Cell, leadingSpaces)
				for i := range indentCells {
					indentCells[i] = Cell{
						Char:       ' ',
						Foreground: DefaultForeground,
						Background: DefaultBackground,
					}
				}

				if wrapPoint > leadingSpaces && wrapPoint < len(line)-1 {
					// Found a valid word boundary - move cells after it to new line
					cellsToMove := make([]Cell, len(line)-wrapPoint-1)
					copy(cellsToMove, line[wrapPoint+1:])

					// Trim the current line (keep the boundary char)
					b.screen[b.cursorY-1] = line[:wrapPoint+1]

					// Place indent + moved cells at the start of the new line
					newLine := append(indentCells, cellsToMove...)
					b.screen[b.cursorY] = append(newLine, b.screen[b.cursorY]...)

					// Position cursor after the indent and moved cells
					b.cursorX = leadingSpaces + len(cellsToMove)
				} else {
					// No valid word boundary (single word or no break after indent)
					// Just wrap with indent, no cells moved
					if leadingSpaces > 0 {
						b.screen[b.cursorY] = append(indentCells, b.screen[b.cursorY]...)
					}
					b.cursorX = leadingSpaces
				}
			} else {
				// Standard auto-wrap: move to next line
				b.setHorizMoveDir(-1, false)
				b.cursorX = 0
				b.trackCursorYMove(b.cursorY + 1)
				b.cursorY++
				if b.cursorY >= effectiveRows {
					b.scrollUpInternal()
					b.cursorY = effectiveRows - 1
				}
			}
		} else {
			// Auto-wrap disabled (DECAWM off): stay at last column, overwrite character
			b.cursorX = effectiveCols - 1
		}
	}

	// Ensure screen has enough rows
	for b.cursorY >= len(b.screen) {
		b.screen = append(b.screen, b.makeEmptyLine())
		b.lineInfos = append(b.lineInfos, b.makeDefaultLineInfo())
	}

	// Ensure line is long enough for the cursor position
	b.ensureLineLength(b.cursorY, b.cursorX+1)

	fg := b.currentFg
	bg := b.currentBg
	if b.currentReverse {
		fg, bg = bg, fg
	}

	cell := Cell{
		Char:              ch,
		Foreground:        fg,
		Background:        bg,
		Bold:              b.currentBold,
		Italic:            b.currentItalic,
		Underline:         b.currentUnderline,
		UnderlineStyle:    b.currentUnderlineStyle,
		UnderlineColor:    b.currentUnderlineColor,
		HasUnderlineColor: b.currentHasUnderlineColor,
		Reverse:           b.currentReverse,
		Blink:             b.currentBlink,
		Strikethrough:     b.currentStrikethrough,
		FlexWidth:         b.currentFlexWidth,
		BGP:               b.currentBGP,
		XFlip:             b.currentXFlip,
		YFlip:             b.currentYFlip,
	}

	// Use the calculated charWidth (already accounts for custom glyphs and ambiguous width mode)
	cell.CellWidth = charWidth

	b.screen[b.cursorY][b.cursorX] = cell
	// Only set direction to right if we didn't wrap (wrap already set it to left)
	if !shouldWrap {
		b.setHorizMoveDir(1, false) // Character output moves cursor right
	}
	b.cursorX++
	b.markDirty()
}

// appendCombiningMark appends a combining character to the previous cell.
// If there's no previous cell to attach to, the character is ignored.
func (b *Buffer) appendCombiningMark(ch rune) {
	// Find the previous cell to attach the combining mark to
	prevX := b.cursorX - 1
	prevY := b.cursorY

	// If we're at the start of a line, try the end of the previous line
	if prevX < 0 {
		if prevY > 0 {
			prevY--
			if prevY < len(b.screen) && len(b.screen[prevY]) > 0 {
				prevX = len(b.screen[prevY]) - 1
			} else {
				// No previous cell to attach to
				return
			}
		} else {
			// No previous cell to attach to (very start of buffer)
			return
		}
	}

	// Ensure the previous row exists and has the cell
	if prevY >= len(b.screen) || prevX >= len(b.screen[prevY]) {
		return
	}

	// Append the combining mark to the previous cell
	b.screen[prevY][prevX].Combining += string(ch)
	b.markDirty()
}

// ensureLineLength ensures a line has at least the specified length,
// filling gaps with the line's default cell
func (b *Buffer) ensureLineLength(row, length int) {
	if row >= len(b.screen) {
		return
	}
	line := b.screen[row]
	if len(line) >= length {
		return
	}
	// Get fill cell from line info or use empty cell
	var fillCell Cell
	if row < len(b.lineInfos) {
		fillCell = b.lineInfos[row].DefaultCell
		fillCell.Char = ' '
	} else {
		fillCell = EmptyCell()
	}
	// Extend line
	for len(line) < length {
		line = append(line, fillCell)
	}
	b.screen[row] = line
}

// --- Line Navigation ---

// Newline moves cursor to the beginning of the next line
func (b *Buffer) Newline() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cursorX = 0
	b.trackCursorYMove(b.cursorY + 1)
	b.cursorY++
	effectiveRows := b.EffectiveRows()
	if b.cursorY >= effectiveRows {
		b.scrollUpInternal()
		b.cursorY = effectiveRows - 1
	}
	b.markDirty()
}

// CarriageReturn moves cursor to the beginning of the current line
func (b *Buffer) CarriageReturn() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.setHorizMoveDir(-1, false) // Moving left
	b.cursorX = 0
	b.markDirty()
}

// LineFeed moves cursor down one line
func (b *Buffer) LineFeed() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.trackCursorYMove(b.cursorY + 1)
	b.cursorY++
	effectiveRows := b.EffectiveRows()
	if b.cursorY >= effectiveRows {
		b.scrollUpInternal()
		b.cursorY = effectiveRows - 1
	}
	b.markDirty()
}

// Tab moves cursor to the next tab stop
func (b *Buffer) Tab() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.setHorizMoveDir(1, false) // Moving right
	b.cursorX = ((b.cursorX / 8) + 1) * 8
	effectiveCols := b.EffectiveCols()
	if b.cursorX >= effectiveCols {
		b.cursorX = effectiveCols - 1
	}
	b.markDirty()
}

// Backspace moves cursor left one position
func (b *Buffer) Backspace() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.setHorizMoveDir(-1, false) // Moving left
	if b.cursorX > 0 {
		b.cursorX--
	}
	b.markDirty()
}

// --- Screen Scrolling ---

func (b *Buffer) scrollUpInternal() {
	if len(b.screen) == 0 {
		return
	}

	// Push top line to scrollback - this is a scroll-causing event
	b.pushLineToScrollback(b.screen[0], b.lineInfos[0])
	b.lastScrollCausingEvent = time.Now()

	// Shift screen up
	copy(b.screen, b.screen[1:])
	copy(b.lineInfos, b.lineInfos[1:])

	// Add new empty line at bottom with current attributes
	lastIdx := len(b.screen) - 1
	b.screen[lastIdx] = b.makeEmptyLine()
	b.lineInfos[lastIdx] = b.makeDefaultLineInfo()

	// Content scrolled up = new content at bottom = cursor moving toward newer content
	// Set direction directly since most cursor movements bypass setCursorInternal
	b.lastCursorMoveDir = 1 // Down

	b.markDirty()
}

// ScrollUp scrolls up by n lines
func (b *Buffer) ScrollUp(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := 0; i < n; i++ {
		b.scrollUpInternal()
	}
}

// ScrollDown scrolls down by n lines
func (b *Buffer) ScrollDown(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	screenLen := len(b.screen)
	for i := 0; i < n && screenLen > 0; i++ {
		copy(b.screen[1:], b.screen[:screenLen-1])
		copy(b.lineInfos[1:], b.lineInfos[:screenLen-1])
		b.screen[0] = b.makeEmptyLine()
		b.lineInfos[0] = b.makeDefaultLineInfo()
	}
	b.markDirty()
}

// --- Screen Clearing ---

// ClearScreen clears the entire screen and resets view to show top
func (b *Buffer) ClearScreen() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.updateScreenInfo() // Update screen default attributes
	b.initScreen()

	// Reset cursor to top-left
	b.trackCursorYMove(0)
	b.cursorX = 0
	b.cursorY = 0

	// Reset scroll to show logical row 0 at the top of the visible area
	// When logicalRows > rows, we need scrollOffset = logicalHiddenAbove to see row 0
	effectiveRows := b.EffectiveRows()
	if effectiveRows > b.rows {
		b.scrollOffset = effectiveRows - b.rows
	} else {
		b.scrollOffset = 0
	}

	b.markDirty()
}

// ClearToEndOfLine clears from cursor to end of line
// This updates the line's default cell and truncates the line at cursor position
func (b *Buffer) ClearToEndOfLine() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.cursorY >= len(b.screen) {
		return
	}

	// Update line info with current attributes (for rendering beyond stored content)
	if b.cursorY < len(b.lineInfos) {
		b.lineInfos[b.cursorY].DefaultCell = b.currentDefaultCell()
	}

	// Truncate line at cursor position (variable width lines)
	if b.cursorX < len(b.screen[b.cursorY]) {
		b.screen[b.cursorY] = b.screen[b.cursorY][:b.cursorX]
	}

	b.markDirty()
}

// ClearToStartOfLine clears from start of line to cursor
// Note: Does NOT update LineInfo (LineInfo is for right side of line)
// Note: Does NOT extend the line - only clears existing cells
func (b *Buffer) ClearToStartOfLine() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.cursorY >= len(b.screen) {
		return
	}

	line := b.screen[b.cursorY]
	lineLen := len(line)

	// Only clear cells that actually exist in the line
	// No need to extend the line - cells beyond the line are conceptually blank
	clearCell := b.currentDefaultCell()
	endX := b.cursorX
	if endX >= lineLen {
		endX = lineLen - 1
	}
	for x := 0; x <= endX; x++ {
		line[x] = clearCell
	}
	b.markDirty()
}

// ClearLine clears the entire current line
// This updates the line's default cell
func (b *Buffer) ClearLine() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.cursorY >= len(b.screen) {
		return
	}

	// Update line info with current attributes
	if b.cursorY < len(b.lineInfos) {
		b.lineInfos[b.cursorY].DefaultCell = b.currentDefaultCell()
	}

	// Clear the line (make it empty - variable width)
	b.screen[b.cursorY] = b.makeEmptyLine()
	b.markDirty()
}

// ClearToEndOfScreen clears from cursor to end of screen
// This updates the ScreenInfo default cell
func (b *Buffer) ClearToEndOfScreen() {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Update screen info with current attributes
	b.updateScreenInfo()

	// Clear current line from cursor to end
	if b.cursorY < len(b.screen) {
		if b.cursorY < len(b.lineInfos) {
			b.lineInfos[b.cursorY].DefaultCell = b.currentDefaultCell()
		}
		if b.cursorX < len(b.screen[b.cursorY]) {
			b.screen[b.cursorY] = b.screen[b.cursorY][:b.cursorX]
		}
	}

	// Clear all lines below cursor
	for y := b.cursorY + 1; y < len(b.screen); y++ {
		b.screen[y] = b.makeEmptyLine()
		if y < len(b.lineInfos) {
			b.lineInfos[y] = b.makeDefaultLineInfo()
		}
	}
	b.markDirty()
}

// ClearToStartOfScreen clears from start of screen to cursor
// Note: Does NOT update ScreenInfo (ScreenInfo is for lines below stored content)
// Note: Does NOT extend lines - only clears existing cells
func (b *Buffer) ClearToStartOfScreen() {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Clear all lines above cursor
	for y := 0; y < b.cursorY && y < len(b.screen); y++ {
		b.screen[y] = b.makeEmptyLine()
		if y < len(b.lineInfos) {
			b.lineInfos[y] = b.makeDefaultLineInfo()
		}
	}

	// Clear current line from start to cursor (only existing cells)
	if b.cursorY < len(b.screen) {
		line := b.screen[b.cursorY]
		lineLen := len(line)
		clearCell := b.currentDefaultCell()
		endX := b.cursorX
		if endX >= lineLen {
			endX = lineLen - 1
		}
		for x := 0; x <= endX; x++ {
			line[x] = clearCell
		}
	}
	b.markDirty()
}

// --- Line Insert/Delete ---

// InsertLines inserts n blank lines at cursor
func (b *Buffer) InsertLines(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	screenLen := len(b.screen)
	for i := 0; i < n && screenLen > 0; i++ {
		if b.cursorY < screenLen-1 {
			copy(b.screen[b.cursorY+1:], b.screen[b.cursorY:screenLen-1])
			copy(b.lineInfos[b.cursorY+1:], b.lineInfos[b.cursorY:screenLen-1])
		}
		b.screen[b.cursorY] = b.makeEmptyLine()
		b.lineInfos[b.cursorY] = b.makeDefaultLineInfo()
	}
	b.markDirty()
}

// DeleteLines deletes n lines at cursor
func (b *Buffer) DeleteLines(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	screenLen := len(b.screen)
	for i := 0; i < n && screenLen > 0; i++ {
		if b.cursorY < screenLen-1 {
			copy(b.screen[b.cursorY:], b.screen[b.cursorY+1:])
			copy(b.lineInfos[b.cursorY:], b.lineInfos[b.cursorY+1:])
		}
		b.screen[screenLen-1] = b.makeEmptyLine()
		b.lineInfos[screenLen-1] = b.makeDefaultLineInfo()
	}
	b.markDirty()
}

// --- Character Insert/Delete ---

// DeleteChars deletes n characters at cursor
func (b *Buffer) DeleteChars(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.cursorY >= len(b.screen) {
		return
	}
	line := b.screen[b.cursorY]
	lineLen := len(line)

	if b.cursorX >= lineLen {
		return // Nothing to delete
	}

	// Shift characters left
	if b.cursorX+n < lineLen {
		copy(line[b.cursorX:], line[b.cursorX+n:])
		b.screen[b.cursorY] = line[:lineLen-n]
	} else {
		// Delete extends past end of line - just truncate
		b.screen[b.cursorY] = line[:b.cursorX]
	}
	b.markDirty()
}

// InsertChars inserts n blank characters at cursor
func (b *Buffer) InsertChars(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.cursorY >= len(b.screen) {
		return
	}

	// Ensure line is long enough
	b.ensureLineLength(b.cursorY, b.cursorX)

	line := b.screen[b.cursorY]
	lineLen := len(line)

	// Create space for new characters
	newCells := make([]Cell, n)
	fillCell := b.currentDefaultCell()
	for i := range newCells {
		newCells[i] = fillCell
	}

	// Insert at cursor position
	if b.cursorX >= lineLen {
		line = append(line, newCells...)
	} else {
		// Make room and insert
		line = append(line[:b.cursorX], append(newCells, line[b.cursorX:]...)...)
	}
	b.screen[b.cursorY] = line
	b.markDirty()
}

// EraseChars erases n characters at cursor (replaces with blanks)
// Does not extend line beyond current length - only erases existing cells
func (b *Buffer) EraseChars(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.cursorY >= len(b.screen) {
		return
	}

	line := b.screen[b.cursorY]
	lineLen := len(line)

	// Only erase existing cells, don't extend line
	if b.cursorX >= lineLen {
		return // Nothing to erase
	}

	endPos := b.cursorX + n
	if endPos > lineLen {
		endPos = lineLen
	}

	fillCell := b.currentDefaultCell()
	for i := b.cursorX; i < endPos; i++ {
		line[i] = fillCell
	}
	b.markDirty()
}
