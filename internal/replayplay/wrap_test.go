package replayplay

// wrap_test.go — A REPLAY OF A WORKER SESSION LOSES NO TEXT.
//
// THE DEFECT THIS FILE PINS (measured 2026-10-28, task 01a18269, session
// 93043087 — a WORKER run, so the material is the session log and there is no
// screen dump to be cell-exact about).
//
// `gopi session replay <id> --turn 1` laid a row's text out at the FULL canvas
// width and only THEN prefixed its continuation rows with the block's mark
// (`↳ `). The mark pushed the wrapped tail two cells past the canvas, and
// fitCells — which cannot tell an over-wide row from a long text — silently cut
// the text to make room for the mark. At 200 columns the operator's own tool
// result came out as:
//
//	↳ … carries no commit to
//	↳ thing to push. This is NOT a refused pull …
//
// The session log says "… carries no commit to deliver: `rev-list --count HEAD
// --not origin/master` answered 0 against origin/master, so there is nothing to
// pull and nothing to push." The words "no" and "deliver: … so there is nothing
// to pull and" existed in NO row at all. That is content LOSS, not wrapping: the
// replay of a worker run showed truncated sentences, and nothing anywhere said
// so (Rule 15).
//
// WHAT IS PINNED HERE, in three arms:
//
//  1. NO TEXT IS LOST — for a range of canvas widths and both prefix kinds, the
//     rows of a carried block, stripped of their marks, concatenate back to the
//     source text. This is the property, not one magic width.
//  2. THE MEASURED SENTENCE SURVIVES at 200 columns, whole, in the reader's own
//     words — the arm a regression will actually fail.
//  3. A DUMP'S CELL GRID IS UNTOUCHED — a chat surface records the screen it
//     painted, and its replay must stay cell-exact on the recorded geometry. The
//     wrap belongs to the LOG material only.

import (
	"strings"
	"testing"
)

// The sentence the operator's session recorded, and the two fragments the
// pre-fix replay split it into across a mark (see the file comment). They are
// quoted rather than generated: this is the evidence the fix is judged on.
const (
	wrapFixtureSentence = "carries no commit to deliver: `rev-list --count HEAD --not origin/master` answered 0 against origin/master, so there is nothing to pull and nothing to push."
	wrapFixtureLostAt   = "nothing to pull and thing to push"
)

// TestWrapPrefixedLosesNoText is the property arm: whatever the width, the rows
// carry the text. The marks are stripped before the comparison exactly the way a
// reader's eye strips them — the first row wears the caller's own first prefix,
// every later row wears the continuation mark.
func TestWrapPrefixedLosesNoText(t *testing.T) {
	const text = wrapFixtureSentence
	for _, cols := range []int{20, 32, 40, 57, 80, 117, 200, 233} {
		rows := wrapPrefixed(text, "", roundResultLabel, cols)
		if len(rows) == 0 {
			t.Fatalf("cols=%d: no rows at all", cols)
		}
		for _, r := range rows {
			if w := cellsOf(r); w > cols {
				t.Errorf("cols=%d: a row is %d cells wide (%q) — fitCells would cut its text", cols, w, r)
			}
		}
		// Reassemble: ride over the continuation marks the wrapper itself wrote,
		// and compare with all whitespace removed. A break AT a space consumes
		// that one space — the row ended on a whole word, so the space is not
		// needed — and that is the ONLY thing a break may cost: every non-space
		// character must still be there, which is exactly what the pre-fix replay
		// violated ("no" sat between two rows and existed in neither).
		var b strings.Builder
		for i, r := range rows {
			if i > 0 {
				if !strings.HasPrefix(r, roundResultLabel) {
					t.Fatalf("cols=%d: row %d does not open with the continuation mark: %q", cols, i, r)
				}
				r = strings.TrimPrefix(r, roundResultLabel)
			}
			b.WriteString(r)
		}
		got := strings.Join(strings.Fields(b.String()), "")
		want := strings.Join(strings.Fields(text), "")
		if got != want {
			t.Errorf("cols=%d: the rows do not reassemble into the text\n got: %s\nwant: %s", cols, got, want)
		}
	}
}

// TestWrapPrefixedKeepsTheMeasuredSentence is the arm the real regression failed:
// at the width the operator's run used, the words between the two rows are
// still there. A wrapper that pins its continuation on AFTER the wrap produces
// the fixture's "nothing to pull and thing to push" — one word short, and
// grammatically plausible, which is why nobody noticed at first.
func TestWrapPrefixedKeepsTheMeasuredSentence(t *testing.T) {
	rows := wrapPrefixed(wrapFixtureSentence, "", roundResultLabel, 200)
	joined := strings.Join(rows, "\n")
	if strings.Contains(joined, wrapFixtureLostAt) {
		t.Fatalf("the replay still drops a word across a row mark:\n%s", joined)
	}
	if !strings.Contains(strings.ReplaceAll(joined, roundResultLabel, ""), "nothing to pull and nothing to push") {
		t.Fatalf("the measured sentence is not whole in the rows:\n%s", joined)
	}
}

