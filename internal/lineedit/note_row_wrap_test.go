package lineedit

import (
	"strings"
	"testing"
)

// note_row_wrap_test.go — the CALLER-ROW WIDTH contract (what a note row does
// when its text is wider than the band).
//
// The defect these tests pin (the operator's chat dump): a tool-activity row is
// a real command, and it is frequently longer than the terminal. On master the
// row was cut at the row's own last cell with NO boundary and NO marker, so the
// transcript read
//
//	⚙ call bash cd ~/work/GoPi; echo "=== s
//	tatus ==="; git status -sb | head -20
//
// — a token (here the shell word `status`) split across two rows, and the
// continuation carrying the same blank 2-cell indent every fresh note row has.
// Copying the first row does not run, and copying the second produces garbage:
// the transcript stopped recording WHAT RAN, which is the one thing it exists
// for (see Editor.Note's own doc). The status row already refuses exactly this
// shape — a field it must shorten ends with statusEllipsis, so a shortened field
// reads as continuing rather than as corrupt data.
//
// The policy asserted here, in the order the properties can break:
//
//  1. CUT AT A BOUNDARY — the head of a wrapped note row ends where the text has
//     a break (space, or a long token's own separator), never in the middle of an
//     identifier, path or word;
//  2. MARKED CONTINUATION — the next row begins with the note continuation
//     gutter, so a continuation is distinguishable at a glance from a fresh
//     `⚙ …` row (and from the turn body's own indent);
//  3. NOTHING IS LOST — every rune of the command is on the screen, in order: the
//     rows' texts, concatenated with the gutter stripped, are the note. On the
//     row that had no boundary to break at this is byte for byte rather than
//     "after normalization", and the row wears NO cut marker: the note path draws
//     no `…` at all, because a marker inside the command's own cells is a
//     character the reader pastes into a shell (see note_row_cut_test.go and
//     copied_text_test.go);
//  4. the SAME rule for CJK (a wide rune is never split, and the cell budget is
//     counted in cells rather than bytes);
//  5. a note that FITS is drawn unchanged, on one row, with no gutter.

const (
	// noteWrapShellCommand is the operator's own row: the `=== s` / `tatus ===`
	// split, verbatim (one space where the dump's extract had a newline).
	noteWrapShellCommand = `echo "=== status ==="; git status -sb | head -20`
	// noteWrapShellPrefix is what cmd/agent puts in front of a bash call's
	// argument ("⚙ call bash …"), so the row under test is the caller's real
	// shape rather than the bare command.
	noteWrapShellPrefix = " call bash "
	// noteWrapShellTail is the last word of noteWrapShellCommand: the note is
	// committed by the read loop asynchronously, so a test waits for a string the
	// row's OWN tail carries rather than for the whole row.
	noteWrapShellTail = "head -20"
	// noteWrapLongToken is a single token LONGER than the harness's row width
	// with separator characters in it (a branch list, a path): there is no space
	// to break on, so the row cutter has to pick a point out of the token's own
	// character classes.
	noteWrapLongToken = "git branch --list fix/pushback-rule31-evidence refs/heads/archive/x"
	// noteWrapCJK is a long CJK row: two cells per rune, so a byte-counted cut
	// splits it at a different place than a cell-counted one (Rule 10.2 — the
	// cell grid is the ground truth, not the byte count).
	noteWrapCJK = "⚙ call bash 这是很长的中文命令行文本内容用来验证宽字符换行不丢失任何字符并且不拆开"
)

// noteWrapGutter is the marker a continuation of a note row carries, spelled
// HERE rather than read from the package: the assertion is about what the SCREEN
// shows, and a test that read the fix's own constant would keep passing if the
// fix renamed the marker to the empty string.
const noteWrapGutter = "↳ "

// noSpaces removes every space from s, so a comparison can be about characters
// rather than about the cells a boundary break consumed.
func noSpaces(s string) string { return strings.ReplaceAll(s, " ", "") }

// noteWrapRowWidth is the widest a rendered row is allowed to be: the band's own
// width (#1's harness is statusTestCols wide). A row wider than this is the
// one-screen-row defect wearing a note's clothes.
const noteWrapRowWidth = statusTestCols

