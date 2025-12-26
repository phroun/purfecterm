package cli

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/phroun/purfecterm"
)

// Renderer handles rendering the terminal buffer to the actual CLI terminal
type Renderer struct {
	term *Terminal
	mu   sync.Mutex

	// Render state
	renderNeeded bool
	lastCells    [][]renderedCell // Previous frame for differential rendering
	renderTicker *time.Ticker

	// Output buffer for batching writes
	output strings.Builder

	// Border characters
	borderChars borderCharSet
}

// renderedCell stores the last rendered state of a cell for diff comparison
type renderedCell struct {
	char          rune
	combining     string
	fg            purfecterm.Color
	bg            purfecterm.Color
	bold          bool
	italic        bool
	underline     bool
	reverse       bool
	blink         bool
	strikethrough bool
}

// borderCharSet contains the characters for drawing borders
type borderCharSet struct {
	topLeft     rune
	topRight    rune
	bottomLeft  rune
	bottomRight rune
	horizontal  rune
	vertical    rune
	titleLeft   rune
	titleRight  rune
}

var borderStyles = map[BorderStyle]borderCharSet{
	BorderSingle: {
		topLeft: '\u250c', topRight: '\u2510', bottomLeft: '\u2514', bottomRight: '\u2518',
		horizontal: '\u2500', vertical: '\u2502', titleLeft: '\u2524', titleRight: '\u251c',
	},
	BorderDouble: {
		topLeft: '\u2554', topRight: '\u2557', bottomLeft: '\u255a', bottomRight: '\u255d',
		horizontal: '\u2550', vertical: '\u2551', titleLeft: '\u2561', titleRight: '\u255e',
	},
	BorderHeavy: {
		topLeft: '\u250f', topRight: '\u2513', bottomLeft: '\u2517', bottomRight: '\u251b',
		horizontal: '\u2501', vertical: '\u2503', titleLeft: '\u252b', titleRight: '\u2523',
	},
	BorderRounded: {
		topLeft: '\u256d', topRight: '\u256e', bottomLeft: '\u2570', bottomRight: '\u256f',
		horizontal: '\u2500', vertical: '\u2502', titleLeft: '\u2524', titleRight: '\u251c',
	},
}

// NewRenderer creates a new renderer for the terminal
func NewRenderer(term *Terminal) *Renderer {
	r := &Renderer{
		term:         term,
		renderNeeded: true,
	}

	if term.options.BorderStyle != BorderNone {
		r.borderChars = borderStyles[term.options.BorderStyle]
	}

	return r
}

// RequestRender marks that a render is needed
func (r *Renderer) RequestRender() {
	r.mu.Lock()
	r.renderNeeded = true
	r.mu.Unlock()
}

// RenderLoop runs the main render loop
func (r *Renderer) RenderLoop() {
	// Render at ~60fps max, but only when needed
	r.renderTicker = time.NewTicker(16 * time.Millisecond)
	defer r.renderTicker.Stop()

	for {
		select {
		case <-r.renderTicker.C:
			r.mu.Lock()
			needsRender := r.renderNeeded
			r.renderNeeded = false
			r.mu.Unlock()

			if needsRender {
				r.Render()
			}
		case <-r.term.stopRender:
			return
		}
	}
}

