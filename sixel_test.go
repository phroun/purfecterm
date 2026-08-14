package purfecterm

import "testing"

// A single full sixel column decodes to a 1x6 image in the selected color.
func TestSixelDecodeBasic(t *testing.T) {
	img := DecodeSixel(nil, "#1;2;100;0;0~", DefaultBackground) // color 1 = red, '~' = all 6 bits
	if img.W != 1 || img.H != 6 {
		t.Fatalf("size = %dx%d, want 1x6", img.W, img.H)
	}
	for y := 0; y < 6; y++ {
		r, g, b, a := img.At(0, y)
		if r != 255 || g != 0 || b != 0 || a != 255 {
			t.Fatalf("pixel (0,%d) = %d,%d,%d,%d, want opaque red", y, r, g, b, a)
		}
	}
}

// RLE repeats a sixel; "-" starts a new band below.
func TestSixelRLEAndBands(t *testing.T) {
	img := DecodeSixel(nil, "#1;2;100;0;0!3~-@", DefaultBackground)
	// band 0: 3 columns of 6 red pixels; band 1: column 0, top pixel ('@' = bit 0).
	if img.W != 3 || img.H != 7 {
		t.Fatalf("size = %dx%d, want 3x7", img.W, img.H)
	}
	if r, _, _, _ := img.At(2, 5); r != 255 {
		t.Fatal("(2,5) should be red")
	}
	if r, _, _, _ := img.At(0, 6); r != 255 {
		t.Fatal("(0,6) in band 1 should be red")
	}
}

// P2=1 makes unset pixels transparent.
func TestSixelTransparency(t *testing.T) {
	// 'D' = value 5 = bits 0 and 2 set, bit 1 clear.
	img := DecodeSixel([]int{0, 1}, "#1;2;100;100;100D", DefaultBackground)
	if img.H != 3 {
		t.Fatalf("H = %d, want 3", img.H)
	}
	if _, _, _, a := img.At(0, 0); a != 255 {
		t.Fatal("set pixel (0,0) should be opaque")
	}
	if _, _, _, a := img.At(0, 1); a != 0 {
		t.Fatal("unset pixel (0,1) should be transparent under P2=1")
	}
	if _, _, _, a := img.At(0, 2); a != 255 {
		t.Fatal("set pixel (0,2) should be opaque")
	}
}

// A Sixel DCS through the parser places a cell-anchored image and leaves the
// cursor on the image's last row (sixel scrolling).
func TestSixelPlacement(t *testing.T) {
	b := NewBuffer(20, 10, 100)
	b.SetCellPixelSize(10, 20)
	p := NewParser(b)
	p.Parse([]byte("\x1bPq#1;2;100;0;0~\x1b\\")) // 1x6 red column

	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatalf("images = %d, want 1", len(imgs))
	}
	if imgs[0].Row != 0 || imgs[0].Col != 0 {
		t.Fatalf("anchor = (%d,%d), want (0,0)", imgs[0].Row, imgs[0].Col)
	}
	if imgs[0].CellsWide != 1 || imgs[0].CellsHigh != 1 {
		t.Fatalf("cells = %dx%d, want 1x1", imgs[0].CellsWide, imgs[0].CellsHigh)
	}
	// One cell tall, so the cursor stays on row 0 — the image's own last row.
	// The program's trailing newline is what moves past it.
	cursorAt(t, b, 0, 0)
}

// A placed image scrolls up with the text and is dropped when it leaves the top.
func TestSixelScrollShift(t *testing.T) {
	b := NewBuffer(10, 4, 100)
	b.SetCellPixelSize(10, 20)
	p := NewParser(b)
	p.Parse([]byte("\x1b[?8452l")) // disable sixel scrolling so the anchor stays put
	p.Parse([]byte("\x1b[3;1H"))   // cursor to row 2
	p.Parse([]byte("\x1bPq~\x1b\\"))

	if imgs := b.GetImages(); len(imgs) != 1 || imgs[0].Row != 2 {
		t.Fatalf("image anchor = %v, want row 2", imgs)
	}
	p.Parse([]byte("\x1b[4;1H\n")) // LF at the bottom -> full-screen scroll up
	if imgs := b.GetImages(); len(imgs) != 1 || imgs[0].Row != 1 {
		t.Fatalf("after scroll, anchor = %v, want row 1", imgs)
	}
	// Clearing the screen drops images.
	p.Parse([]byte("\x1b[2J"))
	if imgs := b.GetImages(); len(imgs) != 0 {
		t.Fatalf("ED2 should clear images, got %d", len(imgs))
	}
}
