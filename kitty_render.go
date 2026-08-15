package purfecterm

// What a renderer draws, as opposed to what a client placed.
//
// Two kitty graphics features mean the placement list is not the draw list:
//
//   - A VIRTUAL placement (U=1) is not drawn at its anchor at all. The client
//     prints U+10EEEE placeholder cells carrying the image ID in their
//     foreground color, and the image is drawn INTO those cells, one tile per
//     cell. That is how a program which lays out text — a multiplexer, a pager —
//     positions an image using nothing but ordinary character cells.
//
//   - A RELATIVE placement (P= / Q=) has no anchor of its own. It hangs off
//     another placement at a cell offset (H=, V=) and moves with it, which is
//     how an overlay stays glued to the image it annotates — including one the
//     terminal only knows the position of because placeholder cells put it
//     there.
//
// Both are resolved here, once, under the buffer lock, so a renderer gets them
// by calling GetImagesByZ and blitting what comes back.

// maxKittyRelativeDepth bounds how long a chain of relative placements may be.
// A client can make two placements each other's parent, so resolution needs a
// stop; eight is far past any real nesting and cheap to walk.
const maxKittyRelativeDepth = 8

// kittyRef identifies a placement the way the protocol's P=/Q= pair does.
type kittyRef struct {
	image     uint32
	placement uint32
}

