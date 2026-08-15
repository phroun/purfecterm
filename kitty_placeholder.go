package purfecterm

// Unicode placeholders for the kitty graphics protocol.
//
// A virtual placement (U=1) is not drawn where it was created. Instead the
// client prints cells containing U+10EEEE, and the image is drawn INTO those
// cells. That lets an application which lays out text — a multiplexer, a pager —
// position an image using nothing but ordinary character cells, without knowing
// anything about graphics.
//
// The image ID rides in the cell's FOREGROUND COLOR, and the row and column
// within the image are encoded as combining diacritics after the placeholder.
// A cell with no diacritic inherits from the cell before it, which is what keeps
// a wide image from needing an explicit mark on every cell.

// KittyPlaceholderRune is the character a client prints to stand in for one
// cell of a virtual image placement.
const KittyPlaceholderRune = rune(0x10EEEE)

// kittyDiacritics maps a combining mark to the number it encodes. This is the
// protocol's fixed list, whose index IS the value; it is shared with kitty's
// rowcolumn-diacritics table.
var kittyDiacritics = []rune{
	0x0305, 0x030D, 0x030E, 0x0310, 0x0312, 0x033D, 0x033E, 0x033F,
	0x0346, 0x034A, 0x034B, 0x034C, 0x0350, 0x0351, 0x0352, 0x0357,
	0x035B, 0x0363, 0x0364, 0x0365, 0x0366, 0x0367, 0x0368, 0x0369,
	0x036A, 0x036B, 0x036C, 0x036D, 0x036E, 0x036F, 0x0483, 0x0484,
	0x0485, 0x0486, 0x0487, 0x0592, 0x0593, 0x0594, 0x0595, 0x0597,
	0x0598, 0x0599, 0x059C, 0x059D, 0x059E, 0x059F, 0x05A0, 0x05A1,
	0x05A8, 0x05A9, 0x05AB, 0x05AC, 0x05AF, 0x05C4, 0x0610, 0x0611,
	0x0612, 0x0613, 0x0614, 0x0615, 0x0616, 0x0617, 0x0657, 0x0658,
	0x0659, 0x065A, 0x065B, 0x065D, 0x065E, 0x06D6, 0x06D7, 0x06D8,
	0x06D9, 0x06DA, 0x06DB, 0x06DC, 0x06DF, 0x06E0, 0x06E1, 0x06E2,
	0x06E4, 0x06E7, 0x06E8, 0x06EB, 0x06EC, 0x0730, 0x0732, 0x0733,
	0x0735, 0x0736, 0x073A, 0x073D, 0x073F, 0x0740, 0x0741, 0x0743,
	0x0745, 0x0747, 0x0749, 0x074A, 0x07EB, 0x07EC, 0x07ED, 0x07EE,
	0x07EF, 0x07F0, 0x07F1, 0x07F3, 0x0816, 0x0817, 0x0818, 0x0819,
	0x081B, 0x081C, 0x081D, 0x081E, 0x081F, 0x0820, 0x0821, 0x0822,
	0x0823, 0x0825, 0x0826, 0x0827, 0x0829, 0x082A, 0x082B, 0x082C,
	0x082D, 0x0951, 0x0953, 0x0954, 0x0F82, 0x0F83, 0x0F86, 0x0F87,
	0x135D, 0x135E, 0x135F, 0x17DD, 0x193A, 0x1A17, 0x1A75, 0x1A76,
	0x1A77, 0x1A78, 0x1A79, 0x1A7A, 0x1A7B, 0x1A7C, 0x1B6B, 0x1B6D,
	0x1B6E, 0x1B6F, 0x1B70, 0x1B71, 0x1B72, 0x1B73, 0x1CD0, 0x1CD1,
	0x1CD2, 0x1CDA, 0x1CDB, 0x1CE0, 0x1DC0, 0x1DC1, 0x1DC3, 0x1DC4,
	0x1DC5, 0x1DC6, 0x1DC7, 0x1DC8, 0x1DC9, 0x1DCB, 0x1DCC, 0x1DD1,
	0x1DD2, 0x1DD3, 0x1DD4, 0x1DD5, 0x1DD6, 0x1DD7, 0x1DD8, 0x1DD9,
	0x1DDA, 0x1DDB, 0x1DDC, 0x1DDD, 0x1DDE, 0x1DDF, 0x1DE0, 0x1DE1,
	0x1DE2, 0x1DE3, 0x1DE4, 0x1DE5, 0x1DE6, 0x1DFE, 0x20D0, 0x20D1,
	0x20D4, 0x20D5, 0x20D6, 0x20D7, 0x20DB, 0x20DC, 0x20E1, 0x20E7,
	0x20E9, 0x20F0, 0x2CEF, 0x2CF0, 0x2CF1, 0x2DE0, 0x2DE1, 0x2DE2,
	0x2DE3, 0x2DE4, 0x2DE5, 0x2DE6, 0x2DE7, 0x2DE8, 0x2DE9, 0x2DEA,
	0x2DEB, 0x2DEC, 0x2DED, 0x2DEE, 0x2DEF, 0x2DF0, 0x2DF1, 0x2DF2,
	0x2DF3, 0x2DF4, 0x2DF5, 0x2DF6, 0x2DF7, 0x2DF8, 0x2DF9, 0x2DFA,
	0x2DFB, 0x2DFC, 0x2DFD, 0x2DFE, 0x2DFF, 0xA66F, 0xA67C, 0xA67D,
	0xA6F0, 0xA6F1, 0xA8E0, 0xA8E1, 0xA8E2, 0xA8E3, 0xA8E4, 0xA8E5,
	0xA8E6, 0xA8E7, 0xA8E8, 0xA8E9, 0xA8EA, 0xA8EB, 0xA8EC, 0xA8ED,
	0xA8EE, 0xA8EF, 0xA8F0, 0xA8F1, 0xAAB0, 0xAAB2, 0xAAB3, 0xAAB7,
	0xAAB8, 0xAABE, 0xAABF, 0xAAC1, 0xFE20, 0xFE21, 0xFE22, 0xFE23,
	0xFE24, 0xFE25, 0xFE26, 0x10A0F, 0x10A38, 0x1D185, 0x1D186, 0x1D187,
	0x1D188, 0x1D189, 0x1D1AA, 0x1D1AB, 0x1D1AC, 0x1D1AD, 0x1D242,
	0x1D243, 0x1D244,
}

