package purfecterm

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
