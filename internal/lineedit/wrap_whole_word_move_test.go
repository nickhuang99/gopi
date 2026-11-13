package lineedit

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// wrap_whole_word_move_test.go — the FOURTH disposition, with a minimal sample
// that turns RED without it.
//
// THE SAMPLES, MEASURED ON THIS TREE (both directions run in this file's own
// comments so a reader can re-derive them):
//
//	pre-fix  wrapCols("a:aa", 3)                      = ["a:a" "a"]        ← `aa` (2 cells) halved
//	post-fix wrapCols("a:aa", 3)                      = ["a:" "aa"]        ← moved down whole
//	pre-fix  wrapCols("\x1b[1mkey\x1b[0m endpoint",10) = ["…key\x1b[0m endpoi" "nt"]
//	post-fix wrapCols("\x1b[1mkey\x1b[0m endpoint",10) = ["\x1b[1mkey\x1b[0m " "endpoint"]
//
// `:` and `"` are neither wrapWordRune nor wrapSoftBreakPunct, so the source
// offers no boundary in front of the edge and the CELL cut lands inside a token
// that fits a whole row. The move is what fixes it; a boundary there would be
// invented, and re-cutting `key:value` is not a trade the policy makes.

// TestWrapCols_MovesAWholeTokenDown is the minimal reproducer: each case is a
// junction the cell cut would halve, on a token that fits the row.
func TestWrapCols_MovesAWholeTokenDown(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		width int
		want  []string
	}{
		// The minimal sample: `:` gives no boundary, `aa` is 2 cells of room 3.
		{"colon-then-token", "a:aa", 3, []string{"a:", "aa"}},
		// A quoted value: the cut lands BEFORE the closing `"`, i.e. after the
		// token's own last rune — not inside it — so nothing has to move and the
		// row keeps every byte up to the quote. (The move is for junctions that
		// land INSIDE a token; this one does not, and inventing a boundary here
		// would re-cut a token the source never separated.)
		{"quote-after-token", "a\"endpoint\"", 10, []string{"a\"endpoint", "\""}},
		// An SGR-wrapped pair — the row carries a sequence, which suppresses the
		// BOUNDARY arm but must not licence a halved token.
		{"sgr-key-then-token", "\x1b[1mkey\x1b[0m endpoint", 10, []string{"\x1b[1mkey\x1b[0m ", "endpoint"}},
		// `:` again, at a width where the token is exactly a row.
		{"token-exactly-a-row", "key:endpoint", 8, []string{"key:", "endpoint"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := wrapCols(c.in, c.width)
			if joined := strings.Join(got, ""); joined != c.in {
				t.Fatalf("wrapCols(%q, %d) = %q — the rows do not reassemble to the input", c.in, c.width, got)
			}
			if strings.Join(got, "\x00") != strings.Join(c.want, "\x00") {
				t.Fatalf("wrapCols(%q, %d) = %q, want %q — the token fits the row, so it moves down whole",
					c.in, c.width, got, c.want)
			}
			for i, row := range got {
				if w := displayWidth(row); w > c.width {
					t.Fatalf("wrapCols(%q, %d) row %d is %d cells: %q", c.in, c.width, i, w, row)
				}
			}
		})
	}
}

// TestWrapCols_StillCutsATokenWiderThanTheRow is the disposition's ceiling, and
// it is the epic's own second acceptance sample: `abcdefgh` is 8 cells on a
// 4-cell row, so there is no wider row to move it to and the cell cut stands.
// This arm is what proves the fix did not widen the cut — it narrowed it.
func TestWrapCols_StillCutsATokenWiderThanTheRow(t *testing.T) {
	cases := []struct {
		in    string
		width int
		want  []string
	}{
		{"abcdefgh", 4, []string{"abcd", "efgh"}},
		{"xx abcdefgh", 4, []string{"xx ", "abcd", "efgh"}},
		{"a:aaaaaaaa", 3, []string{"a:a", "aaa", "aaa", "a"}},
	}
	for _, c := range cases {
		got := wrapCols(c.in, c.width)
		if strings.Join(got, "") != c.in {
			t.Fatalf("wrapCols(%q, %d) = %q — the rows do not reassemble to the input", c.in, c.width, got)
		}
		if strings.Join(got, "\x00") != strings.Join(c.want, "\x00") {
			t.Fatalf("wrapCols(%q, %d) = %q, want %q — a token wider than a row keeps the cell cut",
				c.in, c.width, got, c.want)
		}
	}
}

