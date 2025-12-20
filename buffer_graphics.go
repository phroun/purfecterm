package purfecterm

// --- Custom Glyph System Methods ---

// SetBGP sets the Base Glyph Palette for subsequent characters
// -1 means use the foreground color code as the palette number
func (b *Buffer) SetBGP(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.currentBGP = n
}

// ResetBGP resets the Base Glyph Palette to default (-1)
func (b *Buffer) ResetBGP() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.currentBGP = -1
}

// GetBGP returns the current Base Glyph Palette setting
func (b *Buffer) GetBGP() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.currentBGP
}

// SetXFlip sets the horizontal flip attribute for subsequent characters
func (b *Buffer) SetXFlip(on bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.currentXFlip = on
}

// GetXFlip returns the current horizontal flip setting
func (b *Buffer) GetXFlip() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.currentXFlip
}

// SetYFlip sets the vertical flip attribute for subsequent characters
func (b *Buffer) SetYFlip(on bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.currentYFlip = on
}

// GetYFlip returns the current vertical flip setting
func (b *Buffer) GetYFlip() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.currentYFlip
}

// --- Palette Management ---

// DeleteAllPalettes removes all custom palettes
func (b *Buffer) DeleteAllPalettes() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.palettes = make(map[int]*Palette)
	b.markDirty()
}

// DeletePalette removes a specific palette
func (b *Buffer) DeletePalette(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.palettes, n)
	b.markDirty()
}

// InitPalette creates or reinitializes a palette with the specified number of entries
func (b *Buffer) InitPalette(n int, length int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.palettes[n] = NewPalette(length)
	b.markDirty()
}

// SetPaletteEntry sets a single entry in a palette
// colorCode uses SGR-style: 30-37/40-47 (normal), 90-97/100-107 (bright), 8 (transparent), 9 (default fg)
// If dim is true, the color is a dim variant
func (b *Buffer) SetPaletteEntry(paletteNum int, idx int, colorCode int, dim bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	palette, ok := b.palettes[paletteNum]
	if !ok {
		return // Palette doesn't exist
	}
	if idx < 0 || idx >= len(palette.Entries) {
		return // Index out of bounds
	}

	entry := &palette.Entries[idx]
	entry.Dim = dim

	switch colorCode {
	case 8:
		entry.Type = PaletteEntryTransparent
		palette.UsesBg = true // Track for cache key optimization
	case 9:
		entry.Type = PaletteEntryDefaultFG
		palette.UsesDefaultFG = true // Track for cache key optimization
	default:
		entry.Type = PaletteEntryColor
		// Map SGR color codes to actual colors
		// 30-37, 40-47 -> ANSI 0-7
		// 90-97, 100-107 -> ANSI 8-15 (bright)
		var colorIdx int
		if colorCode >= 30 && colorCode <= 37 {
			colorIdx = colorCode - 30
		} else if colorCode >= 40 && colorCode <= 47 {
			colorIdx = colorCode - 40
		} else if colorCode >= 90 && colorCode <= 97 {
			colorIdx = colorCode - 90 + 8
		} else if colorCode >= 100 && colorCode <= 107 {
			colorIdx = colorCode - 100 + 8
		} else {
			// Unknown color code, default to white
			colorIdx = 7
		}
		if colorIdx >= 0 && colorIdx < len(ANSIColors) {
			entry.Color = ANSIColors[colorIdx]
		}
	}

	b.markDirty()
}

// SetPaletteEntryColor sets a palette entry directly from a Color value
// Use this for 256-color and true color palette entries
func (b *Buffer) SetPaletteEntryColor(paletteNum int, idx int, color Color, dim bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	palette, ok := b.palettes[paletteNum]
	if !ok {
		return // Palette doesn't exist
	}
	if idx < 0 || idx >= len(palette.Entries) {
		return // Index out of bounds
	}

	entry := &palette.Entries[idx]
	entry.Type = PaletteEntryColor
	entry.Color = color
	entry.Dim = dim
	b.markDirty()
}

// GetPalette returns a palette by number, or nil if it doesn't exist
func (b *Buffer) GetPalette(n int) *Palette {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.palettes[n]
}

// --- Custom Glyph Management ---

// DeleteAllGlyphs removes all custom glyph definitions
func (b *Buffer) DeleteAllGlyphs() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.customGlyphs = make(map[rune]*CustomGlyph)
	b.markDirty()
}

// DeleteGlyph removes a specific custom glyph definition
func (b *Buffer) DeleteGlyph(r rune) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.customGlyphs, r)
	b.markDirty()
}

// SetGlyph defines a custom glyph for a rune
// width is the pixel width, pixels are palette indices (row by row, left to right)
// height is automatically calculated from len(pixels)/width
func (b *Buffer) SetGlyph(r rune, width int, pixels []int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.customGlyphs[r] = NewCustomGlyph(width, pixels)
	b.markDirty()
}

// GetGlyph returns the custom glyph for a rune, or nil if none defined
func (b *Buffer) GetGlyph(r rune) *CustomGlyph {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.customGlyphs[r]
}