// Render performs a full or differential render of the terminal
func (r *Renderer) Render() {
	r.term.mu.Lock()
	opts := r.term.options
	buffer := r.term.buffer
	r.term.mu.Unlock()

	cols, rows := buffer.GetSize()
	cursorX, cursorY := buffer.GetCursor()
	cursorVisible := buffer.IsCursorVisible()
	isDark := buffer.IsDarkTheme()
	scrollOffset := buffer.GetScrollOffset()

	// Calculate window position
	startX := opts.OffsetX
	startY := opts.OffsetY

	// Account for border
	contentStartX := startX
	contentStartY := startY
	if opts.BorderStyle != BorderNone {
		contentStartX++
		contentStartY++
	}

	// Reset output buffer
	r.output.Reset()

	// Hide cursor during rendering to prevent flicker
	r.output.WriteString("\033[?25l")

	// Draw border if configured
	if opts.BorderStyle != BorderNone {
		r.renderBorder(startX, startY, cols, rows, opts.Title, scrollOffset)
	}

	// Get previous frame for differential rendering
	prevCells := r.lastCells
	needsFullRender := prevCells == nil || len(prevCells) != rows

	// Initialize new cell buffer
	newCells := make([][]renderedCell, rows)
	for y := 0; y < rows; y++ {
		newCells[y] = make([]renderedCell, cols)
	}

	// Current attributes for SGR optimization
	var currentFg, currentBg purfecterm.Color
	currentBold := false
	currentItalic := false
	currentUnderline := false
	currentReverse := false
	currentBlink := false
	currentStrikethrough := false
	firstAttr := true

	// Render each cell
	for y := 0; y < rows; y++ {
		rowChanged := needsFullRender
		if !needsFullRender && len(prevCells[y]) != cols {
			rowChanged = true
		}

		for x := 0; x < cols; x++ {
			cell := buffer.GetVisibleCell(x, y)

			// Resolve colors based on theme
			fg := opts.Scheme.ResolveColor(cell.Foreground, true, isDark)
			bg := opts.Scheme.ResolveColor(cell.Background, false, isDark)

			// Handle reverse video
			if cell.Reverse {
				fg, bg = bg, fg
			}

			// Store for next frame comparison
			newCells[y][x] = renderedCell{
				char:          cell.Char,
				combining:     cell.Combining,
				fg:            fg,
				bg:            bg,
				bold:          cell.Bold,
				italic:        cell.Italic,
				underline:     cell.Underline,
				reverse:       cell.Reverse,
				blink:         cell.Blink,
				strikethrough: cell.Strikethrough,
			}

			// Check if cell changed
			if !rowChanged && !needsFullRender {
				prev := prevCells[y][x]
				if prev.char == cell.Char &&
					prev.combining == cell.Combining &&
					prev.fg == fg &&
					prev.bg == bg &&
					prev.bold == cell.Bold &&
					prev.italic == cell.Italic &&
					prev.underline == cell.Underline &&
					prev.blink == cell.Blink &&
					prev.strikethrough == cell.Strikethrough {
					continue
				}
			}

			// Move cursor to position
			r.output.WriteString(fmt.Sprintf("\033[%d;%dH", contentStartY+y+1, contentStartX+x+1))

			// Build SGR sequence for attributes
			var sgr []string

			// Check if we need to reset
			needsReset := false
			if !firstAttr {
				if (currentBold && !cell.Bold) ||
					(currentItalic && !cell.Italic) ||
					(currentUnderline && !cell.Underline) ||
					(currentReverse && !cell.Reverse) ||
					(currentBlink && !cell.Blink) ||
					(currentStrikethrough && !cell.Strikethrough) {
					needsReset = true
				}
			}

			if needsReset || firstAttr {
				sgr = append(sgr, "0") // Reset
				currentBold = false
				currentItalic = false
				currentUnderline = false
				currentReverse = false
				currentBlink = false
				currentStrikethrough = false
				currentFg = purfecterm.Color{}
				currentBg = purfecterm.Color{}
			}
			firstAttr = false

			// Add attributes
			if cell.Bold && !currentBold {
				sgr = append(sgr, "1")
				currentBold = true
			}
			if cell.Italic && !currentItalic {
				sgr = append(sgr, "3")
				currentItalic = true
			}
			if cell.Underline && !currentUnderline {
				sgr = append(sgr, "4")
				currentUnderline = true
			}
			if cell.Blink && !currentBlink {
				sgr = append(sgr, "5")
				currentBlink = true
			}
			if cell.Strikethrough && !currentStrikethrough {
				sgr = append(sgr, "9")
				currentStrikethrough = true
			}

			// Add colors
			if fg != currentFg {
				sgr = append(sgr, fg.ToSGRCode(true))
				currentFg = fg
			}
			if bg != currentBg {
				sgr = append(sgr, bg.ToSGRCode(false))
				currentBg = bg
			}

			// Write SGR sequence if needed
			if len(sgr) > 0 {
				r.output.WriteString("\033[")
				r.output.WriteString(strings.Join(sgr, ";"))
				r.output.WriteString("m")
			}

			// Write character
			if cell.Char == 0 || cell.Char == ' ' {
				r.output.WriteRune(' ')
			} else {
				r.output.WriteRune(cell.Char)
				if cell.Combining != "" {
					r.output.WriteString(cell.Combining)
				}
			}
		}
	}

	// Render status bar if configured
	if opts.ShowStatusBar {
		r.renderStatusBar(startX, contentStartY+rows, cols, scrollOffset)
	}

	// Reset attributes
	r.output.WriteString("\033[0m")

	// Position and show cursor if visible, not scrolled, and focused
	r.term.mu.Lock()
	focused := r.term.focused
	r.term.mu.Unlock()

	if cursorVisible && scrollOffset == 0 && focused {
		r.output.WriteString(fmt.Sprintf("\033[%d;%dH", contentStartY+cursorY+1, contentStartX+cursorX+1))
		r.output.WriteString("\033[?25h")
	}

	// Flush output
	os.Stdout.WriteString(r.output.String())

	// Store current frame
	r.lastCells = newCells
}

