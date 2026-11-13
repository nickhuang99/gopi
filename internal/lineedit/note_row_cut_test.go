package lineedit

import (
	"strings"
	"testing"
)

// note_row_cut_test.go — the NOTE-ROW CUT contract: every row boundary a ⚙
// call row prints is either a break the TEXT itself offers, or it SAYS it was
// cut.
//
// The defect these tests pin (re-measured on master 2044ae98, a real 117x26 pty;
// session 01a0e4c7's evidence/grid-before.txt): the operator's bash one-liner is
// a chain of QUOTED arguments —
//
//	cd ~/work/GoPi ; echo "=== branch ===" ; echo "=== worktrees (the + prefix) ===" ; …
//
// — and the row cutter broke at the last SPACE inside its budget, which was a
// space INSIDE `"=== worktrees (the + prefix) ==="`. Because the break was a
// boundary, no marker was printed either:
//
//	000|⚙ call bash cd ~/work/GoPi ; echo "=== branch ===" ; echo "=== status ===" ; echo "===
//	001|↳ worktrees (the + prefix) ===" ; echo "=== does the branch ex…
//
// The first row ends inside a quoted argument while 48 cells of the second sat
// UNUSED: the row could have carried the rest, and a reader (or a copy-paste)
// gets a fragment of a shell word either way. This is the same shape the
// operator's `echo "===` row reported, one cell budget further along.
//
// The rule asserted here, in the order it can break:
//
//  1. A row never ends inside a QUOTED run: a space inside `"…"` or `'…'` is text,
//     not a break.
//  2. A row NEVER wears the cut marker (NoteCutMarker): the rows are lossless —
//     the tail continues on the next row, which is what NoteContinuation says —
//     so a marker would stand for text that is not missing while occupying a cell
//     of the command the reader copies (the 112-column grid: `-ma…` | `xdepth`,
//     which pastes as `-ma…xdepth` and runs nowhere). The status row's vocabulary
//     keeps the glyph; the note path stopped drawing it.
//  3. No characters are lost across the rows: strip the continuation gutter, join
//     them, and the note is back (a space break consumes the space it broke on,
//     so the join is compared with whitespace ignored).
//  4. The block's glyphs are DOCUMENTED: NoteRowLegend names the constant in use,
//     so `↳` is a defined symbol rather than an invention.

// noteRowCutCommand is the operator's own command shape — a chain of quoted
// arguments — long enough to wrap the harness's 48-cell band several times.
const noteRowCutCommand = `echo "=== status ===" ; echo "=== worktrees (the + prefix) ===" ; ` +
	`echo "=== does the branch exist anywhere locally? ===" ; echo fix/pushback-rule31-evidence`

// noteRowCutRows commits the note and returns the rows it occupied, plus the
// note as it was handed to the band.
func noteRowCutRows(t *testing.T, note string, tail string) []string {
	t.Helper()
	h := newStatusHarness(t)
	h.ed.SetPalette(BasicPalette)
	h.ed.Note(note, BasicPalette.ToolCall)
	// Wait for a string the LAST row carries, so the whole block is committed
	// before the grid is read (the caller names it: the note's own tail is what
	// proves the wrap reached the end).
	h.waitContents(t, tail)
	h.replay()
	rows := noteWrapRows(h, note)
	if len(rows) < 2 {
		t.Fatalf("the note rendered %d row(s) (%q) — want a wrapped block", len(rows), rows)
	}
	return rows
}

// checkNoteRowBreaks reads each row against the note's own text: it locates the
// row's body in the note at or after the previous row's end, and asserts two
// things about every row boundary — the row carries NO cut marker (the note path
// is lossless, see the file header), and no character of the note is lost between
// the rows. The only text a boundary may consume is the single SPACE it breaks on
// (each row is drawn on its own line, so no cell has to carry it).
//
// This is deliberately a check on the SCREEN'S rows against the SOURCE TEXT
// rather than a comparison against the cutter's own output: a test that asked
// the fix what it did would pass for any cut the fix chose.
func checkNoteRowBreaks(t *testing.T, note string, rows []string) {
	t.Helper()
	var joined strings.Builder
	for i, row := range rows {
		body := row
		if strings.HasPrefix(body, noteWrapGutter) {
			body = strings.TrimPrefix(body, noteWrapGutter)
		}
		if strings.Contains(row, NoteCutMarker) {
			t.Errorf("row %d wears the cut marker %q: %q\n  the note path is LOSSLESS — the tail continues on the next row, so the marker stands for nothing while sitting in a cell of the command the reader copies",
				i, NoteCutMarker, row)
		}
		joined.WriteString(body)
	}
	// The marker would hide a lost character from this comparison, so the
	// assertion above is not decoration: run it first, then check the text.
	if strings.ContainsRune(joined.String(), []rune(NoteCutMarker)[0]) {
		t.Fatalf("the rows carry the cut marker %q, so no lossless comparison can be made:\n%q", NoteCutMarker, joined.String())
	}
	if got, want := noSpaces(joined.String()), noSpaces(note); got != want {
		t.Errorf("the cut rows dropped or altered characters\n got: %q\nwant: %q", got, want)
	}
}

