package purfecterm

import (
	"sync"
	"time"
)

// scrollMagneticThresholdPercent is the percentage of total scrollable content
// that forms the magnetic zone at the boundary between logical screen and scrollback.
const scrollMagneticThresholdPercent = 5

// keyboardAutoScrollDuration is how long after keyboard activity the terminal
// will auto-scroll to keep the cursor visible on cursor movements.
const keyboardAutoScrollDuration = 500 * time.Millisecond

// manualScrollCooldown is how long after manual scrolling before auto-scroll
// can resume (if no keyboard activity or scroll-causing event occurs).
const manualScrollCooldown = 5 * time.Second

// HorizMemo stores horizontal scroll memo data for a single scanline.
// This is populated during paint to track cursor position relative to rendered content.
type HorizMemo struct {
	Valid           bool // True if this scanline was processed during paint
	LogicalRow      int  // Which buffer row is being rendered at this scanline (for debugging)
	LeftmostCell    int  // Leftmost rendered cell column number
	RightmostCell   int  // Rightmost rendered cell column number
	DistanceToLeft  int  // Distance to scroll left to reach cursor (-1 if N/A)
	DistanceToRight int  // Distance to scroll right to reach cursor (-1 if N/A)
	CursorLocated   bool // True if cursor was found within rendered area
}

// scrollMagneticThresholdMin is the minimum magnetic threshold in lines.
const scrollMagneticThresholdMin = 2

// scrollMagneticThresholdMax is the maximum magnetic threshold in lines.
const scrollMagneticThresholdMax = 50

// getMagneticThreshold calculates the dynamic magnetic threshold based on
// total scrollable content (scrollback size + logical rows hidden above).
// Returns 5% of total scrollable content, clamped between min and max values.
func (b *Buffer) getMagneticThreshold() int {
	scrollbackSize := len(b.scrollback)
	effectiveRows := b.EffectiveRows()

	// Calculate how much of the logical screen is hidden above
	logicalHiddenAbove := 0
	if effectiveRows > b.rows {
		logicalHiddenAbove = effectiveRows - b.rows
	}

	// Total scrollable area above visible
	totalScrollableAbove := scrollbackSize + logicalHiddenAbove

	// Calculate 5% of total scrollable content
	threshold := totalScrollableAbove * scrollMagneticThresholdPercent / 100

	// Clamp to reasonable bounds
	if threshold < scrollMagneticThresholdMin {
		threshold = scrollMagneticThresholdMin
	}
	if threshold > scrollMagneticThresholdMax {
		threshold = scrollMagneticThresholdMax
	}

	return threshold
}

// getEffectiveScrollOffset returns the scroll offset adjusted for the magnetic zone.
// In the magnetic zone, rendering should behave as if viewing the full logical screen.
// Past the magnetic zone, the threshold is subtracted so content transitions smoothly.
// This must be called with the lock held.
func (b *Buffer) getEffectiveScrollOffset() int {
	effectiveRows := b.EffectiveRows()

	// Calculate how much of the logical screen is hidden above
	logicalHiddenAbove := 0
	if effectiveRows > b.rows {
		logicalHiddenAbove = effectiveRows - b.rows
	}

	// Calculate boundary row (where yellow line would appear without magnetic zone)
	boundaryRow := b.scrollOffset - logicalHiddenAbove

	// If not scrolled into scrollback at all, no adjustment needed
	if boundaryRow <= 0 {
		return b.scrollOffset
	}

	magneticThreshold := b.getMagneticThreshold()

	if boundaryRow <= magneticThreshold {
		// In magnetic zone - render as if viewing full logical screen
		return logicalHiddenAbove
	}

	// Past magnetic zone - subtract threshold so content transitions smoothly
	return b.scrollOffset - magneticThreshold
}

