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
	"log"
	"os"

	"github.com/mappu/miqt/qt"
	terminal "github.com/phroun/purfecterm/qt"
)

func main() {
	qt.NewQApplication(os.Args)

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
		log.Fatal("Unable to create terminal:", err)
	}

	// Add to window
	win.SetCentralWidget(term.Widget())
	win.Show()

	// Start the shell
	err = term.RunShell()
	if err != nil {
		log.Fatal("Failed to start shell:", err)
	}

	qt.QApplication_Exec()
}
