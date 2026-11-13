package lineedit

import (
	"strings"
	"unicode/utf8"

	"github.com/nick/gopi/internal/sgr"
)

// band_word_move.go — the answer band's row cutter applies the wrap policy's
// FOURTH disposition (wrap_whole_word_move.go): a token the cell edge would cut
// in half moves DOWN whole instead, so the band's rows are tokens a reader can
// copy instead of halves that do not run.
//
// WHY THE BAND NEEDED IT. wrapCols and WrapToCols break at a boundary the source
// offers — whitespace, wrapSoftBreakPunct, a wide↔narrow junction — and end a row
// inside a token only when that token is wider than a whole row (the case with no
// wider row to move it to; wrap_whole_word_sweep_test.go pins exactly that). The
// band cut with cutCols alone: a pure CELL cut, no boundary and no move, so on the
// operator's screen
//
//	run /compact then check the endpoint of s//tyle now
//
// at 32 cells ended inside `endpoint` and `s//tyle` and said so with ↵ — a row
// that does not run, on the surface the operator copies out of.
//
// WHY IT IS PREFIX-DECIDABLE, AND THAT IS THE WHOLE DIFFICULTY. One band line is
// drawn TWICE: the streaming paint (drain → writeLive → writeRow, which holds a
// would-wrap unfinished line whole and otherwise commits each row as it fills) and
// the commit replay (commitPrefix → commitLines → writeRow, which holds the whole
// line). fitCommitRow reconciles the two by the head the live row already shows,
// and that reconcile is arithmetic on COLUMNS — so the two sides must reach the
// SAME break for the same line. A rule that asks "does this token fit a whole
// row?" answers with the token's own END, which the paint has not seen yet: the
// paint would move the token down and the replay would leave it, and the leftover
// cells are exactly the measured duplication (delta=1, 60 extra cells).
//
// So every question the band asks is about BYTES ALREADY DRAWN:
//
//   - does the cell edge land inside the run the wrap policy moves as one token
//     (bandCutHalvesToken — the path slash INCLUDED, see bandTokenSoftPunct)?
//   - does that token start after the row's own first byte (a move leaves the row
//     non-empty, so it terminates)?
//   - is that start a position a row may end at (wrapSafeRowEnd: never inside an
//     SGR sequence, whose parameters would print as text)?
//
// The one byte read AT `cut` is the rune the committed row already shows: a row
// may end where its own last drawn character is, which is why the predicate is
// decidable in the prefix. Nothing reads FURTHER than that, so the two paints
// cannot disagree. A token WIDER than the row still ends up cut — the row it
// starts on has no wider one to move it to — and it is still labelled, which is
// what narrows the band's ↵ to one meaning: the token being cut is wider than the
// whole row.
//
// Invariant: head + tail == text, byte for byte, so the rows of a line still
// reassemble to the line.

// bandWordShift returns the byte offset this row must END at so that the text the
// cell edge would cut moves down whole, or `cut` unchanged when no move applies.
//
// `from` is the row's own first byte inside `text` (0 for the band, whose writeRow
// hands each wrap the row's remaining text as a fresh string).
//
// THE LADDER, in order, and each rung is a WIDER row than the cut:
//
//  1. the whole TOKEN moves down (the disposition proper): `the endpoint` at 6
//     cells ends after `the `, so `endpoint` starts the next row whole;
//  2. the token cannot move because it BEGINS the row, so its last WORD run moves
//     the break back to that run's own start: `src/internal` at 5 cells ends after
//     `src/`, not one byte later inside `internal`;
//  3. neither could help, so the break goes to the last boundary the WRAP POLICY
//     itself offers in front of the edge — where wrapCols would have ended the row;
//  4. nothing in front of the edge offers anything: the raw cell cut stands, and
//     the row says so with the band's cut label (an over-wide token, e.g.
//     `abcdefgh` at 4 cells).
func bandWordShift(text string, from, cut int) int {
	if cut <= from || cut >= len(text) {
		return cut
	}
	if !bandCutHalvesToken(text, cut) {
		return cut
	}
	if start := bandCutRunStart(text, from, cut, bandTokenRune); start > from && wrapSafeRowEnd(text, from, start) {
		return start
	}
	if word := bandCutRunStart(text, from, cut, wrapWordRune); word > from && word < cut && wrapSafeRowEnd(text, from, word) {
		return word
	}
	if at := bandWrapBoundaryBefore(text, from, cut); at > from {
		return at
	}
	return cut
}

