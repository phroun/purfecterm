package purfecterm

import "fmt"

// MouseButton constants for mouse event reporting
const (
	MouseButtonLeft    = 0
	MouseButtonMiddle  = 1
	MouseButtonRight   = 2
	MouseButtonRelease = 3 // X10 encoding only
	MouseButtonNone    = 3 // For motion events with no button

	// Modifier flags (added to button value)
	MouseModShift   = 4
	MouseModAlt     = 8
	MouseModControl = 16

	// Motion flag (added to button value for motion events)
	MouseMotionFlag = 32

	// Scroll wheel
	MouseScrollUp   = 64
	MouseScrollDown = 65
)

// EncodeMouseEvent encodes a mouse event into the appropriate escape sequence
// based on the buffer's current mouse encoding mode.
// Parameters:
//   - button: button value (MouseButton* constants, with modifier flags ORed in)
//   - x, y: 1-based cell coordinates
//   - press: true for press/motion, false for release
//   - encodingMode: 0 for X10, 1006 for SGR
//
// Returns the escape sequence bytes, or nil if the event cannot be encoded.
func EncodeMouseEvent(button, x, y int, press bool, encodingMode int) []byte {
	switch encodingMode {
	case 1006: // SGR extended encoding: ESC [ < button ; x ; y M/m
		suffix := byte('M') // press
		if !press {
			suffix = byte('m') // release
		}
		return []byte(fmt.Sprintf("\x1b[<%d;%d;%d%c", button, x, y, suffix))

	default: // X10 encoding: ESC [ M cb cx cy
		cb := button + 32
		if !press {
			cb = MouseButtonRelease + 32
		}
		cx := x + 32
		cy := y + 32
		// X10 encoding can't represent coordinates > 223
		if cx > 255 || cy > 255 {
			return nil
		}
		return []byte{'\x1b', '[', 'M', byte(cb), byte(cx), byte(cy)}
	}
}