// Buffer manages the terminal screen and scrollback buffer
type Buffer struct {
	mu sync.RWMutex

	// Physical dimensions (visible area from widget)
	cols int
	rows int

	// Logical dimensions (terminal's idea of its size, may differ from physical)
	// 0 means "use physical dimension"
	logicalCols int
	logicalRows int

	cursorX       int
	cursorY       int
	cursorVisible bool
	cursorShape   int // 0=block, 1=underline, 2=bar
	cursorBlink   int // 0=no blink, 1=slow blink, 2=fast blink

	bracketedPasteMode bool

	currentFg        Color
	currentBg            Color
	currentBold          bool
	currentItalic        bool
	currentUnderline     bool
	currentUnderlineStyle UnderlineStyle
	currentUnderlineColor Color
	currentHasUnderlineColor bool
	currentReverse       bool
	currentBlink         bool
	currentStrikethrough bool
	currentFlexWidth     bool // Current attribute for East Asian Width mode

	// Flexible cell width mode (East Asian Width)
	flexWidthMode      bool               // When true, new chars get FlexWidth=true and calculated CellWidth
	visualWidthWrap    bool               // When true, wrap based on accumulated visual width, not cell count
	ambiguousWidthMode AmbiguousWidthMode // How to handle ambiguous width chars: Auto/Narrow/Wide

	// Screen storage - lines can have variable width
	screen    [][]Cell
	lineInfos []LineInfo

	// Buffer-wide default for logical lines with no stored data
	screenInfo ScreenInfo

	// Scrollback storage
	scrollback         [][]Cell
	scrollbackInfo     []LineInfo
	maxScrollback      int
	scrollOffset       int  // Vertical scroll offset
	scrollbackDisabled bool // When true, scrollback accumulation is disabled (for games)

	// Horizontal scrolling
	horizOffset int // Horizontal scroll offset (in columns)

	// Auto-scroll to cursor on keyboard activity
	lastKeyboardActivity time.Time // When keyboard activity last occurred
	cursorDrawnLastFrame bool      // Set by widget after drawing cursor
	lastCursorMoveDir    int       // -1=up, 0=none, 1=down (for vertical auto-scroll)
	lastManualVertScroll time.Time // When user last manually scrolled vertically

	// Horizontal auto-scroll tracking
	lastHorizCursorMoveDir  int       // -1=left, 0=unknown, 1=right (for horiz auto-scroll)
	lastManualHorizScroll   time.Time // When user last manually scrolled horizontally
	lastScrollCausingEvent  time.Time // When a scroll-causing event last occurred (line to scrollback)
	horizMemos              []HorizMemo // Per-scanline horizontal scroll memos (populated during paint)
	isAbsoluteHorizPosition bool      // True if last horiz move was absolute (CSI H/f/G)

	// Auto-scroll mode control (DEC Private Mode)
	autoScrollDisabled bool // When true, cursor-following auto-scroll is disabled

	// DECAWM - Auto-wrap mode (DEC Private Mode 7)
	autoWrapMode bool // When true (default), cursor wraps to next line at end of row

	// Smart word wrap mode (DEC Private Mode 7702)
	smartWordWrap bool // When true, wrap at word boundaries instead of mid-word

	selectionActive      bool
	selStartX, selStartY int
	selEndX, selEndY     int

	savedCursorX int
	savedCursorY int

	dirty         bool
	onDirty       func()
	onScaleChange func()     // Called when screen scaling modes change
	onThemeChange func(bool) // Called when theme changes (arg: isDark)

	// Theme state (DECSCNM - Screen Mode)
	darkTheme          bool // Current theme: true=dark, false=light
	preferredDarkTheme bool // User's preferred theme from config (restored on reset)

	// Screen scaling modes
	columnMode132 bool // 132-column mode: horizontal scale 0.6060 (ESC [ 3 h/l)
	columnMode40  bool // 40-column mode: horizontal scale 2.0 (custom)
	lineDensity   int  // Line density: 25 (default), 30, 43, 50, 60

	// Custom glyph system - tile-based graphics
	currentBGP   int  // Current Base Glyph Palette (-1 = use foreground color code)
	currentXFlip bool // Current horizontal flip attribute
	currentYFlip bool // Current vertical flip attribute

	// Global palette and glyph storage (shared across all cells)
	palettes     map[int]*Palette      // Palette number -> Palette
	customGlyphs map[rune]*CustomGlyph // Rune -> CustomGlyph

	// Note: Glyph cache invalidation uses content hashing (Palette.ComputeHash, CustomGlyph.ComputeHash)
	// instead of version tracking, so alternating between glyph frames will be cache hits

	// Sprite overlay system
	sprites      map[int]*Sprite        // Sprite ID -> Sprite
	cropRects    map[int]*CropRectangle // Crop rectangle ID -> CropRectangle
	spriteUnitX  int                    // Subdivisions per cell horizontally (default 8)
	spriteUnitY  int                    // Subdivisions per cell vertically (default 8)

	// Screen crop (in sprite coordinate units, -1 = no crop)
	widthCrop  int // X coordinate beyond which nothing renders
	heightCrop int // Y coordinate below which nothing renders

	// Screen splits for multi-region rendering
	screenSplits map[int]*ScreenSplit // Split ID -> ScreenSplit

	// Max content width from splits (for horizontal scrollbar, independent from scrollback)
	splitContentWidth int
}