// kittyDiacriticValue is the reverse index, built once.
var kittyDiacriticValue = func() map[rune]int {
	m := make(map[rune]int, len(kittyDiacritics))
	for i, r := range kittyDiacritics {
		m[r] = i
	}
	return m
}()

// KittyDiacriticValue returns the number a combining mark encodes, and whether
// the rune is one of the protocol's marks at all.
func KittyDiacriticValue(r rune) (int, bool) {
	v, ok := kittyDiacriticValue[r]
	return v, ok
}

// KittyDiacriticFor returns the combining mark encoding n, for a client or a
// test building placeholder cells.
func KittyDiacriticFor(n int) (rune, bool) {
	if n < 0 || n >= len(kittyDiacritics) {
		return 0, false
	}
	return kittyDiacritics[n], true
}

// KittyPlaceholder describes one placeholder cell: which virtual placement it
// belongs to and which cell of that image it shows.
type KittyPlaceholder struct {
	ImageID  uint32
	Row, Col int // position within the image, 0-based
	HasRow   bool
	HasCol   bool
}

// DecodeKittyPlaceholder reads a placeholder cell. base is the cell's rune,
// combining holds any combining marks that followed it, and fg is the cell's
// foreground color, which carries the image ID. It reports whether the cell is
// a placeholder at all.
//
// Row and column may be absent, in which case the caller inherits them from the
// preceding cell — the protocol's shorthand for a run of cells across one row.
func DecodeKittyPlaceholder(base rune, combining []rune, fg Color) (KittyPlaceholder, bool) {
	if base != KittyPlaceholderRune {
		return KittyPlaceholder{}, false
	}
	ph := KittyPlaceholder{ImageID: kittyIDFromColor(fg)}
	for i, c := range combining {
		v, ok := KittyDiacriticValue(c)
		if !ok {
			continue
		}
		switch i {
		case 0:
			ph.Row, ph.HasRow = v, true
		case 1:
			ph.Col, ph.HasCol = v, true
		case 2:
			// The third diacritic carries the high byte of the image ID, for
			// IDs that do not fit in a 24-bit color.
			ph.ImageID |= uint32(v) << 24
		}
	}
	return ph, true
}

// kittyIDFromColor extracts the image ID a placeholder cell's foreground color
// encodes. The protocol packs the low 24 bits of the ID into an RGB color.
func kittyIDFromColor(fg Color) uint32 {
	r, g, b := fg.R, fg.G, fg.B
	return uint32(r)<<16 | uint32(g)<<8 | uint32(b)
}

// KittyVirtualPlacement finds the virtual placement a placeholder cell refers
// to, or nil when the image was never transmitted or was deleted.
func (b *Buffer) KittyVirtualPlacement(imageID uint32) *PlacedImage {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, im := range b.images {
		if im.Virtual && im.ImageID == imageID {
			return im
		}
	}
	return nil
}