// renderBorder draws the terminal window border
func (r *Renderer) renderBorder(x, y, innerCols, innerRows int, title string, scrollOffset int) {
	bc := r.borderChars
	totalWidth := innerCols + 2

	// Top border
	r.output.WriteString(fmt.Sprintf("\033[%d;%dH", y+1, x+1))
	r.output.WriteString("\033[0m") // Reset attributes

	r.output.WriteRune(bc.topLeft)

	// Title in top border
	if title != "" && len(title) < innerCols-4 {
		padding := (innerCols - len(title) - 2) / 2
		for i := 0; i < padding; i++ {
			r.output.WriteRune(bc.horizontal)
		}
		r.output.WriteRune(bc.titleRight)
		r.output.WriteString(" ")
		r.output.WriteString(title)
		r.output.WriteString(" ")
		r.output.WriteRune(bc.titleLeft)
		remaining := innerCols - padding - len(title) - 4
		for i := 0; i < remaining; i++ {
			r.output.WriteRune(bc.horizontal)
		}
	} else {
		for i := 0; i < innerCols; i++ {
			r.output.WriteRune(bc.horizontal)
		}
	}
	r.output.WriteRune(bc.topRight)

	// Side borders
	for row := 0; row < innerRows; row++ {
		// Left border
		r.output.WriteString(fmt.Sprintf("\033[%d;%dH", y+row+2, x+1))
		r.output.WriteRune(bc.vertical)

		// Right border with optional scrollbar
		r.output.WriteString(fmt.Sprintf("\033[%d;%dH", y+row+2, x+totalWidth))
		if scrollOffset > 0 {
			// Show scrollbar indicator
			maxScroll := r.term.buffer.GetMaxScrollOffset()
			if maxScroll > 0 {
				scrollPos := float64(maxScroll-scrollOffset) / float64(maxScroll)
				thumbPos := int(scrollPos * float64(innerRows-1))
				if row == thumbPos {
					r.output.WriteString("\033[7m") // Reverse video
					r.output.WriteRune(bc.vertical)
					r.output.WriteString("\033[27m") // Normal video
				} else {
					r.output.WriteRune(bc.vertical)
				}
			} else {
				r.output.WriteRune(bc.vertical)
			}
		} else {
			r.output.WriteRune(bc.vertical)
		}
	}

	// Bottom border
	r.output.WriteString(fmt.Sprintf("\033[%d;%dH", y+innerRows+2, x+1))
	r.output.WriteRune(bc.bottomLeft)
	for i := 0; i < innerCols; i++ {
		r.output.WriteRune(bc.horizontal)
	}
	r.output.WriteRune(bc.bottomRight)
}

