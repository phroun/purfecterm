package purfecterm

import (
	"strings"
	"testing"
)

func newKbdTest() (*Buffer, *Parser, *[]string) {
	b := NewBuffer(80, 24, 100)
	p := NewParser(b)
	var replies []string
	p.SetResponseSink(func(d []byte) { replies = append(replies, string(d)) })
	return b, p, &replies
}

// The query is how an application detects support: a terminal that implements
// nothing never answers, so answering at all is the signal. This is the exact
// sequence behind "extended keyboard support is required".
func TestKeyboardQueryAnswers(t *testing.T) {
	_, p, replies := newKbdTest()
	p.Parse([]byte("\x1b[?u"))
	if len(*replies) != 1 || (*replies)[0] != "\x1b[?0u" {
		t.Fatalf("CSI ? u answered %q, want %q", *replies, "\x1b[?0u")
	}

	p.Parse([]byte("\x1b[>5u")) // push disambiguate + report-alternates
	p.Parse([]byte("\x1b[?u"))
	if got := (*replies)[len(*replies)-1]; got != "\x1b[?5u" {
		t.Errorf("after a push the query answered %q, want %q", got, "\x1b[?5u")
	}
}

// CSI ? u must NOT fall through to restore-cursor-position, which shares the
// final byte. Before the protocol existed, every support query moved the cursor.
func TestKeyboardQueryDoesNotRestoreCursor(t *testing.T) {
	b, p, _ := newKbdTest()
	b.SetCursor(0, 0)
	b.SaveCursor() // remembers (0,0)
	b.SetCursor(10, 5)

	p.Parse([]byte("\x1b[?u"))
	if x, y := b.GetCursor(); x != 10 || y != 5 {
		t.Errorf("the query moved the cursor to (%d,%d); it must not restore", x, y)
	}
	// A bare CSI u is still RCP.
	p.Parse([]byte("\x1bu"))
	p.Parse([]byte("\x1b[u"))
	if x, y := b.GetCursor(); x != 0 || y != 0 {
		t.Errorf("cursor = (%d,%d) after a bare CSI u, want the saved (0,0)", x, y)
	}
}

// Push and pop nest, and popping past the bottom lands on no enhancements.
func TestKeyboardPushPop(t *testing.T) {
	b, p, _ := newKbdTest()
	p.Parse([]byte("\x1b[>1u"))
	p.Parse([]byte("\x1b[>3u"))
	if got := b.KeyboardFlags(); got != 3 {
		t.Fatalf("flags = %d after two pushes, want 3", got)
	}
	p.Parse([]byte("\x1b[<u")) // pop one, default
	if got := b.KeyboardFlags(); got != 1 {
		t.Errorf("flags = %d after one pop, want 1", got)
	}
	p.Parse([]byte("\x1b[<5u")) // pop past the bottom
	if got := b.KeyboardFlags(); got != 0 {
		t.Errorf("flags = %d after popping past the bottom, want 0", got)
	}
}

// The set modes: 1 replaces, 2 sets bits, 3 clears them.
func TestKeyboardSetModes(t *testing.T) {
	b, p, _ := newKbdTest()
	p.Parse([]byte("\x1b[=5;1u"))
	if got := b.KeyboardFlags(); got != 5 {
		t.Errorf("mode 1 gave %d, want 5", got)
	}
	p.Parse([]byte("\x1b[=2;2u"))
	if got := b.KeyboardFlags(); got != 7 {
		t.Errorf("mode 2 gave %d, want 7 (bits added)", got)
	}
	p.Parse([]byte("\x1b[=4;3u"))
	if got := b.KeyboardFlags(); got != 3 {
		t.Errorf("mode 3 gave %d, want 3 (bits cleared)", got)
	}
	// Default mode is 1.
	p.Parse([]byte("\x1b[=16u"))
	if got := b.KeyboardFlags(); got != 16 {
		t.Errorf("default mode gave %d, want 16", got)
	}
	// Unknown bits are refused rather than stored, so a query cannot report
	// support for something this terminal does not implement.
	p.Parse([]byte("\x1b[=255;1u"))
	if got := b.KeyboardFlags(); got != KeyboardAllFlags {
		t.Errorf("flags = %d, want them masked to %d", got, KeyboardAllFlags)
	}
}

// The main and alternate screens keep separate stacks, so a full-screen
// application cannot leave the shell behind it in a mode it never asked for.
func TestKeyboardFlagsArePerScreen(t *testing.T) {
	b, p, _ := newKbdTest()
	p.Parse([]byte("\x1b[>1u")) // main screen wants disambiguation
	b.EnterAltScreen()
	if got := b.KeyboardFlags(); got != 0 {
		t.Errorf("the alt screen inherited flags %d, want 0", got)
	}
	p.Parse([]byte("\x1b[>15u"))
	b.LeaveAltScreen()
	if got := b.KeyboardFlags(); got != 1 {
		t.Errorf("back on the main screen flags = %d, want the original 1", got)
	}
}

