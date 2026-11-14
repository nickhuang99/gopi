package lineedit

import (
	"strings"
	"unicode/utf8"

	"github.com/nickhuang99/gopi/internal/sgr"
)

// Screen is a minimal vt100-style cell grid: enough of the terminal's own
// model to assert what the editor actually DREW.
//
// It exists because of engineering rule 10.2 (rendering truth): a test that
// asserts "the redraw callback fired" cannot see a misplaced cursor, a stale
// line, or a buffer rendered at the wrong column. Feeding the bytes the editor
// wrote into a grid and reading the grid back is the same recipe the codex TUI
// uses (tests/suite/vt100_history.rs asserts `screen().contents()`); it needs
// no pty and no emulator dependency.
//
// It also models the terminal's SCROLLBACK — and that is not decoration. A cell
// grid that models a region scroll as "shift the region up and blank its last
// row" agrees with ANY layout by construction, so it cannot tell a layout that
// keeps its committed lines from one that destroys them. The emulator's real
// rule (measured on xterm 372, devdoc/chat-m2-streaming.md round 5) is:
//
//	A line is SAVED into the scrollback when and only when it is pushed off
//	the SCREEN's first row. Scrolling a region whose top margin is not the
//	screen's first row pushes lines off an interior margin — those lines are
//	DISCARDED, not saved.
//
// Modelling that rule is what makes the A5 assertions mean something: the tests
// read Scrollback() and can therefore fail when a layout re-inverts.
//
// Only the control sequences this editor emits are interpreted, plus the
// common cursor/erase ones a redraw might use: CUP (H/f), CUU/CUD/CUF/CUB
// (A/B/C/D), EL (K), ED (J), CR, LF, SGR (m) and private modes (?…) which are
// ignored. Anything else is skipped, never rendered as text.
//
// SGR is recorded per cell (screenattr.go): every cell carries the sgr.Style it
// was painted with, so a colour or emphasis change is assertable from a byte
// capture. The grid used to DROP SGR ("colour, not cells"), which made the one
// thing a colour layer is about — which cell is red — unverifiable.
//
// A WIDE rune (East Asian wide/fullwidth, sgr-independent: runeCells in
// render.go) occupies TWO cells: the wide rune itself in the first, a
// continuation marker (wideContinuation) in the second, both carrying the same
// style. The editor's cursor arithmetic already counts a CJK character as two
// columns (layoutText), so a grid that stored one cell per rune would report a
// cell for a column that does not exist and leave the painted column blank.
type Screen struct {
	Rows, Cols int
	cells      [][]rune
	// attrs is the per-cell SGR style of every cell — the colour and emphasis
	// it is painted with. It is a SECOND grid, parallel to cells, because a
	// terminal's cell owns exactly one style and because "column 45 of row 12
	// is red" has to be a fact the grid can answer.
	attrs [][]sgr.Style
	// pen is the SGR style in force: the style new cells are written with,
	// changed by CSI m — the same "current pen" a terminal keeps. An erased
	// cell loses its style (a blank cell carries no colour) while the pen
	// survives, which is how a terminal behaves.
	pen sgr.Style
	// attrParams is the scratch the SGR interpreter reads a sequence's
	// parameters from — a slice on the Screen, like indices, so feeding a
	// sequence allocates nothing.
	attrParams []int
	CurRow     int
	CurCol     int

	// top, bottom are the scrolling region's 0-based rows (inclusive). They
	// default to the whole screen; `CSI top;bottom r` narrows them, which is
	// how a real terminal confines scrolling — and what makes a cell grid
	// usable for asserting a layout that scrolls ONLY its own band (A5).
	top, bottom int

	// scroll is the saved-line history: every row pushed off the SCREEN's first
	// row, oldest first, exactly as a terminal keeps it. Its length is the
	// scrollback depth; its content is what the user can still reach.
	scroll []string
	// indices carries the parser's numeric CSI parameters between the first
	// parameter byte and the sequence's final byte — kept on the Screen (rather
	// than in a builder) so no allocation happens per control sequence.
	indices [maxCSIParams]int

	// pending carries the TAIL of a chunk whose last byte could not be
	// interpreted alone: a truncated escape sequence, or the first bytes of a
	// multi-byte rune whose continuation has not arrived. A terminal is a byte
	// STREAM, not a sequence of messages — the split point between two reads is
	// the OS's choice, never the producer's — so the tail is held until the rest
	// of it arrives instead of being dropped. Without this the same paint stream
	// reads differently depending on how the reader chunked it, which is exactly
	// the "the same bytes paint a different screen" defect this field removes.
	//
	// It is bounded (maxPendingEscape): a stream that never completes a sequence
	// must not grow the parser's state without limit (a hostile stream, or a
	// desynchronized one).
	pending []byte
}

