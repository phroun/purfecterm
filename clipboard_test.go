package purfecterm

import (
	"encoding/base64"
	"strings"
	"testing"
)

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// osc52 collects what a parser reports for one fed sequence.
func osc52(t *testing.T, seq string, tune func(*Parser)) (evs []ClipboardEvent, replies []string, scr string) {
	t.Helper()
	b := NewBuffer(40, 3, 100)
	p := NewParser(b)
	p.SetOnClipboard(func(ev ClipboardEvent, reply func([]byte)) {
		evs = append(evs, ev)
		if ev.Query && reply != nil {
			reply([]byte("from the host"))
		}
	})
	p.SetResponseSink(func(d []byte) { replies = append(replies, string(d)) })
	if tune != nil {
		tune(p)
	}
	p.Parse([]byte(seq))
	return evs, replies, screen(b)
}

// A write carries the decoded payload through to the front end, in both
// terminator forms. ST is the one real programs use, and the one that used to
// leave a stray backslash behind.
func TestOSC52Write(t *testing.T) {
	for _, seq := range []string{
		"\x1b]52;c;" + b64("hello") + "\x1b\\",
		"\x1b]52;c;" + b64("hello") + "\x07",
	} {
		evs, _, scr := osc52(t, seq, nil)
		if len(evs) != 1 {
			t.Fatalf("%q produced %d events, want 1", seq, len(evs))
		}
		if string(evs[0].Data) != "hello" {
			t.Errorf("%q decoded %q, want \"hello\"", seq, evs[0].Data)
		}
		if evs[0].Selections != "c" {
			t.Errorf("%q selections %q, want \"c\"", seq, evs[0].Selections)
		}
		if scr != "" {
			t.Errorf("%q left %q on the screen", seq, scr)
		}
	}
}

// THE ST BUG, on its own. handleOSCString ended the sequence on the ESC and
// left the '\' to be printed from ground state. Every ST-terminated OSC did
// this; it went unnoticed because the 7000-series are BEL-terminated in
// practice.
func TestSTTerminatedOSCLeavesNoBackslash(t *testing.T) {
	for _, seq := range []string{
		"\x1b]52;c;" + b64("x") + "\x1b\\", // a supported OSC
		"\x1b]0;some window title\x1b\\",   // an UNsupported one: still consumed
		"\x1b]7000;da\x1b\\",               // a PurfecTerm private one
	} {
		b := NewBuffer(40, 3, 100)
		NewParser(b).Parse([]byte(seq))
		if got := screen(b); got != "" {
			t.Errorf("%q left %q on the screen", seq, got)
		}
	}
}

// An ESC inside an OSC that is NOT ST abandons the OSC and starts the
// sequence it really began -- it must not be swallowed, and it must not
// execute the half-collected OSC.
func TestESCInsideOSCThatIsNotSTStartsANewSequence(t *testing.T) {
	var evs []ClipboardEvent
	b := NewBuffer(40, 3, 100)
	p := NewParser(b)
	p.SetOnClipboard(func(ev ClipboardEvent, _ func([]byte)) { evs = append(evs, ev) })
	// An OSC 52 interrupted by a CSI, then ordinary text.
	p.Parse([]byte("\x1b]52;c;" + b64("nope") + "\x1b[2Jok"))
	if len(evs) != 0 {
		t.Errorf("an interrupted OSC executed anyway: %+v", evs)
	}
	if got := screen(b); got != "ok" {
		t.Errorf("screen = %q, want %q -- the CSI should have run and the text printed", got, "ok")
	}
}

// Selections are filtered to the ones acted on, and an empty or unsupported
// Pc means the clipboard.
func TestOSC52Selections(t *testing.T) {
	for _, tc := range []struct{ pc, want string }{
		{"c", "c"},
		{"p", "p"},
		{"cp", "cp"},
		{"pc", "cp"}, // stable order, not the order given
		{"", "c"},
		{"s0", "c"}, // cut buffers are not honored; fall back to clipboard
		{"qc", "c"},
	} {
		evs, _, _ := osc52(t, "\x1b]52;"+tc.pc+";"+b64("x")+"\x1b\\", nil)
		if len(evs) != 1 || evs[0].Selections != tc.want {
			t.Errorf("Pc=%q -> %+v, want selections %q", tc.pc, evs, tc.want)
		}
	}
}

