// Example: Basic GTK3 terminal emulator.
//
// This creates a simple terminal window running the user's shell.
//
// Prerequisites:
//   Linux: sudo apt install libgtk-3-dev
//   macOS: brew install gtk+3
//
// Run with: go run main.go
package main

import (
	"log"
	"os"

	"github.com/gotk3/gotk3/gtk"
	terminal "github.com/phroun/purfecterm/gtk"
)

func main() {
	gtk.Init(&os.Args)

	win, err := gtk.WindowNew(gtk.WINDOW_TOPLEVEL)
	if err != nil {
		log.Fatal("Unable to create window:", err)
	}
	win.SetTitle("PurfecTerm GTK Example")
	win.SetDefaultSize(800, 600)
	win.Connect("destroy", gtk.MainQuit)

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
	win.Add(term.Widget())
	win.ShowAll()

	// Start the shell
	err = term.RunShell()
	if err != nil {
		log.Fatal("Failed to start shell:", err)
	}

	gtk.Main()
}