// A hard reset clears both screens: a stale flag set outliving the application
// that asked for it leaves the shell unable to read its own keys.
func TestKeyboardResetClearsFlags(t *testing.T) {
	b, p, _ := newKbdTest()
	p.Parse([]byte("\x1b[>15u"))
	p.Parse([]byte("\x1bc")) // RIS
	if got := b.KeyboardFlags(); got != 0 {
		t.Errorf("flags = %d after a hard reset, want 0", got)
	}
}

// The push stack is bounded, so an application that pushes and never pops
// cannot grow it without limit.
func TestKeyboardStackIsBounded(t *testing.T) {
	b, p, _ := newKbdTest()
	for i := 0; i < maxKeyboardStackDepth*3; i++ {
		p.Parse([]byte("\x1b[>1u"))
	}
	b.mu.RLock()
	depth := len(b.mainKeyboard.stack)
	b.mu.RUnlock()
	if depth > maxKeyboardStackDepth {
		t.Errorf("stack depth %d, want at most %d", depth, maxKeyboardStackDepth)
	}
}

// Encoding: with no enhancements a key is its old self, so an application that
// never negotiated sees exactly what it always did.
func TestEncodeKeyLegacy(t *testing.T) {
	cases := []struct {
		name string
		ev   KeyEvent
		want string
	}{
		{"plain letter", KeyEvent{Code: 'a'}, "a"},
		{"shifted letter", KeyEvent{Code: 'a', Shifted: 'A', Mods: ModShift}, "A"},
		{"ctrl letter", KeyEvent{Code: 'c', Mods: ModCtrl}, "\x03"},
		{"enter", KeyEvent{Code: KeyEnter}, "\r"},
		{"tab", KeyEvent{Code: KeyTab}, "\t"},
		{"escape", KeyEvent{Code: KeyEscape}, "\x1b"},
	}
	for _, c := range cases {
		if got := string(EncodeKeyEvent(c.ev, 0)); got != c.want {
			t.Errorf("%s: encoded %q, want %q", c.name, got, c.want)
		}
	}
}

// Disambiguation is the whole point of flag 1: Esc, Tab, Enter and Backspace
// stop being indistinguishable from Ctrl-[, Ctrl-I, Ctrl-M and Ctrl-H.
func TestEncodeKeyDisambiguates(t *testing.T) {
	f := KeyboardDisambiguate
	if got := string(EncodeKeyEvent(KeyEvent{Code: KeyEscape}, f)); got != "\x1b[27u" {
		t.Errorf("Esc encoded %q, want \\x1b[27u", got)
	}
	if got := string(EncodeKeyEvent(KeyEvent{Code: KeyTab}, f)); got != "\x1b[9u" {
		t.Errorf("Tab encoded %q, want \\x1b[9u", got)
	}
	// Ctrl-I is now distinct from Tab.
	if got := string(EncodeKeyEvent(KeyEvent{Code: 'i', Mods: ModCtrl}, f)); got != "\x1b[105;5u" {
		t.Errorf("Ctrl-I encoded %q, want \\x1b[105;5u", got)
	}
	// An ordinary letter is still plain text under flag 1 alone.
	if got := string(EncodeKeyEvent(KeyEvent{Code: 'a'}, f)); got != "a" {
		t.Errorf("'a' encoded %q, want plain a under disambiguation alone", got)
	}
}

// Flag 8 sends every key as an escape code, text keys included.
func TestEncodeKeyReportAllKeys(t *testing.T) {
	f := KeyboardDisambiguate | KeyboardReportAllKeys
	if got := string(EncodeKeyEvent(KeyEvent{Code: 'a'}, f)); got != "\x1b[97u" {
		t.Errorf("'a' encoded %q, want \\x1b[97u", got)
	}
}

// Flag 2 adds event types, and a release is dropped entirely without it —
// otherwise every keystroke would arrive twice.
func TestEncodeKeyEventTypes(t *testing.T) {
	release := KeyEvent{Code: 'a', EventType: KeyRelease}
	if got := EncodeKeyEvent(release, KeyboardDisambiguate); got != nil {
		t.Errorf("a release encoded to %q without flag 2, want nothing", got)
	}
	f := KeyboardDisambiguate | KeyboardReportEvents | KeyboardReportAllKeys
	if got := string(EncodeKeyEvent(release, f)); got != "\x1b[97;1:3u" {
		t.Errorf("release encoded %q, want \\x1b[97;1:3u", got)
	}
	repeat := KeyEvent{Code: 'a', EventType: KeyRepeat}
	if got := string(EncodeKeyEvent(repeat, f)); got != "\x1b[97;1:2u" {
		t.Errorf("repeat encoded %q, want \\x1b[97;1:2u", got)
	}
	// Without flag 2 a repeat degrades to an ordinary press rather than vanishing.
	if got := string(EncodeKeyEvent(repeat, 0)); got != "a" {
		t.Errorf("repeat without flag 2 encoded %q, want a plain press", got)
	}
}

