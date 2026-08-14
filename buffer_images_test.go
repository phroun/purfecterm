package purfecterm

import "testing"

// placeTestImage anchors a cellsHigh-tall opaque image at (col,row) and returns
// the buffer's view of it.
func placeTestImage(t *testing.T, b *Buffer, col, row, cellsHigh int) *PlacedImage {
	t.Helper()
	const cw, ch = 10, 20
	b.SetCellPixelSize(cw, ch)
	b.SetSixelScrolling(false) // keep the cursor put; this test is about the anchor
	b.SetCursor(col, row)
	b.PlaceSixelImage(&SixelImage{
		W: cw, H: ch * cellsHigh, RGBA: make([]byte, cw*ch*cellsHigh*4),
	})
	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatalf("placed %d images, want 1", len(imgs))
	}
	if imgs[0].Row != row || imgs[0].Col != col {
		t.Fatalf("anchored at (%d,%d), want (%d,%d)", imgs[0].Col, imgs[0].Row, col, row)
	}
	if imgs[0].CellsHigh != cellsHigh {
		t.Fatalf("CellsHigh = %d, want %d", imgs[0].CellsHigh, cellsHigh)
	}
	return imgs[0]
}

// The cursor is left on the image's LAST row, not below it: "when sixel mode is
// exited, the text cursor is set to the current sixel cursor position", and the
// sixel cursor is a pixel position inside the last band written. Programs end an
// image with their own newline (chafa, img2sixel do), which then lands exactly
// one row under the image; leaving the cursor below it instead puts a blank line
// under every image.
//
// The row count follows the cell size the renderer reported, which is why a
// widget that never calls SetCellPixelSize — leaving the nominal 10x20 fallback
// in place while drawing into a taller cell — reserves too many rows.
func TestSixelImageLeavesCursorOnItsLastRow(t *testing.T) {
	for _, cellH := range []int{16, 20, 40} {
		b := NewBuffer(40, 30, 200)
		b.SetCellPixelSize(10, cellH)
		b.SetCursor(0, 0)
		// Exactly 4 cells tall at this cell size: rows 0-3.
		b.PlaceSixelImage(&SixelImage{
			W: 10, H: 4 * cellH, RGBA: make([]byte, 10*4*cellH*4),
		})

		imgs := b.GetImages()
		if len(imgs) != 1 {
			t.Fatalf("cell height %d: placed %d images, want 1", cellH, len(imgs))
		}
		if imgs[0].CellsHigh != 4 {
			t.Errorf("cell height %d: CellsHigh = %d, want 4", cellH, imgs[0].CellsHigh)
		}
		if _, cy := b.GetCursor(); cy != 3 {
			t.Errorf("cell height %d: cursor landed on row %d, want 3 (the image's last row); "+
				"a trailing newline would leave %d blank row(s)", cellH, cy, cy-3)
		}
	}
}

// A partial last row still counts as a row the image occupies, so the cursor
// lands on it rather than on the last FULL row.
func TestSixelImagePartialRowCountsAsARow(t *testing.T) {
	b := NewBuffer(40, 30, 200)
	b.SetCellPixelSize(10, 20)
	b.SetCursor(0, 0)
	b.PlaceSixelImage(&SixelImage{W: 10, H: 50, RGBA: make([]byte, 10*50*4)}) // 2.5 cells

	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatalf("placed %d images, want 1", len(imgs))
	}
	if imgs[0].CellsHigh != 3 {
		t.Errorf("CellsHigh = %d, want 3 (50px over a 20px cell)", imgs[0].CellsHigh)
	}
	if _, cy := b.GetCursor(); cy != 2 {
		t.Errorf("cursor landed on row %d, want 2 (the partial last row)", cy)
	}
}

// An image running past the bottom scrolls the screen just far enough to show
// its last row, and the cursor sits on that row — not one scroll further, which
// would push the image's top off for no reason.
func TestSixelImageAtBottomScrollsJustEnough(t *testing.T) {
	const rows = 10
	b := NewBuffer(40, rows, 200)
	b.SetCellPixelSize(10, 20)
	b.SetCursor(0, rows-2) // row 8; a 4-cell image needs rows 8..11
	b.PlaceSixelImage(&SixelImage{W: 10, H: 80, RGBA: make([]byte, 10*80*4)})

	// Rows 8..11 need two scrolls to bring row 11 up to the last row (9).
	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatalf("placed %d images, want 1", len(imgs))
	}
	if imgs[0].Row != rows-2-2 {
		t.Errorf("image anchored at row %d, want %d after two scrolls", imgs[0].Row, rows-2-2)
	}
	if _, cy := b.GetCursor(); cy != rows-1 {
		t.Errorf("cursor landed on row %d, want %d (bottom row, the image's last)", cy, rows-1)
	}
	// The image's last row must be exactly the bottom row.
	if bottom := imgs[0].Row + imgs[0].CellsHigh - 1; bottom != rows-1 {
		t.Errorf("image's last row = %d, want %d (the bottom row)", bottom, rows-1)
	}
}

