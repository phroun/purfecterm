package purfecterm

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newKittyTestBuffer() (*Buffer, *Parser, *[]string) {
	b := NewBuffer(80, 24, 100)
	b.SetCellPixelSize(10, 20)
	p := NewParser(b)
	var replies []string
	p.SetResponseSink(func(d []byte) { replies = append(replies, string(d)) })
	return b, p, &replies
}

// rgbaPayload builds w*h RGBA pixels of one color.
func rgbaPayload(w, h int, r, g, bl, a byte) []byte {
	out := make([]byte, 0, w*h*4)
	for i := 0; i < w*h; i++ {
		out = append(out, r, g, bl, a)
	}
	return out
}

func kittySeq(control string, payload []byte) []byte {
	return []byte("\x1b_G" + control + ";" +
		base64.StdEncoding.EncodeToString(payload) + "\x1b\\")
}

// The base case: transmit raw RGBA and display it in one command.
func TestKittyTransmitAndDisplay(t *testing.T) {
	b, p, replies := newKittyTestBuffer()
	p.Parse(kittySeq("a=T,f=32,s=20,v=40,i=7", rgbaPayload(20, 40, 1, 2, 3, 255)))

	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatalf("placed %d images, want 1", len(imgs))
	}
	im := imgs[0]
	if im.ImageID != 7 {
		t.Errorf("ImageID = %d, want 7", im.ImageID)
	}
	if im.Image.W != 20 || im.Image.H != 40 {
		t.Errorf("decoded %dx%d, want 20x40", im.Image.W, im.Image.H)
	}
	if r, g, bl, a := im.Image.At(0, 0); r != 1 || g != 2 || bl != 3 || a != 255 {
		t.Errorf("pixel = %d,%d,%d,%d, want 1,2,3,255", r, g, bl, a)
	}
	// 20x40 over a 10x20 cell.
	if im.CellsWide != 2 || im.CellsHigh != 2 {
		t.Errorf("cells = %dx%d, want 2x2", im.CellsWide, im.CellsHigh)
	}
	if len(*replies) != 1 || !strings.Contains((*replies)[0], "i=7;OK") {
		t.Errorf("replies = %q, want an OK for i=7", *replies)
	}
}

// f=24 has no alpha channel, so every pixel comes out opaque.
func TestKittyRGBFormatIsOpaque(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	payload := []byte{10, 20, 30, 40, 50, 60} // two RGB pixels
	p.Parse(kittySeq("a=T,f=24,s=2,v=1,i=3", payload))

	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatalf("placed %d images, want 1", len(imgs))
	}
	if _, _, _, a := imgs[0].Image.At(0, 0); a != 255 {
		t.Errorf("alpha = %d, want 255 for f=24", a)
	}
	if r, g, bl, _ := imgs[0].Image.At(1, 0); r != 40 || g != 50 || bl != 60 {
		t.Errorf("second pixel = %d,%d,%d, want 40,50,60", r, g, bl)
	}
}

// A PNG payload (f=100) goes through the shared image decoder.
func TestKittyPNGFormat(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	p.Parse(kittySeq("a=T,f=100,i=9", pngBytes(t, 12, 8)))

	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatalf("placed %d images, want 1", len(imgs))
	}
	if imgs[0].Image.W != 12 || imgs[0].Image.H != 8 {
		t.Errorf("decoded %dx%d, want 12x8", imgs[0].Image.W, imgs[0].Image.H)
	}
}

// A large payload arrives in chunks; nothing is displayed until m=0.
func TestKittyChunkedTransmission(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	payload := rgbaPayload(10, 10, 9, 9, 9, 255)
	enc := base64.StdEncoding.EncodeToString(payload)

	third := len(enc) / 3
	third -= third % 4 // chunks must split on base64 quanta
	c1, c2, c3 := enc[:third], enc[third:2*third], enc[2*third:]

	p.Parse([]byte("\x1b_Ga=T,f=32,s=10,v=10,i=4,m=1;" + c1 + "\x1b\\"))
	if len(b.GetImages()) != 0 {
		t.Fatal("image displayed before the final chunk arrived")
	}
	p.Parse([]byte("\x1b_Gm=1;" + c2 + "\x1b\\"))
	if len(b.GetImages()) != 0 {
		t.Fatal("image displayed before the final chunk arrived")
	}
	p.Parse([]byte("\x1b_Gm=0;" + c3 + "\x1b\\"))

	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatalf("placed %d images after the final chunk, want 1", len(imgs))
	}
	if r, _, _, _ := imgs[0].Image.At(9, 9); r != 9 {
		t.Errorf("last pixel = %d, want 9: chunks were not reassembled in order", r)
	}
}

// o=z payloads are zlib-compressed.
func TestKittyZlibCompression(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	raw := rgbaPayload(8, 8, 5, 6, 7, 255)
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	zw.Write(raw)
	zw.Close()

	p.Parse(kittySeq("a=T,f=32,s=8,v=8,i=11,o=z", buf.Bytes()))
	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatalf("placed %d images, want 1", len(imgs))
	}
	if r, g, bl, _ := imgs[0].Image.At(3, 3); r != 5 || g != 6 || bl != 7 {
		t.Errorf("pixel = %d,%d,%d, want 5,6,7", r, g, bl)
	}
}

