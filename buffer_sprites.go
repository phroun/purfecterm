package purfecterm

// --- Sprite Overlay System Methods ---

// SetSpriteUnits sets how many subdivisions per cell for sprite coordinates
func (b *Buffer) SetSpriteUnits(unitX, unitY int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if unitX > 0 {
		b.spriteUnitX = unitX
	}
	if unitY > 0 {
		b.spriteUnitY = unitY
	}
	b.markDirty()
}

// GetSpriteUnits returns the subdivisions per cell for sprite coordinates
func (b *Buffer) GetSpriteUnits() (unitX, unitY int) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.spriteUnitX, b.spriteUnitY
}

// DeleteAllSprites removes all sprites
func (b *Buffer) DeleteAllSprites() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sprites = make(map[int]*Sprite)
	b.markDirty()
}

// DeleteSprite removes a specific sprite
func (b *Buffer) DeleteSprite(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.sprites, id)
	b.markDirty()
}

// SetSprite creates or updates a sprite
func (b *Buffer) SetSprite(id int, x, y float64, zIndex, fgp, flipCode int, xScale, yScale float64, cropRect int, runes []rune) {
	b.mu.Lock()
	defer b.mu.Unlock()

	sprite := NewSprite(id)
	sprite.X = x
	sprite.Y = y
	sprite.ZIndex = zIndex
	sprite.FGP = fgp
	sprite.FlipCode = flipCode
	sprite.XScale = xScale
	sprite.YScale = yScale
	sprite.CropRect = cropRect
	sprite.SetRunes(runes)

	b.sprites[id] = sprite
	b.markDirty()
}

// GetSprite returns a sprite by ID, or nil if not found
func (b *Buffer) GetSprite(id int) *Sprite {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.sprites[id]
}

// MoveSprite updates only the position of an existing sprite
// Returns false if sprite doesn't exist
func (b *Buffer) MoveSprite(id int, x, y float64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	sprite := b.sprites[id]
	if sprite == nil {
		return false
	}
	sprite.X = x
	sprite.Y = y
	b.markDirty()
	return true
}

// UpdateSpriteRunes updates only the runes of an existing sprite
// Returns false if sprite doesn't exist
func (b *Buffer) UpdateSpriteRunes(id int, runes []rune) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	sprite := b.sprites[id]
	if sprite == nil {
		return false
	}
	sprite.SetRunes(runes)
	b.markDirty()
	return true
}

// MoveSpriteAndRunes updates position and runes of an existing sprite
// Returns false if sprite doesn't exist
func (b *Buffer) MoveSpriteAndRunes(id int, x, y float64, runes []rune) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	sprite := b.sprites[id]
	if sprite == nil {
		return false
	}
	sprite.X = x
	sprite.Y = y
	sprite.SetRunes(runes)
	b.markDirty()
	return true
}

// GetSpritesForRendering returns sprites sorted by Z-index and ID for rendering
// Returns two slices: behind (negative Z) and front (non-negative Z)
func (b *Buffer) GetSpritesForRendering() (behind, front []*Sprite) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	behind = make([]*Sprite, 0)
	front = make([]*Sprite, 0)

	for _, sprite := range b.sprites {
		if sprite.ZIndex < 0 {
			behind = append(behind, sprite)
		} else {
			front = append(front, sprite)
		}
	}

	// Sort by Z-index, then by ID
	sortSprites := func(sprites []*Sprite) {
		for i := 0; i < len(sprites); i++ {
			for j := i + 1; j < len(sprites); j++ {
				if sprites[i].ZIndex > sprites[j].ZIndex ||
					(sprites[i].ZIndex == sprites[j].ZIndex && sprites[i].ID > sprites[j].ID) {
					sprites[i], sprites[j] = sprites[j], sprites[i]
				}
			}
		}
	}

	sortSprites(behind)
	sortSprites(front)

	return behind, front
}

