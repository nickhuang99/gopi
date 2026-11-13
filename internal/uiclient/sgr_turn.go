package uiclient

// sgr_turn.go — WHERE one turn's painting BEGINS inside a recorded stream.
//
// THE PROBLEM. `/screen 58` has to hand a reader the screen as it stood at turn
// 58, and a stream is a flat sequence of frames: nothing in the format says where
// a turn starts. A byte offset chosen by hand is not an answer — the stream grows
// while a run lives, so "the last 64 KiB" describes whatever happens to be newest,
// which is how a capture filed as turn 58 came to hold turn 59's screen.
//
// THE ANCHOR IS THE RENDERER'S OWN MARK, NOT A COPY OF IT. The chat renderer
// paints a between-turns rule above every answer (`internal/lineedit`'s
// TurnHeader): `<rule run> turn N · <prompt>`. This file does not spell that shape
// — it ASKS the renderer for it (lineedit.TurnHeader with an empty prompt, at a
// width wide enough that no degradation path applies) and derives the STEM that
// precedes the number by taking the common prefix of two rendered headers. A
// change to the glyph, the spacing or the word reaches this locator without an
// edit here, and there is no second definition to drift (Rule 14).
//
// WHY THE WHOLE HEADER AND NOT JUST `turn 58`. A turn's PROMPT is the operator's
// text, and a prompt may contain the word "turn 58" (this very epic's briefings
// do). A bare `turn <digits>` match therefore lands inside a prompt, inside a tool
// row, or inside an answer. The rule run + spacing + `turn ` is what the renderer
// draws and what prose cannot forge — a bare `turn <digits>` is a substring of
// ordinary text, and this repository's own briefings contain it.
//
// COUNT THEM YOURSELF RATHER THAN TRUSTING A NUMBER HERE: this file used to state
// how many times `turn 58` occurred on the reference recording, and that figure was
// wrong within an hour (the recording was still being written). A count in a comment
// is a claim a later reader acts on, and it rots — the same reason the two false
// positive shapes below are recorded WITH their gates rather than as anecdotes.
//
// WHY NOT THE CURSOR POSITION (`\x1b[22;1H`). That row number is a function of the
// grid and of where the band sits, so it is a fact about one terminal, not about
// the format: it changes with the window height, and an answer painted above a
// scrolled band lands elsewhere. The rule+label is drawn identically at every
// width (that is what TurnHeader's degradation order guarantees).
//
// A FRAME THAT PAINTS A LOWER NUMBER IS NOT A BOUNDARY. Repaints re-emit older
// rows (a scroll, a resize, the band's own rewrite). So the walk keeps a running
// maximum and opens a new span only when a frame carries a number strictly ABOVE
// it: the boundary is the FIRST frame of that turn, and every later frame naming
// an older turn is chrome.
//
// THIS RULE IS DEFENSIVE ON THE RECORDING THAT EXISTS, and saying so is the honest
// version: on session e68fddf5 every header frame carries a strictly greater number
// than the one before (14 frames, 14 turns, zero repeats), so the running maximum
// never fires there. It is kept because `Span(n)` must return the FIRST frame that
// painted turn n — a contract, not an observation. What it must NOT be read as is
// "repaints are handled because they were seen": they were not seen.

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/nick/gopi/internal/lineedit"
)

const (
	// sgrTurnProbeCols is the width a probe header is rendered at. It is wide
	// enough that EVERY degradation path in lineedit.TurnHeader is skipped (the
	// whole rule, both spacings and the label fit), so the stem below is the
	// undamaged shape rather than some narrow-terminal spelling.
	sgrTurnProbeCols = 4096
	// sgrTurnAnchorProbeTurn is the turn number the derived stem is VERIFIED
	// against: a number whose digits the renderer must reproduce verbatim right
	// after the stem. It exists so a header shape the stem derivation cannot
	// describe is REFUSED here instead of silently matching nothing in a stream.
	sgrTurnAnchorProbeTurn = 12345
	// sgrTurnAnchorMaxDigits bounds the digits a turn number may occupy while
	// scanning. It is the carry the chunked scan keeps so an anchor split across
	// two reads is still found; a longer run is not a turn number this locator
	// accepts (a recording cannot reach 10^9 turns), and treating it as one would
	// mean buffering without a bound.
	sgrTurnAnchorMaxDigits = 9
	// sgrTurnScanChunk is the read size a frame's payload is scanned in. Frames
	// are usually a few hundred bytes, so this costs one read for almost every
	// frame and bounds memory for the rare large repaint.
	sgrTurnScanChunk = 8 << 10
	// sgrTurnLabelTailByte is the byte the renderer writes immediately after the
	// turn number — the spacing before the separator (lineedit's turnLabelTail).
	// It is compared to terminate the digit run, so `turn 58` cannot be read out
	// of `turn 580`.
	sgrTurnLabelTailByte = ' '
)