// TestWrapCols_NoRowEndsInsideASequence pins the one thing the move must not
// break: a row may not end INSIDE an escape sequence, or the sequence's
// parameters print as text (the package's cutCols already refuses this on its
// own path). `中中` + SGR + `a` at width 4 is the sample that caught it — the
// token start lands INSIDE the sequence, and the fix refuses that move.
func TestWrapCols_NoRowEndsInsideASequence(t *testing.T) {
	const s, width = "中中\x1b[31ma", 4
	rows := wrapCols(s, width)
	if joined := strings.Join(rows, ""); joined != s {
		t.Fatalf("wrapCols(%q, %d) = %q — the rows do not reassemble to the input", s, width, rows)
	}
	// A boundary that tore the sequence would leave its parameters as TEXT on a
	// row: `31m` must never appear outside a `\x1b[` that opened it.
	for i, row := range rows {
		if strings.Contains(row, "31m") != strings.Contains(row, "\x1b[31m") {
			t.Fatalf("row %d of wrapCols(%q, %d) carries the sequence's parameters as text: %q (rows %q)",
				i, s, width, row, rows)
		}
	}
	// And the move must have ENDED the row on a safe offset: every row start is
	// reached by walking the source in rune/sequence steps (wrapSafeRowEnd).
	off := 0
	for i := 0; i+1 < len(rows); i++ {
		off += len(rows[i])
		if !wrapSafeRowEnd(s, 0, off) {
			t.Fatalf("row %d of wrapCols(%q, %d) ends at byte %d, which is inside a rune or a sequence: %q",
				i, s, width, off, rows)
		}
	}
}

// TestWrapWholeWordShift_SweepWidenedAlphabet is the PREVIOUS sweep's claim, run
// on an alphabet that actually reaches the hole. The earlier sweep walked
// `ab /中` at widths 2..16 and found zero halved movable tokens — true, and
// vacuous for this defect: every member of that alphabet is a word rune, a
// space, a soft-break separator or wide, so every junction the sweep could reach
// had a boundary in front of it (or was a token wider than the row). Adding the
// two classes that produce the hole (`:` and `"`) turns the same claim red
// without the fix, which is what makes it a proof rather than a pin.
func TestWrapWholeWordShift_SweepWidenedAlphabet(t *testing.T) {
	const alphabet, depth = "ab :\"", 4
	widths := []int{2, 3, 4, 5, 6, 7, 8, 12}
	junctions, judged, strings_ := 0, 0, 0
	var walk func(prefix string, d int)
	walk = func(prefix string, d int) {
		if prefix != "" {
			strings_++
			for _, width := range widths {
				rows := wrapCols(prefix, width)
				if joined := strings.Join(rows, ""); joined != prefix {
					t.Fatalf("wrapCols(%q, %d) rows carry %q, want the input back", prefix, width, joined)
				}
				off := 0
				for i := 0; i+1 < len(rows); i++ {
					off += len(rows[i])
					if !utf8.RuneStart(prefix[off]) {
						t.Fatalf("wrapCols(%q, %d) row %d ends inside a rune", prefix, width, i)
					}
					start, end := wrapTokenAt(prefix, off)
					if start >= off || off >= end {
						continue // no token straddles this junction
					}
					junctions++
					token := prefix[start:end]
					if displayWidth(token) <= width {
						t.Fatalf("wrapCols(%q, %d) row %d ends at %q: the token %q (%d cells) fits a %d-cell row but was halved — it must move down whole (rows: %q)",
							prefix, width, i, rows[i], token, displayWidth(token), width, rows)
					}
					judged++
				}
			}
		}
		if d == 0 {
			return
		}
		for _, r := range alphabet {
			walk(prefix+string(r), d-1)
		}
	}
	walk("", depth)
	if judged == 0 {
		t.Fatalf("the sweep judged no token wider than its row — a vacuous pass is not a pass")
	}
	t.Logf("swept %d strings (%d runes^%d) x %d widths: %d junctions landed inside a token, %d of them wider than the row",
		strings_, utf8.RuneCountInString(alphabet), depth, len(widths), junctions, judged)
}