// renderStatusBar draws the status bar at the bottom
func (r *Renderer) renderStatusBar(x, y, width int, scrollOffset int) {
	r.output.WriteString(fmt.Sprintf("\033[%d;%dH", y+1, x+1))

	// Status bar style: reversed colors
	r.output.WriteString("\033[7m")

	cols, rows := r.term.buffer.GetSize()
	cursorX, cursorY := r.term.buffer.GetCursor()

	// Build status text
	var status string
	if scrollOffset > 0 {
		maxScroll := r.term.buffer.GetMaxScrollOffset()
		percent := 100 - (scrollOffset * 100 / maxScroll)
		status = fmt.Sprintf(" [%d%%] Lines: %d | Cursor: %d,%d | Size: %dx%d ",
			percent, r.term.buffer.GetScrollbackSize(), cursorX+1, cursorY+1, cols, rows)
	} else {
		status = fmt.Sprintf(" Lines: %d | Cursor: %d,%d | Size: %dx%d ",
			r.term.buffer.GetScrollbackSize(), cursorX+1, cursorY+1, cols, rows)
	}

	// Pad to full width
	if len(status) < width {
		status = status + strings.Repeat(" ", width-len(status))
	} else if len(status) > width {
		status = status[:width]
	}

	r.output.WriteString(status)
	r.output.WriteString("\033[27m") // End reverse video
}

// ForceFullRedraw clears the cached state and forces a complete redraw
func (r *Renderer) ForceFullRedraw() {
	r.mu.Lock()
	r.lastCells = nil
	r.renderNeeded = true
	r.mu.Unlock()
}

// NeedsRender returns true if there are pending changes to render
func (r *Renderer) NeedsRender() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.renderNeeded
}

