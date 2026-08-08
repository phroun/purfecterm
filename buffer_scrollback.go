package purfecterm

import (
	"fmt"
	"sort"
	"strings"
)

// --- Scrollback Management Methods ---

// ClearScrollback clears the scrollback buffer
func (b *Buffer) ClearScrollback() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.scrollback = nil
	b.scrollbackInfo = nil
	b.scrollOffset = 0
	b.markDirty()
}

// Reset resets the terminal to initial state
// Moves current screen content to scrollback, then resets all modes and cursor
func (b *Buffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Push current screen content to scrollback first
	for i := 0; i < len(b.screen); i++ {
		if len(b.screen[i]) > 0 {
			b.pushLineToScrollback(b.screen[i], b.lineInfos[i])
		}
	}

	// Reset screen
	b.initScreen()

	// Reset cursor
	b.cursorX = 0
	b.cursorY = 0
	b.cursorVisible = true
	b.cursorShape = 0
	b.cursorBlink = 0
	b.savedCursorX = 0
	b.savedCursorY = 0

	// Reset attributes
	b.currentFg = DefaultForeground
	b.currentBg = DefaultBackground
	b.currentBold = false
	b.currentItalic = false
	b.currentUnderline = false
	b.currentReverse = false
	b.currentBlink = false
	b.currentStrikethrough = false
	b.currentFlexWidth = false

	// Reset modes
	b.bracketedPasteMode = false
	b.mouseTrackingMode = 0
	b.mouseEncodingMode = 0
	b.flexWidthMode = false
	b.visualWidthWrap = false
	b.ambiguousWidthMode = AmbiguousWidthAuto
	b.autoWrapMode = true
	b.smartWordWrap = true // Smart word wrap default enabled
	b.autoScrollDisabled = false
	b.scrollbackDisabled = false
	b.columnMode132 = false
	b.columnMode40 = false
	b.lineDensity = 25

	// Reset theme to user preference
	themeChanged := b.darkTheme != b.preferredDarkTheme
	b.darkTheme = b.preferredDarkTheme

	// Reset custom graphics state
	b.currentBGP = -1
	b.currentXFlip = false
	b.currentYFlip = false

	// Reset scroll offset
	b.scrollOffset = 0
	b.horizOffset = 0

	b.markDirty()
	b.notifyScaleChange()
	if themeChanged {
		b.notifyThemeChange()
	}
}

// ScrollbackSaveOptions tunes how SaveScrollbackText/SaveScrollbackANS serialize
// a buffer. The zero value reproduces the historical behavior, so the plain
// SaveScrollback* entry points stay unchanged; callers wanting a variant pass
// this to the *Opts forms. It is the place to add further capture knobs.
type ScrollbackSaveOptions struct {
	// TrimTrailingBlankLines stops the CONTENT lines at the last used line
	// instead of dumping the empty tail of the fixed-height screen grid. "Used"
	// is the buffer's own notion: a written line has cells, an untouched or
	// cleared one is zero-length (see makeEmptyLine / ClearScreen). The ANS
	// form still writes its full reload payload — palette/glyph headers and the
	// end-state footer (splits, sprites, crop, cursor) — so a trimmed stream
	// remains a faithful, reloadable session, just without the blank tail.
	TrimTrailingBlankLines bool
}

// usedLineCount returns how many leading lines of the combined scrollback+screen
// sequence to serialize so the empty tail of the screen grid is omitted: one
// past the last line that has any cells, or 0 when there is no content at all.
func usedLineCount(scrollback, screen [][]Cell) int {
	for i := len(screen) - 1; i >= 0; i-- {
		if len(screen[i]) > 0 {
			return len(scrollback) + i + 1
		}
	}
	for i := len(scrollback) - 1; i >= 0; i-- {
		if len(scrollback[i]) > 0 {
			return i + 1
		}
	}
	return 0
}