// Transmit without display (a=t), then place separately (a=p). The image
// survives between the two, which is the point of separate storage.
func TestKittyTransmitThenPlace(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	p.Parse(kittySeq("a=t,f=32,s=10,v=20,i=5", rgbaPayload(10, 20, 1, 1, 1, 255)))
	if len(b.GetImages()) != 0 {
		t.Fatal("a=t displayed the image; it should only transmit")
	}

	p.Parse([]byte("\x1b_Ga=p,i=5,p=2\x1b\\"))
	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatalf("a=p placed %d images, want 1", len(imgs))
	}
	if imgs[0].ImageID != 5 || imgs[0].PlacementID != 2 {
		t.Errorf("placement = image %d placement %d, want 5/2", imgs[0].ImageID, imgs[0].PlacementID)
	}

	// The same image can be placed again elsewhere without retransmission.
	b.SetCursor(0, 10)
	p.Parse([]byte("\x1b_Ga=p,i=5,p=3\x1b\\"))
	if got := len(b.GetImages()); got != 2 {
		t.Errorf("after a second placement there are %d, want 2", got)
	}
}

// Placing an image that was never transmitted is ENOENT, not a crash.
func TestKittyPlaceMissingImage(t *testing.T) {
	b, p, replies := newKittyTestBuffer()
	p.Parse([]byte("\x1b_Ga=p,i=999\x1b\\"))
	if len(b.GetImages()) != 0 {
		t.Error("placed an image that was never transmitted")
	}
	if len(*replies) != 1 || !strings.Contains((*replies)[0], "ENOENT") {
		t.Errorf("replies = %q, want an ENOENT", *replies)
	}
}

// c and r size the placement in CELLS, scaling the image to fit.
func TestKittyCellSizing(t *testing.T) {
	b, p, _ := newKittyTestBuffer() // 10x20 cells
	p.Parse(kittySeq("a=T,f=32,s=20,v=20,i=1,c=4,r=3", rgbaPayload(20, 20, 1, 1, 1, 255)))

	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatalf("placed %d images, want 1", len(imgs))
	}
	if imgs[0].CellsWide != 4 || imgs[0].CellsHigh != 3 {
		t.Errorf("cells = %dx%d, want 4x3", imgs[0].CellsWide, imgs[0].CellsHigh)
	}
	if w, h := imgs[0].DestSize(); w != 40 || h != 60 {
		t.Errorf("DestSize = %dx%d, want 40x60 (4x10 by 3x20)", w, h)
	}
}

// With only c given, the row count follows from the aspect ratio.
func TestKittyCellSizingSingleAxis(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	// 40x40 source, 4 columns = 40px wide, so 40px tall = 2 rows of 20px.
	p.Parse(kittySeq("a=T,f=32,s=40,v=40,i=1,c=4", rgbaPayload(40, 40, 1, 1, 1, 255)))

	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatalf("placed %d images, want 1", len(imgs))
	}
	if w, h := imgs[0].DestSize(); w != 40 || h != 40 {
		t.Errorf("DestSize = %dx%d, want 40x40 (aspect ratio preserved)", w, h)
	}
}

// x/y/w/h crop the source before display.
func TestKittySourceCrop(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	p.Parse(kittySeq("a=T,f=32,s=40,v=40,i=1,x=10,y=5,w=20,h=30",
		rgbaPayload(40, 40, 1, 1, 1, 255)))

	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatalf("placed %d images, want 1", len(imgs))
	}
	x, y, w, h := imgs[0].SourceRect()
	if x != 10 || y != 5 || w != 20 || h != 30 {
		t.Errorf("SourceRect = %d,%d %dx%d, want 10,5 20x30", x, y, w, h)
	}
	// A crop running past the edge is clamped rather than read out of bounds.
	imgs[0].SrcX, imgs[0].SrcW = 30, 999
	if x, _, w, _ := imgs[0].SourceRect(); x+w > 40 {
		t.Errorf("crop x=%d w=%d escapes the 40px image", x, w)
	}
}

// The cursor advances past the image unless C=1 says otherwise.
func TestKittyCursorMovement(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	b.SetCursor(0, 0)
	p.Parse(kittySeq("a=T,f=32,s=20,v=40,i=1", rgbaPayload(20, 40, 1, 1, 1, 255)))
	// 2 cells wide, 2 rows tall: right by 2, down by 2.
	if x, y := b.GetCursor(); x != 2 || y != 2 {
		t.Errorf("cursor = (%d,%d), want (2,2)", x, y)
	}

	b2, p2, _ := newKittyTestBuffer()
	b2.SetCursor(0, 0)
	p2.Parse(kittySeq("a=T,f=32,s=20,v=40,i=1,C=1", rgbaPayload(20, 40, 1, 1, 1, 255)))
	if x, y := b2.GetCursor(); x != 0 || y != 0 {
		t.Errorf("cursor = (%d,%d) with C=1, want it left alone at (0,0)", x, y)
	}
}

// A negative z-index puts the image under the text; the renderer groups on it.
func TestKittyZIndexGrouping(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	p.Parse(kittySeq("a=T,f=32,s=10,v=20,i=1,z=-5,C=1", rgbaPayload(10, 20, 1, 1, 1, 255)))
	p.Parse(kittySeq("a=T,f=32,s=10,v=20,i=2,z=3,C=1", rgbaPayload(10, 20, 2, 2, 2, 255)))
	p.Parse(kittySeq("a=T,f=32,s=10,v=20,i=3,z=1,C=1", rgbaPayload(10, 20, 3, 3, 3, 255)))

	below, above := b.GetImagesByZ()
	if len(below) != 1 || below[0].ImageID != 1 {
		t.Errorf("below = %v, want just image 1", idsOf(below))
	}
	if len(above) != 2 {
		t.Fatalf("above = %v, want two images", idsOf(above))
	}
	if above[0].ImageID != 3 || above[1].ImageID != 2 {
		t.Errorf("above order = %v, want image 3 (z=1) then image 2 (z=3)", idsOf(above))
	}
}

