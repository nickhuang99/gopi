package lineedit

import (
	"strings"
	"testing"
)

// wrap_word_boundary_test.go — the wrap policy breaks at a WORD boundary before
// it breaks at a cell boundary.
//
// WHY THIS EXISTS. A row of the answer is a COPYING surface: the operator reads
// it, and copies the token out of it. A row that ends `COMMANDS.` with `md`
// starting the row below carries a token that no longer runs when pasted — the
// grid is the deliverable, so a cell-boundary break INSIDE a word is a defect,
// not a cosmetic choice.
//
// THE RULE (one policy, see wrapCols): a row ends where the SOURCE already
// offers a boundary — a space or tab, or one of `/ - _ ? & = , ; ) ]`, or
// between two wide (CJK) runes, which do not separate their words with spaces —
// and only a single token that does not fit a whole row by itself is cut at a
// cell boundary. No character is ever dropped, added or reordered: the rows
// reassemble to the input, byte for byte. A `.` is deliberately NOT a boundary:
// it is what holds `COMMANDS.md` and `v1.2.3` together.

// TestWrapCols_KeepsADottedNameWhole is the operator's own sighting: a filename
// with a dot in it is ONE token, so a width that cannot hold it on the current
// row moves it to the next row whole rather than splitting it at the dot.
func TestWrapCols_KeepsADottedNameWhole(t *testing.T) {
	const (
		in    = "see the COMMANDS.md file"
		width = 12
	)
	got := wrapCols(in, width)
	want := []string{"see the ", "COMMANDS.md ", "file"}
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

// TestWrapCols_KeepsPathComponentsWhole: a path breaks after its separators, so
// every row of it is a run of whole components — a copy of a row still names
// something that was in the answer.
func TestWrapCols_KeepsPathComponentsWhole(t *testing.T) {
	const (
		in    = "/home/user/.gopi/sessions/01a1"
		width = 20
	)
	got := wrapCols(in, width)
	want := []string{"/home/user/.gopi/", "sessions/01a1"}
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

// TestWrapCols_HardCutsOnlyATokenTooWide is the one remaining cell-boundary
// break: a single 200-cell token with no boundary in it at all cannot be moved
// to a wider row, so it is cut — and the cut rows still reassemble to the token
// exactly, every byte once, in order.
func TestWrapCols_HardCutsOnlyATokenTooWide(t *testing.T) {
	const width = 20
	in := strings.Repeat("x", 200)
	got := wrapCols(in, width)
	if len(got) != len(in)/width {
		t.Fatalf("wrapCols(200 cells, %d) = %d rows, want %d: %q", width, len(got), len(in)/width, got)
	}
	for i, row := range got {
		if w := displayWidth(row); w != width {
			t.Fatalf("row %d = %d cells, want a full %d-cell row: %q", i, w, width, row)
		}
	}
	if joined := strings.Join(got, ""); joined != in {
		t.Fatalf("the cut rows carry %q, want the token %q", joined, in)
	}
}

// TestWrapCols_CJKBreaksBetweenWideRunes pins the wide-rune half of the rule: a
// row may break anywhere BETWEEN two wide runes (those scripts carry no spaces),
// never inside one — a half-drawn wide rune would cost a cell that is not there
// — and the rows reassemble to the sentence.
func TestWrapCols_CJKBreaksBetweenWideRunes(t *testing.T) {
	const (
		width = 41 // twenty wide runes (40 cells); the 21st must start the next row
		in    = "这是一段很长的中文说明文字用来验证宽字符不会被切成半个而且拼接回去等于原文"
	)
	got := wrapCols(in, width)
	var joined strings.Builder
	for i, row := range got {
		if w := displayWidth(row); w > width {
			t.Fatalf("row %d is %d cells, wider than %d: %q", i, w, width, row)
		}
		if w := displayWidth(row); w%2 != 0 {
			t.Fatalf("row %d is %d cells — an odd width over pure CJK text means a wide rune was halved: %q", i, w, row)
		}
		joined.WriteString(row)
	}
	if joined.String() != in {
		t.Fatalf("the rows carry %q, want the input %q", joined.String(), in)
	}
}

// TestWrapCols_JoinsBackAfterABoundaryBreak is the invariant every break above
// shares, stated once: boundary breaks add row breaks and nothing else.
func TestWrapCols_JoinsBackAfterABoundaryBreak(t *testing.T) {
	ins := []string{
		"target=/home/user/workspace/01a15629-211d-741e-b9d4-a0895abf4c22",
		"run make test-budget PKGS='./internal/lineedit' && git status",
		"the project's COMMANDS.md lists it (see [the docs])",
		"混合中文 and ascii tokens - with dashes_and_underscores here",
	}
	for _, in := range ins {
		// Width 1 is deliberately absent: a two-cell rune cannot fit it at all, so
		// that row is the emitted-alone fallback, not a policy statement.
		for _, width := range []int{2, 3, 7, 16, 33, 40} {
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
}