// SgrTurnSpan is one turn's frame span inside a stream: [StartFrame, EndFrame).
//
// EndFrame is EXCLUSIVE and is the next located turn's StartFrame, or the frame
// count for the newest turn — a turn's painting runs until the next header, which
// is exactly what the operator saw while that turn was live.
type SgrTurnSpan struct {
	// Turn is the number the renderer printed in the header.
	Turn int
	// StartFrame is the index of the frame that painted the header.
	StartFrame int
	// EndFrame is one past this turn's last frame.
	EndFrame int
}

// Frames reports how many frames the span holds.
func (s SgrTurnSpan) Frames() int { return s.EndFrame - s.StartFrame }

// SgrTurns is every turn boundary a recording carries, plus the stream's own
// totals so a caller can state what it is looking at.
type SgrTurns struct {
	// Meta is the stream's header as the shared decoder read it.
	Meta SgrStreamMeta
	// StreamFrames / StreamBytes are the whole recording's totals.
	StreamFrames int
	StreamBytes  int64
	// Trailer reports whether the source reached its trailer (a run still writing
	// has not, and a caller must be able to say which it holds).
	Trailer bool
	// Spans is every located turn, in stream order, ascending by Turn.
	Spans []SgrTurnSpan
}

// Current returns the NEWEST turn the recording holds — "the screen as it stands"
// for a `/screen` typed with no argument, because the last painting on the screen
// belongs to the turn that just finished.
func (t SgrTurns) Current() (SgrTurnSpan, bool) {
	if len(t.Spans) == 0 {
		return SgrTurnSpan{}, false
	}
	return t.Spans[len(t.Spans)-1], true
}

// Span returns the span for an exact turn number.
func (t SgrTurns) Span(turn int) (SgrTurnSpan, bool) {
	for _, s := range t.Spans {
		if s.Turn == turn {
			return s, true
		}
	}
	return SgrTurnSpan{}, false
}

// RangeWords describes the turns this recording carries, for a refusal that has
// to tell the caller what WOULD have worked.
func (t SgrTurns) RangeWords() string {
	if len(t.Spans) == 0 {
		return "none"
	}
	return strconv.Itoa(t.Spans[0].Turn) + ".." + strconv.Itoa(t.Spans[len(t.Spans)-1].Turn)
}

// sgrTurnAnchorStem derives the bytes that precede a turn number in the renderer's
// header, by asking the renderer itself.
//
// TWO RENDERED HEADERS, ONE COMMON PREFIX. TurnHeader(0) and TurnHeader(1) differ
// only in the digit, so their common prefix is everything the header draws before
// the number — the rule run, its spacing and the word. The third render, of a
// number with digits the first two do not share (12345), VERIFIES that the prefix
// is followed by the digits verbatim: a header shape this derivation cannot
// describe (a rule dropped, the number elided) is refused by name rather than
// silently matching nothing (Rule 15).
func sgrTurnAnchorStem() (string, error) {
	zero := lineedit.TurnHeader(lineedit.TurnInfo{Index: 0}, sgrTurnProbeCols)
	one := lineedit.TurnHeader(lineedit.TurnInfo{Index: 1}, sgrTurnProbeCols)
	if zero == "" || one == "" {
		return "", fmt.Errorf("uiclient: the renderer drew no turn header at width %d, so a turn boundary cannot be located", sgrTurnProbeCols)
	}
	i := 0
	for i < len(zero) && i < len(one) && zero[i] == one[i] {
		i++
	}
	stem := zero[:i]
	probe := lineedit.TurnHeader(lineedit.TurnInfo{Index: sgrTurnAnchorProbeTurn}, sgrTurnProbeCols)
	digits := strconv.Itoa(sgrTurnAnchorProbeTurn)
	if !strings.HasPrefix(probe, stem) || !strings.HasPrefix(probe[len(stem):], digits) {
		return "", fmt.Errorf("uiclient: the renderer's turn header %q is not %q followed by the turn number — a boundary cannot be located from it", probe, stem)
	}
	return stem, nil
}