// ScreenSplit defines a split region that can show a different part of the buffer.
// ScreenY is a LOGICAL scanline number relative to the scroll boundary (yellow dotted line).
// The first logical scanline (0) begins after the scrollback area - no splits can occur
// in the scrollback area above the yellow dotted line.
type ScreenSplit struct {
	ScreenY         int     // Y in sprite units relative to logical screen start (NOT absolute screen)
	BufferRow       int     // 0-indexed row in logical screen to start drawing from
	BufferCol       int     // 0-indexed column in logical screen to start drawing from
	TopFineScroll   int     // 0 to (subdivisions-1), higher = more of top row clipped
	LeftFineScroll  int     // 0 to (subdivisions-1), higher = more of left column clipped
	CharWidthScale  float64 // Character width multiplier (0 = inherit from main screen)
	LineDensity     int     // Line density override (0 = inherit from main screen)
}

// NewBuffer creates a new terminal buffer
func NewBuffer(cols, rows, maxScrollback int) *Buffer {
	b := &Buffer{
		cols:                cols,
		rows:                rows,
		logicalCols:         0, // 0 means use physical
		logicalRows:         0, // 0 means use physical
		cursorVisible:       true,
		currentFg:           DefaultForeground,
		currentBg:           DefaultBackground,
		maxScrollback:       maxScrollback,
		screenInfo:          DefaultScreenInfo(),
		dirty:               true,
		darkTheme:           true, // Default to dark theme
		preferredDarkTheme:  true, // User preference defaults to dark
		lineDensity:         25,            // Default line density
		currentBGP:          -1,            // -1 = use foreground color code as palette
		palettes:     make(map[int]*Palette),
		customGlyphs: make(map[rune]*CustomGlyph),
		sprites:             make(map[int]*Sprite),
		cropRects:           make(map[int]*CropRectangle),
		spriteUnitX:         8,  // Default: 8 subdivisions per cell
		spriteUnitY:         8,  // Default: 8 subdivisions per cell
		widthCrop:           -1, // -1 = no crop
		heightCrop:          -1, // -1 = no crop
		screenSplits:        make(map[int]*ScreenSplit),
		autoWrapMode:        true, // DECAWM default enabled
		smartWordWrap:       true, // Smart word wrap default enabled
	}
	b.initScreen()
	return b
}

// EffectiveCols returns the logical column count (physical if logical is 0)
func (b *Buffer) EffectiveCols() int {
	if b.logicalCols > 0 {
		return b.logicalCols
	}
	return b.cols
}

