package cli

import "testing"

// The three erase names encode as a terminal sends them.
//
// direct-key-handler names BS (8) and DEL (127) apart on the way IN because it
// cannot know which one a terminal will send for its backspace — terminfo
// still answers both ways, kbs=^H for vt100/vt220/ansi and kbs=^? for xterm,
// linux, screen, tmux and rxvt. Going OUT there is no such ambiguity: both
// erase behind the cursor, and what a terminal sends for that is DEL.
//
// Emitting BS instead is not a harmless difference. A guest that maps terminal
// input to real key events reads 0x08 as Ctrl-H, and a browser has no binding
// for Ctrl-H, so the keystroke vanishes — which is exactly how a hosted
// browser ends up with no working backspace at all. readline and vim forgive
// it because terminfo often maps erase to ^H; a browser does not.
func TestEraseKeysEncodeAsATerminalSendsThem(t *testing.T) {
	for _, c := range []struct{ key, want, what string }{
		{"Backspace", "\x7f", "BS's name still sends DEL, as terminals do"},
		{"Delete", "\x7f", "DEL's name is the same key and encodes alike"},
		{"FDel", "\x1b[3~", "forward delete is the only one that erases ahead"},
	} {
		if got := string(keyToBytes(c.key)); got != c.want {
			t.Errorf("%s: keyToBytes(%q) = %q, want %q", c.what, c.key, got, c.want)
		}
	}

	// Forward delete must not collide with either erase-behind key: a guest
	// told to erase ahead when the user erased behind loses the wrong text.
	if keyToBytes("FDel") == nil {
		t.Fatal("FDel is unmapped, so it would reach the guest as literal text")
	}
	if string(keyToBytes("FDel")) == string(keyToBytes("Backspace")) {
		t.Error("forward delete and backspace encode alike; the guest cannot tell " +
			"which direction to erase")
	}
}

// An unmapped name is not an error here — it still goes out — but it goes out
// bracketed, so it cannot be mistaken for text the user typed.
func TestUnknownKeyNameGoesOutBracketed(t *testing.T) {
	if got := string(keyToBytes("Nonsense")); got != "<Nonsense>" {
		t.Errorf("keyToBytes(%q) = %q, want %q", "Nonsense", got, "<Nonsense>")
	}

	// A single rune is a typed character, not a name, and must stay bare —
	// bracketing it would put angle brackets around every non-ASCII keystroke.
	if got := string(keyToBytes("é")); got != "é" {
		t.Errorf("keyToBytes(%q) = %q; a typed character was treated as a key name",
			"é", got)
	}

	// A modified chord this encoder cannot build is bracketed too. It used to
	// return nil and send nothing, justified by nil meaning "not consumed" —
	// but no caller reads that bool, so the chord simply vanished.
	if got := string(keyToBytes("C-Nonsense")); got != "<C-Nonsense>" {
		t.Errorf("keyToBytes(%q) = %q, want %q", "C-Nonsense", got, "<C-Nonsense>")
	}
}

// Modified erase keys take the same branch, both names alike.
func TestModifiedEraseKeys(t *testing.T) {
	for _, c := range []struct{ key, want, what string }{
		{"M-Backspace", "\x1b\x7f", "Alt+Backspace"},
		{"M-Delete", "\x1b\x7f", "Alt+Delete, the same key"},
		{"C-Backspace", "\x08", "Ctrl+Backspace is where BS legitimately goes out"},
		{"C-Delete", "\x08", "Ctrl+Delete, the same key"},
	} {
		if got := string(keyToBytes(c.key)); got != c.want {
			t.Errorf("%s: keyToBytes(%q) = %q, want %q", c.what, c.key, got, c.want)
		}
	}
}
