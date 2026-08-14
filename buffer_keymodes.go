package purfecterm

// Keyboard and interaction modes that an input adapter consults to encode keys
// and pointer events. The buffer only records the mode an application requested;
// the adapters (cli/input.go, the gtk/qt key handlers) act on it.

// SetApplicationCursorKeys sets DECCKM (?1): when on, the unmodified cursor keys
// send the SS3 form (ESC O A) instead of the CSI form (ESC [ A).
func (b *Buffer) SetApplicationCursorKeys(on bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.appCursorKeys = on
}

// IsApplicationCursorKeys reports whether DECCKM is active.
func (b *Buffer) IsApplicationCursorKeys() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.appCursorKeys
}

// SetApplicationKeypad sets application keypad mode (DECKPAM / DECKPNM).
func (b *Buffer) SetApplicationKeypad(on bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.appKeypad = on
}

// IsApplicationKeypad reports whether application keypad mode is active.
func (b *Buffer) IsApplicationKeypad() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.appKeypad
}

// SetFocusReporting sets ?1004: when on, the adapter reports focus changes as
// CSI I (focus in) and CSI O (focus out).
func (b *Buffer) SetFocusReporting(on bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.focusReporting = on
}

// IsFocusReporting reports whether focus reporting (?1004) is active.
func (b *Buffer) IsFocusReporting() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.focusReporting
}

// SetAltScrollMode sets ?1007: when on, the mouse wheel sends arrow keys while
// the alternate screen is active and no mouse tracking is in effect.
func (b *Buffer) SetAltScrollMode(on bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.altScrollMode = on
}

// IsAltScrollMode reports whether alternate-scroll mode (?1007) is active.
func (b *Buffer) IsAltScrollMode() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.altScrollMode
}

// FocusReportSequence returns the bytes to send for a focus change when focus
// reporting is enabled, or nil otherwise. focused true -> CSI I, false -> CSI O.
func (b *Buffer) FocusReportSequence(focused bool) []byte {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if !b.focusReporting {
		return nil
	}
	if focused {
		return []byte{0x1b, '[', 'I'}
	}
	return []byte{0x1b, '[', 'O'}
}

// resetKeyModes clears the keyboard/interaction modes (RIS).
func (b *Buffer) resetKeyModes() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.appCursorKeys = false
	b.appKeypad = false
	b.focusReporting = false
	b.altScrollMode = false
}
