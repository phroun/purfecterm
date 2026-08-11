package cli

import "testing"

// A "G-"-prefixed key (the private Glyph / AltGr-Level3 modifier) encodes to a
// kitty CSI-u sequence carrying the produced glyph's codepoint and the Glyph
// modifier bit (value 256, sent 1-indexed as 257), so a kitty-aware child sees
// a distinct chord instead of an anonymous typed character.
func TestGlyphKeyToBytes(t *testing.T) {
	cases := []struct {
		key  string
		want string
	}{
		{"G-€", "\x1b[8364;257u"}, // € = U+20AC = 8364
		{"G-@", "\x1b[64;257u"},   // a Glyph-composed ASCII char still round-trips
		{"G-é", "\x1b[233;257u"},  // é = U+00E9 = 233
	}
	for _, c := range cases {
		got := string(keyToBytes(c.key))
		if got != c.want {
			t.Errorf("keyToBytes(%q) = %q, want %q", c.key, got, c.want)
		}
	}
}

// A plain composed glyph with no Glyph marker still sends raw UTF-8 (the
// multi-byte fallback), so ordinary AltGr typing is unchanged when a host does
// not mark it.
func TestPlainGlyphUnchanged(t *testing.T) {
	if got := string(keyToBytes("€")); got != "€" {
		t.Errorf("keyToBytes(\"€\") = %q, want %q", got, "€")
	}
}
