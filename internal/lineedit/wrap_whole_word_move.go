package lineedit

import (
	"unicode"
	"unicode/utf8"

	"github.com/nickhuang99/gopi/internal/sgr"
)

// wrap_whole_word_move.go — the wrap policy's FOURTH disposition, implemented.
//
// THE DISPOSITION. wrapCols ends a row at a boundary the source offers
// (wrapBreakAfter: whitespace, wrapSoftBreakPunct, a wide↔narrow junction), and
// falls back to a CELL cut when the source offers none in front of the edge. The
// gap between those two arms is a token that FITS A WHOLE ROW BY ITSELF but does
// not fit the room left on the current one: nothing but the cell cut could end
// the row inside it, and the cell cut halves a token the operator then cannot
// copy out of the grid. Such a token MOVES DOWN whole — the row ends before it.
//
// WHAT WAS MISSING, MEASURED. `:` and `"` are neither word characters nor
// wrapSoftBreakPunct, so a source like `a:aa` at width 3 offered no boundary in
// front of the edge and the cell cut landed between the two `a`s:
//
//	pre-fix  wrapCols("a:aa", 3)  = ["a:a" "a"]     ← `aa` (2 cells ≤ 3) halved
//	post-fix wrapCols("a:aa", 3)  = ["a:"  "aa"]    ← moved down whole
//
// The same hole produced "\x1b[1mkey\x1b[0m endpoint" @10 = ["…key\x1b[0m endpoi"
// "nt"], i.e. `endpoint` halved although it is 8 cells on a 10-cell row.
//
// WHY A MOVE AND NOT A BOUNDARY. A boundary the source did not offer may not be
// invented (that would re-cut tokens the operator wrote as one word, e.g. after
// `:` in `key:value`); the move keeps the source's own tokenisation and only
// decides WHERE the row ends. The invariant survives: the rows reassemble to s
// exactly, and the moved-down prefix is a prefix of the row that already fit.
//
// WHAT DOES NOT CHANGE. A token WIDER than a whole row has no wider row to move
// to, so the cell cut stays the only lossless answer for it — `wrapCols(
// "abcdefgh", 4)` keeps ["abcd" "efgh"]. That is the existing policy, and this
// file is additive: it never widens the cut, it only removes cuts it can avoid.

// wrapWordRune reports whether r is part of a copyable token — the rune class
// wrapWholeWordShift moves down. It is the SAME definition the package's wrap
// tests use (wrapIsWordRune in wrap_cjk_token_test.go delegates here, so the
// deliverable's own assertion and the implementation cannot drift): a narrow
// letter, digit or `.`. `_` `-` `/` are deliberately out — they are
// wrapSoftBreakPunct, i.e. a place a row may legitimately end.
func wrapWordRune(r rune) bool {
	if r == utf8.RuneError || isWideRune(r) {
		return false
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.'
}

// wrapWholeWordShift returns the byte offset a row must END at so that the token
// straddling `cut` moves down whole, or `cut` itself when no such move applies.
//
// It applies exactly when both hold:
//
//  1. `cut` lands INSIDE a token — the rune before it and the rune at it are
//     both wrapWordRune, so the cell cut would halve a word;
//  2. that whole token fits the row width, i.e. it has a wider row to move to;
//  3. the token starts after `from`, so the row keeps at least one byte and the
//     move cannot loop in place.
//
// It does NOT consult `esc`: the caller decides that. A row carrying SGR keeps
// its own rule (wrapCols' `esc` suppression of BOUNDARY breaks), which is about
// where a sequence's effect starts, a question a row END does not change — but
// this function is deliberately silent on it so the two rules stay separable.
func wrapWholeWordShift(s string, from, cut, width int) int {
	if cut <= from || cut >= len(s) {
		return cut
	}
	// (1) the cut must split a token: a word rune on BOTH sides of it.
	before, _ := utf8.DecodeLastRuneInString(s[:cut])
	after, _ := utf8.DecodeRuneInString(s[cut:])
	if !wrapWordRune(before) || !wrapWordRune(after) {
		return cut
	}
	// The token's own span, walked in both directions from the cut.
	start := cut
	for start > from {
		r, size := utf8.DecodeLastRuneInString(s[:start])
		if !wrapWordRune(r) {
			break
		}
		start -= size
	}
	end := cut
	for end < len(s) {
		r, size := utf8.DecodeRuneInString(s[end:])
		if !wrapWordRune(r) {
			break
		}
		end += size
	}
	// (3) the row keeps its own bytes; (2) the token fits a whole row.
	if start <= from {
		return cut
	}
	if displayWidth(s[start:end]) > width {
		return cut
	}
	// A start inside an escape sequence is not a place a row may end: a torn
	// sequence would print its own parameters as text (the same failure the
	// package's cutCols refuses). Measured: `中中` + SGR + `a` at width 4.
	if !wrapSafeRowEnd(s, from, start) {
		return cut
	}
	return start
}

// wrapSafeRowEnd reports whether `at` is a position a row may END at without
// tearing a character or a control sequence: it is reached by walking `s` from
// `from` forward, rune by rune and sequence by sequence, and landing ON `at`.
//
// It exists because the move above is computed from the token's own span, which
// knows nothing about escape sequences: `中中` + an SGR + `a` at width 4 puts the
// cell cut past the sequence and the token start INSIDE it, and ending the row
// there printed the sequence's parameters as text (`中中\x1b[`) — a torn
// sequence outranks the word question, exactly as a half-drawn rune does.
func wrapSafeRowEnd(s string, from, at int) bool {
	if at <= from || at > len(s) {
		return false
	}
	for i := from; i < len(s); {
		if i == at {
			return true
		}
		if n := sgr.EscapeSeqLen(s[i:]); n > 0 {
			i += n
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return at == len(s)
}
