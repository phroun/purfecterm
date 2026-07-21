package purfecterm

// ScriptClass classifies a rune into a script class a renderer can map to a
// configured font (see Buffer.SetScriptFont / OSC 7005): "hebrew", "arabic",
// "cjk", or "" for Latin and everything else (which the primary font and the
// renderer's own fallback handle). The ranges cover the letters plus the
// Presentation Forms an Arabic/Hebrew shaper emits, and the full
// CJK/Hangul/kana/fullwidth blocks.
func ScriptClass(r rune) string {
	switch {
	case (r >= 0x0590 && r <= 0x05FF) || // Hebrew
		(r >= 0xFB1D && r <= 0xFB4F): // Hebrew presentation forms
		return "hebrew"
	case (r >= 0x0600 && r <= 0x06FF) || // Arabic
		(r >= 0x0750 && r <= 0x077F) || // Arabic Supplement
		(r >= 0x08A0 && r <= 0x08FF) || // Arabic Extended-A
		(r >= 0xFB50 && r <= 0xFDFF) || // Arabic Presentation Forms-A
		(r >= 0xFE70 && r <= 0xFEFF): // Arabic Presentation Forms-B
		return "arabic"
	case (r >= 0x1100 && r <= 0x11FF) || // Hangul Jamo
		(r >= 0x3040 && r <= 0x30FF) || // Hiragana + Katakana
		(r >= 0x3100 && r <= 0x312F) || // Bopomofo
		(r >= 0x3130 && r <= 0x318F) || // Hangul Compatibility Jamo
		(r >= 0x3400 && r <= 0x4DBF) || // CJK Ext A
		(r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified Ideographs
		(r >= 0xA960 && r <= 0xA97F) || // Hangul Jamo Extended-A
		(r >= 0xAC00 && r <= 0xD7FF) || // Hangul Syllables + Jamo Ext-B
		(r >= 0xF900 && r <= 0xFAFF) || // CJK Compatibility Ideographs
		(r >= 0xFF00 && r <= 0xFFEF) || // Halfwidth/Fullwidth Forms
		(r >= 0x20000 && r <= 0x2FA1F): // CJK Ext B..F + compat supplement
		return "cjk"
	}
	return ""
}
