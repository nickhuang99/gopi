// Package sgr is the terminal's Select Graphic Rendition vocabulary: the
// parameter numbers, the palette's named indices, the Style a cell carries, the
// sequence builders that emit them, and the parameter interpreter that reads
// them back.
//
// Why a package and not constants at each call site (rules 13 and 14): SGR is a
// PROTOCOL with two sides. The editor EMITS it (the dim/reset pair around a
// status line today, the colour layer a renderer draws with next) and the vt100
// test grid PARSES it (internal/lineedit/screen.go). A parameter number typed on
// both sides is two sources of truth for one wire format: the emitter writes 2
// and the parser reads 1, and nothing fails until a terminal shows the wrong
// colour. Here a parameter's number, its meaning, the sequence that carries it
// and the state it produces are one definition.
//
// The package is a leaf — stdlib only, no project imports — so any consumer may
// import it without a cycle.
package sgr

import (
	"fmt"
	"strconv"
	"strings"
)

// Control-sequence punctuation. The parser in internal/lineedit compares bytes
// against these; the builders below compose with CSI.
const (
	// CSI is the Control Sequence Introducer every SGR sequence starts with.
	CSI = "\x1b["
	// CSIStart is CSI's second byte — what the parser looks for to tell a CSI
	// sequence from a two-byte escape.
	CSIStart = '['
	// ESC is the escape byte every control sequence starts with (the project's
	// own name for it is internal/sgr's: `sgr.Esc` references THIS constant for
	// a parser walking raw bytes, and the sequence builders below compose from
	// it instead of typing the byte again).
	ESC = "\x1b"
	// SS3 is the Single Shift 3 introducer (`ESC O`), which the SS3 input forms
	// begin with: a terminal in APPLICATION cursor mode sends the arrow keys as
	// `ESC O A` rather than `ESC [ A`. It is a control-sequence introducer like
	// CSI and is named here for the same reason: one definition, referenced
	// everywhere a sequence is composed (Rule 13/14).
	//
	// ITS SPELLING IS COMPOSED, never typed (integration of
	// task/01a108aa67987887b369cc0d7a928f50, 2026-10-07). This constant
	// arrived on that branch as the literal "\x1bO" while master was
	// independently naming the SAME pair as ESC + SS3Selector, so the two
	// sides did not collide on a value — they collided on the spelling of a
	// value, which is precisely the "second spelling of the escape vocabulary"
	// the gate below exists to refuse. The resolution keeps BOTH: master's two
	// byte-level constants stay the source (a decoder like internal/lineedit's
	// walks raw bytes and needs them individually), and this one introducer is
	// DERIVED from them, so `sgr.SS3` can never drift from `ESC`/`SS3Selector`.
	SS3 = ESC + string(SS3Selector)
	// SS3Selector is the single character that turns ESC into SS3 ('ESC O'),
	// the introducer a terminal in APPLICATION cursor mode sends the arrow /
	// Home / End keys as. It is named here with the rest of the introducer
	// vocabulary so a decoder composes `ESC O` from two bytes we own rather
	// than spelling the pair itself (Rule 14).
	SS3Selector = 'O'
	// ParamSeparator separates a CSI sequence's numeric parameters.
	ParamSeparator = ';'
	// ParamDigitMin and ParamDigitMax bound a parameter's digits.
	ParamDigitMin = '0'
	ParamDigitMax = '9'
	// FinalByteMin and FinalByteMax bound a CSI sequence's final byte.
	FinalByteMin = 0x40
	FinalByteMax = 0x7e
)

// The final bytes of the CSI sequences this project emits or interprets: a
// sequence's last byte names its command, so it is a named constant rather than
// a literal in a parser switch (rule 1b).
const (
	// FinalSGR is the SGR command byte — "select graphic rendition".
	FinalSGR = 'm'
	// FinalCUP and FinalCUPAlt are the two accepted cursor-position forms.
	FinalCUP    = 'H'
	FinalCUPAlt = 'f'
	// FinalCUU/CUD/CUF/CUB move the cursor up/down/forward/back.
	FinalCUU = 'A'
	FinalCUD = 'B'
	FinalCUF = 'C'
	FinalCUB = 'D'
	// FinalEL erases in the line, FinalED in the display. Their parameter
	// (below) says how far the erase reaches.
	FinalEL = 'K'
	FinalED = 'J'
	// FinalDECSTBM sets the scrolling region (top;bottom).
	FinalDECSTBM = 'r'
	// FinalTilde terminates the function-key-shaped CSI sequences the
	// bracketed-paste framing uses (`CSI 200 ~` / `CSI 201 ~`). It is a CSI
	// final byte like the rest of this block — the terminal writes "~" there,
	// not a digit — so it is named with them rather than typed at the call site.
	FinalTilde = '~'
)

// PrivatePrefix is DEC's private-mode prefix: `CSI ? … h/l`.
const PrivatePrefix = '?'

// The final bytes of the private/device sequences this project emits whole:
// `CSI ? … h/l` (DEC private modes), and the area-erase pair. Named here for
// the same reason the public finals above are — one protocol fact, one
// definition — so a caller composing its own sequence has a byte to reference
// instead of a literal (Rule 1b).
const (
	// FinalPrivateMode is `h` (set) / `l` (reset) on a private DEC mode.
	FinalPrivateModeOn  = 'h'
	FinalPrivateModeOff = 'l'
)

