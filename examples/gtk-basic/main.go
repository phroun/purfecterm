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
	"fmt"
	"log"
	"os"
	"runtime"

	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
	terminal "github.com/phroun/purfecterm/gtk"
)

func main() {
	// Lock main thread for GTK (required on macOS)
	runtime.LockOSThread()

	gtk.Init(&os.Args)

	win, err := gtk.WindowNew(gtk.WINDOW_TOPLEVEL)
	if err != nil {
		log.Fatal("Unable to create window:", err)
	}
	win.SetTitle("PurfecTerm GTK Example")
	win.SetDefaultSize(800, 600)

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

	// Handle window close
	win.Connect("destroy", func() {
		term.Close()
		gtk.MainQuit()
	})

	win.ShowAll()

	// Start the shell AFTER ShowAll to ensure widget is realized
	glib.IdleAdd(func() bool {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
		err := term.RunShell()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start shell: %v\n", err)
		}
		return false // Don't repeat
	})

	gtk.Main()
}
