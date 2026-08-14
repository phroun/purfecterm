package cli

import "testing"

// DECCKM (?1) makes the CLI adapter encode unmodified cursor keys as SS3.
func TestCLIApplicationCursorKeys(t *testing.T) {
	term, err := New(Options{Cols: 10, Rows: 3, Embedded: true})
	if err != nil {
		t.Fatal(err)
	}
	var got []byte
	term.SetInputCallback(func(b []byte) bool { got = append(got, b...); return true })
	term.SetFocused(true)

	term.HandleKeyString("Up")
	if string(got) != "\x1b[A" {
		t.Fatalf("normal-mode Up = %q, want ESC [ A", got)
	}

	term.FeedString("\x1b[?1h") // DECCKM on
	got = nil
	term.HandleKeyString("Up")
	if string(got) != "\x1bOA" {
		t.Fatalf("application-mode Up = %q, want ESC O A", got)
	}
	got = nil
	term.HandleKeyString("Left")
	if string(got) != "\x1bOD" {
		t.Fatalf("application-mode Left = %q, want ESC O D", got)
	}

	term.FeedString("\x1b[?1l") // DECCKM off
	got = nil
	term.HandleKeyString("Up")
	if string(got) != "\x1b[A" {
		t.Fatalf("after reset, Up = %q, want ESC [ A", got)
	}
}
