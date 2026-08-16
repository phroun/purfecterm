package purfecterm

// Encoding key events for the kitty keyboard protocol.
//
// This lives here rather than in a widget because every front end has the same
// job and the same chances to get it wrong: which keys are legacy-encoded, how
// modifiers are numbered, when a release event may be sent at all. A widget
// translates its toolkit's key event into a KeyEvent and hands it over.

import (
	"strconv"
	"strings"
)

// Key event types, as the protocol numbers them.
const (
	KeyPress   = 1
	KeyRepeat  = 2
	KeyRelease = 3
)

// Modifier bits, as the protocol numbers them. The encoded value is these ORed
// together plus one, so "no modifiers" is 1.
const (
	ModShift = 1 << iota
	ModMega
	ModCtrl
	ModSuper
	ModHyper
	ModMicro
	ModCapsLock
	ModNumLock
)

// ModAll is every modifier bit this package defines. A stray bit outside it
// would be encoded verbatim into the CSI parameter — the wire value is the bits
// plus one — so an override is masked to this before it is applied.
const ModAll = ModShift | ModMega | ModCtrl | ModSuper | ModHyper | ModMicro |
	ModCapsLock | ModNumLock

// ModifierOverride adjusts the modifier bits a windowing backend derived, for
// modifiers that backend cannot see for itself.
//
// The answer is not purfecterm's to give. Hyper has no bit in GTK or Qt at all,
// and mew synthesizes it from a doubled Ctrl or Alt — which is a choice about
// one keyboard convention, not a fact about terminals. Which physical key even
// IS Hyper varies by platform and by what the user's keymap binds. An embedder
// has the platform event stream and knows its own conventions, so this hands
// them the decision instead of guessing on their behalf.
//
// Clear is applied BEFORE Set, so a bit named in both ends up set. The zero
// value changes nothing, deliberately: a "keep these" mask would let a
// forgotten field silently erase every modifier, and that reads at the far end
// as Ctrl having stopped working rather than as a mistake in the hook.
//
// Only the kitty keyboard protocol carries these. Hyper, Micro and Glyph have
// no legacy encoding at all — no ESC prefix, no control code — so against a
// guest that never negotiated the protocol an override adds nothing. Clearing a
// legacy-expressible modifier does not reach the legacy path either, which
// builds its sequences from its own booleans rather than from this mask.
type ModifierOverride struct {
	Set   int // bits to add
	Clear int // bits to remove, applied first
}

// ApplyModifierOverride folds an override into a modifier set, ignoring any
// bits that are not modifiers.
func ApplyModifierOverride(mods int, o ModifierOverride) int {
	return (mods&^(o.Clear&ModAll) | o.Set&ModAll) & ModAll
}

// Functional key codes the protocol assigns from the Unicode private use area.
// These are the ones with no sensible codepoint of their own.
const (
	KeyEscape    = 27
	KeyEnter     = 13
	KeyTab       = 9
	KeyBackspace = 127

	KeyInsert   = 2
	KeyDelete   = 3
	KeyPageUp   = 5
	KeyPageDown = 6
	KeyHome     = 7
	KeyEnd      = 8
	KeyF1       = 11
	KeyF2       = 12
	KeyF3       = 13
	KeyF4       = 14
	KeyF5       = 15
	KeyF6       = 17
	KeyF7       = 18
	KeyF8       = 19
	KeyF9       = 20
	KeyF10      = 21
	KeyF11      = 23
	KeyF12      = 24

	KeyUp    = 'A'
	KeyDown  = 'B'
	KeyRight = 'C'
	KeyLeft  = 'D'
)

// KeyEvent is one key press, repeat or release, in the terms the protocol
// encodes: a Unicode key code plus what the layout and modifiers made of it.
type KeyEvent struct {
	// Code is the key's Unicode codepoint in its BASE, unshifted form — 'a'
	// for the A key, not 'A'. Functional keys use the protocol's assigned
	// numbers (see the CSI-tilde and letter-suffix tables below).
	Code rune

	// Shifted is the codepoint the key produced WITH shift ('A'), and Base is
	// the one it would produce on the standard layout. Both are reported only
	// under KeyReportAlternates, and only when they differ from Code.
	Shifted rune
	Base    rune

	// Text is what the key would insert, reported only under
	// KeyboardReportText.
	Text string

	Mods      int  // ModShift etc., ORed
	EventType int  // KeyPress, KeyRepeat or KeyRelease
	Suffix    byte // 'u' for a plain key, '~' or a letter for a functional one
}

// legacyEncodable reports whether a key HAS an old-style encoding at all. Ctrl
// and Mega do (a C0 control, an ESC prefix); Super, Hyper and Micro do not, so a
// key carrying one of those has to go out as CSI whatever the flags say.
func (e KeyEvent) legacyEncodable() bool {
	if e.Suffix != 'u' && e.Suffix != 0 {
		return false // a functional key already has its own sequence
	}
	return e.Mods&(ModSuper|ModHyper|ModMicro) == 0
}

