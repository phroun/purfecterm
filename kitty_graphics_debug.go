package purfecterm

// Opt-in diagnostics for the graphics protocol.
//
// A graphics failure is close to invisible from the outside: a client sets q=2
// to silence responses, the terminal declines to draw, and the screen simply
// stays empty. Nothing in the session says which command failed or why. Setting
// PURFECTERM_GRAPHICS_LOG to a path makes the terminal write one line per
// graphics command with its outcome, which is the difference between reading a
// blank screen and reading a reason.

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	graphicsLogOnce sync.Once
	graphicsLogFile *os.File
	graphicsLogMu   sync.Mutex
)

// graphicsLogTarget opens the log on first use, or returns nil when the
// environment does not ask for one. Nothing is opened, and no work is done per
// command, unless the variable is set.
func graphicsLogTarget() *os.File {
	graphicsLogOnce.Do(func() {
		path := os.Getenv("PURFECTERM_GRAPHICS_LOG")
		if path == "" {
			return
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return
		}
		graphicsLogFile = f
		fmt.Fprintf(f, "\n=== purfecterm graphics log opened %s ===\n",
			time.Now().Format(time.RFC3339))
	})
	return graphicsLogFile
}

// logGraphics records one graphics command and what became of it.
func logGraphics(format string, args ...any) {
	f := graphicsLogTarget()
	if f == nil {
		return
	}
	graphicsLogMu.Lock()
	defer graphicsLogMu.Unlock()
	fmt.Fprintf(f, format+"\n", args...)
}

// GraphicsLoggingEnabled reports whether diagnostics are on, so a caller can
// skip building a message it would only throw away.
func GraphicsLoggingEnabled() bool { return graphicsLogTarget() != nil }

// resetGraphicsLogForTest reopens the log against the current environment. The
// target is resolved once per process, which is what keeps it free when unset;
// a test changing the variable needs that decision made again.
func resetGraphicsLogForTest() {
	graphicsLogMu.Lock()
	defer graphicsLogMu.Unlock()
	if graphicsLogFile != nil {
		graphicsLogFile.Close()
	}
	graphicsLogFile = nil
	graphicsLogOnce = sync.Once{}
}

// DescribePlacement summarises what a renderer is about to draw, for the
// graphics log. It samples the source rather than scanning it — a full-window
// frame is twenty million pixels and this runs per paint — which is enough to
// tell a picture from an empty canvas.
//
// This exists because the protocol log answers "what was the terminal told"
// and not "what did it draw", and those diverge exactly where a frame is
// composed into the wrong place: every command succeeds and the screen stays
// dark.
func DescribePlacement(p *PlacedImage) string {
	if p == nil || p.Image == nil {
		return "<nil placement>"
	}
	sx, sy, sw, sh := p.SourceRect()
	dw, dh := p.DestSize()

	var sampled, opaque int
	const step = 16
	for y := sy; y < sy+sh; y += step {
		for x := sx; x < sx+sw; x += step {
			if x < 0 || y < 0 || x >= p.Image.W || y >= p.Image.H {
				continue
			}
			sampled++
			if p.Image.RGBA[(y*p.Image.W+x)*4+3] != 0 {
				opaque++
			}
		}
	}
	pct := 0
	if sampled > 0 {
		pct = opaque * 100 / sampled
	}
	return fmt.Sprintf(
		"image=%d placement=%d cell=(%d,%d) %dx%d src=(%d,%d %dx%d) dest=%dx%d z=%d opaque=%d%%",
		p.ImageID, p.PlacementID, p.Col, p.Row, p.CellsWide, p.CellsHigh,
		sx, sy, sw, sh, dw, dh, p.ZIndex, pct)
}

// logPlacements records a paint's placements, rate limited: a render loop
// paints continuously and an unbounded log would bury the thing being looked
// for. Only a change in what is drawn is worth a line.
var lastPlacementSummary string

// LogPlacements is the exported entry point a renderer calls.
func LogPlacements(where string, images []*PlacedImage) {
	if !GraphicsLoggingEnabled() {
		return
	}
	var b strings.Builder
	for _, im := range images {
		b.WriteString("\n    " + DescribePlacement(im))
	}
	summary := b.String()
	graphicsLogMu.Lock()
	changed := summary != lastPlacementSummary
	lastPlacementSummary = summary
	graphicsLogMu.Unlock()
	if !changed {
		return
	}
	if summary == "" {
		logGraphics("PAINT %s: no placements", where)
		return
	}
	logGraphics("PAINT %s drawing %d:%s", where, len(images), summary)
}
