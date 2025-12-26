# TUI Toolkit Design for PurfecTerm Integration

This document outlines the architecture needed to create a text-based user interface (TUI) toolkit that supports PurfecTerm's CLI adapter as an embeddable widget alongside other controls.

## Overview

The `purfecterm/cli` package already provides the foundation for embedding:

```go
term, _ := cli.New(cli.Options{
    Embedded: true,           // Don't take over raw mode
    Cols: 80, Rows: 24,
    OffsetX: 10, OffsetY: 5,  // Position on screen
    BorderStyle: cli.BorderRounded,
})
term.Start()                  // Only starts render loop
term.SetFocused(true)         // Enable cursor and input
term.HandleInput(data)        // Receive keystrokes from parent
output := term.RenderToString() // Get ANSI output for compositing
```

A TUI toolkit would provide the framework around this: layout management, focus traversal, event routing, and base widgets.

## Core Architecture

### 1. Widget Interface

All widgets (including PurfecTerm) should implement a common interface:

```go
type Widget interface {
    // Lifecycle
    Init() error
    Destroy()

    // Geometry
    SetBounds(x, y, width, height int)
    GetBounds() (x, y, width, height int)
    GetMinSize() (width, height int)
    GetPreferredSize() (width, height int)

    // Rendering
    Render() string              // Return ANSI escape sequences
    NeedsRender() bool           // Dirty flag for optimization

    // Focus
    SetFocused(focused bool)
    IsFocused() bool
    CanFocus() bool              // Some widgets (labels) can't focus

    // Input (when focused)
    HandleKey(key string) bool   // Return true if consumed
    HandleMouse(event MouseEvent) bool

    // Hierarchy
    Parent() Container
    SetParent(parent Container)
}
```

### 2. Container Interface

Containers hold child widgets and manage layout:

```go
type Container interface {
    Widget

    // Children
    Add(child Widget)
    Remove(child Widget)
    Children() []Widget

    // Layout
    Layout()                     // Recalculate child positions

    // Focus navigation
    FocusNext() Widget           // Tab
    FocusPrev() Widget           // Shift+Tab
    FocusChild(child Widget)
}
```

### 3. PurfecTerm Widget Adapter

Wrap `cli.Terminal` to implement the Widget interface:

```go
type TerminalWidget struct {
    term   *cli.Terminal
    parent Container
    x, y   int
    width, height int
}

func NewTerminalWidget(opts cli.Options) *TerminalWidget {
    opts.Embedded = true  // Force embedded mode
    term, _ := cli.New(opts)
    return &TerminalWidget{term: term}
}

func (w *TerminalWidget) SetBounds(x, y, width, height int) {
    w.x, w.y = x, y
    w.width, w.height = width, height

    // Update terminal options
    w.term.Resize(width-2, height-2)  // Account for border
    // Update OffsetX/OffsetY would require new API or recreation
}

func (w *TerminalWidget) Render() string {
    return w.term.RenderToString()
}

func (w *TerminalWidget) SetFocused(focused bool) {
    w.term.SetFocused(focused)
}

func (w *TerminalWidget) HandleKey(key string) bool {
    // Convert key name back to bytes
    data := keyToBytes(key)
    return w.term.HandleInput(data)
}

func (w *TerminalWidget) CanFocus() bool {
    return true  // Terminal is always focusable
}
```

## Layout System

### Box Layout (Vertical/Horizontal)

```go
type BoxLayout struct {
    direction Direction  // Horizontal or Vertical
    spacing   int
    children  []Widget
}

func (b *BoxLayout) Layout() {
    x, y, w, h := b.GetBounds()

    if b.direction == Vertical {
        currentY := y
        for _, child := range b.children {
            _, prefH := child.GetPreferredSize()
            child.SetBounds(x, currentY, w, prefH)
            currentY += prefH + b.spacing
        }
    }
    // Similar for Horizontal...
}
```

### Grid Layout

```go
type GridLayout struct {
    rows, cols int
    rowHeights []int  // Fixed or proportional
    colWidths  []int
    cells      map[gridPos]Widget
}
```