// The remaining private and device-attribute prefixes the parser skips whole:
// kitty's `CSI < u`, DEC's `CSI = …`, and `CSI > 0 c`. Named together because
// they are one protocol fact — "this sequence is addressed to the terminal, not
// to the grid".
const (
	PrivatePrefixAlt     = '<'
	PrivatePrefixEquals  = '='
	PrivatePrefixGreater = '>'
)

// PrivatePrefixes is the three prefixes above as a string CUTSET, so a reader
// that has to SKIP them (the capability probe's answer parser, which meets
// replies it did not ask for) asks this package rather than spelling the set
// again — one protocol fact, one definition (Rule 14). It is derived from the
// bytes above, never typed: a fourth prefix added there joins this set by
// construction.
const PrivatePrefixes = string(PrivatePrefix) + string(PrivatePrefixAlt) +
	string(PrivatePrefixEquals) + string(PrivatePrefixGreater)

// The QUERY half of the protocol: sequences a program sends TO the terminal and
// the terminal answers. The answers are read back by internal/lineedit's
// capability probe, so the parameter numbers below are shared by the emitter and
// the reader (Rule 13/14) — and, more importantly, the FACT each query asks is
// documented where the query is composed rather than at the call site.
const (
	// FinalDSR is the Device Status Report final byte; with
	// DSRCursorPosition as its parameter it asks "where is the cursor".
	FinalDSR = 'n'
	// DSRCursorPosition is DSR's cursor-position parameter: `CSI 6 n`. Every
	// terminal that can report anything answers this one, which is what makes
	// it the probe's LIVENESS question (see lineedit/probe.go).
	DSRCursorPosition = 6
	// FinalDA1 is the Primary Device Attributes final byte: `CSI c`.
	FinalDA1 = 'c'
	// DA1PrivatePrefix is the prefix every real DA1 ANSWER carries: terminals
	// reply `CSI ? … c`, never bare `CSI … c`.
	DA1PrivatePrefix = PrivatePrefix
	// DAParamSixelGraphics / DAParamANSIcolours are DA1 parameters this project
	// interprets. THE TRAP THIS BLOCK EXISTS TO DOCUMENT: DA1's `4` reports
	// SIXEL GRAPHICS, not a colour depth — xterm's own ctlseqs(ms) lists DA1's
	// parameters (`1` 132-columns, `2` printer, `3` ReGIS, `4` Sixel, `6`
	// selective erase, `22` "ANSI color, e.g. VT525") and `4` is the graphics
	// one. A parser that read `4` as "16 colours" would downgrade every
	// 256-colour terminal that draws sixels. Depth is NOT in DA1 at all; it is
	// answered by XTGETTCAP (below).
	DAParamSixelGraphics = 4
	// DAParamANSIcolours is DA1 parameter 22 — the ONE DA1 parameter that
	// speaks about colour, and it says "ANSI colour exists", a FLOOR of 8/16
	// rather than a depth.
	DAParamANSIcolours = 22
)

// OSC (Operating System Command) vocabulary. An OSC is a STRING sequence —
// `ESC ]` … terminated by BEL or ST — which is why it needs its own introducer
// and terminator rather than the CSI punctuation above.
const (
	// OSC introduces an operating-system command string: `ESC ]`.
	OSC = "\x1b]"
	// OSCIntro is OSC's second byte, for a parser walking raw bytes.
	OSCIntro = ']'
	// OSCParamSeparator separates an OSC's numeric selector from its payload —
	// the same `;` character as ParamSeparator, named for this context.
	OSCParamSeparator = ";"
	// OSCQuery is the payload that turns an OSC into a QUESTION (`OSC 11 ; ?`).
	// Without it the sequence SETS the value instead of reporting it — the
	// difference between a probe and a side effect on the operator's terminal.
	OSCQuery = "?"
	// OSCStringTerminator (ST) ends an OSC: `ESC \`. BEL is the older,
	// universally accepted terminator, and both are answered in the wild, so
	// the probe composes with ST (7-bit, encodable in any locale) and the
	// answer parser accepts either (lineedit's parser: FCSTerminator).
	OSCStringTerminator = "\x1b\\"
	// OSCBackgroundColour is OSC 11, "report the background colour" — the
	// colour query a terminal answers with an `rgb:` triplet whose presence is
	// a POSITIVE truecolour signal.
	OSCBackgroundColour = 11
	// OSCColourPrefix is how a reported colour starts inside the answer's
	// payload: `rgb:RRRR/GGGG/BBBB`.
	OSCColourPrefix = "rgb:"
)