// LocateSgrTurns walks a recording and reports where each turn's painting begins.
//
// NOTHING IS COPIED HERE: the walk records frame positions (one pass over the
// record lines, no payload read) and then scans each payload only for the anchor.
// A recording with no header at all is NOT an error — it is a fact the caller must
// state (an old stream, or a session that never painted a numbered turn), and this
// function reports it as an EMPTY span list so the refusal is written where the
// user is, with the turn numbers that would have worked.
func LocateSgrTurns(path string) (SgrTurns, error) {
	var out SgrTurns
	f, err := os.Open(path)
	if err != nil {
		return out, fmt.Errorf("uiclient: cannot open the SGR stream %s: %w", path, err)
	}
	defer f.Close() //stylegate:ignore a read-only open; the close cannot change the walk's result
	st, err := f.Stat()
	if err != nil {
		return out, fmt.Errorf("uiclient: cannot measure the SGR stream %s: %w", path, err)
	}
	meta, marks, payload, trailer, err := walkSgrMarks(f, st.Size(), path)
	if err != nil {
		return out, err
	}
	out.Meta = meta
	out.StreamFrames = len(marks)
	out.StreamBytes = payload
	out.Trailer = trailer
	if len(marks) == 0 {
		return out, nil
	}
	stem, err := sgrTurnAnchorStem()
	if err != nil {
		return out, err
	}
	// THE RUNNING MAXIMUM IS THE BOUNDARY RULE (see the file header): a frame
	// naming a turn at or below it is a repaint of chrome, never a new turn.
	high := 0
	for idx, m := range marks {
		turn, terr := sgrFrameNewestTurn(f, m, stem)
		if terr != nil {
			return out, terr
		}
		if turn <= high {
			continue
		}
		if len(out.Spans) > 0 {
			out.Spans[len(out.Spans)-1].EndFrame = idx
		}
		out.Spans = append(out.Spans, SgrTurnSpan{Turn: turn, StartFrame: idx, EndFrame: len(marks)})
		high = turn
	}
	return out, nil
}

// sgrFrameNewestTurn reports the highest turn number the frame's payload paints,
// or 0 when it paints none.
//
// THE STEM ALONE IS NOT ENOUGH, AND THAT IS MEASURED (session e68fddf5, 55377
// frames). A recording is full of text that QUOTES an old screen: a briefing, a
// tool result or an answer that pastes `──── turn 58 · …` is bytes the renderer
// painted, and a matcher that keys on the stem alone reports a boundary inside
// prose. Two conditions — both observed to be necessary on that stream, and both
// about the CURSOR rather than the glyphs — separate a real header from a copy:
//
//  1. THE STEM MUST FOLLOW A REAL CURSOR-POSITION SEQUENCE. The renderer positions
//     the cursor (ESC `[<row>;<col>H`) and THEN draws the header, so the header is
//     the first thing drawn at that position. A quoted copy is drawn after other
//     text, so the bytes in front of it are that text.
//  2. THAT SEQUENCE MUST BEGIN WITH A REAL ESC BYTE (0x1b), NOT THE FOUR
//     CHARACTERS `\x1b`. The second-level copy measured in that stream is a frame
//     that prints the ESCAPE'S OWN SPELLING: it carries a genuine CUP and then the
//     literal text `\x1b[22;1H` in front of the header — so a scan that accepted
//     the text form as a cursor move would report the copy as a boundary.
//
// The running-maximum rule above is NOT a substitute for these: on that stream it
// hides the copies only because they happen to be painted after the turn they
// quote. A copy that arrives before the next real header would open a span, and
// `/screen N` would then cut a paragraph of prose.
func sgrFrameNewestTurn(ra io.ReaderAt, m SgrFrameMark, stem string) (int, error) {
	if m.Bytes <= 0 {
		return 0, nil
	}
	needle := []byte(stem)
	keep := len(needle) + sgrTurnAnchorMaxDigits + 1 + sgrTurnCursorMaxLen
	var carry []byte
	newest := 0
	off := m.Offset
	rem := m.Bytes
	for rem > 0 {
		n := sgrTurnScanChunk
		if rem < n {
			n = rem
		}
		buf := make([]byte, n)
		if _, rerr := ra.ReadAt(buf, off); rerr != nil && rerr != io.EOF {
			return 0, fmt.Errorf("uiclient: cannot read a %d-byte frame at offset %d of the SGR stream: %w", m.Bytes, m.Offset, rerr)
		}
		window := append(carry, buf...)
		for start := 0; ; {
			i := strings.Index(string(window[start:]), stem)
			if i < 0 {
				break
			}
			if sgrTurnHeaderFollowsCursor(window, start+i) {
				at := start + i + len(needle)
				if turn, ok := sgrTurnDigitsAt(window, at); ok && turn > newest {
					newest = turn
				}
			}
			start += i + len(needle)
			if start >= len(window) {
				break
			}
		}
		carry = window
		if len(carry) > keep {
			carry = carry[len(carry)-keep:]
		}
		off += int64(n)
		rem -= n
	}
	return newest, nil
}

