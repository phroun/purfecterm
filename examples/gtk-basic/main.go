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

	// Feed a welcome message directly (not via PTY) to test rendering
	term.Feed("\x1b[32mWelcome to PurfecTerm!\x1b[0m\r\n")
	term.Feed("If you see this, rendering works.\r\n\r\n")

	// Start the shell AFTER ShowAll to ensure widget is realized
	glib.IdleAdd(func() bool {
		fmt.Println("Starting shell...")
		err := term.RunShell()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start shell: %v\n", err)
		} else {
			fmt.Println("Shell started successfully")
		}
		return false // Don't repeat
	})
}