// maxPendingEscape bounds the carry buffer: the longest sequence the parser can
// be inside is one escape (plus an OSC payload); anything past this is a stream
// the parser cannot resynchronize with, and holding more of it would only delay
// the next paint. Bytes beyond the bound are dropped rather than accumulated.
const maxPendingEscape = 512

// maxCSIParams is how many (semicolon-separated) numeric parameters a control
// sequence may carry before the extras are ignored. A terminal ignores what it
// does not understand; a bound is still needed so a hostile byte stream cannot
// grow the parser's state.
const maxCSIParams = 8

// NewScreen builds an empty grid of cols × rows cells filled with spaces.
func NewScreen(cols, rows int) *Screen {
	if cols <= 0 {
		cols = 1
	}
	if rows <= 0 {
		rows = 1
	}
	cells := make([][]rune, rows)
	attrs := make([][]sgr.Style, rows)
	for i := range cells {
		cells[i] = blankRow(cols)
		attrs[i] = blankAttrRow(cols)
	}
	return &Screen{
		Rows: rows, Cols: cols,
		cells:      cells,
		attrs:      attrs,
		pen:        sgr.Default(),
		attrParams: make([]int, 0, maxCSIParams),
		top:        0, bottom: rows - 1,
	}
}

// blankCellRune is the cell a blank row is made of. It is a named constant
// because a resize PADS with it: a zero rune would paint `\x00` cells (measured
// 2026-09-28 in the dump artifact before this was named).
const blankCellRune = ' '

// blankRow is one row of spaces — a fresh row is never nil, so rendering
// never has to nil-check a cell.
func blankRow(cols int) []rune {
	row := make([]rune, cols)
	for i := range row {
		row[i] = blankCellRune
	}
	return row
}

// Feed interprets one chunk of terminal output.
//
// A chunk boundary is an artifact of how the caller READ the stream, never a
// fact about the stream: the same bytes split at a different offset must paint
// the same screen. So a chunk that ends mid-sequence (a truncated escape, or the
// first bytes of a multi-byte rune) carries its tail into the next call rather
// than dropping it (see Screen.pending). Dropping it was measurable: the SAME
// `gopi chat` paint stream, replayed through this interpreter in 64-byte reads,
// lost the entire answer — the region's scroll pushed every answer row off the
// screen, and the read that should have written them had thrown away the escape
// that positioned the cursor, so 74 lines of scrollback became 0.
func (s *Screen) Feed(p []byte) {
	buf := p
	if len(s.pending) > 0 {
		buf = append(s.pending, p...)
		s.pending = nil
	}
	for len(buf) > 0 {
		b := buf[0]
		switch {
		case b == escByte:
			n := s.consumeEscape(buf[1:])
			if n < 0 {
				// Sequence still incomplete: the rest of it is not in this
				// chunk. HOLD the tail — the remainder arrives with the next
				// read, and dropping it here would render the following bytes
				// as text (or, worse, lose the positioning that puts the next
				// row where the layout meant it).
				s.holdPending(buf)
				return
			}
			buf = buf[1+n:]
		case b == '\r':
			s.CurCol = 0
			buf = buf[1:]
		case b == '\n':
			s.newline()
			buf = buf[1:]
		case b == '\b':
			if s.CurCol > 0 {
				s.CurCol--
			}
			buf = buf[1:]
		case b < printableMin:
			// Any other control byte has no cell meaning here.
			buf = buf[1:]
		default:
			if !canBeginRune(b) {
				// Not a truncated rune — a byte that can never BEGIN one: a lone
				// continuation byte (0x80..0xBF) with nothing in front of it, or
				// a lead past U+10FFFF (0xF5..0xFF). This is the one reading
				// holdPending cannot serve, and serving it is not a small
				// mistake — it is a LIVELOCK with a paint hole behind it. The
				// byte stays at the head of the held tail, so every following
				// Feed re-offers it, takes this same branch and re-holds: the
				// hold never completes, and every genuine paint byte queued
				// behind it (the rest of that frame — cursor moves, text, SGR)
				// is swallowed uninterpreted until the tail passes
				// maxPendingEscape and holdPending discards the whole state.
				// Measured 2026-09-28 (no pty): "\x80\x1b[1;1Hok" in ANY chunking
				// painted an EMPTY screen — the `ok` was never written — while
				// the same bytes with the stray 0x80 removed painted `ok`, and
				// the parser was still holding 9 bytes for the next read. The
				// byte is not part of the rune stream at all, so the parser
				// resynchronizes PAST it, exactly as a real terminal draws a
				// replacement glyph and continues with the next byte.
				// RED: TestScreenFeedByteThatCannotBeginARuneIsResynchronized
				// (+ ...UnstartableByteIsSkippedInTheSameChunk, ...ThenPaintIn-
				// ASeparateFeed, ...RepeatedNoiseDoesNotGrowTheHold).
				buf = buf[1:]
				continue
			}
			if !utf8.FullRune(buf) {
				// A truncated VALID prefix: this rune's own continuation bytes
				// are not in the chunk yet (0xE4 at a chunk end). Only this case
				// earns a hold — the bytes behind it are a rune, not noise, and
				// the next read completes it. FullRune is the criterion rather
				// than a size guess: false means the prefix is incomplete AND
				// could still become valid.
				s.holdPending(buf)
				return
			}
			r, size := utf8.DecodeRune(buf)
			s.put(r)
			buf = buf[size:]
		}
	}
}

