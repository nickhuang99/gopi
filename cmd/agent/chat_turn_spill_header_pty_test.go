package main

// chat_turn_spill_header_pty_test.go — the SPILL FORM of the turn header, driven
// end to end on a real pty and read off the real grid.
//
// WHY A PTY AND NOT A UNIT CALL. A header is a claim about what a reader sees, and
// the whole point of the specialised form is that a reader CAN TELL a spilled turn
// from a typed one. Asserting that on the renderer's return value would test the
// string, not the screen: the band wraps, cuts rows at the layout's own edge, and
// drives the grid one row at a time. So this case builds the production shape —
// real 117x26 pty, real chat layout, real editor, the REAL boundary
// (chatSealMessage) spilling a real payload through the REAL store — and asserts
// on the dump sink's cells.
//
// WHAT IT PINS, one leg per way the form can be quietly wrong:
//
//	size      — the header states the payload's TRUE size, READ BACK FROM THE FILE.
//	            The defect this closes: the already-spilled route used the
//	            REFERENCE LINE's own rune count as the payload size, so a header
//	            describing a 64 KiB paste reported ~70. This leg writes a payload
//	            whose byte, rune and line counts are deliberately different from the
//	            reference line's, and asserts the header states the file's.
//	distinct  — a spilled turn and a typed turn in the SAME band do not read alike:
//	            the spilled one carries the size vocabulary and the file handle, the
//	            typed one carries the operator's words and none of that.
//	total     — every row the layout holds is within the pty's columns: the new form
//	            is bound by the same total-in-cols discipline as the old one.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/inputsource"
	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/uiclient"
)

// spillPtyPayloadLines is how many lines the fixture payload holds. Three
// properties are chosen together, and each of them is load-bearing:
//
//   - the payload EXCEEDS the editor's own spill threshold, so this is a paste
//     that would REALLY have been spilled rather than one the case forced the
//     store to spill. A fixture below the threshold would test the header on a
//     shape production never produces.
//   - it is a 64 KiB-class paste's little brother — the defect being closed is that
//     such a payload was described by the reference line's own ~70 characters, so
//     the case must be one where "the file's size" and "the reference's size"
//     cannot be confused.
//   - the header can draw only turnSpillHeadMaxLines-equivalent lines of its head,
//     so the elision marker has something to count.
const spillPtyPayloadLines = 512

// spillPtyPayloadLine is one line of the fixture payload: CJK, so the payload's
// RUNE count differs sharply from its byte count and the two cannot be confused
// for one another.
const spillPtyPayloadLine = "知状态日志一行行行行行行行\n"

// spillPtyBuildPayload returns the payload and its true facts, computed by the
// fixture rather than by the code under test.
func spillPtyBuildPayload() (body string, bytes, runes, lines int) {
	var b strings.Builder
	for i := 0; i < spillPtyPayloadLines; i++ {
		b.WriteString(spillPtyPayloadLine)
	}
	body = b.String()
	return body, len(body), len([]rune(body)), spillPtyPayloadLines
}