// EffectiveRows returns the logical row count (physical if logical is 0)
func (b *Buffer) EffectiveRows() int {
	if b.logicalRows > 0 {
		return b.logicalRows
	}
	return b.rows
}

// SetDirtyCallback sets a callback to be invoked when the buffer changes
func (b *Buffer) SetDirtyCallback(fn func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onDirty = fn
}

// SetScaleChangeCallback sets a callback to be invoked when screen scaling modes change
// This allows the widget to recalculate terminal dimensions when scale changes
func (b *Buffer) SetScaleChangeCallback(fn func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onScaleChange = fn
}

func (b *Buffer) markDirty() {
	b.dirty = true
	if b.onDirty != nil {
		b.onDirty()
	}
}

func (b *Buffer) notifyScaleChange() {
	if b.onScaleChange != nil {
		b.onScaleChange()
	}
}

// SetThemeChangeCallback sets a callback to be invoked when the terminal theme changes
// The callback receives true for dark theme, false for light theme
func (b *Buffer) SetThemeChangeCallback(fn func(bool)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onThemeChange = fn
}

func (b *Buffer) notifyThemeChange() {
	if b.onThemeChange != nil {
		b.onThemeChange(b.darkTheme)
	}
}

// SetDarkTheme sets the current theme (true=dark, false=light)
// This is called by DECSCNM (CSI ? 5 h/l) escape sequences
func (b *Buffer) SetDarkTheme(dark bool) {
	b.mu.Lock()
	changed := b.darkTheme != dark
	b.darkTheme = dark
	if changed {
		b.markDirty()
	}
	b.mu.Unlock()
	if changed {
		b.notifyThemeChange()
	}
}

// IsDarkTheme returns the current theme state (true=dark, false=light)
func (b *Buffer) IsDarkTheme() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.darkTheme
}

// SetPreferredDarkTheme sets the user's preferred theme from config
// This is restored on terminal reset
func (b *Buffer) SetPreferredDarkTheme(dark bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.preferredDarkTheme = dark
	// Also set current theme to match preference initially
	b.darkTheme = dark
}

// GetPreferredDarkTheme returns the user's preferred theme
func (b *Buffer) GetPreferredDarkTheme() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.preferredDarkTheme
}

// UpdatePreferredDarkTheme updates the user's preferred theme from config
// without changing the current DECSCNM state. Use this when updating
// settings while preserving any \e[?5h/\e[?5l state the program has set.
func (b *Buffer) UpdatePreferredDarkTheme(dark bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.preferredDarkTheme = dark
	// Note: Unlike SetPreferredDarkTheme, we do NOT change b.darkTheme here
	// to preserve any DECSCNM state the running program has set
}

func (b *Buffer) initScreen() {
	effectiveRows := b.EffectiveRows()
	b.screen = make([][]Cell, effectiveRows)
	b.lineInfos = make([]LineInfo, effectiveRows)
	for i := range b.screen {
		b.screen[i] = b.makeEmptyLine()
		b.lineInfos[i] = b.makeDefaultLineInfo()
	}
}

// makeEmptyLine creates an empty line (zero length - will grow as chars are written)
func (b *Buffer) makeEmptyLine() []Cell {
	// Start with zero length - lines grow dynamically as characters are written
	return make([]Cell, 0)
}

// makeDefaultLineInfo creates a LineInfo with current attributes
func (b *Buffer) makeDefaultLineInfo() LineInfo {
	return LineInfo{
		Attribute:   LineAttrNormal,
		DefaultCell: b.currentDefaultCell(),
	}
}

// currentDefaultCell creates an empty cell with current attribute settings
func (b *Buffer) currentDefaultCell() Cell {
	fg := b.currentFg
	bg := b.currentBg
	if b.currentReverse {
		fg, bg = bg, fg
	}
	return EmptyCellWithAttrs(fg, bg, b.currentBold, b.currentItalic, b.currentUnderline, b.currentReverse, b.currentBlink)
}