// The UTF-8 byte ranges the standard draws (RFC 3629), named (Rule 1) because
// the RANGES are the criterion canBeginRune applies — a bare hex literal there
// would hide which of them is a lead and which is not. A string may not BEGIN
// with 10xxxxxx: a continuation byte arriving where a rune must start is a
// prefix of nothing, and no amount of waiting makes it one, which is the whole
// subject of the resynchronization canBeginRune enables.
const (
	utf8ContinuationMin = 0x80 // 10xxxxxx, first continuation byte
	utf8ContinuationMax = 0xBF // 10xxxxxx, last continuation byte
	// utf8InvalidLeadMin is the first lead the standard REFUSES: RFC 3629 caps
	// a rune at U+10FFFF, so 0xF5..0xFF encode no rune at all (the range needs
	// no upper constant — a byte cannot exceed 0xFF).
	utf8InvalidLeadMin = 0xF5
	utf8Lead2Min       = 0xC2 // 110xxxxx, the first lead that is not an over-long form
	utf8Lead4Max       = 0xF4 // 11110xxx, the last lead that stays under U+10FFFF
)

// canBeginRune reports whether b can be the FIRST byte of a UTF-8 sequence at
// all: ASCII, or a lead byte in the ranges the standard admits (0xC2..0xF4).
// Everything else — a continuation byte with nothing before it, an over-long
// lead, 0xF5..0xFF — begins no rune, ever.
//
// This is the criterion that separates the two very different readings of the
// tuple `utf8.DecodeRune` returns. (RuneError, size<=1) means one of two things:
// "the rest of this rune is in the next chunk" (hold it, as Feed has always
// done) or "this byte is not a rune and never becomes one" (skip it — holding
// it means holding it FOREVER). `utf8.FullRune` tells the two apart, and
// canBeginRune is why the second case has somewhere to go.
func canBeginRune(b byte) bool {
	switch {
	case b < utf8.RuneSelf:
		return true // 0x00..0x7F: ASCII, a complete rune by itself
	case b < utf8ContinuationMin:
		return false // no byte falls here; kept whole so the range pair reads as a pair
	case b <= utf8ContinuationMax:
		return false // 10xxxxxx: a continuation byte — a rune start it is not
	case b < utf8Lead2Min:
		return false // 0xC0, 0xC1: over-long two-byte forms, structurally invalid
	case b <= utf8Lead4Max:
		return true // 0xC2..0xF4: the lead byte of a real 2-, 3- or 4-byte rune
	case b >= utf8InvalidLeadMin:
		return false // 0xF5..0xFF: past U+10FFFF, never a lead byte
	default:
		return false // unreachable by construction; a byte begins no rune by default
	}
}

// holdPending keeps an un-interpretable tail for the next Feed.
//
// A tail that FITS the bound is held whole: the remaining bytes arrive with the
// next read, and dropping them here would render the following bytes as text.
//
// A tail PAST the bound is dropped WHOLE rather than truncated, and the
// direction is the whole point: truncation keeps the LAST maxPendingEscape bytes
// — precisely the half without the introducer — and the parser then re-offers
// the sequence's PARAMETERS as ordinary characters, so they paint as cells.
// Measured 2026-09-28 on the armed chat grid as the literal run
// `;61H24;73H5;230m` (three placed sequences whose every `ESC[` had been
// dropped); reproduced deterministically by
// screen_introducer_lost_test.go's over-long-hold leg and, on the pre-fix tree,
// by the pre-existing TestScreenReconcilesAfterAnOverlongHold. The parser may
// hold a partial sequence or discard it; it must never keep a fragment whose
// introducer it has thrown away — sgr.EscapeSeqLen can only do its job while the
// ESC is still there.
func (s *Screen) holdPending(tail []byte) {
	if len(tail) > maxPendingEscape {
		// A stream the parser cannot resynchronize with: end the held state
		// instead of re-offering a headless middle of it.
		s.pending = s.pending[:0]
		return
	}
	s.pending = append(s.pending[:0], tail...)
}

