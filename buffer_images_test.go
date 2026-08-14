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

// The cursor lands immediately under the image: an image N cells tall advances
// it exactly N rows, leaving no blank gap before whatever the program prints
// next. N follows the cell size the renderer reported, which is why a widget
// that never calls SetCellPixelSize — leaving the nominal 10x20 fallback in
// place while drawing into a taller cell — reserves too many rows and opens a
// gap under every image.
func TestSixelImageAdvancesCursorByItsOwnHeight(t *testing.T) {
	for _, cellH := range []int{16, 20, 40} {
		b := NewBuffer(40, 30, 200)
		b.SetCellPixelSize(10, cellH)
		b.SetCursor(0, 0)
		// Exactly 4 cells tall at this cell size.
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
		if _, cy := b.GetCursor(); cy != 4 {
			t.Errorf("cell height %d: cursor landed on row %d, want 4 — %d blank row(s) under the image",
				cellH, cy, cy-4)
		}
	}
}

// A partial last row still counts as a row: the cursor clears the image rather
// than landing inside it.
func TestSixelImagePartialRowRoundsUp(t *testing.T) {
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
	if _, cy := b.GetCursor(); cy != 3 {
		t.Errorf("cursor landed on row %d, want 3", cy)
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
