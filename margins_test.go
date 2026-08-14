package purfecterm

import "testing"

func cursorAt(t *testing.T, b *Buffer, wantX, wantY int) {
	t.Helper()
	x, y := b.GetCursor()
	if x != wantX || y != wantY {
		t.Fatalf("cursor = (%d,%d), want (%d,%d)", x, y, wantX, wantY)
	}
}

func cellChar(b *Buffer, x, y int) rune {
	c := b.GetVisibleCell(x, y).Char
	if c == 0 {
		return ' '
	}
	return c
}

// DECSTBM sets the region (0-based inclusive) and homes the cursor.
func TestDECSTBMSetAndHome(t *testing.T) {
	b := NewBuffer(10, 6, 100)
	p := NewParser(b)
	p.Parse([]byte("\x1b[3;5H")) // move cursor somewhere first
	p.Parse([]byte("\x1b[2;4r")) // region rows 2..4 (1-based)
	top, bottom := b.GetScrollRegion()
	if top != 1 || bottom != 3 {
		t.Fatalf("region = (%d,%d), want (1,3)", top, bottom)
	}
	cursorAt(t, b, 0, 0) // DECSTBM homes the cursor (origin mode off)
}

// A full-screen scroll feeds scrollback; an interior-region scroll does not.
func TestRegionScrollSkipsScrollback(t *testing.T) {
	// Full screen: LF at the last row scrolls into history.
	full := NewBuffer(10, 3, 100)
	pf := NewParser(full)
	pf.Parse([]byte("\x1b[3;1H")) // last row
	before := full.GetScrollbackSize()
	pf.Parse([]byte("\n"))
	if full.GetScrollbackSize() != before+1 {
		t.Fatalf("full-screen LF should add 1 scrollback line, got %d->%d", before, full.GetScrollbackSize())
	}

	// Region: LF at the bottom margin scrolls the band, not history.
	reg := NewBuffer(10, 6, 100)
	pr := NewParser(reg)
	pr.Parse([]byte("\x1b[2;4r")) // region [1,3]
	pr.Parse([]byte("\x1b[4;1H")) // bottom margin (1-based row 4)
	sb := reg.GetScrollbackSize()
	pr.Parse([]byte("\n\n\n"))
	if reg.GetScrollbackSize() != sb {
		t.Fatalf("region scroll must not touch scrollback, got %d->%d", sb, reg.GetScrollbackSize())
	}
}

// LF at the bottom margin scrolls only the region; rows outside are untouched.
func TestRegionScrollContent(t *testing.T) {
	b := NewBuffer(10, 6, 100)
	p := NewParser(b)
	p.Parse([]byte("\x1b[2;4r"))                      // region [1,3]
	p.Parse([]byte("\x1b[1;1HZ"))                     // row0 (above region)
	p.Parse([]byte("\x1b[2;1HA\x1b[3;1HB\x1b[4;1HC")) // region rows
	p.Parse([]byte("\x1b[5;1HY"))                     // row4 (below region)
	p.Parse([]byte("\x1b[4;1H\n"))                    // LF at bottom margin -> region scrolls up

	// A left, B/C shift up, bottom margin blanked; Z and Y untouched.
	for _, tc := range []struct {
		x, y int
		want rune
	}{
		{0, 0, 'Z'}, {0, 1, 'B'}, {0, 2, 'C'}, {0, 3, ' '}, {0, 4, 'Y'},
	} {
		if got := cellChar(b, tc.x, tc.y); got != tc.want {
			t.Errorf("cell(%d,%d) = %q, want %q", tc.x, tc.y, got, tc.want)
		}
	}
}