### Flex Layout

```go
type FlexLayout struct {
    direction Direction
    children  []flexChild
}

type flexChild struct {
    widget Widget
    flex   float64  // 0 = fixed size, >0 = proportional
}
```

## Focus Management

### Focus Ring

Track focusable widgets in order:

```go
type FocusManager struct {
    widgets []Widget  // In tab order
    current int       // Index of focused widget
}

func (fm *FocusManager) FocusNext() {
    if fm.current >= 0 {
        fm.widgets[fm.current].SetFocused(false)
    }

    // Find next focusable widget
    for i := 1; i <= len(fm.widgets); i++ {
        next := (fm.current + i) % len(fm.widgets)
        if fm.widgets[next].CanFocus() {
            fm.current = next
            fm.widgets[next].SetFocused(true)
            return
        }
    }
}
```

### Focus Stealing

Allow widgets to request focus:

```go
func (fm *FocusManager) RequestFocus(widget Widget) {
    for i, w := range fm.widgets {
        if w == widget {
            fm.widgets[fm.current].SetFocused(false)
            fm.current = i
            widget.SetFocused(true)
            return
        }
    }
}
```

## Event System

### Input Pipeline

```
stdin → RawModeReader → KeyParser → EventRouter → FocusedWidget
                            ↓
                    (direct-key-handler)
```

### Event Router

```go
type EventRouter struct {
    focus    *FocusManager
    keyboard *keyboard.Handler
    root     Container
}

func (er *EventRouter) Start() {
    er.keyboard.OnKey = func(key string) {
        // Global shortcuts first
        if er.handleGlobalKey(key) {
            return
        }

        // Route to focused widget
        if focused := er.focus.Current(); focused != nil {
            focused.HandleKey(key)
        }
    }
    er.keyboard.Start()
}

func (er *EventRouter) handleGlobalKey(key string) bool {
    switch key {
    case "Tab":
        er.focus.FocusNext()
        return true
    case "S-Tab":
        er.focus.FocusPrev()
        return true
    case "C-q":
        er.quit()
        return true
    }
    return false
}
```

## Render Pipeline

### Compositor

Combine widget renders into final output:

```go
type Compositor struct {
    root   Container
    output strings.Builder
}

func (c *Compositor) Render() string {
    c.output.Reset()

    // Hide cursor during render
    c.output.WriteString("\033[?25l")

    // Clear screen (or use differential updates)
    c.output.WriteString("\033[2J")

    // Render all widgets
    c.renderWidget(c.root)

    return c.output.String()
}

func (c *Compositor) renderWidget(w Widget) {
    // Render this widget
    c.output.WriteString(w.Render())

    // Render children if container
    if container, ok := w.(Container); ok {
        for _, child := range container.Children() {
            c.renderWidget(child)
        }
    }
}
```

### Render Loop

```go
func (app *Application) renderLoop() {
    ticker := time.NewTicker(16 * time.Millisecond)  // ~60fps

    for {
        select {
        case <-ticker.C:
            if app.needsRender() {
                output := app.compositor.Render()
                os.Stdout.WriteString(output)
            }
        case <-app.quit:
            return
        }
    }
}
```

## Base Widgets

A minimal toolkit should provide:

### Label
```go
type Label struct {
    text  string
    style Style
}
```

### Button
```go
type Button struct {
    text    string
    onClick func()
}

func (b *Button) HandleKey(key string) bool {
    if key == "Enter" || key == " " {
        b.onClick()
        return true
    }
    return false
}
```

### TextInput
```go
type TextInput struct {
    value   string
    cursor  int
    onChange func(string)
}
```

### List/Menu
```go
type List struct {
    items    []string
    selected int
    onSelect func(int)
}
```

### ScrollView
```go
type ScrollView struct {
    child    Widget
    offsetX  int
    offsetY  int
    viewport struct{ width, height int }
}
```

## Example Application

