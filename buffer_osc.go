package purfecterm

// Window title (OSC 0/1/2) and default/palette color overrides (OSC 4/10/11/12).
// The buffer records these; a renderer honors the color overrides through the
// Effective* accessors, and an adapter reflects the title via the callback.

// SetWindowTitle records the window title and notifies the title callback.
func (b *Buffer) SetWindowTitle(title string) {
	b.mu.Lock()
	b.windowTitle = title
	cb := b.onTitleChange
	b.mu.Unlock()
	if cb != nil {
		cb(title)
	}
}

// GetWindowTitle returns the current window title.
func (b *Buffer) GetWindowTitle() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.windowTitle
}

// SetTitleChangeCallback registers a callback invoked when OSC 0/1/2 sets the
// title.
func (b *Buffer) SetTitleChangeCallback(fn func(string)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onTitleChange = fn
}

// SetDefaultForegroundColor overrides the default foreground (OSC 10).
func (b *Buffer) SetDefaultForegroundColor(c Color) {
	b.mu.Lock()
	cc := c
	b.oscFg = &cc
	b.markDirty()
	b.mu.Unlock()
}

// SetDefaultBackgroundColor overrides the default background (OSC 11).
func (b *Buffer) SetDefaultBackgroundColor(c Color) {
	b.mu.Lock()
	cc := c
	b.oscBg = &cc
	b.markDirty()
	b.mu.Unlock()
}

// SetCursorColor overrides the cursor color (OSC 12).
func (b *Buffer) SetCursorColor(c Color) {
	b.mu.Lock()
	cc := c
	b.oscCursor = &cc
	b.markDirty()
	b.mu.Unlock()
}

// EffectiveDefaultForeground returns the OSC 10 override, or the built-in default.
func (b *Buffer) EffectiveDefaultForeground() Color {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.oscFg != nil {
		return *b.oscFg
	}
	return DefaultForeground
}

// EffectiveDefaultBackground returns the OSC 11 override, or the built-in default.
func (b *Buffer) EffectiveDefaultBackground() Color {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.oscBg != nil {
		return *b.oscBg
	}
	return DefaultBackground
}

// EffectiveCursorColor returns the OSC 12 override, or the default foreground.
func (b *Buffer) EffectiveCursorColor() Color {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.oscCursor != nil {
		return *b.oscCursor
	}
	return DefaultForeground
}

// SetPaletteColor overrides a 256-color palette entry (OSC 4).
func (b *Buffer) SetPaletteColor(index int, c Color) {
	if index < 0 || index > 255 {
		return
	}
	b.mu.Lock()
	if b.oscPalette == nil {
		b.oscPalette = map[int]Color{}
	}
	b.oscPalette[index] = c
	b.markDirty()
	b.mu.Unlock()
}

// GetPaletteColor returns the OSC 4 override for a palette index, or the built-in
// 256-color value.
func (b *Buffer) GetPaletteColor(index int) Color {
	b.mu.RLock()
	if b.oscPalette != nil {
		if c, ok := b.oscPalette[index]; ok {
			b.mu.RUnlock()
			return c
		}
	}
	b.mu.RUnlock()
	return Get256Color(index)
}

// resetOSCColors drops all OSC color overrides (RIS).
func (b *Buffer) resetOSCColors() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.oscFg = nil
	b.oscBg = nil
	b.oscCursor = nil
	b.oscPalette = nil
}
