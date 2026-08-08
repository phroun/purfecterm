package purfecterm

import (
	"strings"
	"testing"
)

// The trim option stops content at the last used line, dropping the empty tail
// of the fixed-height screen grid; the default keeps dumping the whole grid.
func TestSaveScrollbackTextTrimTail(t *testing.T) {
	b := NewBuffer(20, 5, 100)
	NewParser(b).Parse([]byte("line1\r\nline2\r\n")) // rows 0,1 used; 2..4 blank

	if full := b.SaveScrollbackText(); full != "line1\nline2\n\n\n\n" {
		t.Fatalf("untrimmed text = %q, want the full 5-line grid", full)
	}
	trimmed := b.SaveScrollbackTextOpts(ScrollbackSaveOptions{TrimTrailingBlankLines: true})
	if trimmed != "line1\nline2\n" {
		t.Fatalf("trimmed text = %q, want %q", trimmed, "line1\nline2\n")
	}
}

// Trimming the ANS form drops the blank tail but keeps the reload footer, with
// the cursor-reposition move-up reduced to the last line actually output: the
// cursor is homed over a 10-row grid holding 3 used lines, so the full-grid
// ESC[9A becomes ESC[2A once the 7 blank rows are trimmed.
func TestSaveScrollbackANSTrimCursorMoveUp(t *testing.T) {
	b := NewBuffer(20, 10, 100)
	NewParser(b).Parse([]byte("A\r\nB\r\nC\r\n\x1b[H")) // 3 used rows, cursor home

	if full := b.SaveScrollbackANS(); !strings.Contains(full, "\x1b[9A") {
		t.Fatalf("untrimmed ANS should move up 9 from the grid bottom; got %q", full)
	}
	trimmed := b.SaveScrollbackANSOpts(ScrollbackSaveOptions{TrimTrailingBlankLines: true})
	if !strings.Contains(trimmed, "\x1b[2A") {
		t.Fatalf("trimmed ANS should move up 2 (to the last used line); got %q", trimmed)
	}
	if strings.Contains(trimmed, "\x1b[9A") {
		t.Fatalf("trimmed ANS still carried the full-grid move-up; got %q", trimmed)
	}
}
