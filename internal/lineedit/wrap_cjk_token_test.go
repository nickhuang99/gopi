package lineedit

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// wrap_cjk_token_test.go — a Latin/digit token embedded in a CJK run is never
// halved by the grid fallback.
//
// THE DEFECT THIS PINS. wrapCols breaks at the LAST boundary the source offers,
// and falls back to a CELL cut when the source offers none. CJK text separates
// its words with no spaces, so the only boundary it has is the junction between
// two wide runes — and a token written with Latin letters or digits (`319 KB`,
// `regenerable artefact`) is NARROW on both of its edges. So `…说明文字` +
// `regenerableartefact` offered no boundary at `字|r`, the row ended by cell cut
// inside the token, and the operator could not copy it out (the grid is the
// deliverable).
//
// THE RULE. The junction between a WIDE rune and a NARROW one is a boundary in
// BOTH directions. It is the same statement the wide↔wide junction already made
// — "the source changes script here, so a token does not straddle it" — so the
// cell cut is left to the one case that has no alternative: a single token wider
// than a whole row (it cannot be moved to a wider row, because there is none).

// wrapIsWordRune reports whether r is part of a copyable token: a narrow rune
// that carries a word's own characters. `_` `-` `/` are left out on purpose —
// they are wrapSoftBreakPunct, i.e. a legitimate place for a row to end.
//
// ONE DEFINITION (Rule 14): this delegates to the production predicate the wrap
// policy itself uses (wrap_whole_word_move.go), so the assertion below and the
// implementation it checks cannot drift apart — a test that re-spelled the class
// could pass while the code moved a different one.
func wrapIsWordRune(r rune) bool { return wrapWordRune(r) }

// wrapTokenAt returns the byte span of the word run containing byte offset off.
func wrapTokenAt(s string, off int) (start, end int) {
	start, end = off, off
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:start])
		if !wrapIsWordRune(r) {
			break
		}
		start -= size
	}
	for end < len(s) {
		r, size := utf8.DecodeRuneInString(s[end:])
		if !wrapIsWordRune(r) {
			break
		}
		end += size
	}
	return start, end
}

// assertNoWordIsHalved is the deliverable's own assertion, stated once: walking
// every row junction of the wrapped text, a junction that falls BETWEEN two word
// runes has halved a token — and that is only permitted when the token is wider
// than a whole row (the documented fallback).
func assertNoWordIsHalved(t *testing.T, in string, rows []string, width int) {
	t.Helper()
	off := 0
	for i := 0; i+1 < len(rows); i++ {
		off += len(rows[i])
		// The junction must sit on a rune boundary at all; a junction inside a
		// multi-byte rune is a half-drawn character, which outranks this check.
		if !utf8.RuneStart(in[off]) {
			t.Fatalf("row %d ends %q INSIDE a rune of %q", i, rows[i], in)
		}
		before, _ := utf8.DecodeLastRuneInString(in[:off])
		after, _ := utf8.DecodeRuneInString(in[off:])
		if !wrapIsWordRune(before) || !wrapIsWordRune(after) {
			continue
		}
		start, end := wrapTokenAt(in, off)
		token := in[start:end]
		if displayWidth(token) <= width {
			t.Fatalf("wrapCols(%q, %d) row %d ends %q and row %d starts %q: the token %q (%d cells) fits a %d-cell row but was halved; rows: %q",
				in, width, i, rows[i], i+1, rows[i+1], token, displayWidth(token), width, rows)
		}
	}
}

// TestWrapCols_KeepsLatinTokenWholeAfterCJK is the narrow-edge sighting: `319`
// starting right after a wide rune used to have no boundary in front of it, so
// the row ended `磁盘占用31` and the number came back as `1` + `9 KB`.
func TestWrapCols_KeepsLatinTokenWholeAfterCJK(t *testing.T) {
	const (
		in    = "磁盘占用319 KB"
		width = 10
	)
	got := wrapCols(in, width)
	want := []string{"磁盘占用", "319 KB"}
	if len(got) != len(want) {
		t.Fatalf("wrapCols(%q, %d) = %q, want %q", in, width, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d = %q, want %q (full rows: %q)", i, got[i], want[i], got)
		}
	}
	if joined := strings.Join(got, ""); joined != in {
		t.Fatalf("the rows carry %q, want the input %q", joined, in)
	}
}

// TestWrapCols_KeepsLongTokenWholeAfterCJK is the same junction with a token that
// still FITS a row: `regenerableartefact` is 20 cells and the row is 22, so a
// cell cut inside it is pure loss — the whole token belongs on the next row.
func TestWrapCols_KeepsLongTokenWholeAfterCJK(t *testing.T) {
	const (
		in    = "中文说明文字regenerableartefact"
		width = 22
	)
	got := wrapCols(in, width)
	want := []string{"中文说明文字", "regenerableartefact"}
	if len(got) != len(want) {
		t.Fatalf("wrapCols(%q, %d) = %q, want %q", in, width, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d = %q, want %q (full rows: %q)", i, got[i], want[i], got)
		}
	}
	if joined := strings.Join(got, ""); joined != in {
		t.Fatalf("the rows carry %q, want the input %q", joined, in)
	}
}

// TestWrapCols_CJKAnswersNeverHalveAToken sweeps the shapes the answer band
// actually meets — a CJK sentence carrying `319 KB` and `regenerable artefact`,
// the tokens both spaced and glued to the surrounding characters — across the
// widths a band is ever laid out at, and asserts the deliverable's invariant on
// every one: reassembly is byte-exact, no row is wider than the band, and no
// token that fits a row is halved.
func TestWrapCols_CJKAnswersNeverHalveAToken(t *testing.T) {
	ins := []string{
		"磁盘占用 319 KB，这是 regenerable artefact 的大小",
		"磁盘占用319 KB，这是regenerable artefact的大小",
		"实测 cmd/agent/cli_tasks.go 的 319 KB 与 regenerable artefact",
		"缓存目录（319 KB）需要重建，这是一个 regenerable artefact",
		"这一段说明里嵌了319KB这样的 token 然后是regenerableartefact这样的词",
		"占用319 KB（约0.3MB）的regenerableartefact文件需要重建然后再次测量它",
		"中文说明文字regenerableartefact",
		"磁盘占用319 KB",
	}
	for _, in := range ins {
		for _, width := range []int{9, 10, 12, 16, 18, 20, 22, 24, 30, 33, 40, 47, 80} {
			got := wrapCols(in, width)
			if joined := strings.Join(got, ""); joined != in {
				t.Fatalf("wrapCols(%q, %d) rows carry %q, want the input back", in, width, joined)
			}
			for i, row := range got {
				// A row wider than the band is only legal when the row IS a
				// single over-wide rune emitted alone.
				if w := displayWidth(row); w > width && utf8.RuneCountInString(row) > 1 {
					t.Fatalf("wrapCols(%q, %d) row %d is %d cells: %q", in, width, i, w, row)
				}
			}
			assertNoWordIsHalved(t, in, got, width)
		}
	}
}
