package lineedit

import (
	"strings"
	"unicode/utf8"
)

// wrap_token_boundary.go — the wrap policy's boundary arm asks the WHOLE-TOKEN
// question before it adopts a boundary.
//
// THE DEFECT THIS FILE REPAIRS. wrapCols has two arms: end the row at the last
// boundary the source offers (wrapBreakAfter: whitespace, wrapSoftBreakPunct, a
// wide↔narrow junction), or — when the source offers none — end it by the CELL
// cut, letting wrapWholeWordShift move a halved token down. Measured:
//
//	pre-fix  WrapToCols("这个 endpoint 的处理在 s//tyle 里面已经改好了", 28)
//	         = ["这个 endpoint 的处理在 s//", "tyle 里面已经改好了"]
//
// The boundary arm fired at byte 33 — the second `/` of `s//tyle` — because `/`
// is in wrapSoftBreakPunct, so the row "legitimately" ended there and the cell
// arm (the one that moves a halved token down) was never reached (`last > from`
// held, so the `default` arm was dead on this input). The reader got `s//` on
// one row and `tyle` on the next: two fragments, neither of which runs.
//
// WHY A BOUNDARY CAN STILL BE WRONG. "A row MAY end after `/`" is not "it must":
// `s//tyle` is ONE token. A boundary therefore only counts when the text on the
// row BELOW begins a token the reader can copy. Ending after a lone separator
// (`run /` + `compact`) does start a whole word below — that is the existing,
// correct behaviour, and it stays. Ending after `s//` starts `tyle`, which is
// the tail of a word, not a word.
//
// THE CRITERION, and it needs no new vocabulary. Walking the run of
// wrapTokenRune on both sides of the boundary:
//
//   - both sides must be token interiors, or nothing is being split;
//   - the run IN FRONT of the boundary must carry at least one wrapWordRune —
//     a lone separator (`run|/compact`) carries none, so it is not a word being
//     halved and the boundary stands;
//   - the run BEHIND it must carry one too, or there is no word below to spoil;
//   - the whole token must fit the row width (otherwise there is no wider row
//     to move it to — the same rule wrapWholeWordShift applies), must start
//     after `from` (the row keeps its own bytes; no empty row, no loop), and
//     must be a position a row may end at at all (wrapSafeRowEnd).
//
// When all of that holds the row ends at the TOKEN's start instead: `s//tyle`
// moves down whole. Nothing else changes — this arm only ever moves a cut
// EARLIER, never later, so it cannot make a row wider, cannot re-order bytes and
// cannot invent a break the source did not offer.
//
// WHY THE TOKEN SET EXTENDS wrapWordRune BY THE SLASH. wrapWordRune is a letter,
// a digit or `.`; the slash is deliberately out of it because the WRAP policy may
// end a row after one. But "may end there" is not "the token stops there" — the
// band side reached the same conclusion first (`bandTokenRune`,
// band_word_move.go, commit a361a9d6) and this file's set is deliberately the
// same one. TestWrapTokenRuneMatchesTheBandToken holds the two together so a
// later widening of either cannot silently desynchronise the wrap side from the
// band side — the drift that produced this defect in the first place.
//
// `-` is NOT in the set, and that is a measurement, not a preference: welding
// hyphenated number ranges (`00-01 00-02`) into one token moves a break far
// enough back that a frame the reader has already been shown SHRINKS (the
// answer band's streaming paint and commit replay stop agreeing on it). The band
// side paid for that finding in TestBandTokenSetIsTheMeasuredSlashOnly; the set
// here is `/` for exactly the same reason.

// wrapTokenInteriorPunct are the narrow separators a token continues THROUGH,
// even though wrapSoftBreakPunct lets a row end after them. Exactly one, and it
// is the measured one (see the file comment): the path slash of `s//tyle` and
// `a/file.go`.
const wrapTokenInteriorPunct = "/"

