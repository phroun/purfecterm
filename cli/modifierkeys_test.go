package cli

import (
	"testing"

	"github.com/phroun/purfecterm"
)

// A modifier reporting itself has to survive the trip to the guest as a KEY.
//
// It arrives named — "LMod:S" for the left Shift going down — and the only
// encoding that can carry it is the kitty protocol's, under the flag that asks
// for every key. What it must never do is reach the legacy encoder's fallback,
// which brackets an unencodable name and sends it as text: a guest that is an
// editor then types "<LMod:S>" every time a modifier is touched.

const allKeys = purfecterm.KeyboardDisambiguate | purfecterm.KeyboardReportAllKeys
const allKeysWithEvents = allKeys | purfecterm.KeyboardReportEvents

func TestBareModifiersEncodeAsTheirOwnKeys(t *testing.T) {
	for _, c := range []struct {
		key, want, what string
	}{
		{"LMod:S", "\x1b[57441u", "left Shift"},
		{"LMod:C", "\x1b[57442u", "left Control"},
		{"LMod:M", "\x1b[57443u", "left Mega"},
		{"LMod:s", "\x1b[57444u", "left Super"},
		{"LMod:H", "\x1b[57445u", "left Hyper"},
		{"LMod:m", "\x1b[57446u", "left Micro"},
		{"RMod:S", "\x1b[57447u", "right Shift"},
		{"RMod:C", "\x1b[57448u", "right Control"},
		{"RMod:M", "\x1b[57449u", "right Mega"},
		{"RMod:s", "\x1b[57450u", "right Super"},
		{"RMod:H", "\x1b[57451u", "right Hyper"},
		{"RMod:m", "\x1b[57452u", "right Micro"},
	} {
		if got := string(encodeKittyKeyName(c.key, allKeys)); got != c.want {
			t.Errorf("%s: %q encoded as %q, want %q", c.what, c.key, got, c.want)
		}
	}
}

// The release is the whole reason a guest tracks these, and it says so with the
// protocol's event type rather than a second press.
func TestBareModifierRelease(t *testing.T) {
	got := string(encodeKittyKeyName("LMod:M:Release", allKeysWithEvents))
	if want := "\x1b[57443;1:3u"; got != want {
		t.Errorf("left Mega release encoded as %q, want %q", got, want)
	}
	// A guest that asked for every key but NOT for events gets the press and
	// not the release, same as any other key: a release arriving at something
	// that cannot read event types reads as the keystroke happening twice.
	if got := encodeKittyKeyName("LMod:M:Release", allKeys); got != nil {
		t.Errorf("release sent to a guest that asked for no events: %q", got)
	}
}

// Reported under KeyboardReportAllKeys and under nothing else — the protocol's
// own rule. A guest that negotiated less has no reason to expect codepoint
// 57441 and would read it as a private-use character.
func TestBareModifiersNeedTheAllKeysFlag(t *testing.T) {
	for _, flags := range []int{
		0,
		purfecterm.KeyboardDisambiguate,
		purfecterm.KeyboardDisambiguate | purfecterm.KeyboardReportEvents,
		purfecterm.KeyboardReportAlternates | purfecterm.KeyboardReportText,
	} {
		if got := encodeKittyKeyName("LMod:S", flags); got != nil {
			t.Errorf("flags %d: sent %q for a modifier nobody asked to see", flags, got)
		}
	}
}

// A producer that cannot tell the caps apart says so by giving no side, and the
// protocol has no number for that: it names the left cap and the right cap and
// nothing in between. It goes out as the left one, because the alternative is
// the guest hearing nothing — and a modifier it never saw go down is a modifier
// it believes is still up.
func TestSidelessModifierGoesOutAsTheLeftCap(t *testing.T) {
	for _, c := range []struct{ key, want string }{
		{"Mod:S", "\x1b[57441u"},
		{"Mod:H", "\x1b[57445u"},
		{"Mod:m:Release", "\x1b[57446;1:3u"},
	} {
		if got := string(encodeKittyKeyName(c.key, allKeysWithEvents)); got != c.want {
			t.Errorf("%q encoded as %q, want %q", c.key, got, c.want)
		}
	}
}

// A letter this package does not know is still one of these events, and still
// has no code — the half that keeps it out of the bracketing path.
func TestUnknownModifierLetterEncodesNothing(t *testing.T) {
	for _, key := range []string{"LMod:X", "Mod:", "RMod:Shift"} {
		if got := encodeKittyKeyName(key, allKeysWithEvents); got != nil {
			t.Errorf("%q encoded as %q, want nothing", key, got)
		}
	}
}

// The bug this fixes: every one of these used to reach unknownKeyBytes.
func TestNoModifierNameIsSentAsText(t *testing.T) {
	for _, key := range []string{
		"LMod:S", "RMod:C", "Mod:H", // presses, the shape that fell through
		"LMod:M:Release", "Mod:m:Release", // and their releases
		"LMod:X", "Mod:", // a letter this package does not know
	} {
		if got := keyToBytes(key); got != nil {
			t.Errorf("keyToBytes(%q) = %q, want nothing; a modifier has no legacy encoding",
				key, got)
		}
	}
}

// The names that merely LOOK like one still encode as themselves, so the guard
// above cannot quietly swallow a real key.
func TestModifierGuardIsNotTooGreedy(t *testing.T) {
	for _, c := range []struct{ key, want string }{
		{"M-o", "\x1bo"},
		{"Mod", "<Mod>"},
	} {
		if got := string(keyToBytes(c.key)); got != c.want {
			t.Errorf("keyToBytes(%q) = %q, want %q", c.key, got, c.want)
		}
	}
}
