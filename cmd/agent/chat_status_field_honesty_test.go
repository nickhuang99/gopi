package main

// chat_status_field_honesty_test.go — the status row may not reserve a field and
// leave it UNREADABLE, and it may not print a zero-looking duration for a turn
// that was not zero.
//
// THE OPERATOR'S OWN ROW is the fixture (the last line of the /tmp/error.txt dump
// that filed this defect — a turn of 5 iterations and 14 bash calls):
//
//	deepseek-flash ·  turn 5  ·  tools ·                   ·  ctx 15.4k/200.0k    0s · ready
//	                ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
//	                19 cells of blank: the tool column, reserved and printed EMPTY
//
// Two failures are visible on that one row and both are assertions here:
//
//  1. A RESERVED-AND-EMPTY FIELD. The tool column holds its 16 cells on every
//     repaint (that is the alignment contract, and it is right), but its content
//     when no tool has run is TWO SPACES — an indent — so the reader cannot tell
//     "no tool ran" from "the field is broken". A field that reserves space and
//     prints nothing is worse than an absent field.
//  2. A ZERO FOR A NON-ZERO TURN. "0s" is what the live block shows for ANY turn
//     shorter than a second, i.e. exactly the turns where the live block is the
//     only thing on the screen that could have answered "is it working?".
//
// The test is written against the PROPERTIES and never against the fix's own
// vocabulary: it names no constant the fix introduces, so this file is
// byte-identical on the red tree and on the green one — which is what makes the
// pasted failure a statement about the defect rather than about a rename.
//
// It drives the REAL wiring (the source, the in-flight tracker, the source's own
// status() accessor — the same one the editor's read loop calls) and asserts on
// the ROW AS DRAWN, never on a fixture of the row.