// lastToken returns s's trailing run of non-space characters — the token a
// mid-token cut would leave dangling at the end of a row.
func lastToken(s string) string {
	return s[len(s)-len(trailingToken(s)):]
}

// trailingToken is lastToken's helper: the longest suffix of s with no space.
func trailingToken(s string) string {
	i := len(s)
	for i > 0 && s[i-1] != ' ' {
		i--
	}
	return s[i:]
}

// noteWrapRows replays the harness's write log and returns the screen rows the
// note occupies, top to bottom: the row that carries the glyph plus every
// non-empty row after it that is not the next tool row and not the editing row —
// which is exactly what a reader sees as the note's block, and which cannot
// depend on whether the fix marked the continuation (the RED run's rows must be
// captured the same way as the GREEN run's, or the failure would be about the
// harness rather than about the defect).
func noteWrapRows(h *statusHarness, want string) []string {
	h.replay()
	var rows []string
	collecting := false
	for i := 0; i < statusTestRows; i++ {
		line := strings.TrimRight(h.screen.Line(i), " ")
		switch {
		case strings.Contains(line, ToolGlyph):
			// The note's first row: it starts the block (the harness commits no
			// other tool row before it).
			collecting = true
			rows = append(rows, strings.TrimLeft(line, " "))
		case !collecting:
		case line == "":
			collecting = false
		default:
			rows = append(rows, line)
		}
	}
	return rows
}

// TestNoteRowWiderThanTheBandKeepsItsTokensWhole is the RED test of the epic.
// The row is the operator's own long bash command; a wrapped row must end at a
// boundary and its continuation must be MARKED — on master it ends mid-token
// (`=== s`) and the continuation is indistinguishable from a fresh row.
func TestNoteRowWiderThanTheBandKeepsItsTokensWhole(t *testing.T) {
	h := newStatusHarness(t)
	h.ed.SetPalette(BasicPalette)

	note := ToolGlyph + noteWrapShellPrefix + noteWrapShellCommand
	if displayWidth(note) <= noteWrapRowWidth {
		t.Fatalf("the fixture note is %d cells wide, not wider than the %d-cell band — this test would prove nothing",
			displayWidth(note), noteWrapRowWidth)
	}
	h.ed.Note(note, BasicPalette.ToolCall)
	h.waitContents(t, noteWrapShellTail)
	h.replay()

	rows := noteWrapRows(h, note)
	if len(rows) < 2 {
		t.Fatalf("the wrapped note rendered %d row(s) (%q) — want at least 2", len(rows), rows)
	}
	// (a) every row fits the band.
	for i, row := range rows {
		if w := displayWidth(row); w > noteWrapRowWidth {
			t.Errorf("row %d is %d cells wide, over the %d-cell band: %q", i, w, noteWrapRowWidth, row)
		}
	}
	// (b) no row ends in the middle of a token: the head's trailing token is also
	// a token of the note's own text, not a prefix of one.
	head := rows[0]
	if tok := lastToken(strings.TrimRight(head, " ")); tok != "" && !strings.HasSuffix(note, tok) {
		if !strings.Contains(note, tok+" ") && !strings.Contains(note, tok+"\n") {
			t.Errorf("the first row ends mid-token: %q ends with %q, which is a PREFIX of a longer token in %q — a copied line does not run",
				head, tok, note)
		}
	}
	// (c) the continuation is MARKED: it starts with the note continuation
	// gutter, so it cannot be mistaken for a fresh `⚙ …` row.
	for i := 1; i < len(rows); i++ {
		if !strings.HasPrefix(rows[i], noteWrapGutter) {
			t.Errorf("continuation row %d = %q, want it to start with the note continuation gutter %q — an unmarked continuation is exactly the defect (the reader cannot tell it from a fresh row)",
				i, rows[i], noteWrapGutter)
		}
	}
	// (d) nothing was lost: stripping the gutter and joining the rows back
	// reproduces the note's text (whitespace-normalized, since a boundary break
	// may consume the space it broke on).
	var joined strings.Builder
	for _, row := range rows {
		joined.WriteString(" ")
		joined.WriteString(strings.TrimPrefix(row, noteWrapGutter))
	}
	if got, want := strings.Join(strings.Fields(joined.String()), " "), strings.Join(strings.Fields(note), " "); got != want {
		t.Errorf("the wrapped rows do not reconstruct the note\n got: %q\nwant: %q", got, want)
	}
}

