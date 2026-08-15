package purfecterm

// The kitty graphics protocol (APC _G ... ST).
//
//	ESC _ G <comma-separated key=value control data> ; <base64 payload> ESC \
//
// Unlike Sixel and the iTerm2 inline images protocol, an image here is
// addressable: it is transmitted once under an ID and then PLACED, possibly
// several times, and either the image or a placement can be deleted later. That
// is why storage (kitty_graphics_store.go) is separate from placement.

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// MaxKittyPayloadBytes caps one assembled image payload. Chunked transmission
// invites a client to stream without end, so the accumulator is bounded on the
// same reasoning as the OSC 52 clipboard limit.
const MaxKittyPayloadBytes = 64 << 20 // 64 MiB

// MaxKittyPixels caps the pixel count of a decoded image, so that a small
// header claiming enormous dimensions cannot make the terminal allocate them.
const MaxKittyPixels = 64 << 20 // 64M pixels

// kittyCmd is one parsed control-data block.
type kittyCmd struct {
	action byte // a
	format int  // f: 24, 32, 100
	medium byte // t: d, f, t, s
	quiet  int  // q: 1 suppress OK, 2 suppress errors too

	width, height int // s, v: pixel dimensions of raw data
	dataSize      int // S
	dataOffset    int // O
	compression   byte

	more bool // m=1: another chunk follows

	imageID     uint32 // i
	imageNumber uint32 // I
	placementID uint32 // p

	srcX, srcY, srcW, srcH int // x, y, w, h
	cellOffX, cellOffY     int // X, Y
	cols, rows             int // c, r
	zIndex                 int // z
	noCursorMove           bool
	deleteWhat             byte // d
	virtual                bool // U

	parentImage     uint32 // P
	parentPlacement uint32 // Q
	relH, relV      int    // H, V

	hasImageID  bool
	hasDeleteZ  bool
	hasSrcX     bool
	hasSrcY     bool
	hasZ        bool
	rawPayload  string
	parseFailed bool
}

// parseKittyCmd reads the control data preceding the payload.
func parseKittyCmd(control, payload string) kittyCmd {
	c := kittyCmd{
		action: 'T', format: 32, medium: 'd', rawPayload: payload,
	}
	for _, field := range strings.Split(control, ",") {
		if field == "" {
			continue
		}
		key, value, ok := strings.Cut(field, "=")
		if !ok || key == "" {
			continue
		}
		num := func() int {
			n, err := strconv.Atoi(value)
			if err != nil {
				c.parseFailed = true
				return 0
			}
			return n
		}
		u32 := func() uint32 {
			n, err := strconv.ParseUint(value, 10, 32)
			if err != nil {
				c.parseFailed = true
				return 0
			}
			return uint32(n)
		}
		letter := func() byte {
			if len(value) != 1 {
				c.parseFailed = true
				return 0
			}
			return value[0]
		}
		switch key {
		case "a":
			c.action = letter()
		case "q":
			c.quiet = num()
		case "f":
			c.format = num()
		case "t":
			c.medium = letter()
		case "s":
			c.width = num()
		case "v":
			c.height = num()
		case "S":
			c.dataSize = num()
		case "O":
			c.dataOffset = num()
		case "o":
			c.compression = letter()
		case "m":
			c.more = num() == 1
		case "i":
			c.imageID, c.hasImageID = u32(), true
		case "I":
			c.imageNumber = u32()
		case "p":
			c.placementID = u32()
		case "x":
			c.srcX, c.hasSrcX = num(), true
		case "y":
			c.srcY, c.hasSrcY = num(), true
		case "w":
			c.srcW = num()
		case "h":
			c.srcH = num()
		case "X":
			c.cellOffX = num()
		case "Y":
			c.cellOffY = num()
		case "c":
			c.cols = num()
		case "r":
			c.rows = num()
		case "z":
			c.zIndex, c.hasZ = num(), true
		case "C":
			c.noCursorMove = num() == 1
		case "d":
			c.deleteWhat = letter()
			c.hasDeleteZ = true
		case "U":
			c.virtual = num() == 1
		case "P":
			c.parentImage = u32()
		case "Q":
			c.parentPlacement = u32()
		case "H":
			c.relH = num()
		case "V":
			c.relV = num()
		}
	}
	return c
}

// kittyTransfer is an in-flight chunked transmission.
type kittyTransfer struct {
	cmd  kittyCmd
	data []byte
}