// TestWrapPrefixedKeepsExplicitNewlines pins the shape the old wrapper had: an
// embedded newline starts a new paragraph, an empty paragraph is a row of its
// own, and every row after the FIRST of the text wears the continuation mark —
// so a multi-paragraph tool result cannot look like several blocks.
func TestWrapPrefixedKeepsExplicitNewlines(t *testing.T) {
	rows := wrapPrefixed("one\n\ntwo", "", roundResultLabel, 80)
	want := []string{"one", roundResultLabel, roundResultLabel + "two"}
	if len(rows) != len(want) {
		t.Fatalf("rows = %#v, want %#v", rows, want)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, rows[i], want[i])
		}
	}
}

// TestWrapPrefixedDegenerateCanvasKeepsText is the Rule-15 arm: a canvas the mark
// alone would fill must lose no text and print nothing empty. The mark is
// dropped — announced by the row itself, which then carries content — rather
// than eating a character per row.
func TestWrapPrefixedDegenerateCanvasKeepsText(t *testing.T) {
	rows := wrapPrefixed("abcdef", "", roundResultLabel, 1)
	if strings.Join(rows, "") != "abcdef" {
		t.Fatalf("rows = %#v, want the text character by character", rows)
	}
	if got := wrapPrefixed("x", "", roundResultLabel, 0); got != nil {
		t.Fatalf("cols=0 answered %#v, want nil (a canvas of no width lays out nothing)", got)
	}
}

// TestTurnBodyRowsCarryTheWholePromptAtTheOperatorsWidth drives the PUBLIC
// lay-out the static view prints (`--turn N`), not the wrapper: a turn whose
// prompt is the measured sentence must print it whole at the operator's 200
// columns AND at the 80-column fallback. This is the arm that reproduces the
// operator's own `--turn 1` invocation, and it is RED on the pre-fix tree:
//
//	PROMPT: … so there is nothing to pull and nothing to
//	↳ ush.                                      ← "push" lost its opening cell
//
// The pair of rows is the defect: a break AT a space consumed the space and then
// the mark cost one more cell, and fitCells took it out of the TEXT.
func TestTurnBodyRowsCarryTheWholePromptAtTheOperatorsWidth(t *testing.T) {
	for _, cols := range []int{80, 117, 200} {
		turn := Round{Label: "turn 1/1", Prompt: wrapFixtureSentence}
		rows := TurnBodyRows(turn, cols)
		// Ride over the continuation marks the lay-out itself wrote — the same
		// way a reader's eye does — so the comparison is about the TEXT.
		var b, joined strings.Builder
		for i, r := range rows {
			b.WriteString(r.Cells)
			b.WriteString("\n")
			cell := r.Cells
			if i > 0 {
				cell = strings.TrimPrefix(cell, roundResultLabel)
			}
			joined.WriteString(cell)
		}
		body := b.String()
		if strings.Contains(body, wrapFixtureLostAt) {
			t.Fatalf("cols=%d: TurnBodyRows drops a word across a row mark:\n%s", cols, body)
		}
		got := strings.Join(strings.Fields(joined.String()), "")
		want := strings.Join(strings.Fields(roundPromptLabel+wrapFixtureSentence), "")
		if got != want {
			t.Fatalf("cols=%d: the prompt does not round-trip through the rows\n got: %s\nwant: %s", cols, got, want)
		}
		for _, r := range rows {
			if w := cellsOf(r.Cells); w > cols {
				t.Fatalf("cols=%d: a row is %d cells wide: %q", cols, w, r.Cells)
			}
		}
	}
}

// TestTurnBodyRowsCarryTheWholeResultAtTheOperatorsWidth drives the PUBLIC
// lay-out the static view prints (`--turn N`), not the wrapper: a turn whose
// tool result is the measured sentence must print it whole at 200 columns.
// This is the arm that reproduces the operator's own `--turn 1` invocation.
func TestTurnBodyRowsCarryTheWholeResultAtTheOperatorsWidth(t *testing.T) {
	turn := Round{
		Label: "turn 1/1",
		Calls: []ToolCallView{{
			Name:     "bash",
			InputRaw: `echo hi`,
			Result:   wrapFixtureSentence,
		}},
	}
	rows := TurnBodyRows(turn, 200)
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(r.Cells)
		b.WriteString("\n")
	}
	body := b.String()
	if strings.Contains(body, wrapFixtureLostAt) {
		t.Fatalf("TurnBodyRows still drops a word across a row mark:\n%s", body)
	}
	if !strings.Contains(body, "nothing to pull and nothing to push") {
		t.Fatalf("the tool result is not whole in the turn body:\n%s", body)
	}
	for _, r := range rows {
		if w := cellsOf(r.Cells); w > 200 {
			t.Fatalf("a row is %d cells at cols=200: %q", w, r.Cells)
		}
	}
}