// DCS (Device Control String) vocabulary, used by XTGETTCAP — the ONLY query
// that answers the colour-DEPTH question.
//
// WHY XTGETTCAP AND NOT DA1 (the measurement that decided it). DA1 has no
// colour-depth parameter at all (see DAParamSixelGraphics above), and a probe
// that inferred depth from `TERM` would be back to trusting a declaration. The
// terminfo database's `colors` and `RGB` capabilities ARE the depth, and
// XTGETTCAP asks the terminal for a terminfo capability directly: xterm's
// ctlseqs(ms) documents `DCS + q Pt ST` with "a few more terminal capabilities
// are recognized, which are not names of special keys: Co for termcap colors,
// TN for termcap name, and RGB for the ncurses direct-color extension", and its
// answer `DCS 1 + r Pt ST` carries the value hex-encoded in the same form the
// query used. Terminal emulators that do not implement it answer nothing, which
// the probe treats as "unknown", never as "no colour".
const (
	// DCS introduces a device control string: `ESC P`. It is NOT an OSC that
	// happens to start differently — the two share only the ST terminator —
	// which is why the DCS vocabulary gets its own block rather than borrowing
	// OSC's introducer.
	DCS = "\x1bP"
	// DCSRequest is XTGETTCAP's request byte: `DCS + q …` — "what is this
	// terminfo capability".
	DCSRequest = 'q'
	// DCSPlus is the parameter that marks a DECRQSS/XTGETTCAP-style REQUEST
	// (as opposed to a response), the same `+` the answer's `+ r` echoes.
	DCSPlus = '+'
	// NOTE — XTGETTCAP has NO separator between `q` and its payload: the wire
	// form is `DCS + q Pt ST`, and a `;` inserted there (the shape ERASED from
	// this file after a real terminal refused to answer it) produces a request
	// no emulator recognises. The absence is documented rather than left to be
	// rediscovered, because `;` is the separator everywhere else in this
	// vocabulary.
	// DCSPayloadHexDigits is how many hex digits encode ONE character of a
	// DCS payload name (2 digits per character — the encoding XTGETTCAP
	// mandates for both its request and its answer).
	DCSPayloadHexDigits = 2
)

// The terminfo capability NAMES the depth question is asked with. They are the
// outside world's vocabulary (ncurses'), spelled once here because the probe's
// request and its answer parser must agree on them character for character.
const (
	// TerminfoColors is the `colors` capability: the number of colour entries
	// the terminal keeps — 8, 16, 256, or 16777216.
	TerminfoColors = "colors"
	// TerminfoRGB is the ncurses direct-colour capability: present (and
	// non-zero) exactly when the terminal can address colours as 24-bit RGB.
	TerminfoRGB = "RGB"
)

// CursorPositionQuery composes the liveness question — `CSI 6 n` (DSR 6) — and
// the answer is a Cursor Position Report (`CSI row ; col R`). It is the ONE
// query a probe can rely on to tell "this terminal answers" from "nothing will
// answer": every emulator, every multiplexer and every serial console that
// understands a cursor understands this, and nothing else is asked before it.
func CursorPositionQuery() string {
	return CSI + strconv.Itoa(DSRCursorPosition) + string(FinalDSR)
}

// DeviceAttributesQuery composes DA1 — `CSI c`. It is asked SECOND and its
// answer is read for the two parameters above ONLY (a reply carrying `22` says
// the terminal has ANSI colour at all; `4` says it can draw sixels and says
// nothing about colour). A reply is also the probe's second liveness witness.
func DeviceAttributesQuery() string { return CSI + string(FinalDA1) }

// BackgroundColourQuery composes `OSC 11 ; ? ST` — "what colour is your
// background". Its answer is the positive truecolour signal: an `rgb:` triplet
// with four hex digits per channel cannot be produced by a 16-colour terminal.
func BackgroundColourQuery() string {
	return OSC + strconv.Itoa(OSCBackgroundColour) + OSCParamSeparator + OSCQuery + OSCStringTerminator
}

// GetCapabilityQuery composes XTGETTCAP's request for ONE terminfo capability:
// `DCS + q <name in hex pairs> ST`.
func GetCapabilityQuery(name string) string {
	return DCS + string(DCSPlus) + string(DCSRequest) + hexEncode(name) + OSCStringTerminator
}

// CapabilityQueryNames lists the terminfo capabilities the depth question asks
// for, in the order the probe asks them.
func CapabilityQueryNames() []string { return []string{TerminfoColors, TerminfoRGB} }

// hexEncode renders s as XTGETTCAP's payload: two hex digits per character,
// upper-case (the form xterm answers in, so a captured answer and this request
// read the same way in a report).
func hexEncode(s string) string {
	const digitsPerByte = 2
	out := make([]byte, 0, len(s)*digitsPerByte)
	for i := 0; i < len(s); i++ {
		out = append(out, upperHex(s[i]>>4), upperHex(s[i]&0x0f))
	}
	return string(out)
}

// upperHex renders one nibble as its upper-case hex digit.
func upperHex(n byte) byte {
	if n < 10 {
		return '0' + n
	}
	return 'A' + (n - 10)
}

// The parameters the whole-screen and whole-line erases, the cursor-show mode
// and the bracketed-paste mode are composed from. A sequence is built from
// these rather than from typed digits, so "erase the whole screen" is one fact
// with one spelling (Rule 1b).
const (
	// EraseModeAll is the erase parameter that means "the whole line" / "the
	// whole display" (CSI 2K / CSI 2J). EraseModeToEnd is the default when the
	// parameter is absent (CSI K / CSI J).
	EraseModeAll   = 2
	EraseModeToEnd = 0

	// ModeCursorShow / ModeBracketedPaste are the DEC private modes the editor
	// sets and clears: `CSI ? 25 h` and `CSI ? 2004 h/l`.
	ModeCursorShow     = 25
	ModeBracketedPaste = 2004

	// ModeAltScreen is the DEC private mode that enters (1049) and leaves
	// (1049) the alternate screen buffer. A full-screen surface owns it: the
	// replay player enters it so the screen it restores at exit is the
	// operator's own, not the one it painted on.
	ModeAltScreen = 1049
)

// SS3Seq builds a single-shift-3 sequence from its final byte — `ESC O A` and
// friends, the APPLICATION-cursor-mode spelling of the arrow keys.
func SS3Seq(final byte) string { return SS3 + string(final) }