// EncodeKeyEvent renders a key event for the given enhancement flags, returning
// nil when the event should produce nothing — a release with no one listening
// for releases, most commonly.
//
// With no flags this is the legacy encoding every terminal has always sent, so
// a front end can route ALL key events through here and let the flags decide.
func EncodeKeyEvent(e KeyEvent, flags int) []byte {
	if e.EventType == 0 {
		e.EventType = KeyPress
	}
	// Releases and repeats exist only when the application asked for them.
	// Sending a release to an application that did not is how a keystroke
	// arrives twice.
	if e.EventType != KeyPress && flags&KeyboardReportEvents == 0 {
		if e.EventType == KeyRepeat {
			e.EventType = KeyPress // a repeat is an ordinary press to a legacy app
		} else {
			return nil
		}
	}

	if e.EventType == KeyPress && e.legacyEncodable() && !mustUseCSI(e, flags) {
		return legacyKeyBytes(e)
	}
	return csiKeyBytes(e, flags)
}

// mustUseCSI decides whether an otherwise legacy-encodable key has to go out in
// the protocol's form.
func mustUseCSI(e KeyEvent, flags int) bool {
	if flags&KeyboardReportAllKeys != 0 {
		return true // every key, text included
	}
	if flags&KeyboardDisambiguate == 0 {
		return false // nothing asked for the collisions to be resolved
	}
	// Disambiguation is about exactly the keys whose legacy bytes collide.
	// Esc/Tab/Enter/Backspace ARE the C0 controls that Ctrl-[ , Ctrl-I, Ctrl-M
	// and Ctrl-H produce, so both sides of each pair have to become explicit —
	// resolving only one half would leave them just as indistinguishable.
	return ambiguousLegacyKey(e.Code) || e.Mods&(ModCtrl|ModMega) != 0
}

// ambiguousLegacyKey reports whether a key's legacy encoding collides with
// another key's — the collisions KeyboardDisambiguate exists to resolve. Esc,
// Tab, Enter and Backspace are indistinguishable from Ctrl-[, Ctrl-I, Ctrl-M
// and Ctrl-H once they become bytes.
func ambiguousLegacyKey(code rune) bool {
	switch code {
	case KeyEscape, KeyTab, KeyEnter, KeyBackspace, 0:
		return true
	}
	return false
}

// legacyKeyBytes renders the pre-protocol encoding: the key's own bytes, a C0
// control under Ctrl, and an ESC prefix under Mega.
func legacyKeyBytes(e KeyEvent) []byte {
	code := e.Code
	if e.Shifted != 0 && e.Mods&ModShift != 0 {
		code = e.Shifted
	}
	if code == 0 {
		return nil
	}
	var out []byte
	if e.Mods&ModCtrl != 0 {
		if c := ctrlByte(code); c >= 0 {
			out = []byte{byte(c)}
		}
	}
	if out == nil {
		out = []byte(string(code))
	}
	if e.Mods&ModMega != 0 {
		out = append([]byte{0x1b}, out...)
	}
	return out
}

// ctrlByte maps a key to its C0 control byte, or -1 when it has none.
func ctrlByte(code rune) int {
	switch {
	case code >= 'a' && code <= 'z':
		return int(code-'a') + 1
	case code >= 'A' && code <= 'Z':
		return int(code-'A') + 1
	case code == ' ' || code == '@':
		return 0
	case code >= '[' && code <= '_':
		return int(code-'[') + 27
	case code == '?':
		return 127
	}
	return -1
}

// csiKeyBytes renders the protocol's CSI form:
//
//	CSI code[:shifted[:base]] [; mods[:event] [; text]] suffix
//
// Trailing defaults are omitted, which is what keeps an ordinary press short.
func csiKeyBytes(e KeyEvent, flags int) []byte {
	var b strings.Builder
	b.WriteString("\x1b[")

	// The key code section. A functional key with a letter suffix carries no
	// number of its own unless it is modified.
	code := e.Code
	suffix := e.Suffix
	if suffix == 0 {
		suffix = 'u'
	}
	letterSuffix := suffix != 'u' && suffix != '~'

	keyPart := strconv.Itoa(int(code))
	if letterSuffix {
		keyPart = "1" // the placeholder a modified arrow or Home/End uses
	}

	if flags&KeyboardReportAlternates != 0 {
		alt := ""
		if e.Shifted != 0 && e.Shifted != code {
			alt = ":" + strconv.Itoa(int(e.Shifted))
		}
		if e.Base != 0 && e.Base != code {
			if alt == "" {
				alt = ":"
			}
			alt += ":" + strconv.Itoa(int(e.Base))
		}
		keyPart += alt
	}
	b.WriteString(keyPart)

	// The modifier section, present when there is a modifier, a non-press
	// event, or text still to come.
	mods := e.Mods + 1
	event := e.EventType
	needText := flags&KeyboardReportText != 0 && e.Text != "" && event != KeyRelease
	needEvent := event != KeyPress && flags&KeyboardReportEvents != 0
	if mods > 1 || needEvent || needText {
		b.WriteString(";")
		b.WriteString(strconv.Itoa(mods))
		if needEvent {
			b.WriteString(":" + strconv.Itoa(event))
		}
	}
	if needText {
		b.WriteString(";")
		for i, r := range e.Text {
			if i > 0 {
				b.WriteString(":")
			}
			b.WriteString(strconv.Itoa(int(r)))
		}
	}

	b.WriteByte(suffix)
	return []byte(b.String())
}
