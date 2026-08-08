package purfecterm

import "testing"

// liveEvent is a flattened record of a single live-capture callback, so a test
// can assert on the exact ordered stream the observer saw.
type liveEvent struct {
	kind string // "write","cursor","newline","wrap","backspace","scrolloff","clear"
	x, y int
	text string
	sgr  string
	n    int
}

type liveObserver struct {
	NopCaptureObserver
	events []liveEvent
}

func (o *liveObserver) OnWrite(x, y int, text, sgr string) {
	o.events = append(o.events, liveEvent{kind: "write", x: x, y: y, text: text, sgr: sgr})
}
func (o *liveObserver) OnCursorMove(x, y int) {
	o.events = append(o.events, liveEvent{kind: "cursor", x: x, y: y})
}
func (o *liveObserver) OnNewline(x, y int) {
	o.events = append(o.events, liveEvent{kind: "newline", x: x, y: y})
}
func (o *liveObserver) OnLineWrap(x, y int) {
	o.events = append(o.events, liveEvent{kind: "wrap", x: x, y: y})
}
func (o *liveObserver) OnBackspace(x, y int) {
	o.events = append(o.events, liveEvent{kind: "backspace", x: x, y: y})
}
func (o *liveObserver) OnScrollLineOff(n int) {
	o.events = append(o.events, liveEvent{kind: "scrolloff", n: n})
}
func (o *liveObserver) OnClearScreen(sgr string) {
	o.events = append(o.events, liveEvent{kind: "clear", text: sgr})
}
func (o *liveObserver) OnClearEndOfLine(x, y int, sgr string) {
	o.events = append(o.events, liveEvent{kind: "eol", x: x, y: y, text: sgr})
}
func (o *liveObserver) OnClearBeginOfLine(x, y int, sgr string) {
	o.events = append(o.events, liveEvent{kind: "bol", x: x, y: y, text: sgr})
}
func (o *liveObserver) OnClearLine(y int, sgr string) {
	o.events = append(o.events, liveEvent{kind: "line", y: y, text: sgr})
}
func (o *liveObserver) OnClearEndOfScreen(x, y int, sgr string) {
	o.events = append(o.events, liveEvent{kind: "eos", x: x, y: y, text: sgr})
}
func (o *liveObserver) OnClearBeginOfScreen(x, y int, sgr string) {
	o.events = append(o.events, liveEvent{kind: "bos", x: x, y: y, text: sgr})
}

func (o *liveObserver) kinds() []string {
	ks := make([]string, len(o.events))
	for i, e := range o.events {
		ks[i] = e.kind
	}
	return ks
}

func newLiveTerm(cols, rows int) (*Buffer, *Parser, *liveObserver) {
	b := NewBuffer(cols, rows, 100)
	p := NewParser(b)
	obs := &liveObserver{}
	p.SetCaptureObserver(obs)
	b.SetCaptureLive(true)
	return b, p, obs
}

// With live capture off, none of the live events fire even though structural
// handlers all run — the rung is opt-in and costs nothing when disabled.
func TestCaptureLiveGating(t *testing.T) {
	b := NewBuffer(20, 3, 100)
	p := NewParser(b)
	obs := &liveObserver{}
	p.SetCaptureObserver(obs)
	// Live NOT enabled.
	p.Parse([]byte("ab\r\ncd\r\nef\r\ngh"))
	if len(obs.events) != 0 {
		t.Fatalf("live off: got %d events, want 0", len(obs.events))
	}

	// Turning it on begins emission; turning it off stops it again.
	b.SetCaptureLive(true)
	p.Parse([]byte("x"))
	if len(obs.events) == 0 {
		t.Fatal("live on: expected write events")
	}
	before := len(obs.events)
	b.SetCaptureLive(false)
	p.Parse([]byte("y"))
	if len(obs.events) != before {
		t.Fatalf("live off again: events grew from %d to %d", before, len(obs.events))
	}
}