// RenderToString renders the terminal and returns the ANSI escape sequence string
// instead of writing to stdout. This is useful for embedded mode where the parent
// TUI needs to composite the terminal with other widgets.
// Note: This always performs a full render (no differential optimization).
// If a clip rectangle is set, only cells within that rectangle are rendered.
func (r *Renderer) RenderToString() string {
	r.term.mu.Lock()
	opts := r.term.options
	buffer := r.term.buffer
	focused := r.term.focused
	clipEnabled := r.term.clipEnabled
	clipRect := r.term.clipRect
	r.term.mu.Unlock()

	cols, rows := buffer.GetSize()
	cursorX, cursorY := buffer.GetCursor()
	cursorVisible := buffer.IsCursorVisible()
	isDark := buffer.IsDarkTheme()
	scrollOffset := buffer.GetScrollOffset()

	// Calculate window position
	startX := opts.OffsetX
	startY := opts.OffsetY

	// Account for border
	contentStartX := startX
	contentStartY := startY
	if opts.BorderStyle != BorderNone {
		contentStartX++
		contentStartY++
	}

	// Use a local buffer
	var output strings.Builder

	// Hide cursor during rendering to prevent flicker
	output.WriteString("\033[?25l")

	// Draw border if configured (only visible parts if clipping)
	if opts.BorderStyle != BorderNone {
		if clipEnabled {
			r.renderBorderToClipped(&output, startX, startY, cols, rows, opts.Title, scrollOffset, clipRect)
		} else {
			r.renderBorderTo(&output, startX, startY, cols, rows, opts.Title, scrollOffset)
		}
	}

	// Current attributes for SGR optimization
	var currentFg, currentBg purfecterm.Color
	currentBold := false
	currentItalic := false
	currentUnderline := false
	currentReverse := false
	currentBlink := false
	currentStrikethrough := false
	firstAttr := true

	// Render each cell
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			// Check clipping - screen coordinates are 1-based for ANSI
			screenX := contentStartX + x + 1
			screenY := contentStartY + y + 1
			if clipEnabled && !clipRect.Contains(screenX-1, screenY-1) {
				continue // Skip cells outside clip rectangle
			}

			cell := buffer.GetVisibleCell(x, y)

			// Resolve colors based on theme
			fg := opts.Scheme.ResolveColor(cell.Foreground, true, isDark)
			bg := opts.Scheme.ResolveColor(cell.Background, false, isDark)

			// Handle reverse video
			if cell.Reverse {
				fg, bg = bg, fg
			}

			// Move cursor to position
			output.WriteString(fmt.Sprintf("\033[%d;%dH", screenY, screenX))

			// Build SGR sequence for attributes
			var sgr []string

			// Check if we need to reset
			needsReset := false
			if !firstAttr {
				if (currentBold && !cell.Bold) ||
					(currentItalic && !cell.Italic) ||
					(currentUnderline && !cell.Underline) ||
					(currentReverse && !cell.Reverse) ||
					(currentBlink && !cell.Blink) ||
					(currentStrikethrough && !cell.Strikethrough) {
					needsReset = true
				}
			}

			if needsReset || firstAttr {
				sgr = append(sgr, "0") // Reset
				currentBold = false
				currentItalic = false
				currentUnderline = false
				currentReverse = false
				currentBlink = false
				currentStrikethrough = false
				currentFg = purfecterm.Color{}
				currentBg = purfecterm.Color{}
			}
			firstAttr = false

			// Add attributes
			if cell.Bold && !currentBold {
				sgr = append(sgr, "1")
				currentBold = true
			}
			if cell.Italic && !currentItalic {
				sgr = append(sgr, "3")
				currentItalic = true
			}
			if cell.Underline && !currentUnderline {
				sgr = append(sgr, "4")
				currentUnderline = true
			}
			if cell.Blink && !currentBlink {
				sgr = append(sgr, "5")
				currentBlink = true
			}
			if cell.Strikethrough && !currentStrikethrough {
				sgr = append(sgr, "9")
				currentStrikethrough = true
			}

			// Add colors
			if fg != currentFg {
				sgr = append(sgr, fg.ToSGRCode(true))
				currentFg = fg
			}
			if bg != currentBg {
				sgr = append(sgr, bg.ToSGRCode(false))
				currentBg = bg
			}

			// Write SGR sequence if needed
			if len(sgr) > 0 {
				output.WriteString("\033[")
				output.WriteString(strings.Join(sgr, ";"))
				output.WriteString("m")
			}

			// Write character
			if cell.Char == 0 || cell.Char == ' ' {
				output.WriteRune(' ')
			} else {
				output.WriteRune(cell.Char)
				if cell.Combining != "" {
					output.WriteString(cell.Combining)
				}
			}
		}
	}

	// Render status bar if configured (check clipping)
	if opts.ShowStatusBar {
		statusY := contentStartY + rows
		if !clipEnabled || (statusY >= clipRect.Y && statusY < clipRect.Y+clipRect.Height) {
			if clipEnabled {
				r.renderStatusBarToClipped(&output, startX, statusY, cols, scrollOffset, clipRect)
			} else {
				r.renderStatusBarTo(&output, startX, statusY, cols, scrollOffset)
			}
		}
	}

	// Reset attributes
	output.WriteString("\033[0m")

	// Position and show cursor if visible, not scrolled, focused, and within clip
	if cursorVisible && scrollOffset == 0 && focused {
		cursorScreenX := contentStartX + cursorX
		cursorScreenY := contentStartY + cursorY
		if !clipEnabled || clipRect.Contains(cursorScreenX, cursorScreenY) {
			output.WriteString(fmt.Sprintf("\033[%d;%dH", cursorScreenY+1, cursorScreenX+1))
			output.WriteString("\033[?25h")
		}
	}

	return output.String()
}