// TestChatTurnSpillHeader_DescribesThePasteNotTheReference is the end-to-end case.
func TestChatTurnSpillHeader_DescribesThePasteNotTheReference(t *testing.T) {
	body, wantBytes, wantRunes, wantLines := spillPtyBuildPayload()
	dir := t.TempDir()
	store := lineedit.NewPasteStore(dir)
	if store == nil {
		t.Fatal("NewPasteStore returned nil for a non-empty directory")
	}
	// THE REAL SPILL, through the REAL store — the same file the editor writes on
	// the raw route, at the same path shape.
	ref, err := store.Spill([]byte(body))
	if err != nil {
		t.Fatalf("Spill: %v", err)
	}
	// THE REAL BOUNDARY: the line the editor leaves on the input row goes through
	// chatSealMessage exactly as cli_chat.go sends it, so the description the
	// header draws is the one production builds.
	line := lineedit.PasteRefFor(ref)
	sess := &chatSession{sessionDir: dir}
	out, err := chatSealMessage(sess, line, true, inputsource.Human)
	if err != nil {
		t.Fatalf("chatSealMessage on a spilled line: %v", err)
	}
	if !out.Spilled || !out.Spill.Spilled {
		t.Fatalf("the boundary did not describe the spill (Spilled=%v, Spill.Spilled=%v)", out.Spilled, out.Spill.Spilled)
	}
	// The reference line's OWN rune count, which is what the defect printed: the
	// leg below fails loudly if the header ever states this instead of the file's.
	refRunes := len([]rune(line))
	if refRunes == wantRunes || wantBytes == refRunes {
		t.Fatalf("the fixture is undiscriminating: the reference line is %d runes, the payload %d bytes / %d runes — a header could state any of them and pass",
			refRunes, wantBytes, wantRunes)
	}
	// And the fixture must be a payload that would REALLY have spilled, not one the
	// case talked the store into spilling (lineedit's own size rule: one read at or
	// above PasteThreshold is a paste by SIZE).
	if wantBytes <= lineedit.PasteThreshold {
		t.Fatalf("the fixture payload is %d bytes, at or below the editor's own spill threshold (%d): production would never have spilled it",
			wantBytes, lineedit.PasteThreshold)
	}

	// ── the real surface ───────────────────────────────────────────────────
	pair := chatOpenPtyPair(t, turnDumpPtyCols, turnDumpPtyRows)
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
	if in.Answer() == nil {
		t.Fatal("the input layer reports no answer band although the layout is installed")
	}

	renderer := newChatTurnRenderer(in)
	// Turn 1: the SPILLED turn, opened through the PRODUCTION dispatch — the very
	// `chatBeginTurn` cli_chat.go calls, with the very chatOutbound the boundary
	// produced. Asserting the header through the dispatch (rather than calling the
	// renderer's spill entry point directly) is what makes this case evidence that
	// production ROUTES a spill to the spill form.
	chatBeginTurn(renderer, 1, out)
	renderer.Write("the spill was described\n")
	renderer.Finish()
	renderer.EndTurn()
	// Turn 2: an ORDINARY turn, in the same band — the distinguishability leg needs
	// both forms on one grid.
	const typedPrompt = "and now an ordinary question about it"
	chatBeginTurn(renderer, 2, chatOutbound{Message: typedPrompt, Display: typedPrompt})
	renderer.Write("the ordinary answer\n")
	renderer.Finish()
	renderer.EndTurn()
	newChatFlowSeam(in, nil).Flush()
	in.SettleDump()

	cand, err := uiclient.ReadDump(sink.Path())
	if err != nil {
		t.Fatalf("read the artifact back (%s): %v", sink.Path(), err)
	}
	grid := spillPtyGrid(cand)
	if grid == "" {
		t.Fatalf("the artifact (%s) carries no cells (frames=%d dumps=%d)", sink.Path(), cand.Frames, cand.Dumps)
	}

	// The observed header, logged so the -v log of a green run IS the evidence a
	// reader needs (Rule 5/31): the numbers below are asserted against the
	// fixture's own, and this line is what they looked like when they passed.
	t.Logf("spilled turn header at %dx%d:\n%s", turnDumpPtyCols, turnDumpPtyRows,
		spillPtyRowWith(grid, lineedit.TurnSpillBytesMark))

	// ── size: the header states the FILE's facts ──────────────────────────
	wantBytesMark := strconv.Itoa(wantBytes) + lineedit.TurnSpillBytesMark
	if !strings.Contains(grid, wantBytesMark) {
		t.Errorf("the spilled turn's header does not state the payload's true size (%q):\n%s", wantBytesMark, grid)
	}
	if strings.Contains(grid, strconv.Itoa(refRunes)+lineedit.TurnSpillBytesMark) {
		t.Errorf("the header states the REFERENCE LINE's size (%d bytes) instead of the file's (%d):\n%s", refRunes, wantBytes, grid)
	}
	// The payload's own line count, and the handle on the file.
	if !strings.Contains(grid, strconv.Itoa(wantLines)+lineedit.TurnSpillLinesMark) {
		t.Errorf("the header does not state the payload's line count (%d):\n%s", wantLines, grid)
	}
	// ── distinct: the two forms do not read alike ─────────────────────────
	if !strings.Contains(grid, typedPrompt) {
		t.Errorf("the ordinary turn's prompt is missing from the band:\n%s", grid)
	}
	spillRow := spillPtyRowWith(grid, lineedit.TurnSpillBytesMark)
	plainRow := spillPtyRowWith(grid, typedPrompt)
	if spillRow == "" {
		t.Fatalf("no row carries the spill's size vocabulary:\n%s", grid)
	}
	if plainRow == "" {
		t.Fatalf("no row carries the typed prompt:\n%s", grid)
	}
	if spillRow == plainRow {
		t.Errorf("the spilled turn and the typed turn share one row:\n%s", grid)
	}
	// The typed turn must NOT acquire the spill's vocabulary: a form that leaked
	// into the ordinary header would be a regression in the other direction.
	if strings.Contains(plainRow, lineedit.TurnSpillBytesMark) || strings.Contains(plainRow, lineedit.TurnSpillLinesMark) {
		t.Errorf("an ordinary turn's header carries the spill vocabulary:\n%s", plainRow)
	}
	if !strings.Contains(plainRow, spillPtyTurnLabel) {
		t.Errorf("the ordinary turn's header does not name its turn:\n%s", plainRow)
	}
	if !strings.Contains(spillRow, lineedit.TurnSpillLabelName) {
		t.Errorf("the spilled turn's header does not name its form (%q):\n%s", lineedit.TurnSpillLabelName, spillRow)
	}
	// The HANDLE, as the band can afford it: 117 columns cannot hold this run's
	// absolute spill path (the suite's own scratch root is long), so what is
	// asserted is the contract that holds at ANY width — the header shows the
	// path's leading cells and MARKS the cut, rather than printing a path that looks
	// complete and is not. The whole-path case is asserted in internal/lineedit's
	// own header tests, at a width that can hold it.
	if !strings.Contains(spillRow, ref.Path[:spillPtyHandlePrefixCells]) {
		t.Errorf("the header does not open the handle with the path's own leading cells (%q):\n%s",
			ref.Path[:spillPtyHandlePrefixCells], spillRow)
	}
	if !strings.Contains(spillRow, lineedit.StatusEllipsis) {
		t.Errorf("the header cut the handle without marking it, so the path reads as complete:\n%s", spillRow)
	}

	// ── total: nothing painted past the pty's own width ───────────────────
	for i, row := range cand.Dump.Grid {
		if w := spillPtyCells(row); w > turnDumpPtyCols {
			t.Errorf("grid row %d holds %d cells, past the pty's %d: %q", i, w, turnDumpPtyCols, row)
		}
	}
	if _, err := os.Stat(ref.Path); err != nil {
		t.Errorf("the file the header names is not readable: %v", err)
	}
	if filepath.Dir(ref.Path) != filepath.Join(dir, lineedit.PasteDirName) {
		t.Errorf("the payload landed in %s, want the store's own %s", ref.Path, filepath.Join(dir, lineedit.PasteDirName))
	}
}

