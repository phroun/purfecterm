package cli

import (
	"strings"

	"github.com/phroun/direct-key-handler/keyboard"
	"github.com/phroun/purfecterm"
)

// Encoding key events for the kitty keyboard protocol, from the names
// direct-key-handler produces.
//
// The gtk and qt backends do this already, starting from a toolkit keycode.
// This one starts from a name, because that is what arrives here: either
// direct-key-handler decoded it from the outer terminal, or an embedding host
// synthesized it and handed it to Terminal.HandleKeyString.
//
// Without this the whole negotiated protocol went unhonoured on this path.
// keyToBytes emits legacy sequences, which have no way to say "this key came
// UP" — so a guest that asked for release events, as a browser must to track a
// held key, received presses only and had no idea a key was ever let go.

// eventSuffixes are the markers direct-key-handler trails a name with. A name
// carries at most one.
var eventSuffixes = map[string]int{
	":Release": purfecterm.KeyRelease,
	":Repeat":  purfecterm.KeyRepeat,
}

// sideSuffixes mark WHICH of a paired modifier key an event came from, as in
// "M-Press:Left". They are not event types — they decorate a modifier key's own
// press or release, which only KeyboardReportAllKeys carries — but they mark a
// name as something other than a plain keystroke just as the event markers do.
var sideSuffixes = []string{":Left", ":Right"}

// splitEventSuffix separates a trailing event or side marker from the name it
// decorates. With no marker the base is the whole name and the event is a
// press, which is what an undecorated name means.
//
// The third result is what keyToBytes needs: whether there WAS a marker, which
// a press type alone cannot answer, and which is true for the side markers even
// though they carry no event type.
func splitEventSuffix(key string) (base string, eventType int, suffixed bool) {
	for suffix, typ := range eventSuffixes {
		if strings.HasSuffix(key, suffix) {
			return key[:len(key)-len(suffix)], typ, true
		}
	}
	for _, suffix := range sideSuffixes {
		if strings.HasSuffix(key, suffix) {
			return key[:len(key)-len(suffix)], purfecterm.KeyPress, true
		}
	}
	return key, purfecterm.KeyPress, false
}

// prefixMods maps this vocabulary's modifier prefixes to the protocol's bits.
//
// The caret is absent on purpose: "^A" is not a prefix plus a base, it is one
// token naming a Control chord, and kittyKeyName unpacks it.
var prefixMods = map[string]int{
	"S-": purfecterm.ModShift,
	"C-": purfecterm.ModCtrl,
	"M-": purfecterm.ModMega,
	"m-": purfecterm.ModMicro,
	"s-": purfecterm.ModSuper,
	"H-": purfecterm.ModHyper,
}

// splitKittyMods peels modifier prefixes off a name, returning the bits and
// the base. An unrecognised prefix stops the peel and stays in the base, where
// it will fail to resolve rather than being silently swallowed.
func splitKittyMods(key string) (mods int, base string) {
	for {
		matched := false
		for prefix, bit := range prefixMods {
			if strings.HasPrefix(key, prefix) && len(key) > len(prefix) {
				mods |= bit
				key = key[len(prefix):]
				matched = true
				break
			}
		}
		if !matched {
			return mods, key
		}
	}
}

// kittyFunctionalKeys maps the keys direct-key-handler names to the codepoint
// and CSI suffix the protocol assigns them.
//
// Keyed by keyboard.Key rather than by spelling, for the reason keyBytes is:
// the names belong to direct-key-handler and a word copied out of it goes stale
// silently.
var kittyFunctionalKeys = map[keyboard.Key]struct {
	code   rune
	suffix byte
}{
	// Keys whose legacy bytes collide with a Control chord — the collision the
	// disambiguation flag exists to undo.
	keyboard.KeyEscape:      {purfecterm.KeyEscape, 'u'},
	keyboard.KeyReturn:      {purfecterm.KeyEnter, 'u'},
	keyboard.KeyKeypadEnter: {purfecterm.KeyEnter, 'u'},
	keyboard.KeyTab:         {purfecterm.KeyTab, 'u'},
	keyboard.KeyBackspace:   {purfecterm.KeyBackspace, 'u'},
	keyboard.KeyDEL:         {purfecterm.KeyBackspace, 'u'},
	keyboard.KeySpace:       {' ', 'u'},

	// Cursor keys keep their letter suffix.
	keyboard.KeyUp:    {purfecterm.KeyUp, 'A'},
	keyboard.KeyDown:  {purfecterm.KeyDown, 'B'},
	keyboard.KeyRight: {purfecterm.KeyRight, 'C'},
	keyboard.KeyLeft:  {purfecterm.KeyLeft, 'D'},
	keyboard.KeyHome:  {'H', 'H'},
	keyboard.KeyEnd:   {'F', 'F'},

	// The CSI-tilde family.
	keyboard.KeyInsert:   {purfecterm.KeyInsert, '~'},
	keyboard.KeyDelete:   {purfecterm.KeyDelete, '~'}, // forward delete, "FDel"
	keyboard.KeyPageUp:   {purfecterm.KeyPageUp, '~'},
	keyboard.KeyPageDown: {purfecterm.KeyPageDown, '~'},

	// F1-F4 carry letter suffixes; F5 and up use the tilde family.
	keyboard.KeyF1:  {'P', 'P'},
	keyboard.KeyF2:  {'Q', 'Q'},
	keyboard.KeyF3:  {'R', 'R'},
	keyboard.KeyF4:  {'S', 'S'},
	keyboard.KeyF5:  {purfecterm.KeyF5, '~'},
	keyboard.KeyF6:  {purfecterm.KeyF6, '~'},
	keyboard.KeyF7:  {purfecterm.KeyF7, '~'},
	keyboard.KeyF8:  {purfecterm.KeyF8, '~'},
	keyboard.KeyF9:  {purfecterm.KeyF9, '~'},
	keyboard.KeyF10: {purfecterm.KeyF10, '~'},
	keyboard.KeyF11: {purfecterm.KeyF11, '~'},
	keyboard.KeyF12: {purfecterm.KeyF12, '~'},
}