// escByte is the ESC control byte.
const escByte = 0x1b

// oscBEL is the BEL byte, the alternative terminator of an OSC string sequence
// (the other being ST, ESC \) — a terminal accepts either.
const oscBEL = 0x07

// oscSTFinal is the byte that completes a String Terminator (ESC <that byte>).
const oscSTFinal = '\\'

// printableMin is the first printable ASCII byte; everything below is a
// control byte.
const printableMin = 0x20

// consumeEscape interprets a control sequence whose ESC has already been
// consumed, and returns how many bytes of buf it used, or -1 when the sequence
// is not complete yet.
// dcsIntroducer is the second byte of a DCS (`ESC P`) — the introducer the
// XTGETTCAP capability query rides on, named here so the parser and its
// regression test cannot drift apart (Rule 1/13).
const dcsIntroducer = 'P'

func (s *Screen) consumeEscape(buf []byte) int {
	if len(buf) == 0 {
		return -1
	}
	if buf[0] == ']' {
		// OSC — Operating System Command. It is a STRING sequence: its payload
		// runs to a String Terminator (ESC \\) or to BEL, and NONE of it paints
		// a cell. This arm exists because the sequence is not hypothetical: a
		// bubbletea program asks the terminal for its background colour with
		// `ESC ] 11 ; ? ESC \` before it draws anything, and without this case
		// the parser took the `]` for a two-byte escape and rendered the
		// payload as TEXT ("1;?") at the top-left of the screen — measured in
		// the pty evidence run, where the header row read " opi ui …" because a
		// stray "1;?" had been written over its first four cells.
		return stringSeqLen(buf)
	}
	if buf[0] == dcsIntroducer {
		// DCS — Device Control String, and the SAME string framing as the OSC
		// above (payload up to ST or BEL, nothing of it paints).
		//
		// WHY THIS ARM EXISTS (measured 2026-10-03, task 01a0ff97). The chat
		// surface's own capability probe sends `ESC P + q <hex> ESC \`
		// (XTGETTCAP, sgr.TermcapQuerySeq) before it paints anything, and with
		// no DCS arm the parser read the `P` as a two-byte escape (ESC + one
		// byte) — so the payload `+q636F6C6F7273` (and the second query's
		// `+q524742`) painted into the grid as TEXT, and the payload's own ST
		// (`ESC \`) was read as the START of the next sequence, swallowing the
		// introducer of whatever followed it.
		//
		// What followed it is the layout's own `ESC[1;24r`. Swallowing that
		// `ESC` left the bytes `[1;24r` to be laid down as five printable
		// cells, so the scrolling region was NEVER INSTALLED. Everything the
		// chat surface commits into the answer band then flowed DOWNWARD
		// instead of scrolling inside the region: the startup screen (logo,
		// tips, "This session") was overwritten by the next band row, and
		// because `top` never became 0 the replaced rows reached neither the
		// grid nor the scrollback.
		//
		// MEASURED, same bytes, one difference:
		//
		//	feed as captured   : region=0..25  scrollback=0   bannerRow=-1
		//	feed minus the DCS : region=0..23  scrollback=30  "gopi v0.1.0" at scrollback[23]
		//
		// and the 30 bytes removed are exactly the two XTGETTCAP queries. The
		// artefact an agent read back therefore held a blank grid for rows
		// 0..22 while the operator's terminal had shown the startup screen in
		// full.
		return stringSeqLen(buf)
	}
	if buf[0] != '[' {
		// ESC + one byte (e.g. ESC ( B): consume the pair.
		if len(buf) < 2 {
			return -1
		}
		return 2
	}
	// CSI: an optional private prefix, numeric parameters, then a final byte in
	// 0x40..0x7e.
	//
	// The parameter scratch is cleared BEFORE parsing, never only after the
	// sequence completes: a sequence that arrives split across two reads is
	// parsed twice (the first attempt returns -1 and its bytes are re-offered
	// with the tail), so digits accumulated by the first attempt would be read
	// as if the sequence REPEATED them. Measured (2026-09-26): `ESC[1;3r` split
	// after `1;3` became `ESC[11;33r`, and a scroll region of rows 1..3 was
	// installed as rows 11..33 — a layout that then discarded the rows it was
	// meant to keep.
	for i := range s.indices {
		s.indices[i] = 0
	}
	i := 1
	if i < len(buf) && sgr.IsPrivatePrefix(buf[i]) {
		i++ // private / device-attribute prefix: parsed and ignored
	}
	n := 0
	for ; i < len(buf); i++ {
		c := buf[i]
		if c == sgr.ParamSeparator {
			if n < maxCSIParams {
				n++
			}
			continue
		}
		if c >= sgr.ParamDigitMin && c <= sgr.ParamDigitMax {
			if n < maxCSIParams {
				s.indices[n] = s.indices[n]*10 + int(c-sgr.ParamDigitMin)
			}
			continue
		}
		break
	}
	if i >= len(buf) {
		return -1 // no final byte yet
	}
	c := buf[i]
	if c < sgr.FinalByteMin || c > sgr.FinalByteMax {
		// A malformed parameter: the sequence ends here and means nothing.
		s.clearParams(n)
		return i + 1
	}
	s.controlSequence(n, c)
	s.clearParams(n)
	return i + 1
}

