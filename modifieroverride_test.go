package purfecterm

import "testing"

// The zero override changes nothing.
//
// This is the property the shape was chosen for. Expressed the other way — a
// "keep these" mask ANDed in — the zero value erases every modifier, so a
// caller who returns an empty struct, or fills one field and forgets the other,
// silently strips Ctrl and Shift from every keystroke. That failure surfaces at
// the far end as the terminal having broken, not as a mistake in the hook.
func TestZeroOverrideChangesNothing(t *testing.T) {
	for _, mods := range []int{
		0,
		ModCtrl,
		ModShift | ModMega,
		ModAll,
	} {
		if got := ApplyModifierOverride(mods, ModifierOverride{}); got != mods {
			t.Errorf("zero override turned %d into %d", mods, got)
		}
	}
}

// Set adds, Clear removes, and Clear runs first so a bit in both ends up set.
//
// The order has to be fixed and stated, because the two are not commutative and
// a caller writing {Set: ModHyper, Clear: ModHyper} means something by it. Set
// winning is the useful reading: "drop everything of this kind, except this."
func TestSetAndClear(t *testing.T) {
	for _, c := range []struct {
		mods int
		o    ModifierOverride
		want int
		what string
	}{
		{ModCtrl, ModifierOverride{Set: ModHyper}, ModCtrl | ModHyper,
			"the case this exists for: a backend that cannot see Hyper is told"},
		{ModCtrl | ModMega, ModifierOverride{Clear: ModMega}, ModCtrl,
			"clearing a modifier the backend did derive"},
		{ModCtrl, ModifierOverride{Set: ModHyper, Clear: ModCtrl}, ModHyper,
			"both at once, on different bits"},
		{ModCtrl, ModifierOverride{Set: ModCtrl, Clear: ModCtrl}, ModCtrl,
			"the same bit in both: Set wins, because Clear is applied first"},
		{0, ModifierOverride{Clear: ModAll}, 0,
			"clearing what was never there"},
		{ModAll, ModifierOverride{Clear: ModAll}, 0,
			"clearing everything is allowed — it just has to be asked for"},
	} {
		if got := ApplyModifierOverride(c.mods, c.o); got != c.want {
			t.Errorf("%s: ApplyModifierOverride(%d, %+v) = %d, want %d",
				c.what, c.mods, c.o, got, c.want)
		}
	}
}

// A bit that is not a modifier cannot reach the wire.
//
// The encoded parameter is the bits plus one, so an out-of-range value is not a
// wrong chord — it is a CSI sequence with a nonsense number in it, which a
// guest may parse as anything at all. The hook is written by someone else, so
// the mask is enforced here rather than trusted.
func TestOverrideCannotIntroduceStrayBits(t *testing.T) {
	stray := 1 << 20

	if got := ApplyModifierOverride(ModCtrl, ModifierOverride{Set: stray}); got != ModCtrl {
		t.Errorf("a stray Set bit survived: %d", got)
	}
	if got := ApplyModifierOverride(ModCtrl, ModifierOverride{Set: stray | ModHyper}); got != ModCtrl|ModHyper {
		t.Errorf("a stray bit alongside a real one: %d, want %d", got, ModCtrl|ModHyper)
	}
	// A stray Clear bit is harmless in itself, but is masked too so the contract
	// is the same in both directions.
	if got := ApplyModifierOverride(ModCtrl, ModifierOverride{Clear: stray}); got != ModCtrl {
		t.Errorf("a stray Clear bit changed the result: %d", got)
	}
	// And an input already carrying a stray bit is cleaned on the way through.
	if got := ApplyModifierOverride(ModCtrl|stray, ModifierOverride{}); got != ModCtrl {
		t.Errorf("a stray bit in the input survived: %d", got)
	}
}

// The override reaches the encoded sequence.
//
// ApplyModifierOverride is only useful if what it produces is what gets
// written, so this goes through EncodeKeyEvent rather than stopping at the
// arithmetic. Hyper is bit 16, so the wire parameter is 16 + 1.
func TestOverriddenModifiersReachTheWire(t *testing.T) {
	flags := KeyboardDisambiguate | KeyboardReportAllKeys

	plain := EncodeKeyEvent(KeyEvent{
		Code: 'a', Mods: 0, EventType: KeyPress, Suffix: 'u',
	}, flags)

	hyper := EncodeKeyEvent(KeyEvent{
		Code:      'a',
		Mods:      ApplyModifierOverride(0, ModifierOverride{Set: ModHyper}),
		EventType: KeyPress,
		Suffix:    'u',
	}, flags)

	if string(plain) == string(hyper) {
		t.Fatalf("Hyper made no difference to the encoding: both %q", string(plain))
	}
	if want := "\x1b[97;17u"; string(hyper) != want {
		t.Errorf("Hyper encoded as %q, want %q (bit 16, sent as 16+1)",
			string(hyper), want)
	}
}