// Anything that is not valid base64 CLEARS rather than pasting noise, and so
// does an empty payload. This is xterm's rule.
func TestOSC52ClearsOnGarbage(t *testing.T) {
	for _, pd := range []string{"", "!!!not base64!!!", "%%%%"} {
		evs, _, _ := osc52(t, "\x1b]52;c;"+pd+"\x1b\\", nil)
		if len(evs) != 1 {
			t.Fatalf("Pd=%q produced %d events, want 1", pd, len(evs))
		}
		if evs[0].Data != nil {
			t.Errorf("Pd=%q gave data %q, want a clear (nil)", pd, evs[0].Data)
		}
	}
}

// An oversized payload is a clear, not a truncation: half a secret on the
// clipboard is worse than none.
func TestOSC52OversizedPayloadClears(t *testing.T) {
	big := b64(strings.Repeat("A", 5000))
	evs, _, _ := osc52(t, "\x1b]52;c;"+big+"\x1b\\", func(p *Parser) {
		p.SetClipboardPolicy(ClipboardPolicy{AllowWrite: true, Limit: 1024})
	})
	if len(evs) != 1 || evs[0].Data != nil {
		t.Errorf("oversized payload -> %+v, want a clear", evs)
	}
}

// Writes act by default; reads do not.
func TestOSC52Defaults(t *testing.T) {
	pol := DefaultClipboardPolicy()
	if !pol.AllowWrite {
		t.Error("writes should act by default")
	}
	if pol.AllowRead {
		t.Error("reads must NOT be answered by default: any program that can print could exfiltrate the clipboard")
	}
	// And a fresh parser starts there, rather than at the zero value.
	if got := NewParser(NewBuffer(10, 2, 10)).ClipboardPolicy(); !got.AllowWrite || got.AllowRead {
		t.Errorf("NewParser policy = %+v, want the documented default", got)
	}
}

// A query is answered from the front end, in the same shape it was asked.
func TestOSC52QueryWhenAllowed(t *testing.T) {
	evs, replies, _ := osc52(t, "\x1b]52;c;?\x1b\\", func(p *Parser) {
		p.SetClipboardPolicy(ClipboardPolicy{AllowWrite: true, AllowRead: true})
	})
	if len(evs) != 1 || !evs[0].Query {
		t.Fatalf("query produced %+v, want one query event", evs)
	}
	want := "\x1b]52;c;" + b64("from the host") + "\x1b\\"
	if len(replies) != 1 || replies[0] != want {
		t.Errorf("replied %q, want %q", replies, want)
	}
}

// Denied, the query is answered with an EMPTY payload rather than silence:
// programs block waiting for the reply.
func TestOSC52QueryDeniedAnswersEmpty(t *testing.T) {
	evs, replies, _ := osc52(t, "\x1b]52;c;?\x1b\\", nil) // read off by default
	if len(evs) != 0 {
		t.Errorf("a denied query should not reach the front end: %+v", evs)
	}
	want := "\x1b]52;c;\x1b\\"
	if len(replies) != 1 || replies[0] != want {
		t.Errorf("replied %q, want %q (empty, not silence)", replies, want)
	}
}

// With writes disabled, a write is dropped -- and does not become a clear.
func TestOSC52WriteDenied(t *testing.T) {
	evs, _, _ := osc52(t, "\x1b]52;c;"+b64("x")+"\x1b\\", func(p *Parser) {
		p.SetClipboardPolicy(ClipboardPolicy{AllowWrite: false})
	})
	if len(evs) != 0 {
		t.Errorf("writes disabled, but the front end heard %+v", evs)
	}
}

// No callback registered: consumed and dropped, nothing on screen. This is
// the default for a consumer that has not opted in.
func TestOSC52WithoutACallbackIsSilent(t *testing.T) {
	b := NewBuffer(40, 3, 100)
	NewParser(b).Parse([]byte("\x1b]52;c;" + b64("hello") + "\x1b\\"))
	if got := screen(b); got != "" {
		t.Errorf("left %q on the screen", got)
	}
}