// SerializeLineANS renders one line's cells to a self-contained ANSI string —
// the form the `lines` capture rung emits, one line of the ordered transcript.
// It applies the DEC line attribute, resets styling at the start of each run and
// at the end so the line stands alone, and writes standard SGR (colour, bold,
// italic, underline, reverse, blink, strikethrough) as it changes across the
// line. purfecterm's private glyph/flip/flex extensions are not emitted — the
// transcript targets line-oriented output where they do not appear — and a
// consumer wanting plain text simply strips the SGR. No trailing newline; the
// caller separates lines.
func SerializeLineANS(line []Cell, info LineInfo) string {
	var b strings.Builder
	switch info.Attribute {
	case LineAttrDoubleWidth:
		b.WriteString("\x1b#6")
	case LineAttrDoubleTop:
		b.WriteString("\x1b#3")
	case LineAttrDoubleBottom:
		b.WriteString("\x1b#4")
	}

	// Track state against the real defaults so a default cell emits nothing and a
	// wholly plain line serializes to just its text.
	fg, bg := DefaultForeground, DefaultBackground
	var bold, italic, underline, reverse, blink, strike bool
	for _, cell := range line {
		// SGR has no per-attribute "off" we track, so when any tracked attribute
		// changes reset all and re-assert — the same scheme SaveScrollbackANS uses.
		if bold != cell.Bold || italic != cell.Italic || underline != cell.Underline ||
			reverse != cell.Reverse || blink != cell.Blink || strike != cell.Strikethrough ||
			fg != cell.Foreground || bg != cell.Background {
			b.WriteString("\x1b[0m")
			fg, bg = DefaultForeground, DefaultBackground
			bold, italic, underline, reverse, blink, strike = false, false, false, false, false, false
		}
		if cell.Bold && !bold {
			b.WriteString("\x1b[1m")
			bold = true
		}
		if cell.Italic && !italic {
			b.WriteString("\x1b[3m")
			italic = true
		}
		if cell.Underline && !underline {
			b.WriteString("\x1b[4m")
			underline = true
		}
		if cell.Reverse && !reverse {
			b.WriteString("\x1b[7m")
			reverse = true
		}
		if cell.Blink && !blink {
			b.WriteString("\x1b[5m")
			blink = true
		}
		if cell.Strikethrough && !strike {
			b.WriteString("\x1b[9m")
			strike = true
		}
		if cell.Foreground != fg {
			b.WriteString("\x1b[" + cell.Foreground.ToSGRCode(true) + "m")
			fg = cell.Foreground
		}
		if cell.Background != bg {
			b.WriteString("\x1b[" + cell.Background.ToSGRCode(false) + "m")
			bg = cell.Background
		}
		if cell.Char != 0 {
			b.WriteRune(cell.Char)
			if len(cell.Combining) > 0 {
				b.WriteString(cell.Combining)
			}
		}
	}
	// Close the line only if it ended in a non-default state, so a plain line
	// carries no trailing reset.
	if bold || italic || underline || reverse || blink || strike ||
		fg != DefaultForeground || bg != DefaultBackground {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}

// SaveScrollbackText returns the scrollback and screen content as plain text.
func (b *Buffer) SaveScrollbackText() string {
	return b.SaveScrollbackTextOpts(ScrollbackSaveOptions{})
}

// SaveScrollbackTextOpts is SaveScrollbackText with serialization options.
func (b *Buffer) SaveScrollbackTextOpts(o ScrollbackSaveOptions) string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	limit := len(b.scrollback) + len(b.screen)
	if o.TrimTrailingBlankLines {
		limit = usedLineCount(b.scrollback, b.screen)
	}

	var result strings.Builder
	emitted := 0
	writeLine := func(line []Cell) bool {
		if emitted >= limit {
			return false
		}
		for _, cell := range line {
			if cell.Char != 0 {
				result.WriteRune(cell.Char)
			}
		}
		result.WriteString("\n")
		emitted++
		return true
	}

	// Output scrollback lines, then screen lines
	for _, line := range b.scrollback {
		if !writeLine(line) {
			break
		}
	}
	for _, line := range b.screen {
		if !writeLine(line) {
			break
		}
	}

	return result.String()
}