// spillPtyTurnLabel is the ordinary header's opening word. It is spelled here
// because the ordinary form's own vocabulary lives behind an unexported constant
// in internal/lineedit, while every mark this case reads off the SPILL form comes
// from the package's own exported constants (Rule 14).
const spillPtyTurnLabel = "turn "

// spillPtyHandlePrefixCells is how many of the path's own leading cells must
// survive into the header at this width: enough to name the run's scratch root and
// the way into it, which is what makes the handle usable by hand. The number is a
// floor for the ASSERTION, not a budget the renderer reads.
const spillPtyHandlePrefixCells = 20

// spillPtyGrid joins the artifact's cell rows, scrollback first, as the band's own
// text — the reading a reader has, not the renderer's intentions.
func spillPtyGrid(cand uiclient.Candidate) string {
	rows := make([]string, 0, len(cand.Dump.Scrollback)+len(cand.Dump.Grid))
	for _, line := range cand.Dump.Scrollback {
		rows = append(rows, strings.TrimRight(turnDumpStripCells(line), " "))
	}
	for _, row := range cand.Dump.Grid {
		rows = append(rows, strings.TrimRight(turnDumpStripCells(row), " "))
	}
	return strings.Join(rows, "\n")
}

// spillPtyRowWith returns the first cell row carrying want, or "".
func spillPtyRowWith(grid, want string) string {
	for _, row := range strings.Split(grid, "\n") {
		if strings.Contains(row, want) {
			return row
		}
	}
	return ""
}

// spillPtyCells counts the display cells a dump row holds, in the layout's own
// terms — the same measure TurnHeader is total in.
func spillPtyCells(row string) int {
	return len([]rune(turnDumpStripCells(row)))
}