// updateScreenInfo updates the screen info with current attributes
// Called on clear screen, clear to end of screen, and formfeed
func (b *Buffer) updateScreenInfo() {
	b.screenInfo = ScreenInfo{
		DefaultCell: b.currentDefaultCell(),
	}
}

// Resize resizes the physical terminal dimensions
// This updates the visible area but does NOT truncate line content
func (b *Buffer) Resize(cols, rows int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if cols == b.cols && rows == b.rows {
		return
	}

	// Calculate logicalHiddenAbove BEFORE resize to track scrollback visibility state
	oldEffectiveRows := b.EffectiveRows()
	oldLogicalHiddenAbove := 0
	if oldEffectiveRows > b.rows {
		oldLogicalHiddenAbove = oldEffectiveRows - b.rows
	}

	// Check if user was viewing scrollback before resize
	// (scrollOffset > logicalHiddenAbove means boundary line would be visible or past it)
	wasViewingScrollback := b.scrollOffset > oldLogicalHiddenAbove

	// When window gets wider, prefer to unscroll horizontally first
	// This reveals hidden columns on the left before showing blank columns on the right
	if cols > b.cols && b.horizOffset > 0 {
		colsAdded := cols - b.cols
		if colsAdded >= b.horizOffset {
			// All hidden columns can be revealed
			b.horizOffset = 0
		} else {
			// Reduce scroll offset by the number of new columns
			b.horizOffset -= colsAdded
		}
	}

	b.cols = cols
	b.rows = rows

	// If logical dimensions are 0 (using physical), we may need to adjust screen size
	if b.logicalRows == 0 {
		b.adjustScreenToRows(rows)
	}

	// Clamp cursor to logical dimensions (not physical)
	effectiveCols := b.EffectiveCols()
	effectiveRows := b.EffectiveRows()
	if b.cursorX >= effectiveCols {
		b.cursorX = effectiveCols - 1
	}
	if b.cursorY >= effectiveRows {
		b.trackCursorYMove(effectiveRows - 1)
		b.cursorY = effectiveRows - 1
	}

	// Calculate new logicalHiddenAbove after resize
	newLogicalHiddenAbove := 0
	if effectiveRows > rows {
		newLogicalHiddenAbove = effectiveRows - rows
	}

	// Preserve scrollback visibility state:
	// If user was NOT viewing scrollback before, don't let resize reveal scrollback.
	// Instead, allow blank lines at the bottom and favor showing more of the logical screen.
	if !wasViewingScrollback && b.scrollOffset > newLogicalHiddenAbove {
		// Cap scrollOffset so scrollback doesn't become visible
		b.scrollOffset = newLogicalHiddenAbove
	}

	// Also clamp to maximum scroll offset (scrollback + hidden + magnetic threshold)
	maxOffset := b.getMaxScrollOffsetInternal()
	if b.scrollOffset > maxOffset {
		b.scrollOffset = maxOffset
	}

	b.markDirty()
}