// sgrTurnDigitsAt parses the turn number that starts at window[at], and reports
// whether it is one: at least one digit, at most sgrTurnAnchorMaxDigits of them,
// terminated by the header's own spacing. A run terminated by anything else is
// prose that happens to follow the stem, not the renderer's number.
func sgrTurnDigitsAt(window []byte, at int) (int, bool) {
	end := at
	for end < len(window) && end-at < sgrTurnAnchorMaxDigits && window[end] >= '0' && window[end] <= '9' {
		end++
	}
	if end == at {
		return 0, false
	}
	if end < len(window) && window[end] != sgrTurnLabelTailByte {
		return 0, false
	}
	n, err := strconv.Atoi(string(window[at:end]))
	if err != nil {
		return 0, false
	}
	return n, true
}

// ReadSgrTurnFile copies exactly the frames of one span into a range value.
//
// IT IS A SECOND PASS ON PURPOSE: locating reads no payload, and copying needs
// nothing but the positions the locator already found, so a caller that wants one
// turn does not pay for a copy of the others (a session's stream runs to
// megabytes; one turn's screen is one band).
func ReadSgrTurnFile(path string, span SgrTurnSpan) (SgrRange, [][]byte, error) {
	var rng SgrRange
	if span.EndFrame <= span.StartFrame {
		return rng, nil, fmt.Errorf("uiclient: turn %d's span is empty (frames %d..%d)", span.Turn, span.StartFrame, span.EndFrame)
	}
	f, err := os.Open(path)
	if err != nil {
		return rng, nil, fmt.Errorf("uiclient: cannot open the SGR stream %s: %w", path, err)
	}
	defer f.Close() //stylegate:ignore a read-only open; the close cannot change the selection
	st, err := f.Stat()
	if err != nil {
		return rng, nil, fmt.Errorf("uiclient: cannot measure the SGR stream %s: %w", path, err)
	}
	meta, marks, payload, trailer, err := walkSgrMarks(f, st.Size(), path)
	if err != nil {
		return rng, nil, err
	}
	if span.StartFrame >= len(marks) {
		return rng, nil, fmt.Errorf("uiclient: turn %d starts at frame %d but the stream holds %d frame(s)", span.Turn, span.StartFrame, len(marks))
	}
	end := span.EndFrame
	if end > len(marks) {
		end = len(marks)
	}
	rng.Meta = meta
	rng.StreamFrames = len(marks)
	rng.StreamBytes = payload
	rng.SourceTrailer = trailer
	rng.StartFrame = span.StartFrame
	frames := make([][]byte, 0, end-span.StartFrame)
	var kept int64
	for _, m := range marks[span.StartFrame:end] {
		buf := make([]byte, m.Bytes)
		if m.Bytes > 0 {
			if _, rerr := f.ReadAt(buf, m.Offset); rerr != nil && rerr != io.EOF {
				return rng, nil, fmt.Errorf("uiclient: cannot read a %d-byte frame at offset %d of the SGR stream: %w", m.Bytes, m.Offset, rerr)
			}
		}
		frames = append(frames, buf)
		kept += int64(m.Bytes)
	}
	rng.Frames = len(frames)
	rng.Bytes = kept
	// A span that does not start at frame 0 HAS older frames before it, and saying
	// so is what stops a reader treating a turn as the whole session (Rule 15).
	rng.Truncated = span.StartFrame > 0
	return rng, frames, nil
}

