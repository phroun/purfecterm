package purfecterm

import "testing"

// Font slots: SGR 10-20 selects the per-cell font slot written into cells,
// and OSC 7004 configures the terminal's slot -> family map (with unset slots
// inheriting slot 0). ResetAttributes returns to the primary slot.
func TestFontSlots(t *testing.T) {
	b := NewBuffer(20, 3, 100)
	p := NewParser(b)

	// SGR 11 -> font slot 1 on subsequent cells.
	p.ParseString("\x1b[11mA")
	if got := b.GetVisibleCell(0, 0).Font; got != 1 {
		t.Fatalf("SGR 11 should write font slot 1, got %d", got)
	}
	// SGR 20 -> fraktur (slot 10).
	p.ParseString("\x1b[20mB")
	if got := b.GetVisibleCell(1, 0).Font; got != 10 {
		t.Fatalf("SGR 20 should write font slot 10, got %d", got)
	}
	// SGR 10 -> back to the primary face (slot 0).
	p.ParseString("\x1b[10mC")
	if got := b.GetVisibleCell(2, 0).Font; got != 0 {
		t.Fatalf("SGR 10 should reset to font slot 0, got %d", got)
	}

	// ResetAttributes clears the current slot.
	b.SetFont(5)
	b.ResetAttributes()
	if b.GetFont() != 0 {
		t.Fatalf("ResetAttributes should reset font slot to 0, got %d", b.GetFont())
	}

	// OSC 7004 slot -> family mapping, with unset slots inheriting slot 0.
	p.ParseString("\x1b]7004;f;0;PrimaryFace\x07")
	p.ParseString("\x1b]7004;f;1;AltFace\x07")
	if fam := b.GetFontSlot(1); fam != "AltFace" {
		t.Fatalf("slot 1 family = %q, want AltFace", fam)
	}
	if fam := b.GetFontSlot(2); fam != "PrimaryFace" {
		t.Fatalf("unset slot should fall back to slot 0, got %q", fam)
	}
	// Clear one slot -> inherits slot 0 again.
	p.ParseString("\x1b]7004;fd;1\x07")
	if fam := b.GetFontSlot(1); fam != "PrimaryFace" {
		t.Fatalf("cleared slot 1 should inherit slot 0, got %q", fam)
	}
	// Clear all slots.
	p.ParseString("\x1b]7004;fda\x07")
	if fam := b.GetFontSlot(0); fam != "" {
		t.Fatalf("after fda, slot 0 should be empty, got %q", fam)
	}
}