// executeKittyGraphics handles one APC _G command. body is everything after the
// leading "G".
func (p *Parser) executeKittyGraphics(body string) {
	control, payload, _ := strings.Cut(body, ";")
	cmd := parseKittyCmd(control, payload)
	if GraphicsLoggingEnabled() {
		logGraphics("APC _G %s  (payload %d bytes)", control, len(payload))
	}

	// A continuation chunk carries only m= (and q=), except for an animation
	// frame, which repeats a=f on every chunk. Keying on m= rather than on the
	// absence of a= is what lets a chunked FRAME reassemble instead of being
	// read as a fresh command per chunk.
	if p.kittyXfer != nil && control != "" && strings.Contains(control, "m=") {
		p.appendKittyChunk(cmd)
		return
	}
	if p.kittyXfer != nil && control == "" {
		p.appendKittyChunk(cmd)
		return
	}

	switch cmd.action {
	case 'q': // query support without storing
		p.respondKitty(cmd, cmd.imageID, 0, "OK")
	case 't', 'T':
		p.beginKittyTransfer(cmd)
	case 'p':
		p.placeStoredKittyImage(cmd)
	case 'd':
		p.deleteKittyImages(cmd)
	case 'f': // transmit frame data
		p.beginKittyTransfer(cmd)
	case 'a': // animation control
		p.controlKittyAnimation(cmd)
	case 'c': // compose one frame onto another
		p.composeKittyFrames(cmd)
	default:
		p.respondKittyError(cmd, cmd.imageID, "EINVAL", "unsupported action")
	}
}

// beginKittyTransfer starts (and, when unchunked, completes) a transmission.
func (p *Parser) beginKittyTransfer(cmd kittyCmd) {
	if cmd.parseFailed {
		p.respondKittyError(cmd, cmd.imageID, "EINVAL", "bad control data")
		return
	}
	data, err := decodeKittyPayload(cmd)
	if err != "" {
		p.respondKittyError(cmd, cmd.imageID, err, "payload")
		return
	}
	if cmd.more {
		p.kittyXfer = &kittyTransfer{cmd: cmd, data: data}
		return
	}
	p.finishKittyTransfer(cmd, data)
}

// appendKittyChunk adds a continuation chunk, completing on m=0.
func (p *Parser) appendKittyChunk(cmd kittyCmd) {
	xfer := p.kittyXfer
	if xfer == nil {
		return
	}
	data, errCode := decodeKittyPayload(kittyCmd{
		medium: 'd', rawPayload: cmd.rawPayload, compression: 0,
	})
	if errCode != "" {
		p.kittyXfer = nil
		p.respondKittyError(xfer.cmd, xfer.cmd.imageID, errCode, "chunk")
		return
	}
	if len(xfer.data)+len(data) > MaxKittyPayloadBytes {
		p.kittyXfer = nil
		p.respondKittyError(xfer.cmd, xfer.cmd.imageID, "EINVAL", "payload too large")
		return
	}
	xfer.data = append(xfer.data, data...)
	if cmd.more {
		return
	}
	p.kittyXfer = nil
	p.finishKittyTransfer(xfer.cmd, xfer.data)
}

