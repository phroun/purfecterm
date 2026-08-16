package cli

import (
	"testing"

	"github.com/phroun/direct-key-handler/keyboard"
	"github.com/phroun/purfecterm"
)

const reportEvents = purfecterm.KeyboardDisambiguate | purfecterm.KeyboardReportEvents

// A key release reaches the guest once it has asked for release events.
//
// This is the whole point of the file. The legacy encoding has no way to say a
// key came UP — no ESC prefix, no control code, nothing — so before this path
// existed a guest that negotiated KeyboardReportEvents still received presses
// only. A browser tracking a held key had no idea it was ever let go.
func TestReleaseReachesAGuestThatAskedForIt(t *testing.T) {
	for _, c := range []struct{ key, want, what string }{
		{"a:Release", "\x1b[97;1:3u", "a plain letter coming up"},
		{"a", "a", "and the press it pairs with: a plain text key stays legacy\n\t\t\t// until report-all-keys is asked for"},
		{"a:Repeat", "\x1b[97;1:2u", "auto-repeat, the other event type"},
		{"Return:Release", "\x1b[13;1:3u", "a named key coming up"},
		{"^A:Release", "\x1b[97;5:3u", "a Control chord coming up: base letter, Ctrl bit"},
		{"M-a:Release", "\x1b[97;3:3u", "Mega held, key coming up"},
	} {
		got := encodeKittyKeyName(c.key, reportEvents)
		if got == nil {
			t.Errorf("%s (%s): encoded to nothing", c.key, c.what)
			continue
		}
		if string(got) != c.want {
			t.Errorf("%s (%s) = %q, want %q", c.key, c.what, string(got), c.want)
		}
	}
}

// A guest that never asked gets nothing new.
//
// Sending release events to an application that did not negotiate them is how
// a keystroke appears to arrive twice, so silence here is the correct answer
// and the legacy encoder stays the only path in play.
func TestNothingChangesForAGuestThatAskedForNothing(t *testing.T) {
	for _, key := range []string{"a", "Return", "^A", "M-a", "a:Release"} {
		if got := encodeKittyKeyName(key, 0); got != nil {
			t.Errorf("%s: with no flags negotiated, produced %q; the legacy "+
				"encoder must remain the only path", key, string(got))
		}
	}

	// And with disambiguation alone — no event reporting — a release still
	// sends nothing, because EncodeKeyEvent drops what was not asked for.
	if got := encodeKittyKeyName("a:Release", purfecterm.KeyboardDisambiguate); len(got) != 0 {
		t.Errorf("a:Release under disambiguation-only = %q, want nothing", string(got))
	}
}

// What this encoder writes, direct-key-handler reads back as the same key.
//
// Asserting the bytes alone would only say this file agrees with itself. The
// round trip says a guest can recover which key was struck, which is the
// property that matters — and it is the same check the erase and Enter keys
// get on the legacy path.
func TestKittyEncodingRoundTrips(t *testing.T) {
	for _, key := range []string{
		"a", "Z", "Return", "Tab", "Escape", "Up", "Down", "Home", "End",
		"F1", "F5", "F12", "Insert", "FDel", "PageUp", "PageDown",
		"^A", "M-a", "S-Up", "C-Home",
	} {
		out := encodeKittyKeyName(key, reportEvents)
		if out == nil {
			t.Errorf("%s: encoded to nothing", key)
			continue
		}
		got := decodeBack(t, out)
		if len(got) != 1 || got[0] != key {
			t.Errorf("%s encoded %q, which decodes as %v, want [%s]",
				key, string(out), got, key)
		}
	}
}

// The two Enter keys still encode as the protocol says, which means alike.
//
// The legacy path keeps them apart with CR and SS3 M, because that is the only
// way the legacy wire can. The protocol assigns Return and the keypad's Enter
// the same code, so encoding them alike here is agreement with the spec rather
// than the conflation the legacy split exists to prevent.
func TestBothEnterKeysUseTheProtocolCode(t *testing.T) {
	ret := encodeKittyKeyName("Return", reportEvents)
	ent := encodeKittyKeyName("Enter", reportEvents)
	if ret == nil || ent == nil {
		t.Fatalf("Return=%q Enter=%q, want both encoded", string(ret), string(ent))
	}
	if want := "\x1b[13u"; string(ret) != want {
		t.Errorf("Return = %q, want %q", string(ret), want)
	}
	if string(ret) != string(ent) {
		t.Errorf("Return %q and Enter %q differ; the protocol gives them one code",
			string(ret), string(ent))
	}
}

// A key with no protocol code falls back rather than guessing one.
//
// The locks, the system keys and F13 up have no codepoint this package knows.
// Inventing one would put a wrong key on the wire, which is worse than the
// legacy path's visible "<F13>".
func TestUnknownKeysFallBackInsteadOfGuessing(t *testing.T) {
	for _, key := range []string{"F13", "CapsLock", "NumLock", "Menu", "Nonsense"} {
		if got := encodeKittyKeyName(key, reportEvents); got != nil {
			t.Errorf("%s produced %q; it has no protocol code and must fall back",
				key, string(got))
		}
	}
}

// Every key this encoder claims to handle is one direct-key-handler can emit.
//
// Keyed by keyboard.Key, so a rename upstream cannot silently orphan an entry —
// but a key REMOVED upstream would stop compiling, and a key whose name this
// table never sees would go unnoticed. This walks the real list.
func TestFunctionalTableNamesRealKeys(t *testing.T) {
	known := make(map[keyboard.Key]bool)
	for _, k := range keyboard.AllKeys() {
		known[k] = true
	}
	for k := range kittyFunctionalKeys {
		if !known[k] {
			t.Errorf("%v is in the kitty table but is not a key direct-key-handler emits", k)
		}
	}
}
