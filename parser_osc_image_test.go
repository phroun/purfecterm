package purfecterm

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// pngBytes encodes a w x h image with one semi-transparent pixel at (0,0), so a
// round trip proves alpha survives as STRAIGHT alpha rather than being
// premultiplied somewhere along the way.
func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}
	img.SetNRGBA(0, 0, color.NRGBA{R: 0, G: 255, B: 0, A: 128})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// inlineImageSeq builds an OSC 1337 File= sequence around a payload.
func inlineImageSeq(args string, payload []byte) []byte {
	return []byte("\x1b]1337;File=" + args + ":" +
		base64.StdEncoding.EncodeToString(payload) + "\x07")
}

func newImageTestBuffer() (*Buffer, *Parser) {
	b := NewBuffer(80, 24, 100)
	b.SetCellPixelSize(10, 20)
	return b, NewParser(b)
}

// A PNG arriving over OSC 1337 with inline=1 is decoded and anchored at the
// cursor at its own pixel size, and its straight alpha survives the trip.
func TestInlineImagePlacesPNG(t *testing.T) {
	b, p := newImageTestBuffer()
	p.Parse(inlineImageSeq("inline=1", pngBytes(t, 20, 12)))

	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatalf("placed %d images, want 1", len(imgs))
	}
	im := imgs[0]
	if im.Image.W != 20 || im.Image.H != 12 {
		t.Errorf("decoded %dx%d, want 20x12", im.Image.W, im.Image.H)
	}
	if w, h := im.DestSize(); w != 20 || h != 12 {
		t.Errorf("DestSize = %dx%d, want the natural 20x12 (no width/height given)", w, h)
	}
	// 20x12 over a 10x20 cell: 2 cells wide, 1 row tall (12px rounds up).
	if im.CellsWide != 2 || im.CellsHigh != 1 {
		t.Errorf("cells = %dx%d, want 2x1", im.CellsWide, im.CellsHigh)
	}
	// (0,0) was set semi-transparent; straight alpha means the stored color is
	// untouched by the alpha, so green stays full-strength.
	r, g, bl, a := im.Image.At(0, 0)
	if a != 128 {
		t.Errorf("alpha = %d, want 128", a)
	}
	if r != 0 || g != 255 || bl != 0 {
		t.Errorf("pixel = (%d,%d,%d), want (0,255,0): alpha was premultiplied into it", r, g, bl)
	}
}

// inline defaults to 0, which is a silent file download, not a picture. A bare
// File= must place nothing — getting this backwards would draw every download.
func TestInlineImageIgnoredWithoutInlineFlag(t *testing.T) {
	b, p := newImageTestBuffer()
	p.Parse(inlineImageSeq("name=Zm9v", pngBytes(t, 8, 8)))
	if imgs := b.GetImages(); len(imgs) != 0 {
		t.Errorf("placed %d images for a download (inline defaults to 0), want 0", len(imgs))
	}
}

// width= in cells resolves against the cell grid, and the height follows to
// preserve the aspect ratio.
func TestInlineImageWidthInCells(t *testing.T) {
	b, p := newImageTestBuffer() // 10x20 cells
	p.Parse(inlineImageSeq("inline=1;width=4", pngBytes(t, 20, 10)))

	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatalf("placed %d images, want 1", len(imgs))
	}
	w, h := imgs[0].DestSize()
	if w != 40 { // 4 cells * 10px
		t.Errorf("dest width = %d, want 40 (4 cells at 10px)", w)
	}
	if h != 20 { // 2:1 source scaled to 40 wide
		t.Errorf("dest height = %d, want 20 (aspect ratio preserved)", h)
	}
}

// Every unit form the protocol defines, resolved against a known grid.
func TestInlineImageDimensionUnits(t *testing.T) {
	cases := []struct {
		spec string
		kind imageDimensionKind
		val  int
	}{
		{"12", imageDimCells, 12},
		{"12px", imageDimPixels, 12},
		{"50%", imageDimPercent, 50},
		{"auto", imageDimAuto, 0},
		{"AUTO", imageDimAuto, 0},
		{"", imageDimAuto, 0},
		{"garbage", imageDimAuto, 0},
		{"0", imageDimAuto, 0},  // a zero size is meaningless; treat as auto
		{"-5", imageDimAuto, 0}, // likewise negative
	}
	for _, c := range cases {
		got := parseImageDimension(c.spec)
		if got.kind != c.kind || (c.kind != imageDimAuto && got.value != c.val) {
			t.Errorf("parseImageDimension(%q) = {%v %d}, want {%v %d}",
				c.spec, got.kind, got.value, c.kind, c.val)
		}
	}
}