import (
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// statusHonestyModel is the model name the fixture composes with.
const statusHonestyModel = "deepseek-flash"

// statusHonestyField is one reserved column of the row: its name (from the row's
// own name map) and the CELLS it really occupies on the composed row.
type statusHonestyField struct {
	name string
	text string
}

// statusHonestyFieldCells splits the composed row into the cells each RESERVED
// column occupies, using the row's own geometry (statusRowColumns) and the row's
// own column names (statusColumnNames) — so this instrument cannot drift from the
// layout production composes with. The keys ARE the row's left-to-right order
// (statusColModel == 0, …).
func statusHonestyFieldCells(row []rune) []statusHonestyField {
	var out []statusHonestyField
	cols := statusRowColumns()
	col := 0
	for key := statusColModel; int(key) < len(cols); key++ {
		if int(key) > 0 {
			col += lineedit.StatusColumnSepCells
		}
		start := col
		end := col + cols[key].Width
		if start > len(row) {
			start = len(row)
		}
		if end > len(row) {
			end = len(row)
		}
		out = append(out, statusHonestyField{name: statusColumnNames[key], text: string(row[start:end])})
		col += cols[key].Width
	}
	return out
}

// statusHonestyToolColumn is the cells the TOOL column of the composed row
// occupies. It is the one column this defect is about, and it is located through
// the same instrument as every other column rather than by a copied cell offset.
func statusHonestyToolColumn(row string) string {
	for _, f := range statusHonestyFieldCells([]rune(row)) {
		if f.name == statusColumnNameTool {
			return f.text
		}
	}
	return ""
}

// assertNoBlankReservedField is the shared assertion: every reserved column of the
// composed row carries a value or a WORD — never padding. The blank case is
// spelled out rather than implied, because "the cell is spaces" is exactly the
// shape that shipped, and "carries a word" is the difference between a field that
// says "nothing here" and one that says nothing at all.
func assertNoBlankReservedField(t *testing.T, what string, st lineedit.Status) {
	t.Helper()
	row := st.Line(statusColumnsAtCols)
	for _, f := range statusHonestyFieldCells([]rune(row)) {
		if strings.TrimSpace(f.text) == "" {
			t.Errorf("%s: the %q column is reserved and printed BLANK (its cells are %q) — "+
				"a field that reserves space and prints nothing is worse than an absent field\nrow = %q",
				what, f.name, f.text, row)
			continue
		}
		if !strings.ContainsFunc(f.text, unicode.IsLetter) {
			t.Errorf("%s: the %q column carries no word a reader can decode (its cells are %q) — "+
				"a reserved field must state a fact in words, not only a glyph or an indent\nrow = %q",
				what, f.name, f.text, row)
		}
	}
}

// TestStatusRow_NoReservedFieldIsPrintedBlank drives a turn in which a tool runs
// and reads the row as drawn, asserting that EVERY reserved column carries either
// a value or a word saying it has none.
//
// The turn is driven through the production wiring: chatStatusSource (which owns
// the counters and the layout) and chatTurnStatus (which owns the in-flight state
// and the keepalive). Nothing here re-derives the row's arithmetic.
func TestStatusRow_NoReservedFieldIsPrintedBlank(t *testing.T) {
	s := newChatStatusSource(statusHonestyModel, true)
	// The operational read-outs of a session that has really been working: the
	// operator's own numbers, with a clock that is running.
	started := time.Now().Add(-4 * time.Minute)
	s.setOpsFn(func() statusOps {
		return statusOps{
			ctxUsed:      15400,
			ctxMax:       200000,
			ctxMeasured:  true,
			elapsed:      time.Since(started),
			elapsedKnown: true,
		}
	})

	// The turn: iteration 5, one tool running, then the tool done and the turn
	// finished — the state the operator's row was dumped in.
	tracker := newChatTurnStatus(s, nil)
	tracker.begin(5)
	s.setTool("bash", "git branch -a")
	running := s.status()

	assertNoBlankReservedField(t, "a tool running", running)

	// THE HEADLINE: a tool ran and the row names it.
	if row := running.Line(statusColumnsAtCols); !strings.Contains(row, toolGlyph) {
		t.Errorf("a tool ran and the row does not name it:\nrow = %q", row)
	}

	// The tool done, the turn over: the row is back at the prompt with the
	// counters of a turn that really happened.
	s.setToolDone("bash")
	s.clearTool()
	tracker.publish(s, 5, statusLiveIdleTurn, 15400)
	idle := s.status()
	assertNoBlankReservedField(t, "the turn finished", idle)
}

// TestStatusRow_SubSecondTurnIsNotRoundedToZero pins the second half of the row's
// honesty clause on the case the operator hit: a turn SHORTER than one second.
//
// It is the case where the row is the only thing on screen that could answer "is
// it working?", and the shipped producer answers "0s" — a number no clock
// reported, about a turn that was not zero. The row must state a measurement or
// an honest unknown; it may not print the zero-looking label.
func TestStatusRow_SubSecondTurnIsNotRoundedToZero(t *testing.T) {
	const subSecond = 400 * time.Millisecond
	s := newChatStatusSource(statusHonestyModel, true)
	s.setOpsFn(func() statusOps {
		return statusOps{
			ctxUsed: 15400, ctxMax: 200000, ctxMeasured: true,
			elapsed: subSecond, elapsedKnown: true,
		}
	})
	s.setToolDone("bash")
	row := s.status().Line(statusColumnsAtCols)
	if got := statusElapsedLabel(subSecond); got == statusElapsedZero {
		t.Errorf("the elapsed formatter rounds a MEASURED %v to %q — the zero-looking label is "+
			"the one thing this row must not print for a non-zero turn\nelapsed label = %q\nrow = %q",
			subSecond, got, got, row)
	}
	if !strings.Contains(row, statusElapsedLabel(subSecond)) {
		t.Errorf("the row does not carry the measured elapsed label %q:\nrow = %q",
			statusElapsedLabel(subSecond), row)
	}
	// The unknown case is the other honest exit: with no clock the block states
	// the state word alone rather than inventing a duration.
	noClock := newChatStatusSource(statusHonestyModel, true)
	noClock.setOpsFn(func() statusOps {
		return statusOps{ctxUsed: 15400, ctxMax: 200000, ctxMeasured: true}
	})
	noClock.setToolDone("bash")
	if row := noClock.status().Line(statusColumnsAtCols); strings.Contains(row, statusElapsedZero) {
		t.Errorf("with no clock the row prints %q, a duration it never measured:\nrow = %q",
			statusElapsedZero, row)
	}
}

// TestStatusRow_IdleFactIsAWordNotPadding pins the idle case on its own: at the
// prompt, with nothing running, EVERY reserved field of the row states a measured
// fact — and the tool column's fact is a word, not an indent.
//
// This is the "a field that reserves space and prints nothing is worse than an
// absent field" clause, asserted at the prompt where it is most visible: the
// operator's own dump ended on exactly this row.
func TestStatusRow_IdleFactIsAWordNotPadding(t *testing.T) {
	s := newChatStatusSource(statusHonestyModel, true)
	s.setOpsFn(func() statusOps {
		return statusOps{
			ctxUsed: 15400, ctxMax: 200000, ctxMeasured: true,
			elapsed: 4 * time.Minute, elapsedKnown: true,
		}
	})
	s.update(5, statusLiveIdleTurn, 15400, "")
	row := s.status().Line(statusColumnsAtCols)

	// 1. Every reserved column either carries its value or a word.
	assertNoBlankReservedField(t, "the prompt", s.status())

	// 2. The tool column states, in words, that no tool has run — the assertion
	//    the blank indent failed, stated as the property rather than as the fix's
	//    own spelling of it.
	tool := statusHonestyToolColumn(row)
	if !strings.ContainsFunc(tool, unicode.IsLetter) {
		t.Errorf("the prompt's tool column carries no word (its cells are %q): the reader cannot "+
			"tell \"no tool ran\" from \"this field is broken\"\nrow = %q", tool, row)
	}
	// 3. A word in the tool column may not cost the alignment: it has to fit the
	//    column's reservation, and the row stays one screen row inside its pin.
	if w := lineedit.DisplayWidth(strings.TrimSpace(tool)); w > statusToolCol {
		t.Fatalf("the idle tool field is %d cells, over the tool column's reservation of %d:\nrow = %q",
			w, statusToolCol, row)
	}
	if w := lineedit.DisplayWidth(row); w > statusColumnsAtCols {
		t.Fatalf("the row is %d cells wide at the pin %d:\nrow = %q", w, statusColumnsAtCols, row)
	}
	t.Logf("idle row = %q", row)
}
