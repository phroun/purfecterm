package purfecterm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// placeholderRun writes n placeholder cells for image id, the first carrying
// the image row and column and the rest inheriting from it — which is how a
// client actually emits a row of a virtual placement.
func placeholderRun(id uint32, imgRow, imgCol, n int) string {
	rowMark, _ := KittyDiacriticFor(imgRow)
	colMark, _ := KittyDiacriticFor(imgCol)
	var s strings.Builder
	fmt.Fprintf(&s, "\x1b[38;2;%d;%d;%dm", (id>>16)&0xff, (id>>8)&0xff, id&0xff)
	s.WriteRune(KittyPlaceholderRune)
	s.WriteRune(rowMark)
	s.WriteRune(colMark)
	for i := 1; i < n; i++ {
		s.WriteRune(KittyPlaceholderRune)
	}
	s.WriteString("\x1b[0m")
	return s.String()
}

// drawn flattens the two z bands into the order a renderer paints them.
func drawn(b *Buffer) []*PlacedImage {
	below, above := b.GetImagesByZ()
	return append(append([]*PlacedImage{}, below...), above...)
}

// A virtual placement is drawn INTO the placeholder cells that name it: one
// tile per cell run, each showing its own slice of the image, sized to the
// cells it covers rather than to the pixels it was cut from.
func TestKittyPlaceholderTiles(t *testing.T) {
	b, p, _ := newKittyTestBuffer() // 80x24, 10x20 px cells

	// A 40x40 image shown as 4x2 cells, virtual so it waits for placeholders.
	p.Parse(kittySeq("a=T,f=32,s=40,v=40,i=5,c=4,r=2,U=1", rgbaPayload(40, 40, 9, 9, 9, 255)))
	if got := drawn(b); len(got) != 0 {
		t.Fatalf("a virtual placement drew %d images before any placeholder cell", len(got))
	}

	// Two rows of four cells, starting at screen row 3, column 2.
	p.Parse([]byte("\x1b[4;3H" + placeholderRun(5, 0, 0, 4)))
	p.Parse([]byte("\x1b[5;3H" + placeholderRun(5, 1, 0, 4)))

	tiles := drawn(b)
	if len(tiles) != 2 {
		t.Fatalf("drew %d tiles, want 2 (one per row run)", len(tiles))
	}
	for i, tile := range tiles {
		if tile.Row != 3+i || tile.Col != 2 {
			t.Errorf("tile %d at (row %d, col %d), want (%d, 2)", i, tile.Row, tile.Col, 3+i)
		}
		if tile.CellsWide != 4 || tile.CellsHigh != 1 {
			t.Errorf("tile %d covers %dx%d cells, want 4x1", i, tile.CellsWide, tile.CellsHigh)
		}
		// The run spans the image's full width and one of its two rows.
		sx, sy, sw, sh := tile.SourceRect()
		if sx != 0 || sw != 40 {
			t.Errorf("tile %d source x span = %d+%d, want 0+40", i, sx, sw)
		}
		if wantY, wantH := i*20, 20; sy != wantY || sh != wantH {
			t.Errorf("tile %d source y span = %d+%d, want %d+%d", i, sy, sh, wantY, wantH)
		}
		// Drawn to fill its cells: 4 cells of 10px, one row of 20px.
		if dw, dh := tile.DestSize(); dw != 40 || dh != 20 {
			t.Errorf("tile %d dest = %dx%d px, want 40x20", i, dw, dh)
		}
		if tile.ImageID != 5 {
			t.Errorf("tile %d ImageID = %d, want 5", i, tile.ImageID)
		}
	}
}

