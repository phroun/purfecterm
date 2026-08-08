package purfecterm

import (
	"bytes"
	"strings"
	"testing"
)

type recordObserver struct {
	NopCaptureObserver
	chunks [][]byte
}

func (r *recordObserver) OnOutput(data []byte) {
	// The slice is only valid for the call, so copy — exactly what a real
	// consumer must do to retain it.
	r.chunks = append(r.chunks, append([]byte(nil), data...))
}

// OnOutput tees the literal input, in the same chunks it was fed, before any
// parsing — and only once an observer is registered.
func TestCaptureObserverOnOutput(t *testing.T) {
	b := NewBuffer(20, 5, 100)
	p := NewParser(b)

	// No observer: no cost, nothing recorded, parsing still happens.
	p.Parse([]byte("before\r\n"))

	rec := &recordObserver{}
	p.SetCaptureObserver(rec)
	p.Parse([]byte("ab\x1b[31mc")) // a chunk carrying an escape
	p.Parse([]byte("d\r\n"))       // a second chunk

	if len(rec.chunks) != 2 {
		t.Fatalf("recorded %d chunks, want 2 (one per Parse)", len(rec.chunks))
	}
	if !bytes.Equal(rec.chunks[0], []byte("ab\x1b[31mc")) {
		t.Errorf("chunk 0 = %q, want the verbatim first feed", rec.chunks[0])
	}
	if !bytes.Equal(rec.chunks[1], []byte("d\r\n")) {
		t.Errorf("chunk 1 = %q, want the verbatim second feed", rec.chunks[1])
	}

	// Clearing the observer stops the tee.
	p.SetCaptureObserver(nil)
	p.Parse([]byte("after"))
	if len(rec.chunks) != 2 {
		t.Errorf("recorded %d chunks after clearing, want still 2", len(rec.chunks))
	}
}

type lineObserver struct {
	NopCaptureObserver
	off []string
}

func (o *lineObserver) OnLineOff(line []Cell, info LineInfo) {
	o.off = append(o.off, SerializeLineANS(line, info))
}

// OnLineOff fires as each line scrolls off the top; EmitRemainingScreenLines
// flushes the on-screen tail. Together they are the ordered transcript, and a
// plain line serializes to exactly its text.
func TestCaptureLineOff(t *testing.T) {
	b := NewBuffer(20, 3, 100) // 3 visible rows
	p := NewParser(b)
	obs := &lineObserver{}
	p.SetCaptureObserver(obs)

	// Five lines into a 3-row screen: the first two scroll off.
	p.Parse([]byte("L1\r\nL2\r\nL3\r\nL4\r\nL5"))
	if got := obs.off; len(got) != 2 || got[0] != "L1" || got[1] != "L2" {
		t.Fatalf("scrolled-off = %q, want [L1 L2] (plain, no escapes)", got)
	}

	// The tail still on screen completes the transcript, in order.
	b.EmitRemainingScreenLines()
	tail := obs.off[2:]
	if len(tail) != 3 || tail[0] != "L3" || tail[1] != "L4" || tail[2] != "L5" {
		t.Fatalf("flushed tail = %q, want [L3 L4 L5]", tail)
	}
}

// SerializeLineANS keeps colour as SGR and leaves plain runs bare.
func TestSerializeLineANS(t *testing.T) {
	b := NewBuffer(20, 3, 100)
	NewParser(b).Parse([]byte("a\x1b[31mb\x1b[0mc")) // a, red b, default c
	b.mu.RLock()
	line := b.screen[0]
	info := b.lineInfos[0]
	b.mu.RUnlock()
	got := SerializeLineANS(line, info)
	// Some SGR must wrap the b, and the a/c must be present as plain text.
	if !strings.Contains(got, "a") || !strings.Contains(got, "b") || !strings.Contains(got, "c") {
		t.Fatalf("serialized = %q, want a/b/c present", got)
	}
	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("serialized = %q, want SGR for the coloured cell", got)
	}
}
