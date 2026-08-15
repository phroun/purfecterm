package cli

import (
	"testing"
)

// A control chord carrying a further modifier encodes, rather than vanishing.
//
// direct-key-handler spells Control with a caret against the keys the caret
// suits, so Ctrl+Shift+A is "S-^A" and Alt+Ctrl+A is "M-^A" — the caret holds
// the Control and the prefixes hold the rest. encodeModifiedKey had no branch
// for a caret base: it fell past every table, returned nil, and keyToBytes then
// returned nil too because the name contains a "-". The keystroke went nowhere,
// with nothing logged and nothing sent.
//
// These forms come only from the kitty protocol. A legacy terminal has no way
// to say Ctrl+Shift+A at all, which is also why Shift is dropped here: an ASCII
// control code is five bits with no room for it, so ^A is what the wire carries
// for both, and that is a property of the encoding rather than a choice.
func TestControlChordsWithAModifierAreEncoded(t *testing.T) {
	for _, c := range []struct{ key, want, what string }{
		{"M-^A", "\x1b\x01", "Alt+Ctrl+A: ESC before the control code"},
		{"S-^A", "\x01", "Ctrl+Shift+A: Shift has nowhere to go on the wire"},
		{"M-S-^A", "\x1b\x01", "Alt+Ctrl+Shift+A: Alt survives, Shift does not"},
		{"M-^[", "\x1b\x1b", "Alt+Ctrl+[, whose control code is ESC itself"},
		{"M-^@", "\x1b\x00", "Alt+Ctrl+@: NUL is a real encoding whose byte is zero"},
	} {
		got := keyToBytes(c.key)
		if got == nil {
			t.Errorf("%s (%s): dropped", c.key, c.what)
			continue
		}
		if string(got) != c.want {
			t.Errorf("%s (%s) = %q, want %q", c.key, c.what, string(got), c.want)
		}
	}

	// The unmodified caret chords keep their old answers; extracting controlByte
	// out of keyToBytes must not have changed what they encode to.
	for _, c := range []struct{ key, want string }{
		{"^A", "\x01"}, {"^Z", "\x1a"}, {"^@", "\x00"}, {"^[", "\x1b"},
		{"^\\", "\x1c"}, {"^]", "\x1d"}, {"^^", "\x1e"}, {"^_", "\x1f"},
	} {
		if got := string(keyToBytes(c.key)); got != c.want {
			t.Errorf("%s = %q, want %q", c.key, got, c.want)
		}
	}
}

// Alt+Ctrl+A survives the trip out and back as itself, which is the strongest
// statement available about the branch above: the bytes are not merely
// non-empty, they name the chord that produced them.
//
// Only this one is asserted, and the two that cannot round-trip say why:
//
//	"S-^A" comes back as "^A" — Shift is not in the five bits of a control
//	code, so the loss is in the wire format, not in this encoder.
//
//	"M-^[" comes back as two Escapes, and "M-€" as ESC plus undecoded bytes.
//	Both are what a terminal sends; direct-key-handler's legacy path just does
//	not reassemble ESC-prefixed forms in those two shapes. A guest that does is
//	unaffected, so this is a limit of the decoder, not of the encoding.
func TestAltControlChordRoundTrips(t *testing.T) {
	out := keyToBytes("M-^A")
	if got := decodeBack(t, out); len(got) != 1 || got[0] != "M-^A" {
		t.Errorf("M-^A encoded %q, which decodes as %v, want [M-^A]", string(out), got)
	}
}

// Alt with a character outside ASCII encodes, rather than vanishing.
//
// The branch guarded on len(baseKey) == 1 in BYTES, so "M-€" — one character in
// three bytes — missed it and was dropped. Runes are the unit a keystroke is
// counted in.
func TestAltWithAMultiByteCharacter(t *testing.T) {
	for _, c := range []struct{ key, want string }{
		{"M-€", "\x1b€"},
		{"M-é", "\x1bé"},
		{"M-a", "\x1ba"}, // the ASCII case, which already worked
	} {
		got := keyToBytes(c.key)
		if got == nil {
			t.Errorf("%s: dropped", c.key)
			continue
		}
		if string(got) != c.want {
			t.Errorf("%s = %q, want %q", c.key, string(got), c.want)
		}
	}
}

// The Alt bit is read out of the bitmask, not out of the xterm code.
//
// An xterm modifier code is 1 + (Shift 1 | Alt 2 | Ctrl 4). The old test was
// mod&2, which is the Alt bit only before the +1: code 2 is Shift alone and
// answered "has Alt", so a Shift-modified character was encoded as an Alt one.
//
// Nothing reaches this today — direct-key-handler carries Shift in a
// character's own case and never sends "S-a" — so this asserts against
// encodeModifiedKey directly rather than pretending the name arrives.
func TestAltBitComesFromTheModifierBitmask(t *testing.T) {
	for _, c := range []struct {
		mod  int
		want string
		what string
	}{
		{2, "", "Shift alone: not Alt, and there is nothing else to send"},
		{3, "\x1ba", "Alt alone"},
		{4, "\x1ba", "Shift+Alt"},
		{5, "", "Ctrl alone: arrives as a caret chord instead, never here"},
		{6, "", "Shift+Ctrl: likewise"},
		{7, "\x1ba", "Alt+Ctrl"},
		{8, "\x1ba", "Shift+Alt+Ctrl"},
	} {
		got := encodeModifiedKey(c.mod, "a")
		if c.want == "" {
			if got != nil {
				t.Errorf("mod %d (%s) = %q, want nil", c.mod, c.what, string(got))
			}
			continue
		}
		if string(got) != c.want {
			t.Errorf("mod %d (%s) = %q, want %q", c.mod, c.what, string(got), c.want)
		}
	}
}

// An event suffix decides whether there is a keystroke to send at all.
//
// A release and a modifier's side are things a legacy PTY cannot express, so
// they send nothing. A repeat is a keystroke — a terminal sends the character
// again — so the suffix comes off and the key underneath encodes as itself.
// Previously the suffix rode into the lookups, matched nothing, and the whole
// decorated name was sent to the guest as text.
func TestEventSuffixes(t *testing.T) {
	for _, c := range []struct {
		key  string
		want string
		what string
	}{
		{"a:Release", "", "a key coming up sends nothing"},
		{"Return:Release", "", "and that holds for named keys too"},
		{"S-:Left", "", "which of a paired modifier key it was"},
		{"S-:Right", "", "likewise"},
		{"a:Repeat", "a", "an auto-repeat is another keystroke"},
		{"M-a:Repeat", "\x1ba", "including a modified one"},
		{"Return:Repeat", "\r", "and a named one"},
	} {
		got := keyToBytes(c.key)
		if c.want == "" {
			if got != nil {
				t.Errorf("%s (%s) = %q, want nil", c.key, c.what, string(got))
			}
			continue
		}
		if string(got) != c.want {
			t.Errorf("%s (%s) = %q, want %q", c.key, c.what, string(got), c.want)
		}
	}
}
