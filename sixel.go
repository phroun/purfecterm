package purfecterm

import "strings"

// Sixel graphics decoder (DCS <P1;P2;P3> q <data> ST).
//
// Sixel encodes a bitmap as a stream of "sixels" — one printable character per
// column carrying six vertical pixels. The decoder produces a self-contained
// RGBA image so a renderer only needs to blit it; color registers are resolved
// at decode time. See DecodeSixel for the supported control set.

// SixelImage is a decoded Sixel bitmap in row-major RGBA (4 bytes per pixel,
// alpha 0 = transparent).
type SixelImage struct {
	W, H int
	RGBA []byte
	// Raw is the original DCS sequence (ESC P ... ST) that produced this image,
	// for renderers that pass Sixel through to a capable host terminal.
	Raw []byte
}

// At returns the RGBA of pixel (x,y), for tests. Returns (0,0,0,0) out of bounds.
func (s *SixelImage) At(x, y int) (r, g, b, a uint8) {
	if s == nil || x < 0 || y < 0 || x >= s.W || y >= s.H {
		return 0, 0, 0, 0
	}
	i := (y*s.W + x) * 4
	return s.RGBA[i], s.RGBA[i+1], s.RGBA[i+2], s.RGBA[i+3]
}

type sixelColor struct{ r, g, b uint8 }

type sixelPixel struct {
	c   sixelColor
	set bool
}

// defaultSixelPalette returns the VT340 16-color default palette (the values are
// DEC percentages scaled to 8-bit), padded to 256 entries.
func defaultSixelPalette() []sixelColor {
	pct := [16][3]int{
		{0, 0, 0}, {20, 20, 80}, {80, 13, 13}, {20, 80, 20},
		{80, 20, 80}, {20, 80, 80}, {80, 80, 20}, {53, 53, 53},
		{26, 26, 26}, {33, 33, 60}, {60, 26, 26}, {33, 60, 33},
		{60, 33, 60}, {33, 60, 60}, {60, 60, 33}, {80, 80, 80},
	}
	regs := make([]sixelColor, 256)
	for i, p := range pct {
		regs[i] = sixelColor{pctTo255(p[0]), pctTo255(p[1]), pctTo255(p[2])}
	}
	return regs
}

func pctTo255(v int) uint8 {
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	return uint8((v*255 + 50) / 100)
}

// hlsToRGB converts DEC sixel HLS (H 0-360 with 0=blue, L/S 0-100) to RGB.
func hlsToRGB(h, l, s int) sixelColor {
	// DEC hue 0 is blue; rotate to the standard HSL where 0 is red.
	hf := float64((h+240)%360) / 360.0
	lf := clamp01(float64(l) / 100.0)
	sf := clamp01(float64(s) / 100.0)
	if sf == 0 {
		v := uint8(lf*255 + 0.5)
		return sixelColor{v, v, v}
	}
	var q float64
	if lf < 0.5 {
		q = lf * (1 + sf)
	} else {
		q = lf + sf - lf*sf
	}
	p := 2*lf - q
	r := hueToRGB(p, q, hf+1.0/3.0)
	g := hueToRGB(p, q, hf)
	b := hueToRGB(p, q, hf-1.0/3.0)
	return sixelColor{uint8(r*255 + 0.5), uint8(g*255 + 0.5), uint8(b*255 + 0.5)}
}