// stringSeqLen returns the byte length of a STRING control sequence whose
// introducer is `ESC X` at buf[0:2] — OSC (`ESC ]`) and DCS (`ESC P`) are the
// two the chat surface's own bytes carry. It returns -1 when the sequence is
// not complete yet (the caller buffers and retries).
//
// ONE reader for both introducers, because the framing is the protocol's and is
// identical: a payload runs to ST (`ESC \`) or BEL, and an ESC inside the
// payload that is not an ST ABORTS the string. The failure mode of getting this
// wrong is the same for both and is measured — see the DCS arm of consumeEscape
// for the defect that made this a shared reader rather than a second copy.
func stringSeqLen(buf []byte) int {
	for i := 2; i < len(buf); i++ {
		switch buf[i] {
		case oscBEL:
			return i + 1
		case sgr.Esc:
			if i+1 >= len(buf) {
				return -1 // whether this is an ST or an abort is not decidable yet
			}
			if buf[i+1] == oscSTFinal {
				return i + 2
			}
			// An ESC inside an OSC payload that is NOT the ST introducer ABORTS
			// the string: terminals abandon it there and read the ESC as the
			// start of the next sequence. Consuming the following byte instead
			// would eat the next sequence's own introducer and paint its digits
			// as text — measured: an unterminated `ESC]11;?` followed by
			// `ESC[1;1Hok` rendered the grid row as "1;1Hok".
			return i
		}
	}
	return -1
}

// clearParams resets the parser's parameter slots, so the next sequence never
// reads a leftover value.
func (s *Screen) clearParams(n int) {
	for i := 0; i <= n && i < maxCSIParams; i++ {
		s.indices[i] = 0
	}
}

// param returns the i-th parameter, or def when it was omitted or zero (the CSI
// convention).
func (s *Screen) param(i, def int) int {
	if i >= maxCSIParams || s.indices[i] == 0 {
		return def
	}
	return s.indices[i]
}

// paramOrZero returns the i-th parameter as written (0 when omitted) — for the
// sequences where 0 is a meaningful parameter, such as EL.
func (s *Screen) paramOrZero(i int) int {
	if i >= maxCSIParams {
		return 0
	}
	return s.indices[i]
}

// controlSequence applies one CSI sequence with n parsed parameters.
func (s *Screen) controlSequence(n int, final byte) {
	switch final {
	case sgr.FinalCUP, sgr.FinalCUPAlt:
		row, col := s.param(0, 1), s.param(1, 1)
		s.CurRow = clamp(row-1, 0, s.Rows-1)
		s.CurCol = clamp(col-1, 0, s.Cols-1)
	case sgr.FinalCUU:
		s.CurRow = clamp(s.CurRow-s.param(0, 1), 0, s.Rows-1)
	case sgr.FinalCUD:
		s.CurRow = clamp(s.CurRow+s.param(0, 1), 0, s.Rows-1)
	case sgr.FinalCUF:
		s.CurCol = clamp(s.CurCol+s.param(0, 1), 0, s.Cols-1)
	case sgr.FinalCUB:
		s.CurCol = clamp(s.CurCol-s.param(0, 1), 0, s.Cols-1)
	case sgr.FinalEL:
		s.eraseLine(s.paramOrZero(0))
	case sgr.FinalED:
		s.eraseDisplay(s.paramOrZero(0))
	case sgr.FinalDECSTBM:
		// DECSTBM: set the scrolling region (CSI top;bottom r). An omitted or
		// zero parameter means the screen's own edge, as a terminal does.
		top, bottom := 0, s.Rows-1
		if n >= 0 && s.indices[0] > 0 {
			top = s.indices[0] - 1
		}
		if n >= 1 && s.indices[1] > 0 {
			bottom = s.indices[1] - 1
		}
		s.setScrollRegion(top, bottom)
	case sgr.FinalSGR:
		s.sgr(n)
	}
}