// adjustScreenToRows adjusts the screen slice to have the target number of rows
// without truncating line content (lines remain variable width)
// Only moves lines to scrollback if actual content exceeds the new height
func (b *Buffer) adjustScreenToRows(targetRows int) {
	currentRows := len(b.screen)

	if targetRows == currentRows {
		return
	}

	if targetRows > currentRows {
		// Add new empty lines
		for i := currentRows; i < targetRows; i++ {
			b.screen = append(b.screen, b.makeEmptyLine())
			b.lineInfos = append(b.lineInfos, b.makeDefaultLineInfo())
		}
	} else {
		// Shrink: only move lines to scrollback if content doesn't fit
		// Find the last row with actual content
		lastContentRow := -1
		for i := currentRows - 1; i >= 0; i-- {
			if len(b.screen[i]) > 0 {
				lastContentRow = i
				break
			}
		}

		// Calculate how many lines need to go to scrollback
		// Only push if content extends beyond target height
		linesToPush := 0
		if lastContentRow >= targetRows {
			linesToPush = lastContentRow - targetRows + 1
		}

		// Push content lines to scrollback
		for i := 0; i < linesToPush; i++ {
			b.pushLineToScrollback(b.screen[0], b.lineInfos[0])
			b.screen = b.screen[1:]
			b.lineInfos = b.lineInfos[1:]
		}

		// Adjust cursor position to stay with content
		if linesToPush > 0 {
			newY := b.cursorY - linesToPush
			if newY < 0 {
				newY = 0
			}
			b.trackCursorYMove(newY)
			b.cursorY = newY
		}

		// Now trim or add to reach target rows
		currentRows = len(b.screen)
		if currentRows > targetRows {
			// Trim empty lines from bottom
			b.screen = b.screen[:targetRows]
			b.lineInfos = b.lineInfos[:targetRows]
		} else if currentRows < targetRows {
			// Add empty lines to reach target
			for i := currentRows; i < targetRows; i++ {
				b.screen = append(b.screen, b.makeEmptyLine())
				b.lineInfos = append(b.lineInfos, b.makeDefaultLineInfo())
			}
		}
	}
}

// pushLineToScrollback adds a line to the scrollback buffer
func (b *Buffer) pushLineToScrollback(line []Cell, info LineInfo) {
	// Skip if scrollback is disabled (lines are discarded instead)
	if b.scrollbackDisabled {
		return
	}

	trimmed := false
	if len(b.scrollback) >= b.maxScrollback {
		b.scrollback = b.scrollback[1:]
		b.scrollbackInfo = b.scrollbackInfo[1:]
		trimmed = true
	}
	b.scrollback = append(b.scrollback, line)
	b.scrollbackInfo = append(b.scrollbackInfo, info)

	// If scrollback was trimmed from front and we're scrolled into scrollback,
	// adjust offset to keep viewing the same content
	if trimmed && b.scrollOffset > 0 {
		b.scrollOffset--
	}
	// Note: if user was at scrollOffset 0, they stay at 0 (viewing newest content)
	// If at some other scrollback position, they stay there but see newer lines
}

// SetLogicalSize sets the logical terminal dimensions
// A value of 0 means "use physical dimension"
// This implements the ESC [ 8 ; rows ; cols t escape sequence
func (b *Buffer) SetLogicalSize(logicalRows, logicalCols int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	oldEffectiveRows := b.EffectiveRows()

	// Update logical dimensions (0 means use physical)
	b.logicalCols = logicalCols
	b.logicalRows = logicalRows

	// Auto-toggle smart word wrap based on logical width:
	// - When a specific width is set (cols > 0), disable smart wrap
	// - When returning to default (cols = 0), enable smart wrap
	if logicalCols > 0 {
		b.smartWordWrap = false
	} else {
		b.smartWordWrap = true
	}

	newEffectiveRows := b.EffectiveRows()

	if newEffectiveRows == oldEffectiveRows {
		b.markDirty()
		return
	}

	if newEffectiveRows > oldEffectiveRows {
		// Growing - add empty lines at bottom if needed
		// The logical top stays the same, we gain scrollable area
		for len(b.screen) < newEffectiveRows {
			b.screen = append(b.screen, b.makeEmptyLine())
			b.lineInfos = append(b.lineInfos, LineInfo{
				Attribute:   LineAttrNormal,
				DefaultCell: b.screenInfo.DefaultCell,
			})
		}
	} else {
		// Shrinking - need to move excess lines to scrollback
		// Find the last line with actual content
		b.shrinkLogicalScreen(newEffectiveRows)
	}

	// Clamp cursor to new dimensions
	effectiveCols := b.EffectiveCols()
	if b.cursorX >= effectiveCols {
		b.cursorX = effectiveCols - 1
	}
	if b.cursorY >= newEffectiveRows {
		b.trackCursorYMove(newEffectiveRows - 1)
		b.cursorY = newEffectiveRows - 1
	}

	b.markDirty()
}