func idsOf(list []*PlacedImage) []uint32 {
	out := make([]uint32, len(list))
	for i, im := range list {
		out[i] = im.ImageID
	}
	return out
}

// Deletion: lowercase removes placements and keeps the pixels, so the image can
// be placed again; uppercase frees the data and it cannot.
func TestKittyDeleteLowercaseKeepsImageData(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	p.Parse(kittySeq("a=T,f=32,s=10,v=20,i=8,C=1", rgbaPayload(10, 20, 1, 1, 1, 255)))

	p.Parse([]byte("\x1b_Ga=d,d=i,i=8\x1b\\"))
	if len(b.GetImages()) != 0 {
		t.Fatal("d=i left the placement in place")
	}
	p.Parse([]byte("\x1b_Ga=p,i=8\x1b\\"))
	if len(b.GetImages()) != 1 {
		t.Error("d=i freed the image data; a lowercase delete must keep it")
	}
}

func TestKittyDeleteUppercaseFreesImageData(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	p.Parse(kittySeq("a=T,f=32,s=10,v=20,i=8,C=1", rgbaPayload(10, 20, 1, 1, 1, 255)))

	p.Parse([]byte("\x1b_Ga=d,d=I,i=8\x1b\\"))
	if len(b.GetImages()) != 0 {
		t.Fatal("d=I left the placement in place")
	}
	p.Parse([]byte("\x1b_Ga=p,i=8\x1b\\"))
	if len(b.GetImages()) != 0 {
		t.Error("d=I kept the image data; an uppercase delete must free it")
	}
}

// d=a removes every placement.
func TestKittyDeleteAll(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	for i := 1; i <= 3; i++ {
		p.Parse(kittySeq(fmt.Sprintf("a=T,f=32,s=10,v=20,i=%d,C=1", i),
			rgbaPayload(10, 20, 1, 1, 1, 255)))
	}
	p.Parse([]byte("\x1b_Ga=d,d=a\x1b\\"))
	if got := len(b.GetImages()); got != 0 {
		t.Errorf("%d placements survived d=a", got)
	}
}

// d=z removes only placements at one z-index.
func TestKittyDeleteByZIndex(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	p.Parse(kittySeq("a=T,f=32,s=10,v=20,i=1,z=5,C=1", rgbaPayload(10, 20, 1, 1, 1, 255)))
	p.Parse(kittySeq("a=T,f=32,s=10,v=20,i=2,z=9,C=1", rgbaPayload(10, 20, 1, 1, 1, 255)))

	p.Parse([]byte("\x1b_Ga=d,d=z,z=5\x1b\\"))
	imgs := b.GetImages()
	if len(imgs) != 1 || imgs[0].ImageID != 2 {
		t.Errorf("after d=z z=5 the survivors are %v, want just image 2", idsOf(imgs))
	}
}

// d=r removes images whose ID falls in a range.
func TestKittyDeleteByIDRange(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	for i := 1; i <= 5; i++ {
		p.Parse(kittySeq(fmt.Sprintf("a=T,f=32,s=10,v=20,i=%d,C=1", i),
			rgbaPayload(10, 20, 1, 1, 1, 255)))
	}
	p.Parse([]byte("\x1b_Ga=d,d=r,x=2,y=4\x1b\\"))

	got := idsOf(b.GetImages())
	if len(got) != 2 || got[0] != 1 || got[1] != 5 {
		t.Errorf("survivors = %v, want images 1 and 5", got)
	}
}

// A query reports support without storing anything.
func TestKittyQueryDoesNotStore(t *testing.T) {
	b, p, replies := newKittyTestBuffer()
	p.Parse(kittySeq("a=q,f=32,s=1,v=1,i=31", rgbaPayload(1, 1, 0, 0, 0, 255)))
	if len(b.GetImages()) != 0 {
		t.Error("a=q placed an image; a query must not display")
	}
	if len(*replies) != 1 || !strings.Contains((*replies)[0], "i=31;OK") {
		t.Errorf("replies = %q, want an OK for i=31", *replies)
	}
}

// q=1 suppresses the OK; q=2 suppresses errors too.
func TestKittyQuietSuppressesResponses(t *testing.T) {
	_, p, replies := newKittyTestBuffer()
	p.Parse(kittySeq("a=T,f=32,s=10,v=10,i=1,q=1", rgbaPayload(10, 10, 1, 1, 1, 255)))
	if len(*replies) != 0 {
		t.Errorf("q=1 still replied: %q", *replies)
	}
	p.Parse([]byte("\x1b_Ga=p,i=987,q=2\x1b\\"))
	if len(*replies) != 0 {
		t.Errorf("q=2 still reported an error: %q", *replies)
	}
	// Without quiet, the same failure does report.
	p.Parse([]byte("\x1b_Ga=p,i=987\x1b\\"))
	if len(*replies) != 1 {
		t.Errorf("replies = %q, want one error", *replies)
	}
}

