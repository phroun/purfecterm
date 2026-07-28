package purfecterm

import (
	"encoding/base64"
	"strings"
)

// OSC 52 — the clipboard escape sequence.
//
//	OSC 52 ; Pc ; Pd ST     (ESC ] 5 2 ; Pc ; Pd ESC \)
//	OSC 52 ; Pc ; Pd BEL    (the BEL-terminated form, equally common)
//
// Pc names the selection(s); Pd is either base64 data (a WRITE), "?" (a
// QUERY), or anything that is not valid base64 (a CLEAR — xterm's rule, and
// the safe reading of garbage: a malformed payload must not paste noise).
//
// This is the one channel a program running INSIDE a terminal has to the
// system clipboard. vim, neovim, emacs, tmux and zellij all emit it; a
// terminal without it is one where `vim "+y` silently does nothing.

// ClipboardSelectionClipboard and ClipboardSelectionPrimary are the two
// selections honored. The rest of xterm's set (q, s, cut buffers 0-7) is
// accepted in Pc and filtered out — no real program depends on them.
const (
	ClipboardSelectionClipboard = 'c'
	ClipboardSelectionPrimary   = 'p'
)

// ClipboardEvent is one OSC 52 operation, already decoded and policy-checked.
type ClipboardEvent struct {
	// Selections holds the supported selections named by Pc, in the order
	// 'c' then 'p'. Never empty: an empty Pc defaults to clipboard.
	Selections string

	// Data is the decoded payload for a write, and nil for a clear or a
	// query. A clear and a zero-length write are the same thing.
	Data []byte

	// Query is true for a read request. Data is nil; answer through the
	// reply function the callback was handed.
	Query bool
}

// ClipboardPolicy governs what OSC 52 is allowed to do. The zero value is
// not the default — see DefaultClipboardPolicy, which is what a parser
// starts with.
type ClipboardPolicy struct {
	// AllowWrite permits acting on writes and clears. Default TRUE: a write
	// is initiated by the user's own program, the worst case is clipboard
	// spam from something they chose to run, and this is the posture of
	// kitty, foot and Windows Terminal.
	AllowWrite bool

	// AllowRead permits answering queries. Default FALSE, and deliberately
	// so: a read lets ANY program that can print to the terminal — a `cat`ed
	// file, a malicious script's output — silently exfiltrate whatever is on
	// the clipboard, which is regularly a password. xterm and kitty both
	// default this off. A front end that wants it can enable it and put a
	// confirmation in its callback; the async reply makes that natural.
	AllowRead bool

	// Limit caps one payload's decoded size in bytes. Zero means the
	// default. An oversized payload is treated as a CLEAR rather than
	// truncated: half a secret on the clipboard is worse than none.
	Limit int
}

// DefaultClipboardLimit is a generous cap for one OSC 52 payload. kitty
// defaults to 8 MiB; xterm historically far less.
const DefaultClipboardLimit = 1 << 20 // 1 MiB

// DefaultClipboardPolicy is what a parser starts with: writes act, queries
// do not, one payload is capped at 1 MiB.
func DefaultClipboardPolicy() ClipboardPolicy {
	return ClipboardPolicy{AllowWrite: true, AllowRead: false, Limit: DefaultClipboardLimit}
}

// SetClipboardPolicy replaces the OSC 52 policy. A zero Limit means the
// default.
func (p *Parser) SetClipboardPolicy(pol ClipboardPolicy) {
	if pol.Limit <= 0 {
		pol.Limit = DefaultClipboardLimit
	}
	p.clipboardPolicy = pol
}

// ClipboardPolicy returns the OSC 52 policy in force.
func (p *Parser) ClipboardPolicy() ClipboardPolicy { return p.clipboardPolicy }