// shrinkLogicalScreen shrinks the screen to targetRows
// Lines above the new top are transferred to scrollback
func (b *Buffer) shrinkLogicalScreen(targetRows int) {
	if targetRows <= 0 || len(b.screen) <= targetRows {
		return
	}

	// Find the last line that has actual content (non-empty)
	lastContentLine := -1
	for i := len(b.screen) - 1; i >= 0; i-- {
		if len(b.screen[i]) > 0 {
			lastContentLine = i
			break
		}
	}

	if lastContentLine < 0 {
		// No content at all - just resize
		b.screen = b.screen[:targetRows]
		b.lineInfos = b.lineInfos[:targetRows]
		return
	}

	// Count from lastContentLine up to get targetRows lines
	// but never go beyond the current top (index 0)
	newTopLine := lastContentLine - targetRows + 1
	if newTopLine < 0 {
		newTopLine = 0
	}

	// Transfer lines above newTopLine to scrollback
	for i := 0; i < newTopLine; i++ {
		b.pushLineToScrollback(b.screen[i], b.lineInfos[i])
	}

	// Keep lines from newTopLine to end, but only up to targetRows
	if newTopLine > 0 {
		b.screen = b.screen[newTopLine:]
		b.lineInfos = b.lineInfos[newTopLine:]
	}

	// Trim to targetRows (this handles the case where we have more lines than target)
	if len(b.screen) > targetRows {
		b.screen = b.screen[:targetRows]
		b.lineInfos = b.lineInfos[:targetRows]
	}

	// If we still have fewer lines than target, add empty ones
	for len(b.screen) < targetRows {
		b.screen = append(b.screen, b.makeEmptyLine())
		b.lineInfos = append(b.lineInfos, LineInfo{
			Attribute:   LineAttrNormal,
			DefaultCell: b.screenInfo.DefaultCell,
		})
	}
}

// GetLogicalSize returns the logical terminal dimensions
// Returns 0 for dimensions that are set to "use physical"
func (b *Buffer) GetLogicalSize() (logicalRows, logicalCols int) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.logicalRows, b.logicalCols
}

// GetSize returns the current terminal dimensions
func (b *Buffer) GetSize() (cols, rows int) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.cols, b.rows
}

















// SetBracketedPasteMode enables or disables bracketed paste mode
func (b *Buffer) SetBracketedPasteMode(enabled bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.bracketedPasteMode = enabled
}

// IsBracketedPasteModeEnabled returns whether bracketed paste mode is enabled
func (b *Buffer) IsBracketedPasteModeEnabled() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.bracketedPasteMode
}

// SetFlexWidthMode enables or disables flexible East Asian Width mode
// When enabled, new characters get FlexWidth=true and their CellWidth calculated
// based on Unicode East_Asian_Width property (0.5/1.0/1.5/2.0 cell units)
func (b *Buffer) SetFlexWidthMode(enabled bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.flexWidthMode = enabled
	b.currentFlexWidth = enabled
}

// IsFlexWidthModeEnabled returns whether flexible East Asian Width mode is enabled
func (b *Buffer) IsFlexWidthModeEnabled() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.flexWidthMode
}

// SetVisualWidthWrap enables or disables visual width-based line wrapping
// When enabled, lines wrap based on accumulated visual width (sum of CellWidth)
// When disabled, lines wrap based on cell count
func (b *Buffer) SetVisualWidthWrap(enabled bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.visualWidthWrap = enabled
}

// IsVisualWidthWrapEnabled returns whether visual width-based wrapping is enabled
func (b *Buffer) IsVisualWidthWrapEnabled() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.visualWidthWrap
}

