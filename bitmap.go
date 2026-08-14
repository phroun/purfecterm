package purfecterm

// Decoding compressed still images (PNG, JPEG, GIF) into the RGBA form the
// renderers already blit. This is deliberately independent of any one terminal
// protocol: Sixel produces its pixels itself, while the iTerm2 inline images
// protocol — and anything else that ships an encoded file — arrives here.

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"

	// Registering the standard decoders is all it takes for DecodeImage to
	// handle them; image.Decode dispatches on the registry, not on a list kept
	// here. A program that wants more formats registers them the same way —
	// `import _ "golang.org/x/image/webp"` anywhere in the binary — and
	// DecodeImage picks them up with no change to this package.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	// TIFF is not in the standard library's registry, but it is not exotic
	// here: chafa emits TIFF, not PNG, for its iTerm2 output, so a terminal
	// that only registered the standard three silently dropped every image
	// from `chafa -f iterm`. Registered for real rather than left to the
	// embedding program, since a user running chafa is not in a position to
	// know they need to link a decoder in.
	_ "golang.org/x/image/tiff"
)

// Bitmap is a decoded image in the form every renderer here consumes:
// row-major RGBA, four bytes per pixel, STRAIGHT (not premultiplied) alpha.
//
// It is an alias rather than a distinct type: Sixel decoding produced this
// layout first and named the type after itself, and the renderers, PlacedImage
// and the public API are all written against that name. Aliasing keeps one
// representation while letting non-Sixel sources describe themselves honestly.
type Bitmap = SixelImage

// ErrUnknownImageFormat is returned when the bytes match no registered decoder.
var ErrUnknownImageFormat = errors.New("purfecterm: unrecognized image format")

// DecodeImage decodes an encoded still image into a Bitmap, returning the
// format name that claimed it ("png", "jpeg", "gif", or whatever else has been
// registered). Only the first frame of an animation is read.
func DecodeImage(data []byte) (*Bitmap, string, error) {
	if len(data) == 0 {
		return nil, "", ErrUnknownImageFormat
	}
	src, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		if errors.Is(err, image.ErrFormat) {
			return nil, "", ErrUnknownImageFormat
		}
		return nil, "", fmt.Errorf("purfecterm: decoding image: %w", err)
	}
	bm := BitmapFromImage(src)
	if bm.W == 0 || bm.H == 0 {
		return nil, format, ErrUnknownImageFormat
	}
	return bm, format, nil
}

// BitmapFromImage converts any image.Image into a Bitmap, so a caller that
// already has decoded pixels — from a format this package does not know, or
// generated outright — can hand them over without going through an encoder.
//
// The conversion goes through NRGBA, which is exactly the target layout: RGBA
// byte order with straight alpha. That also normalizes away palettes, gray,
// CMYK, and the premultiplied alpha of image.RGBA, so every source arrives in
// one form.
func BitmapFromImage(src image.Image) *Bitmap {
	if src == nil {
		return &Bitmap{}
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return &Bitmap{}
	}
	// Already the right type and tightly packed: take its pixels as they are.
	if n, ok := src.(*image.NRGBA); ok && n.Stride == w*4 && n.Rect == image.Rect(0, 0, w, h) {
		return &Bitmap{W: w, H: h, RGBA: n.Pix}
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return &Bitmap{W: w, H: h, RGBA: dst.Pix}
}