// HasCustomGlyph returns true if a custom glyph is defined for the rune
func (b *Buffer) HasCustomGlyph(r rune) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	_, ok := b.customGlyphs[r]
	return ok
}

// ResolveGlyphColor resolves a palette index to an actual color for rendering
// cell is the cell being rendered, paletteIdx is the pixel's palette index
// Returns the color to use and whether the pixel should be rendered (false = transparent)
func (b *Buffer) ResolveGlyphColor(cell *Cell, paletteIdx int) (Color, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// Determine which palette to use
	paletteNum := cell.BGP
	if paletteNum < 0 {
		// Use foreground color SGR code as palette number
		// Standard colors: index 0-7 → SGR 30-37, index 8-15 → SGR 90-97
		// 256-color, truecolor, or default → use palette 39 (default foreground)
		switch cell.Foreground.Type {
		case ColorTypeStandard:
			idx := int(cell.Foreground.Index)
			if idx < 8 {
				paletteNum = 30 + idx // SGR 30-37
			} else {
				paletteNum = 90 + (idx - 8) // SGR 90-97
			}
		default:
			paletteNum = 39 // Default foreground palette
		}
	}

	palette := b.palettes[paletteNum]

	// Case 1: Palette doesn't exist - use fallback rendering
	if palette == nil {
		return b.fallbackGlyphColor(cell, paletteIdx)
	}

	// Case 2: Single entry palette - 0=background, 1+=entry
	if len(palette.Entries) == 1 {
		if paletteIdx == 0 {
			return cell.Background, true // Use cell background
		}
		entry := &palette.Entries[0]
		return b.resolveEntry(entry, cell)
	}

	// Case 3: Multi-entry palette - use as-is, clamp out of range
	if paletteIdx < 0 {
		paletteIdx = 0
	}
	if paletteIdx >= len(palette.Entries) {
		paletteIdx = len(palette.Entries) - 1
	}
	entry := &palette.Entries[paletteIdx]
	return b.resolveEntry(entry, cell)
}

// fallbackGlyphColor provides colors when no palette exists
// 0 = background, 1 = foreground, 2 = dim foreground, 3+ = bright foreground
func (b *Buffer) fallbackGlyphColor(cell *Cell, paletteIdx int) (Color, bool) {
	switch paletteIdx {
	case 0:
		return cell.Background, true
	case 1:
		return cell.Foreground, true
	case 2:
		// Dim variant - darken the foreground
		return TrueColor(
			uint8(float64(cell.Foreground.R)*0.6),
			uint8(float64(cell.Foreground.G)*0.6),
			uint8(float64(cell.Foreground.B)*0.6),
		), true
	default:
		// Bright variant - lighten the foreground
		return TrueColor(
			uint8(min(255, int(cell.Foreground.R)+64)),
			uint8(min(255, int(cell.Foreground.G)+64)),
			uint8(min(255, int(cell.Foreground.B)+64)),
		), true
	}
}

// resolveEntry converts a palette entry to a color
func (b *Buffer) resolveEntry(entry *PaletteEntry, cell *Cell) (Color, bool) {
	switch entry.Type {
	case PaletteEntryTransparent:
		return cell.Background, true
	case PaletteEntryDefaultFG:
		if entry.Dim {
			return TrueColor(
				uint8(float64(cell.Foreground.R)*0.6),
				uint8(float64(cell.Foreground.G)*0.6),
				uint8(float64(cell.Foreground.B)*0.6),
			), true
		}
		return cell.Foreground, true
	default:
		color := entry.Color
		if entry.Dim {
			color = TrueColor(
				uint8(float64(color.R)*0.6),
				uint8(float64(color.G)*0.6),
				uint8(float64(color.B)*0.6),
			)
		}
		return color, true
	}
}

// ColorToANSICode returns the ANSI color code for this color.
// For Standard colors (0-15), returns 30-37 or 90-97.
// For Palette colors (0-255), returns the palette index mapped to SGR codes.
// For TrueColor or Default, returns 37 (white) as fallback.
func (b *Buffer) ColorToANSICode(c Color) int {
	switch c.Type {
	case ColorTypeStandard:
		idx := int(c.Index)
		if idx < 8 {
			return 30 + idx
		}
		return 90 + (idx - 8)
	case ColorTypePalette:
		// For palette colors 0-15, treat like standard colors
		idx := int(c.Index)
		if idx < 8 {
			return 30 + idx
		} else if idx < 16 {
			return 90 + (idx - 8)
		}
		// For extended palette colors, return the index directly
		// (caller should use 38;5;N format)
		return idx
	case ColorTypeDefault:
		return 39 // Default foreground code
	case ColorTypeTrueColor:
		// Try to find matching ANSI color by RGB
		for i, ansi := range ANSIColorsRGB {
			if c.R == ansi.R && c.G == ansi.G && c.B == ansi.B {
				if i < 8 {
					return 30 + i
				}
				return 90 + (i - 8)
			}
		}
		return 37 // Fallback to white
	}
	return 37 // Default to white
}