// walkSgrMarks records every frame's position in a stream without reading a
// single payload byte, and decodes the header (and the trailer, when the run
// closed). It is the ONE walk both the locator and the copier use: a second
// spelling of "where does frame N start" is how a copy drifts one frame from the
// span it was given (Rule 14).
//
// TOLERANCE MATCHES THE REST OF THE FORMAT: a stream cut mid-record yields the
// frames it completed and NO error (a recording is being appended to while it is
// read), while a missing header or an unknown version is an error — truncation is
// a state of the world, a foreign file is a different file.
//
// AND A FILE THAT CONTRADICTS ITSELF IS A THIRD THING, refused here so no caller
// can mistake it for a reading (sgrstream_integrity.go): a trailer with data after
// it, or a complete record line that is neither a trailer nor a frame length, is
// the FILE's defect and is named with its offset and size. Before that existed the
// walk stopped at the first trailer — correct for a closed stream, and wrong for a
// path two writers had touched: the truncating writer's empty trailer came first,
// the original writer's frames kept arriving behind a NUL hole, and the locator
// reported zero turns over 38 MB of painted bytes. Callers then read "zero turns"
// as "the anchor does not match the renderer" and went looking for a code defect.
func walkSgrMarks(ra io.ReaderAt, size int64, src string) (SgrStreamMeta, []SgrFrameMark, int64, bool, error) {
	var meta SgrStreamMeta
	if size <= 0 {
		return meta, nil, 0, false, fmt.Errorf("uiclient: the SGR stream is empty (no header to read)")
	}
	head, next, err := readSgrRecordAt(ra, 0, size)
	if err != nil {
		return meta, nil, 0, false, fmt.Errorf("uiclient: SGR stream has no readable header: %w", err)
	}
	meta, err = readSgrHeader(head)
	if err != nil {
		return meta, nil, 0, false, fmt.Errorf("uiclient: %w", err)
	}
	pos := next
	var marks []SgrFrameMark
	var payload int64
	trailer := false
	for pos < size {
		line, after, lerr := readSgrRecordAt(ra, pos, size)
		if lerr != nil {
			break
		}
		if IsSgrStreamTrailerLine(line) {
			if terr := readSgrTrailer(line, &meta); terr != nil {
				return meta, nil, 0, false, fmt.Errorf("uiclient: %w", terr)
			}
			trailer = true
			// A TRAILER IS NOT THE END OF THE FILE, AND THE FILE SAYS SO HERE.
			// The reading is arithmetic on the size the OS reported, not a count
			// the writer declared, so a second writer cannot make it agree.
			finding, ierr := sgrAfterTrailerFinding(ra, after, size, meta)
			if ierr != nil {
				return meta, nil, 0, false, ierr
			}
			var findings []SgrStreamContradiction
			if finding != nil {
				findings = append(findings, *finding)
			}
			if counted := sgrCountMismatchFinding(pos, meta, len(marks), payload); counted != nil {
				findings = append(findings, *counted)
			}
			if len(findings) > 0 {
				return meta, nil, 0, false, &SgrStreamContradictionError{Path: src, Size: size, Findings: findings}
			}
			break
		}
		n, cerr := strconv.Atoi(line)
		if cerr != nil || n < 0 || n > sgrStreamLenPrefixMax {
			// A COMPLETE line that is neither a record nor a length is damage,
			// not a length complaint: it is the sparse hole met from inside the
			// frame area, or bytes a second writer put there. Naming it
			// "frame prefix %q is not a length" described the prefix grammar and
			// said nothing about the file being in two pieces.
			bad := sgrBadRecordFinding(pos, line)
			return meta, nil, 0, false, &SgrStreamContradictionError{Path: src, Size: size, Findings: []SgrStreamContradiction{bad}}
		}
		if after+int64(n) > size {
			// The writer announced a frame it has not finished: everything before
			// it is intact, and the capture is a snapshot of what had been painted.
			break
		}
		marks = append(marks, SgrFrameMark{Bytes: n, Offset: after})
		payload += int64(n)
		pos = after + int64(n)
	}
	return meta, marks, payload, trailer, nil
}

// sgrTurnCursorMaxLen bounds the bytes of control sequences that may sit between
// the cursor-position sequence and the header. Measured on a real recording the
// gap is three sequences (CUP, erase-line, SGR colour), so this is several times
// the honest size; it exists so the backwards walk in
// sgrTurnHeaderFollowsCursor terminates on a corrupt frame rather than scanning
// the whole payload.
const sgrTurnCursorMaxLen = 96