// A plain run of characters batches into a single OnWrite at its start
// position with an empty (default-pen) SGR, flushed at end of feed.
func TestCaptureLiveWriteBatch(t *testing.T) {
	_, p, obs := newLiveTerm(20, 3)
	p.Parse([]byte("hello"))
	writes := 0
	for _, e := range obs.events {
		if e.kind == "write" {
			writes++
			if e.text != "hello" || e.x != 0 || e.y != 0 || e.sgr != "" {
				t.Fatalf("write = %+v, want {hello 0 0 <empty>}", e)
			}
		}
	}
	if writes != 1 {
		t.Fatalf("got %d write events, want 1 batched run", writes)
	}
}

// A pen change splits the write run and carries an absolute 0;-prefixed SGR on
// the coloured segment.
func TestCaptureLiveWritePenSplit(t *testing.T) {
	_, p, obs := newLiveTerm(20, 3)
	p.Parse([]byte("ab\x1b[31mcd"))
	var writes []liveEvent
	for _, e := range obs.events {
		if e.kind == "write" {
			writes = append(writes, e)
		}
	}
	if len(writes) != 2 {
		t.Fatalf("got %d write runs, want 2 (pen split)", len(writes))
	}
	if writes[0].text != "ab" || writes[0].sgr != "" {
		t.Errorf("run 0 = %+v, want {ab <empty>}", writes[0])
	}
	if writes[1].text != "cd" || writes[1].x != 2 {
		t.Errorf("run 1 = %+v, want text cd at x=2", writes[1])
	}
	if writes[1].sgr == "" || writes[1].sgr[:2] != "\x1b[" {
		t.Errorf("run 1 sgr = %q, want an SGR sequence", writes[1].sgr)
	}
}

// A newline flushes the pending run and then fires OnNewline, in that order.
func TestCaptureLiveNewline(t *testing.T) {
	_, p, obs := newLiveTerm(20, 3)
	p.Parse([]byte("ab\r\ncd"))
	// Expect: write(ab), cursor(CR->0), newline, write(cd).
	// The \r is a CarriageReturn (cursor), the \n a LineFeed (newline).
	k := obs.kinds()
	// Find the newline; the write before it must be "ab".
	nl := -1
	for i, e := range obs.events {
		if e.kind == "newline" {
			nl = i
			break
		}
	}
	if nl < 0 {
		t.Fatalf("no newline event; kinds=%v", k)
	}
	// The last write before the newline is the "ab" run.
	sawAB := false
	for i := 0; i < nl; i++ {
		if obs.events[i].kind == "write" && obs.events[i].text == "ab" {
			sawAB = true
		}
	}
	if !sawAB {
		t.Fatalf("expected ab flushed before newline; kinds=%v", k)
	}
	if obs.events[nl].y != 1 {
		t.Errorf("newline y = %d, want 1", obs.events[nl].y)
	}
}

// Auto-wrap at the right margin fires OnLineWrap after flushing the current run.
func TestCaptureLiveWrap(t *testing.T) {
	_, p, obs := newLiveTerm(4, 3) // 4 columns
	p.Parse([]byte("abcdef"))      // 6 chars into 4 cols -> wraps
	wraps := 0
	for _, e := range obs.events {
		if e.kind == "wrap" {
			wraps++
		}
	}
	if wraps < 1 {
		t.Fatalf("got %d wrap events, want >=1; kinds=%v", wraps, obs.kinds())
	}
}

// Absolute cursor positioning (CUP) fires OnCursorMove with the clamped target.
func TestCaptureLiveCursorMove(t *testing.T) {
	_, p, obs := newLiveTerm(20, 5)
	p.Parse([]byte("\x1b[3;5H")) // row 3, col 5 (1-based) -> y=2, x=4
	var mv *liveEvent
	for i := range obs.events {
		if obs.events[i].kind == "cursor" {
			mv = &obs.events[i]
		}
	}
	if mv == nil {
		t.Fatalf("no cursor event; kinds=%v", obs.kinds())
	}
	if mv.x != 4 || mv.y != 2 {
		t.Errorf("cursor = (%d,%d), want (4,2)", mv.x, mv.y)
	}
}