// t=f reads the payload from a file named in the (base64) payload.
func TestKittyFileTransmission(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "img.rgba")
	if err := os.WriteFile(path, rgbaPayload(4, 4, 7, 7, 7, 255), 0o600); err != nil {
		t.Fatal(err)
	}

	b, p, _ := newKittyTestBuffer()
	p.Parse(kittySeq("a=T,f=32,s=4,v=4,i=1,t=f", []byte(path)))

	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatalf("placed %d images from a file, want 1", len(imgs))
	}
	if r, _, _, _ := imgs[0].Image.At(0, 0); r != 7 {
		t.Errorf("pixel = %d, want 7", r)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("t=f deleted the file; only t=t may consume it")
	}
}

// t=t consumes the temp file it read.
func TestKittyTempFileIsConsumed(t *testing.T) {
	dir := os.TempDir()
	f, err := os.CreateTemp(dir, "purfecterm-kitty-*.rgba")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	f.Write(rgbaPayload(4, 4, 3, 3, 3, 255))
	f.Close()
	defer os.Remove(path)

	b, p, _ := newKittyTestBuffer()
	p.Parse(kittySeq("a=T,f=32,s=4,v=4,i=1,t=t", []byte(path)))
	if len(b.GetImages()) != 1 {
		t.Fatal("no image placed from the temp file")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("t=t left the temp file behind; the terminal consumes it")
	}
}

// A t=t path outside a temp directory is read but NOT deleted, so a client
// cannot use the protocol to remove arbitrary files.
func TestKittyTempFileOutsideTempDirIsNotDeleted(t *testing.T) {
	// Deliberately NOT t.TempDir: that lives under the system temp directory,
	// which is precisely the case this guard exempts. A directory beside the
	// package is the "arbitrary file" a hostile t=t would be aiming at.
	dir, err := os.MkdirTemp(".", "kitty-not-tmp-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	abs, err := filepath.Abs(filepath.Join(dir, "precious.rgba"))
	if err != nil {
		t.Fatal(err)
	}
	path := abs
	if err := os.WriteFile(path, rgbaPayload(2, 2, 1, 1, 1, 255), 0o600); err != nil {
		t.Fatal(err)
	}
	if isTempPath(path) {
		t.Fatalf("%s was classified as a temp path; the guard is too broad", path)
	}

	_, p, _ := newKittyTestBuffer()
	p.Parse(kittySeq("a=T,f=32,s=2,v=2,i=1,t=t", []byte(path)))
	if _, err := os.Stat(path); err != nil {
		t.Error("a t=t path outside a temp directory was deleted")
	}
}

// Malformed commands are refused without placing or panicking.
func TestKittyRejectsMalformedCommands(t *testing.T) {
	for _, seq := range []string{
		"\x1b_Ga=T,f=32,s=10,v=10;!!!not base64!!!\x1b\\",
		"\x1b_Ga=T,f=32,s=10,v=10;" + base64.StdEncoding.EncodeToString([]byte("short")) + "\x1b\\",
		"\x1b_Ga=T,f=99,s=1,v=1;" + base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4}) + "\x1b\\",
		"\x1b_Ga=T,f=32;" + base64.StdEncoding.EncodeToString([]byte{1, 2, 3, 4}) + "\x1b\\", // no s/v
		"\x1b_Ga=Z\x1b\\",     // unknown action
		"\x1b_Ga=T,s=x\x1b\\", // non-numeric
		"\x1b_G\x1b\\",
		"\x1b_G;\x1b\\",
	} {
		b, p, _ := newKittyTestBuffer()
		p.Parse([]byte(seq))
		if got := len(b.GetImages()); got != 0 {
			t.Errorf("placed %d images for %q, want 0", got, seq)
		}
	}
}

// An APC that is not kitty graphics is discarded, and does not print.
func TestNonGraphicsAPCIsIgnored(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	p.Parse([]byte("\x1b_Xsomething\x1b\\ok"))
	if len(b.GetImages()) != 0 {
		t.Error("a non-graphics APC placed an image")
	}
	// The trailing text still prints; the APC must not swallow it.
	var row strings.Builder
	for x := 0; x < 4; x++ {
		if c := b.GetVisibleCell(x, 0); c.Char != 0 {
			row.WriteRune(c.Char)
		}
	}
	if got := strings.TrimSpace(row.String()); !strings.HasPrefix(got, "ok") {
		t.Errorf("row 0 = %q, want the text after the APC to print", got)
	}
}

// A BEL terminates an APC as well as ST.
func TestKittyAcceptsBELTerminator(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	p.Parse([]byte("\x1b_Ga=T,f=32,s=4,v=4,i=1;" +
		base64.StdEncoding.EncodeToString(rgbaPayload(4, 4, 1, 1, 1, 255)) + "\x07"))
	if len(b.GetImages()) != 1 {
		t.Error("a BEL-terminated kitty command was not executed")
	}
}

// Placeholder diacritics round-trip through the encoder and decoder.
func TestKittyPlaceholderDiacritics(t *testing.T) {
	for _, n := range []int{0, 1, 2, 15, 100, 255} {
		r, ok := KittyDiacriticFor(n)
		if !ok {
			t.Fatalf("no diacritic for %d", n)
		}
		got, ok := KittyDiacriticValue(r)
		if !ok || got != n {
			t.Errorf("diacritic %U decoded to %d (ok=%v), want %d", r, got, ok, n)
		}
	}
	if _, ok := KittyDiacriticFor(-1); ok {
		t.Error("a negative index produced a diacritic")
	}
	if _, ok := KittyDiacriticValue('a'); ok {
		t.Error("an ordinary letter was read as a diacritic")
	}
}

