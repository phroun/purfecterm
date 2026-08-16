package cli

import (
	"testing"
)

// A control chord carrying a further modifier encodes, rather than vanishing.
//
// direct-key-handler spells Control with a caret against the keys the caret
// suits, so Ctrl+Shift+A is "S-^A" and Mega+Ctrl+A is "M-^A" — the caret holds
// the Control and the prefixes hold the rest. encodeModifiedKey had no branch
// for a caret base: it fell past every table, returned nil, and keyToBytes then
// returned nil too because the name contains a "-". The keystroke went nowhere,
// with nothing logged and nothing sent.
//
// These forms come only from the kitty protocol. Shift is dropped on the way
// out because the LEGACY encoding has no room for it — an ASCII control code is
// five bits — so ^A is what that wire carries for both chords.
//
// That is a limit of the encoding chosen here, not of the chord. The kitty
// protocol says Ctrl+Shift+A perfectly well, as CSI 97;6u, which is how it
// arrived. This encoder emits the legacy form unconditionally because it
// consults none of the flags keyboard_protocol.go negotiates, so the loss is
// real today and is not permanent.
func TestControlChordsWithAModifierAreEncoded(t *testing.T) {
	for _, c := range []struct{ key, want, what string }{
		{"M-^A", "\x1b\x01", "Mega+Ctrl+A: ESC before the control code"},
		{"S-^A", "\x01", "Ctrl+Shift+A: Shift has nowhere to go on the wire"},
		{"M-S-^A", "\x1b\x01", "Mega+Ctrl+Shift+A: Mega survives, Shift does not"},
		{"M-^[", "\x1b\x1b", "Mega+Ctrl+[, whose control code is ESC itself"},
		{"M-^@", "\x1b\x00", "Mega+Ctrl+@: NUL is a real encoding whose byte is zero"},
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

// Mega+Ctrl+A survives the trip out and back as itself, which is the strongest
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
func TestMegaControlChordRoundTrips(t *testing.T) {
	out := keyToBytes("M-^A")
	if got := decodeBack(t, out); len(got) != 1 || got[0] != "M-^A" {
		t.Errorf("M-^A encoded %q, which decodes as %v, want [M-^A]", string(out), got)
	}
}

// Mega with a character outside ASCII encodes, rather than vanishing.
//
// The branch guarded on len(baseKey) == 1 in BYTES, so "M-€" — one character in
// three bytes — missed it and was dropped. Runes are the unit a keystroke is
// counted in.
func TestMegaWithAMultiByteCharacter(t *testing.T) {
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

// The Mega bit is read out of the bitmask, not out of the xterm code.
//
// An xterm modifier code is 1 + (Shift 1 | Mega 2 | Ctrl 4). The old test was
// mod&2, which is the Mega bit only before the +1: code 2 is Shift alone and
// answered "has Mega", so a Shift-modified character was encoded as a Mega one.
//
// Nothing reaches this today — direct-key-handler carries Shift in a
// character's own case and never sends "S-a" — so this asserts against
// encodeModifiedKey directly rather than pretending the name arrives.
func TestMegaBitComesFromTheModifierBitmask(t *testing.T) {
	for _, c := range []struct {
		mod  int
		want string
		what string
	}{
		{2, "", "Shift alone: not Mega, and there is nothing else to send"},
		{3, "\x1ba", "Mega alone"},
		{4, "\x1ba", "Shift+Mega"},
		{5, "", "Ctrl alone: arrives as a caret chord instead, never here"},
		{6, "", "Shift+Ctrl: likewise"},
		{7, "\x1ba", "Mega+Ctrl"},
		{8, "\x1ba", "Shift+Mega+Ctrl"},
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

// No key name encodes to nothing.
//
// This is the rule the rest of this file is a special case of. Whatever a
// key name is — a chord this encoder cannot build, an event it cannot express,
// a modifier prefix it does not parse, a key from a protocol level it does not
// speak — it produces bytes, and if there is no encoding those bytes are the
// name in angle brackets.
//
// The rule exists because the alternative is an encoder deciding, from its own
// inability to spell something, that the keypress did not occur. That decision
// is not this layer's to make: direct-key-handler emits what it emits because
// it was built to, and a consumer that cannot represent a token has a gap, not
// a licence to discard it. A gap belongs at the guest where somebody can see
// it.
func TestNoKeyNameEncodesToNothing(t *testing.T) {
	for _, key := range []string{
		"C-Nonsense", // a modified chord with no encoding
		"M-F13",      // a modified function key this encoder lacks
		"C-CapsLock", // a modified lock key
		"s-a",        // Super, a prefix parseModifiers does not read
		"m-a",        // Micro, likewise
		"G-abc",      // a glyph chord with no single-rune payload
		"^AB",        // a caret name that is not a control chord
		"F13",        // a plain key with no encoding
		"CapsLock",   // a plain lock key
	} {
		if keyToBytes(key) == nil {
			t.Errorf("%s encodes to nothing; an unencodable key must stay visible "+
				"at the guest, not vanish", key)
		}
	}
}

// On the legacy path a REPEAT is the press it has always been, and a release is
// nothing at all.
//
// The two differ because a repeat has a legacy form and a release does not. A
// held key on a terminal that never heard of this protocol arrives as another
// press, so that is what a guest which negotiated nothing must receive —
// sending it nothing instead makes a held key stop dead after its first press,
// which is what this used to assert. A release has no such fallback: there is
// no legacy way to say "came up", and one delivered to a guest that cannot read
// it looks like the keystroke arriving twice.
//
// Neither answer is a judgement that a release is not a key. The release is
// real and expressible: cli/keyboard_kitty.go writes it, and a guest that
// pushed KeyboardReportEvents receives it. Control reaches keyToBytes only when
// the kitty path declined, and the protocol's rule for declining is this.
//
// Bracketing them, which this file asserted before that, sent the guest
// "<a:Release>" as literal text on every key it let go.
func TestLegacyPathRepeatsPressesAndDropsReleases(t *testing.T) {
	for _, tc := range []struct{ key, want string }{
		{"a:Repeat", "a"},
		{"M-a:Repeat", "\x1ba"},
		{"Up:Repeat", "\x1b[A"},
		{"Return:Repeat", "\r"},
	} {
		if got := string(keyToBytes(tc.key)); got != tc.want {
			t.Errorf("%s produced %q on the legacy path, want %q — a held key must "+
				"keep repeating for a guest that cannot read event types",
				tc.key, got, tc.want)
		}
	}
	for _, key := range []string{"a:Release", "Return:Release", "S-:Left"} {
		if got := keyToBytes(key); got != nil {
			t.Errorf("%s produced %q on the legacy path; a guest that negotiated "+
				"no event reporting must receive nothing", key, string(got))
		}
	}
}