// A percentage is of the session, and pixels are taken literally.
func TestInlineImagePercentAndPixels(t *testing.T) {
	b, p := newImageTestBuffer() // 80 cols * 10px = 800px wide
	p.Parse(inlineImageSeq("inline=1;width=50%;height=40px;preserveAspectRatio=0",
		pngBytes(t, 20, 10)))

	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatalf("placed %d images, want 1", len(imgs))
	}
	w, h := imgs[0].DestSize()
	if w != 400 {
		t.Errorf("dest width = %d, want 400 (50%% of 800px)", w)
	}
	if h != 40 {
		t.Errorf("dest height = %d, want 40 (40px literal)", h)
	}
}

// With both dimensions given and the aspect ratio preserved, the image fits
// INSIDE the box rather than filling it, so neither axis overflows the request.
func TestInlineImagePreserveAspectRatioFitsInsideBox(t *testing.T) {
	b, p := newImageTestBuffer()
	// 2:1 source into a 100x100px box: width binds, height comes down to 50.
	p.Parse(inlineImageSeq("inline=1;width=100px;height=100px", pngBytes(t, 20, 10)))

	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatalf("placed %d images, want 1", len(imgs))
	}
	w, h := imgs[0].DestSize()
	if w != 100 || h != 50 {
		t.Errorf("DestSize = %dx%d, want 100x50 (fit inside, not fill)", w, h)
	}
}

// preserveAspectRatio=0 fills the box exactly as asked.
func TestInlineImageAspectRatioOffFillsBox(t *testing.T) {
	b, p := newImageTestBuffer()
	p.Parse(inlineImageSeq("inline=1;width=100px;height=100px;preserveAspectRatio=0",
		pngBytes(t, 20, 10)))

	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatalf("placed %d images, want 1", len(imgs))
	}
	if w, h := imgs[0].DestSize(); w != 100 || h != 100 {
		t.Errorf("DestSize = %dx%d, want 100x100", w, h)
	}
}

// Malformed input is dropped rather than placed or panicked on. A terminal
// parses whatever the far end sends, so none of these may take it down.
func TestInlineImageRejectsMalformedInput(t *testing.T) {
	valid := base64.StdEncoding.EncodeToString(pngBytes(t, 4, 4))
	for _, seq := range []string{
		"\x1b]1337;File=inline=1:!!!not base64!!!\x07",
		"\x1b]1337;File=inline=1:" + base64.StdEncoding.EncodeToString([]byte("not an image")) + "\x07",
		"\x1b]1337;File=inline=1:\x07",                     // empty payload
		"\x1b]1337;File=inline=1" + "\x07",                 // no colon, no payload
		"\x1b]1337;Something=else:" + valid + "\x07",       // not File=
		"\x1b]1337;MultipartFile=inline=1\x07",             // multipart, unsupported
		"\x1b]1337;FilePart=" + valid + "\x07",             // ditto
		"\x1b]1337;FileEnd\x07",                            // ditto
		"\x1b]1337\x07",                                    // no args at all
		"\x1b]1337;File=inline=1;width=:" + valid + "\x07", // empty dimension
	} {
		b, p := newImageTestBuffer()
		p.Parse([]byte(seq))
		if imgs := b.GetImages(); len(imgs) != 0 {
			// The empty-dimension case is legitimate input and DOES place.
			if seq == "\x1b]1337;File=inline=1;width=:"+valid+"\x07" {
				continue
			}
			t.Errorf("placed %d images for %q, want 0", len(imgs), seq)
		}
	}
}

// An oversized payload is refused before it is decoded, so a hostile stream
// cannot make the terminal allocate without bound.
func TestInlineImageRejectsOversizedPayload(t *testing.T) {
	b, p := newImageTestBuffer()
	huge := bytes.Repeat([]byte("A"), (MaxInlineImageBytes/3+16)*4)
	p.Parse([]byte("\x1b]1337;File=inline=1:" + string(huge) + "\x07"))
	if imgs := b.GetImages(); len(imgs) != 0 {
		t.Errorf("placed %d images for an oversized payload, want 0", len(imgs))
	}
}

// The sequence is equally valid ST-terminated.
func TestInlineImageAcceptsSTTerminator(t *testing.T) {
	b, p := newImageTestBuffer()
	seq := fmt.Sprintf("\x1b]1337;File=inline=1:%s\x1b\\",
		base64.StdEncoding.EncodeToString(pngBytes(t, 6, 6)))
	p.Parse([]byte(seq))
	if imgs := b.GetImages(); len(imgs) != 1 {
		t.Errorf("placed %d images for an ST-terminated sequence, want 1", len(imgs))
	}
}