// AltScreenSeq enters (on) or leaves (off) the alternate screen buffer:
// `CSI ? 1049 h` / `CSI ? 1049 l`. It is the private-mode builder's sibling
// rather than a literal at each call site, so a surface that owns the buffer
// names the mode instead of spelling the escape (Rule 13/14).
func AltScreenSeq(on bool) string { return PrivateModeSeq(ModeAltScreen, on) }

// CursorHideSeq clears the cursor: `CSI ? 25 l`. It is the inverse of
// CursorShowSeq, and it exists so a caller that hides the cursor composes it
// through the same mode constant rather than by hand.
func CursorHideSeq() string { return PrivateModeSeq(ModeCursorShow, false) }

// CursorSeq builds an absolute cursor placement (CUP): `CSI row ; col H`. The
// arguments are 1-based, which is the wire's own numbering — the conversion
// from a 0-based screen index happens at the CALLER, once, so the off-by-one
// has one place to be wrong instead of every hand-composed sequence having its
// own (Rule 1b: a sequence is a value, not a spelling).
func CursorSeq(row, col int) string {
	return CSI + strconv.Itoa(row) + string(ParamSeparator) + strconv.Itoa(col) + string(FinalCUP)
}

// CursorHomeSeq parks the cursor at the screen's first cell (`CSI H`, the CUP
// with both parameters defaulted): the one place a caller that has just erased
// the display wants the cursor, so the next thing written — the shell's own
// prompt, a child process's output — starts at the top-left instead of where a
// previous owner left it.
func CursorHomeSeq() string { return CSI + string(FinalCUP) }

// EraseLineSeq clears the whole line (CSI 2K); EraseDisplaySeq the whole
// display (CSI 2J); EraseToEndLineSeq clears from the cursor to the line's end
// (CSI K), which is what a row repaint needs when the previous content was
// longer than the new one.
func EraseLineSeq() string      { return CSI + strconv.Itoa(EraseModeAll) + string(FinalEL) }
func EraseDisplaySeq() string   { return CSI + strconv.Itoa(EraseModeAll) + string(FinalED) }
func EraseToEndLineSeq() string { return CSI + strconv.Itoa(EraseModeToEnd) + string(FinalEL) }

// CursorShowSeq makes the hardware cursor visible again (CSI ? 25 h) — the
// editor hides it while it owns the screen and must not exit leaving it off.
func CursorShowSeq() string { return PrivateModeSeq(ModeCursorShow, true) }

// BracketedPasteSeq sets or clears bracketed paste (CSI ? 2004 h/l), so a
// pasted block arrives as one marked region instead of a burst of keystrokes.
func BracketedPasteSeq(on bool) string { return PrivateModeSeq(ModeBracketedPaste, on) }

// PasteMarkerStartParam / PasteMarkerEndParam select the two halves of the
// bracketed-paste FRAMING: `CSI 200 ~` opens a pasted block and `CSI 201 ~`
// closes it.
//
// The framing is not the mode: BracketedPasteSeq (`CSI ? 2004 h/l`) is what a
// program tells the TERMINAL ("I understand bracketed paste"), while these
// markers are what the terminal sends BACK around the pasted bytes. Both are
// named here because both are the terminal's wire vocabulary, and a reader that
// had only one of them would have to guess at the other's spelling.
const (
	PasteMarkerStartParam = 200
	PasteMarkerEndParam   = 201
)

// BracketedPasteMarkerSeq composes one half of the bracketed-paste framing:
// `CSI <param> ~`. It is the ONE producer of the marker's bytes — a caller that
// typed the sequence itself would be the second spelling of a wire fact
// (Rule 13/14), which is exactly what internal/sgr exists to prevent.
func BracketedPasteMarkerSeq(param int) string {
	return CSI + strconv.Itoa(param) + string(FinalTilde)
}

// PrivateModeSeq composes `CSI ? <mode> h|l`. One builder, because the h/l pair
// is one protocol fact and a caller that typed the final byte itself could
// invert the mode with a single-character slip.
func PrivateModeSeq(mode int, on bool) string {
	final := byte(FinalPrivateModeOff)
	if on {
		final = FinalPrivateModeOn
	}
	return CSI + string(PrivatePrefix) + strconv.Itoa(mode) + string(final)
}

// ScrollRegionSeq sets the scrolling region (DECSTBM): `CSI top ; bottom r`,
// 1-based rows taken as given. ResetScrollRegionSeq clears it (`CSI r`), which
// is what takes the terminal out of any region a caller installed.
func ScrollRegionSeq(top, bottom int) string {
	return CSI + strconv.Itoa(top) + string(ParamSeparator) + strconv.Itoa(bottom) + string(FinalDECSTBM)
}

func ResetScrollRegionSeq() string { return CSI + string(FinalDECSTBM) }

// MoveCursorSeq builds a RELATIVE cursor move (CUU/CUD/CUF/CUB) by direction
// byte: `CSI n A|B|C|D`. n <= 0 yields "" — a terminal treats a bare move as
// one cell, which is not what "move zero cells" means, so the caller gets an
// empty sequence to append rather than a surprise motion.
func MoveCursorSeq(dir byte, n int) string {
	if n <= 0 {
		return ""
	}
	return CSI + strconv.Itoa(n) + string(dir)
}

