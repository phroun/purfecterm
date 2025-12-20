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
- `purfecterm-gtk` - GTK3 terminal widget
- `purfecterm-qt` - Qt5 terminal widget

## Installation

```bash
go get github.com/phroun/purfecterm


### GTK3 Widget

```bash
# Linux
sudo apt install libgtk-3-dev

# macOS
brew install gtk+3

go get github.com/phroun/purfecterm/purfecterm-gtk
```

### Qt5 Widget

```bash
# Linux
sudo apt install qtbase5-dev

# macOS
brew install qt@5

go get github.com/phroun/purfecterm/purfecterm-qt
```

## Usage

### GTK3 Example

```go
package main

import (
    "github.com/gotk3/gotk3/gtk"
    purfectermgtk "github.com/phroun/purfecterm/purfecterm-gtk"
)

func main() {
    gtk.Init(nil)

    win, _ := gtk.WindowNew(gtk.WINDOW_TOPLEVEL)
    win.SetTitle("Terminal")
    win.SetDefaultSize(800, 600)
    win.Connect("destroy", gtk.MainQuit)

    term, _ := purfectermgtk.New(purfectermgtk.Options{
        Cols:       80,
        Rows:       24,
        FontFamily: "Monospace",
        FontSize:   14,
    })

    win.Add(term.Widget())
    win.ShowAll()

    term.RunShell()
    gtk.Main()
}
```

### Qt5 Example

```go
package main

import (
    "github.com/mappu/miqt/qt"
    purfectermqt "github.com/phroun/purfecterm/purfecterm-qt"
)

func main() {
    qt.NewQApplication(nil)

    win := qt.NewQMainWindow(nil)
    win.SetWindowTitle("Terminal")
    win.Resize(800, 600)

    term, _ := purfectermqt.New(purfectermqt.Options{
        Cols:       80,
        Rows:       24,
        FontFamily: "Monospace",
        FontSize:   14,
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
    
    // Feed ANSI data
    buf.Feed([]byte("\x1b[31mHello, World!\x1b[0m\n"))
    
    // Get cell content
    cell := buf.GetCell(0, 0)
    fmt.Printf("Character: %c, Color: %v\n", cell.Char, cell.FG)
}
```

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