// Tiles cut from the same image meet exactly: adjacent slices share an edge,
// with no pixel drawn twice and none skipped, even when the image does not
// divide evenly by the cell count.
func TestKittyPlaceholderTilesTileExactly(t *testing.T) {
	b, p, _ := newKittyTestBuffer()

	// 33 px across 4 cells: 8.25 px per cell, so the edges cannot all be round.
	p.Parse(kittySeq("a=T,f=32,s=33,v=10,i=6,c=4,r=1,U=1", rgbaPayload(33, 10, 1, 1, 1, 255)))

	// A space between image columns 1 and 2 splits the row into two runs, so
	// the seam falls at a known place and can be checked from both sides.
	p.Parse([]byte("\x1b[1;1H" + placeholderRun(6, 0, 0, 2) + " " + placeholderRun(6, 0, 2, 2)))

	tiles := drawn(b)
	if len(tiles) != 2 {
		t.Fatalf("drew %d tiles, want 2 (the space splits the row)", len(tiles))
	}
	ax, _, aw, _ := tiles[0].SourceRect()
	bx, _, bw, _ := tiles[1].SourceRect()
	if ax != 0 {
		t.Errorf("first tile starts at x=%d, want 0", ax)
	}
	if bx != ax+aw {
		t.Errorf("second tile starts at x=%d but the first ends at %d: tiles must meet exactly",
			bx, ax+aw)
	}
	if bx+bw != 33 {
		t.Errorf("tiles reach x=%d, want 33: the last tile must cover the remainder", bx+bw)
	}
}

// A placeholder cell with no diacritics continues the run to its left. A
// non-placeholder cell breaks that inheritance, so the next run starts over.
func TestKittyPlaceholderInheritanceStopsAtOtherText(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	p.Parse(kittySeq("a=T,f=32,s=40,v=20,i=8,c=4,r=1,U=1", rgbaPayload(40, 20, 2, 2, 2, 255)))

	// Two cells, a space, then two more bare placeholders. The bare ones after
	// the space have nothing to inherit, so they are image column 0 again.
	p.Parse([]byte("\x1b[1;1H" + placeholderRun(8, 0, 0, 2) + " " +
		placeholderRun(8, 0, 0, 1)[:len(placeholderRun(8, 0, 0, 1))]))

	tiles := drawn(b)
	if len(tiles) != 2 {
		t.Fatalf("drew %d tiles, want 2 (the space breaks the run)", len(tiles))
	}
	if tiles[0].Col != 0 || tiles[0].CellsWide != 2 {
		t.Errorf("first run at col %d covering %d cells, want 0 covering 2",
			tiles[0].Col, tiles[0].CellsWide)
	}
	if tiles[1].Col != 3 {
		t.Errorf("second run at col %d, want 3 (after the space)", tiles[1].Col)
	}
	if sx, _, _, _ := tiles[1].SourceRect(); sx != 0 {
		t.Errorf("second run shows image x=%d, want 0: inheritance must not cross the space", sx)
	}
}

// A placeholder for an image that was never transmitted, or has been deleted,
// simply draws nothing.
func TestKittyPlaceholderForMissingImageDrawsNothing(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	p.Parse([]byte("\x1b[1;1H" + placeholderRun(404, 0, 0, 4)))
	if got := drawn(b); len(got) != 0 {
		t.Fatalf("drew %d images for a placeholder naming no image", len(got))
	}

	p.Parse(kittySeq("a=T,f=32,s=20,v=20,i=404,c=2,r=1,U=1", rgbaPayload(20, 20, 3, 3, 3, 255)))
	if got := drawn(b); len(got) != 1 {
		t.Fatalf("drew %d images once the image arrived, want 1", len(got))
	}
	p.Parse([]byte("\x1b_Ga=d,d=I,i=404\x1b\\"))
	if got := drawn(b); len(got) != 0 {
		t.Fatalf("drew %d images after the placement was deleted", len(got))
	}
}