// bandCutHalvesToken reports whether `cut` splits one token — the run the band
// moves down whole, which is NOT the same thing as "a word rune on both sides".
//
// WHY IT IS NOT "TWO WORD RUNES ON BOTH SIDES". That was the first version, and it
// is the defect this file repairs. `wrapWordRune` is a letter, a digit or `.`;
// wrapSoftBreakPunct ( `/ - _ ? & = , ; ) ]` ) is deliberately OUT of it, because
// the WRAP policy may END a row there. But "may end there" is not "must end
// there": `s//tyle` IS one token, and the band's row end is the CELL edge's own
// (writeRow → bandCut → cutCols — wrapCols is never consulted), so on a mixed
// CJK/Latin line the edge lands inside the path token and the old predicate
// answered "no half" exactly where the band had already made one:
//
//	bandCut("这个 endpoint 的处理在 s//tyle 里面已经改好了", 28)
//	  pre-fix  = ("这个 endpoint 的处理在 s//", "tyle 里面已经改好了")   ← s//tyle halved
//	  post-fix = ("这个 endpoint 的处理在",     " s//tyle 里面已经改好了") ← moved down whole
//
// So the run is wrapWordRune EXTENDED by bandTokenSoftPunct (the path slash), and
// the cut is a halved token when BOTH sides belong to that run. Read on the bytes
// before `cut` and the one at it — never further (the prefix rule above).
func bandCutHalvesToken(text string, cut int) bool {
	if cut <= 0 || cut >= len(text) {
		return false
	}
	before, _ := utf8.DecodeLastRuneInString(text[:cut])
	after, _ := utf8.DecodeRuneInString(text[cut:])
	return bandTokenRune(before) && bandTokenRune(after)
}

// bandCutRunStart returns the first byte of the run ending at `cut` whose runes all
// satisfy `in`, never before `from`. bandCutHalvesToken's token is
// bandTokenRune's run; the label's narrower question is wrapWordRune's.
func bandCutRunStart(text string, from, cut int, in func(rune) bool) int {
	start := cut
	for start > from {
		r, size := utf8.DecodeLastRuneInString(text[:start])
		if !in(r) {
			break
		}
		start -= size
	}
	return start
}

// bandTokenRune reports whether r belongs to the token the band moves down whole:
// the wrap policy's word rune, plus the path slash the source MAY end a row after —
// because the band's job is the token, not the earliest legal break.
func bandTokenRune(r rune) bool {
	if wrapWordRune(r) {
		return true
	}
	if isWideRune(r) || r == utf8.RuneError {
		return false
	}
	return strings.ContainsRune(bandTokenSoftPunct, r)
}

// bandTokenSoftPunct widens bandTokenRune past wrapWordRune by the ONE separator
// this disposition was measured on: the path slash, so `s//tyle` and `a/file.go`
// stay one token even though wrapSoftBreakPunct lets a row end after each `/`.
//
// WHY NOT THE WHOLE wrapSoftBreakPunct SET, MEASURED. Widening by `-` (or by the
// full set) turns TestAnswerBand_WrappedLineLosesNothingInAnyFrame RED: the band's
// own transcript carries number ranges (`00-01 00-02 …`) and shell flags, and a
// hyphen that welds such a run into one token moves a break far enough back that a
// frame the reader had already been shown SHRINKS (the streaming paint and the
// commit replay stop agreeing on that frame). `/` is the separator the deliverable
// names and the only one measured green against that guard, so the set is exactly
// `/` — deliberately NARROWER than the wrap set instead of derived from it.
//
// The two wrap-set characters this file refuses to treat as interiors are `)` and
// `]`: they CLOSE a group, and no token continues through one (`f(a)b` is three
// things, not one).
const bandTokenSoftPunct = "/"