// renderBorderTo draws the border to a specific output buffer
func (r *Renderer) renderBorderTo(output *strings.Builder, x, y, innerCols, innerRows int, title string, scrollOffset int) {
	bc := r.borderChars
	totalWidth := innerCols + 2

	// Top border
	output.WriteString(fmt.Sprintf("\033[%d;%dH", y+1, x+1))
	output.WriteString("\033[0m") // Reset attributes

	output.WriteRune(bc.topLeft)

	// Title in top border
	if title != "" && len(title) < innerCols-4 {
		padding := (innerCols - len(title) - 2) / 2
		for i := 0; i < padding; i++ {
			output.WriteRune(bc.horizontal)
		}
		output.WriteRune(bc.titleRight)
		output.WriteString(" ")
		output.WriteString(title)
		output.WriteString(" ")
		output.WriteRune(bc.titleLeft)
		remaining := innerCols - padding - len(title) - 4
		for i := 0; i < remaining; i++ {
			output.WriteRune(bc.horizontal)
		}
	} else {
		for i := 0; i < innerCols; i++ {
			output.WriteRune(bc.horizontal)
		}
	}
	output.WriteRune(bc.topRight)

	// Side borders
	for row := 0; row < innerRows; row++ {
		// Left border
		output.WriteString(fmt.Sprintf("\033[%d;%dH", y+row+2, x+1))
		output.WriteRune(bc.vertical)

		// Right border with optional scrollbar
		output.WriteString(fmt.Sprintf("\033[%d;%dH", y+row+2, x+totalWidth))
		if scrollOffset > 0 {
			// Show scrollbar indicator
			maxScroll := r.term.buffer.GetMaxScrollOffset()
			if maxScroll > 0 {
				scrollPos := float64(maxScroll-scrollOffset) / float64(maxScroll)
				thumbPos := int(scrollPos * float64(innerRows-1))
				if row == thumbPos {
					output.WriteString("\033[7m") // Reverse video
					output.WriteRune(bc.vertical)
					output.WriteString("\033[27m") // Normal video
				} else {
					output.WriteRune(bc.vertical)
				}
			} else {
				output.WriteRune(bc.vertical)
			}
		} else {
			output.WriteRune(bc.vertical)
		}
	}

	// Bottom border
	output.WriteString(fmt.Sprintf("\033[%d;%dH", y+innerRows+2, x+1))
	output.WriteRune(bc.bottomLeft)
	for i := 0; i < innerCols; i++ {
		output.WriteRune(bc.horizontal)
	}
	output.WriteRune(bc.bottomRight)
}

// renderStatusBarTo draws the status bar to a specific output buffer
func (r *Renderer) renderStatusBarTo(output *strings.Builder, x, y, width int, scrollOffset int) {
	output.WriteString(fmt.Sprintf("\033[%d;%dH", y+1, x+1))

	// Status bar style: reversed colors
	output.WriteString("\033[7m")

	cols, rows := r.term.buffer.GetSize()
	cursorX, cursorY := r.term.buffer.GetCursor()

	// Build status text
	var status string
	if scrollOffset > 0 {
		maxScroll := r.term.buffer.GetMaxScrollOffset()
		percent := 100 - (scrollOffset * 100 / maxScroll)
		status = fmt.Sprintf(" [%d%%] Lines: %d | Cursor: %d,%d | Size: %dx%d ",
			percent, r.term.buffer.GetScrollbackSize(), cursorX+1, cursorY+1, cols, rows)
	} else {
		status = fmt.Sprintf(" Lines: %d | Cursor: %d,%d | Size: %dx%d ",
			r.term.buffer.GetScrollbackSize(), cursorX+1, cursorY+1, cols, rows)
	}

	// Pad to full width
	if len(status) < width {
		status = status + strings.Repeat(" ", width-len(status))
	} else if len(status) > width {
		status = status[:width]
	}

	output.WriteString(status)
	output.WriteString("\033[27m") // End reverse video
}

