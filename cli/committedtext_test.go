package cli

import (
	"bytes"
	"testing"

	"github.com/phroun/direct-key-handler/keyboard"
	"github.com/phroun/purfecterm"
)

// An input method's commit reaches the guest as TEXT.
//
// direct-key-handler marks text with no key behind it with the "Text:" prefix,
// because a bare name means a key was pressed and none was. Nothing here knew
// the prefix, so every commit fell through to unknownKeyBytes and the guest was
// sent "<Text:ö>" — literal angle brackets, into its document.
//
// A guest that asked for associated text gets the protocol's own form for this,
// keycode 0, which it defines as "no known key is associated with the text".
func TestACommitReachesAGuestThatAskedAsKeycodeZero(t *testing.T) {
	flags := purfecterm.KeyboardDisambiguate | purfecterm.KeyboardReportEvents |
		purfecterm.KeyboardReportAllKeys | purfecterm.KeyboardReportText

	got := encodeKittyKeyName(keyboard.TextPrefix+"ö", flags)
	if want := []byte("\x1b[0;1;246u"); !bytes.Equal(got, want) {
		t.Errorf("a commit encoded %q, want %q", string(got), string(want))
	}
}

// kitty packs a commit and the key that dismissed the palette into one event,
// so the payload is any length and every codepoint has to survive.
func TestASeveralRuneCommitKeepsEveryCodepoint(t *testing.T) {
	flags := purfecterm.KeyboardReportAllKeys | purfecterm.KeyboardReportText

	got := encodeKittyKeyName(keyboard.TextPrefix+"ö,", flags)
	if want := []byte("\x1b[0;1;246:44u"); !bytes.Equal(got, want) {
		t.Errorf("a two-rune commit encoded %q, want %q", string(got), string(want))
	}
}

// A guest that never asked for associated text gets the characters themselves,
// which is what it has always been sent for typed text and the one thing every
// guest understands.
//
// It must NOT get keycode 0 with no text section: that says nothing at all, and
// the commit would vanish on the way through.
func TestACommitReachesAnOlderGuestAsItsOwnCharacters(t *testing.T) {
	if got := encodeKittyKeyName(keyboard.TextPrefix+"ö", purfecterm.KeyboardDisambiguate); got != nil {
		t.Errorf("a guest that asked for no text got %q, want the legacy path", string(got))
	}
	if got := keyToBytes(keyboard.TextPrefix + "ö"); !bytes.Equal(got, []byte("ö")) {
		t.Errorf("the legacy encoding of a commit is %q, want the characters", string(got))
	}
}

// The payload is never read as a name. A commit is arbitrary text, so one that
// happens to contain an event marker is still text and must not be truncated to
// the part before it.
func TestACommitIsNotReadAsANameWearingAMarker(t *testing.T) {
	if got := keyToBytes(keyboard.TextPrefix + "a:Release"); !bytes.Equal(got, []byte("a:Release")) {
		t.Errorf("a commit containing an event marker encoded %q, want it whole", string(got))
	}
}
