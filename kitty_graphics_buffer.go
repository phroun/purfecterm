package purfecterm

// Buffer-side glue for the kitty graphics protocol: the image store lives on
// the buffer (so it is cleared and reset with the screen) but is reached only
// through these accessors, which take the buffer's lock.

// kittyStore returns the buffer's image store, creating it on first use.
// Callers that touch it must hold the lock; withKittyStore does that for them.
func (b *Buffer) kittyStore() *kittyImageStore {
	if b.kittyImages == nil {
		b.kittyImages = newKittyImageStore()
	}
	return b.kittyImages
}

// withKittyStore runs fn under the buffer lock, for a read of the store.
func (b *Buffer) withKittyStore(fn func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.kittyStore()
	fn()
}

// putKittyImage stores a transmitted image, evicting old ones past the cap.
func (b *Buffer) putKittyImage(img *kittyImage) {
	b.mu.Lock()
	store := b.kittyStore()
	store.put(img)
	placed := map[uint32]bool{}
	for _, im := range b.images {
		placed[im.ImageID] = true
	}
	b.mu.Unlock()
	b.mu.Lock()
	store.evictTo(MaxKittyImages, func(id uint32) bool { return placed[id] })
	b.mu.Unlock()
}

// removeKittyImage frees one stored image.
func (b *Buffer) removeKittyImage(id uint32) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.kittyStore().remove(id)
}

// removeKittyImageRange frees every stored image with an ID in [lo, hi].
func (b *Buffer) removeKittyImageRange(lo, hi uint32) {
	b.mu.Lock()
	defer b.mu.Unlock()
	store := b.kittyStore()
	for id := range store.byID {
		if id >= lo && id <= hi {
			store.remove(id)
		}
	}
}

// CursorRowCol returns the cursor as (row, col), the order image placement
// works in. GetCursor returns (x, y); this exists so a caller reasoning in
// rows and columns cannot silently transpose them.
func (b *Buffer) CursorRowCol() (row, col int) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.cursorY, b.cursorX
}

// advanceCursorForImage moves the cursor past a placement: right by its width
// in cells and down by its height, scrolling if that runs off the bottom. This
// is the kitty protocol's rule, and unlike Sixel it lands BELOW the image.
func (b *Buffer) advanceCursorForImage(cellsWide, cellsHigh int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	effCols := b.EffectiveCols()
	effRows := b.EffectiveRows()

	x := b.cursorX + cellsWide
	if x >= effCols {
		x = effCols - 1
	}
	if x < 0 {
		x = 0
	}
	b.cursorX = x

	y := b.cursorY + cellsHigh
	if scrollN := y - (effRows - 1); scrollN > 0 {
		for k := 0; k < scrollN; k++ {
			b.scrollUpInternal()
		}
		y = effRows - 1
	}
	if y < 0 {
		y = 0
	}
	b.cursorY = y
	b.markDirty()
}
