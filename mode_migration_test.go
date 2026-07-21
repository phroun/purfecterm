package purfecterm

import "testing"

// The width family moved to PurfecTerm's private 7000s block. ?2027 is
// repurposed to its standards-track meaning (grapheme clustering, inherently
// satisfied) and must NOT toggle flex mode; ?7027 is the real flex opt-in,
// and ?7029/?7030 drive ambiguous width.
func TestModeNumberMigration(t *testing.T) {
	b := NewBuffer(10, 3, 100)
	p := NewParser(b)

	// ?2027h is accepted but inherently satisfied: it must not enable flex.
	p.ParseString("\x1b[?2027h")
	if b.IsFlexWidthModeEnabled() {
		t.Fatal("?2027h must not enable flex mode (standards-track, no-op)")
	}

	// ?7027 is the flex opt-in (Contract B).
	p.ParseString("\x1b[?7027h")
	if !b.IsFlexWidthModeEnabled() {
		t.Fatal("?7027h should enable flex mode")
	}
	p.ParseString("\x1b[?7027l")
	if b.IsFlexWidthModeEnabled() {
		t.Fatal("?7027l should disable flex mode")
	}

	// ?7030 / ?7029 drive ambiguous width.
	p.ParseString("\x1b[?7030h")
	if b.GetAmbiguousWidthMode() != AmbiguousWidthWide {
		t.Fatal("?7030h should set ambiguous width wide")
	}
	p.ParseString("\x1b[?7029h")
	if b.GetAmbiguousWidthMode() != AmbiguousWidthNarrow {
		t.Fatal("?7029h should set ambiguous width narrow")
	}
}