// TestNoteRowLongTokenBreaksAtASeparator is the same contract for a token there
// is no space in: the row cutter must pick a break out of the token's own
// separator characters, and the continuation must not begin with the rest of an
// identifier that started on the row above.
func TestNoteRowLongTokenBreaksAtASeparator(t *testing.T) {
	h := newStatusHarness(t)
	h.ed.SetPalette(BasicPalette)

	note := ToolGlyph + noteWrapShellPrefix + noteWrapLongToken
	h.ed.Note(note, BasicPalette.ToolCall)
	h.waitContents(t, "archive/x")
	h.replay()

	rows := noteWrapRows(h, note)
	if len(rows) < 2 {
		t.Fatalf("the long-token note rendered %d row(s) (%q) — want at least 2", len(rows), rows)
	}
	for i := 1; i < len(rows); i++ {
		if !strings.HasPrefix(rows[i], noteWrapGutter) {
			t.Fatalf("continuation row %d = %q, want the note continuation gutter %q", i, rows[i], noteWrapGutter)
		}
		body := strings.TrimPrefix(rows[i], noteWrapGutter)
		// A continuation must not begin with the BARE remainder of a word that
		// started on the row above: the break has to fall at one of the token's
		// own separators (or the row must say it was cut, with the marker).
		if strings.HasPrefix(body, "-") || strings.HasPrefix(body, ".") {
			t.Errorf("row %d starts with a bare separator fragment %q — the break was taken mid-separator", i, body)
		}
	}
}

// TestNoteRowCJKNeverSplitsAWideRune is the CJK regression guard: a wide rune
// straddling the boundary starts the next row rather than being half-drawn, and
// the row budget is counted in CELLS — a byte-counted cut would break the row at
// a different place and could split a three-byte UTF-8 rune in half.
func TestNoteRowCJKNeverSplitsAWideRune(t *testing.T) {
	h := newStatusHarness(t)
	h.ed.SetPalette(BasicPalette)

	h.ed.Note(noteWrapCJK, BasicPalette.ToolCall)
	h.waitContents(t, "不拆开")
	h.replay()

	rows := noteWrapRows(h, noteWrapCJK)
	if len(rows) < 2 {
		t.Fatalf("the CJK note rendered %d row(s) (%q) — want at least 2", len(rows), rows)
	}
	var joined strings.Builder
	for i, row := range rows {
		if i > 0 && !strings.HasPrefix(row, noteWrapGutter) {
			t.Errorf("CJK continuation row %d = %q, want the continuation gutter %q — a wrapped CJK command is a continuation like any other",
				i, row, noteWrapGutter)
		}
		if w := displayWidth(row); w > noteWrapRowWidth {
			t.Errorf("CJK row %d is %d cells wide, over the %d-cell band: %q", i, w, noteWrapRowWidth, row)
		}
		// A row must not carry the replacement character: that is what a rune
		// split across rows (or across a cut) leaves behind.
		if strings.ContainsRune(row, '\uFFFD') {
			t.Errorf("CJK row %d carries the replacement rune — a wide rune was split: %q", i, row)
		}
		joined.WriteString(strings.TrimPrefix(row, noteWrapGutter))
	}
	// The comparison drops SPACES on both sides: a boundary break CONSUMES the
	// space it broke on (each row is drawn on its own line, so no cell has to
	// carry it), and what this test is about is that no CJK CHARACTER was lost or
	// halved.
	if got, want := noSpaces(joined.String()), noSpaces(noteWrapCJK); got != want {
		t.Errorf("the CJK rows lost text\n got: %q\nwant: %q", got, want)
	}
}

