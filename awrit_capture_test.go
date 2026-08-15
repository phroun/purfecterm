package purfecterm

import (
	"strings"
	"testing"
)

var awritStartup = []byte{27, 32, 70, 27, 55, 27, 91, 63, 115, 27, 91, 35, 80, 27, 91, 42, 120, 27, 91, 52, 108, 27, 91, 63, 50, 53, 108, 27, 91, 63, 49, 108, 27, 91, 63, 53, 108, 27, 91, 63, 50, 48, 48, 52, 108, 27, 91, 63, 49, 48, 48, 52, 108, 27, 91, 63, 49, 48, 48, 48, 108, 27, 91, 63, 49, 48, 48, 50, 108, 27, 91, 63, 49, 48, 48, 51, 108, 27, 91, 63, 49, 48, 48, 53, 108, 27, 91, 63, 49, 48, 48, 54, 108, 27, 91, 63, 56, 104, 27, 91, 63, 55, 104, 27, 91, 63, 49, 48, 52, 57, 104, 27, 91, 63, 49, 48, 49, 54, 104, 27, 91, 63, 49, 48, 48, 51, 104, 27, 91, 72, 27, 91, 50, 74, 27, 91, 63, 117, 27, 91, 99, 27, 95, 71, 102, 61, 50, 52, 44, 105, 61, 52, 50, 57, 52, 49, 49, 49, 50, 57, 53, 44, 116, 61, 100, 44, 115, 61, 49, 44, 118, 61, 49, 44, 122, 61, 49, 59, 65, 65, 65, 65, 27, 92, 27, 95, 71, 97, 61, 102, 44, 105, 61, 52, 50, 57, 52, 49, 49, 49, 50, 57, 53, 44, 102, 61, 50, 52, 44, 116, 61, 100, 44, 115, 61, 49, 44, 118, 61, 49, 44, 122, 61, 49, 44, 114, 61, 50, 59, 65, 65, 65, 65, 27, 92, 27, 95, 71, 97, 61, 99, 44, 67, 61, 49, 44, 105, 61, 52, 50, 57, 52, 49, 49, 49, 50, 57, 53, 44, 114, 61, 50, 44, 99, 61, 49, 44, 120, 61, 48, 44, 121, 61, 48, 44, 119, 61, 49, 44, 104, 61, 49, 27, 92, 27, 91, 62, 49, 53, 117, 27, 91, 63, 50, 48, 48, 52, 104, 27, 91, 63, 49, 48, 48, 52, 104, 27, 91, 63, 49, 48, 48, 48, 104, 27, 91, 63, 49, 48, 48, 50, 104, 27, 91, 63, 49, 48, 48, 51, 104, 27, 91, 63, 49, 48, 49, 53, 104, 27, 91, 63, 49, 48, 48, 54, 104, 27, 91, 63, 49, 48, 49, 54, 104, 27, 91, 72, 27, 91, 50, 74, 27, 91, 48, 59, 48, 72, 27, 95, 71, 102, 61, 51, 50, 44, 116, 61, 115, 44, 115, 61, 51, 44, 118, 61, 51, 44, 97, 61, 84, 44, 113, 61, 50, 44, 67, 61, 49, 44, 105, 61, 49, 59, 76, 50, 70, 51, 99, 109, 108, 48, 88, 122, 69, 52, 89, 50, 74, 107, 77, 71, 78, 104, 89, 87, 73, 122, 79, 68, 77, 51, 79, 68, 103, 61, 27, 92, 27, 95, 71, 97, 61, 97, 44, 105, 61, 49, 44, 99, 61, 49, 27, 92, 27, 93, 50, 50, 59, 100, 101, 102, 97, 117, 108, 116, 27, 92, 27, 95, 71, 97, 61, 100, 44, 100, 61, 65, 27, 92, 27, 91, 72, 27, 91, 50, 74, 27, 91, 63, 49, 48, 52, 57, 108, 27, 91, 63, 49, 48, 48, 51, 108, 27, 91, 63, 49, 48, 49, 54, 108, 27, 91, 63, 50, 53, 104, 27, 91, 63, 114, 27, 56, 27, 91, 35, 81, 27, 91, 63, 50, 48, 48, 52, 108, 27, 91, 63, 49, 48, 48, 52, 108, 27, 91, 63, 49, 48, 49, 54, 108, 27, 91, 63, 49, 48, 48, 54, 108, 27, 91, 63, 49, 48, 49, 53, 108, 27, 91, 63, 49, 48, 48, 51, 108, 27, 91, 63, 49, 48, 48, 50, 108, 27, 91, 63, 49, 48, 48, 48, 108, 27, 91, 60, 49, 117}

// A real capture of awrit's startup and shutdown, replayed byte for byte. It is
// worth pinning as a whole because each thing it exercises was broken
// separately: the leading S7C1T left a stray "F" on screen, the keyboard query
// went unanswered, and the capability probes were answered with claims this
// terminal could not honor.
func TestAwritStartupLeavesNothingOnScreen(t *testing.T) {
	b := NewBuffer(80, 24, 100)
	b.SetCellPixelSize(10, 20)
	p := NewParser(b)
	var replies []string
	p.SetResponseSink(func(d []byte) { replies = append(replies, string(d)) })

	p.Parse(awritStartup)

	// Nothing may be printed: every byte here is a control sequence.
	for y := 0; y < 24; y++ {
		for x := 0; x < 80; x++ {
			if c := b.GetVisibleCell(x, y); c.Char != 0 && c.Char != ' ' {
				t.Errorf("row %d column %d printed %q; the whole capture is control sequences",
					y, x, c.Char)
			}
		}
	}

	all := strings.Join(replies, "")
	// The keyboard query must be answered — this is the detection an
	// application uses to decide it has extended keyboard support at all.
	if !strings.Contains(all, "\x1b[?") || !strings.Contains(all, "u") {
		t.Errorf("no keyboard-flags reply in %q", replies)
	}
	// The FRAME probe must be accepted. awrit asks it to decide whether to run
	// at all: told no, it exits on the spot without drawing anything, which is
	// what refusing it did. Composition is still refused, and a client can work
	// around that one.
	if n := strings.Count(all, "EINVAL"); n != 1 {
		t.Errorf("%d actions refused, want only the compose probe: %q", n, replies)
	}
	if n := strings.Count(all, "OK"); n < 2 {
		t.Errorf("%d actions accepted, want at least the image and its frame: %q", n, replies)
	}
	// The capture's own image arrives over SHARED MEMORY, naming an object that
	// exists only on the machine it was captured from. Its absence here is the
	// honest answer, and the animation command that follows it correctly
	// reports the image missing rather than inventing one.
	if !strings.Contains(all, "ENOENT") {
		t.Error("the shared-memory image resolved in a test environment that has no such object")
	}
	// It ends by popping the flags it pushed, which must leave none set.
	if got := b.KeyboardFlags(); got != 0 {
		t.Errorf("keyboard flags = %d after the capture's pop, want 0", got)
	}
}
