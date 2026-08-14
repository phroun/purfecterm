package purfecterm

// Cell-anchored bitmap images (from Sixel). An image is anchored at a screen
// cell (Row, Col) and occupies CellsWide x CellsHigh cells; it scrolls with the
// text and is dropped when it scrolls off the top or the screen is cleared. A
// renderer blits Image.RGBA at the cell's pixel position.

// PlacedImage is a bitmap anchored to a screen cell.
type PlacedImage struct {
	ID        int
	Row, Col  int // anchor cell (screen-relative, 0-based)
	CellsWide int
	CellsHigh int
	Image     *SixelImage
}

// GetImages returns the currently placed images (renderers blit these). The
// returned slice is a copy; the images themselves are shared.
func (b *Buffer) GetImages() []*PlacedImage {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]*PlacedImage, len(b.images))
	copy(out, b.images)
	return out
}

// SetSixelDisplayMode sets DECSDM (?80).
func (b *Buffer) SetSixelDisplayMode(on bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sixelDisplayMode = on
}

// IsSixelDisplayMode reports whether DECSDM is active.
func (b *Buffer) IsSixelDisplayMode() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.sixelDisplayMode
}

// SetSixelScrolling sets sixel scrolling mode (?8452): when on (the default) the
// cursor lands below the image; when off it stays at the image's origin.
func (b *Buffer) SetSixelScrolling(on bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sixelScrolling = on
}

// IsSixelScrolling reports whether sixel scrolling is active.
func (b *Buffer) IsSixelScrolling() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.sixelScrolling
}

// PlaceSixelImage anchors a decoded image at the cursor, advances the cursor per
// sixel scrolling, and scrolls the screen if the image runs past the bottom.
func (b *Buffer) PlaceSixelImage(img *SixelImage) {
	if img == nil || img.W == 0 || img.H == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	cw, ch := b.cellPixelWidth, b.cellPixelHeight
	if cw <= 0 {
		cw = 10
	}
	if ch <= 0 {
		ch = 20
	}
	cellsW := (img.W + cw - 1) / cw
	cellsH := (img.H + ch - 1) / ch

	b.nextImageID++
	pi := &PlacedImage{
		ID:        b.nextImageID,
		Row:       b.cursorY,
		Col:       b.cursorX,
		CellsWide: cellsW,
		CellsHigh: cellsH,
		Image:     img,
	}
	b.images = append(b.images, pi)

	if b.sixelScrolling {
		effRows := b.EffectiveRows()
		below := b.cursorY + cellsH // desired cursor row, just under the image
		if scrollN := below - (effRows - 1); scrollN > 0 {
			for k := 0; k < scrollN; k++ {
				b.scrollUpInternal() // also shifts image anchors up
			}
			b.cursorY = effRows - 1
		} else {
			b.cursorY = below
		}
		b.cursorX = 0
	}
	b.markDirty()
}

// shiftImagesLocked adjusts image anchors when rows [top,bottom] scroll by delta
// (-1 up, +1 down), dropping images that scroll entirely off the top. Caller
// holds the lock.
//
// An image moves when its EXTENT meets the region, not merely its anchor. Once
// a tall image has scrolled partway off the top its anchor sits above the
// region while its lower rows are still on screen and still scrolling; testing
// the anchor alone froze it at row top-1, where it stuck to the top edge
// forever and — never reaching the drop test below — never scrolled off.
func (b *Buffer) shiftImagesLocked(delta, top, bottom int) {
	if len(b.images) == 0 {
		return
	}
	kept := b.images[:0]
	for _, im := range b.images {
		if im.Row <= bottom && im.Row+im.CellsHigh > top {
			im.Row += delta
		}
		if im.Row+im.CellsHigh <= 0 {
			continue // scrolled entirely off the top
		}
		kept = append(kept, im)
	}
	b.images = kept
}

// clearImagesLocked drops all placed images (full screen clear). Caller holds
// the lock.
func (b *Buffer) clearImagesLocked() {
	b.images = nil
}
