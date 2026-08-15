package purfecterm

// Deletion for the kitty graphics protocol (a=d).
//
// Every target has a lowercase and an uppercase form. Both remove the same
// PLACEMENTS; the uppercase form additionally frees the stored image, provided
// nothing else still places it. That distinction is the whole point of keeping
// storage separate from placement: a client can clear the screen and re-show an
// image later without retransmitting it.

import "strings"

// deleteKittyImages handles a=d.
func (p *Parser) deleteKittyImages(cmd kittyCmd) {
	what := cmd.deleteWhat
	if what == 0 {
		what = 'a'
	}
	freeData := what >= 'A' && what <= 'Z'
	target := byte(strings.ToLower(string(what))[0])

	row, col := p.buffer.CursorRowCol()
	var match func(*PlacedImage) bool

	switch target {
	case 'a': // every placement
		match = func(*PlacedImage) bool { return true }
	case 'i': // by image ID, optionally narrowed to one placement
		match = func(im *PlacedImage) bool {
			if im.ImageID != cmd.imageID {
				return false
			}
			return cmd.placementID == 0 || im.PlacementID == cmd.placementID
		}
	case 'n': // by image number: the most recent image carrying it
		var id uint32
		p.buffer.withKittyStore(func() {
			if img := p.buffer.kittyStore().byImageNumber(cmd.imageNumber); img != nil {
				id = img.id
			}
		})
		if id == 0 {
			p.respondKitty(cmd, cmd.imageID, cmd.imageNumber, "OK")
			return
		}
		match = func(im *PlacedImage) bool {
			if im.ImageID != id {
				return false
			}
			return cmd.placementID == 0 || im.PlacementID == cmd.placementID
		}
	case 'c': // placements covering the cursor cell
		match = func(im *PlacedImage) bool { return placementCovers(im, col, row) }
	case 'p': // placements covering the cell at x,y (1-based in the protocol)
		x, y := protocolCell(cmd.srcX, cmd.hasSrcX), protocolCell(cmd.srcY, cmd.hasSrcY)
		match = func(im *PlacedImage) bool { return placementCovers(im, x, y) }
	case 'q': // as p, but only at a given z-index
		x, y := protocolCell(cmd.srcX, cmd.hasSrcX), protocolCell(cmd.srcY, cmd.hasSrcY)
		match = func(im *PlacedImage) bool {
			return placementCovers(im, x, y) && im.ZIndex == cmd.zIndex
		}
	case 'x': // every placement intersecting a column
		x := protocolCell(cmd.srcX, cmd.hasSrcX)
		match = func(im *PlacedImage) bool { return im.Col <= x && x < im.Col+im.CellsWide }
	case 'y': // every placement intersecting a row
		y := protocolCell(cmd.srcY, cmd.hasSrcY)
		match = func(im *PlacedImage) bool { return im.Row <= y && y < im.Row+im.CellsHigh }
	case 'z': // every placement at a z-index
		match = func(im *PlacedImage) bool { return im.ZIndex == cmd.zIndex }
	case 'r': // images whose ID falls in [x, y]
		lo, hi := uint32(cmd.srcX), uint32(cmd.srcY)
		if lo > hi {
			lo, hi = hi, lo
		}
		match = func(im *PlacedImage) bool { return im.ImageID >= lo && im.ImageID <= hi }
	case 'f': // animation frames: nothing is stored, so nothing to remove
		p.respondKitty(cmd, cmd.imageID, cmd.imageNumber, "OK")
		return
	default:
		p.respondKittyError(cmd, cmd.imageID, "EINVAL", "unknown delete target")
		return
	}

	// Collect the image IDs losing a placement before removing, so the
	// uppercase form knows which stored images to consider freeing.
	touched := map[uint32]bool{}
	for _, im := range p.buffer.GetImages() {
		if match(im) && im.ImageID != 0 {
			touched[im.ImageID] = true
		}
	}
	p.buffer.DeleteImagesFunc(match)

	if freeData {
		for id := range touched {
			// "provided that the image is not referenced elsewhere": a
			// placement that survived this delete still needs the pixels.
			if !p.buffer.HasPlacementFor(id) {
				p.buffer.removeKittyImage(id)
			}
		}
		if target == 'r' {
			lo, hi := uint32(cmd.srcX), uint32(cmd.srcY)
			if lo > hi {
				lo, hi = hi, lo
			}
			p.buffer.removeKittyImageRange(lo, hi)
		}
	}
	p.respondKitty(cmd, cmd.imageID, cmd.imageNumber, "OK")
}

// protocolCell converts a 1-based protocol cell coordinate to the 0-based one
// the buffer uses. An absent key means the cursor's own position, which the
// caller has already substituted, so only a present key is shifted.
func protocolCell(v int, present bool) int {
	if !present {
		return v
	}
	if v > 0 {
		return v - 1
	}
	return v
}

// placementCovers reports whether a placement's cell box contains (col,row).
func placementCovers(im *PlacedImage, col, row int) bool {
	return im.Col <= col && col < im.Col+im.CellsWide &&
		im.Row <= row && row < im.Row+im.CellsHigh
}
