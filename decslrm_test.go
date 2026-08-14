package purfecterm

import "testing"

// CSI s is SCP until DECLRMM (?69) is enabled, after which it is DECSLRM.
func TestDECSLRMDisambiguation(t *testing.T) {
	b := NewBuffer(20, 5, 100)
	p := NewParser(b)

	// ?69 off: CSI s / CSI u are Save/Restore Cursor Position.
	p.Parse([]byte("\x1b[3;5H")) // cursor (4,2)
	p.Parse([]byte("\x1b[s"))    // SCP
	p.Parse([]byte("\x1b[1;1H")) // move away
	p.Parse([]byte("\x1b[u"))    // RCP
	cursorAt(t, b, 4, 2)
	if l, r := b.GetLeftRightMargins(); l != 0 || r != 19 {
		t.Fatalf("no margins expected, got (%d,%d)", l, r)
	}

	// ?69 on: CSI Pl ; Pr s is DECSLRM.
	p.Parse([]byte("\x1b[?69h"))
	p.Parse([]byte("\x1b[5;15s")) // left=4, right=14 (0-based)
	if l, r := b.GetLeftRightMargins(); l != 4 || r != 14 {
		t.Fatalf("DECSLRM margins = (%d,%d), want (4,14)", l, r)
	}
	cursorAt(t, b, 0, 0) // DECSLRM homes the cursor
}

// A vertical scroll with left/right margins moves only the rectangle; columns
// outside the margins are untouched.
func TestDECSLRMRectangularScroll(t *testing.T) {
	b := NewBuffer(10, 4, 100)
	p := NewParser(b)
	// Mark col 0 (outside left), cols 2..5 (inside), col 7 (outside right), per row.
	for row := 1; row <= 4; row++ {
		digit := rune('0' + row - 1)
		p.Parse([]byte("\x1b[" + string(rune('0'+row)) + ";1HL")) // col0
		p.Parse([]byte("\x1b[" + string(rune('0'+row)) + ";3H" + string(digit)))
		p.Parse([]byte("\x1b[" + string(rune('0'+row)) + ";8HR")) // col7
	}
	p.Parse([]byte("\x1b[?69h\x1b[3;6s")) // left=2, right=5
	p.Parse([]byte("\x1b[4;3H\n"))        // cursor to bottom row, LF -> rectangular scroll up

	// Columns outside the margins are untouched.
	for y := 0; y < 4; y++ {
		if got := cellChar(b, 0, y); got != 'L' {
			t.Errorf("col0 row%d = %q, want L (untouched)", y, got)
		}
		if got := cellChar(b, 7, y); got != 'R' {
			t.Errorf("col7 row%d = %q, want R (untouched)", y, got)
		}
	}
	// Inside column shifted up: '1','2','3' then blank.
	for _, tc := range []struct {
		y    int
		want rune
	}{{0, '1'}, {1, '2'}, {2, '3'}, {3, ' '}} {
		if got := cellChar(b, 2, tc.y); got != tc.want {
			t.Errorf("inside col2 row%d = %q, want %q", tc.y, got, tc.want)
		}
	}
}

// Auto-wrap happens at the right margin and returns to the left margin.
func TestDECSLRMAutoWrap(t *testing.T) {
	b := NewBuffer(10, 3, 100)
	p := NewParser(b)
	p.Parse([]byte("\x1b[?69h\x1b[3;7s")) // left=2, right=6 (5 columns)
	p.Parse([]byte("\x1b[1;3H"))          // cursor to the left margin, row 0
	p.Parse([]byte("ABCDEF"))             // fills cols 2..6 then wraps

	if got := cellChar(b, 2, 0); got != 'A' {
		t.Fatalf("col2 row0 = %q, want A", got)
	}
	if got := cellChar(b, 6, 0); got != 'E' {
		t.Fatalf("col6 row0 = %q, want E", got)
	}
	if got := cellChar(b, 2, 1); got != 'F' {
		t.Fatalf("wrap should land at left margin row1, got %q", got)
	}
}

// CR returns to the left margin (or column 0 when the cursor is left of it).
func TestDECSLRMCarriageReturn(t *testing.T) {
	b := NewBuffer(10, 3, 100)
	p := NewParser(b)
	p.Parse([]byte("\x1b[?69h\x1b[4;8s")) // left=3, right=7

	p.Parse([]byte("\x1b[1;10H\r")) // cursor col9 (>= left) -> CR to left margin
	if x, _ := b.GetCursor(); x != 3 {
		t.Fatalf("CR from col9 -> %d, want left margin 3", x)
	}
	p.Parse([]byte("\x1b[1;2H\r")) // cursor col1 (< left) -> CR to column 0
	if x, _ := b.GetCursor(); x != 0 {
		t.Fatalf("CR from col1 -> %d, want 0", x)
	}
}

// CUF/CUB stop at the right/left margins.
func TestDECSLRMCursorConfinement(t *testing.T) {
	b := NewBuffer(10, 3, 100)
	p := NewParser(b)
	p.Parse([]byte("\x1b[?69h\x1b[4;8s")) // left=3, right=7
	p.Parse([]byte("\x1b[1;5H"))          // col4 (inside)
	p.Parse([]byte("\x1b[100C"))          // CUF -> right margin
	if x, _ := b.GetCursor(); x != 7 {
		t.Fatalf("CUF clamp -> %d, want right margin 7", x)
	}
	p.Parse([]byte("\x1b[100D")) // CUB -> left margin
	if x, _ := b.GetCursor(); x != 3 {
		t.Fatalf("CUB clamp -> %d, want left margin 3", x)
	}
}

// DCH shifts left only within [cursorX, right]; columns beyond the right margin
// and before the cursor are untouched.
func TestDECSLRMDeleteChars(t *testing.T) {
	b := NewBuffer(10, 2, 100)
	p := NewParser(b)
	p.Parse([]byte("\x1b[1;1H0123456789")) // full row before margins
	p.Parse([]byte("\x1b[?69h\x1b[3;7s"))  // left=2, right=6
	p.Parse([]byte("\x1b[1;4H"))           // cursor col3
	p.Parse([]byte("\x1b[2P"))             // DCH 2

	want := map[int]rune{0: '0', 1: '1', 2: '2', 3: '5', 4: '6', 5: ' ', 6: ' ', 7: '7', 8: '8', 9: '9'}
	for x, w := range want {
		if got := cellChar(b, x, 0); got != w {
			t.Errorf("col%d = %q, want %q", x, got, w)
		}
	}
}

// Disabling DECLRMM clears the margins and restores SCP for CSI s.
func TestDECSLRMDisableClears(t *testing.T) {
	b := NewBuffer(20, 5, 100)
	p := NewParser(b)
	p.Parse([]byte("\x1b[?69h\x1b[5;15s"))
	if l, r := b.GetLeftRightMargins(); l != 4 || r != 14 {
		t.Fatalf("margins = (%d,%d), want (4,14)", l, r)
	}
	p.Parse([]byte("\x1b[?69l"))
	if b.IsLeftRightMarginMode() {
		t.Fatal("DECLRMM should be off")
	}
	if l, r := b.GetLeftRightMargins(); l != 0 || r != 19 {
		t.Fatalf("margins should clear to full, got (%d,%d)", l, r)
	}
	// CSI s is SCP again.
	p.Parse([]byte("\x1b[2;2H\x1b[s\x1b[1;1H\x1b[u"))
	cursorAt(t, b, 1, 1)
}
