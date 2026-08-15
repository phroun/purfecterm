package purfecterm

// Animation for the kitty graphics protocol: a=f transmits frame data, a=a
// controls playback.
//
// A client streaming a changing picture — a browser, a video player — does not
// retransmit a whole image per update. It transmits FRAMES against an image it
// already sent and then switches which one is current, which is double
// buffering by another name. That makes animation load-bearing for such a
// client rather than a decoration: refusing it does not degrade the client to
// still images, it stops the client.

// kittyFrame is one frame of an animated image.
type kittyFrame struct {
	bitmap *Bitmap
	gapMS  int
}

// MaxKittyFrames bounds how many frames one image may hold. A client
// transmitting frames without bound is streaming, not animating, and the
// oldest frames past the root are the ones it has already moved on from.
const MaxKittyFrames = 128

// frameAt returns a frame by its 1-based number, or nil.
func (img *kittyImage) frameAt(n int) *kittyFrame {
	if n < 1 || n > len(img.frames) {
		return nil
	}
	return img.frames[n-1]
}

// currentBitmap returns the pixels a placement of this image should show.
func (img *kittyImage) currentBitmap() *Bitmap {
	if f := img.frameAt(img.current); f != nil {
		return f.bitmap
	}
	return img.bitmap
}

// ensureRootFrame makes the base image frame 1, so frame numbering lines up
// with the protocol's before any frame is added.
func (img *kittyImage) ensureRootFrame() {
	if len(img.frames) == 0 {
		img.frames = append(img.frames, &kittyFrame{bitmap: img.bitmap})
		img.current = 1
	}
}

// transmitKittyFrame handles a=f: compose the transmitted rectangle onto a
// canvas and store it as a frame.
func (p *Parser) transmitKittyFrame(cmd kittyCmd, data []byte) {
	img := p.lookupKittyImage(cmd)
	if img == nil {
		p.respondKittyError(cmd, cmd.imageID, "ENOENT", "no such image")
		return
	}
	if cmd.compression == 'z' {
		out, err := inflateZlib(data)
		if err != nil {
			p.respondKittyError(cmd, cmd.imageID, "EINVAL", "bad compressed data")
			return
		}
		data = out
	}

	// s and v are the size of the transmitted RECTANGLE here, not of the whole
	// image, and x,y are where it lands.
	rect := cmd
	if rect.width <= 0 {
		rect.width = img.bitmap.W
	}
	if rect.height <= 0 {
		rect.height = img.bitmap.H
	}
	patch, errCode, msg := kittyBitmap(rect, data)
	if errCode != "" {
		p.respondKittyError(cmd, cmd.imageID, errCode, msg)
		return
	}

	p.buffer.mu.Lock()
	img.ensureRootFrame()

	// The canvas is a previous frame's pixels (c=), the frame being edited
	// (r=), or a fresh one filled with the background colour (Y=).
	var canvas *Bitmap
	switch {
	case cmd.cols > 0 && img.frameAt(cmd.cols) != nil: // c= base frame
		canvas = cloneBitmap(img.frameAt(cmd.cols).bitmap)
	case cmd.rows > 0 && img.frameAt(cmd.rows) != nil: // r= edit in place
		canvas = cloneBitmap(img.frameAt(cmd.rows).bitmap)
	case cmd.cellOffY != 0: // Y= an explicit background colour
		canvas = solidBitmap(img.bitmap.W, img.bitmap.H, cmd.cellOffY)
	default:
		// A frame that names no base is a DIFFERENCE against the picture, so
		// the root is the canvas. Starting from transparent instead left
		// everything the frame did not carry as a hole.
		canvas = cloneBitmap(img.frames[0].bitmap)
	}
	compositeBitmap(canvas, patch, cmd.srcX, cmd.srcY, cmd.cellOffX == 1)

	frame := &kittyFrame{bitmap: canvas, gapMS: cmd.zIndex}
	switch {
	case cmd.rows > 0 && cmd.rows <= MaxKittyFrames:
		// r= NAMES the frame. A client addressing frame 3 while only the root
		// exists means frame 3, not "the next one" — appending instead left
		// its later commands pointing at a frame that was never created. Any
		// gap is filled from the root, which is the picture those frames are
		// differences against.
		for len(img.frames) < cmd.rows {
			img.frames = append(img.frames, &kittyFrame{
				bitmap: cloneBitmap(img.frames[0].bitmap),
			})
		}
		img.frames[cmd.rows-1] = frame
	default:
		if len(img.frames) >= MaxKittyFrames {
			// Drop the oldest non-root frame rather than grow without bound.
			img.frames = append(img.frames[:1], img.frames[2:]...)
			if img.current > 1 {
				img.current--
			}
		}
		img.frames = append(img.frames, frame)
	}
	written := cmd.rows
	if written <= 0 {
		written = len(img.frames)
	}
	p.buffer.showNewestFrame(img, written)
	p.buffer.mu.Unlock()

	p.respondKitty(cmd, img.id, cmd.imageNumber, "OK")
}