// SaveScrollbackANS returns the scrollback and screen with full ANSI/PawScript codes preserved.
// The output format:
// 1. TOP: Custom palette definitions (OSC 7000), custom glyph definitions (OSC 7001)
// 2. BODY: Content lines with DEC line attributes, SGR codes, BGP/flip attributes
// 3. END: Sprite units, screen splits, screen crop, crop rectangles, sprites, cursor position
// Callers may prepend a header comment using OSC 9999 before this output.
func (b *Buffer) SaveScrollbackANS() string {
	return b.SaveScrollbackANSOpts(ScrollbackSaveOptions{})
}

// SaveScrollbackANSOpts is SaveScrollbackANS with serialization options.
func (b *Buffer) SaveScrollbackANSOpts(o ScrollbackSaveOptions) string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var result strings.Builder

	// ========== SECTION 1: Definitions at TOP ==========

	// Output palette definitions (OSC 7000)
	// Get sorted palette keys for deterministic output
	paletteKeys := make([]int, 0, len(b.palettes))
	for k := range b.palettes {
		paletteKeys = append(paletteKeys, k)
	}
	sort.Ints(paletteKeys)

	for _, n := range paletteKeys {
		palette := b.palettes[n]
		if palette == nil || len(palette.Entries) == 0 {
			continue
		}
		// Init palette: ESC ] 7000 ; i ; N ; LEN BEL
		result.WriteString(fmt.Sprintf("\x1b]7000;i;%d;%d\x07", n, len(palette.Entries)))
		// Set each entry
		for idx, entry := range palette.Entries {
			switch entry.Type {
			case PaletteEntryColor:
				// True color: ESC ] 7000 ; s ; N ; IDX ; r ; R ; G ; B BEL
				// or with dim: ESC ] 7000 ; s ; N ; IDX ; r ; 2 ; R ; G ; B BEL
				if entry.Dim {
					result.WriteString(fmt.Sprintf("\x1b]7000;s;%d;%d;r;2;%d;%d;%d\x07",
						n, idx, entry.Color.R, entry.Color.G, entry.Color.B))
				} else {
					result.WriteString(fmt.Sprintf("\x1b]7000;s;%d;%d;r;%d;%d;%d\x07",
						n, idx, entry.Color.R, entry.Color.G, entry.Color.B))
				}
			case PaletteEntryTransparent:
				// Transparent (use background): SGR code 8
				result.WriteString(fmt.Sprintf("\x1b]7000;s;%d;%d;8\x07", n, idx))
			case PaletteEntryDefaultFG:
				// Use foreground: SGR code 9
				result.WriteString(fmt.Sprintf("\x1b]7000;s;%d;%d;9\x07", n, idx))
			}
		}
	}

	// Output glyph definitions (OSC 7001)
	// Get sorted glyph runes for deterministic output
	glyphRunes := make([]rune, 0, len(b.customGlyphs))
	for r := range b.customGlyphs {
		glyphRunes = append(glyphRunes, r)
	}
	sort.Slice(glyphRunes, func(i, j int) bool { return glyphRunes[i] < glyphRunes[j] })

	for _, r := range glyphRunes {
		glyph := b.customGlyphs[r]
		if glyph == nil || glyph.Width == 0 || len(glyph.Pixels) == 0 {
			continue
		}
		// Set glyph: ESC ] 7001 ; s ; RUNE ; WIDTH ; P1 ; P2 ; ... BEL
		result.WriteString(fmt.Sprintf("\x1b]7001;s;%d;%d", int(r), glyph.Width))
		for _, px := range glyph.Pixels {
			result.WriteString(fmt.Sprintf(";%d", px))
		}
		result.WriteString("\x07")
	}

	// ========== SECTION 1b: Terminal Mode Settings ==========

	// Flex width mode (?7027) is toggled per-character as needed based on cell.FlexWidth
	// Start with it off (default state)

	// Output ambiguous width mode setting
	switch b.ambiguousWidthMode {
	case AmbiguousWidthNarrow:
		result.WriteString("\x1b[?7029h") // Enable narrow mode
	case AmbiguousWidthWide:
		result.WriteString("\x1b[?7030h") // Enable wide mode
	}

	// Output visual width wrap mode if enabled (PurfecTerm private mode 7028)
	if b.visualWidthWrap {
		result.WriteString("\x1b[?7028h")
	}

	// ========== SECTION 2: Content Lines ==========

	// Track current attributes to minimize escape sequences
	var lastFg, lastBg Color
	var lastBold, lastItalic, lastUnderline, lastReverse, lastBlink, lastStrikethrough bool
	var lastFlexWidth bool // Track flex width mode state
	var lastAmbiguousWide bool                                // Track if ambiguous width is set to wide
	var lastBGP int = -1
	var lastXFlip, lastYFlip bool
	var lastLineAttr LineAttribute = LineAttrNormal

	// Helper to write ANSI color code using proper SGR codes
	writeColor := func(c Color, isFg bool) {
		result.WriteString("\x1b[" + c.ToSGRCode(isFg) + "m")
	}

	// Count total lines for cursor positioning later
	totalLines := len(b.scrollback) + len(b.screen)
	currentLineNum := 0

	// limit is how many content lines to emit: the whole grid, or (when trimming)
	// one past the last used line so the blank tail is omitted. currentLineNum,
	// which outputLine advances, is the running count checked against it.
	limit := totalLines
	if o.TrimTrailingBlankLines {
		limit = usedLineCount(b.scrollback, b.screen)
	}

	outputLine := func(line []Cell, lineInfo LineInfo) {
		hasNonDefaultBg := false
		currentLineNum++

		// Output DEC line attribute if changed (at start of line)
		if lineInfo.Attribute != lastLineAttr {
			switch lineInfo.Attribute {
			case LineAttrDoubleWidth:
				result.WriteString("\x1b#6") // DECDWL
			case LineAttrDoubleTop:
				result.WriteString("\x1b#3") // DECDHL top
			case LineAttrDoubleBottom:
				result.WriteString("\x1b#4") // DECDHL bottom
			case LineAttrNormal:
				result.WriteString("\x1b#5") // DECSWL (single width)
			}
			lastLineAttr = lineInfo.Attribute
		}

		for _, cell := range line {
			// Check if standard attributes changed (need reset)
			needsReset := false
			if cell.Bold != lastBold || cell.Italic != lastItalic ||
				cell.Underline != lastUnderline || cell.Reverse != lastReverse ||
				cell.Blink != lastBlink || cell.Strikethrough != lastStrikethrough {
				needsReset = true
			}

			if needsReset {
				result.WriteString("\x1b[0m") // Reset all
				lastFg = Color{}
				lastBg = Color{}
				lastBold = false
				lastItalic = false
				lastUnderline = false
				lastReverse = false
				lastBlink = false
				lastStrikethrough = false
				// Reset doesn't affect BGP/flip, but we track them separately
			}

			// Set standard attributes
			if cell.Bold && !lastBold {
				result.WriteString("\x1b[1m")
				lastBold = true
			}
			if cell.Italic && !lastItalic {
				result.WriteString("\x1b[3m")
				lastItalic = true
			}
			if cell.Underline && !lastUnderline {
				result.WriteString("\x1b[4m")
				lastUnderline = true
			}
			if cell.Reverse && !lastReverse {
				result.WriteString("\x1b[7m")
				lastReverse = true
			}
			if cell.Blink && !lastBlink {
				result.WriteString("\x1b[5m")
				lastBlink = true
			}
			if cell.Strikethrough && !lastStrikethrough {
				result.WriteString("\x1b[9m")
				lastStrikethrough = true
			}

			// Set colors
			if cell.Foreground != lastFg {
				writeColor(cell.Foreground, true)
				lastFg = cell.Foreground
			}
			if cell.Background != lastBg {
				writeColor(cell.Background, false)
				lastBg = cell.Background
				if !cell.Background.IsDefault() {
					hasNonDefaultBg = true
				}
			}

			// Set BGP if changed
			if cell.BGP != lastBGP {
				if cell.BGP < 0 {
					result.WriteString("\x1b[159m") // Reset BGP
				} else {
					result.WriteString(fmt.Sprintf("\x1b[158;%dm", cell.BGP))
				}
				lastBGP = cell.BGP
			}

			// Set XFlip if changed
			if cell.XFlip != lastXFlip {
				if cell.XFlip {
					result.WriteString("\x1b[151m") // XFlip on
				} else {
					result.WriteString("\x1b[150m") // XFlip off
				}
				lastXFlip = cell.XFlip
			}

			// Set YFlip if changed
			if cell.YFlip != lastYFlip {
				if cell.YFlip {
					result.WriteString("\x1b[153m") // YFlip on
				} else {
					result.WriteString("\x1b[152m") // YFlip off
				}
				lastYFlip = cell.YFlip
			}

			// Toggle flex width mode if needed for this character
			if cell.FlexWidth != lastFlexWidth {
				if cell.FlexWidth {
					result.WriteString("\x1b[?7027h") // Enable flex width
				} else {
					result.WriteString("\x1b[?7027l") // Disable flex width
				}
				lastFlexWidth = cell.FlexWidth
			}

			// For ambiguous width characters, toggle wide/narrow mode as needed
			// This ensures characters like Greek, Cyrillic, symbols render at correct width
			if cell.FlexWidth && cell.Char != 0 && IsAmbiguousWidth(cell.Char) {
				needsWide := cell.CellWidth >= 2.0
				if needsWide != lastAmbiguousWide {
					if needsWide {
						result.WriteString("\x1b[?7030h") // Ambiguous width: wide
					} else {
						result.WriteString("\x1b[?7029h") // Ambiguous width: narrow
					}
					lastAmbiguousWide = needsWide
				}
			}

			// Output character and combining marks
			if cell.Char != 0 {
				result.WriteRune(cell.Char)
				if len(cell.Combining) > 0 {
					result.WriteString(cell.Combining)
				}
			}
		}

		// If line had background color, clear to end of line
		if hasNonDefaultBg {
			result.WriteString("\x1b[K") // Clear to end of line (preserves bg)
		}
		result.WriteString("\x1b[0m\n") // Reset and newline
		lastFg = Color{}
		lastBg = Color{}
		lastBold = false
		lastItalic = false
		lastUnderline = false
		lastReverse = false
		lastBlink = false
		lastStrikethrough = false

		// If background was dirty, clear the next line to prevent bleeding
		if hasNonDefaultBg {
			result.WriteString("\x1b[2K") // Clear entire line
		}
	}

	// Output scrollback lines, then screen lines, stopping at the content limit.
	for i, line := range b.scrollback {
		if currentLineNum >= limit {
			break
		}
		var lineInfo LineInfo
		if i < len(b.scrollbackInfo) {
			lineInfo = b.scrollbackInfo[i]
		}
		outputLine(line, lineInfo)
	}

	for i, line := range b.screen {
		if currentLineNum >= limit {
			break
		}
		var lineInfo LineInfo
		if i < len(b.lineInfos) {
			lineInfo = b.lineInfos[i]
		}
		outputLine(line, lineInfo)
	}

	// ========== SECTION 3: State at END ==========

	// Output sprite units if not default (8x8)
	if b.spriteUnitX != 8 || b.spriteUnitY != 8 {
		result.WriteString(fmt.Sprintf("\x1b]7002;u;%d;%d\x07", b.spriteUnitX, b.spriteUnitY))
	}

	// Output screen splits (OSC 7003 ss)
	splitIDs := make([]int, 0, len(b.screenSplits))
	for id := range b.screenSplits {
		splitIDs = append(splitIDs, id)
	}
	sort.Ints(splitIDs)

	for _, id := range splitIDs {
		split := b.screenSplits[id]
		if split == nil {
			continue
		}
		// Format: ss;ID;SCREENY;BUFROW;BUFCOL;TOPFINE;LEFTFINE;CWS;LD
		// BUFROW/BUFCOL are 1-indexed in the escape sequence (0 means inherit)
		bufRow := split.BufferRow
		bufCol := split.BufferCol
		if bufRow > 0 {
			bufRow++ // Convert 0-indexed to 1-indexed
		}
		if bufCol > 0 {
			bufCol++
		}
		result.WriteString(fmt.Sprintf("\x1b]7003;ss;%d;%d;%d;%d;%d;%d;%g;%d\x07",
			id, split.ScreenY, bufRow, bufCol,
			split.TopFineScroll, split.LeftFineScroll,
			split.CharWidthScale, split.LineDensity))
	}

	// Output screen crop if set (OSC 7003 c)
	if b.widthCrop >= 0 || b.heightCrop >= 0 {
		if b.widthCrop >= 0 && b.heightCrop >= 0 {
			result.WriteString(fmt.Sprintf("\x1b]7003;c;%d;%d\x07", b.widthCrop, b.heightCrop))
		} else if b.widthCrop >= 0 {
			result.WriteString(fmt.Sprintf("\x1b]7003;c;%d\x07", b.widthCrop))
		} else {
			result.WriteString(fmt.Sprintf("\x1b]7003;c;;%d\x07", b.heightCrop))
		}
	}

	// Output crop rectangles (OSC 7002 cs)
	cropIDs := make([]int, 0, len(b.cropRects))
	for id := range b.cropRects {
		cropIDs = append(cropIDs, id)
	}
	sort.Ints(cropIDs)

	for _, id := range cropIDs {
		crop := b.cropRects[id]
		if crop == nil {
			continue
		}
		// Format: cs;ID;MINX;MINY;MAXX;MAXY
		result.WriteString(fmt.Sprintf("\x1b]7002;cs;%d;%g;%g;%g;%g\x07",
			id, crop.MinX, crop.MinY, crop.MaxX, crop.MaxY))
	}

	// Output sprites (OSC 7002 s)
	spriteIDs := make([]int, 0, len(b.sprites))
	for id := range b.sprites {
		spriteIDs = append(spriteIDs, id)
	}
	sort.Ints(spriteIDs)

	for _, id := range spriteIDs {
		sprite := b.sprites[id]
		if sprite == nil {
			continue
		}
		// Format: s;ID;X;Y;Z;FGP;FLIP;XS;YS;CROP;R1;R2;...
		// Collect all runes from the 2D array
		var runes []rune
		for rowIdx, row := range sprite.Runes {
			if rowIdx > 0 {
				runes = append(runes, 10) // Newline separator
			}
			runes = append(runes, row...)
		}
		result.WriteString(fmt.Sprintf("\x1b]7002;s;%d;%g;%g;%d;%d;%d;%g;%g;%d",
			id, sprite.X, sprite.Y, sprite.ZIndex, sprite.FGP, sprite.FlipCode,
			sprite.XScale, sprite.YScale, sprite.CropRect))
		for _, r := range runes {
			result.WriteString(fmt.Sprintf(";%d", int(r)))
		}
		result.WriteString("\x07")
	}

	// Output cursor position restoration (only if cursor is not at end of content)
	// The cursor is considered "at end" if it's on the last line at or past the content
	// In that case, we don't need CSI A or G codes
	if totalLines > 0 {
		// Calculate how far back the cursor needs to go. When the blank tail was
		// trimmed, the last line OUTPUT is `limit`, not the full grid bottom, so
		// the move-up is measured from there: an ESC[10A over 5 trimmed blank
		// lines becomes ESC[5A. Clamp at zero for a cursor that sat in the tail.
		linesFromEnd := totalLines - (len(b.scrollback) + b.cursorY + 1)
		if o.TrimTrailingBlankLines {
			linesFromEnd = limit - (len(b.scrollback) + b.cursorY + 1)
			if linesFromEnd < 0 {
				linesFromEnd = 0
			}
		}

		// Find the last non-empty character position on the last line
		lastLineLen := 0
		if len(b.screen) > 0 {
			lastLine := b.screen[len(b.screen)-1]
			for i := len(lastLine) - 1; i >= 0; i-- {
				if lastLine[i].Char != 0 && lastLine[i].Char != ' ' {
					lastLineLen = i + 1
					break
				}
			}
		}

		// Only output cursor positioning if cursor is NOT at the natural end position
		// Natural end = last line, at or after last content character
		cursorAtEnd := (linesFromEnd == 0) && (b.cursorX >= lastLineLen)

		if !cursorAtEnd {
			// Need to reposition cursor
			if linesFromEnd > 0 {
				// Move up N lines
				result.WriteString(fmt.Sprintf("\x1b[%dA", linesFromEnd))
			}
			if b.cursorX > 0 {
				// Move to column (1-indexed)
				result.WriteString(fmt.Sprintf("\x1b[%dG", b.cursorX+1))
			}
		}
	}

	return result.String()
}