// RI at the top margin reverse-scrolls the region.
func TestReverseIndexAtTopMargin(t *testing.T) {
	b := NewBuffer(10, 6, 100)
	p := NewParser(b)
	p.Parse([]byte("\x1b[2;4r"))
	p.Parse([]byte("\x1b[2;1HA\x1b[3;1HB\x1b[4;1HC"))
	p.Parse([]byte("\x1b[2;1H\x1bM")) // cursor to top margin, RI

	// Region shifts down: top blanked, A->row3, B->row4; C pushed out.
	for _, tc := range []struct {
		x, y int
		want rune
	}{
		{0, 1, ' '}, {0, 2, 'A'}, {0, 3, 'B'},
	} {
		if got := cellChar(b, tc.x, tc.y); got != tc.want {
			t.Errorf("cell(%d,%d) = %q, want %q", tc.x, tc.y, got, tc.want)
		}
	}
}

// DECOM makes CUP row margin-relative and confines the cursor to the region.
func TestOriginModeCUP(t *testing.T) {
	b := NewBuffer(10, 6, 100)
	p := NewParser(b)
	p.Parse([]byte("\x1b[2;4r")) // region [1,3]
	p.Parse([]byte("\x1b[?6h"))  // DECOM on -> homes to region top
	if !b.IsOriginMode() {
		t.Fatal("origin mode should be on")
	}
	cursorAt(t, b, 0, 1)

	p.Parse([]byte("\x1b[1;1H"))
	cursorAt(t, b, 0, 1) // row 1 relative == top margin (idx 1)
	p.Parse([]byte("\x1b[2;1H"))
	cursorAt(t, b, 0, 2)
	p.Parse([]byte("\x1b[99;1H"))
	cursorAt(t, b, 0, 3) // clamped to bottom margin

	p.Parse([]byte("\x1b[?6l")) // DECOM off -> homes to screen origin
	cursorAt(t, b, 0, 0)
	p.Parse([]byte("\x1b[5;1H"))
	cursorAt(t, b, 0, 4) // absolute
}

// RIS and DECSTR both clear the scroll region and origin mode.
func TestResetClearsRegion(t *testing.T) {
	for _, seq := range []string{"\x1bc", "\x1b[!p"} {
		b := NewBuffer(10, 6, 100)
		p := NewParser(b)
		p.Parse([]byte("\x1b[2;4r\x1b[?6h"))
		p.Parse([]byte(seq))
		top, bottom := b.GetScrollRegion()
		if top != 0 || bottom != 5 {
			t.Errorf("%q: region = (%d,%d), want full (0,5)", seq, top, bottom)
		}
		if b.IsOriginMode() {
			t.Errorf("%q: origin mode should be cleared", seq)
		}
	}
}

type scrollRecorder struct {
	NopCaptureObserver
	scrollOff int
	lineOff   int
}

func (r *scrollRecorder) OnScrollLineOff(n int)          { r.scrollOff += n }
func (r *scrollRecorder) OnLineOff(_ []Cell, _ LineInfo) { r.lineOff++ }

// Capture: an interior-region scroll is invisible to the transcript rungs; a
// full-screen scroll fires both OnScrollLineOff and OnLineOff.
func TestRegionScrollCaptureSuppressed(t *testing.T) {
	reg := NewBuffer(10, 6, 100)
	pr := NewParser(reg)
	rr := &scrollRecorder{}
	reg.SetCaptureObserver(rr)
	reg.SetCaptureLive(true)
	pr.Parse([]byte("\x1b[2;4r\x1b[4;1H\n\n"))
	if rr.scrollOff != 0 || rr.lineOff != 0 {
		t.Fatalf("region scroll fired capture events: scrollOff=%d lineOff=%d, want 0,0", rr.scrollOff, rr.lineOff)
	}

	full := NewBuffer(10, 3, 100)
	pf := NewParser(full)
	rf := &scrollRecorder{}
	full.SetCaptureObserver(rf)
	full.SetCaptureLive(true)
	pf.Parse([]byte("\x1b[3;1H\n\n"))
	if rf.scrollOff != 2 {
		t.Fatalf("full-screen scroll OnScrollLineOff = %d, want 2", rf.scrollOff)
	}
	if rf.lineOff != 2 {
		t.Fatalf("full-screen scroll OnLineOff = %d, want 2", rf.lineOff)
	}
}