// IsPrivatePrefix reports whether b introduces a private or device-attribute
// sequence. One predicate rather than a byte range at each parser, because the
// set is the protocol's, not a caller's.
func IsPrivatePrefix(b byte) bool {
	switch b {
	case PrivatePrefix, PrivatePrefixAlt, PrivatePrefixEquals, PrivatePrefixGreater:
		return true
	}
	return false
}

// Intermediate bytes (0x20..0x2F) may appear inside a CSI sequence between its
// parameters and its final byte.
const (
	ParamIntermediateMin = 0x20
	ParamIntermediateMax = 0x2f
)

// Esc introduces every control sequence, and EscTwoByteMax is the length of the
// longest two-byte escape (ESC plus one final byte) — the span consumed when no
// CSI follows.
//
// A terminal draws NO cell for any byte between an introducer and its final
// byte, so a writer that measures or splits a row must skip the whole span:
// counting the ESC byte and its parameters as cells shifts every column after
// it, and splitting inside a sequence prints the sequence's own digits as text
// (measured on the reserved band: cutting `\x1b[31m` after `\x1b[` rendered
// the band's next row literally as `31mbbbb…`).
const (
	Esc           = '\x1b'
	EscTwoByteMax = 2
)

// EscapeSeqLen returns the byte length of the escape sequence starting at s[0],
// or 0 when s does not start with one. It is the ONE definition of "which bytes
// draw no cell" (Rule 14): the row cutters, the width measurers and the screen
// parser all ask it instead of each rescanning for ESC themselves.
//
// A TRUNCATED sequence (the text ends inside one) reports the length up to the
// end of s, so a caller treating the span as atomic keeps it whole rather than
// splitting it.
func EscapeSeqLen(s string) int {
	if len(s) == 0 || s[0] != Esc {
		return 0
	}
	if len(s) == 1 {
		return 1
	}
	if s[1] != CSIStart {
		return EscTwoByteMax
	}
	i := 2
	if i < len(s) && IsPrivatePrefix(s[i]) {
		i++
	}
	for ; i < len(s); i++ {
		c := s[i]
		if c == ParamSeparator || (c >= ParamDigitMin && c <= ParamDigitMax) ||
			(c >= ParamIntermediateMin && c <= ParamIntermediateMax) {
			continue
		}
		break
	}
	if i >= len(s) {
		return i // truncated: the whole remainder belongs to the sequence
	}
	// A final byte in range ends the sequence; a malformed one ends it anyway,
	// exactly as the screen parser treats it.
	return i + 1
}

// SGR parameter numbers — the vocabulary a `CSI … m` sequence is built from.
const (
	// Reset clears every attribute (SGR 0).
	Reset = 0
	// Bold, Dim, Italic, Underline and Reverse set the corresponding
	// intensity, font or video attribute.
	Bold      = 1
	Dim       = 2
	Italic    = 3
	Underline = 4
	Reverse   = 7

	// FgBase..FgLast are the eight named foreground colours (SGR 30-37);
	// BgBase..BgLast the same eight as backgrounds (SGR 40-47).
	FgBase = 30
	FgLast = 37
	BgBase = 40
	BgLast = 47

	// ExtendedFg and ExtendedBg introduce a colour that needs more parameters
	// than the base range carries: `38;5;n` (palette entry n) or `38;2;r;g;b`
	// (24-bit colour), with the 48 forms for backgrounds.
	ExtendedFg = 38
	ExtendedBg = 48

	// DefaultFg and DefaultBg restore the terminal's own default colour — the
	// pair that resets colour alone.
	DefaultFg = 39
	DefaultBg = 49

	// BrightFgBase..BrightFgLast are the eight bright foreground colours
	// (SGR 90-97); BrightBgBase..BrightBgLast the bright backgrounds
	// (SGR 100-107).
	BrightFgBase = 90
	BrightFgLast = 97
	BrightBgBase = 100
	BrightBgLast = 107
)

// Colour spaces and the selectors that choose between them.
const (
	// ColorModePalette selects a 256-colour palette entry: `38;5;n`.
	ColorModePalette = 5
	// ColorModeTruecolor selects a 24-bit colour: `38;2;r;g;b`.
	ColorModeTruecolor = 2

	// Palette16 is the size of the NAMED colour space: the eight base colours
	// plus their eight bright forms (indices 0..15). The rest of Palette256
	// has no name.
	Palette16 = 16
	// Palette256 is the size of the extended palette `38;5;n` addresses.
	Palette256 = 256
	// ChannelMax is the largest value of one truecolour channel.
	ChannelMax = 255
	// ColorBits is how many bits one truecolour channel occupies, so three of
	// them pack into 0xRRGGBB.
	ColorBits = 8
)

// The named palette indices (0..15). They are what "colour N" means: SGR 30+n
// selects index n and SGR 90+n index n+BrightOffset, so the two spellings
// collapse onto one index — which is exactly what makes them one colour.
const (
	Black = iota
	Red
	Green
	Yellow
	Blue
	Magenta
	Cyan
	White
)

// BrightOffset is the first bright palette index — the number of base colours
// below it.
const BrightOffset = Palette16 / 2

// The bright half of the named palette, indices 8..15, written as SGR 90-97
// (foreground) or 100-107 (background). A separate block so iota starts at
// BrightOffset rather than at 1.
const (
	BrightBlack = BrightOffset + iota
	BrightRed
	BrightGreen
	BrightYellow
	BrightBlue
	BrightMagenta
	BrightCyan
	BrightWhite
)

// DefaultColor is the "no colour" palette index: the cell paints with the
// terminal's own default. It is deliberately outside 0..Palette256-1 so it can
// never be confused with a real palette entry.
const DefaultColor = -1