// controlKittyAnimation handles a=a: choose the current frame and set playback
// state. Selecting a frame is what a client double buffering with frames uses
// to make its newly transmitted frame the visible one.
func (p *Parser) controlKittyAnimation(cmd kittyCmd) {
	img := p.lookupKittyImage(cmd)
	if img == nil {
		p.respondKittyError(cmd, cmd.imageID, "ENOENT", "no such image")
		return
	}

	p.buffer.mu.Lock()
	img.ensureRootFrame()
	if cmd.rows > 0 && img.frameAt(cmd.rows) != nil {
		img.current = cmd.rows
		img.pinned = true // the client is choosing; stop choosing for it
	}
	if cmd.width > 0 { // s=: 1 stop, 2 run-and-wait, 3 run
		img.running = cmd.width >= 2
	}
	if cmd.zIndex != 0 {
		img.gapMS = cmd.zIndex
	}
	if cmd.cols != 0 {
		img.loops = cmd.cols
	}
	// Every placement of this image shows the frame that is now current.
	shown := img.currentBitmap()
	for _, im := range p.buffer.images {
		if im.ImageID == img.id {
			im.Image = shown
		}
	}
	p.buffer.markDirty()
	p.buffer.mu.Unlock()

	p.respondKitty(cmd, img.id, cmd.imageNumber, "OK")
}

// lookupKittyImage resolves the i= or I= an animation command refers to.
func (p *Parser) lookupKittyImage(cmd kittyCmd) *kittyImage {
	var img *kittyImage
	p.buffer.withKittyStore(func() {
		store := p.buffer.kittyStore()
		if cmd.imageID != 0 {
			img = store.get(cmd.imageID)
		} else if cmd.imageNumber != 0 {
			img = store.byImageNumber(cmd.imageNumber)
		}
	})
	return img
}

// cloneBitmap copies a bitmap's pixels, so composing onto it cannot disturb the
// frame it was based on.
func cloneBitmap(src *Bitmap) *Bitmap {
	if src == nil {
		return &Bitmap{}
	}
	out := &Bitmap{W: src.W, H: src.H, RGBA: make([]byte, len(src.RGBA))}
	copy(out.RGBA, src.RGBA)
	return out
}

// solidBitmap builds a canvas filled with a 32-bit RGBA colour, as the
// protocol's Y key specifies. Zero is transparent black.
func solidBitmap(w, h, rgba int) *Bitmap {
	out := &Bitmap{W: w, H: h, RGBA: make([]byte, w*h*4)}
	if rgba == 0 {
		return out
	}
	r := byte(rgba >> 24)
	g := byte(rgba >> 16)
	b := byte(rgba >> 8)
	a := byte(rgba)
	for i := 0; i < w*h; i++ {
		out.RGBA[i*4+0] = r
		out.RGBA[i*4+1] = g
		out.RGBA[i*4+2] = b
		out.RGBA[i*4+3] = a
	}
	return out
}

// compositeBitmap draws patch onto canvas at (x,y), alpha blending unless
// replace is set. Both carry straight alpha, which is what makes the blend the
// textbook one rather than a premultiplied variant.
func compositeBitmap(canvas, patch *Bitmap, x, y int, replace bool) {
	if canvas == nil || patch == nil {
		return
	}
	for py := 0; py < patch.H; py++ {
		cy := y + py
		if cy < 0 || cy >= canvas.H {
			continue
		}
		for px := 0; px < patch.W; px++ {
			cx := x + px
			if cx < 0 || cx >= canvas.W {
				continue
			}
			si := (py*patch.W + px) * 4
			di := (cy*canvas.W + cx) * 4
			sa := int(patch.RGBA[si+3])
			if replace || sa == 255 {
				copy(canvas.RGBA[di:di+4], patch.RGBA[si:si+4])
				continue
			}
			if sa == 0 {
				continue
			}
			for k := 0; k < 3; k++ {
				dst := int(canvas.RGBA[di+k])
				src := int(patch.RGBA[si+k])
				canvas.RGBA[di+k] = byte((src*sa + dst*(255-sa)) / 255)
			}
			da := int(canvas.RGBA[di+3])
			canvas.RGBA[di+3] = byte(sa + da*(255-sa)/255)
		}
	}
}

