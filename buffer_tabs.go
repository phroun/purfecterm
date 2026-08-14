package purfecterm

// Horizontal tab stops (HTS / TBC / CHT / CBT) and the standard ANSI modes
// IRM (insert) and LNM (new-line), plus REP support.
//
// Tab stops are a set of VISUAL columns. A nil set means the default
// every-8-columns stops; the first HTS/TBC materializes an explicit set.

const defaultTabWidth = 8

// materializeTabStops fills the explicit stop set from the every-8 default the
// first time a stop is set or cleared. Caller holds the lock.
func (b *Buffer) materializeTabStops() {
	if b.tabStops != nil {
		return
	}
	b.tabStops = map[int]bool{}
	cols := b.EffectiveCols()
	for c := defaultTabWidth; c < cols; c += defaultTabWidth {
		b.tabStops[c] = true
	}
}

// nextTabStopVisual returns the next tab stop strictly right of visual column v,
// bounded by the last column. Caller holds the lock.
func (b *Buffer) nextTabStopVisual(v int) int {
	cols := b.EffectiveCols()
	if b.tabStops == nil {
		n := ((v / defaultTabWidth) + 1) * defaultTabWidth
		if n > cols-1 {
			n = cols - 1
		}
		return n
	}
	for c := v + 1; c < cols; c++ {
		if b.tabStops[c] {
			return c
		}
	}
	return cols - 1
}

// prevTabStopVisual returns the previous tab stop strictly left of v, bounded by
// 0. Caller holds the lock.
func (b *Buffer) prevTabStopVisual(v int) int {
	if b.tabStops == nil {
		if v <= 0 {
			return 0
		}
		return ((v - 1) / defaultTabWidth) * defaultTabWidth
	}
	for c := v - 1; c > 0; c-- {
		if b.tabStops[c] {
			return c
		}
	}
	return 0
}

// SetTabStop sets a tab stop at the cursor's column (HTS, ESC H).
func (b *Buffer) SetTabStop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.materializeTabStops()
	b.tabStops[b.logicalToVisualLocked(b.cursorY, b.cursorX)] = true
}

// ClearTabStop clears tab stops (TBC): mode 3 clears all, else clears the one at
// the cursor.
func (b *Buffer) ClearTabStop(mode int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if mode == 3 {
		b.tabStops = map[int]bool{}
		return
	}
	b.materializeTabStops()
	delete(b.tabStops, b.logicalToVisualLocked(b.cursorY, b.cursorX))
}

// resetTabStops restores the default every-8-columns stops (RIS).
func (b *Buffer) resetTabStops() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tabStops = nil
}

// TabForward advances the cursor forward n tab stops (HT and CHT), stopping at
// the right margin when left/right margins are active.
func (b *Buffer) TabForward(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n < 1 {
		n = 1
	}
	if b.liveEnabled() {
		b.flushLiveWriteRun()
	}
	b.setHorizMoveDir(1, false)
	_, right, lrActive := b.lrMarginsLocked()
	v := b.logicalToVisualLocked(b.cursorY, b.cursorX)
	for i := 0; i < n; i++ {
		v = b.nextTabStopVisual(v)
	}
	x := b.visualToLogicalLocked(b.cursorY, v)
	if lrActive && x > right {
		x = right
	}
	if max := b.EffectiveCols() - 1; x > max {
		x = max
	}
	b.cursorX = x
	if b.liveEnabled() {
		b.captureObserver.OnCursorMove(b.cursorX, b.cursorY)
	}
	b.markDirty()
}

// TabBackward moves the cursor back n tab stops (CBT), stopping at the left
// margin when left/right margins are active.
func (b *Buffer) TabBackward(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n < 1 {
		n = 1
	}
	if b.liveEnabled() {
		b.flushLiveWriteRun()
	}
	b.setHorizMoveDir(-1, false)
	left, _, lrActive := b.lrMarginsLocked()
	v := b.logicalToVisualLocked(b.cursorY, b.cursorX)
	for i := 0; i < n; i++ {
		v = b.prevTabStopVisual(v)
	}
	x := b.visualToLogicalLocked(b.cursorY, v)
	if lrActive && x < left {
		x = left
	}
	if x < 0 {
		x = 0
	}
	b.cursorX = x
	if b.liveEnabled() {
		b.captureObserver.OnCursorMove(b.cursorX, b.cursorY)
	}
	b.markDirty()
}

// --- IRM / LNM standard modes and REP ---

// SetInsertMode enables IRM (mode 4): printed characters insert and shift the
// rest of the line right instead of overwriting.
func (b *Buffer) SetInsertMode(on bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.insertMode = on
}

// IsInsertMode reports whether IRM is active.
func (b *Buffer) IsInsertMode() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.insertMode
}

// SetNewLineMode enables LNM (mode 20): an output line feed also performs a
// carriage return.
func (b *Buffer) SetNewLineMode(on bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.newLineMode = on
}

// IsNewLineMode reports whether LNM is active.
func (b *Buffer) IsNewLineMode() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.newLineMode
}

// RepeatLastChar writes the most recently printed graphic character n times
// (REP, CSI b). No-op if nothing has been printed yet.
func (b *Buffer) RepeatLastChar(n int) {
	b.mu.Lock()
	ch := b.lastPrintedChar
	b.mu.Unlock()
	if ch == 0 || n < 1 {
		return
	}
	for i := 0; i < n; i++ {
		b.WriteChar(ch)
	}
}