// GetImagesByZ returns the placed images a renderer should draw, split by
// whether they belong under or over the text. Each group is ordered back to
// front.
//
// This is the resolved draw list, not the raw placement list: virtual
// placements are replaced by the tiles their placeholder cells ask for, and
// relative placements are positioned against their parents. Anything that
// cannot be positioned — a placeholder cell for an image that was deleted, a
// relative placement whose parent is gone — is simply not drawn.
//
// A renderer that does not implement z-ordering can keep calling GetImages and
// draw everything over the text, which is where images went before z existed;
// it gets neither placeholders nor relative placements.
func (b *Buffer) GetImagesByZ() (below, above []*PlacedImage) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	tiles := b.kittyPlaceholderTilesLocked()
	drawn := make([]*PlacedImage, 0, len(b.images)+len(tiles))
	for _, im := range b.images {
		if im.Virtual || im.ParentImageID != 0 {
			continue // drawn through placeholder cells, or against a parent
		}
		drawn = append(drawn, im)
	}
	drawn = append(drawn, tiles...)
	drawn = append(drawn, b.resolveKittyRelativesLocked(drawn)...)

	for _, im := range drawn {
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

// resolveKittyRelativesLocked positions the relative placements against the
// placements that already have a position, returning positioned copies. The
// originals are left alone: a parent can move (the screen scrolls, placeholder
// cells are reprinted somewhere else) and the offset must be reapplied to
// wherever it is NOW, not accumulated onto where it was.
//
// Resolution repeats while it makes progress, so a relative placement whose
// parent is itself relative resolves on a later pass.
func (b *Buffer) resolveKittyRelativesLocked(anchored []*PlacedImage) []*PlacedImage {
	var pending []*PlacedImage
	for _, im := range b.images {
		if im.ParentImageID != 0 && !im.Virtual {
			pending = append(pending, im)
		}
	}
	if len(pending) == 0 {
		return nil
	}

	// Where each placement sits. A placement with no explicit placement ID is
	// also reachable as {image, 0}, which is what a client that never assigned
	// one refers to.
	at := make(map[kittyRef][2]int, len(anchored))
	note := func(im *PlacedImage) {
		if im.ImageID == 0 {
			return
		}
		pos := [2]int{im.Row, im.Col}
		if _, dup := at[kittyRef{im.ImageID, im.PlacementID}]; !dup {
			at[kittyRef{im.ImageID, im.PlacementID}] = pos
		}
		if im.PlacementID != 0 {
			if _, dup := at[kittyRef{im.ImageID, 0}]; !dup {
				at[kittyRef{im.ImageID, 0}] = pos
			}
		}
	}
	for _, im := range anchored {
		note(im)
	}

	var out []*PlacedImage
	for depth := 0; depth < maxKittyRelativeDepth && len(pending) > 0; depth++ {
		rest := pending[:0]
		progress := false
		for _, im := range pending {
			pos, ok := at[kittyRef{im.ParentImageID, im.ParentPlacementID}]
			if !ok {
				rest = append(rest, im)
				continue
			}
			placed := *im
			placed.Row = pos[0] + im.OffsetV
			placed.Col = pos[1] + im.OffsetH
			out = append(out, &placed)
			note(&placed)
			progress = true
		}
		pending = rest
		if !progress {
			break // nothing left that any parent can position
		}
	}
	return out
}

// kittyTileRun is a stretch of adjacent placeholder cells on one screen row
// that all belong to the same image row — the unit a tile is cut for, so a
// 40-cell-wide image costs one blit per row rather than forty.
type kittyTileRun struct {
	image                uint32
	imgRow, imgCol       int // position within the image, in cells
	screenRow, screenCol int // where the run starts, in placement coordinates
	cells                int
}

// kittyPlaceholderTilesLocked scans the visible screen for U+10EEEE cells and
// cuts each run of them out of the virtual placement it names. Caller holds the
// lock.
func (b *Buffer) kittyPlaceholderTilesLocked() []*PlacedImage {
	// Placeholder cells only mean anything while a virtual placement exists, so
	// the scan is skipped outright in every session that never uses them.
	virtual := map[uint32]*PlacedImage{}
	for _, im := range b.images {
		if !im.Virtual || im.ImageID == 0 {
			continue
		}
		if _, dup := virtual[im.ImageID]; !dup {
			virtual[im.ImageID] = im
		}
	}
	if len(virtual) == 0 {
		return nil
	}

	cw, ch := b.cellPixelWidth, b.cellPixelHeight
	if cw <= 0 {
		cw = 10
	}
	if ch <= 0 {
		ch = 20
	}

	// A placement's Row is measured before the scroll offset a renderer adds
	// back, while the scan walks the screen as displayed; the offset comes off
	// here so a tile scrolls exactly like an ordinary placement.
	scroll := b.getEffectiveScrollOffset()

	var out []*PlacedImage
	for y := 0; y < b.rows; y++ {
		var run *kittyTileRun
		var prev KittyPlaceholder
		havePrev := false

		// One column past the end so the last run on the row is flushed by the
		// same code that flushes every other.
		for x := 0; x <= b.cols; x++ {
			var ph KittyPlaceholder
			ok := false
			if x < b.cols {
				cell := b.getVisibleCellInternal(x, y)
				ph, ok = DecodeKittyPlaceholder(cell.Char, []rune(cell.Combining), cell.Foreground)
			}
			if ok {
				// The protocol lets a cell omit what the cell to its left
				// already said: same image, same image row, next image column.
				// That is what keeps a wide image from needing a diacritic pair
				// on every one of its cells.
				if ph.ImageID == 0 && havePrev {
					ph.ImageID = prev.ImageID
				}
				if !ph.HasRow && havePrev {
					ph.Row = prev.Row
				}
				if !ph.HasCol && havePrev {
					ph.Col = prev.Col + 1
				}
			}

			extends := ok && run != nil && run.image == ph.ImageID &&
				run.imgRow == ph.Row && run.imgCol+run.cells == ph.Col
			if extends {
				run.cells++
			} else {
				if run != nil {
					out = appendKittyTile(out, virtual[run.image], *run, cw, ch)
				}
				run = nil
				if ok {
					run = &kittyTileRun{
						image: ph.ImageID, imgRow: ph.Row, imgCol: ph.Col,
						screenRow: y - scroll, screenCol: x + b.horizOffset, cells: 1,
					}
				}
			}
			prev, havePrev = ph, ok
		}
	}
	return out
}

// appendKittyTile cuts one run's slice out of its virtual placement and adds it
// to the draw list. A run naming an image with no virtual placement — deleted,
// or never transmitted — contributes nothing, which is how kitty leaves the
// cells blank rather than drawing something arbitrary.
func appendKittyTile(out []*PlacedImage, v *PlacedImage, run kittyTileRun, cw, ch int) []*PlacedImage {
	if v == nil || v.Image == nil {
		return out
	}
	cols, rows := v.CellsWide, v.CellsHigh
	if cols <= 0 || rows <= 0 {
		return out
	}
	if run.imgRow < 0 || run.imgRow >= rows || run.imgCol < 0 || run.imgCol >= cols {
		return out // a cell pointing outside the image shows nothing
	}
	n := run.cells
	if run.imgCol+n > cols {
		n = cols - run.imgCol
	}

	// Cut on the grid the whole image divides into, not by multiplying one
	// cell's width: computing both edges from the same division is what makes
	// adjacent tiles meet exactly, with no seam and no overlap, however badly
	// the image size divides by the cell count.
	sx, sy, sw, sh := v.SourceRect()
	x0 := sx + run.imgCol*sw/cols
	x1 := sx + (run.imgCol+n)*sw/cols
	y0 := sy + run.imgRow*sh/rows
	y1 := sy + (run.imgRow+1)*sh/rows
	if x1 <= x0 || y1 <= y0 {
		return out
	}

	return append(out, &PlacedImage{
		Row: run.screenRow, Col: run.screenCol,
		CellsWide: n, CellsHigh: 1,
		Image:       v.Image,
		ImageID:     v.ImageID,
		PlacementID: v.PlacementID,
		ZIndex:      v.ZIndex,
		SrcX:        x0, SrcY: y0, SrcW: x1 - x0, SrcH: y1 - y0,
		// The tile is drawn to fill its cells exactly, whatever the source
		// slice measures — that is the point of positioning by cells.
		DestW: n * cw, DestH: ch,
	})
}

// IsKittyPlaceholderCell reports whether a cell is a Unicode placeholder, so a
// renderer can skip drawing it as text. The cell's job is to reserve space and
// name an image, not to show a character: U+10EEEE is private-use, so a font
// that happens to have a glyph there would paint something arbitrary, and at a
// negative z-index it would paint it right over the image the cell exists to
// position.
func IsKittyPlaceholderCell(c Cell) bool {
	return c.Char == KittyPlaceholderRune
}
