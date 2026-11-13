package main

// chat_turn_dump_wrapspace_ledger_pty_test.go — the WRAP SPACE LEG of the same
// end-to-end instrument chat_turn_dump_grid_pty_test.go established.
//
// WHY THE SIBLING CASE COULD NOT SEE THIS DEFECT. Its fixture's emphasized line
// breaks in the MIDDLE of a token (`…16emph` / `17emph 18emph…`), and the
// renderer's wrap only consumes a SPACE when the row it leaves ended on one. The
// whole class the operator reported — an emphasis span landing off its own cells
// after a wrap that swallowed a space — was therefore unreachable in that
// fixture, and the suite stayed green while the operator's 117-column screen
// showed the mark 17 columns from its text.
//
// The full account is devdoc/answer-wrapspace-span-ledger.md (root cause plus the
// recipe for spotting a ledger that accumulates).
//
// THE SHAPE THE OPERATOR SAW, reproduced literally: ONE source line (so one
// answerRow, whose span ledger is what accumulates the error) that wraps the band
// several times, each row EXACTLY as wide as the body so the character at the cut
// is the space the wrap consumes, and the emphasized run sitting beyond those
// wraps. The mark must be on the run's own cells; before the fix it is drawn one
// character per consumed space early, so plain filler is emphasized and the run's
// own characters are plain.
//
// The case reads the REAL artifact: a real 117x26 pty, the real chat layout, the
// real `--dump-screen` sink, and the grid's own attr runs — never a byte count.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/uiclient"
)

// The fixture's numbers. Every one is named because the case is about them: the
// width is the operator's capture width, the unit count is the number of
// space-consuming wraps before the span (i.e. the displacement in characters),
// and the emphasized run is long enough that a shifted ledger cannot land it
// inside its own characters by luck.
const (
	// wrapSpaceLedgerUnits is how many space-consuming wraps the fixture's single
	// line makes before the emphasized run.
	wrapSpaceLedgerUnits = 4
	// wrapSpaceLedgerFillRune fills the wrapped rows: neither the emphasized run's
	// rune nor a space, so a cell's expected emphasis follows the rune in it.
	wrapSpaceLedgerFillRune = 'y'
	// wrapSpaceLedgerRunRune is the emphasized run's rune.
	wrapSpaceLedgerRunRune = 'B'
	// wrapSpaceLedgerRunRunes is the emphasized run's length.
	wrapSpaceLedgerRunRunes = 9
	// wrapSpaceLedgerTail follows the run, so the span is INTERIOR (both edges are
	// read) rather than a suffix of the line.
	wrapSpaceLedgerTail = " tail words after the run"
)

// wrapSpaceLedgerBodyCells is the width of a body row inside the turn: the band's
// columns minus the turn's gutter.
func wrapSpaceLedgerBodyCells() int {
	return turnDumpPtyCols - lineedit.DisplayWidth(lineedit.TurnMarker)
}

// wrapSpaceLedgerAnswer is the fixture answer: a short plain line (so the band has
// a committed row before the fixture's own), then ONE long line — `units` filled
// body rows, each followed by the space its wrap consumes, then the emphasized
// run and a tail — then nothing.
func wrapSpaceLedgerAnswer() string {
	unit := strings.Repeat(string(wrapSpaceLedgerFillRune), wrapSpaceLedgerBodyCells()) + " "
	var b strings.Builder
	b.WriteString(turnDumpToken(turnDumpPlainPrefix, 1))
	b.WriteString("\n")
	b.WriteString(strings.Repeat(unit, wrapSpaceLedgerUnits))
	b.WriteString(lineedit.BoldMarker)
	b.WriteString(strings.Repeat(string(wrapSpaceLedgerRunRune), wrapSpaceLedgerRunRunes))
	b.WriteString(lineedit.BoldMarker)
	b.WriteString(wrapSpaceLedgerTail)
	b.WriteString("\n")
	return b.String()
}

// TestChatTurnDump_EmphasisAfterAWrapSpaceKeepsItsCells is the end-to-end
// regression on the artifact a real chat run leaves behind.
//
// RED on master (measured): every one of the run's cells is painted WITHOUT bold
// and the fill cells before it ARE bold — the mark landed wrapSpaceLedgerUnits
// characters early, exactly the operator's 17-column displacement as a function of
// the input.
func TestChatTurnDump_EmphasisAfterAWrapSpaceKeepsItsCells(t *testing.T) {
	pair := chatOpenPtyPair(t, turnDumpPtyCols, turnDumpPtyRows)
	if got := lineedit.TerminalCols(int(pair.Slave.Fd())); got != turnDumpPtyCols {
		t.Fatalf("the pty reports %d columns, want %d — the kernel did not take the size", got, turnDumpPtyCols)
	}

	sink := turnDumpSink(t)
	ed, err := lineedit.Open(pair.Slave, sink)
	if err != nil {
		t.Fatalf("open the pty slave as a terminal: %v", err)
	}
	defer func() { _ = ed.Close() }() //stylegate:ignore the harness's own terminal teardown

	chat := lineedit.NewChatEditor(ed, sink, chatPrompt, nil, nil, nil)
	chat.SetGeometryObserver(chatGeomObserver(sink))
	chat.SetPalette(lineedit.BasicPalette)
	if !chat.EnableRegion() {
		t.Fatalf("EnableRegion(pty %dx%d) = false: the case needs the reserved layout installed", turnDumpPtyCols, turnDumpPtyRows)
	}

	in := &chatInput{editor: chat, dumper: sink}
	renderer := newChatTurnRenderer(in)
	renderer.BeginTurn(1, turnDumpPromptLine)
	for _, delta := range turnDumpDeltas(wrapSpaceLedgerAnswer()) {
		renderer.Write(delta)
	}
	renderer.Finish()
	renderer.EndTurn()
	newChatFlowSeam(in, nil).Flush()
	in.SettleDump()

	cand, err := uiclient.ReadDump(sink.Path())
	if err != nil {
		t.Fatalf("read the artifact back (%s): %v", sink.Path(), err)
	}

	// The measurement, over the artifact's OWN grid: every cell that spells the
	// run's rune, and every cell carrying bold although it is not one of them.
	w := boldFieldWord(t)
	var runCells, plainRunCells, markedFill int
	for row, line := range cand.Dump.Grid {
		for col, r := range []rune(line) {
			style := turnDumpStyleAt(cand, row, col)
			bold := strings.Contains(style, boldFieldWord(t))
			if r == wrapSpaceLedgerRunRune {
				runCells++
				if !bold {
					plainRunCells++
				}
			}
			if r == wrapSpaceLedgerFillRune && bold {
				markedFill++
			}
		}
	}
	if runCells != wrapSpaceLedgerRunRunes {
		t.Fatalf("the artifact's grid spells %d of the run's %d characters — the drawn answer must still show every character once:\n%s",
			runCells, wrapSpaceLedgerRunRunes, strings.Join(cand.Dump.Grid, "\n"))
	}
	if plainRunCells > 0 || markedFill > 0 {
		t.Fatalf("the emphasized run is off its own cells: %d of its %d characters are painted WITHOUT %s and %d filler cell(s) carry it (the ledger is short by one byte per consumed wrap space, and this line wraps the band %d time(s)):\n%s",
			plainRunCells, wrapSpaceLedgerRunRunes, w, markedFill, wrapSpaceLedgerUnits,
			strings.Join(cand.Dump.Grid, "\n"))
	}
}