// FgIndex classifies a foreground parameter: it returns the palette index p
// names and whether p names a colour at all. The two accepted spellings are the
// base range (30-37 → 0-7) and the bright range (90-97 → 8-15); a parameter
// outside both is not a colour and returns false.
func FgIndex(p int) (idx int, ok bool) {
	switch {
	case p >= FgBase && p <= FgLast:
		return p - FgBase, true
	case p >= BrightFgBase && p <= BrightFgLast:
		return p - BrightFgBase + BrightOffset, true
	}
	return DefaultColor, false
}

// BgIndex is FgIndex's background counterpart: 40-47 → 0-7, 100-107 → 8-15.
func BgIndex(p int) (idx int, ok bool) {
	switch {
	case p >= BgBase && p <= BgLast:
		return p - BgBase, true
	case p >= BrightBgBase && p <= BrightBgLast:
		return p - BrightBgBase + BrightOffset, true
	}
	return DefaultColor, false
}

// ----- Style: the state of one cell -------------------------------------

// NoRGB is the "no 24-bit colour" value. It is not a valid colour (a truecolour
// value is 0..0xffffff), so it can never be confused with a real one.
const NoRGB = -1

// rgbFieldPrefix introduces a truecolour value in a Style's text form; the value
// itself is rgbFormat so a colour always reads as six hex digits.
const (
	rgbFieldPrefix = "rgb:"
	rgbFormat      = "%06x"
)

// defaultStyleName is how an uncoloured style renders.
const defaultStyleName = "default"

// Style is the SGR state of one cell: the emphasis attributes plus its
// foreground and background colour. It is the ONE definition of "what colour is
// this cell" (rule 14) — the grid that records it, the parser that fills it and
// the test that asserts it all speak this type.
//
// A colour is either a PALETTE INDEX (Fg/Bg, DefaultColor for "the terminal's
// own default") or a 24-BIT value (FgRGB/BgRGB, NoRGB when unset). The two are
// exclusive: setting one clears the other, so a cell can never claim both a
// palette index and an RGB value it was not painted with.
type Style struct {
	Bold      bool
	Dim       bool
	Italic    bool
	Underline bool
	Reverse   bool

	Fg, Bg int // palette index 0..Palette256-1, or DefaultColor
	FgRGB  int // 0xRRGGBB, or NoRGB
	BgRGB  int
}

// Default returns the uncoloured style — what a cell nobody painted carries, and
// what SGR 0 restores.
func Default() Style {
	return Style{Fg: DefaultColor, Bg: DefaultColor, FgRGB: NoRGB, BgRGB: NoRGB}
}

// Equal reports whether two styles paint a cell identically.
func (s Style) Equal(o Style) bool {
	return s.Bold == o.Bold && s.Dim == o.Dim && s.Italic == o.Italic &&
		s.Underline == o.Underline && s.Reverse == o.Reverse &&
		s.Fg == o.Fg && s.Bg == o.Bg && s.FgRGB == o.FgRGB && s.BgRGB == o.BgRGB
}

// Fields returns the style's non-default parts as "name=value" words in a stable
// order: colour families first, then the emphasis attributes.
func (s Style) Fields() []string {
	var fields []string
	if s.FgRGB != NoRGB {
		fields = append(fields, "fg="+rgbFieldPrefix+fmt.Sprintf(rgbFormat, s.FgRGB))
	} else if s.Fg != DefaultColor {
		fields = append(fields, "fg="+ColorName(s.Fg))
	}
	if s.BgRGB != NoRGB {
		fields = append(fields, "bg="+rgbFieldPrefix+fmt.Sprintf(rgbFormat, s.BgRGB))
	} else if s.Bg != DefaultColor {
		fields = append(fields, "bg="+ColorName(s.Bg))
	}
	for _, e := range []struct {
		on   bool
		name string
	}{
		{s.Bold, "bold"},
		{s.Dim, "dim"},
		{s.Italic, "italic"},
		{s.Underline, "underline"},
		{s.Reverse, "reverse"},
	} {
		if e.on {
			fields = append(fields, e.name)
		}
	}
	return fields
}

// String renders the style for a test failure or a dump: "default" when nothing
// is set, otherwise its fields joined by spaces.
func (s Style) String() string {
	fields := s.Fields()
	if len(fields) == 0 {
		return defaultStyleName
	}
	return strings.Join(fields, " ")
}

// Params returns the SGR parameter list that reproduces this style EXACTLY,
// whatever state preceded it: a reset first, then one parameter group per
// attribute. Absolute by construction — a spec that set only the differences
// would inherit the previous style on the wire and assert the wrong thing.
func (s Style) Params() string {
	params := []string{ResetParams()}
	for _, e := range []struct {
		on bool
		fn func() string
	}{
		{s.Bold, BoldParams},
		{s.Dim, DimParams},
		{s.Italic, ItalicParams},
		{s.Underline, UnderlineParams},
		{s.Reverse, ReverseParams},
	} {
		if e.on {
			params = append(params, e.fn())
		}
	}
	params = append(params, fgParams(s), bgParams(s))
	return strings.Join(params, string(ParamSeparator))
}

// Seq returns Params as a complete sequence, ready to write to a terminal.
func (s Style) Seq() string { return Seq(s.Params()) }

