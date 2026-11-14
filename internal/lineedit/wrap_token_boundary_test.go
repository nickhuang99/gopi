package lineedit

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// wrap_token_boundary_test.go — the boundary arm must not end a row INSIDE a token.
//
// THE DEFECT, and this file is its regression guard. wrapCols ends a row at the
// last boundary the source offers; `/` is one of them (wrapSoftBreakPunct), so
// on a mixed CJK row the edge fell after the second slash of `s//tyle` and the
// row "legitimately" ended there. The reader then got
//
//	row 0 "这个 endpoint 的处理在 s//"
//	row 1 "tyle 里面已经改好了"
//
// — two fragments, neither of which runs. The cell-arm fallback that moves a
// halved token down never ran on this input, because the boundary arm matched
// first, so the whole-word disposition was dead code on the very shape it exists
// for. See wrap_token_boundary.go for the criterion.
//
// EVERY ASSERTION HERE GOES THROUGH WrapToCols — the exported entry the editor,
// the band and the streaming renderer all call (Rule 31). A test that called
// wrapBoundaryWholeTokenStart directly would pass while the entry stayed broken;
// that is exactly how the previous row (01a1c535, bandWordShift) shipped a green
// suite over a defect still visible on screen.

// TestWrapToCols_DoesNotEndARowInsideAToken is the operator's own sighting, at
// the width it was seen at: `s//tyle` must arrive on ONE row.
func TestWrapToCols_DoesNotEndARowInsideAToken(t *testing.T) {
	const (
		in    = "这个 endpoint 的处理在 s//tyle 里面已经改好了"
		width = 28
	)
	got := WrapToCols(in, width)
	if joined := strings.Join(got, ""); joined != in {
		t.Fatalf("WrapToCols(%q, %d) rows carry %q, want the input back", in, width, joined)
	}
	for i, row := range got {
		if strings.Contains(row, "s//tyle") {
			// The token is whole on this row — and it must be the WHOLE token,
			// so no other row may carry a piece of it.
			if strings.Count(in, "s//tyle") != 1 {
				t.Fatalf("fixture carries the token more than once")
			}
			for j, other := range got {
				if j != i && strings.Contains(other, "tyle") {
					t.Fatalf("WrapToCols(%q, %d) = %q — row %d carries %q, a fragment of the token kept whole on row %d",
						in, width, got, j, other, i)
				}
			}
			return
		}
	}
	t.Fatalf("WrapToCols(%q, %d) = %q — the token %q appears on no row: it was halved", in, width, got, "s//tyle")
}

// TestWrapToCols_TokenSightingIsExact pins the full row layout of that sighting,
// so a later change cannot satisfy "the token is whole" by a different division
// of the line and call it green. The rows below are what the fix produces: the
// row ends before the token and the token opens the next row.
func TestWrapToCols_TokenSightingIsExact(t *testing.T) {
	const (
		in    = "这个 endpoint 的处理在 s//tyle 里面已经改好了"
		width = 28
	)
	want := []string{"这个 endpoint 的处理在 ", "s//tyle 里面已经改好了"}
	got := WrapToCols(in, width)
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("WrapToCols(%q, %d) = %q, want %q", in, width, got, want)
	}
}

// TestWrapToCols_ExistingBoundaryBreaksSurvive is the OTHER half, and it is the
// reason the fix asks a question instead of banning the slash: a row MAY end
// after a lone separator, because the row below then opens a whole word. Those
// breaks are existing, correct behaviour and must not regress.
func TestWrapToCols_ExistingBoundaryBreaksSurvive(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		width int
		want  []string
	}{
		// A lone slash with no word in front of it: `compact` opens the next row
		// whole, so this break stands.
		{"lone-slash-keeps-its-break", "run /compact", 11, []string{"run /", "compact"}},
		// The acceptance string this epic must not disturb.
		{
			"acceptance-pure-english",
			"run /compact then check the endpoint of s//tyle now",
			32,
			[]string{"run /compact then check the ", "endpoint of s//tyle now"},
		},
		// A path ends after its own separators: every row is whole components.
		{"path-components", "/home/user/.gopi/sessions/01a1", 20, []string{"/home/user/.gopi/", "sessions/01a1"}},
		// A dotted name is one token, moved down whole.
		{"dotted-name", "see the COMMANDS.md file", 12, []string{"see the ", "COMMANDS.md ", "file"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := WrapToCols(c.in, c.width)
			if joined := strings.Join(got, ""); joined != c.in {
				t.Fatalf("WrapToCols(%q, %d) rows carry %q, want the input back", c.in, c.width, joined)
			}
			if strings.Join(got, "\x00") != strings.Join(c.want, "\x00") {
				t.Fatalf("WrapToCols(%q, %d) = %q, want %q", c.in, c.width, got, c.want)
			}
		})
	}
}