// ResolveSpriteGlyphColor resolves a palette index to a color for sprite rendering
// Similar to ResolveGlyphColor but uses sprite's FGP and handles transparency differently
// Returns the color and whether the pixel should be rendered (false = transparent)
func (b *Buffer) ResolveSpriteGlyphColor(fgp int, paletteIdx int, defaultFg, defaultBg Color) (Color, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// Determine which palette to use
	paletteNum := fgp
	if paletteNum < 0 {
		// Use a default palette number (37 = white foreground)
		paletteNum = 37
	}

	palette := b.palettes[paletteNum]

	// Case 1: Palette doesn't exist - use fallback rendering
	if palette == nil {
		return b.fallbackSpriteColor(paletteIdx, defaultFg, defaultBg)
	}

	// Case 2: Single entry palette - 0=transparent, 1+=entry
	if len(palette.Entries) == 1 {
		if paletteIdx == 0 {
			return Color{}, false // Transparent
		}
		entry := &palette.Entries[0]
		return b.resolveSpriteEntry(entry, defaultFg, defaultBg)
	}

	// Case 3: Multi-entry palette - use as-is, clamp out of range
	if paletteIdx < 0 {
		paletteIdx = 0
	}
	if paletteIdx >= len(palette.Entries) {
		paletteIdx = len(palette.Entries) - 1
	}
	entry := &palette.Entries[paletteIdx]
	return b.resolveSpriteEntry(entry, defaultFg, defaultBg)
}

// fallbackSpriteColor provides colors for sprites when no palette exists
// 0 = transparent, 1 = foreground, 2 = dim foreground, 3+ = bright foreground
func (b *Buffer) fallbackSpriteColor(paletteIdx int, fg, bg Color) (Color, bool) {
	switch paletteIdx {
	case 0:
		return Color{}, false // Transparent
	case 1:
		return fg, true
	case 2:
		// Dim variant
		return TrueColor(
			uint8(float64(fg.R)*0.6),
			uint8(float64(fg.G)*0.6),
			uint8(float64(fg.B)*0.6),
		), true
	default:
		// Bright variant
		return TrueColor(
			uint8(min(255, int(fg.R)+64)),
			uint8(min(255, int(fg.G)+64)),
			uint8(min(255, int(fg.B)+64)),
		), true
	}
}

// resolveSpriteEntry converts a palette entry to a color for sprites
func (b *Buffer) resolveSpriteEntry(entry *PaletteEntry, fg, bg Color) (Color, bool) {
	switch entry.Type {
	case PaletteEntryTransparent:
		return Color{}, false // Transparent for sprites
	case PaletteEntryDefaultFG:
		if entry.Dim {
			return TrueColor(
				uint8(float64(fg.R)*0.6),
				uint8(float64(fg.G)*0.6),
				uint8(float64(fg.B)*0.6),
			), true
		}
		return fg, true
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

// --- Crop Rectangle Methods ---

// DeleteAllCropRects removes all crop rectangles
func (b *Buffer) DeleteAllCropRects() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cropRects = make(map[int]*CropRectangle)
	b.markDirty()
}

// DeleteCropRect removes a specific crop rectangle
func (b *Buffer) DeleteCropRect(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.cropRects, id)
	b.markDirty()
}

// SetCropRect creates or updates a crop rectangle
func (b *Buffer) SetCropRect(id int, minX, minY, maxX, maxY float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cropRects[id] = NewCropRectangle(id, minX, minY, maxX, maxY)
	b.markDirty()
}

// GetCropRect returns a crop rectangle by ID, or nil if not found
func (b *Buffer) GetCropRect(id int) *CropRectangle {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.cropRects[id]
}

// --- Screen Crop Methods ---

// SetScreenCrop sets the width and height crop in sprite coordinate units.
// -1 means no crop for that dimension.
func (b *Buffer) SetScreenCrop(widthCrop, heightCrop int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.widthCrop = widthCrop
	b.heightCrop = heightCrop
	b.markDirty()
}

// GetScreenCrop returns the current width and height crop values.
// -1 means no crop for that dimension.
func (b *Buffer) GetScreenCrop() (widthCrop, heightCrop int) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.widthCrop, b.heightCrop
}

// ClearScreenCrop removes both width and height crops.
func (b *Buffer) ClearScreenCrop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.widthCrop = -1
	b.heightCrop = -1
	b.markDirty()
}