// finishKittyTransfer decodes an assembled payload into an image, stores it,
// and places it when the action asked for display.
func (p *Parser) finishKittyTransfer(cmd kittyCmd, data []byte) {
	if cmd.action == 'f' {
		// A frame carries its own decompression and composition.
		p.transmitKittyFrame(cmd, data)
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
	bm, errCode, msg := kittyBitmap(cmd, data)
	if errCode != "" {
		p.respondKittyError(cmd, cmd.imageID, errCode, msg)
		return
	}

	store := p.buffer.kittyStore()
	id := cmd.imageID
	if id == 0 {
		if cmd.imageNumber != 0 {
			if existing := store.byImageNumber(cmd.imageNumber); existing != nil {
				id = existing.id
			}
		}
		if id == 0 {
			id = store.allocID()
		}
	}
	img := &kittyImage{id: id, number: cmd.imageNumber, bitmap: bm}
	p.buffer.putKittyImage(img)

	if cmd.action == 'T' {
		p.placeKittyImage(cmd, img)
	}
	p.respondKitty(cmd, id, cmd.imageNumber, "OK")
}

// kittyBitmap turns decoded bytes into a Bitmap according to f=.
func kittyBitmap(cmd kittyCmd, data []byte) (*Bitmap, string, string) {
	switch cmd.format {
	case 100: // PNG (and, through the registry, whatever else is registered)
		bm, _, err := DecodeImage(data)
		if err != nil {
			return nil, "EINVAL", "undecodable image"
		}
		return bm, "", ""
	case 24, 32:
		w, h := cmd.width, cmd.height
		if w <= 0 || h <= 0 {
			return nil, "EINVAL", "raw data needs s= and v="
		}
		if w*h > MaxKittyPixels {
			return nil, "EINVAL", "image too large"
		}
		stride := 3
		if cmd.format == 32 {
			stride = 4
		}
		if len(data) < w*h*stride {
			return nil, "EINVAL", "short pixel data"
		}
		rgba := make([]byte, w*h*4)
		for i := 0; i < w*h; i++ {
			rgba[i*4+0] = data[i*stride+0]
			rgba[i*4+1] = data[i*stride+1]
			rgba[i*4+2] = data[i*stride+2]
			if stride == 4 {
				rgba[i*4+3] = data[i*stride+3]
			} else {
				rgba[i*4+3] = 255
			}
		}
		return &Bitmap{W: w, H: h, RGBA: rgba}, "", ""
	}
	return nil, "EINVAL", "unsupported format"
}

// decodeKittyPayload resolves the payload for the transmission medium in use.
func decodeKittyPayload(cmd kittyCmd) ([]byte, string) {
	raw, err := decodeKittyBase64(cmd.rawPayload)
	if err != nil {
		return nil, "EINVAL"
	}
	switch cmd.medium {
	case 'd', 0:
		return raw, ""
	case 'f', 't', 's':
		data, code := readKittyExternal(cmd, string(raw))
		return data, code
	}
	return nil, "EINVAL"
}

// inflateZlib decompresses an o=z payload.
func inflateZlib(data []byte) ([]byte, error) {
	zr, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(io.LimitReader(zr, MaxKittyPayloadBytes))
}

// placeKittyImage creates a placement from a transmit-and-display command.
func (p *Parser) placeKittyImage(cmd kittyCmd, img *kittyImage) {
	pi := p.buildKittyPlacement(cmd, img)
	if pi == nil {
		return
	}
	p.buffer.PlaceKittyImage(pi)
	if !cmd.noCursorMove && !pi.Virtual {
		p.buffer.advanceCursorForImage(pi.CellsWide, pi.CellsHigh)
	}
}

// placeStoredKittyImage handles a=p, placing an already-transmitted image.
func (p *Parser) placeStoredKittyImage(cmd kittyCmd) {
	store := p.buffer.kittyStore()
	var img *kittyImage
	p.buffer.withKittyStore(func() {
		if cmd.imageID != 0 {
			img = store.get(cmd.imageID)
		} else if cmd.imageNumber != 0 {
			img = store.byImageNumber(cmd.imageNumber)
		}
	})
	if img == nil {
		p.respondKittyError(cmd, cmd.imageID, "ENOENT", "no such image")
		return
	}
	p.placeKittyImage(cmd, img)
	p.respondKitty(cmd, img.id, cmd.imageNumber, "OK")
}

// buildKittyPlacement resolves a command's geometry into a placement.
func (p *Parser) buildKittyPlacement(cmd kittyCmd, img *kittyImage) *PlacedImage {
	if img == nil || img.bitmap == nil || img.bitmap.W == 0 || img.bitmap.H == 0 {
		return nil
	}
	cw, ch := p.buffer.GetCellPixelSize()
	if cw <= 0 {
		cw = 10
	}
	if ch <= 0 {
		ch = 20
	}

	srcW, srcH := cmd.srcW, cmd.srcH
	if srcW <= 0 {
		srcW = img.bitmap.W - cmd.srcX
	}
	if srcH <= 0 {
		srcH = img.bitmap.H - cmd.srcY
	}
	if srcW <= 0 || srcH <= 0 {
		return nil
	}

	// c/r give the display size in CELLS; the image is scaled to fit. With only
	// one of them the other follows from the aspect ratio.
	destW, destH := srcW, srcH
	switch {
	case cmd.cols > 0 && cmd.rows > 0:
		destW, destH = cmd.cols*cw, cmd.rows*ch
	case cmd.cols > 0:
		destW = cmd.cols * cw
		destH = srcH * destW / srcW
	case cmd.rows > 0:
		destH = cmd.rows * ch
		destW = srcW * destH / srcH
	}
	if destW < 1 {
		destW = 1
	}
	if destH < 1 {
		destH = 1
	}

	cellsW := (destW + cw - 1) / cw
	cellsH := (destH + ch - 1) / ch
	if cmd.cols > 0 {
		cellsW = cmd.cols
	}
	if cmd.rows > 0 {
		cellsH = cmd.rows
	}

	row, col := p.buffer.CursorRowCol()
	return &PlacedImage{
		Row: row, Col: col,
		CellsWide: cellsW, CellsHigh: cellsH,
		Image:       img.bitmap,
		ImageID:     img.id,
		PlacementID: cmd.placementID,
		ZIndex:      cmd.zIndex,
		Virtual:     cmd.virtual,
		SrcX:        cmd.srcX, SrcY: cmd.srcY, SrcW: srcW, SrcH: srcH,
		DestW: destW, DestH: destH,
	}
}

// respondKitty sends an OK acknowledgement unless the client asked for quiet.
// The protocol also suppresses responses for a command with no image ID and no
// image number, since there would be nothing to correlate the answer with.
func (p *Parser) respondKitty(cmd kittyCmd, id, number uint32, status string) {
	if p.responseSink == nil || cmd.quiet >= 1 {
		return
	}
	if id == 0 && number == 0 {
		return
	}
	var b strings.Builder
	b.WriteString("\x1b_G")
	b.WriteString("i=" + strconv.FormatUint(uint64(id), 10))
	if number != 0 {
		b.WriteString(",I=" + strconv.FormatUint(uint64(number), 10))
	}
	b.WriteString(";" + status + "\x1b\\")
	logGraphics("  <- %q", b.String())
	p.responseSink([]byte(b.String()))
}

// respondKittyError reports a failure. q=2 suppresses even these.
func (p *Parser) respondKittyError(cmd kittyCmd, id uint32, code, detail string) {
	// Logged before the quiet check: q=2 silences the client's copy of this,
	// which is exactly when a diagnostic is worth having.
	logGraphics("  -> ERROR i=%d %s: %s", id, code, detail)
	if p.responseSink == nil || cmd.quiet >= 2 {
		if cmd.quiet >= 2 {
			logGraphics("  <- (suppressed by q=%d)", cmd.quiet)
		}
		return
	}
	reply := fmt.Sprintf("\x1b_Gi=%d;%s:%s\x1b\\", id, code, detail)
	logGraphics("  <- %q", reply)
	p.responseSink([]byte(reply))
}

// readKittyExternal loads a payload from a file, temp file or shared memory
// object. The path arrives base64-encoded in the payload.
func readKittyExternal(cmd kittyCmd, name string) ([]byte, string) {
	if name == "" {
		return nil, "EINVAL"
	}
	var data []byte
	var err error
	switch cmd.medium {
	case 's':
		data, err = readSharedMemory(name)
	default:
		data, err = os.ReadFile(name)
	}
	if err != nil {
		logGraphics("  -> t=%c read of %q failed: %v", cmd.medium, name, err)
		return nil, "ENOENT"
	}
	logGraphics("  -> t=%c read of %q gave %d bytes", cmd.medium, name, len(data))
	if cmd.dataOffset > 0 {
		if cmd.dataOffset >= len(data) {
			return nil, "EINVAL"
		}
		data = data[cmd.dataOffset:]
	}
	if cmd.dataSize > 0 && cmd.dataSize < len(data) {
		data = data[:cmd.dataSize]
	}
	if len(data) > MaxKittyPayloadBytes {
		return nil, "EINVAL"
	}
	if cmd.medium == 't' {
		// A temp file is the terminal's to consume: the protocol has the
		// terminal delete it after reading. Only paths that look like temp
		// files are removed, so a mislabelled t= cannot delete anything else.
		if isTempPath(name) {
			_ = os.Remove(name)
		}
	}
	return data, ""
}

// isTempPath reports whether a path lies in a temporary directory, guarding the
// t=t deletion so a client cannot name an arbitrary file and have it removed.
func isTempPath(name string) bool {
	clean := strings.TrimSpace(name)
	for _, dir := range []string{os.TempDir(), "/tmp", "/var/tmp", "/dev/shm"} {
		if dir == "" {
			continue
		}
		if !strings.HasSuffix(dir, "/") {
			dir += "/"
		}
		if strings.HasPrefix(clean, dir) {
			return true
		}
	}
	return false
}

// decodeKittyBase64 decodes a payload, tolerating both padded and unpadded
// base64 and any whitespace a client wrapped the stream with. Chunked senders
// vary on all three, and rejecting an image over padding would be pedantry.
func decodeKittyBase64(s string) ([]byte, error) {
	clean := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\r', '\n':
			return -1
		}
		return r
	}, s)
	if clean == "" {
		return nil, nil
	}
	if data, err := base64.StdEncoding.DecodeString(clean); err == nil {
		return data, nil
	}
	return base64.RawStdEncoding.DecodeString(clean)
}
