package purfectermqt

// Encoding key events for the kitty keyboard protocol.
//
// This runs ONLY when an application has negotiated enhancement flags. With no
// flags — every application that never asked — the legacy path in keyPressEvent
// is untouched, so nothing about ordinary typing changes.

import (
	"github.com/mappu/miqt/qt"
	"github.com/phroun/purfecterm"
)

// kittyKeyval maps a Qt key to the protocol's key code and the suffix its
// sequence ends with. text is the event's own text, used for keys that carry no
// dedicated constant. ok is false for a key with no protocol representation,
// which falls back to the legacy path.
func kittyKeyval(key qt.Key, text string) (code rune, suffix byte, ok bool) {
	switch key {
	// Keys whose legacy bytes collide with a Ctrl combination — the whole
	// reason the disambiguation flag exists.
	case qt.Key_Escape:
		return purfecterm.KeyEscape, 'u', true
	case qt.Key_Return, qt.Key_Enter:
		return purfecterm.KeyEnter, 'u', true
	case qt.Key_Tab, qt.Key_Backtab:
		return purfecterm.KeyTab, 'u', true
	case qt.Key_Backspace:
		return purfecterm.KeyBackspace, 'u', true

	// Cursor keys keep their letter suffix.
	case qt.Key_Up:
		return purfecterm.KeyUp, 'A', true
	case qt.Key_Down:
		return purfecterm.KeyDown, 'B', true
	case qt.Key_Right:
		return purfecterm.KeyRight, 'C', true
	case qt.Key_Left:
		return purfecterm.KeyLeft, 'D', true
	case qt.Key_Home:
		return 'H', 'H', true
	case qt.Key_End:
		return 'F', 'F', true

	// The CSI-tilde family.
	case qt.Key_Insert:
		return purfecterm.KeyInsert, '~', true
	case qt.Key_Delete:
		return purfecterm.KeyDelete, '~', true
	case qt.Key_PageUp:
		return purfecterm.KeyPageUp, '~', true
	case qt.Key_PageDown:
		return purfecterm.KeyPageDown, '~', true

	// F1-F4 carry letter suffixes; F5 up use the tilde family.
	case qt.Key_F1:
		return 'P', 'P', true
	case qt.Key_F2:
		return 'Q', 'Q', true
	case qt.Key_F3:
		return 'R', 'R', true
	case qt.Key_F4:
		return 'S', 'S', true
	case qt.Key_F5:
		return purfecterm.KeyF5, '~', true
	case qt.Key_F6:
		return purfecterm.KeyF6, '~', true
	case qt.Key_F7:
		return purfecterm.KeyF7, '~', true
	case qt.Key_F8:
		return purfecterm.KeyF8, '~', true
	case qt.Key_F9:
		return purfecterm.KeyF9, '~', true
	case qt.Key_F10:
		return purfecterm.KeyF10, '~', true
	case qt.Key_F11:
		return purfecterm.KeyF11, '~', true
	case qt.Key_F12:
		return purfecterm.KeyF12, '~', true
	}

	// Qt numbers printable keys by their UPPERCASE codepoint, but the protocol
	// wants the base, unshifted one, so a letter is lowered and its shifted
	// form reported separately.
	if key > 0 && key < 0x10000 {
		base := rune(key)
		if base >= 'A' && base <= 'Z' {
			base += 'a' - 'A'
		}
		if base >= ' ' {
			return base, 'u', true
		}
	}
	// A key with no constant of its own but which produced text — a dead key
	// resolving, a layout Qt has no name for — is that text's first rune.
	for _, r := range text {
		return r, 'u', true
	}
	return 0, 0, false
}

// kittyMods packs Qt modifier state into the protocol's bits. The caller passes
// the booleans it already derived, which on macOS have had Qt's Control/Meta
// swap undone.
func kittyMods(hasShift, hasCtrl, hasAlt, hasMeta, capsLock, numLock bool) int {
	mods := 0
	if hasShift {
		mods |= purfecterm.ModShift
	}
	if hasAlt {
		mods |= purfecterm.ModMega
	}
	if hasCtrl {
		mods |= purfecterm.ModCtrl
	}
	if hasMeta {
		mods |= purfecterm.ModMicro
	}
	if capsLock {
		mods |= purfecterm.ModCapsLock
	}
	if numLock {
		mods |= purfecterm.ModNumLock
	}
	return mods
}

// encodeKittyKey renders a key event under the active enhancement flags,
// returning nil when the protocol is not in use or the key has no
// representation, so the caller falls back to the legacy encoding.
func (w *Widget) encodeKittyKey(key qt.Key, text string, mods int, eventType int) []byte {
	flags := w.buffer.KeyboardFlags()
	if flags == 0 {
		return nil
	}
	code, suffix, ok := kittyKeyval(key, text)
	if !ok {
		return nil
	}
	ev := purfecterm.KeyEvent{
		Code:      code,
		Mods:      mods,
		EventType: eventType,
		Suffix:    suffix,
	}
	if code >= 'a' && code <= 'z' {
		ev.Shifted = code - ('a' - 'A')
	}
	// Text is what the key would insert. Qt hands it over directly, so unlike
	// the base code there is nothing to reconstruct — but a control combination
	// produces a C0 byte there, which is not text the application wants.
	if suffix == 'u' && text != "" && mods&(purfecterm.ModCtrl|purfecterm.ModMega|purfecterm.ModMicro) == 0 {
		if r := []rune(text); len(r) > 0 && r[0] >= ' ' {
			ev.Text = text
		}
	}
	return purfecterm.EncodeKeyEvent(ev, flags)
}
