package purfecterm

import "fmt"

// Device status / attribute reports. All replies go through the response sink
// (SetResponseSink); with no sink registered they are silently dropped.

// executeDA handles DA - Device Attributes (CSI c / CSI > c / CSI = c).
func (p *Parser) executeDA() {
	if p.responseSink == nil {
		return
	}
	switch p.csiPrivate {
	case 0: // Primary DA (CSI c / CSI 0 c)
		// VT220 (62) with: 132 columns (1), selective erase (6), ANSI color (22).
		p.responseSink([]byte("\x1b[?62;1;6;22c"))
	case '>': // Secondary DA (CSI > c): type ; firmware version ; keyboard
		p.responseSink([]byte("\x1b[>1;10;0c"))
	case '=': // Tertiary DA (CSI = c): DECReport terminal unit ID (DCS ! | hex ST)
		p.responseSink([]byte("\x1bP!|00000000\x1b\\"))
	}
}

// executeDSR handles DSR - Device Status Report (CSI Ps n / CSI ? Ps n).
func (p *Parser) executeDSR() {
	if p.responseSink == nil {
		return
	}
	ps := p.getParam(0, 0)
	switch p.csiPrivate {
	case 0:
		switch ps {
		case 5: // operating status: terminal OK
			p.responseSink([]byte("\x1b[0n"))
		case 6: // CPR - Cursor Position Report
			row, col := p.buffer.CursorReportPosition()
			p.responseSink([]byte(fmt.Sprintf("\x1b[%d;%dR", row, col)))
		}
	case '?': // DEC-specific DSR
		switch ps {
		case 6: // DECXCPR - extended cursor position report (with page number)
			row, col := p.buffer.CursorReportPosition()
			p.responseSink([]byte(fmt.Sprintf("\x1b[?%d;%d;1R", row, col)))
		}
	}
}

// executeXTSAVE handles XTSAVE (CSI ? Pm s): stash the current value of each DEC
// private mode for a later XTRESTORE.
func (p *Parser) executeXTSAVE() {
	if p.savedModes == nil {
		p.savedModes = map[int]bool{}
	}
	for _, mode := range p.csiParams {
		st := p.decrqmStatus(mode)
		p.savedModes[mode] = st == 1 || st == 3 // set or permanently-set
	}
}

// executeXTRESTORE handles XTRESTORE (CSI ? Pm r): restore each DEC private mode
// to its stashed value. Reuses the private-mode-set path per mode.
func (p *Parser) executeXTRESTORE() {
	saved := p.csiParams
	for _, mode := range saved {
		val, ok := p.savedModes[mode]
		if !ok {
			continue
		}
		p.csiParams = []int{mode}
		p.executePrivateModeSet(val)
	}
	p.csiParams = saved
}
