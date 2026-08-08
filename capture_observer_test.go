package purfecterm

import (
	"bytes"
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