// newline moves to the next row. Inside a scrolling region it scrolls ONLY the
// region when the cursor is on the region's last row (rows outside are
// untouched — that is the guarantee a scroll region buys); outside the region it
// moves down, scrolling the whole screen only on the screen's last row.
func (s *Screen) newline() {
	switch {
	case s.CurRow == s.bottom:
		s.scrollRows(s.top, s.bottom)
	case s.CurRow+1 >= s.Rows:
		s.scrollRows(0, s.Rows-1)
	case s.CurRow < s.Rows-1:
		s.CurRow++
	}
}

// scrollRows scrolls the rows [top, bottom] up by one, blanking the last. The
// row that leaves the top is SAVED into the scrollback — but only when that top
// IS the screen's first row: a line pushed off an interior scroll-region margin
// is discarded by a real terminal, and modelling anything else would make the
// A5 assertions unable to distinguish a layout that keeps its committed lines
// from one that destroys them.
func (s *Screen) scrollRows(top, bottom int) {
	if top < 0 || bottom >= s.Rows || bottom < top {
		return
	}
	if top == 0 {
		s.scroll = append(s.scroll, s.Line(0))
	}
	copy(s.cells[top:bottom], s.cells[top+1:bottom+1])
	copy(s.attrs[top:bottom], s.attrs[top+1:bottom+1])
	s.cells[bottom] = blankRow(s.Cols)
	s.attrs[bottom] = blankAttrRow(s.Cols)
}

// eraseDisplayAll is the EL/ED parameter meaning "the whole line/screen".
const eraseDisplayAll = 2

// eraseDisplay applies ED with its parameter: 0 = the cursor to the END of the
// screen, 1 = the START of the screen to the cursor, 2 = everything.
//
// Modes 0 and 1 are not decoration. A TUI uses them to wipe its own panel: the
// bubbletea renderer emits `CSI 29A CSI J` on every frame whose content grew
// shorter, and before this arm existed that sequence was a NO-OP in the grid —
// so the row the renderer had just wiped kept whatever the previous frame drew
// there. Measured in the pty evidence run: a single stray space survived on the
// header row and the dump read " opi ui → …" while the wire carried the correct
// frame.
//
// "The end of the screen" is the region the terminal would clear: from the
// cursor to the last row, so a partial line clears from the cursor's column and
// the rows below clear entirely.
func (s *Screen) eraseDisplay(mode int) {
	blank := func(row int) {
		s.cells[row] = blankRow(s.Cols)
		s.attrs[row] = blankAttrRow(s.Cols)
	}
	switch mode {
	case eraseDisplayAll:
		for i := range s.cells {
			blank(i)
		}
	case eraseToEnd:
		s.eraseLine(eraseToEnd)
		for r := s.CurRow + 1; r < s.Rows; r++ {
			blank(r)
		}
	case eraseToStart:
		s.eraseLine(eraseToStart)
		for r := 0; r < s.CurRow; r++ {
			blank(r)
		}
	}
}

// eraseToEnd / eraseToStart name EL's and ED's directional parameters, so the
// line and display erasers cannot disagree about what "0" and "1" mean.
const (
	// eraseToEnd clears from the cursor to the end of the line / screen.
	eraseToEnd = 0
	// eraseToStart clears from the start of the line / screen to the cursor.
	eraseToStart = 1
)

// eraseLine applies EL with its parameter: 0 = cursor to end, 1 = start to
// cursor, 2 = the whole line.
func (s *Screen) eraseLine(mode int) {
	switch mode {
	case eraseToEnd:
		for c := s.CurCol; c < s.Cols; c++ {
			s.cells[s.CurRow][c] = ' '
			s.attrs[s.CurRow][c] = sgr.Default()
		}
	case eraseToStart:
		for c := 0; c <= s.CurCol && c < s.Cols; c++ {
			s.cells[s.CurRow][c] = ' '
			s.attrs[s.CurRow][c] = sgr.Default()
		}
	default:
		s.cells[s.CurRow] = blankRow(s.Cols)
		s.attrs[s.CurRow] = blankAttrRow(s.Cols)
	}
}

