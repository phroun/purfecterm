package purfecterm

// Selective erase (DECSCA / DECSED / DECSEL) and the DECRQSS SGR report.

// SetProtectedAttr sets DECSCA: whether subsequently written cells are protected
// from selective erase.
func (b *Buffer) SetProtectedAttr(on bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.currentProtected = on
}

// SGRReport returns the current pen's SGR parameters without the ESC[ / m
// wrapper (for DECRQSS "m"); "0" when the pen is default.
func (b *Buffer) SGRReport() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	s := b.currentPenSGR()
	if s == "" {
		return "0"
	}
	return s[2 : len(s)-1] // strip "\x1b[" and trailing "m"
}

// eraseRowSelective blanks the unprotected cells of row y in [start,end).
// Caller holds the lock.
func (b *Buffer) eraseRowSelective(y, start, end int, blank Cell) {
	if y < 0 || y >= len(b.screen) {
		return
	}
	line := b.screen[y]
	if end > len(line) {
		end = len(line)
	}
	for x := start; x < end; x++ {
		if x >= 0 && !line[x].Protected {
			line[x] = blank
		}
	}
}

// SelectiveEraseLine implements DECSEL (CSI ? Ps K): erase unprotected cells in
// the current line. Ps 0 = cursor to end, 1 = start to cursor, 2 = whole line.
func (b *Buffer) SelectiveEraseLine(mode int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cursorY < 0 || b.cursorY >= len(b.screen) {
		return
	}
	blank := b.currentDefaultCell()
	blank.Protected = false
	line := b.screen[b.cursorY]
	switch mode {
	case 1:
		b.eraseRowSelective(b.cursorY, 0, b.cursorX+1, blank)
	case 2:
		b.eraseRowSelective(b.cursorY, 0, len(line), blank)
	default: // 0
		b.eraseRowSelective(b.cursorY, b.cursorX, len(line), blank)
	}
	b.markDirty()
}

// SelectiveEraseDisplay implements DECSED (CSI ? Ps J): erase unprotected cells.
// Ps 0 = cursor to end, 1 = start to cursor, 2 = whole display.
func (b *Buffer) SelectiveEraseDisplay(mode int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	blank := b.currentDefaultCell()
	blank.Protected = false
	rows := len(b.screen)
	switch mode {
	case 1: // start of screen through cursor
		for y := 0; y < b.cursorY; y++ {
			b.eraseRowSelective(y, 0, len(b.screen[y]), blank)
		}
		if b.cursorY < rows {
			b.eraseRowSelective(b.cursorY, 0, b.cursorX+1, blank)
		}
	case 2: // whole display
		for y := 0; y < rows; y++ {
			b.eraseRowSelective(y, 0, len(b.screen[y]), blank)
		}
	default: // 0: cursor through end of screen
		if b.cursorY < rows {
			b.eraseRowSelective(b.cursorY, b.cursorX, len(b.screen[b.cursorY]), blank)
		}
		for y := b.cursorY + 1; y < rows; y++ {
			b.eraseRowSelective(y, 0, len(b.screen[y]), blank)
		}
	}
	b.markDirty()
}