// SetAmbiguousWidthMode sets the handling for ambiguous East Asian Width characters
// Auto: match width of previous character (default)
// Narrow: always 1.0 width
// Wide: always 2.0 width
func (b *Buffer) SetAmbiguousWidthMode(mode AmbiguousWidthMode) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ambiguousWidthMode = mode
}

// GetAmbiguousWidthMode returns the current ambiguous width mode
func (b *Buffer) GetAmbiguousWidthMode() AmbiguousWidthMode {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.ambiguousWidthMode
}

























// SetAttributes sets current text rendering attributes
func (b *Buffer) SetAttributes(fg, bg Color, bold, italic, underline, reverse bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.currentFg = fg
	b.currentBg = bg
	b.currentBold = bold
	b.currentItalic = italic
	b.currentUnderline = underline
	b.currentReverse = reverse
}

// ResetAttributes resets text attributes to defaults
func (b *Buffer) ResetAttributes() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.currentFg = DefaultForeground
	b.currentBg = DefaultBackground
	b.currentBold = false
	b.currentItalic = false
	b.currentUnderline = false
	b.currentUnderlineStyle = UnderlineNone
	b.currentHasUnderlineColor = false
	b.currentReverse = false
	b.currentBlink = false
	b.currentStrikethrough = false
}

// SetForeground sets the current foreground color
func (b *Buffer) SetForeground(c Color) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.currentFg = c
}

// SetBackground sets the current background color
func (b *Buffer) SetBackground(c Color) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.currentBg = c
}

// SetBold sets bold attribute
func (b *Buffer) SetBold(bold bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.currentBold = bold
}

// SetItalic sets italic attribute
func (b *Buffer) SetItalic(italic bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.currentItalic = italic
}

// SetUnderline sets underline attribute (single underline style)
func (b *Buffer) SetUnderline(underline bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.currentUnderline = underline
	if underline {
		b.currentUnderlineStyle = UnderlineSingle
	} else {
		b.currentUnderlineStyle = UnderlineNone
	}
}

// SetUnderlineStyle sets the underline style (also sets Underline bool for compatibility)
func (b *Buffer) SetUnderlineStyle(style UnderlineStyle) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.currentUnderlineStyle = style
	b.currentUnderline = (style != UnderlineNone)
}

// SetUnderlineColor sets the underline color
func (b *Buffer) SetUnderlineColor(color Color) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.currentUnderlineColor = color
	b.currentHasUnderlineColor = true
}

// ResetUnderlineColor resets underline color to use foreground color
func (b *Buffer) ResetUnderlineColor() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.currentHasUnderlineColor = false
}

// SetReverse sets reverse video attribute
func (b *Buffer) SetReverse(reverse bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.currentReverse = reverse
}

// SetBlink sets blink attribute
func (b *Buffer) SetBlink(blink bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.currentBlink = blink
}

// SetStrikethrough sets strikethrough attribute
func (b *Buffer) SetStrikethrough(strikethrough bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.currentStrikethrough = strikethrough
}






















// SetAutoWrapMode enables or disables auto-wrap at end of line (DECAWM, mode 7).
// When disabled, the cursor stays at the last column and characters overwrite that position.
func (b *Buffer) SetAutoWrapMode(enabled bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.autoWrapMode = enabled
}

// IsAutoWrapModeEnabled returns true if auto-wrap is enabled (DECAWM).
func (b *Buffer) IsAutoWrapModeEnabled() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.autoWrapMode
}

// SetSmartWordWrap enables or disables smart word wrap (mode 7702).
// When enabled, wrap occurs at word boundaries (space, hyphen, comma, semicolon, emdash)
// instead of mid-word.
func (b *Buffer) SetSmartWordWrap(enabled bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.smartWordWrap = enabled
}

// IsSmartWordWrapEnabled returns true if smart word wrap is enabled.
func (b *Buffer) IsSmartWordWrapEnabled() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.smartWordWrap
}

