func hueToRGB(p, q, t float64) float64 {
	if t < 0 {
		t += 1
	}
	if t > 1 {
		t -= 1
	}
	switch {
	case t < 1.0/6.0:
		return p + (q-p)*6*t
	case t < 1.0/2.0:
		return q
	case t < 2.0/3.0:
		return p + (q-p)*(2.0/3.0-t)*6
	}
	return p
}

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// DecodeSixel decodes a Sixel data string (the bytes between "q" and ST) into an
// RGBA image. params are the DCS parameters (P1;P2;P3); P2==1 makes unset pixels
// transparent, otherwise they take bg. Supports: color registers (#Pc select,
// #Pc;1;H;L;S HLS, #Pc;2;R;G;B RGB in 0-100), RLE (!Pn), raster attributes
// ("Pan;Pad;Ph;Pv), carriage return ($) and line feed (-).
func DecodeSixel(params []int, data string, bg Color) *SixelImage {
	transparent := len(params) >= 2 && params[1] == 1
	regs := defaultSixelPalette()
	cur := 0

	var rows [][]sixelPixel // rows[y][x]
	x, band := 0, 0

	ensure := func(px, py int) {
		for len(rows) <= py {
			rows = append(rows, nil)
		}
		for len(rows[py]) <= px {
			rows[py] = append(rows[py], sixelPixel{})
		}
	}
	paint := func(v int, count int) {
		for k := 0; k < count; k++ {
			for bit := 0; bit < 6; bit++ {
				if v&(1<<bit) != 0 {
					py := band*6 + bit
					ensure(x, py)
					rows[py][x] = sixelPixel{regs[cur], true}
				}
			}
			x++
		}
	}

	i := 0
	n := len(data)
	readNums := func() []int {
		var out []int
		val, has := 0, false
		for i < n {
			ch := data[i]
			if ch >= '0' && ch <= '9' {
				val = val*10 + int(ch-'0')
				has = true
				i++
			} else if ch == ';' {
				out = append(out, val)
				val, has = 0, false
				i++
			} else {
				break
			}
		}
		if has || len(out) > 0 {
			out = append(out, val)
		}
		return out
	}

	for i < n {
		ch := data[i]
		switch {
		case ch >= 0x3F && ch <= 0x7E: // a sixel
			paint(int(ch-0x3F), 1)
			i++
		case ch == '!': // RLE: !Pn <sixel>
			i++
			nums := readNums()
			rep := 1
			if len(nums) > 0 && nums[0] > 0 {
				rep = nums[0]
			}
			if i < n && data[i] >= 0x3F && data[i] <= 0x7E {
				paint(int(data[i]-0x3F), rep)
				i++
			}
		case ch == '#': // color register select / define
			i++
			nums := readNums()
			if len(nums) == 1 {
				cur = clampReg(nums[0])
			} else if len(nums) >= 5 {
				reg := clampReg(nums[0])
				switch nums[1] {
				case 1: // HLS
					regs[reg] = hlsToRGB(nums[2], nums[3], nums[4])
				case 2: // RGB (0-100)
					regs[reg] = sixelColor{pctTo255(nums[2]), pctTo255(nums[3]), pctTo255(nums[4])}
				}
				cur = reg
			}
		case ch == '"': // raster attributes (aspect + size); size pre-grows canvas
			i++
			nums := readNums()
			if len(nums) >= 4 {
				pw, ph := nums[2], nums[3]
				if pw > 0 && ph > 0 && pw <= 10000 && ph <= 10000 {
					ensure(pw-1, ph-1)
				}
			}
		case ch == '$': // carriage return within the band
			x = 0
			i++
		case ch == '-': // line feed to the next band
			band++
			x = 0
			i++
		default:
			i++ // ignore anything else (whitespace, etc.)
		}
	}

	// Determine dimensions.
	h := len(rows)
	w := 0
	for _, r := range rows {
		if len(r) > w {
			w = len(r)
		}
	}
	if w == 0 || h == 0 {
		return &SixelImage{W: 0, H: 0}
	}

	img := &SixelImage{W: w, H: h, RGBA: make([]byte, w*h*4)}
	for y := 0; y < h; y++ {
		row := rows[y]
		for px := 0; px < w; px++ {
			var p sixelPixel
			if px < len(row) {
				p = row[px]
			}
			off := (y*w + px) * 4
			if p.set {
				img.RGBA[off] = p.c.r
				img.RGBA[off+1] = p.c.g
				img.RGBA[off+2] = p.c.b
				img.RGBA[off+3] = 255
			} else if !transparent {
				img.RGBA[off] = bg.R
				img.RGBA[off+1] = bg.G
				img.RGBA[off+2] = bg.B
				img.RGBA[off+3] = 255
			} // else leave transparent (0,0,0,0)
		}
	}
	return img
}

func clampReg(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

// stripSixelIntro splits a raw DCS body ("<params> q <data>") into its numeric
// parameters and the sixel data after the 'q'. Used by the DCS dispatch.
func stripSixelIntro(body string) (params []int, data string, ok bool) {
	q := strings.IndexByte(body, 'q')
	if q < 0 {
		return nil, "", false
	}
	head := body[:q]
	data = body[q+1:]
	// The Sixel introducer is optional numeric params (P1;P2;P3) then 'q'. A
	// non-numeric head means this DCS is something else, not Sixel.
	val := 0
	for i := 0; i < len(head); i++ {
		ch := head[i]
		if ch >= '0' && ch <= '9' {
			val = val*10 + int(ch-'0')
		} else if ch == ';' {
			params = append(params, val)
			val = 0
		} else {
			return nil, "", false
		}
	}
	if head != "" {
		params = append(params, val)
	}
	return params, data, true
}
