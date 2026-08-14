package purfecterm

// The iTerm2 inline images protocol (OSC 1337).
//
//	ESC ] 1337 ; File = key=value ; ... : <base64 payload> BEL
//
// Only inline display is implemented. A file transfer (inline=0, the DEFAULT —
// a bare File= is a silent download, not a picture) is accepted and discarded,
// which is the honest response for a terminal with nowhere to put it.

import (
	"encoding/base64"
	"strconv"
	"strings"
)

// MaxInlineImageBytes caps one decoded inline-image payload. A base64 payload
// arrives as a single OSC string held in memory, so an unbounded one is a way
// to make the terminal allocate arbitrarily on a stream it does not control —
// the same reasoning behind the OSC 52 clipboard limit.
const MaxInlineImageBytes = 16 << 20 // 16 MiB

// imageDimension is one parsed width= or height= argument. Its unit is decided
// by a suffix: bare digits are CELLS, "px" is pixels, "%" is a percentage of
// the session, and "auto" (or absent) means the image's own size.
type imageDimension struct {
	kind  imageDimensionKind
	value int
}

type imageDimensionKind int

const (
	imageDimAuto imageDimensionKind = iota
	imageDimCells
	imageDimPixels
	imageDimPercent
)

// parseImageDimension reads one width=/height= value. An unparsable value is
// auto, matching the protocol's forgiving spirit — a bad dimension should not
// throw the whole image away.
func parseImageDimension(s string) imageDimension {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "auto") {
		return imageDimension{kind: imageDimAuto}
	}
	switch {
	case strings.HasSuffix(s, "%"):
		if n, err := strconv.Atoi(strings.TrimSuffix(s, "%")); err == nil && n > 0 {
			return imageDimension{kind: imageDimPercent, value: n}
		}
	case strings.HasSuffix(s, "px"), strings.HasSuffix(s, "Px"),
		strings.HasSuffix(s, "pX"), strings.HasSuffix(s, "PX"):
		if n, err := strconv.Atoi(s[:len(s)-2]); err == nil && n > 0 {
			return imageDimension{kind: imageDimPixels, value: n}
		}
	default:
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return imageDimension{kind: imageDimCells, value: n}
		}
	}
	return imageDimension{kind: imageDimAuto}
}

// resolve turns a dimension into terminal pixels. natural is the image's own
// size on this axis, cellPx the cell size on it, and sessionPx the full session
// extent on it (for percentages). Returns 0 for auto, which the caller resolves
// once it knows whether the aspect ratio has to be preserved.
func (d imageDimension) resolve(natural, cellPx, sessionPx int) int {
	switch d.kind {
	case imageDimCells:
		return d.value * cellPx
	case imageDimPixels:
		return d.value
	case imageDimPercent:
		return sessionPx * d.value / 100
	}
	return 0
}

// inlineImageArgs is a parsed File= argument list.
type inlineImageArgs struct {
	inline              bool
	width, height       imageDimension
	preserveAspectRatio bool
}

// parseInlineImageArgs reads the key=value;... list preceding the payload.
func parseInlineImageArgs(s string) inlineImageArgs {
	// inline defaults to 0 (download, not display); preserveAspectRatio to 1.
	out := inlineImageArgs{preserveAspectRatio: true}
	for _, field := range strings.Split(s, ";") {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "inline":
			out.inline = value == "1"
		case "width":
			out.width = parseImageDimension(value)
		case "height":
			out.height = parseImageDimension(value)
		case "preserveaspectratio":
			out.preserveAspectRatio = value != "0"
		}
		// name, size and type carry no meaning for an inline draw.
	}
	return out
}

// executeOSCImage handles OSC 1337. args is everything after "1337;".
func (p *Parser) executeOSCImage(args string) {
	key, rest, ok := strings.Cut(args, "=")
	if !ok || !strings.EqualFold(strings.TrimSpace(key), "File") {
		return // MultipartFile/FilePart/FileEnd and the non-file OSC 1337 verbs
	}
	meta, payload, ok := strings.Cut(rest, ":")
	if !ok {
		return
	}
	opts := parseInlineImageArgs(meta)
	if !opts.inline {
		return // a download; there is nowhere to put it
	}

	payload = strings.TrimSpace(payload)
	// 4 base64 chars per 3 bytes: reject before allocating the decode.
	if len(payload)/4*3 > MaxInlineImageBytes {
		return
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		// Some senders omit padding; StdEncoding is strict about it.
		raw, err = base64.RawStdEncoding.DecodeString(payload)
		if err != nil {
			return
		}
	}
	img, _, err := DecodeImage(raw)
	if err != nil {
		return
	}
	destW, destH := p.inlineImageDestSize(img, opts)
	p.buffer.PlaceImage(img, destW, destH)
}

// inlineImageDestSize resolves width=/height= against the cell grid into the
// pixel size the image is to be drawn at.
func (p *Parser) inlineImageDestSize(img *Bitmap, opts inlineImageArgs) (int, int) {
	cw, ch := p.buffer.GetCellPixelSize()
	if cw <= 0 {
		cw = 10
	}
	if ch <= 0 {
		ch = 20
	}
	cols, rows := p.buffer.GetEffectiveSize()

	w := opts.width.resolve(img.W, cw, cols*cw)
	h := opts.height.resolve(img.H, ch, rows*ch)

	switch {
	case w <= 0 && h <= 0: // both auto: the image's own pixels
		return img.W, img.H
	case w <= 0: // height given: scale width to match, or keep it natural
		if opts.preserveAspectRatio && img.H > 0 {
			return clampPositive(img.W * h / img.H), clampPositive(h)
		}
		return img.W, clampPositive(h)
	case h <= 0: // width given, mirror image of the above
		if opts.preserveAspectRatio && img.W > 0 {
			return clampPositive(w), clampPositive(img.H * w / img.W)
		}
		return clampPositive(w), img.H
	}

	// Both given. Preserving the aspect ratio means fitting INSIDE the box
	// rather than filling it, so neither axis overflows what was asked for.
	if opts.preserveAspectRatio && img.W > 0 && img.H > 0 {
		if img.W*h > img.H*w { // width is the binding constraint
			h = img.H * w / img.W
		} else {
			w = img.W * h / img.H
		}
	}
	return clampPositive(w), clampPositive(h)
}

// clampPositive keeps a resolved dimension usable: a rounding-down to zero
// would otherwise place an invisible image.
func clampPositive(n int) int {
	if n < 1 {
		return 1
	}
	return n
}
