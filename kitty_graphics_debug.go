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
