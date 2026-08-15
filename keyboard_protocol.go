package purfecterm

// The kitty keyboard protocol's progressive enhancement negotiation.
//
//	CSI ? u              query the current flags; the terminal answers CSI ? <flags> u
//	CSI > <flags> u      push the current flags and set these
//	CSI < <number> u     pop <number> entries (default 1)
//	CSI = <flags> ; <mode> u   set (1), set bits (2), or reset bits (3)
//
// The query is how an application DETECTS support: a terminal that does not
// implement the protocol simply never answers, so one that answers must mean it.
// That is why the reply matters as much as the flags — an application asking
// "extended keyboard support" is asking this question and listening.

// Kitty keyboard protocol flags, as bits in the enhancement set.
const (
	// KeyboardDisambiguate makes every key send an unambiguous escape code,
	// so Esc, Ctrl-I and Tab, Ctrl-M and Enter stop colliding.
	KeyboardDisambiguate = 1
	// KeyboardReportEvents reports key release and repeat, not only press.
	KeyboardReportEvents = 2
	// KeyboardReportAlternates adds shifted and base-layout key codes.
	KeyboardReportAlternates = 4
	// KeyboardReportAllKeys sends every key as an escape code, including plain
	// text keys.
	KeyboardReportAllKeys = 8
	// KeyboardReportText appends the text a key would have produced.
	KeyboardReportText = 16

	// KeyboardAllFlags is every flag this terminal implements, which is all of
	// the ones the protocol defines.
	KeyboardAllFlags = KeyboardDisambiguate | KeyboardReportEvents |
		KeyboardReportAlternates | KeyboardReportAllKeys | KeyboardReportText
)

// maxKeyboardStackDepth bounds the push stack. An application that pushes
// without popping — or one that crashes mid-session — must not be able to grow
// it without limit; past the cap the oldest entry is dropped, which degrades to
// the wrong flags rather than to unbounded memory.
const maxKeyboardStackDepth = 16

// keyboardState is one screen's enhancement flags and its push stack. The main
// and alternate screens keep separate stacks, so a full-screen application that
// pushes flags on entry and pops them on exit cannot disturb the shell it
// suspends back to.
type keyboardState struct {
	flags int
	stack []int
}

// push saves the current flags and adopts new ones.
func (k *keyboardState) push(flags int) {
	if len(k.stack) >= maxKeyboardStackDepth {
		k.stack = k.stack[1:]
	}
	k.stack = append(k.stack, k.flags)
	k.flags = flags & KeyboardAllFlags
}

// pop restores n saved entries. Popping an empty stack resets to no
// enhancements, which is the state a fresh terminal is in.
func (k *keyboardState) pop(n int) {
	if n < 1 {
		n = 1
	}
	for ; n > 0; n-- {
		if len(k.stack) == 0 {
			k.flags = 0
			return
		}
		k.flags = k.stack[len(k.stack)-1]
		k.stack = k.stack[:len(k.stack)-1]
	}
}

// set applies flags per the protocol's mode: 1 replaces, 2 sets the given bits,
// 3 clears them. An unknown mode is treated as 1, the default.
func (k *keyboardState) set(flags, mode int) {
	flags &= KeyboardAllFlags
	switch mode {
	case 2:
		k.flags |= flags
	case 3:
		k.flags &^= flags
	default:
		k.flags = flags
	}
}

// keyboard returns the state for the screen currently in use, so the main and
// alternate screens keep independent stacks. Caller holds the lock.
func (b *Buffer) keyboard() *keyboardState {
	if b.onAltScreen {
		return &b.altKeyboard
	}
	return &b.mainKeyboard
}

// KeyboardFlags returns the active enhancement flags for the current screen.
// A renderer encodes key events according to these.
func (b *Buffer) KeyboardFlags() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.keyboard().flags
}

// SetKeyboardFlags replaces the active flags, for a host driving the terminal
// directly rather than through an escape sequence.
func (b *Buffer) SetKeyboardFlags(flags int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.keyboard().set(flags, 1)
}

// PushKeyboardFlags saves the current flags and adopts new ones.
func (b *Buffer) PushKeyboardFlags(flags int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.keyboard().push(flags)
}

// PopKeyboardFlags restores n saved entries.
func (b *Buffer) PopKeyboardFlags(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.keyboard().pop(n)
}

// applyKeyboardSet handles CSI = flags ; mode u.
func (b *Buffer) applyKeyboardSet(flags, mode int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.keyboard().set(flags, mode)
}

// resetKeyboardProtocol returns both screens to no enhancements, for a hard
// reset. A stale flag set outlasting the application that asked for it leaves
// the shell receiving key events it cannot read.
func (b *Buffer) resetKeyboardProtocol() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.mainKeyboard = keyboardState{}
	b.altKeyboard = keyboardState{}
}

// executeKeyboardProtocol handles the CSI ... u family. Returns false when the
// sequence is not a keyboard-protocol command, so the caller can fall back to
// RCP (restore cursor position), which shares the final byte.
func (p *Parser) executeKeyboardProtocol() bool {
	switch p.csiPrivate {
	case '?': // query
		if p.responseSink != nil {
			reply := "\x1b[?" + itoa(p.buffer.KeyboardFlags()) + "u"
			logGraphics("CSI ? u  <- %q", reply)
			p.responseSink([]byte(reply))
		}
		return true
	case '>': // push
		p.buffer.PushKeyboardFlags(p.getParam(0, 0))
		return true
	case '<': // pop
		p.buffer.PopKeyboardFlags(p.getParam(0, 1))
		return true
	case '=': // set
		p.buffer.applyKeyboardSet(p.getParam(0, 0), p.getParam(1, 1))
		return true
	}
	return false
}