// kittyKeyName resolves a base name to the protocol's codepoint and suffix,
// folding in any modifier the name itself carries.
//
// A caret chord returns its Control bit here rather than through the prefix
// table, because that is where the Control lives: "^A" is Ctrl held on the A
// key, and the protocol wants the base letter with the bit set beside it.
func kittyKeyName(base string) (code rune, suffix byte, extraMods int, ok bool) {
	if k, found := keyByName[base]; found {
		if f, has := kittyFunctionalKeys[k]; has {
			return f.code, f.suffix, 0, true
		}
		// A key this package has no protocol code for — the locks, the system
		// keys, F13 and up. Reporting a wrong code is worse than reporting
		// none, so the caller falls back rather than guessing.
		return 0, 0, 0, false
	}

	// A caret chord: Control, spelled against the key it is held on.
	if b, isCtrl := controlByte(base); isCtrl {
		switch {
		case base[1] >= 'A' && base[1] <= 'Z':
			return rune(base[1] - 'A' + 'a'), 'u', purfecterm.ModCtrl, true
		case base[1] >= 'a' && base[1] <= 'z':
			return rune(base[1]), 'u', purfecterm.ModCtrl, true
		default:
			// "^@", "^[", "^\\" and friends: the punctuation is the base key.
			_ = b
			return rune(base[1]), 'u', purfecterm.ModCtrl, true
		}
	}

	// Anything that is a single character is that character. The protocol wants
	// the BASE, unshifted codepoint, so an uppercase letter is lowered and its
	// shifted form is reported separately by the caller.
	if r := []rune(base); len(r) == 1 {
		c := r[0]
		if c >= 'A' && c <= 'Z' {
			return c + ('a' - 'A'), 'u', purfecterm.ModShift, true
		}
		return c, 'u', 0, true
	}

	return 0, 0, 0, false
}

// encodeKittyKeyName builds the protocol's escape sequence for a key name, or
// nil when the guest has negotiated nothing or this package cannot name the
// key in the protocol's terms.
//
// Returning nil is not a dropped keystroke: the caller falls back to the legacy
// encoder, which is what a guest that asked for nothing expects anyway.
func encodeKittyKeyName(key string, flags int) []byte {
	if flags == 0 {
		return nil
	}
	base, eventType, _ := splitEventSuffix(key)
	mods, base := splitKittyMods(base)
	if base == "" {
		return nil
	}
	code, suffix, extra, ok := kittyKeyName(base)
	if !ok {
		return nil
	}
	mods |= extra

	ev := purfecterm.KeyEvent{
		Code:      code,
		Mods:      mods,
		EventType: eventType,
		Suffix:    suffix,
	}
	if code >= 'a' && code <= 'z' {
		ev.Shifted = code - ('a' - 'A')
	}
	// The text a key inserts, reported only under KeyboardReportText. A chord
	// that is not text-producing has none, and a release inserts nothing.
	if suffix == 'u' && code >= ' ' && eventType != purfecterm.KeyRelease {
		text := code
		if mods&purfecterm.ModShift != 0 && ev.Shifted != 0 {
			text = ev.Shifted
		}
		if mods&(purfecterm.ModCtrl|purfecterm.ModMega|purfecterm.ModSuper) == 0 {
			ev.Text = string(text)
		}
	}
	return purfecterm.EncodeKeyEvent(ev, flags)
}