// A relative placement (P=) has no anchor of its own: it sits at its parent's
// position plus the H/V cell offset, and follows the parent when the screen
// scrolls it.
func TestKittyRelativePlacement(t *testing.T) {
	b, p, replies := newKittyTestBuffer()

	p.Parse([]byte("\x1b[5;3H")) // row 4, col 2
	p.Parse(kittySeq("a=T,f=32,s=40,v=40,i=1,p=1", rgbaPayload(40, 40, 1, 1, 1, 255)))
	p.Parse(kittySeq("a=T,f=32,s=10,v=20,i=2,P=1,Q=1,H=3,V=1", rgbaPayload(10, 20, 2, 2, 2, 255)))

	for _, r := range *replies {
		if strings.Contains(r, "ENOENT") {
			t.Fatalf("relative placement refused: %q", r)
		}
	}

	find := func(list []*PlacedImage, id uint32) *PlacedImage {
		for _, im := range list {
			if im.ImageID == id {
				return im
			}
		}
		return nil
	}
	parent := find(drawn(b), 1)
	child := find(drawn(b), 2)
	if parent == nil || child == nil {
		t.Fatalf("drew %d images, want the parent and its relative", len(drawn(b)))
	}
	if child.Row != parent.Row+1 || child.Col != parent.Col+3 {
		t.Errorf("child at (%d,%d), want (%d,%d) — parent (%d,%d) plus V=1,H=3",
			child.Row, child.Col, parent.Row+1, parent.Col+3, parent.Row, parent.Col)
	}

	// Scroll the parent up a row; the child must move with it, not stay put.
	before := parent.Row
	b.ScrollUp(1)
	parent, child = find(drawn(b), 1), find(drawn(b), 2)
	if parent == nil || child == nil {
		t.Fatal("an image was dropped by the scroll")
	}
	if parent.Row != before-1 {
		t.Fatalf("parent moved to row %d, want %d", parent.Row, before-1)
	}
	if child.Row != parent.Row+1 {
		t.Errorf("child at row %d after scrolling, want %d: the offset must be reapplied, not baked in",
			child.Row, parent.Row+1)
	}
}

// A relative placement can hang off a VIRTUAL parent, which is only positioned
// once its placeholder cells are printed — and moves whenever they move.
func TestKittyRelativeToVirtualParent(t *testing.T) {
	b, p, _ := newKittyTestBuffer()

	p.Parse(kittySeq("a=T,f=32,s=40,v=20,i=10,c=4,r=1,U=1", rgbaPayload(40, 20, 1, 1, 1, 255)))
	p.Parse([]byte("\x1b[3;5H" + placeholderRun(10, 0, 0, 4))) // row 2, col 4
	p.Parse(kittySeq("a=T,f=32,s=10,v=20,i=11,P=10,H=1,V=2", rgbaPayload(10, 20, 2, 2, 2, 255)))

	var child *PlacedImage
	for _, im := range drawn(b) {
		if im.ImageID == 11 {
			child = im
		}
	}
	if child == nil {
		t.Fatal("the relative placement was not drawn against its virtual parent")
	}
	if child.Row != 4 || child.Col != 5 {
		t.Errorf("child at (%d,%d), want (4,5): the placeholder run's corner plus V=2,H=1",
			child.Row, child.Col)
	}

	// Reprint the placeholder cells somewhere else. Nothing about the child
	// changed, but its parent is now elsewhere, so a position baked in when the
	// child was created would be stale — the offset has to be reapplied.
	p.Parse([]byte("\x1b[3;5H    "))                            // erase the old run
	p.Parse([]byte("\x1b[8;11H" + placeholderRun(10, 0, 0, 4))) // row 7, col 10

	child = nil
	for _, im := range drawn(b) {
		if im.ImageID == 11 {
			child = im
		}
	}
	if child == nil {
		t.Fatal("the relative placement was dropped when its parent moved")
	}
	if child.Row != 9 || child.Col != 11 {
		t.Errorf("child at (%d,%d) after the placeholders moved, want (9,11)",
			child.Row, child.Col)
	}
}