// An image rides the text up and is dropped only once its LAST row has left the
// screen. The anchor passing above row 0 is the interesting part: the image is
// still partly on screen there and must keep moving, or it sticks to the top
// edge and never scrolls off.
func TestSixelImageScrollsOffTheTop(t *testing.T) {
	b := NewBuffer(20, 10, 100)
	placeTestImage(t, b, 0, 4, 3)

	// Rows 3, 2, 1, 0, -1, -2 are all still (partly) on screen for a 3-cell
	// image; at -3 its last row has gone and it must be dropped.
	for want := 3; want >= -3; want-- {
		b.ScrollUp(1)
		imgs := b.GetImages()
		if want+3 <= 0 {
			if len(imgs) != 0 {
				t.Fatalf("image still present at row %d; its last row has left the screen", imgs[0].Row)
			}
			return
		}
		if len(imgs) != 1 {
			t.Fatalf("image dropped early: gone while its anchor should be row %d", want)
		}
		if imgs[0].Row != want {
			t.Fatalf("row = %d, want %d: the image stopped tracking the scroll", imgs[0].Row, want)
		}
	}
	t.Fatal("image was never dropped")
}

// A line feed at the bottom margin scrolls the screen, and the image goes with
// it — the path a program actually takes when it prints past the last row.
func TestSixelImageScrollsWithLineFeed(t *testing.T) {
	b := NewBuffer(20, 6, 100)
	placeTestImage(t, b, 2, 1, 2)

	// Anchored at row 1 and 2 cells tall, so after two scrolls it sits at -1
	// with its bottom row still showing on row 0.
	b.SetCursor(0, 5) // bottom row: each line feed scrolls
	for i := 0; i < 2; i++ {
		b.LineFeed()
	}
	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatalf("image dropped after 2 line feeds; its bottom row is still on screen")
	}
	if imgs[0].Row != -1 {
		t.Errorf("row = %d, want -1 after 2 line feeds", imgs[0].Row)
	}
	if imgs[0].Col != 2 {
		t.Errorf("col = %d, want 2: a vertical scroll must not move the image sideways", imgs[0].Col)
	}

	b.LineFeed()
	if imgs := b.GetImages(); len(imgs) != 0 {
		t.Errorf("image survived at row %d; its last row has left the screen", imgs[0].Row)
	}
}

// Reverse scroll walks the anchor back down, including from above the top edge.
func TestSixelImageScrollsBackDown(t *testing.T) {
	b := NewBuffer(20, 10, 100)
	placeTestImage(t, b, 0, 1, 3)

	b.ScrollUp(2) // anchor to -1, still two rows on screen
	if imgs := b.GetImages(); len(imgs) != 1 || imgs[0].Row != -1 {
		t.Fatalf("after 2 scrolls: %v", b.GetImages())
	}
	b.ScrollDown(1)
	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatal("image lost on reverse scroll")
	}
	if imgs[0].Row != 0 {
		t.Errorf("row = %d, want 0: reverse scroll must walk the anchor back down", imgs[0].Row)
	}
}

// An interior scroll region must not drag an image that sits entirely outside
// it — the region's rows move, the rest of the screen does not.
func TestSixelImageIgnoresUnrelatedRegionScroll(t *testing.T) {
	b := NewBuffer(20, 12, 100)
	placeTestImage(t, b, 0, 0, 2) // occupies rows 0-1

	b.SetScrollRegion(6, 10) // 1-based rows 6..10, well below the image
	b.ScrollUp(2)

	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatal("image dropped by a scroll of an unrelated region")
	}
	if imgs[0].Row != 0 {
		t.Errorf("row = %d, want 0: a region below the image must not move it", imgs[0].Row)
	}
}

// Sixel carries its own pixels and asks for no resizing, so a placed Sixel
// image reports the decoded size as its destination and the renderers stay on
// their 1:1 path.
func TestSixelImageDrawsAtItsDecodedSize(t *testing.T) {
	b := NewBuffer(40, 30, 200)
	b.SetCellPixelSize(10, 20)
	b.SetCursor(0, 0)
	b.PlaceSixelImage(&SixelImage{W: 35, H: 50, RGBA: make([]byte, 35*50*4)})

	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatalf("placed %d images, want 1", len(imgs))
	}
	w, h := imgs[0].DestSize()
	if w != 35 || h != 50 {
		t.Errorf("DestSize = %dx%d, want the decoded 35x50 (Sixel must not be rescaled)", w, h)
	}
	// The cell box rounds UP around that size; it must not feed back into the
	// draw size, or a 50px image would stretch to fill 3 cells (60px).
	if imgs[0].CellsWide != 4 || imgs[0].CellsHigh != 3 {
		t.Errorf("cells = %dx%d, want 4x3", imgs[0].CellsWide, imgs[0].CellsHigh)
	}
}

