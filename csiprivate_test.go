package purfecterm

import (
	"strings"
	"testing"
)

// screen returns everything visible, so a test can assert that a control
// sequence left NOTHING behind.
func screen(b *Buffer) string {
	cols, rows := b.GetSize()
	var sb strings.Builder
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			if c := b.GetVisibleCell(x, y); c.Char != 0 {
				sb.WriteRune(c.Char)
			} else {
				sb.WriteByte(' ')
			}
		}
	}
	return strings.TrimRight(sb.String(), " ")
}

// A CSI sequence carrying a private parameter prefix must be CONSUMED WHOLE,
// supported or not. The four prefixes are 0x3C-0x3F: '<', '=', '>', '?'.
//
// An unrecognized prefix used to end the sequence on the spot, leaving the
// parameters to be printed as ordinary text. The case that showed it: a TUI
// resetting the Kitty keyboard protocol on exit with CSI = 0 ; 1 u left
// "0;1u" on the screen as it quit.
func TestCSIPrivatePrefixesAreConsumed(t *testing.T) {
	for _, seq := range []string{
		"\x1b[<u",      // Kitty keyboard: pop the flag stack
		"\x1b[>1u",     // Kitty keyboard: push flags
		"\x1b[=0;1u",   // Kitty keyboard: reset flags -- the reported bug
		"\x1b[?25h",    // DECTCEM, a supported one, for contrast
		"\x1b[>4;2m",   // modifyOtherKeys
		"\x1b[<0;0;0m", // an unsupported '<' form
		"\x1b[=c",      // DA3
		"\x1b[!p",      // DECSTR
	} {
		b := NewBuffer(40, 3, 100)
		NewParser(b).Parse([]byte(seq))
		if got := screen(b); got != "" {
			t.Errorf("%q left %q on the screen; a private-prefixed CSI must be consumed whole", seq, got)
		}
	}
}

// The whole teardown a TUI writes on its way out, in one go: it must leave a
// clean screen behind for the shell prompt.
func TestTUITeardownLeavesNothingOnScreen(t *testing.T) {
	b := NewBuffer(40, 3, 100)
	NewParser(b).Parse([]byte("\x1b[?1049l\x1b[<u\x1b[=0;1u"))
	if got := screen(b); got != "" {
		t.Errorf("teardown left %q on the screen", got)
	}
}

// The guard on the fix: an unsupported final byte with NO private prefix is
// still consumed with its parameters, which already worked.
func TestUnsupportedFinalByteIsStillConsumed(t *testing.T) {
	b := NewBuffer(40, 3, 100)
	NewParser(b).Parse([]byte("\x1b[3;4Z"))
	if got := screen(b); got != "" {
		t.Errorf("unsupported CSI left %q on the screen", got)
	}
}
