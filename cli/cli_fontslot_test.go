package cli

import "testing"

// The per-cell font slot (SGR 10-20) must survive into RenderedCell.Font so a
// parent compositor driving the CLI adapter can honor it.
func TestCLIFontSlotSurvivesGetCells(t *testing.T) {
	term, err := New(Options{Cols: 10, Rows: 2, Embedded: true})
	if err != nil {
		t.Fatal(err)
	}
	// OSC 7004 configures slot 2, SGR 12 selects it, then paint 'Z'.
	term.FeedString("\x1b]7004;f;2;Comic Mono\x07\x1b[12mZ")

	cells := term.GetCells()
	if len(cells) == 0 || len(cells[0]) == 0 {
		t.Fatal("no cells rendered")
	}
	if cells[0][0].Char != 'Z' {
		t.Fatalf("cell (0,0) char = %q, want Z", cells[0][0].Char)
	}
	if cells[0][0].Font != 2 {
		t.Fatalf("cell (0,0) font slot = %d, want 2", cells[0][0].Font)
	}
}
