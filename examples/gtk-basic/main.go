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
	"runtime"

	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
	terminal "github.com/phroun/purfecterm/gtk"
)

const appID = "com.example.purfecterm-gtk"

func main() {
	// Lock main thread for GTK (required on macOS)
	runtime.LockOSThread()

	gtkApp, err := gtk.ApplicationNew(appID, glib.APPLICATION_FLAGS_NONE)
	if err != nil {
		log.Fatal("Unable to create application:", err)
	}

	gtkApp.Connect("activate", func() {
		activate(gtkApp)
	})

	os.Exit(gtkApp.Run(os.Args))
}

func activate(app *gtk.Application) {
	win, err := gtk.ApplicationWindowNew(app)
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
	})

	win.ShowAll()

	// Start the shell and bring window to front after it's fully realized
	glib.IdleAdd(func() bool {
		// Bring window to front on macOS
		win.Present()
		win.GrabFocus()
		term.Widget().GrabFocus()

		if err := term.RunShell(); err != nil {
			log.Printf("Failed to start shell: %v", err)
		}
		return false
	})
}
