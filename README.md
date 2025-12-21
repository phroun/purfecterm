# PurfecTerm

A terminal emulator library for Go with GTK3 and Qt5 widget implementations.

## Features

- Full VT100/VT220/xterm terminal emulation
- 256-color and true color (24-bit) support
- Unicode and CJK character support
- Scrollback buffer with configurable size
- Mouse support (selection, copy/paste)
- PTY management for running shell commands
- Platform support: Linux, macOS, Windows

## Packages

- `purfecterm` - Core terminal buffer, ANSI parser, and PTY abstraction
- `purfecterm/gtk` - GTK3 terminal widget
- `purfecterm/qt` - Qt5 terminal widget

## Installation

```bash
go get github.com/phroun/purfecterm
```

### GTK3 Widget

```bash
# Linux
sudo apt install libgtk-3-dev

# macOS
brew install gtk+3

go get github.com/phroun/purfecterm/gtk
```

### Qt5 Widget

```bash
# Linux
sudo apt install qtbase5-dev

# macOS
brew install qt@5

go get github.com/phroun/purfecterm/qt
```

## Usage

### GTK3 Example

```go
package main

import (
    "os"
    "runtime"

    "github.com/gotk3/gotk3/glib"
    "github.com/gotk3/gotk3/gtk"
    terminal "github.com/phroun/purfecterm/gtk"
)

func main() {
    runtime.LockOSThread()

    app, _ := gtk.ApplicationNew("com.example.terminal", glib.APPLICATION_FLAGS_NONE)
    app.Connect("activate", func() {
        win, _ := gtk.ApplicationWindowNew(app)
        win.SetTitle("Terminal")
        win.SetDefaultSize(800, 600)

        term, _ := terminal.New(terminal.Options{
            Cols: 80, Rows: 24, ScrollbackSize: 10000,
            FontFamily: "Monospace", FontSize: 12,
        })

        win.Add(term.Widget())
        win.Connect("destroy", func() { term.Close() })
        win.ShowAll()
        win.Present()

        glib.IdleAdd(func() bool { term.RunShell(); return false })
    })
    os.Exit(app.Run(os.Args))
}
```

### Qt5 Example

```go
package main

import (
    "os"
    "runtime"

    "github.com/mappu/miqt/qt"
    terminal "github.com/phroun/purfecterm/qt"
)

func main() {
    runtime.LockOSThread()
    qt.NewQApplication(os.Args)

    win := qt.NewQMainWindow(nil)
    win.SetWindowTitle("Terminal")
    win.Resize(800, 600)

    term, _ := terminal.New(terminal.Options{
        Cols: 80, Rows: 24, ScrollbackSize: 10000,
        FontFamily: "Monospace", FontSize: 12,
    })

    win.SetCentralWidget(term.Widget())
    win.Show()

    term.RunShell()
    qt.QApplication_Exec()
}
```

### Core Buffer Only

```go
package main

import (
    "fmt"
    "github.com/phroun/purfecterm"
)

func main() {
    // Create a terminal buffer
    buf := purfecterm.NewBuffer(80, 24, 1000)

    // Create parser and feed ANSI data
    parser := purfecterm.NewParser(buf)
    parser.Parse([]byte("\x1b[31mHello, World!\x1b[0m\n"))

    // Get cell content
    cell := buf.GetCell(0, 0)
    fmt.Printf("Character: %c, Color: R=%d G=%d B=%d\n",
        cell.Char, cell.Foreground.R, cell.Foreground.G, cell.Foreground.B)
}
```

See the `examples/` directory for complete working examples.

## Color Schemes

```go
// Use the default color scheme
scheme := purfecterm.DefaultColorScheme()

// Or create a custom one
scheme := purfecterm.ColorScheme{
    DarkForeground:  purfecterm.Color{R: 200, G: 200, B: 200},
    DarkBackground:  purfecterm.Color{R: 30, G: 30, B: 30},
    LightForeground: purfecterm.Color{R: 40, G: 40, B: 40},
    LightBackground: purfecterm.Color{R: 255, G: 255, B: 255},
    // ... palette colors
}

term.SetColorScheme(scheme)
```

## License

MIT License - see LICENSE file for details.
