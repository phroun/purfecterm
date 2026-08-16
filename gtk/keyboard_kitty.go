package purfectermgtk

// Encoding key events for the kitty keyboard protocol.
//
// This runs ONLY when an application has negotiated enhancement flags. With no
// flags — every application that never asked — the legacy path in onKeyPress is
// untouched, so nothing about ordinary typing changes.

import (
	"github.com/gotk3/gotk3/gdk"
	"github.com/phroun/purfecterm"
)

// kittyKeyval maps a GDK keyval to the protocol's key code and the suffix its
// sequence ends with. ok is false for a key with no protocol representation,
// which falls back to the legacy path.
func kittyKeyval(keyval uint) (code rune, suffix byte, ok bool) {
	switch keyval {
	// Keys whose legacy bytes collide with a Ctrl combination — the whole
	// reason the disambiguation flag exists.
	case gdk.KEY_Escape:
		return purfecterm.KeyEscape, 'u', true
	case gdk.KEY_Return, gdk.KEY_KP_Enter:
		return purfecterm.KeyEnter, 'u', true
	case gdk.KEY_Tab, gdk.KEY_ISO_Left_Tab, gdk.KEY_KP_Tab:
		return purfecterm.KeyTab, 'u', true
	case gdk.KEY_BackSpace:
		return purfecterm.KeyBackspace, 'u', true

	// Cursor keys keep their letter suffix.
	case gdk.KEY_Up, gdk.KEY_KP_Up:
		return purfecterm.KeyUp, 'A', true
	case gdk.KEY_Down, gdk.KEY_KP_Down:
		return purfecterm.KeyDown, 'B', true
	case gdk.KEY_Right, gdk.KEY_KP_Right:
		return purfecterm.KeyRight, 'C', true
	case gdk.KEY_Left, gdk.KEY_KP_Left:
		return purfecterm.KeyLeft, 'D', true
	case gdk.KEY_Home, gdk.KEY_KP_Home:
		return 'H', 'H', true
	case gdk.KEY_End, gdk.KEY_KP_End:
		return 'F', 'F', true

	// The CSI-tilde family.
	case gdk.KEY_Insert, gdk.KEY_KP_Insert:
		return purfecterm.KeyInsert, '~', true
	case gdk.KEY_Delete, gdk.KEY_KP_Delete:
		return purfecterm.KeyDelete, '~', true
	case gdk.KEY_Page_Up, gdk.KEY_KP_Page_Up:
		return purfecterm.KeyPageUp, '~', true
	case gdk.KEY_Page_Down, gdk.KEY_KP_Page_Down:
		return purfecterm.KeyPageDown, '~', true

	// F1-F4 carry letter suffixes; F5 up use the tilde family.
	case gdk.KEY_F1:
		return 'P', 'P', true
	case gdk.KEY_F2:
		return 'Q', 'Q', true
	case gdk.KEY_F3:
		return 'R', 'R', true
	case gdk.KEY_F4:
		return 'S', 'S', true
	case gdk.KEY_F5:
		return purfecterm.KeyF5, '~', true
	case gdk.KEY_F6:
		return purfecterm.KeyF6, '~', true
	case gdk.KEY_F7:
		return purfecterm.KeyF7, '~', true
	case gdk.KEY_F8:
		return purfecterm.KeyF8, '~', true
	case gdk.KEY_F9:
		return purfecterm.KeyF9, '~', true
	case gdk.KEY_F10:
		return purfecterm.KeyF10, '~', true
	case gdk.KEY_F11:
		return purfecterm.KeyF11, '~', true
	case gdk.KEY_F12:
		return purfecterm.KeyF12, '~', true
	}

	// Anything that maps to a character is that character. The protocol wants
	// the BASE, unshifted codepoint, so an uppercase letter is lowered and the
	// shifted form is reported separately.
	if r := gdk.KeyvalToUnicode(keyval); r != 0 {
		base := r
		if base >= 'A' && base <= 'Z' {
			base += 'a' - 'A'
		}
		return base, 'u', true
	}
	return 0, 0, false
}

// kittyMods packs GDK modifier state into the protocol's bits.
func kittyMods(hasShift, hasCtrl, hasMega, hasMicro, hasSuper, capsLock, numLock bool) int {
	mods := 0
	if hasShift {
		mods |= purfecterm.ModShift
	}
	if hasMega {
		mods |= purfecterm.ModMega
	}
	if hasCtrl {
		mods |= purfecterm.ModCtrl
	}
	if hasSuper {
		mods |= purfecterm.ModSuper
	}
	if hasMicro {
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
func (w *Widget) encodeKittyKey(keyval uint, mods int, eventType int) []byte {
	flags := w.buffer.KeyboardFlags()
	if flags == 0 {
		return nil
	}
	code, suffix, ok := kittyKeyval(keyval)
	if !ok {
		return nil
	}
	ev := purfecterm.KeyEvent{
		Code:      code,
		Mods:      mods,
		EventType: eventType,
		Suffix:    suffix,
	}
	// The shifted form and the text a key inserts are only reported when the
	// application asked for them, but they are cheap to fill in either way.
	if shifted := gdk.KeyvalToUnicode(gdk.KeyvalToUpper(keyval)); shifted != 0 && shifted != code {
		ev.Shifted = shifted
	}
	if suffix == 'u' && code >= ' ' {
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