// renderBorderToClipped draws the border with clipping
func (r *Renderer) renderBorderToClipped(output *strings.Builder, x, y, innerCols, innerRows int, title string, scrollOffset int, clip Rect) {
	bc := r.borderChars
	totalWidth := innerCols + 2

	// Helper to check if screen position is within clip
	inClip := func(screenX, screenY int) bool {
		return clip.Contains(screenX, screenY)
	}

	// Top border
	if inClip(x, y) {
		output.WriteString(fmt.Sprintf("\033[%d;%dH", y+1, x+1))
		output.WriteString("\033[0m")
		output.WriteRune(bc.topLeft)
	}

	// Top border horizontal line (simplified - no title when clipped)
	for i := 0; i < innerCols; i++ {
		screenX := x + 1 + i
		if inClip(screenX, y) {
			output.WriteString(fmt.Sprintf("\033[%d;%dH", y+1, screenX+1))
			output.WriteRune(bc.horizontal)
		}
	}

	if inClip(x+totalWidth-1, y) {
		output.WriteString(fmt.Sprintf("\033[%d;%dH", y+1, x+totalWidth))
		output.WriteRune(bc.topRight)
	}

	// Side borders
	for row := 0; row < innerRows; row++ {
		screenY := y + row + 1

		// Left border
		if inClip(x, screenY) {
			output.WriteString(fmt.Sprintf("\033[%d;%dH", screenY+1, x+1))
			output.WriteRune(bc.vertical)
		}

		// Right border
		if inClip(x+totalWidth-1, screenY) {
			output.WriteString(fmt.Sprintf("\033[%d;%dH", screenY+1, x+totalWidth))
			output.WriteRune(bc.vertical)
		}
	}

	// Bottom border
	bottomY := y + innerRows + 1
	if inClip(x, bottomY) {
		output.WriteString(fmt.Sprintf("\033[%d;%dH", bottomY+1, x+1))
		output.WriteRune(bc.bottomLeft)
	}

	for i := 0; i < innerCols; i++ {
		screenX := x + 1 + i
		if inClip(screenX, bottomY) {
			output.WriteString(fmt.Sprintf("\033[%d;%dH", bottomY+1, screenX+1))
			output.WriteRune(bc.horizontal)
		}
	}

	if inClip(x+totalWidth-1, bottomY) {
		output.WriteString(fmt.Sprintf("\033[%d;%dH", bottomY+1, x+totalWidth))
		output.WriteRune(bc.bottomRight)
	}
}

// renderStatusBarToClipped draws the status bar with clipping
func (r *Renderer) renderStatusBarToClipped(output *strings.Builder, x, y, width int, scrollOffset int, clip Rect) {
	cols, rows := r.term.buffer.GetSize()
	cursorX, cursorY := r.term.buffer.GetCursor()

	// Build status text
	var status string
	if scrollOffset > 0 {
		maxScroll := r.term.buffer.GetMaxScrollOffset()
		percent := 100 - (scrollOffset * 100 / maxScroll)
		status = fmt.Sprintf(" [%d%%] Lines: %d | Cursor: %d,%d | Size: %dx%d ",
			percent, r.term.buffer.GetScrollbackSize(), cursorX+1, cursorY+1, cols, rows)
	} else {
		status = fmt.Sprintf(" Lines: %d | Cursor: %d,%d | Size: %dx%d ",
			r.term.buffer.GetScrollbackSize(), cursorX+1, cursorY+1, cols, rows)
	}

	// Pad to full width
	if len(status) < width {
		status = status + strings.Repeat(" ", width-len(status))
	} else if len(status) > width {
		status = status[:width]
	}

	// Render only visible characters
	for i, ch := range status {
		screenX := x + i
		if clip.Contains(screenX, y) {
			output.WriteString(fmt.Sprintf("\033[%d;%dH", y+1, screenX+1))
			output.WriteString("\033[7m") // Reverse video
			output.WriteRune(ch)
		}
	}
	output.WriteString("\033[27m") // End reverse video
}