// fgParams renders the foreground part of a style, including the default form.
func fgParams(s Style) string {
	switch {
	case s.FgRGB != NoRGB:
		return TruecolorFgParams(rgbRed(s.FgRGB), rgbGreen(s.FgRGB), rgbBlue(s.FgRGB))
	case s.Fg == DefaultColor:
		return strconv.Itoa(DefaultFg)
	default:
		return FgParams(s.Fg)
	}
}

// bgParams renders the background part of a style, including the default form.
func bgParams(s Style) string {
	switch {
	case s.BgRGB != NoRGB:
		return TruecolorBgParams(rgbRed(s.BgRGB), rgbGreen(s.BgRGB), rgbBlue(s.BgRGB))
	case s.Bg == DefaultColor:
		return strconv.Itoa(DefaultBg)
	default:
		return BgParams(s.Bg)
	}
}

// ----- emitting: parameters and sequences --------------------------------

// Seq builds a complete SGR sequence from its bare parameter list (e.g. "1;2"
// for bold+dim). Every builder here goes through it, so the sequence SHAPE lives
// in exactly one place.
func Seq(params string) string { return CSI + params + string(FinalSGR) }

// ResetSeq clears every attribute in force.
func ResetSeq() string { return Seq(ResetParams()) }

// ResetParams is the parameter list that clears every attribute — the leading
// element of any absolute specification.
func ResetParams() string { return strconv.Itoa(Reset) }

// BoldParams, DimParams, ItalicParams, UnderlineParams and ReverseParams are
// each one attribute's parameter, for a caller composing a multi-parameter
// sequence; the *Seq forms are the same value wrapped as a sequence.
func BoldParams() string      { return strconv.Itoa(Bold) }
func DimParams() string       { return strconv.Itoa(Dim) }
func ItalicParams() string    { return strconv.Itoa(Italic) }
func UnderlineParams() string { return strconv.Itoa(Underline) }
func ReverseParams() string   { return strconv.Itoa(Reverse) }

// BoldSeq, DimSeq, ItalicSeq, UnderlineSeq and ReverseSeq each turn on their one
// attribute.
func BoldSeq() string      { return Seq(BoldParams()) }
func DimSeq() string       { return Seq(DimParams()) }
func ItalicSeq() string    { return Seq(ItalicParams()) }
func UnderlineSeq() string { return Seq(UnderlineParams()) }
func ReverseSeq() string   { return Seq(ReverseParams()) }

// FgSeq selects palette index idx as the foreground; FgParams is the same choice
// as a bare parameter list.
func FgSeq(idx int) string { return Seq(FgParams(idx)) }

// FgParams returns the SGR parameters selecting palette index idx as the
// foreground: the short base form for 0-7, the bright form for 8-15, and
// `38;5;n` for anything else in the 256 palette. An index outside the palette is
// emitted as an extended colour rather than silently clamped — a consumer that
// reads it back rejects it there, which is honest.
func FgParams(idx int) string { return paletteParams(idx, FgBase, BrightFgBase, ExtendedFg) }

// BgSeq selects palette index idx as the background; BgParams is the bare
// parameter form.
func BgSeq(idx int) string { return Seq(BgParams(idx)) }

// BgParams is FgParams' background counterpart.
func BgParams(idx int) string { return paletteParams(idx, BgBase, BrightBgBase, ExtendedBg) }

// paletteParams picks the shortest faithful spelling of one palette index for the
// given colour family.
func paletteParams(idx, base, brightBase, extended int) string {
	switch {
	case idx >= 0 && idx < BrightOffset:
		return strconv.Itoa(base + idx)
	case idx >= BrightOffset && idx < Palette16:
		return strconv.Itoa(brightBase + idx - BrightOffset)
	default:
		return extendedColorParams(extended, ColorModePalette, idx)
	}
}

// TruecolorFgSeq selects a 24-bit foreground colour; TruecolorFgParams is the
// bare parameter form. Channels are clamped to a byte, since that is what the
// wire carries.
func TruecolorFgSeq(r, g, b int) string { return Seq(TruecolorFgParams(r, g, b)) }

// TruecolorFgParams renders `38;2;r;g;b` without the sequence wrapper.
func TruecolorFgParams(r, g, b int) string { return truecolorParams(ExtendedFg, r, g, b) }

// TruecolorBgSeq selects a 24-bit background colour; TruecolorBgParams is the
// bare parameter form.
func TruecolorBgSeq(r, g, b int) string { return Seq(TruecolorBgParams(r, g, b)) }

// TruecolorBgParams renders `48;2;r;g;b` without the sequence wrapper.
func TruecolorBgParams(r, g, b int) string { return truecolorParams(ExtendedBg, r, g, b) }

// truecolorParams renders the extended selector and its three channels.
func truecolorParams(extended, r, g, b int) string {
	return strconv.Itoa(extended) + string(ParamSeparator) +
		strconv.Itoa(ColorModeTruecolor) + string(ParamSeparator) +
		strconv.Itoa(clampChannel(r)) + string(ParamSeparator) +
		strconv.Itoa(clampChannel(g)) + string(ParamSeparator) +
		strconv.Itoa(clampChannel(b))
}

// extendedColorParams renders the extended selector and one palette index.
func extendedColorParams(extended, mode, n int) string {
	return strconv.Itoa(extended) + string(ParamSeparator) +
		strconv.Itoa(mode) + string(ParamSeparator) + strconv.Itoa(n)
}

