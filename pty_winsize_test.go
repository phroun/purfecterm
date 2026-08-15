package purfecterm

import "testing"

// fakePTY records what it was resized to.
type fakePTY struct {
	cols, rows        int
	widthPx, heightPx int
	calls             int
}

func (f *fakePTY) Read(p []byte) (int, error)  { return 0, nil }
func (f *fakePTY) Write(p []byte) (int, error) { return len(p), nil }
func (f *fakePTY) Resize(cols, rows int) error {
	return f.ResizeWithPixels(cols, rows, f.widthPx, f.heightPx)
}
func (f *fakePTY) ResizeWithPixels(cols, rows, widthPx, heightPx int) error {
	f.cols, f.rows, f.widthPx, f.heightPx = cols, rows, widthPx, heightPx
	f.calls++
	return nil
}

// A plain Resize must not zero the pixel size. A graphical client reads the
// window's pixel dimensions from the tty rather than asking the terminal, and a
// zero there is not "unknown" to it — it is a zero-sized viewport, and it draws
// nothing at all, with no error to show for it.
func TestPTYResizeKeepsPixelSize(t *testing.T) {
	f := &fakePTY{}
	if err := f.ResizeWithPixels(80, 24, 800, 480); err != nil {
		t.Fatal(err)
	}
	if f.widthPx != 800 || f.heightPx != 480 {
		t.Fatalf("pixel size = %dx%d, want 800x480", f.widthPx, f.heightPx)
	}
	// A resize that names only cells keeps what was last reported.
	if err := f.Resize(100, 30); err != nil {
		t.Fatal(err)
	}
	if f.widthPx != 800 || f.heightPx != 480 {
		t.Errorf("a cell-only resize zeroed the pixel size to %dx%d", f.widthPx, f.heightPx)
	}
	if f.cols != 100 || f.rows != 30 {
		t.Errorf("cells = %dx%d, want 100x30", f.cols, f.rows)
	}
}

// The pixel dimensions a terminal reports follow from the cell size it was
// told, so the two cannot drift apart.
func TestPTYPixelSizeFollowsCellSize(t *testing.T) {
	b := NewBuffer(80, 24, 100)
	b.SetCellPixelSize(10, 20)
	cw, ch := b.GetCellPixelSize()
	if got := 80 * cw; got != 800 {
		t.Errorf("width = %d px, want 800", got)
	}
	if got := 24 * ch; got != 480 {
		t.Errorf("height = %d px, want 480", got)
	}
	// A terminal that never learned its cell size reports zero, which is the
	// honest answer — and the reason a renderer must report one.
	b2 := NewBuffer(80, 24, 100)
	if cw2, ch2 := b2.GetCellPixelSize(); cw2 != 0 || ch2 != 0 {
		t.Errorf("an untold buffer reports %dx%d, want 0x0", cw2, ch2)
	}
}