// TestNoteRowFittingTheBandIsUnchanged is the degradation contract on the other
// side: a note that fits its row is drawn whole, on ONE row, with NO continuation
// gutter — the marker means "there is more", and it must not appear on a row the
// band in fact shows in full.
func TestNoteRowFittingTheBandIsUnchanged(t *testing.T) {
	h := newStatusHarness(t)
	h.ed.SetPalette(BasicPalette)

	h.ed.Note(noteRowText, BasicPalette.ToolCall)
	h.waitContents(t, noteRowText)
	h.replay()

	rows := noteWrapRows(h, noteRowText)
	if len(rows) != 1 {
		t.Fatalf("a fitting note rendered %d rows (%q), want exactly 1", len(rows), rows)
	}
	if rows[0] != noteRowText {
		t.Errorf("a fitting note was altered: %q, want %q", rows[0], noteRowText)
	}
	if strings.Contains(rows[0], noteWrapGutter) {
		t.Errorf("a fitting note carries the continuation gutter: %q", rows[0])
	}
}

// TestNoteRowTokenWithNoBoundaryIsLosslessAndUnmarked is the MUST-NOT-MARK half,
// and it is the epic's own RED case at the package level: a note whose row ends
// inside ONE word that offers no boundary anywhere in reach (a long hex digest, an
// unbroken URL segment) has no honest break to take, so the row ends the word's cut
// PREFIX and the next row opens with the rest of it — with NO marker, because
// nothing was dropped.
//
// The defect this pins (the operator's 112-column grid): the arm once ended the
// head with the status row's own ellipsis, so the screen read
//
//	009|… && find . -ma…
//	010|xdepth 2 -type f …
//
// — `-ma` | `xdepth` is the single token `-maxdepth`, every character present, but
// the reader SELECTS the two rows off the grid and pastes `-ma…xdepth`, which no
// shell runs. The grid is the artifact (copied_text_test.go), so the assertions
// here are made on the joined rows: byte for byte the note, and not one character
// of the product's cut vocabulary in them.
func TestNoteRowTokenWithNoBoundaryIsLosslessAndUnmarked(t *testing.T) {
	h := newStatusHarness(t)
	h.ed.SetPalette(BasicPalette)

	// One token, no space and no separator: 72 cells of digest, so it cannot fit
	// a 46-cell row and has no boundary inside its budget.
	digest := strings.Repeat("a9f4c2e7b1d8", 6)
	note := ToolGlyph + noteWrapShellPrefix + digest
	if strings.ContainsAny(digest, " /-._=:,") {
		t.Fatalf("the fixture has a boundary in it (%q) — the test would not exercise the no-boundary path", digest)
	}
	h.ed.Note(note, BasicPalette.ToolCall)
	h.waitContents(t, digest[:20])
	h.replay()

	rows := noteWrapRows(h, note)
	if len(rows) < 2 {
		t.Fatalf("an unbreakable 72-cell token rendered %d row(s) (%q) — want at least 2", len(rows), rows)
	}
	// The shape the defect produced: some row ends a PREFIX of the digest (i.e. a
	// cut landed inside the word). Such a row must hand the rest of the word to the
	// row below and wear nothing.
	cut := 0
	for i, row := range rows {
		tok := lastToken(strings.TrimRight(strings.TrimPrefix(row, noteWrapGutter), " "))
		if tok == "" || tok == digest || !strings.HasPrefix(digest, tok) {
			continue
		}
		cut++
		if strings.Contains(row, noteWrapEllipsis) {
			t.Errorf("row %d carries the cut marker %q: %q\n  the token's remainder opens the row below, so the marker stands for nothing — and a reader who selects these rows copies it into the command",
				i, noteWrapEllipsis, row)
		}
	}
	if cut == 0 {
		t.Fatalf("no row ended inside the digest (%q) — the fixture did not exercise the no-boundary path", rows)
	}
	// NOTHING IS LOST, BYTE FOR BYTE: the note has no spaces, so the join of the
	// rows (gutter stripped) is comparable character for character rather than
	// "after normalization" — which is exactly what a paste does.
	var joined strings.Builder
	for _, row := range rows {
		joined.WriteString(strings.TrimPrefix(row, noteWrapGutter))
	}
	if got, want := joined.String(), note; got != want {
		t.Errorf("the wrapped rows are not the note, byte for byte\n got: %q\nwant: %q", got, want)
	}
}

// noteWrapEllipsis is the marker a cut field carries, spelled here for the same
// reason noteWrapGutter is: the assertion is about what the reader sees.
const noteWrapEllipsis = "…"
