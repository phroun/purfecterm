package cli

import (
	"bytes"
	"testing"
	"time"

	"github.com/phroun/direct-key-handler/keyboard"
)

// decodeBack runs bytes this encoder produced back through direct-key-handler
// and returns the key names it read out of them.
//
// The round trip is the real test. Asserting the bytes alone would only say
// this file agrees with itself; feeding them to the decoder says a guest can
// recover which key was struck, which is the entire property at issue.
func decodeBack(t *testing.T, raw []byte) []string {
	t.Helper()
	h := keyboard.New(keyboard.Options{InputReader: bytes.NewReader(raw)})
	if err := h.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer h.Stop()

	var got []string
	deadline := time.After(2 * time.Second)
	for {
		select {
		case k := <-h.Keys:
			got = append(got, k)
		case <-time.After(150 * time.Millisecond):
			return got // the stream has gone quiet
		case <-deadline:
			t.Fatalf("decoding %q did not settle", raw)
			return got
		}
	}
}

// The two Enter keys must survive the trip out and back as two keys.
//
// The home-row key sends CR. The keypad's sends SS3 M — what a terminal in
// application keypad mode (DECKPAM) sends for it, and the only encoding that
// distinguishes the two at all. In numeric keypad mode a terminal sends CR for
// both, and a guest handed CR cannot tell which was struck; this encoder
// treats the keypad as being in application mode always, so the distinction
// wins over the mode.
func TestEnterKeysRoundTripAsTwoKeys(t *testing.T) {
	// The keypad's key wears the pad prefix now — "P-Enter" — because the pad
	// duplicates keys that exist elsewhere and the prefix says which was
	// struck. Both spellings encode: the prefixed one is what arrives today,
	// and the bare one is still what the key table calls it.
	for _, c := range []struct{ key, want string }{
		{"Return", "Return"},
		{"Enter", "P-Enter"},
		{"P-Enter", "P-Enter"},
	} {
		out := keyToBytes(c.key)
		if out == nil {
			t.Errorf("%s: encoded to nothing", c.key)
			continue
		}
		got := decodeBack(t, out)
		if len(got) != 1 || got[0] != c.want {
			t.Errorf("%s encoded %q, which decodes as %v, want [%s]",
				c.key, string(out), got, c.want)
		}
	}

	// And they are not the same bytes, which is what the round trip depends on.
	if bytes.Equal(keyToBytes("Return"), keyToBytes("Enter")) {
		t.Error("the home-row and keypad Enter keys encode identically; a guest " +
			"cannot tell them apart, and neither can this package reading back")
	}
}

// Neither key may take the unknown-name path, which is what an unmapped name
// does here — the failure that made plain Return send its own six letters.
//
// The keys are named through direct-key-handler's constants rather than as
// literals, so this asks the question the regression actually posed: does this
// encoder have bytes for the key upstream calls KeyReturn, whatever upstream
// spells it today? Written as "Return" the test would go green again the next
// time the word moves to another key.
func TestEnterKeysAreMappedNotTyped(t *testing.T) {
	for _, k := range []keyboard.Key{keyboard.KeyReturn, keyboard.KeyKeypadEnter} {
		name := k.DefaultName()
		if got := keyToBytes(name); bytes.Equal(got, unknownKeyBytes(name)) {
			t.Errorf("%v (emits %q) is unmapped: it encodes to %q, the unknown-key "+
				"form, instead of the bytes a terminal sends for it",
				k, name, string(got))
		}
	}
}

// Modified forms keep the same split rather than collapsing onto CR, and the
// modified home-row key must not go silent: a nil result sends nothing at all,
// which reads as the terminal lacking the chord rather than as a bug.
func TestModifiedEnterKeysStaySplitAndAudible(t *testing.T) {
	for _, c := range []struct{ key, want, what string }{
		{"M-Return", "\x1b\r", "Mega + home-row key"},
		{"C-Return", "\r", "Ctrl + home-row key"},
		{"M-Enter", "\x1b\x1bOM", "Mega + keypad key"},
		{"C-Enter", "\x1bOM", "Ctrl + keypad key"},
	} {
		got := keyToBytes(c.key)
		if got == nil {
			t.Errorf("%s (%s): encoded to nothing; the keystroke is dropped "+
				"silently", c.key, c.what)
			continue
		}
		if string(got) != c.want {
			t.Errorf("%s (%s) = %q, want %q", c.key, c.what, string(got), c.want)
		}
	}

	// The modified pairs stay distinct too.
	if bytes.Equal(keyToBytes("M-Return"), keyToBytes("M-Enter")) {
		t.Error("Mega on the two Enter keys encodes identically")
	}
}
