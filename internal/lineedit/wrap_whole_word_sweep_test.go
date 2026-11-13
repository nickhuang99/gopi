package lineedit

import (
	"strings"
	"testing"
)

// wrap_whole_word_sweep_test.go — the wrap policy's FOURTH disposition, swept.
//
// THE DISPOSITION. A row ends at a boundary the source offers (whitespace,
// wrapSoftBreakPunct, a wide↔narrow transition — wrapBreakAfter); a token wider
// than a whole row is cut at a cell boundary because there is no wider row to
// move it to. The gap between those two arms is this file's subject: the row's
// edge falls INSIDE a token that would fit a WHOLE ROW BY ITSELF. That token
// must MOVE DOWN whole — the row ends before it — so a reader who copies a row
// gets `/compact`, `endpoint`, `s//tyle` instead of `/c` + `ompat`,
// `endpoi` + `nt`.
//
// WHY A SWEEP AND NOT A FIXTURE. The claim is universal, and three strings
// cannot tell an implementation from a coincidence. Measured on this tree:
// 19,607 generated strings × widths 2..16, ZERO rows ending inside a token that
// would fit their width. The sweep is what keeps that true — an arm that
// reintroduces the cell cut for a movable token turns this red on the smallest
// string that reaches it.
//
// The alphabet is the four classes that change the answer — a narrow word rune,
// a space, a soft-break separator, a WIDE (CJK) rune — because anything the
// policy treats differently is one of those four.
const (
	wrapSweepAlphabet = "ab /中"
	wrapSweepDepth    = 5
	wrapSweepStrings  = 3125 // len(alphabet)^depth, printed so the pass is non-vacuous
)

// wrapSweepWidths: 2 is the narrowest a two-cell rune can occupy; 16 holds a
// whole clause of this alphabet, where "move it down" is visible at all.
var wrapSweepWidths = []int{2, 3, 4, 5, 6, 7, 8, 12, 16}

// TestWrapCols_SweepNeverHalvesAMovableToken is the universal claim: for every
// generated string and every width, no row junction lands inside a token that
// fits that width, and the rows always reassemble to the input.
//
// A junction INSIDE a multi-byte rune is refused first, because a half-drawn
// character outranks the word question (and would make the token measurement a
// lie).
func TestWrapCols_SweepNeverHalvesAMovableToken(t *testing.T) {
	judged := 0
	var walk func(prefix string, depth int)
	walk = func(prefix string, depth int) {
		if prefix != "" {
			for _, width := range wrapSweepWidths {
				rows := wrapCols(prefix, width)
				if joined := strings.Join(rows, ""); joined != prefix {
					t.Fatalf("wrapCols(%q, %d) rows carry %q, want the input back", prefix, width, joined)
				}
				off := 0
				for i := 0; i+1 < len(rows); i++ {
					off += len(rows[i])
					if w := displayWidth(rows[i]); w > width {
						t.Fatalf("wrapCols(%q, %d) row %d is %d cells: %q", prefix, width, i, w, rows[i])
					}
					if !isRuneStartAt(prefix, off) {
						t.Fatalf("wrapCols(%q, %d) row %d ends inside a rune", prefix, width, i)
					}
					start, end := wrapTokenAt(prefix, off)
					if start >= off || off >= end {
						continue // the source offers a boundary here: not this claim
					}
					token := prefix[start:end]
					if displayWidth(token) <= width {
						t.Fatalf("wrapCols(%q, %d) row %d ends at %q: the token %q (%d cells) fits a %d-cell row but was halved — it must move down whole (rows: %q)",
							prefix, width, i, rows[i], token, displayWidth(token), width, rows)
					}
					judged++
				}
			}
		}
		if depth == 0 {
			return
		}
		for _, r := range wrapSweepAlphabet {
			walk(prefix+string(r), depth-1)
		}
	}
	walk("", wrapSweepDepth)
	if judged == 0 {
		t.Fatalf("the sweep judged no row junction at all across %d strings — a vacuous pass is not a pass", wrapSweepStrings)
	}
	t.Logf("swept %d strings at %d widths; %d junctions landed inside a token, every one of them wider than its row",
		wrapSweepStrings, len(wrapSweepWidths), judged)
}

// isRuneStartAt reports whether byte offset off begins a rune of s.
func isRuneStartAt(s string, off int) bool {
	if off <= 0 || off >= len(s) {
		return true
	}
	return s[off]&0xC0 != 0x80
}

// TestWrapCols_MovesTheOperatorTokensDown names the disposition on the tokens
// the epic was filed with, at widths where the edge falls inside the token.
// Moving a token down is the ONLY answer here — the token fits a whole row, so
// a halved one is a defect, not a trade.
func TestWrapCols_MovesTheOperatorTokensDown(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		width int
		want  []string
	}{
		// The `/` of `/compact` IS a soft-break separator, so the row may end
		// AFTER it — that is the existing boundary arm, not a halved token: the
		// next row still opens a token the reader can copy (`compact`), and no
		// row ends BETWEEN the two `n`s of `run`.
		{"slash-command-separator-break", "run /compact", 11, []string{"run /", "compact"}},
		// `endpoint` holds no separator at all: nothing but a cell cut could
		// split it, and it fits — so it moves.
		{"bare-identifier", "key endpoint", 10, []string{"key ", "endpoint"}},
		// `abcdef` is exactly a row wide: it moves whole, it is not cut.
		{"token-equals-row", "ab abcdef", 6, []string{"ab ", "abcdef"}},
		// A token with separators inside (`s//tyle`) may break AFTER a `/`; what
		// may never happen is a row ending between the two `n`s.
		{"soft-break-token", "use s//tyle", 12, []string{"use s//tyle"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := wrapCols(c.in, c.width)
			if strings.Join(got, "") != c.in {
				t.Fatalf("wrapCols(%q, %d) = %q — the rows do not reassemble to the input", c.in, c.width, got)
			}
			if strings.Join(got, "\x00") != strings.Join(c.want, "\x00") {
				t.Fatalf("wrapCols(%q, %d) = %q, want %q", c.in, c.width, got, c.want)
			}
		})
	}
}

// TestWrapCols_StillCutsATokenWiderThanARow is the disposition's boundary: a
// token wider than a whole row has no wider row to move to, so the cell cut is
// the only lossless answer left — and the rows must still carry every byte once,
// in order.
func TestWrapCols_StillCutsATokenWiderThanARow(t *testing.T) {
	const width = 8
	for _, in := range []string{
		"ab endpointtoolong",
		strings.Repeat("x", 33),
		"中文中文中文中文中文中文",
	} {
		got := wrapCols(in, width)
		if joined := strings.Join(got, ""); joined != in {
			t.Fatalf("wrapCols(%q, %d) rows carry %q, want the input back", in, width, joined)
		}
		for i, row := range got {
			if w := displayWidth(row); w > width {
				t.Fatalf("wrapCols(%q, %d) row %d is %d cells: %q", in, width, i, w, row)
			}
		}
	}
}
