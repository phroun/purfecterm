## Changelog

### 0.2.11
- Use callback for resize on TerminalCapabilities

### 0.2.10 -- December 20, 2025
- Moved into separate repository:  https://github.com/phroun/purfecterm/

### 0.2.9 -- December 15, 2025
- Flexible East Asian Width mode (private mode 2027): cells can have variable
  visual widths (1.0 or 2.0) based on Unicode East_Asian_Width property
- Visual width wrap mode (private mode 2028): line wrapping based on
  accumulated visual width rather than cell count
- Ambiguous width modes (private modes 2029/2030): control rendering of
  ambiguous characters as narrow (1.0), wide (2.0), or auto-match previous
- Mouse selection and rendering properly handle variable cell widths
- Mouse selection tracks buffer-absolute coordinates for scroll stability
- Drag-beyond-edge auto-scrolling during text selection (GTK, Qt):
  vertical and horizontal scrolling when dragging selection beyond edges
- Custom glyph system for tile-based pixel-art graphics:
  - OSC 7000: Palette management (create, delete, set entries)
  - OSC 7001: Glyph definition (replace Unicode chars with pixel-art)
  - SGR 150-153: XFlip/YFlip for sprite mirroring
  - SGR 158;N / 159: Base Glyph Palette (BGP) selection
  - Palette-indexed colors with transparent, dim, and bright variants
  - Extended palette colors: 256-color (`;5;N`) and true RGB (`;r;R;G;B`) support
  - Fallback rendering when palette not defined (0=bg, 1=fg, 2=dim, 3+=bright)
- Sprite overlay system with Z-ordering and crop rectangles:
  - OSC 7002: Sprite management (create, move, delete, update)
  - Z-index layering: negative Z renders behind text, non-negative in front
  - Multi-tile sprites using linefeed (rune 10) as row separator
  - 2x2 NES-style composite sprites for 16x16 pixel-art characters
  - XFlip/YFlip for entire sprites (FLIP parameter: 0-3)
  - Scale factors (XS/YS) for sprite magnification
  - Crop rectangles for clipping sprites to regions
  - Optimized move command (m) for smooth animations
  - Sprites positioned relative to logical screen origin
  - Proper scroll offset handling: sprites scroll with content
- Cursor ANSI codes (CUP, Tab, etc.) respect logical screen dimensions
- Blink attribute applies to custom glyphs with wave animation (BlinkModeBounce)
- Pixel seam fix: extend rectangles by 1px where adjacent non-transparent pixels
  exist to eliminate hairline gaps in sprite/glyph rendering
- Ambiguous width mode (CSI ? 2029/2030) applies to custom glyph characters:
  auto mode uses underlying character's width category with fallback
- Private ANSI codes documented in docs/private-ansi-codes.md
- Fixed ClearToStartOfLine and ClearToStartOfScreen not to pad lines with
  blank cells unnecessarily (fixes horizontal scrollbar showing extra width)
- Removed debug print statements from parser
- Unicode combining character support: Hebrew vowel points (nikkud),
  Arabic marks, Thai marks, Devanagari vowel signs, and other diacritics
  properly attach to previous cell rather than taking new cells
- Pango text rendering in GTK for proper Unicode combining character shaping
- Font fallback configuration: `font_family_unicode` and `font_family_cjk`
  settings for consistent rendering of special characters across GTK and Qt
- Variable-width line storage: lines are no longer truncated on window resize
- LineInfo and ScreenInfo structs for rendering beyond stored line content
- Logical vs physical terminal dimensions with ESC [ 8 ; h ; w t support
- Horizontal scrollbar that shows when content exceeds visible width
- Shift+scroll for horizontal scrolling in both GTK and Qt widgets
- Unicode character width handling: wide characters squeezed to fit,
  narrow characters centered within cell bounds
- Proper scrollback transfer when logical screen shrinks
- Glyph cache for Qt rendering performance
- Magnetic scroll effect at scrollback boundary with dynamic threshold
- Auto-scroll to cursor on keyboard activity
- Split rendering refactored to scanline approach for better performance
- Line attributes properly use effective scroll offset
- Screen crop command simplified: use 0 for inherit values
- Qt keyboard handling improvements:
  - Accept key events to prevent macOS Services menu interception
  - Fixed modifier encoding for values >= 10
  - Fixed macOS keyboard using correct native keycode method
  - Ctrl/Meta swap and macOS Option key handling
  - Tab/Shift+Tab focus navigation using QShortcuts
  - Shift+Alt+Tab and Shift+Meta+Tab shortcuts for PurfecTerm
- FFI design documentation for struct immutability concerns
- Custom purfecterm-gtk terminal emulator
- Cross-platform font fallbacks (JetBrains Mono, Consolas, DejaVu, etc.)
- VT100 double-size text rendering (DECDHL/DECDWL)
- Bobbing wave animation for blink text attribute
- Context menu with paste support
- VGA/ANSI color palette mapping
- macOS-style scrollbar styling using terminal background color
- Terminal theme system: light/dark mode support with palette color schemes
- Smart word wrap (DEC private mode 7702):
  - Wraps at word boundaries instead of mid-word
  - Preserves leading indentation on wrapped lines
  - Auto-enabled by default, toggles on logical screen size change
- SGR rendering enhancements:
  - Italic text rendering (SGR 3/23)
  - Strikethrough (SGR 9/29)
  - Underline styles via subparameters: curly, dotted, dashed, double
  - Underline color support (SGR 58/59)
- BGP (Base Glyph Palette) SGR codes changed from 168/169 to 158/159
