package purfecterm

import "testing"

func respParser(b *Buffer) (*Parser, *string) {
	p := NewParser(b)
	var resp string
	p.SetResponseSink(func(bb []byte) { resp += string(bb) })
	return p, &resp
}

// Device Attributes: primary, secondary, tertiary all reply.
func TestDeviceAttributes(t *testing.T) {
	b := NewBuffer(20, 5, 100)
	p, resp := respParser(b)

	p.Parse([]byte("\x1b[c")) // Primary DA
	if *resp != "\x1b[?62;1;6;22c" {
		t.Fatalf("primary DA = %q", *resp)
	}
	*resp = ""
	p.Parse([]byte("\x1b[>c")) // Secondary DA
	if *resp != "\x1b[>1;10;0c" {
		t.Fatalf("secondary DA = %q", *resp)
	}
	*resp = ""
	p.Parse([]byte("\x1b[=c")) // Tertiary DA
	if *resp != "\x1bP!|00000000\x1b\\" {
		t.Fatalf("tertiary DA = %q", *resp)
	}
}

// DSR: status report and cursor position report (visual column, origin-relative
// row).
func TestDeviceStatusReport(t *testing.T) {
	b := NewBuffer(20, 10, 100)
	p, resp := respParser(b)

	p.Parse([]byte("\x1b[5n")) // operating status
	if *resp != "\x1b[0n" {
		t.Fatalf("DSR 5n = %q", *resp)
	}
	*resp = ""
	p.Parse([]byte("\x1b[3;7H\x1b[6n")) // CPR at row 3 col 7
	if *resp != "\x1b[3;7R" {
		t.Fatalf("CPR = %q, want \\e[3;7R", *resp)
	}
	*resp = ""
	// Origin mode: CPR row is relative to the top margin.
	p.Parse([]byte("\x1b[2;6r\x1b[?6h\x1b[6n"))
	if *resp != "\x1b[1;1R" {
		t.Fatalf("origin CPR = %q, want \\e[1;1R", *resp)
	}
}

// IRM (insert mode) shifts the rest of the line right as characters are typed.
func TestInsertMode(t *testing.T) {
	b := NewBuffer(10, 2, 100)
	p := NewParser(b)
	p.Parse([]byte("\x1b[1;1HABCDE"))
	p.Parse([]byte("\x1b[1;3H")) // cursor on 'C' (col 2)
	p.Parse([]byte("\x1b[4h"))   // IRM on
	p.Parse([]byte("XY"))

	want := "ABXYCDE"
	for i, w := range want {
		if got := cellChar(b, i, 0); got != w {
			t.Errorf("col%d = %q, want %q", i, got, w)
		}
	}
}

// LNM makes an output line feed also carriage-return.
func TestNewLineMode(t *testing.T) {
	b := NewBuffer(10, 3, 100)
	p := NewParser(b)
	p.Parse([]byte("\x1b[20h"))    // LNM on
	p.Parse([]byte("\x1b[1;5H\n")) // cursor col4, LF
	cursorAt(t, b, 0, 1)
}

// Tab stops: default every-8, CBT/CHT, and a custom HTS stop after TBC-all.
func TestTabStops(t *testing.T) {
	b := NewBuffer(40, 2, 100)
	p := NewParser(b)
	p.Parse([]byte("\x1b[1;1H\t")) // -> 8
	if x, _ := b.GetCursor(); x != 8 {
		t.Fatalf("HT -> %d, want 8", x)
	}
	p.Parse([]byte("\t")) // -> 16
	if x, _ := b.GetCursor(); x != 16 {
		t.Fatalf("HT -> %d, want 16", x)
	}
	p.Parse([]byte("\x1b[Z")) // CBT -> 8
	if x, _ := b.GetCursor(); x != 8 {
		t.Fatalf("CBT -> %d, want 8", x)
	}
	p.Parse([]byte("\x1b[2I")) // CHT 2 -> 24
	if x, _ := b.GetCursor(); x != 24 {
		t.Fatalf("CHT 2 -> %d, want 24", x)
	}
	// Clear all stops, set a custom one at column 5.
	p.Parse([]byte("\x1b[3g\x1b[1;6H\x1bH\x1b[1;1H\t"))
	if x, _ := b.GetCursor(); x != 5 {
		t.Fatalf("custom HTS stop -> %d, want 5", x)
	}
}

// REP repeats the last printed character.
func TestRepeatChar(t *testing.T) {
	b := NewBuffer(10, 2, 100)
	p := NewParser(b)
	p.Parse([]byte("\x1b[1;1HA\x1b[3b")) // A then REP 3
	for x := 0; x < 4; x++ {
		if got := cellChar(b, x, 0); got != 'A' {
			t.Errorf("col%d = %q, want A", x, got)
		}
	}
}

// HPA/HPR/VPR position the cursor.
func TestPositionSequences(t *testing.T) {
	b := NewBuffer(20, 10, 100)
	p := NewParser(b)
	p.Parse([]byte("\x1b[1;1H\x1b[10`")) // HPA col 10 -> x=9
	if x, _ := b.GetCursor(); x != 9 {
		t.Fatalf("HPA -> %d, want 9", x)
	}
	p.Parse([]byte("\x1b[3a")) // HPR +3 -> 12
	if x, _ := b.GetCursor(); x != 12 {
		t.Fatalf("HPR -> %d, want 12", x)
	}
	p.Parse([]byte("\x1b[5e")) // VPR +5 -> row 5
	if _, y := b.GetCursor(); y != 5 {
		t.Fatalf("VPR -> %d, want 5", y)
	}
}

// Keyboard/interaction modes are tracked, reportable via DECRQM, and reset by RIS.
func TestKeyModes(t *testing.T) {
	b := NewBuffer(20, 5, 100)
	p, resp := respParser(b)

	p.Parse([]byte("\x1b[?1h")) // DECCKM
	if !b.IsApplicationCursorKeys() {
		t.Fatal("DECCKM should be on")
	}
	p.Parse([]byte("\x1b[?1$p")) // DECRQM query
	if *resp != "\x1b[?1;1$y" {
		t.Fatalf("DECRQM ?1 = %q, want \\e[?1;1$y", *resp)
	}

	p.Parse([]byte("\x1b=")) // DECKPAM
	if !b.IsApplicationKeypad() {
		t.Fatal("application keypad should be on")
	}
	p.Parse([]byte("\x1b>")) // DECKPNM
	if b.IsApplicationKeypad() {
		t.Fatal("application keypad should be off")
	}

	p.Parse([]byte("\x1b[?1004h")) // focus reporting
	if string(b.FocusReportSequence(true)) != "\x1b[I" {
		t.Fatal("focus-in sequence wrong")
	}
	if string(b.FocusReportSequence(false)) != "\x1b[O" {
		t.Fatal("focus-out sequence wrong")
	}

	p.Parse([]byte("\x1b[?1007h")) // alternate scroll
	if !b.IsAltScrollMode() {
		t.Fatal("alt scroll should be on")
	}

	p.Parse([]byte("\x1bc")) // RIS
	if b.IsApplicationCursorKeys() || b.IsFocusReporting() || b.IsAltScrollMode() || b.IsApplicationKeypad() {
		t.Fatal("RIS should reset key modes")
	}
}
