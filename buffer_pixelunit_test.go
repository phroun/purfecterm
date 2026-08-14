package purfecterm

import "testing"

// The two pixel units are independent axes: a renderer can pin a synthetic
// pointer grid without disturbing the real cell size images are measured
// against. Conflating them made an image's row count follow the pointer grid.
func TestPointerPixelUnitIsIndependentOfCellSize(t *testing.T) {
	b := NewBuffer(40, 20, 100)
	b.SetCellPixelSize(10, 20)
	b.SetPointerPixelUnit(1000, 1000) // a fixed sub-cell grid, as a scaling renderer wants

	if w, h := b.GetCellPixelSize(); w != 10 || h != 20 {
		t.Errorf("cell size = %dx%d, want the real 10x20", w, h)
	}
	if w, h := b.GetPointerPixelUnit(); w != 1000 || h != 1000 {
		t.Errorf("pointer unit = %dx%d, want 1000x1000", w, h)
	}

	// Image geometry must follow the REAL cell, not the pointer grid: at 1000
	// this image would have collapsed to a single row.
	b.SetCursor(0, 0)
	b.PlaceSixelImage(&SixelImage{W: 30, H: 60, RGBA: make([]byte, 30*60*4)})
	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatalf("placed %d images, want 1", len(imgs))
	}
	if imgs[0].CellsWide != 3 || imgs[0].CellsHigh != 3 {
		t.Errorf("cells = %dx%d, want 3x3 from the real 10x20 cell",
			imgs[0].CellsWide, imgs[0].CellsHigh)
	}
}

// Unset, the pointer unit is the real cell size, so an ordinary renderer that
// only calls SetCellPixelSize keeps working unchanged.
func TestPointerPixelUnitDefaultsToCellSize(t *testing.T) {
	b := NewBuffer(40, 20, 100)
	b.SetCellPixelSize(9, 17)
	if w, h := b.GetPointerPixelUnit(); w != 9 || h != 17 {
		t.Errorf("pointer unit = %dx%d, want the cell size 9x17", w, h)
	}
	// Per-axis fallback, so a renderer can pin one axis only.
	b.SetPointerPixelUnit(1000, 0)
	if w, h := b.GetPointerPixelUnit(); w != 1000 || h != 17 {
		t.Errorf("pointer unit = %dx%d, want 1000x17", w, h)
	}
}

// CSI 16 t reports the REAL cell size: a program reads it to size an image, and
// a synthetic pointer grid there would make every image absurd.
func TestCellSizeReportUsesRealCell(t *testing.T) {
	b := NewBuffer(40, 20, 100)
	b.SetCellPixelSize(10, 20)
	b.SetPointerPixelUnit(1000, 1000)

	var got []byte
	p := NewParser(b)
	p.SetResponseSink(func(data []byte) { got = append(got, data...) })
	p.Parse([]byte("\x1b[16t"))

	if want := "\x1b[6;20;10t"; string(got) != want {
		t.Errorf("CSI 16 t answered %q, want %q (the real cell, not the pointer grid)", got, want)
	}
}