// Backspace fires OnBackspace at the new position.
func TestCaptureLiveBackspace(t *testing.T) {
	_, p, obs := newLiveTerm(20, 3)
	p.Parse([]byte("ab\b"))
	bs := 0
	for _, e := range obs.events {
		if e.kind == "backspace" {
			bs++
			if e.x != 1 {
				t.Errorf("backspace x = %d, want 1", e.x)
			}
		}
	}
	if bs != 1 {
		t.Fatalf("got %d backspace events, want 1; kinds=%v", bs, obs.kinds())
	}
}

// Scrolling the screen fires OnScrollLineOff(1) per line pushed off the top.
func TestCaptureLiveScrollOff(t *testing.T) {
	_, p, obs := newLiveTerm(20, 3) // 3 rows
	p.Parse([]byte("L1\r\nL2\r\nL3\r\nL4\r\nL5"))
	off := 0
	for _, e := range obs.events {
		if e.kind == "scrolloff" {
			off += e.n
		}
	}
	if off != 2 {
		t.Fatalf("scrolled off %d lines, want 2; kinds=%v", off, obs.kinds())
	}
}

// A screen clear fires OnClearScreen carrying the pen active at the clear (the
// screen background), so a blue-background clear reports the blue pen.
func TestCaptureLiveClear(t *testing.T) {
	_, p, obs := newLiveTerm(20, 3)
	p.Parse([]byte("hi\x1b[44m\x1b[2J")) // set bg blue, then clear
	var clear *liveEvent
	for i := range obs.events {
		if obs.events[i].kind == "clear" {
			clear = &obs.events[i]
		}
	}
	if clear == nil {
		t.Fatalf("no clear event; kinds=%v", obs.kinds())
	}
	if clear.text == "" || !containsSub(clear.text, "44") {
		t.Fatalf("clear pen = %q, want the blue (44) background", clear.text)
	}
}

// An erase-to-end-of-line fires OnClearToEndOfLine at the cursor, carrying the
// current pen whose background fills the erased tail.
func TestCaptureLiveClearToEOL(t *testing.T) {
	_, p, obs := newLiveTerm(20, 3)
	p.Parse([]byte("abcdef\x1b[3G\x1b[41m\x1b[K")) // write, cursor to col 3, bg red, erase to EOL
	var eol *liveEvent
	for i := range obs.events {
		if obs.events[i].kind == "eol" {
			eol = &obs.events[i]
		}
	}
	if eol == nil {
		t.Fatalf("no clear-to-EOL event; kinds=%v", obs.kinds())
	}
	if eol.x != 2 {
		t.Errorf("EOL x = %d, want 2 (cursor col)", eol.x)
	}
	if !containsSub(eol.text, "41") {
		t.Errorf("EOL pen = %q, want the red (41) background", eol.text)
	}
}

// The rest of the erase family each fires its own event, carrying the current
// pen: EL1 begin-of-line, EL2 whole line, ED0 end-of-screen, ED1 begin-of-screen.
func TestCaptureLiveEraseFamily(t *testing.T) {
	cases := []struct {
		name string
		seq  string
		kind string
		bg   string
	}{
		{"begin-of-line (EL1)", "abc\x1b[2G\x1b[41m\x1b[1K", "bol", "41"},
		{"whole line (EL2)", "abc\x1b[42m\x1b[2K", "line", "42"},
		{"end-of-screen (ED0)", "abc\x1b[43m\x1b[0J", "eos", "43"},
		{"begin-of-screen (ED1)", "abc\x1b[44m\x1b[1J", "bos", "44"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, p, obs := newLiveTerm(20, 3)
			p.Parse([]byte(c.seq))
			var ev *liveEvent
			for i := range obs.events {
				if obs.events[i].kind == c.kind {
					ev = &obs.events[i]
				}
			}
			if ev == nil {
				t.Fatalf("no %s event; kinds=%v", c.kind, obs.kinds())
			}
			if !containsSub(ev.text, c.bg) {
				t.Errorf("%s pen = %q, want bg %s", c.kind, ev.text, c.bg)
			}
		})
	}
}

func containsSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