// ColorName names a palette entry the way a person reads it: the sixteen named
// entries by name, DefaultColor as "default", anything else in the palette by
// number. It exists so a test failure or a dump says "red", not "1".
func ColorName(idx int) string {
	switch idx {
	case DefaultColor:
		return defaultStyleName
	case Black:
		return "black"
	case Red:
		return "red"
	case Green:
		return "green"
	case Yellow:
		return "yellow"
	case Blue:
		return "blue"
	case Magenta:
		return "magenta"
	case Cyan:
		return "cyan"
	case White:
		return "white"
	case BrightBlack:
		return "bright-black"
	case BrightRed:
		return "bright-red"
	case BrightGreen:
		return "bright-green"
	case BrightYellow:
		return "bright-yellow"
	case BrightBlue:
		return "bright-blue"
	case BrightMagenta:
		return "bright-magenta"
	case BrightCyan:
		return "bright-cyan"
	case BrightWhite:
		return "bright-white"
	}
	return colorNumberPrefix + strconv.Itoa(idx)
}

// colorNumberPrefix introduces a palette index that has no name.
const colorNumberPrefix = "colour-"

// ----- reading: a parameter list becomes a Style -------------------------

// ApplyParam applies one SGR parameter to a style, in the left-to-right order a
// terminal reads a `CSI … m` list: the emphasis attributes turn ON (only a reset
// clears them), a colour replaces the previous colour of its family, and Reset
// restores the default style.
//
// params is the FULL parameter list and i is the index being applied, because an
// extended colour is spread over several parameters: `38;5;n` and
// `38;2;r;g;b` are one colour carried by three or five of them. ApplyParam
// returns the index of the LAST parameter it consumed, so a caller walks the list
// with i = ApplyParam(...) + 1. A truncated extended colour consumes the rest of
// the list and changes nothing, rather than letting its digits be read as
// further parameters — `38;2;1;2` must not turn into "dim, bold".
func ApplyParam(style *Style, params []int, i int) int {
	if i < 0 || i >= len(params) {
		return i
	}
	p := params[i]
	switch {
	case p == Reset:
		*style = Default()
	case p == Bold:
		style.Bold = true
	case p == Dim:
		style.Dim = true
	case p == Italic:
		style.Italic = true
	case p == Underline:
		style.Underline = true
	case p == Reverse:
		style.Reverse = true
	case p == DefaultFg:
		style.Fg, style.FgRGB = DefaultColor, NoRGB
	case p == DefaultBg:
		style.Bg, style.BgRGB = DefaultColor, NoRGB
	case p == ExtendedFg, p == ExtendedBg:
		return applyExtended(style, params, i, p == ExtendedBg)
	default:
		if idx, ok := FgIndex(p); ok {
			style.Fg, style.FgRGB = idx, NoRGB
		} else if idx, ok := BgIndex(p); ok {
			style.Bg, style.BgRGB = idx, NoRGB
		}
		// Any other parameter is not one the grid models; a terminal ignores
		// what it does not know, so it is skipped rather than guessed at.
	}
	return i
}

// applyExtended reads the selector and payload of an extended colour starting
// after params[i] (which is ExtendedFg or ExtendedBg) and returns the index of
// the last parameter consumed. A truncated sequence consumes the remaining
// parameters and changes no colour: leaving them for the caller would make
// `38;2;1;2` read as "dim, bold", attributes no terminal ever applied.
func applyExtended(style *Style, params []int, i int, background bool) int {
	if i+1 >= len(params) {
		return len(params) - 1
	}
	switch params[i+1] {
	case ColorModePalette:
		if i+2 >= len(params) {
			return len(params) - 1
		}
		setColor(style, background, params[i+2], NoRGB)
		return i + 2
	case ColorModeTruecolor:
		if i+4 >= len(params) {
			return len(params) - 1
		}
		setColor(style, background, DefaultColor, RGB(params[i+2], params[i+3], params[i+4]))
		return i + 4
	}
	return i // an unknown selector names no colour
}

// setColor paints one colour family on a style. A truecolour value carries
// DefaultColor as its palette index, because the two forms are exclusive: a cell
// never keeps a stale palette index behind an RGB value, or the reverse.
func setColor(style *Style, background bool, idx, rgb int) {
	if rgb != NoRGB {
		idx = DefaultColor
	}
	if background {
		style.Bg, style.BgRGB = idx, rgb
		return
	}
	style.Fg, style.FgRGB = idx, rgb
}

// ----- packing a 24-bit colour ------------------------------------------

// rgbRedShift and rgbGreenShift are the bit positions of the red and green
// channels inside a packed 0xRRGGBB value; the blue channel needs no shift
// because it occupies the low ColorBits.
const (
	rgbRedShift   = 2 * ColorBits
	rgbGreenShift = 1 * ColorBits
)

// rgbRed, rgbGreen and rgbBlue extract one channel of a packed 0xRRGGBB value.
func rgbRed(v int) int   { return (v >> rgbRedShift) & ChannelMax }
func rgbGreen(v int) int { return (v >> rgbGreenShift) & ChannelMax }
func rgbBlue(v int) int  { return v & ChannelMax }

// RGB packs three channels into the 0xRRGGBB value a Style carries.
func RGB(r, g, b int) int {
	return clampChannel(r)<<rgbRedShift | clampChannel(g)<<rgbGreenShift | clampChannel(b)
}

// clampChannel bounds one colour channel to a byte.
func clampChannel(v int) int {
	switch {
	case v < 0:
		return 0
	case v > ChannelMax:
		return ChannelMax
	default:
		return v
	}
}
