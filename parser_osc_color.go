package purfecterm

import (
	"fmt"
	"strconv"
	"strings"
)

// OSC 0/1/2 (title) and OSC 4/10/11/12 (color) handlers.

// executeOSCTitle handles OSC 0/1/2: set the window/icon title. All three set
// the window title here (icon-name-only, OSC 1, is treated the same).
func (p *Parser) executeOSCTitle(args string) {
	p.buffer.SetWindowTitle(args)
}

// executeOSCColorFgBg handles OSC 10 (foreground), 11 (background), 12 (cursor).
// A "?" spec queries the current value; otherwise it sets the color.
func (p *Parser) executeOSCColorFgBg(which int, spec string) {
	spec = strings.TrimSpace(spec)
	if spec == "?" {
		if p.responseSink == nil {
			return
		}
		var c Color
		switch which {
		case 10:
			c = p.buffer.EffectiveDefaultForeground()
		case 11:
			c = p.buffer.EffectiveDefaultBackground()
		case 12:
			c = p.buffer.EffectiveCursorColor()
		}
		p.responseSink([]byte(fmt.Sprintf("\x1b]%d;%s\x07", which, formatXColor(c))))
		return
	}
	c, ok := parseXColor(spec)
	if !ok {
		return
	}
	switch which {
	case 10:
		p.buffer.SetDefaultForegroundColor(c)
	case 11:
		p.buffer.SetDefaultBackgroundColor(c)
	case 12:
		p.buffer.SetCursorColor(c)
	}
}

// executeOSCPaletteColor handles OSC 4 ; index ; spec (repeatable). A "?" spec
// queries; otherwise it sets a 256-color palette entry.
func (p *Parser) executeOSCPaletteColor(args string) {
	parts := strings.Split(args, ";")
	// Process index;spec pairs.
	for i := 0; i+1 < len(parts); i += 2 {
		idx, err := strconv.Atoi(strings.TrimSpace(parts[i]))
		if err != nil {
			continue
		}
		spec := strings.TrimSpace(parts[i+1])
		if spec == "?" {
			if p.responseSink != nil {
				c := p.buffer.GetPaletteColor(idx)
				p.responseSink([]byte(fmt.Sprintf("\x1b]4;%d;%s\x07", idx, formatXColor(c))))
			}
			continue
		}
		if c, ok := parseXColor(spec); ok {
			p.buffer.SetPaletteColor(idx, c)
		}
	}
}

// parseXColor parses an X11 color spec: "rgb:rr/gg/bb" (1-4 hex digits per
// component) or "#rgb" / "#rrggbb" / "#rrrgggbbb" / "#rrrrggggbbbb".
func parseXColor(spec string) (Color, bool) {
	if strings.HasPrefix(spec, "rgb:") {
		comps := strings.Split(spec[4:], "/")
		if len(comps) != 3 {
			return Color{}, false
		}
		var v [3]uint8
		for i, c := range comps {
			b, ok := scaleHexComponent(c)
			if !ok {
				return Color{}, false
			}
			v[i] = b
		}
		return TrueColor(v[0], v[1], v[2]), true
	}
	if strings.HasPrefix(spec, "#") {
		hex := spec[1:]
		if len(hex)%3 != 0 || len(hex) == 0 {
			return Color{}, false
		}
		n := len(hex) / 3
		var v [3]uint8
		for i := 0; i < 3; i++ {
			b, ok := scaleHexComponent(hex[i*n : i*n+n])
			if !ok {
				return Color{}, false
			}
			v[i] = b
		}
		return TrueColor(v[0], v[1], v[2]), true
	}
	return Color{}, false
}

// scaleHexComponent parses a 1-4 digit hex component and scales it to 8 bits.
func scaleHexComponent(s string) (uint8, bool) {
	if len(s) < 1 || len(s) > 4 {
		return 0, false
	}
	val, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, false
	}
	// Scale from len*4 bits down to 8 bits.
	bits := len(s) * 4
	if bits == 8 {
		return uint8(val), true
	}
	max := (uint32(1) << bits) - 1
	return uint8(uint32(val) * 255 / max), true
}

// formatXColor renders a Color as xterm's 16-bit "rgb:rrrr/gggg/bbbb" reply.
func formatXColor(c Color) string {
	return fmt.Sprintf("rgb:%02x%02x/%02x%02x/%02x%02x", c.R, c.R, c.G, c.G, c.B, c.B)
}