// bandWrapBoundaryBefore returns the last byte offset in [from, cut) at which a row
// may end by the wrap policy's own boundary (wrapBreakAfter), or -1 when the text in
// front of the edge offers none.
//
// It is rung 3 of bandWordShift's ladder: a token with nowhere better to go still
// must not be cut one byte past a boundary the source offered. Every offset it asks
// about is strictly < cut, so the prefix rule holds.
func bandWrapBoundaryBefore(text string, from, cut int) int {
	last := -1
	for i := from; i < cut; {
		if n := sgr.EscapeSeqLen(text[i:]); n > 0 {
			i += n
			continue
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		i += size
		if i >= cut {
			break
		}
		if wrapBreakAfter(r, text, i) {
			last = i
		}
	}
	return last
}

// bandCut splits one band row's text at `width` display cells, moving a halved
// token down whole, and reports whether the row still ends inside that token.
//
// `halved` is what writeRow's cut label is decided on: the row stopped mid-token
// and the move could not avoid it, which happens exactly when the token is wider
// than the whole row (or begins the row, or the move would tear a sequence). It is
// therefore the narrow meaning the label now carries, replacing "the break landed
// between two word characters" — the disposition above removes those cases.
//
// `width` is the room the row actually has (the band's width less the turn's
// marker column already written).
func bandCut(text string, width int) (head, tail string, halved bool) {
	head, tail = cutCols(text, width)
	if tail == "" {
		return head, tail, false
	}
	if at := bandWordShift(text, 0, len(head)); at < len(head) {
		// THE WHITESPACE IN FRONT OF THE MOVED TOKEN GOES DOWN WITH IT.
		//
		// Two reasons, and the second is the one that took a measurement. A row
		// that ends in spaces carries cells nothing is in, and — the real one —
		// keeping them here makes the two shapes of one wrap disagree: the token
		// then starts a row on its own, while every other row of the band opens
		// with the whitespace its line had at that position. The continuation
		// trims ONE leading space to hold the block's column (trimWrapSpace), so
		// the space is drawn there exactly once either way; what is not
		// interchangeable is the row BREAK, which the reader's copy reads.
		sp := at
		for sp > 0 && text[sp-1] == bandMoveSpace {
			sp--
		}
		if sp > 0 {
			return text[:sp], text[sp:], false
		}
		return text[:at], text[at:], false
	}
	// The LABEL's question stays the narrow one it always was — a break between two
	// WORD runes, the break a reader cannot see. It is deliberately narrower than
	// bandCutHalvesToken: a row that ended after a path slash ended at a place the
	// source offered, and putting ↵ on such a row would spend the one meaning the
	// label has (this token is wider than a whole row).
	return head, tail, bandCutWordHalved(text, len(head))
}

// bandCutWordHalved is the cut-label predicate: the row stopped between two
// wrapWordRune characters. It is wrapWholeWordShift's original condition, kept so
// the disposition can widen without the band's ↵ vocabulary drifting with it.
func bandCutWordHalved(text string, cut int) bool {
	if cut <= 0 || cut >= len(text) {
		return false
	}
	before, _ := utf8.DecodeLastRuneInString(text[:cut])
	after, _ := utf8.DecodeRuneInString(text[cut:])
	return wrapWordRune(before) && wrapWordRune(after)
}

// bandMoveSpace is the character the move carries down with its token: the answer
// body's own word separator. A tab is deliberately out — its width is a terminal
// question, not a wrap question.
const bandMoveSpace = ' '
