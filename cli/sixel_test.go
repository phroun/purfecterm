package cli

import (
	"strings"
	"testing"
)

// The CLI renderer passes a placed Sixel image through to the host by re-emitting
// its raw DCS at the anchor cell.
func TestCLISixelPassthrough(t *testing.T) {
	term, err := New(Options{Cols: 20, Rows: 10, Embedded: true})
	if err != nil {
		t.Fatal(err)
	}
	term.Buffer().SetCellPixelSize(10, 20)
	term.FeedString("\x1bPq#1;2;100;0;0~\x1b\\")

	out := NewRenderer(term).RenderToString()
	if !strings.Contains(out, "\x1bPq#1;2;100;0;0~\x1b\\") {
		t.Fatalf("expected raw Sixel passthrough in render output, got %q", out)
	}
}