// sgrTurnMaxSequencesBeforeHeader bounds how many complete control sequences the
// backwards walk will skip, and sgrTurnCursorMaxLen bounds the bytes it will read
// doing it. Both are UPPER BOUNDS, not the observed shape: measured over the 14
// header frames of a real recording (session e68fddf5) the stem has at most TWO
// complete sequences and 26 bytes in front of it, so ten sequences is a 5x margin
// and 96 bytes a 3.7x one. The bound exists so a corrupt frame cannot make the walk
// scan a whole payload; it is not a statement about what the renderer draws.
const sgrTurnMaxSequencesBeforeHeader = 10

// sgrByteESC is the single byte a control sequence starts with — named because
// the difference between it and the two-character TEXT spelling of an escape is
// half of what this predicate exists to tell apart.
const sgrByteESC = 0x1b

// sgrTurnHeaderFollowsCursor reports whether the stem at window[at] is what the
// renderer DREW at a fresh cursor position, rather than text that happens to
// contain the header's shape.
//
// THE CRITERION IS AN ABSENCE: between the header and the most recent
// cursor-position sequence there must be NOTHING BUT CONTROL SEQUENCES. That one
// sentence covers both measured false positives, and neither is caught by the stem
// alone nor by the running maximum:
//
//   - a quoted header drawn mid-line (the diagnostic said, then the stem) has
//     PRINTABLE TEXT in the gap, so the walk reaches a printable byte and stops;
//   - a frame that prints the escape's own spelling in front of a header carries
//     that spelling as PRINTABLE BYTES, so the same walk refuses it — there is no
//     ESC byte behind it.
//
// WHY THE WALK SKIPS SEQUENCES INSTEAD OF DEMANDING A CURSOR MOVE IMMEDIATELY
// BEFORE: what the renderer emits between the cursor move and the rule is not
// fixed. Measured over the 14 header frames of a real recording (session
// e68fddf5), grouped by the bytes in front of the stem:
//
//	count=13  ESC[22;1H ESC[38;2;255;248;220m
//	count=1   ESC[17;1H ESC[38;2;255;248;220m
//
// So the honest shape is "a cursor move, then some number of sequences, then the
// rule" — two segments here, and a third (an erase-line) would pass an ANY-COUNT
// skip and fail a fixed-length one. Demanding the cursor move ADJACENT to the
// stem is the other extreme and it locates nothing: 0 of 12 turns on that same
// stream.
//
// THE ROW NUMBER VARIES (22 and 17 in the same recording), which is why the
// cursor sequence is only required to EXIST: it is a screen coordinate that moves
// with the band and the window height, never a fact about the format. This is the
// same reason the locator does not key on `ESC[22;1H` the way an earlier matcher
// did.
func sgrTurnHeaderFollowsCursor(window []byte, at int) bool {
	pos := at
	sawCUP := false
	skipped := 0
	for skipped < sgrTurnMaxSequencesBeforeHeader && pos > 0 {
		start, isCUP, ok := sgrControlSeqEndingAt(window, pos, sgrTurnCursorMaxLen)
		if !ok {
			break
		}
		if isCUP {
			sawCUP = true
		}
		pos = start
		skipped++
	}
	return sawCUP
}

// sgrControlSeqEndingAt reports whether a complete control sequence ends exactly
// at window[pos-1], and if so where it starts and whether it is a cursor-position
// sequence (a CSI whose final byte is H).
//
// IT REFUSES ANYTHING IT CANNOT READ COMPLETELY. A sequence whose introducer is
// missing, or whose parameters are not the bytes a CSI may carry, is NOT a
// sequence — which is exactly the case the literal-text copy produces, and why
// this returns ok=false rather than guessing at a boundary.
func sgrControlSeqEndingAt(window []byte, pos, maxLen int) (start int, isCUP bool, ok bool) {
	lo := pos - maxLen
	if lo < 0 {
		lo = 0
	}
	final := window[pos-1]
	if pos-1 < lo || final < 0x40 || final > 0x7e {
		return 0, false, false
	}
	i := pos - 2
	for i >= lo && window[i] >= 0x20 && window[i] <= 0x3f {
		i--
	}
	if i < lo || window[i] != '[' {
		return 0, false, false
	}
	i--
	if i < lo || window[i] != sgrByteESC {
		return 0, false, false
	}
	return i, final == 'H', true
}