// SetOnClipboard registers the front end's clipboard bridge. Only the front
// end knows what "the clipboard" is on its platform:
//
//	GTK widget    gtk_clipboard_set_text (CLIPBOARD; PRIMARY for 'p')
//	Qt widget     QClipboard::setText (Clipboard / Selection modes)
//	embedded      hand it to the host (a KittyTK desktop, say)
//	standalone    RE-EMIT the sequence outward, so a PurfecTerm inside kitty
//	              or tmux forwards rather than handles, and nested stacks work
//
// For a write or a clear, act on ev. For a query, call reply once with the
// current contents — or never, to deny; the querying program is answered
// only when reply is called.
//
// Called on whatever goroutine is parsing. GTK and Qt both need to marshal
// to their UI thread; do that yourself.
//
// With no callback registered OSC 52 is consumed and dropped, which is the
// right default for a consumer that has not opted in.
func (p *Parser) SetOnClipboard(fn func(ev ClipboardEvent, reply func([]byte))) {
	p.onClipboard = fn
}

// SetResponseSink installs the channel by which the terminal answers the
// program running inside it. Responses go where keystrokes go — to the
// child's input — so a front end wires this to the same place.
//
// Used for OSC 52 query replies today. DSR and DA are stubbed in the CSI
// handler ("would need to send response") and want exactly this sink.
func (p *Parser) SetResponseSink(fn func([]byte)) { p.responseSink = fn }

// executeOSCClipboard handles a complete OSC 52 payload: "Pc;Pd".
func (p *Parser) executeOSCClipboard(args string) {
	if p.onClipboard == nil {
		return // consumed and dropped: nobody opted in
	}
	pc, pd, ok := strings.Cut(args, ";")
	if !ok {
		// No semicolon at all is malformed. Treat it as a clear of the
		// default selection rather than guessing at a payload.
		pd = ""
	}
	sel := supportedSelections(pc)

	if pd == "?" {
		if !p.clipboardPolicy.AllowRead {
			// NOT silence: a program blocks waiting for the answer. Say
			// "nothing" and let it get on with its life.
			p.sendClipboardReply(sel, nil)
			return
		}
		selCopy := sel
		p.onClipboard(ClipboardEvent{Selections: sel, Query: true}, func(data []byte) {
			p.sendClipboardReply(selCopy, data)
		})
		return
	}

	if !p.clipboardPolicy.AllowWrite {
		return
	}

	// Anything that is not valid base64 CLEARS, per xterm. So does an empty
	// payload, and so does one over the cap — a half-pasted secret is worse
	// than nothing.
	data, err := base64.StdEncoding.DecodeString(pd)
	if err != nil || len(data) == 0 || len(data) > p.clipboardPolicy.Limit {
		data = nil
	}
	p.onClipboard(ClipboardEvent{Selections: sel, Data: data}, nil)
}

// sendClipboardReply answers a query in the same shape it was asked.
func (p *Parser) sendClipboardReply(sel string, data []byte) {
	if p.responseSink == nil {
		return
	}
	var sb strings.Builder
	sb.WriteString("\x1b]52;")
	sb.WriteString(sel)
	sb.WriteByte(';')
	sb.WriteString(base64.StdEncoding.EncodeToString(data))
	sb.WriteString("\x1b\\")
	p.responseSink([]byte(sb.String()))
}

// supportedSelections filters Pc down to the selections that are acted on,
// in a stable order. An empty or wholly unsupported Pc means the clipboard:
// xterm's spec says empty defaults to "s0", but in practice every program
// that omits Pc means the clipboard, and answering a query with nothing
// selected would be a non-answer.
func supportedSelections(pc string) string {
	var sb strings.Builder
	if strings.ContainsRune(pc, ClipboardSelectionClipboard) {
		sb.WriteRune(ClipboardSelectionClipboard)
	}
	if strings.ContainsRune(pc, ClipboardSelectionPrimary) {
		sb.WriteRune(ClipboardSelectionPrimary)
	}
	if sb.Len() == 0 {
		return string(ClipboardSelectionClipboard)
	}
	return sb.String()
}
