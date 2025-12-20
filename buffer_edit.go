package purfecterm

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
