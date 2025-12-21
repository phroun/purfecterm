// Example: Using the core terminal buffer without a GUI widget.
//
// This demonstrates:
// - Creating a terminal buffer
// - Parsing ANSI escape sequences
// - Reading cell contents
//
// Run with: go run main.go
package main

import (
	"fmt"

	"github.com/phroun/purfecterm"
)

func main() {
	// Create a terminal buffer (80 cols, 24 rows, 1000 lines scrollback)
	buf := purfecterm.NewBuffer(80, 24, 1000)

	// Create a parser to process ANSI escape sequences
	parser := purfecterm.NewParser(buf)

	// Feed some ANSI data - red "Hello", reset, then " World!"
	parser.Parse([]byte("\x1b[31mHello\x1b[0m World!\n"))

	// Read the cells and display info
	fmt.Println("Buffer contents:")
	for x := 0; x < 12; x++ {
		cell := buf.GetCell(x, 0)
		if cell.Char == 0 {
			break
		}
		fmt.Printf("  [%d] '%c' FG: R=%d G=%d B=%d\n",
			x, cell.Char, cell.Foreground.R, cell.Foreground.G, cell.Foreground.B)
	}

	// Demonstrate cursor position
	curX, curY := buf.GetCursor()
	fmt.Printf("\nCursor position: (%d, %d)\n", curX, curY)

	// Demonstrate more escape sequences
	parser.ParseString("\x1b[1;32mBold Green\x1b[0m  \x1b[4mUnderlined\x1b[0m")

	fmt.Println("\nSecond line contents:")
	for x := 0; x < 22; x++ {
		cell := buf.GetCell(x, 1)
		if cell.Char == 0 {
			break
		}
		attrs := ""
		if cell.Bold {
			attrs += "B"
		}
		if cell.Underline {
			attrs += "U"
		}
		if attrs == "" {
			attrs = "-"
		}
		fmt.Printf("  [%d] '%c' attrs=%s\n", x, cell.Char, attrs)
	}
}
