package cli

import (
	"testing"

	"github.com/phroun/direct-key-handler/keyboard"
)

// unencodable lists the keys this package has no bytes for.
//
// "Unencodable" names the state of this encoder, not a fact about keyboards.
// The legacy wire has no sequence for a lock or a system key — Caps Lock
// changes the state a later keystroke arrives in and sends nothing of its own —
// but the kitty protocol reports every one of these under
// KeyboardReportAllKeys, which keyboard_protocol.go already negotiates and this
// encoder does not yet honour. F13-F20 are plainly just missing:
// direct-key-handler decodes them from kitty codepoints 57376+ and there is no
// legacy form it reads, so encoding them means implementing the CSI-u path
// rather than guessing a sequence that cannot round-trip.
//
// Each of these reaches the guest bracketed, so the gap is visible where it
// happens. Listing them here says the omission is known, not that the keys are
// somehow not keys.
var unencodable = map[keyboard.Key]bool{
	keyboard.KeyCapsLock:    true,
	keyboard.KeyClear:       true,
	keyboard.KeyScrollLock:  true,
	keyboard.KeyPrintScreen: true,
	keyboard.KeyPause:       true,
	keyboard.KeyMenu:        true,

	keyboard.KeyF13: true,
	keyboard.KeyF14: true,
	keyboard.KeyF15: true,
	keyboard.KeyF16: true,
	keyboard.KeyF17: true,
	keyboard.KeyF18: true,
	keyboard.KeyF19: true,
	keyboard.KeyF20: true,

	// The input-method and international caps, which direct-key-handler names
	// and the legacy wire has no sequence for at all. Henkan and Muhenkan
	// (convert / no-convert) and the Hangul and Kana locks drive an input
	// method on the machine the KEYBOARD is attached to, and there has never
	// been a byte for saying so down a terminal line. Ro and Yen are the two
	// extra caps a JIS board carries; a terminal sends whatever character the
	// layout puts on them, which is not this key being reported.
	//
	// Begin is the DEC pad's centre cap, Power is a system key, and Zig and Zag
	// are shapes no terminal has an encoding for either.
	keyboard.KeyBegin:      true,
	keyboard.KeyHangulLock: true,
	keyboard.KeyHanja:      true,
	keyboard.KeyHenkan:     true,
	keyboard.KeyKanaLock:   true,
	keyboard.KeyMuhenkan:   true,
	keyboard.KeyPower:      true,
	keyboard.KeyRo:         true,
	keyboard.KeyYen:        true,
	keyboard.KeyZag:        true,
	keyboard.KeyZig:        true,
}

// Every key direct-key-handler can emit is either encoded here or listed as
// deliberately unencodable.
//
// This is the check that was missing when the home-row key was renamed. The
// encoder's table was transcribed from upstream by hand and nothing compared
// the two afterwards, so "Return" began arriving, matched nothing, and was
// typed at the guest as six letters — no error, no failing test, just a key
// that stopped working. Reading the key list from AllKeys() means a key added
// or withdrawn upstream shows up here on the next dependency bump and has to be
// decided about, in one place, instead of being discovered by a user.
func TestEveryKeyIsAccountedFor(t *testing.T) {
	for _, k := range keyboard.AllKeys() {
		_, encoded := keyBytes[k]
		switch {
		case encoded && unencodable[k]:
			t.Errorf("%v (emits %q) is both encoded and listed as unencodable; "+
				"drop it from one list", k, k.DefaultName())
		case !encoded && !unencodable[k]:
			t.Errorf("%v (emits %q) has no encoding and is not listed as "+
				"unencodable, so it reaches the guest as %q. Give it bytes in "+
				"keyBytes, or add it to unencodable to say the omission is meant.",
				k, k.DefaultName(), string(unknownKeyBytes(k.DefaultName())))
		}
	}
}

// The name index covers the encoded keys, and covers them under the spellings
// upstream actually emits.
//
// keyBytes is keyed by constant, but keyToBytes is handed a string, so the
// index between them is where a mismatch would now hide. Building it from
// AllKeys() is what makes that impossible — this test is the assertion that it
// really is built that way and not backfilled by hand.
func TestNameIndexResolvesEveryEncodedKey(t *testing.T) {
	for k := range keyBytes {
		name := k.DefaultName()
		if name == "" {
			t.Errorf("%v has bytes but no name upstream; it can never be reached", k)
			continue
		}
		if got := keyByName[name]; got != k {
			t.Errorf("%q resolves to %v, want %v", name, got, k)
		}
	}
}

// Encoded keys round-trip: the bytes this encoder writes decode back to the
// same key. That is the property a guest depends on, and it is stated in
// constants so it survives any respelling.
func TestEncodedKeysRoundTripToTheSameKey(t *testing.T) {
	for _, k := range keyboard.AllKeys() {
		b, ok := keyBytes[k]
		if !ok {
			continue
		}
		// Two keys encode to a byte that is also an ordinary character, so the
		// decoder reads the character and cannot report the key. Neither is a
		// defect and neither is fixable from this side:
		//
		//   Backspace shares DEL with KeyDEL by design — a terminal sends one
		//   byte for its backspace and this encoder cannot invent which lineage
		//   the guest will assume, so both erase-behind names go out as DEL.
		//
		//   Space is 0x20, which is the space character. A guest cannot tell a
		//   pressed spacebar from a typed space, and no terminal makes it able
		//   to; that is what the character IS.
		if k == keyboard.KeyBackspace || k == keyboard.KeySpace {
			continue
		}
		got := decodeBack(t, b)
		// A keypad key comes back wearing its "P-" prefix, and that is not a
		// failed round trip. The prefix says WHICH of two duplicate keys was
		// struck, and the legacy wire has no way to carry that — a terminal
		// sends one encoding for the character wherever it sits. keyToBytes
		// takes the prefix off for the same reason, so the pair is consistent:
		// what goes out is the twin's bytes, and what comes back is the pad's
		// name for them.
		name := k.DefaultName()
		if len(got) == 1 {
			if bare, isPad := stripPadPrefix(got[0]); isPad {
				got[0] = bare
			}
		}
		if len(got) != 1 || got[0] != name {
			t.Errorf("%v encodes %q, which decodes as %v, want [%s]",
				k, string(b), got, name)
		}
	}
}