// A relative placement cannot outlive its parent, and neither can a chain of
// them: deleting the root takes the whole tree.
func TestKittyRelativePlacementDeletedWithParent(t *testing.T) {
	b, p, _ := newKittyTestBuffer()
	p.Parse(kittySeq("a=T,f=32,s=20,v=20,i=1", rgbaPayload(20, 20, 1, 1, 1, 255)))
	p.Parse(kittySeq("a=T,f=32,s=20,v=20,i=2,P=1,H=1", rgbaPayload(20, 20, 2, 2, 2, 255)))
	p.Parse(kittySeq("a=T,f=32,s=20,v=20,i=3,P=2,H=1", rgbaPayload(20, 20, 3, 3, 3, 255)))
	if got := len(b.GetImages()); got != 3 {
		t.Fatalf("placed %d images, want 3", got)
	}

	p.Parse([]byte("\x1b_Ga=d,d=i,i=1\x1b\\"))
	if got := b.GetImages(); len(got) != 0 {
		t.Errorf("%d placements survived deleting the root of the chain", len(got))
	}
}

// Naming a parent that is not placed is an error, not a placement stashed at
// the origin: the client is told so it can place the parent first.
func TestKittyRelativeToMissingParentIsNotFound(t *testing.T) {
	b, p, replies := newKittyTestBuffer()
	p.Parse(kittySeq("a=T,f=32,s=20,v=20,i=9,P=1234,H=1", rgbaPayload(20, 20, 1, 1, 1, 255)))

	if got := len(b.GetImages()); got != 0 {
		t.Errorf("placed %d images against a parent that does not exist", got)
	}
	if len(*replies) != 1 || !strings.Contains((*replies)[0], "ENOENT") {
		t.Errorf("replies = %q, want one ENOENT", *replies)
	}
}

// t=t has the terminal delete the file it read. The guard must resolve the
// path, or "/tmp/../<anything>" turns an image transmission into an arbitrary
// unlink.
func TestKittyTempPathGuardResistsTraversal(t *testing.T) {
	// A real file that is definitely NOT in any temp directory, and that the
	// test only ever names — never writes to and never removes.
	const outside = "/etc/passwd"
	if _, err := os.Stat(outside); err != nil {
		t.Skipf("no %s to aim the guard at: %v", outside, err)
	}

	inside := filepath.Join(os.TempDir(), "purfecterm-guard-test")
	if err := os.WriteFile(inside, []byte("x"), 0o600); err != nil {
		t.Skipf("cannot write in %s: %v", os.TempDir(), err)
	}
	defer os.Remove(inside)
	if !isTempPath(inside) {
		t.Errorf("a real file in %s was not recognized as a temp path", os.TempDir())
	}

	// The traversal spelling: built by concatenation, not filepath.Join, since
	// Join would clean the ".." away and the raw string is the attack.
	traversal := os.TempDir() + "/.." + outside
	if isTempPath(traversal) {
		t.Errorf("%q passed the temp-path guard: t=t would unlink %s", traversal, outside)
	}

	// A symlink inside a temp directory pointing out of it must not launder the
	// target either.
	link := filepath.Join(os.TempDir(), "purfecterm-guard-link")
	os.Remove(link)
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("cannot create a symlink: %v", err)
	}
	defer os.Remove(link)
	if isTempPath(link) {
		t.Errorf("a symlink out of the temp directory passed the guard")
	}
}

// t=f names a file the terminal opens. A pipe or a character device never ends,
// so reading one would hang the terminal or exhaust its memory; only a regular
// file can hold an image, and only a regular file is opened.
func TestKittyFileReadRefusesNonRegularFiles(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("cannot create a fifo: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := readKittyFile(fifo); err == nil {
			t.Error("reading a fifo succeeded; it should be refused unopened")
		}
	}()
	<-done

	regular := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(regular, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := readKittyFile(regular)
	if err != nil || string(data) != "hello" {
		t.Errorf("readKittyFile(regular) = %q, %v; want \"hello\", nil", data, err)
	}
}
