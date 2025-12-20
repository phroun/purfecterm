// Example: Basic Qt5 terminal emulator.
//
// This creates a simple terminal window running the user's shell.
//
// Prerequisites:
//   Linux: sudo apt install qtbase5-dev
//   macOS: brew install qt@5
//
// Run with: go run main.go
package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/mappu/miqt/qt"
	terminal "github.com/phroun/purfecterm/qt"
)

func main() {
	// Lock main thread for Qt
	runtime.LockOSThread()

	app := qt.NewQApplication(os.Args)

	win := qt.NewQMainWindow(nil)
	win.SetWindowTitle("PurfecTerm Qt Example")
	win.Resize(800, 600)

	// Create the terminal widget
	term, err := terminal.New(terminal.Options{
		Cols:           80,
		Rows:           24,
		ScrollbackSize: 10000,
		FontFamily:     "Monospace",
		FontSize:       12,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to create terminal: %v\n", err)
		os.Exit(1)
	}

	// Add to window
	win.SetCentralWidget(term.Widget())

	// Handle window close
	win.OnCloseEvent(func(event *qt.QCloseEvent) {
		term.Close()
		event.Accept()
	})

	win.Show()

	// Start the shell after showing window
	// Use a single-shot timer to defer until event loop is running
	qt.QTimer_SingleShot(0, func() {
		err := term.RunShell()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start shell: %v\n", err)
		}
	})

	app.Exec()
}