// A placeholder cell carries the image ID in its foreground color and its
// position in combining marks.
func TestKittyPlaceholderDecoding(t *testing.T) {
	rowMark, _ := KittyDiacriticFor(3)
	colMark, _ := KittyDiacriticFor(7)
	fg := TrueColor(0, 1, 42) // image ID 0x00012A = 298

	ph, ok := DecodeKittyPlaceholder(KittyPlaceholderRune, []rune{rowMark, colMark}, fg)
	if !ok {
		t.Fatal("the placeholder rune was not recognized")
	}
	if ph.ImageID != 298 {
		t.Errorf("ImageID = %d, want 298 from the foreground color", ph.ImageID)
	}
	if !ph.HasRow || ph.Row != 3 || !ph.HasCol || ph.Col != 7 {
		t.Errorf("position = (%d,%d) has=(%v,%v), want (3,7) both present",
			ph.Row, ph.Col, ph.HasRow, ph.HasCol)
	}

	// Missing marks are reported absent, so the caller inherits them.
	ph2, _ := DecodeKittyPlaceholder(KittyPlaceholderRune, nil, fg)
	if ph2.HasRow || ph2.HasCol {
		t.Error("absent diacritics were reported as present")
	}
	if _, ok := DecodeKittyPlaceholder('x', nil, fg); ok {
		t.Error("an ordinary rune was read as a placeholder")
	}
}

// A virtual placement (U=1) is held for its placeholders and kept out of the
// ordinary draw passes.
func TestKittyVirtualPlacement(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	p.Parse(kittySeq("a=T,f=32,s=20,v=40,i=77,U=1,c=2,r=2",
		rgbaPayload(20, 40, 1, 1, 1, 255)))

	below, above := b.GetImagesByZ()
	if len(below) != 0 || len(above) != 0 {
		t.Error("a virtual placement was handed to the ordinary draw passes")
	}
	vp := b.KittyVirtualPlacement(77)
	if vp == nil {
		t.Fatal("the virtual placement was not retained for its placeholders")
	}
	if vp.CellsWide != 2 || vp.CellsHigh != 2 {
		t.Errorf("virtual placement is %dx%d cells, want 2x2", vp.CellsWide, vp.CellsHigh)
	}
	if b.KittyVirtualPlacement(999) != nil {
		t.Error("an unknown image resolved to a placement")
	}
	// A virtual placement never moves the cursor.
	if x, y := b.GetCursor(); x != 0 || y != 0 {
		t.Errorf("cursor = (%d,%d), want (0,0): a virtual placement must not move it", x, y)
	}
}

// Placements scroll with the text, like every other image.
func TestKittyPlacementScrollsWithText(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	b.SetCursor(0, 5)
	p.Parse(kittySeq("a=T,f=32,s=10,v=40,i=1,C=1", rgbaPayload(10, 40, 1, 1, 1, 255)))
	if imgs := b.GetImages(); len(imgs) != 1 || imgs[0].Row != 5 {
		t.Fatalf("setup: %v", b.GetImages())
	}
	b.ScrollUp(3)
	if imgs := b.GetImages(); len(imgs) != 1 || imgs[0].Row != 2 {
		t.Errorf("after 3 scrolls the placement is at row %d, want 2", b.GetImages()[0].Row)
	}
}

// The image store is bounded, so a client that transmits without ever deleting
// cannot grow it without limit.
func TestKittyImageStoreIsBounded(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	for i := 1; i <= MaxKittyImages+50; i++ {
		p.Parse(kittySeq(fmt.Sprintf("a=t,f=32,s=1,v=1,i=%d,q=1", i),
			rgbaPayload(1, 1, 1, 1, 1, 255)))
	}
	b.withKittyStore(func() {
		if n := len(b.kittyStore().byID); n > MaxKittyImages {
			t.Errorf("store holds %d images, want at most %d", n, MaxKittyImages)
		}
	})
}

// A frame or animation command naming an image that was never transmitted is
// ENOENT — a different answer from "not supported", and the one that tells a
// client to retransmit rather than to give up on the feature.
func TestKittyAnimationOnMissingImageIsNotFound(t *testing.T) {
	for _, action := range []string{"f", "a", "c"} {
		_, p, replies := newKittyTestBuffer()
		p.Parse([]byte("\x1b_Ga=" + action + ",i=42,f=32,s=1,v=1\x1b\\"))
		if len(*replies) != 1 || !strings.Contains((*replies)[0], "ENOENT") {
			t.Errorf("a=%s on a missing image answered %q, want ENOENT", action, *replies)
		}
	}
}