// wrapTokenRune reports whether r belongs to a token the wrap policy keeps
// whole: the word rune (a narrow letter, digit or `.`) plus the path slash.
//
// It is the same set bandTokenRune uses (band_word_move.go). Two definitions
// would be the drift this file exists to end, so the agreement is asserted:
// TestWrapTokenRuneMatchesTheBandToken walks every rune either side classifies
// and fails if the two answers differ.
func wrapTokenRune(r rune) bool {
	if wrapWordRune(r) {
		return true
	}
	if isWideRune(r) || r == utf8.RuneError {
		return false
	}
	return strings.ContainsRune(wrapTokenInteriorPunct, r)
}

// wrapTokenRunBefore returns the start of the run of wrapTokenRune runes that
// ends at `cut`, and whether that run carries a word rune. It never walks past
// `floor`.
func wrapTokenRunBefore(s string, floor, cut int) (start int, hasWord bool) {
	start = cut
	for start > floor {
		r, size := utf8.DecodeLastRuneInString(s[:start])
		if !wrapTokenRune(r) {
			break
		}
		if wrapWordRune(r) {
			hasWord = true
		}
		start -= size
	}
	return start, hasWord
}

// wrapTokenRunAfter returns the end of the run of wrapTokenRune runes that
// begins at `cut`, and whether that run carries a word rune.
func wrapTokenRunAfter(s string, cut int) (end int, hasWord bool) {
	end = cut
	for end < len(s) {
		r, size := utf8.DecodeRuneInString(s[end:])
		if !wrapTokenRune(r) {
			break
		}
		if wrapWordRune(r) {
			hasWord = true
		}
		end += size
	}
	return end, hasWord
}

// wrapBoundaryWholeTokenStart reports where a row must end when the boundary it
// would otherwise adopt at `last` falls INSIDE a token, and returns `last`
// itself when that boundary is legitimate (or when no move is available).
//
// It is called by wrapCols' boundary arm, so the order is: the boundary is
// consulted first, the whole-token question second — and the token question has
// the final word. See the file comment for the criterion and why each clause is
// there.
func wrapBoundaryWholeTokenStart(s string, from, last, width int) int {
	if last <= from || last >= len(s) {
		return last
	}
	before, _ := utf8.DecodeLastRuneInString(s[:last])
	after, _ := utf8.DecodeRuneInString(s[last:])
	if !wrapTokenRune(before) || !wrapTokenRune(after) {
		// The boundary is not inside a token: nothing is being halved.
		return last
	}
	start, leftHasWord := wrapTokenRunBefore(s, from, last)
	if !leftHasWord {
		// A LONE separator (`run|/compact`): the row ends after a `/` that has
		// no word in front of it, so the row below opens a whole word. That is
		// the documented behaviour, not a halved token.
		return last
	}
	end, rightHasWord := wrapTokenRunAfter(s, last)
	if !rightHasWord {
		// Slashes only behind the boundary (`a//` + `// b`): nothing copyable is
		// being spoiled on the next row.
		return last
	}
	if displayWidth(s[start:end]) > width {
		// The token does not fit a whole row, so no wider row exists to move it
		// to — the same fallback wrapWholeWordShift keeps.
		return last
	}
	if start <= from {
		// The token BEGINS the row, so there is no earlier place to end it — but
		// it fits this one, so the row takes the whole token instead of stopping
		// short inside it. Measured by the sweep: `WrapToCols("aa/a ", 4)` used
		// to be ["aa/" "a "] even though `aa/a` is exactly 4 cells, i.e. the row
		// was cut immediately in front of the one cell that would have made it
		// whole. The cut only moves FORWARD here, and only to a position the row
		// width already covers (the token fits), so the bounds still hold.
		if !wrapSafeRowEnd(s, from, end) {
			return last
		}
		return end
	}
	if !wrapSafeRowEnd(s, from, start) {
		// The token's start is inside an escape sequence: a torn sequence would
		// print its parameters as text. A control sequence outranks the word.
		return last
	}
	return start
}