// put renders one rune at the cursor and advances it, wrapping at the right edge
// as a terminal does. A WIDE rune advances and occupies two cells (both painted
// with the current pen) — one cell per rune would leave the column the user
// actually sees blank, and the cursor's own arithmetic (layoutText) already
// counts two.
func (s *Screen) put(r rune) {
	width := runeCells(r)
	if width > s.Cols {
		width = s.Cols // a wide rune never straddles the grid's edge
	}
	if width > 0 && s.CurCol+width > s.Cols {
		s.CurCol = 0
		s.newline()
	}
	s.cells[s.CurRow][s.CurCol] = r
	s.attrs[s.CurRow][s.CurCol] = s.pen
	for i := 1; i < width; i++ {
		s.cells[s.CurRow][s.CurCol+i] = wideContinuation
		s.attrs[s.CurRow][s.CurCol+i] = s.pen
	}
	s.CurCol += width
}

// setScrollRegion installs a scrolling region, clamped to the screen and kept
// sane when the parameters are inverted (a terminal refuses an invalid region;
// refusing here would silently ignore a bug in the sequence, so the clamp is
// deliberate and the region always covers at least one row).
func (s *Screen) setScrollRegion(top, bottom int) {
	top = clamp(top, 0, s.Rows-1)
	bottom = clamp(bottom, 0, s.Rows-1)
	if bottom < top {
		top, bottom = bottom, top
	}
	s.top, s.bottom = top, bottom
}

// Resize changes the grid's size while PRESERVING the parser's state — the
// installed scrolling region, the cursor, and the cells that still fit.
//
// WHY THIS EXISTS (measured 2026-09-28, task 01a0e433). A caller that only
// learned the real terminal size LATE used to have one way to move the grid:
// build a NEW screen. A fresh screen is blank AND, worse, it has no scrolling
// region installed — the region arrives in the paint stream as `CSI 1;24r`, and
// that sequence has already gone by. The result was silently catastrophic rather
// than merely empty: the layout paints the answer from a cursor at the band's
// bottom row (row 24 of 26, i.e. NOT the screen's last row), so every `\n` took
// the screen's own newline branch (`CurRow++` — the region no longer confined
// anything), and the answer crawled DOWN the screen until each new row landed on
// the row the last one occupied. Measured: an armed `gopi chat --dump-screen` at
// 117x26 whose artifact held the editing line and NOTHING of a 42-line answer,
// while the SAME bytes replayed on one uninterrupted screen carried the answer
// plus 47 lines of scrollback.
//
// WHAT IS PRESERVED, and why each one matters here:
//   - the scrolling region, re-clamped to the new height (dropping it is the
//     defect above);
//   - the cursor, because the painter keeps writing from where it was and every
//     subsequent absolute placement is expressed relative to the layout, not to
//     the grid's size;
//   - the cells and their styles, row by row, and the scrollback — the history a
//     resize in a real terminal also keeps.
//
// It is a NO-OP when the size is unchanged, so a caller can call it
// unconditionally.
func (s *Screen) Resize(cols, rows int) {
	if cols <= 0 || rows <= 0 || (cols == s.Cols && rows == s.Rows) {
		return
	}
	s.cells = resizeRows(s.cells, cols, rows, blankCellRune)
	s.attrs = resizeRows(s.attrs, cols, rows, sgr.Default())
	s.Cols = cols
	s.Rows = rows
	// The region is re-clamped rather than reset: a terminal that is re-sized
	// keeps its margins where they still fit (DECSTBM is only re-set when the
	// application sends it again), and dropping them here would re-create the
	// defect this method exists to remove.
	s.top = clamp(s.top, 0, rows-1)
	s.bottom = clamp(s.bottom, 0, rows-1)
	if s.bottom < s.top {
		s.top = s.bottom
	}
	s.CurRow = clamp(s.CurRow, 0, rows-1)
	s.CurCol = clamp(s.CurCol, 0, cols-1)
	// A pending partial sequence must not be re-offered headless: keeping it
	// would let it re-apply to the new grid, and DROPPING it here is what turns
	// a sequence's parameters into text — a read boundary that left the parser
	// holding a bare `ESC[` would lose the introducer while the parameters
	// arrived in the next write, and they then paint as cells (the observed
	// `;61H24;73H5;230m` run; see screen_introducer_lost_test.go).
	//
	// So the held tail is RE-INTERPRETED against the new grid instead of being
	// discarded. It cannot double-apply: a tail is held only when it draws
	// nothing yet (a truncated sequence, or a rune whose continuation has not
	// arrived), and re-feeding it either completes the sequence — whose cells
	// were never drawn — or holds it again. Recording a cursor placement a
	// moment late is harmless; losing it paints the next paint's parameters.
	pending := s.pending
	s.pending = s.pending[:0]
	if len(pending) > 0 {
		// After the geometry is measured, so a re-interpreted placement is
		// clamped to the grid it will actually draw into.
		defer s.Feed(pending)
	}
}