```go
func main() {
    app := tui.NewApplication()

    // Create layout
    root := tui.NewVBox()

    // Header
    header := tui.NewLabel("My TUI App")
    header.SetStyle(tui.StyleBold)
    root.Add(header)

    // Main content: terminal + sidebar
    content := tui.NewHBox()

    // Terminal widget (PurfecTerm)
    term := tui.NewTerminalWidget(cli.Options{
        BorderStyle: cli.BorderRounded,
        Title:       "Shell",
    })
    term.RunShell()
    content.Add(term, tui.Flex(2))  // 2/3 width

    // Sidebar
    sidebar := tui.NewVBox()
    sidebar.Add(tui.NewLabel("Commands:"))
    sidebar.Add(tui.NewButton("Build", func() { term.WriteString("make\n") }))
    sidebar.Add(tui.NewButton("Test", func() { term.WriteString("go test\n") }))
    sidebar.Add(tui.NewButton("Quit", func() { app.Quit() }))
    content.Add(sidebar, tui.Flex(1))  // 1/3 width

    root.Add(content, tui.Flex(1))

    // Status bar
    status := tui.NewLabel("Press Tab to switch focus, Ctrl+Q to quit")
    root.Add(status)

    app.SetRoot(root)
    app.Run()
}
```

## Integration Considerations

### Terminal Resizing

The toolkit must handle SIGWINCH and propagate resize events:

```go
func (app *Application) handleResize() {
    cols, rows := getTerminalSize()
    app.root.SetBounds(0, 0, cols, rows)
    app.root.Layout()

    // Notify terminal widgets specifically
    for _, w := range app.terminalWidgets {
        w.HandleResize(cols, rows)
    }
}
```

### Raw Mode Management

The toolkit owns raw mode, not individual widgets:

```go
func (app *Application) Run() {
    // Enter raw mode once
    oldState, _ := term.MakeRaw(int(os.Stdin.Fd()))
    defer term.Restore(int(os.Stdin.Fd()), oldState)

    // Enable alternate screen
    fmt.Print("\033[?1049h")
    defer fmt.Print("\033[?1049l")

    // Run event loop...
}
```

### Mouse Support

Enable mouse tracking at the application level:

```go
func (app *Application) enableMouse() {
    fmt.Print("\033[?1000h")  // Enable mouse click reporting
    fmt.Print("\033[?1002h")  // Enable mouse drag reporting
    fmt.Print("\033[?1006h")  // Enable SGR extended mouse mode
}
```

## PurfecTerm API Gaps

Current `cli.Terminal` may need these additions for full toolkit integration:

1. **Dynamic Position Updates**
   ```go
   func (t *Terminal) SetOffset(x, y int)  // Change position without recreating
   ```

2. **Bounds Query**
   ```go
   func (t *Terminal) GetBounds() (x, y, cols, rows int)
   ```

3. **Minimum Size**
   ```go
   func (t *Terminal) GetMinSize() (cols, rows int)  // e.g., 20x5
   ```

4. **Dirty Flag**
   ```go
   func (t *Terminal) NeedsRender() bool  // For render optimization
   ```

5. **Key String Input**
   ```go
   func (t *Terminal) HandleKeyString(key string) bool  // Accept "Up", "C-c", etc.
   ```

## Recommended Libraries

For building the toolkit, consider:

- **Input**: `github.com/phroun/direct-key-handler` (already used by cli)
- **Colors**: Standard ANSI, or a color library for RGB/HSL
- **Unicode**: `golang.org/x/text` for proper width calculation
- **Testing**: Create a virtual terminal buffer for headless testing

## Summary

Building a TUI toolkit around PurfecTerm requires:

1. **Widget abstraction** - Common interface for all UI elements
2. **Layout engine** - Box, grid, flex layouts for arranging widgets
3. **Focus management** - Tab navigation, focus stealing
4. **Event routing** - Keyboard/mouse events to focused widget
5. **Compositor** - Combine widget renders into final output
6. **Application shell** - Raw mode, alternate screen, resize handling

The `cli.Terminal` with `Embedded: true` is already 90% of the way there. The toolkit provides the remaining infrastructure to make it one widget among many.
