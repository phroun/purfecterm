package purfecterm

import "testing"

// Smart word wrap (mode 7702) moves the indent — and any word carried past the
// wrap point — to the head of the row below. That row is a real grid row an
// application may already have painted with absolute cursor positioning. The
// original implementation PREPENDED the indent to whatever was already there,
// and nothing truncated the surplus, so a full-screen program that re-wraps the
// same region every frame (e.g. an animation redrawn while its host terminal is
// being resized narrower than the content) grew a single line's backing slice by
// the indent on every wrap — an unbounded row that drove memory and the
// quadratic visual-width scan without limit.
//
// The invariant the fix restores: repeatedly wrapping onto an already-painted
// row must not make that row's backing slice grow with the number of wraps. The
// leading cells are OVERWRITTEN in place, so the width is bounded by the content,
// independent of how many times the region re-wraps.
func TestSmartWrapDoesNotGrowRowUnbounded(t *testing.T) {
	const cols, rows = 20, 6

	// widest backing slice across the whole screen after `iters` frames, each
	// frame repainting the same over-wide indented line from the top-left via
	// absolute positioning — exactly what a redrawing full-screen app does.
	widestAfter := func(iters int) int {
		b := NewBuffer(cols, rows, 100)
		if !b.IsSmartWordWrapEnabled() {
			t.Fatal("smart word wrap should be enabled by default")
		}
		p := NewParser(b)
		for i := 0; i < iters; i++ {
			// CUP to row 1, col 1 (1-based), then a line far wider than `cols`
			// with a leading indent and word boundaries so the smart-wrap path
			// (not the plain auto-wrap path) handles it.
			p.Parse([]byte("\x1b[1;1H   the quick brown fox jumps over the lazy dog"))
		}
		widest := 0
		for _, line := range b.screen {
			if len(line) > widest {
				widest = len(line)
			}
		}
		return widest
	}

	few := widestAfter(20)
	many := widestAfter(400)

	// With the fix the width is content-bound and identical regardless of how
	// many frames were drawn. With the prepend bug, `many` grows roughly in
	// proportion to the extra iterations and dwarfs `few`.
	if many > few {
		t.Errorf("row backing slice grew with iteration count: 20 frames -> %d cells, 400 frames -> %d cells; "+
			"smart wrap must overwrite the wrap target in place, not prepend to it", few, many)
	}

	// A hard ceiling too, independent of the comparison above: no single row may
	// exceed a small multiple of the terminal width.
	if many > 8*cols {
		t.Errorf("widest row is %d cells for a %d-column terminal; a wrapped line must stay bounded", many, cols)
	}
}