// A client that streams — a browser, a video player — transmits FRAMES against
// an image it already sent and then switches which is current. That is double
// buffering, and it is load-bearing: refusing it does not degrade such a client
// to still images, it stops it.
func TestKittyAnimationFrameSwitching(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	// Root image: 2x2, all red. Displayed at once.
	p.Parse(kittySeq("a=T,f=32,s=2,v=2,i=1,C=1", rgbaPayload(2, 2, 255, 0, 0, 255)))
	imgs := b.GetImages()
	if len(imgs) != 1 {
		t.Fatalf("placed %d images, want 1", len(imgs))
	}
	if r, _, _, _ := imgs[0].Image.At(0, 0); r != 255 {
		t.Fatalf("root frame is not red")
	}

	// Frame 2: all green, staged. Transmitting does not change what is shown —
	// a client stages frames and then either composes them into the picture or
	// selects one.
	p.Parse(kittySeq("a=f,f=32,s=2,v=2,i=1", rgbaPayload(2, 2, 0, 255, 0, 255)))
	if r, _, _, _ := b.GetImages()[0].Image.At(0, 0); r != 255 {
		t.Error("a staged frame reached the screen before it was asked for")
	}

	// Selecting it shows it, and every placement of the image follows.
	p.Parse([]byte("\x1b_Ga=a,i=1,r=2\x1b\\"))
	r, g, _, _ := b.GetImages()[0].Image.At(0, 0)
	if g != 255 || r != 0 {
		t.Errorf("after selecting frame 2 the pixel is (%d,%d,..), want green", r, g)
	}

	// And back to the root frame.
	p.Parse([]byte("\x1b_Ga=a,i=1,r=1\x1b\\"))
	if r, _, _, _ := b.GetImages()[0].Image.At(0, 0); r != 255 {
		t.Error("selecting frame 1 did not return to the root image")
	}
}

// A frame may carry only part of the canvas, landing at x,y over a base frame.
func TestKittyAnimationPartialFrame(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	p.Parse(kittySeq("a=T,f=32,s=4,v=4,i=1,C=1", rgbaPayload(4, 4, 255, 0, 0, 255)))

	// A 2x2 blue patch at (2,2), composed over frame 1.
	p.Parse(kittySeq("a=f,f=32,s=2,v=2,x=2,y=2,c=1,i=1", rgbaPayload(2, 2, 0, 0, 255, 255)))
	p.Parse([]byte("\x1b_Ga=a,i=1,r=2\x1b\\"))

	img := b.GetImages()[0].Image
	if _, _, bl, _ := img.At(3, 3); bl != 255 {
		t.Errorf("the patch did not land at its offset")
	}
	if r, _, _, _ := img.At(0, 0); r != 255 {
		t.Errorf("the base frame's pixels were lost outside the patch")
	}
}

// An unset alpha composes rather than overwrites, and X=1 asks for replacement.
func TestKittyAnimationCompositionModes(t *testing.T) {
	for _, c := range []struct {
		name    string
		control string
		wantRed byte
	}{
		{"alpha blend leaves the base showing", "a=f,f=32,s=2,v=2,c=1,i=1", 255},
		{"X=1 replaces outright", "a=f,f=32,s=2,v=2,c=1,X=1,i=1", 0},
	} {
		b, p, _ := newKittyTestBuffer()
		p.Parse(kittySeq("a=T,f=32,s=2,v=2,i=1,C=1", rgbaPayload(2, 2, 255, 0, 0, 255)))
		// A fully TRANSPARENT green patch: blended it is invisible, replaced it wins.
		p.Parse(kittySeq(c.control, rgbaPayload(2, 2, 0, 255, 0, 0)))
		p.Parse([]byte("\x1b_Ga=a,i=1,r=2\x1b\\"))
		if r, _, _, _ := b.GetImages()[0].Image.At(0, 0); r != c.wantRed {
			t.Errorf("%s: red = %d, want %d", c.name, r, c.wantRed)
		}
	}
}

// A frame for an image that was never transmitted is ENOENT, not a crash.
func TestKittyAnimationFrameForMissingImage(t *testing.T) {
	b, p, replies := newKittyTestBuffer()
	p.Parse(kittySeq("a=f,f=32,s=2,v=2,i=404", rgbaPayload(2, 2, 1, 1, 1, 255)))
	if len(b.GetImages()) != 0 {
		t.Error("a frame placed an image")
	}
	if len(*replies) != 1 || !strings.Contains((*replies)[0], "ENOENT") {
		t.Errorf("replies = %q, want ENOENT", *replies)
	}
}

// Frame storage is bounded: a client that streams frames forever is streaming,
// not animating, and must not grow the store without limit.
func TestKittyAnimationFrameCountIsBounded(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	p.Parse(kittySeq("a=T,f=32,s=2,v=2,i=1,C=1,q=1", rgbaPayload(2, 2, 1, 1, 1, 255)))
	for i := 0; i < MaxKittyFrames*2; i++ {
		p.Parse(kittySeq("a=f,f=32,s=2,v=2,i=1,q=1", rgbaPayload(2, 2, 2, 2, 2, 255)))
	}
	var n int
	b.withKittyStore(func() {
		if img := b.kittyStore().get(1); img != nil {
			n = len(img.frames)
		}
	})
	if n > MaxKittyFrames {
		t.Errorf("image holds %d frames, want at most %d", n, MaxKittyFrames)
	}
}

// The probe awrit sends on startup must be ANSWERED, not refused: it asks
// whether frames work before deciding to run at all.
func TestKittyAnimationProbeIsAccepted(t *testing.T) {
	_, p, replies := newKittyTestBuffer()
	p.Parse(kittySeq("f=24,i=4294111295,t=d,s=1,v=1,z=1", []byte{0, 0, 0}))
	p.Parse(kittySeq("a=f,i=4294111295,f=24,t=d,s=1,v=1,z=1,r=2", []byte{0, 0, 0}))

	all := strings.Join(*replies, "")
	if strings.Contains(all, "EINVAL") || strings.Contains(all, "ENOENT") {
		t.Errorf("the frame probe was refused: %q", *replies)
	}
	if strings.Count(all, "OK") < 2 {
		t.Errorf("replies = %q, want both the image and the frame accepted", *replies)
	}
}

