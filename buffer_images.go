package purfecterm

import "sort"

// Cell-anchored bitmap images (from Sixel). An image is anchored at a screen
// cell (Row, Col) and occupies CellsWide x CellsHigh cells; it scrolls with the
// text and is dropped when it scrolls off the top or the screen is cleared. A
// renderer blits Image.RGBA at the cell's pixel position, scaled to DestSize.

// PlacedImage is a bitmap anchored to a screen cell.
type PlacedImage struct {
	ID        int
	Row, Col  int // anchor cell (screen-relative, 0-based)
	CellsWide int
	CellsHigh int
	Image     *SixelImage

	// ImageID and PlacementID identify a kitty graphics placement, which unlike
	// Sixel or an iTerm2 inline image is addressable after the fact: the same
	// stored image can carry several placements, and either can be deleted by
	// ID. Both are 0 for a protocol with no such notion.
	ImageID     uint32
	PlacementID uint32

	// ZIndex orders drawing. Negative means BEHIND the text, which is what
	// makes text-over-image work; images at the same z draw in placement order.
	ZIndex int

	// Virtual marks a placement that is not drawn at Row/Col but positioned by
	// Unicode placeholder cells in the text. It is held so the placeholder
	// cells can find it, and skipped by an ordinary draw pass.
	Virtual bool

	// SrcX/SrcY/SrcW/SrcH crop the source image before drawing; zero width or
	// height means the whole image. This is the protocol's x/y/w/h.
	SrcX, SrcY, SrcW, SrcH int

	// DestW/DestH are the size in terminal pixels the image is to be DRAWN at,
	// which is not always the size it was decoded at. Sixel carries its own
	// pixels and asks for none, so it sets these to the decoded size and draws
	// 1:1. A protocol that sizes an image in cells or in a percentage of the
	// session — iTerm2's inline images does both — resolves that to pixels here,
	// and the renderer scales to it. CellsWide/CellsHigh are derived from this,
	// not from the decoded size. Zero means "the decoded size"; read them
	// through DestSize rather than directly.
	DestW, DestH int
}