// Flag 4 reports the shifted and base-layout codes alongside the key.
func TestEncodeKeyAlternates(t *testing.T) {
	f := KeyboardDisambiguate | KeyboardReportAllKeys | KeyboardReportAlternates
	ev := KeyEvent{Code: 'a', Shifted: 'A', Mods: ModShift}
	if got := string(EncodeKeyEvent(ev, f)); got != "\x1b[97:65;2u" {
		t.Errorf("encoded %q, want \\x1b[97:65;2u", got)
	}
}

// Flag 16 appends the text the key would insert.
func TestEncodeKeyText(t *testing.T) {
	f := KeyboardDisambiguate | KeyboardReportAllKeys | KeyboardReportText
	ev := KeyEvent{Code: 'a', Shifted: 'A', Mods: ModShift, Text: "A"}
	if got := string(EncodeKeyEvent(ev, f)); got != "\x1b[97;2;65u" {
		t.Errorf("encoded %q, want \\x1b[97;2;65u", got)
	}
}

// Modifiers are the ORed bits plus one, which is what puts Ctrl at 5.
func TestEncodeKeyModifierNumbering(t *testing.T) {
	f := KeyboardDisambiguate | KeyboardReportAllKeys
	for _, c := range []struct {
		mods int
		want string
	}{
		{0, "\x1b[97u"},
		{ModShift, "\x1b[97;2u"},
		{ModAlt, "\x1b[97;3u"},
		{ModCtrl, "\x1b[97;5u"},
		{ModCtrl | ModShift, "\x1b[97;6u"},
		{ModCtrl | ModAlt, "\x1b[97;7u"},
		{ModSuper, "\x1b[97;9u"},
	} {
		got := string(EncodeKeyEvent(KeyEvent{Code: 'a', Mods: c.mods}, f))
		if got != c.want {
			t.Errorf("mods %d encoded %q, want %q", c.mods, got, c.want)
		}
	}
}

// A functional key keeps its own suffix rather than being forced into CSI u.
func TestEncodeFunctionalKeys(t *testing.T) {
	f := KeyboardDisambiguate
	up := KeyEvent{Code: KeyUp, Suffix: 'A', Mods: ModCtrl}
	if got := string(EncodeKeyEvent(up, f)); got != "\x1b[1;5A" {
		t.Errorf("Ctrl-Up encoded %q, want \\x1b[1;5A", got)
	}
	del := KeyEvent{Code: KeyDelete, Suffix: '~'}
	if got := string(EncodeKeyEvent(del, f)); !strings.HasSuffix(got, "~") {
		t.Errorf("Delete encoded %q, want a ~-terminated sequence", got)
	}
}

// ESC with an INTERMEDIATE byte continues into a final byte. Returning to
// ground on the intermediate left the final byte to be read as text and
// printed: ESC SP F (S7C1T), which applications send on startup, put a stray
// "F" on the screen.
func TestEscapeIntermediateConsumesItsFinalByte(t *testing.T) {
	for _, seq := range []string{
		"\x1b F", // S7C1T
		"\x1b G", // S8C1T
		"\x1b L", // ANSI conformance level 1
		"\x1b(B", // charset selection, an intermediate this parser already knew
	} {
		b := NewBuffer(20, 5, 100)
		NewParser(b).Parse([]byte(seq))
		for x := 0; x < 5; x++ {
			if c := b.GetVisibleCell(x, 0); c.Char != 0 && c.Char != ' ' {
				t.Errorf("%q printed %q at column %d; the sequence must be consumed",
					seq, c.Char, x)
			}
		}
	}
}

// Text after such a sequence still prints — consuming the final byte must not
// swallow what follows.
func TestEscapeIntermediateDoesNotSwallowText(t *testing.T) {
	b := NewBuffer(20, 5, 100)
	NewParser(b).Parse([]byte("\x1b Fhi"))
	if c := b.GetVisibleCell(0, 0); c.Char != 'h' {
		t.Errorf("column 0 = %q, want 'h'", c.Char)
	}
	if c := b.GetVisibleCell(1, 0); c.Char != 'i' {
		t.Errorf("column 1 = %q, want 'i'", c.Char)
	}
}