// a=c copies a rectangle from the frame named by r INTO the frame named by c.
// That is the opposite of what the specification's key table says, and the
// traffic is what settles it: a client stages a damage region with a=f and then
// composes it into the picture at the offset it belongs at. Read the other way
// the picture is copied over the damage, which on screen was the content
// appearing for one paint and then being wiped.
func TestKittyComposeFrames(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	// Frame 1 (the picture): 4x4 red. Frame 2 (staged damage): 4x4 blue.
	p.Parse(kittySeq("a=T,f=32,s=4,v=4,i=1,C=1", rgbaPayload(4, 4, 255, 0, 0, 255)))
	p.Parse(kittySeq("a=f,f=32,s=4,v=4,i=1,r=2,X=1", rgbaPayload(4, 4, 0, 0, 255, 255)))

	// Copy a 2x2 region from frame 2 into the picture at (2,2).
	p.Parse([]byte("\x1b_Ga=c,i=1,r=2,c=1,x=2,y=2,X=0,Y=0,w=2,h=2,C=1\x1b\\"))

	// The placement shows frame 1, which now carries the damage.
	img := b.GetImages()[0].Image
	if _, _, bl, _ := img.At(3, 3); bl != 255 {
		t.Errorf("the damage did not reach the picture at its destination")
	}
	if r, _, _, _ := img.At(0, 0); r != 255 {
		t.Errorf("the picture was overwritten outside the composed region")
	}
}

// Transmitting a frame does not change what is on screen; only composing into
// the displayed frame, or selecting one with a=a, does.
func TestKittyTransmittingAFrameDoesNotChangeTheDisplay(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	p.Parse(kittySeq("a=T,f=32,s=2,v=2,i=1,C=1", rgbaPayload(2, 2, 255, 0, 0, 255)))
	p.Parse(kittySeq("a=f,f=32,s=2,v=2,i=1,r=2,X=1", rgbaPayload(2, 2, 0, 255, 0, 255)))

	if _, g, _, _ := b.GetImages()[0].Image.At(0, 0); g == 255 {
		t.Error("a staged frame reached the screen before it was composed")
	}
	p.Parse([]byte("\x1b_Ga=a,i=1,r=2\x1b\\"))
	if _, g, _, _ := b.GetImages()[0].Image.At(0, 0); g != 255 {
		t.Error("selecting the frame did not show it")
	}
}

// The destination offset is x,y and the source offset X,Y — distinct, and easy
// to transpose since the same letters mean other things elsewhere.
func TestKittyComposeSourceAndDestinationEdgesAreDistinct(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	// Staged frame with a single green pixel at (3,3), everything else clear.
	patch := make([]byte, 4*4*4)
	i := (3*4 + 3) * 4
	patch[i+1], patch[i+3] = 255, 255
	p.Parse(kittySeq("a=T,f=32,s=4,v=4,i=1,C=1", rgbaPayload(4, 4, 255, 0, 0, 255)))
	p.Parse(kittySeq("a=f,f=32,s=4,v=4,i=1,r=2,X=1", patch))

	// Take the 1x1 SOURCE rect at (3,3) to DESTINATION (0,0) of the picture.
	p.Parse([]byte("\x1b_Ga=c,i=1,r=2,c=1,X=3,Y=3,x=0,y=0,w=1,h=1,C=1\x1b\\"))

	if _, g, _, _ := b.GetImages()[0].Image.At(0, 0); g != 255 {
		r, g2, bl, _ := b.GetImages()[0].Image.At(0, 0)
		t.Errorf("destination (0,0) = (%d,%d,%d), want the green source pixel — "+
			"x/y and X/Y are transposed", r, g2, bl)
	}
}

// Composing against a frame that does not exist is ENOENT, not a crash.
func TestKittyComposeMissingFrame(t *testing.T) {
	_, p, replies := newKittyTestBuffer()
	p.Parse(kittySeq("a=T,f=32,s=2,v=2,i=1,C=1,q=1", rgbaPayload(2, 2, 1, 1, 1, 255)))
	p.Parse([]byte("\x1b_Ga=c,i=1,r=9,c=1\x1b\\"))
	if len(*replies) != 1 || !strings.Contains((*replies)[0], "ENOENT") {
		t.Errorf("replies = %q, want ENOENT for a missing frame", *replies)
	}
}

// The compose probe awrit sends must be ACCEPTED: told no, it skips its image
// transmission entirely and exits.
func TestKittyComposeProbeIsAccepted(t *testing.T) {
	_, p, replies := newKittyTestBuffer()
	p.Parse(kittySeq("f=24,i=4294111295,t=d,s=1,v=1,z=1", []byte{0, 0, 0}))
	p.Parse(kittySeq("a=f,i=4294111295,f=24,t=d,s=1,v=1,z=1,r=2", []byte{0, 0, 0}))
	p.Parse([]byte("\x1b_Ga=c,C=1,i=4294111295,r=2,c=1,x=0,y=0,w=1,h=1\x1b\\"))

	all := strings.Join(*replies, "")
	if strings.Contains(all, "EINVAL") || strings.Contains(all, "ENOENT") {
		t.Errorf("a probe was refused: %q", *replies)
	}
	if n := strings.Count(all, "OK"); n < 3 {
		t.Errorf("%d probes accepted, want all three: %q", n, *replies)
	}
}

