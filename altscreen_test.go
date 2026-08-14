package purfecterm

import "testing"

// Entering the alt screen gives a fresh blank screen with a home cursor; leaving
// restores the primary content and cursor.
func TestAltScreenEnterLeave(t *testing.T) {
	b := NewBuffer(10, 4, 100)
	p := NewParser(b)
	p.Parse([]byte("\x1b[1;1HMAIN")) // "MAIN" on row 0
	p.Parse([]byte("\x1b[3;1H"))     // cursor to row 2

	p.Parse([]byte("\x1b[?1049h")) // enter alt
	if !b.IsAltScreen() {
		t.Fatal("should be on alt screen")
	}
	cursorAt(t, b, 0, 0) // alt homes the cursor
	if got := cellChar(b, 0, 0); got != ' ' {
		t.Fatalf("alt screen should be blank, got %q", got)
	}
	p.Parse([]byte("ALT"))
	if got := cellChar(b, 0, 0); got != 'A' {
		t.Fatalf("alt write failed, got %q", got)
	}

	p.Parse([]byte("\x1b[?1049l")) // leave
	if b.IsAltScreen() {
		t.Fatal("should be back on primary screen")
	}
	if got := cellChar(b, 0, 0); got != 'M' {
		t.Fatalf("primary content not restored, got %q", got)
	}
	cursorAt(t, b, 0, 2) // primary cursor restored
}

// The alt screen has its own scrollback; using it never touches the primary's,
// which is restored intact on leave.
func TestAltScreenIndependentScrollback(t *testing.T) {
	b := NewBuffer(10, 3, 100)
	p := NewParser(b)
	p.Parse([]byte("m0\r\nm1\r\nm2\r\nm3")) // one line scrolls into primary scrollback
	mainSB := b.GetScrollbackSize()
	if mainSB == 0 {
		t.Fatal("expected primary scrollback")
	}

	p.Parse([]byte("\x1b[?1049h"))
	if b.GetScrollbackSize() != 0 {
		t.Fatalf("alt screen scrollback should start empty, got %d", b.GetScrollbackSize())
	}
	p.Parse([]byte("a0\r\na1\r\na2\r\na3\r\na4")) // scroll within the alt screen
	if b.GetScrollbackSize() == 0 {
		t.Fatal("alt screen should accumulate its own scrollback")
	}

	p.Parse([]byte("\x1b[?1049l"))
	if b.GetScrollbackSize() != mainSB {
		t.Fatalf("primary scrollback = %d after leave, want %d", b.GetScrollbackSize(), mainSB)
	}
}

type switchRecorder struct {
	NopCaptureObserver
	switches []bool
}

func (r *switchRecorder) OnScreenSwitch(toAlt bool) { r.switches = append(r.switches, toAlt) }

// OnScreenSwitch fires on every enter/leave with the right direction, and only
// on an actual switch (a redundant enter/leave is a no-op).
func TestAltScreenSwitchEvent(t *testing.T) {
	b := NewBuffer(10, 4, 100)
	p := NewParser(b)
	rec := &switchRecorder{}
	b.SetCaptureObserver(rec) // no SetCaptureLive needed; switch fires whenever an observer is set

	p.Parse([]byte("\x1b[?1049h")) // enter
	p.Parse([]byte("\x1b[?1049h")) // redundant enter (no-op)
	p.Parse([]byte("\x1b[?1049l")) // leave
	p.Parse([]byte("\x1b[?1049l")) // redundant leave (no-op)

	want := []bool{true, false}
	if len(rec.switches) != len(want) {
		t.Fatalf("switches = %v, want %v", rec.switches, want)
	}
	for i, w := range want {
		if rec.switches[i] != w {
			t.Fatalf("switches = %v, want %v", rec.switches, want)
		}
	}
}

// SetCaptureScope decides which screen's content events reach the observer; the
// switch event above is never scope-gated.
func TestCaptureScopeGating(t *testing.T) {
	cases := []struct {
		scope             CaptureScope
		wantMain, wantAlt bool
	}{
		{CaptureMain, true, false},
		{CaptureAlt, false, true},
		{CaptureBoth, true, true},
	}
	for _, tc := range cases {
		b := NewBuffer(10, 3, 100)
		p := NewParser(b)
		rec := &scrollRecorder{}
		b.SetCaptureObserver(rec)
		b.SetCaptureLive(true)
		b.SetCaptureScope(tc.scope)

		// Full-screen scroll on the primary screen.
		p.Parse([]byte("\x1b[3;1H\n"))
		mainFired := rec.scrollOff > 0

		// Full-screen scroll on the alt screen.
		rec.scrollOff = 0
		p.Parse([]byte("\x1b[?1049h\x1b[3;1H\n"))
		altFired := rec.scrollOff > 0

		if mainFired != tc.wantMain || altFired != tc.wantAlt {
			t.Errorf("scope %d: main=%v alt=%v, want main=%v alt=%v",
				tc.scope, mainFired, altFired, tc.wantMain, tc.wantAlt)
		}
	}
}

// A resize while the alt screen is active refits the primary screen on restore
// (no panic, content preserved, new size in effect).
func TestAltScreenResizeDuringAlt(t *testing.T) {
	b := NewBuffer(10, 4, 100)
	p := NewParser(b)
	p.Parse([]byte("\x1b[1;1HMAIN"))

	p.Parse([]byte("\x1b[?1049h"))
	b.Resize(10, 8)
	cols, rows := b.GetSize()
	if cols != 10 || rows != 8 {
		t.Fatalf("size after resize = (%d,%d), want (10,8)", cols, rows)
	}

	p.Parse([]byte("\x1b[?1049l"))
	if got := cellChar(b, 0, 0); got != 'M' {
		t.Fatalf("primary content lost across resize, got %q", got)
	}
	if _, rows := b.GetSize(); rows != 8 {
		t.Fatalf("primary rows after restore = %d, want 8", rows)
	}
}