// resizeRows copies a row-major grid onto a differently sized one: rows that
// still fit keep their cells (truncated to the new width, padded when it grew),
// and the rows beyond the new height are dropped. A padded cell takes the
// caller's BLANK value, never a zero value — a zero rune is not a space, and a
// zero style is not "default" (both were measurable as `\x00` cells in the
// artifact before the blank was passed in explicitly).
func resizeRows[T any](src [][]T, cols, rows int, blank T) [][]T {
	dst := make([][]T, rows)
	for r := range dst {
		row := make([]T, cols)
		for c := range row {
			row[c] = blank
		}
		if r < len(src) {
			copy(row, src[r])
		}
		dst[r] = row
	}
	return dst
}

// ScrollRegion reports the installed scrolling region (0-based, inclusive) —
// tests assert that a layout actually confined its scrolling.
func (s *Screen) ScrollRegion() (top, bottom int) { return s.top, s.bottom }

// Scrollback returns the lines pushed off the SCREEN's first row, oldest first,
// trailing blanks trimmed. It is the saved history a user can scroll back to:
// the thing a "committed to scrollback" claim is about.
func (s *Screen) Scrollback() []string { return append([]string(nil), s.scroll...) }

// ScrollbackText returns the scrollback as newline-joined text.
func (s *Screen) ScrollbackText() string { return strings.Join(s.scroll, "\n") }

// MarkScrollback appends a marker line to the saved history. A test uses it to
// separate what a terminal would really have saved (an ATTESTED boundary) from
// what the layout under test discards — the difference between the two is
// exactly the defect class this modelling exists to expose.
func (s *Screen) MarkScrollback(marker string) { s.scroll = append(s.scroll, marker) }

// Line returns row (0-based) with trailing blanks trimmed — the row as a human
// reads it.
//
// A WIDE rune's continuation column is skipped: it is a column of the rune, not
// a character, so a row holding one 知 is the one-rune string "知" whether the
// grid stores one cell or (as it does) two.
func (s *Screen) Line(row int) string {
	if row < 0 || row >= s.Rows {
		return ""
	}
	var b strings.Builder
	b.Grow(s.Cols)
	for col := 0; col < s.Cols; col++ {
		if s.isContinuation(row, col) {
			continue
		}
		b.WriteRune(s.cells[row][col])
	}
	return strings.TrimRight(b.String(), " ")
}

// Contents returns every row, trailing blanks trimmed, newline-joined.
func (s *Screen) Contents() string {
	out := make([]string, s.Rows)
	for i := range out {
		out[i] = s.Line(i)
	}
	return strings.Join(out, "\n")
}

// clamp bounds v to [lo, hi].
//
// It is int-only on purpose: the grid's colours are sgr.Style values and the
// only values this file bounds are cursor rows/columns and erase modes, so this
// is the single operation the cell grid needs. A caller bounding a colour takes
// its range from internal/sgr.
func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// sgr interprets one SGR sequence (CSI … m) and leaves its style as the screen's
// pen, which every cell written from now on carries.
//
// n is the number of parameter separators the CSI parser counted, so the
// parameter count is n+1 — a bare `CSI m` is one parameter, SGR 0, exactly as a
// terminal reads it. The interpretation itself (which parameter means which
// attribute, how an extended colour consumes the parameters after it) lives in
// internal/sgr, so the grid and the code that EMITS SGR cannot disagree about
// the wire format (rules 13/14).
func (s *Screen) sgr(n int) {
	params := s.attrParams[:0]
	count := n + 1
	if count > maxCSIParams {
		count = maxCSIParams
	}
	for i := 0; i < count; i++ {
		params = append(params, s.indices[i])
	}
	for i := 0; i < len(params); {
		i = sgr.ApplyParam(&s.pen, params, i) + 1
	}
}