// The diagnostics are opt-in and record the failures a client never sees: a
// q=2 command silences its own error, which is exactly when the terminal has
// to be able to say what went wrong.
func TestGraphicsLogRecordsSilencedFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gfx.log")
	t.Setenv("PURFECTERM_GRAPHICS_LOG", path)
	resetGraphicsLogForTest()
	t.Cleanup(resetGraphicsLogForTest)

	_, p, replies := newKittyTestBuffer()
	// t=f naming a file that does not exist, silenced with q=2.
	p.Parse(kittySeq("a=T,f=32,s=2,v=2,i=1,t=f,q=2", []byte("/nonexistent/purfecterm-test")))

	if len(*replies) != 0 {
		t.Errorf("q=2 still replied: %q", *replies)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no log written: %v", err)
	}
	log := string(data)
	if !strings.Contains(log, "APC _G") {
		t.Errorf("log does not record the command: %q", log)
	}
	if !strings.Contains(log, "ENOENT") {
		t.Errorf("log does not record the silenced failure: %q", log)
	}
	if !strings.Contains(log, "/nonexistent/purfecterm-test") {
		t.Errorf("log does not name what could not be read: %q", log)
	}
}

// With the variable unset nothing is opened and nothing is written.
func TestGraphicsLogIsOffByDefault(t *testing.T) {
	t.Setenv("PURFECTERM_GRAPHICS_LOG", "")
	resetGraphicsLogForTest()
	t.Cleanup(resetGraphicsLogForTest)

	if GraphicsLoggingEnabled() {
		t.Error("graphics logging is on without being asked for")
	}
	_, p, _ := newKittyTestBuffer()
	p.Parse(kittySeq("a=T,f=32,s=2,v=2,i=1", rgbaPayload(2, 2, 1, 1, 1, 255)))
}

// r= NAMES the frame. A client addressing frame 3 while only the root exists
// means frame 3 — appending into the next free slot instead left its later
// commands pointing at a frame that was never created, which is exactly what
// awrit's second and third images hit: a=f r=3 landed in slot 2, and the a=c
// that followed reported the frame missing.
func TestKittyFrameNumberIsHonoured(t *testing.T) {
	b, p, replies := newKittyTestBuffer()
	p.Parse(kittySeq("a=T,f=32,s=2,v=2,i=1,C=1,q=1", rgbaPayload(2, 2, 255, 0, 0, 255)))

	// Straight to frame 3, skipping 2 entirely.
	p.Parse(kittySeq("a=f,f=32,s=2,v=2,i=1,r=3,X=1,q=1", rgbaPayload(2, 2, 0, 255, 0, 255)))
	// The compose that follows must FIND frame 3.
	p.Parse([]byte("\x1b_Ga=c,i=1,r=3,c=1,x=0,y=0,w=1,h=1\x1b\\"))

	for _, r := range *replies {
		if strings.Contains(r, "ENOENT") {
			t.Fatalf("frame 3 was not created where it was named: %q", *replies)
		}
	}
	p.Parse([]byte("\x1b_Ga=a,i=1,r=3\x1b\\"))
	if _, g, _, _ := b.GetImages()[0].Image.At(1, 1); g != 255 {
		t.Errorf("frame 3 does not hold the data addressed to it")
	}
}

// A gap opened by naming a distant frame is filled from the root, so the
// intervening frames are the picture rather than holes.
func TestKittyFrameGapIsFilledFromRoot(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	p.Parse(kittySeq("a=T,f=32,s=2,v=2,i=1,C=1,q=1", rgbaPayload(2, 2, 255, 0, 0, 255)))
	p.Parse(kittySeq("a=f,f=32,s=2,v=2,i=1,r=4,X=1,q=1", rgbaPayload(2, 2, 0, 255, 0, 255)))

	// Frame 2 was never sent; it must be the root picture, not transparent.
	p.Parse([]byte("\x1b_Ga=a,i=1,r=2\x1b\\"))
	r, _, _, a := b.GetImages()[0].Image.At(0, 0)
	if a != 255 || r != 255 {
		t.Errorf("the filled frame is (%d,..,alpha %d), want the opaque root", r, a)
	}
}

// A frame that names no base is a difference against the picture, so what it
// does not carry stays visible rather than becoming a hole.
func TestKittyFrameWithoutBaseStartsFromRoot(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	p.Parse(kittySeq("a=T,f=32,s=4,v=4,i=1,C=1,q=1", rgbaPayload(4, 4, 255, 0, 0, 255)))
	// A 4x1 strip covering only the top row.
	p.Parse(kittySeq("a=f,f=32,s=4,v=1,i=1,r=2,X=1,q=1", rgbaPayload(4, 1, 0, 255, 0, 255)))
	p.Parse([]byte("\x1b_Ga=a,i=1,r=2\x1b\\"))

	img := b.GetImages()[0].Image
	if _, g, _, _ := img.At(0, 0); g != 255 {
		t.Errorf("the strip is not where it was placed")
	}
	if r, _, _, a := img.At(0, 3); r != 255 || a != 255 {
		t.Errorf("below the strip is (%d,..,alpha %d), want the root picture", r, a)
	}
}
