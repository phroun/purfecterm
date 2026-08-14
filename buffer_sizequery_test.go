package purfecterm

import (
	"fmt"
	"testing"
)

// The size queries answer from the REAL cell size, so a program can size an
// image from them. A terminal that never learned its cell size answers 0, which
// is the honest "unknown" the protocol has no better word for.
func TestPixelSizeQueriesAnswer(t *testing.T) {
	b := NewBuffer(80, 24, 100)
	b.SetCellPixelSize(10, 20)
	b.SetPointerPixelUnit(1000, 1000) // must not leak into these answers

	var got []byte
	p := NewParser(b)
	p.SetResponseSink(func(d []byte) { got = append(got, d...) })

	p.Parse([]byte("\x1b[14t"))
	if want := fmt.Sprintf("\x1b[4;%d;%dt", 24*20, 80*10); string(got) != want {
		t.Errorf("CSI 14 t answered %q, want %q", got, want)
	}

	got = nil
	p.Parse([]byte("\x1b[16t"))
	if want := "\x1b[6;20;10t"; string(got) != want {
		t.Errorf("CSI 16 t answered %q, want %q", got, want)
	}

	got = nil
	p.Parse([]byte("\x1b[18t"))
	if want := "\x1b[8;24;80t"; string(got) != want {
		t.Errorf("CSI 18 t answered %q, want %q", got, want)
	}
}

// With no sink registered the queries are silently dropped rather than
// panicking — which is what happens to a headless buffer nobody wired up.
func TestPixelSizeQueriesWithoutSinkDoNotPanic(t *testing.T) {
	b := NewBuffer(80, 24, 100)
	b.SetCellPixelSize(10, 20)
	NewParser(b).Parse([]byte("\x1b[14t\x1b[16t\x1b[18t"))
}