// DestSize returns the size in terminal pixels this image should be drawn at,
// falling back to the decoded image's own size when unset. Renderers scale the
// decoded pixels to this; when it equals the decoded size the blit is 1:1.
func (p *PlacedImage) DestSize() (w, h int) {
	if p == nil || p.Image == nil {
		return 0, 0
	}
	w, h = p.DestW, p.DestH
	if w <= 0 {
		w = p.Image.W
	}
	if h <= 0 {
		h = p.Image.H
	}
	return w, h
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

// PlaceSixelImage anchors a decoded Sixel image at the cursor. Sixel carries
// its own pixels and asks for no resizing, so it is drawn at the size it
// decoded to; see PlaceImage for the rest of the behavior.
func (b *Buffer) PlaceSixelImage(img *SixelImage) {
	if img == nil {
		return
	}
	b.PlaceImage(img, img.W, img.H)
}

// PlaceImage anchors a decoded bitmap at the cursor, to be drawn at destW x
// destH terminal pixels, leaves the cursor on the image's last row per sixel
// scrolling, and scrolls the screen if the image runs past the bottom. A
// non-positive destination falls back to the bitmap's own size.
//
// This is the entry point for any image source — Sixel, the iTerm2 inline
// images protocol, or a caller placing pixels directly.
func (b *Buffer) PlaceImage(img *Bitmap, destW, destH int) {
	if img == nil || img.W == 0 || img.H == 0 {
		return
	}
	if destW <= 0 {
		destW = img.W
	}
	if destH <= 0 {
		destH = img.H
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
	cellsW := (destW + cw - 1) / cw
	cellsH := (destH + ch - 1) / ch

	b.nextImageID++
	pi := &PlacedImage{
		ID:        b.nextImageID,
		Row:       b.cursorY,
		Col:       b.cursorX,
		CellsWide: cellsW,
		CellsHigh: cellsH,
		Image:     img,
		DestW:     destW,
		DestH:     destH,
	}
	b.images = append(b.images, pi)

	if b.sixelScrolling {
		// "When sixel mode is exited, the text cursor is set to the current
		// sixel cursor position" — and the sixel cursor is a pixel position
		// inside the LAST band written, so the text cursor lands on the image's
		// own last row, not below it. Advancing one further is the classic
		// off-by-one: every program that ends its image with a newline (chafa,
		// img2sixel) then leaves a blank line under it.
		effRows := b.EffectiveRows()
		last := b.cursorY + cellsH - 1 // the image's own last row
		if scrollN := last - (effRows - 1); scrollN > 0 {
			for k := 0; k < scrollN; k++ {
				b.scrollUpInternal() // also shifts image anchors up
			}
			b.cursorY = effRows - 1
		} else {
			b.cursorY = last
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

// SourceRect returns the crop applied to the source image before drawing, as a
// rectangle guaranteed to lie inside it. An unset crop is the whole image.
func (p *PlacedImage) SourceRect() (x, y, w, h int) {
	if p == nil || p.Image == nil {
		return 0, 0, 0, 0
	}
	x, y, w, h = p.SrcX, p.SrcY, p.SrcW, p.SrcH
	if w <= 0 {
		w = p.Image.W - x
	}
	if h <= 0 {
		h = p.Image.H - y
	}
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if x > p.Image.W {
		x = p.Image.W
	}
	if y > p.Image.H {
		y = p.Image.H
	}
	if x+w > p.Image.W {
		w = p.Image.W - x
	}
	if y+h > p.Image.H {
		h = p.Image.H - y
	}
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	return x, y, w, h
}

// GetImagesByZ returns the placed images a renderer should draw, split by
// whether they belong under or over the text. Each group is ordered back to
// front. Virtual placements are excluded: they are drawn from the placeholder
// cells that reference them, not from the placement list.
//
// A renderer that does not implement z-ordering can keep calling GetImages and
// draw everything over the text, which is where images went before z existed.
func (b *Buffer) GetImagesByZ() (below, above []*PlacedImage) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, im := range b.images {
		if im.Virtual {
			continue
		}
		if im.ZIndex < 0 {
			below = append(below, im)
		} else {
			above = append(above, im)
		}
	}
	sortByZ(below)
	sortByZ(above)
	return below, above
}

// sortByZ orders a group back to front, keeping placement order within one
// z-index so that equal-z images stack in the order they arrived.
func sortByZ(list []*PlacedImage) {
	sort.SliceStable(list, func(i, j int) bool { return list[i].ZIndex < list[j].ZIndex })
}

// PlaceKittyImage adds a fully specified placement and returns it. Unlike
// PlaceImage it does not touch the cursor: the kitty protocol decides cursor
// movement from its own C key, and a virtual placement never moves it at all.
func (b *Buffer) PlaceKittyImage(pi *PlacedImage) *PlacedImage {
	if pi == nil || pi.Image == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextImageID++
	pi.ID = b.nextImageID
	// A placement ID is unique per image, so re-placing the same (image,
	// placement) pair replaces rather than stacks.
	if pi.ImageID != 0 {
		kept := b.images[:0]
		for _, ex := range b.images {
			if ex.ImageID == pi.ImageID && ex.PlacementID == pi.PlacementID {
				continue
			}
			kept = append(kept, ex)
		}
		b.images = kept
	}
	b.images = append(b.images, pi)
	b.markDirty()
	return pi
}

// DeleteImagesFunc drops every placement the predicate accepts and reports how
// many went. Deleting a placement never deletes the stored image; the caller
// decides that from the protocol's uppercase/lowercase distinction.
func (b *Buffer) DeleteImagesFunc(match func(*PlacedImage) bool) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	kept := b.images[:0]
	n := 0
	for _, im := range b.images {
		if match(im) {
			n++
			continue
		}
		kept = append(kept, im)
	}
	b.images = kept
	if n > 0 {
		b.markDirty()
	}
	return n
}

// HasPlacementFor reports whether any placement references a stored image.
func (b *Buffer) HasPlacementFor(imageID uint32) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, im := range b.images {
		if im.ImageID == imageID {
			return true
		}
	}
	return false
}