// DestSize falls back to the decoded size, so an image built without the fields
// set (or by code predating them) still draws 1:1 rather than vanishing.
func TestPlacedImageDestSizeFallsBackToDecoded(t *testing.T) {
	pi := &PlacedImage{Image: &SixelImage{W: 12, H: 7}}
	if w, h := pi.DestSize(); w != 12 || h != 7 {
		t.Errorf("DestSize = %dx%d, want 12x7", w, h)
	}
	pi.DestW, pi.DestH = 24, 14
	if w, h := pi.DestSize(); w != 24 || h != 14 {
		t.Errorf("DestSize = %dx%d, want the explicit 24x14", w, h)
	}
	// A nil image has no size to fall back to.
	if w, h := (&PlacedImage{}).DestSize(); w != 0 || h != 0 {
		t.Errorf("DestSize on a nil image = %dx%d, want 0x0", w, h)
	}
}

// Shrinking the window pushes lines into scrollback, which slides the text up
// exactly as a scroll does — and anchored images have to travel with it. The
// resize path does not run through scrollRegionUp, so it needs its own shift;
// without one the image stays put while the text slides out from under it.
func TestSixelImageFollowsTextOnShrink(t *testing.T) {
	const rows = 12
	b := NewBuffer(20, rows, 100)
	b.SetCellPixelSize(10, 20)
	b.SetSixelScrolling(false)

	// Put content on every row so the shrink has to push lines off.
	for y := 0; y < rows; y++ {
		b.SetCursor(0, y)
		b.WriteChar('x')
	}
	b.SetCursor(0, 8)
	b.PlaceSixelImage(&SixelImage{W: 10, H: 40, RGBA: make([]byte, 10*40*4)}) // 2 cells

	if imgs := b.GetImages(); len(imgs) != 1 || imgs[0].Row != 8 {
		t.Fatalf("setup: %#v", b.GetImages())
	}

	// Last content row is 11; shrinking to 6 rows pushes 11-6+1 = 6 lines off.
	b.Resize(20, 6)

	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatalf("image dropped by the shrink; it should have moved up to row 2")
	}
	if imgs[0].Row != 2 {
		t.Errorf("row = %d, want 2: the image did not follow the text into scrollback", imgs[0].Row)
	}
}

// An image that the shrink pushes entirely off the top is dropped, not left at
// a negative row forever.
func TestSixelImageDroppedWhenShrinkPushesItOff(t *testing.T) {
	const rows = 12
	b := NewBuffer(20, rows, 100)
	b.SetCellPixelSize(10, 20)
	b.SetSixelScrolling(false)

	for y := 0; y < rows; y++ {
		b.SetCursor(0, y)
		b.WriteChar('x')
	}
	b.SetCursor(0, 1)
	b.PlaceSixelImage(&SixelImage{W: 10, H: 40, RGBA: make([]byte, 10*40*4)}) // rows 1-2

	b.Resize(20, 6) // pushes 6 lines: rows 1-2 land at -5..-4
	if imgs := b.GetImages(); len(imgs) != 0 {
		t.Errorf("image survived at row %d; the shrink pushed it off the top", imgs[0].Row)
	}
}

// Anchored images belong to the screen they were placed on. An alternate-screen
// application's images must not survive back onto the primary screen, and the
// primary's must be there again when it returns.
func TestImagesAreStashedWithTheAltScreen(t *testing.T) {
	b := NewBuffer(40, 12, 100)
	b.SetCellPixelSize(10, 20)
	b.SetSixelScrolling(false)

	b.SetCursor(0, 1)
	b.PlaceSixelImage(&SixelImage{W: 10, H: 20, RGBA: make([]byte, 10*20*4)})
	if len(b.GetImages()) != 1 {
		t.Fatal("setup: the primary screen's image is missing")
	}

	b.EnterAltScreen()
	if got := len(b.GetImages()); got != 0 {
		t.Errorf("the alt screen started with %d images, want a clean screen", got)
	}
	b.SetCursor(0, 3)
	b.PlaceSixelImage(&SixelImage{W: 10, H: 20, RGBA: make([]byte, 10*20*4)})
	if len(b.GetImages()) != 1 {
		t.Fatal("the alt screen's own image was not placed")
	}

	b.LeaveAltScreen()
	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatalf("after leaving the alt screen there are %d images, want the primary's 1", len(imgs))
	}
	if imgs[0].Row != 1 {
		t.Errorf("the surviving image is at row %d, want the primary's row 1 — the alt screen's leaked", imgs[0].Row)
	}
}