// composeKittyFrames handles a=c: copy a rectangle from one frame onto another.
//
// This is how a client updates only what changed — keep the previous frame,
// overlay the damaged region — which is why it turns out to be as load-bearing
// as frame transfer itself for anything streaming a moving picture.
//
// The key letters do NOT mean here what they mean elsewhere in the protocol,
// which is a wart worth naming: x,y are the DESTINATION edge (against a=f,
// where they are where the transmitted data lands), X,Y are the SOURCE edge
// (against a=f, where X is a replace flag and Y a background colour), and C is
// the composition mode (against a placement, where it suppresses cursor
// movement).
func (p *Parser) composeKittyFrames(cmd kittyCmd) {
	img := p.lookupKittyImage(cmd)
	if img == nil {
		p.respondKittyError(cmd, cmd.imageID, "ENOENT", "no such image")
		return
	}

	p.buffer.mu.Lock()
	img.ensureRootFrame()
	dst := img.frameAt(cmd.rows) // r= the frame being edited
	src := img.frameAt(cmd.cols) // c= the frame supplying the pixels
	if dst == nil || src == nil {
		p.buffer.mu.Unlock()
		p.respondKittyError(cmd, cmd.imageID, "ENOENT", "no such frame")
		return
	}

	w, h := cmd.srcW, cmd.srcH
	if w <= 0 {
		w = src.bitmap.W
	}
	if h <= 0 {
		h = src.bitmap.H
	}
	// Editing a frame must not disturb anything still pointing at its pixels,
	// so the edit lands on a copy that replaces it.
	edited := cloneBitmap(dst.bitmap)
	copyBitmapRect(edited, src.bitmap,
		cmd.cellOffX, cmd.cellOffY, // X,Y: source edge
		cmd.srcX, cmd.srcY, // x,y: destination edge
		w, h, cmd.noCursorMove) // C=1: overwrite rather than blend
	dst.bitmap = edited

	// The edited frame becomes the one on show, so a composed damage region
	// reaches the screen rather than sitting in a frame nothing displays.
	p.buffer.showNewestFrame(img, cmd.rows)
	if img.current == cmd.rows {
		for _, im := range p.buffer.images {
			if im.ImageID == img.id {
				im.Image = edited
			}
		}
		p.buffer.markDirty()
	}
	p.buffer.mu.Unlock()

	p.respondKitty(cmd, img.id, cmd.imageNumber, "OK")
}

// copyBitmapRect moves a w x h rectangle from src at (sx,sy) onto dst at
// (dx,dy), blending unless replace is set. Both carry straight alpha.
func copyBitmapRect(dst, src *Bitmap, sx, sy, dx, dy, w, h int, replace bool) {
	if dst == nil || src == nil {
		return
	}
	for row := 0; row < h; row++ {
		syr, dyr := sy+row, dy+row
		if syr < 0 || syr >= src.H || dyr < 0 || dyr >= dst.H {
			continue
		}
		for col := 0; col < w; col++ {
			sxc, dxc := sx+col, dx+col
			if sxc < 0 || sxc >= src.W || dxc < 0 || dxc >= dst.W {
				continue
			}
			si := (syr*src.W + sxc) * 4
			di := (dyr*dst.W + dxc) * 4
			sa := int(src.RGBA[si+3])
			if replace || sa == 255 {
				copy(dst.RGBA[di:di+4], src.RGBA[si:si+4])
				continue
			}
			if sa == 0 {
				continue
			}
			for k := 0; k < 3; k++ {
				d := int(dst.RGBA[di+k])
				s := int(src.RGBA[si+k])
				dst.RGBA[di+k] = byte((s*sa + d*(255-sa)) / 255)
			}
			da := int(dst.RGBA[di+3])
			dst.RGBA[di+3] = byte(sa + da*(255-sa)/255)
		}
	}
}

// showNewestFrame makes the frame just written the visible one, unless the
// client has pinned a frame itself with a=a r=.
//
// A terminal that cannot run a playback clock still has to decide WHICH frame a
// placement shows. Holding the root is the one indefensible choice: a client
// that transmits frames is transmitting them to be seen, and a browser's root
// frame is whatever its renderer had before it had drawn anything — blank. So
// the newest frame wins by default, which is also what a real playback would
// settle on once a finite loop finished. A client that selects frames
// explicitly keeps that choice. Caller holds the lock.
func (b *Buffer) showNewestFrame(img *kittyImage, frame int) {
	if img.pinned || frame < 1 || frame > len(img.frames) {
		return
	}
	img.current = frame
	shown := img.currentBitmap()
	for _, im := range b.images {
		if im.ImageID == img.id {
			im.Image = shown
		}
	}
	b.markDirty()
}