// insideQuotes reports whether byte offset i of s lies inside a quoted run,
// tracking `"` and `'` from the start (a backslash-escaped byte is always text).
func insideQuotes(s string, i int) bool {
	inside := false
	var quote byte
	for j := 0; j < i; j++ {
		c := s[j]
		switch {
		case c == '\\':
			j++
		case inside:
			if c == quote {
				inside = false
			}
		case c == '"' || c == '\'':
			inside, quote = true, c
		}
	}
	return inside
}

// TestNoteRowBreaksOutsideQuotedArguments is the epic's RED case: the break must
// not land inside a quoted shell argument.
func TestNoteRowBreaksOutsideQuotedArguments(t *testing.T) {
	note := ToolGlyph + noteWrapShellPrefix + noteRowCutCommand
	if displayWidth(note) <= noteWrapRowWidth {
		t.Fatalf("the fixture note is %d cells, not wider than the %d-cell band", displayWidth(note), noteWrapRowWidth)
	}
	rows := noteRowCutRows(t, note, "pushback-rule31-evidence")
	checkNoteRowBreaks(t, note, rows)

	// The specific shape: no row may END inside a quoted run EXCEPT where the next
	// row takes over from that very byte. A barrier test cannot see that open-ended
	// quoted arguments do not exist: the note's own text can end a shell word
	// inside a quote, and then the token's remainder is on the row below and the
	// two join byte for byte — which is the property this file asserts, and the
	// only one that matters for a paste.
	for i, row := range rows {
		body := strings.TrimPrefix(row, noteWrapGutter)
		end := strings.Index(note, strings.TrimRight(body, " "))
		if end < 0 {
			continue
		}
		end += len(strings.TrimRight(body, " "))
		if end >= len(note) || !insideQuotes(note, end) {
			continue
		}
		next := ""
		if i+1 < len(rows) {
			next = strings.TrimPrefix(rows[i+1], noteWrapGutter)
		}
		if !strings.HasPrefix(next, note[end:]) {
			t.Errorf("row %d ends INSIDE a quoted argument and row %d does not take over from that byte: %q / %q\n  the reader copies a fragment of a shell word",
				i, i+1, row, next)
		}
	}
}

// TestNoteRowCutIsNeverSilent is the general half, on a hostile row: a single
// quoted token with no space outside it at all. On master the cutter fell through
// to its marked word cut here ("the row says it was cut"); now there is no marker
// to fall back on, so the row hands its remainder to the row below and the rows
// join back into the note, byte for byte. "Never silent" is satisfied the other way
// round: the reader is never TOLD something was dropped, because nothing ever is.
func TestNoteRowCutIsNeverSilent(t *testing.T) {
	note := ToolGlyph + ` call bash echo "` + strings.Repeat("quotedword", 8) + `"`
	rows := noteRowCutRows(t, note, `"`)
	checkNoteRowBreaks(t, note, rows)

	// NOTHING LOST, BYTE FOR BYTE: this note is a single quoted word with no space
	// outside it, so a lossless cutter must reproduce it character for character
	// once the gutter is stripped — the same join a reader's selection performs.
	var joined strings.Builder
	for _, row := range rows {
		if strings.Contains(row, NoteCutMarker) {
			t.Fatalf("row %q wears the cut marker %q — the note path draws no marker at all", row, NoteCutMarker)
		}
		joined.WriteString(strings.TrimPrefix(row, noteWrapGutter))
	}
	if got, want := joined.String(), note; got != want {
		t.Errorf("the cut rows are not the note, byte for byte\n got: %q\nwant: %q", got, want)
	}
}

// TestNoteRowLegendNamesEveryGlyph is the DOCUMENTATION half of the epic: the
// continuation marker the operator's bug row called undefined is a row of the
// activity block's own glyph table, and every glyph that table names is the
// constant the renderer draws with (so the table cannot drift from the screen).
func TestNoteRowLegendNamesEveryGlyph(t *testing.T) {
	for _, want := range []struct {
		glyph string
		what  string
	}{{ToolGlyph, "the fresh-note glyph"},
		{NoteContinuation, "the continuation gutter"},
		{NoteCutMarker, "the cut marker"},
	} {
		if !strings.Contains(NoteRowLegend, want.glyph) {
			t.Errorf("NoteRowLegend does not name %s (%q):\n%s", want.what, want.glyph, NoteRowLegend)
		}
	}
	// The legend's own escape hatch against a second spelling: the glyphs in it
	// must be the constants, so a renamed constant cannot leave the table behind.
	if strings.Count(NoteRowLegend, NoteContinuation) != 1 {
		t.Errorf("NoteRowLegend mentions the continuation gutter %d times, want exactly 1 (one row per glyph)",
			strings.Count(NoteRowLegend, NoteContinuation))
	}
}

// min is a helper for the windowed failure message above.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