// TestWrapToCols_SweepNeverEndsInsideAToken is the universal claim this file
// exists to hold, and it is swept rather than sampled: for every generated
// string and every width, no row junction may land between two runes of ONE
// token that fits the row — i.e. the two rows either side of a junction must not
// be two fragments of a word the source wrote as one.
//
// The predicate is deliberately NOT the one the fixed code uses. It is stated
// from the READER's side — "the piece before the junction and the piece after it
// are both middles of one token" — using wrapTokenRune directly, so a later
// change to wrapBoundaryWholeTokenStart that narrows its own question turns this
// red instead of quietly agreeing with itself.
func TestWrapToCols_SweepNeverEndsInsideAToken(t *testing.T) {
	judged := 0
	var walk func(prefix string, depth int)
	walk = func(prefix string, depth int) {
		if prefix != "" {
			for _, width := range wrapSweepWidths {
				rows := WrapToCols(prefix, width)
				if joined := strings.Join(rows, ""); joined != prefix {
					t.Fatalf("WrapToCols(%q, %d) rows carry %q, want the input back", prefix, width, joined)
				}
				off := 0
				for i := 0; i+1 < len(rows); i++ {
					off += len(rows[i])
					if !utf8.RuneStart(prefix[off]) {
						t.Fatalf("WrapToCols(%q, %d) row %d ends inside a rune", prefix, width, i)
					}
					before, _ := utf8.DecodeLastRuneInString(prefix[:off])
					after, _ := utf8.DecodeRuneInString(prefix[off:])
					if !wrapTokenRune(before) || !wrapTokenRune(after) {
						continue
					}
					start, leftWord := wrapTokenRunBefore(prefix, 0, off)
					end, rightWord := wrapTokenRunAfter(prefix, off)
					if !leftWord || !rightWord {
						continue // a lone separator: the row below opens a whole word
					}
					judged++
					token := prefix[start:end]
					if displayWidth(token) > width {
						continue // wider than a row: no wider row to move it to
					}
					t.Fatalf("WrapToCols(%q, %d) row %d ends %q and row %d starts %q: the junction is INSIDE the token %q (%d cells, fits a %d-cell row); rows: %q",
						prefix, width, i, rows[i], i+1, rows[i+1], token, displayWidth(token), width, rows)
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
	// The sweep is non-vacuous by construction: it only fails, so a green tells
	// us nothing about how many junctions it looked at. Count them anyway, on a
	// small sample, so a sweep that stopped walking cannot pass quietly.
	if judged == 0 {
		t.Fatalf("the sweep reached no token junction at all across %d strings — a vacuous green", wrapSweepStrings)
	}
}

// TestWrapTokenRuneMatchesTheBandToken holds the wrap side's token set to the
// band side's. The defect this epic repairs WAS a disagreement between those two
// predicates: the band had already been widened to admit the slash (a361a9d6)
// while the wrap side still treated it as a boundary, so the same string was
// whole in the band's model and halved in the wrap's. Two predicates that must
// answer the same question are one predicate (Rule 14) — where they cannot be
// merged, they are pinned together here.
func TestWrapTokenRuneMatchesTheBandToken(t *testing.T) {
	for r := rune(0x20); r < 0x3000; r++ {
		if got, want := wrapTokenRune(r), bandTokenRune(r); got != want {
			t.Fatalf("rune %q (U+%04X): wrapTokenRune=%v, bandTokenRune=%v — the wrap side and the band side disagree about what one token is, which is the class of defect this epic repairs",
				r, r, got, want)
		}
	}
	// The measured, deliberate exception, stated as a decision: `-` welds number
	// ranges into one token and shrinks a frame the reader has already seen, so
	// neither side admits it.
	if wrapTokenRune('-') {
		t.Fatalf("`-` must not be a token interior: welding `00-01 00-02` into one token makes an already-painted frame SHRINK (see bandTokenSoftPunct's measurement)")
	}
	if !wrapTokenRune('/') {
		t.Fatalf("`/` must be a token interior, or `s//tyle` is halved again")
	}
}

// TestWrapToCols_TokenTooWideForARowStillGetsTheCellCut is the invariant the
// whole-token move must NOT break: a token wider than the row has no wider row
// to move to, so the cell cut remains the only lossless answer — and a row is
// never emptied to make the move.
func TestWrapToCols_TokenTooWideForARowStillGetsTheCellCut(t *testing.T) {
	const width = 8
	for _, in := range []string{
		"ab s//tyleteleteletele",
		"中文s//tyle" + strings.Repeat("x", 20),
		strings.Repeat("/", 30),
	} {
		got := WrapToCols(in, width)
		if joined := strings.Join(got, ""); joined != in {
			t.Fatalf("WrapToCols(%q, %d) rows carry %q, want the input back", in, width, joined)
		}
		for i, row := range got {
			if row == "" && len(got) > 1 {
				t.Fatalf("WrapToCols(%q, %d) emitted an empty row %d: %q", in, width, i, got)
			}
			if w := displayWidth(row); w > width && utf8.RuneCountInString(row) > 1 {
				t.Fatalf("WrapToCols(%q, %d) row %d is %d cells: %q", in, width, i, w, row)
			}
		}
	}
}

// TestWrapToCols_SGRRowKeepsTheCellCut pins the SGR exception: a row carrying a
// control sequence may not break at a BOUNDARY at all (a boundary break re-reads
// the text past it on the next row, so the sequence's effect would start on a row
// it was not written on). The whole-token question sits INSIDE that arm, so it
// must be unreachable for an `esc` row — the row keeps its cell cut.
func TestWrapToCols_SGRRowKeepsTheCellCut(t *testing.T) {
	const (
		in    = "\x1b[1m这个 endpoint 的处理在 s//tyle 里面\x1b[0m"
		width = 28
	)
	got := WrapToCols(in, width)
	if joined := strings.Join(got, ""); joined != in {
		t.Fatalf("WrapToCols(%q, %d) rows carry %q, want the input back", in, width, joined)
	}
	for i, row := range got {
		if w := displayWidth(row); w > width && utf8.RuneCountInString(row) > 1 {
			t.Fatalf("WrapToCols(%q, %d) row %d is %d cells: %q", in, width, i, w, row)
		}
	}
	// The token question must not have moved a cut on this row: the row that
	// carries the sequence ends at the CELL edge, exactly as before the fix.
	if len(got) < 2 {
		t.Fatalf("WrapToCols(%q, %d) = %q — expected the cell cut to still split this row", in, width, got)
	}
}
