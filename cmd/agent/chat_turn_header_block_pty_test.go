package main

// chat_turn_header_block_pty_test.go — the MULTI-LINE turn header read off a REAL
// pty's painted grid (Rule 31: the surface is executed, and the evidence is the
// cells the terminal would show).
//
// The defect the block closes is a SCREEN fact — a long prompt was cut to one row
// with nothing saying what was missing — so the verification has to be a screen
// fact too. This case drives the REAL chat layout on a REAL pty through the SAME
// turn renderer the REPL uses, settles the REAL dump sink, and reads the header
// back as grid rows:
//
//	multi-row     — a long prompt's header occupies MORE THAN ONE band row, and
//	                every one of those rows is inside the pty's width (no row
//	                wrapped, because a wrapped header row would steal a band row
//	                the per-row finalize ledger did not count).
//	counted-end   — the block's LAST row is `... (+N more line(s))`, with N the
//	                number of rows the reader is not being shown — the explicit
//	                statement that replaces the silent cut.
//	paste-room    — a spilled paste's prompt draws MORE rows than a typed prompt,
//	                and the reference (size AND path) is legible on the grid: the
//	                operand of the reader's next action is not what got cut.
//	narrow-intact — at 40 columns every painted header row equals the row the
//	                package composed at that width, i.e. no row is damaged.
//
// The rows are compared against lineedit.TurnHeaderLines' own answer at the same
// width, so the case asserts "the screen shows what the package decided" rather
// than a second derivation of the layout.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

// The pty geometries the header block is read at. 80 is the operator-facing width
// a long prompt is usually read at; 40 is the package's own narrow harness width
// (turnHarnessCols), where the block must still be intact.
const (
	headerBlockPtyCols    = 80
	headerBlockPtyRows    = 26
	headerBlockNarrowCols = 40
)

// chatHeaderBlockPrompt is the fixture prompt: ASCII words only, so a grid row
// compares byte-for-byte with the composed row (a CJK prompt is asserted by the
// package's own block tests, where the grid is a Screen rather than a dump).
func chatHeaderBlockPrompt(repeat int) string {
	var b strings.Builder
	for i := 0; i < repeat; i++ {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(chatHeaderBlockWord)
	}
	return b.String()
}

const chatHeaderBlockWord = "explain-the-failing-step-and-why"

// chatHeaderBlockPastePrompt is a paste-shaped prompt: the reference in the fixed
// spelling PasteRefFor renders, then the question about the payload.
func chatHeaderBlockPastePrompt() string {
	return lineedit.PasteRefFor(lineedit.PasteRef{Runes: chatHeaderBlockPasteRunes, Path: chatHeaderBlockPastePath}) +
		" " + chatHeaderBlockPrompt(chatHeaderBlockPasteTail)
}

const (
	chatHeaderBlockPasteRunes = 51200
	chatHeaderBlockPastePath  = "/tmp/pastes/paste-1.txt"
	// chatHeaderBlockPasteTail is how many words follow the reference: the paste
	// leg is about the ROW BUDGET, which only shows once there is more text than a
	// row carries.
	chatHeaderBlockPasteTail = 12
)

// chatHeaderBlockGrid drives ONE turn through the real chat renderer on a real pty
// and returns the settled artifact's grid plus the width it was painted at.
func chatHeaderBlockGrid(t *testing.T, cols, rows int, prompt string) ([]string, int) {
	t.Helper()
	pair := chatOpenPtyPair(t, cols, rows)
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
		t.Fatalf("EnableRegion(pty %dx%d) = false: the case needs the reserved layout installed", cols, rows)
	}
	in := &chatInput{editor: chat, dumper: sink}
	if in.Answer() == nil {
		t.Fatal("the input layer reports no answer band although the layout is installed")
	}

	renderer := newChatTurnRenderer(in)
	renderer.BeginTurn(1, prompt)
	renderer.Write("the answer body starts here\n")
	renderer.Finish()
	renderer.EndTurn()
	newChatFlowSeam(in, nil).Flush()
	in.SettleDump()

	cand, err := uiclient.ReadDump(sink.Path())
	if err != nil {
		t.Fatalf("read the artifact back (%s): %v", sink.Path(), err)
	}
	if len(cand.Dump.Grid) == 0 {
		t.Fatalf("the artifact (%s) holds no grid rows (frames=%d dumps=%d): nothing could be read", sink.Path(), cand.Frames, cand.Dumps)
	}
	got := make([]string, 0, len(cand.Dump.Grid))
	for _, row := range cand.Dump.Grid {
		got = append(got, strings.TrimRight(turnDumpStripCells(row), " "))
	}
	return got, cols
}

// chatHeaderBlockFind is where the block's first row sits in the grid, or -1. The
// search is for the composed row (right-trimmed), so the case reads the block from
// the SCREEN rather than from the stream.
func chatHeaderBlockFind(grid []string, first string) int {
	want := strings.TrimRight(first, " ")
	for i, row := range grid {
		if row == want {
			return i
		}
	}
	return -1
}

// chatHeaderBlockCount reads N out of the marker row.
func chatHeaderBlockCount(row string) (int, bool) {
	rest, ok := strings.CutPrefix(row, lineedit.TurnHeaderMoreMarker)
	if !ok {
		return 0, false
	}
	end := strings.Index(rest, " more line")
	if end < 0 {
		return 0, false
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil {
		return 0, false
	}
	return n, true
}

// TestChatTurnHeader_PaintsAMultiRowCountedHeaderOnARealPty is the epic's own
// acceptance leg: one long prompt, read off the pty's painted grid as a BLOCK
// whose last row counts what it did not show.
func TestChatTurnHeader_PaintsAMultiRowCountedHeaderOnARealPty(t *testing.T) {
	prompt := chatHeaderBlockPrompt(12)
	grid, cols := chatHeaderBlockGrid(t, headerBlockPtyCols, headerBlockPtyRows, prompt)
	want := lineedit.TurnHeaderLines(lineedit.TurnInfo{Index: 1, Prompt: prompt}, cols)
	if len(want) < 2 {
		t.Fatalf("the fixture prompt composed %d header row(s) at cols=%d: the case needs a block", len(want), cols)
	}
	at := chatHeaderBlockFind(grid, want[0])
	if at < 0 {
		t.Fatalf("the header's first row %q is not on the pty's grid.\n--- grid ---\n%s", want[0], strings.Join(grid, "\n"))
	}
	for i, row := range want {
		if got := grid[at+i]; got != strings.TrimRight(row, " ") {
			t.Fatalf("header row %d on the grid = %q, want %q\n--- grid ---\n%s", i, got, strings.TrimRight(row, " "), strings.Join(grid, "\n"))
		}
		if w := lineedit.DisplayWidth(row); w > cols {
			t.Fatalf("header row %d is %d cells at cols=%d — a row wider than the pty would wrap and steal a band row", i, w, cols)
		}
	}
	last := grid[at+len(want)-1]
	// The evidence leg (Rule 31): the artifact's own rows, printed verbatim, so a
	// reader of the run's log sees the screen the case judged.
	for i := at; i < at+len(want) && i < len(grid); i++ {
		t.Logf("pty %dx%d block row %d: %q", headerBlockPtyCols, headerBlockPtyRows, i-at, grid[i])
	}
	n, ok := chatHeaderBlockCount(last)
	if !ok {
		t.Fatalf("the block's last grid row %q is not the counted marker row %q", last, lineedit.TurnHeaderMoreMarker)
	}
	if n < 1 {
		t.Fatalf("the marker row %q counts %d rows, want at least 1", last, n)
	}
	// The answer that followed the header is on the grid BELOW the block: the
	// block consumed rows, it did not overrun the turn.
	//
	// THE REQUEST LAYER SITS BETWEEN THEM, and that is the production order, not
	// an accident of this fixture: drawRequestRow is called by BeginTurn
	// immediately after the header (internal/lineedit/layers.go — "it is the
	// FIRST row of the body, before any tool call or the first answer fragment"),
	// so the operator's own line is physically above its body on the grid and the
	// append order is the causal order. The assertion below therefore looks for
	// the body ANYWHERE under the block and requires every row it skips to be part
	// of that request layer — the block must not have eaten the body, which is the
	// claim this leg has always been about.
	body := -1
	for i := at + len(want); i < len(grid); i++ {
		if strings.Contains(grid[i], "the answer body starts here") {
			body = i
			break
		}
	}
	if body < 0 {
		t.Fatalf("the answer body is not on the grid below the block (searched from row %d)\n--- grid ---\n%s", at+len(want), strings.Join(grid, "\n"))
	}
	for i := at + len(want); i < body; i++ {
		// The request layer's FIRST row opens with its marker; its continuation
		// rows are the same gutter as blank pad (requestRowRows indents them to
		// the marker's width, so the block keeps one left edge). Anything else in
		// this gap would mean the block did not contain the turn.
		marker := lineedit.LayerMarkerForTest(lineedit.LayerRequestForTest)
		pad := strings.Repeat(" ", lineedit.DisplayWidth(marker))
		if i == at+len(want) {
			if !strings.HasPrefix(grid[i], marker) {
				t.Fatalf("grid row %d is the first row under the header block but does not open with the request layer's marker %q: %q\n--- grid ---\n%s",
					i, marker, grid[i], strings.Join(grid, "\n"))
			}
			continue
		}
		if !strings.HasPrefix(grid[i], pad) || strings.TrimSpace(grid[i]) == "" {
			t.Fatalf("grid row %d sits between the header block and the answer body but is neither the request row nor one of its continuations: %q\n--- grid ---\n%s",
				i, grid[i], strings.Join(grid, "\n"))
		}
	}
}

// TestChatTurnHeader_PasteBlockGetsMoreRoomAndKeepsTheReference is the paste leg on
// the real screen: more rows than a typed prompt of the same width, and the
// reference — its size and its absolute path — legible in the painted cells.
func TestChatTurnHeader_PasteBlockGetsMoreRoomAndKeepsTheReference(t *testing.T) {
	cols := headerBlockPtyCols
	typed := chatHeaderBlockPrompt(12)
	typedGrid, _ := chatHeaderBlockGrid(t, cols, headerBlockPtyRows, typed)
	pasteGrid, _ := chatHeaderBlockGrid(t, cols, headerBlockPtyRows, chatHeaderBlockPastePrompt())

	typedWant := lineedit.TurnHeaderLines(lineedit.TurnInfo{Index: 1, Prompt: typed}, cols)
	pasteWant := lineedit.TurnHeaderLines(lineedit.TurnInfo{Index: 1, Prompt: chatHeaderBlockPastePrompt()}, cols)
	typedAt := chatHeaderBlockFind(typedGrid, typedWant[0])
	pasteAt := chatHeaderBlockFind(pasteGrid, pasteWant[0])
	if typedAt < 0 || pasteAt < 0 {
		t.Fatalf("a block's first row is missing from the grid (typed at %d, paste at %d)\n--- paste grid ---\n%s",
			typedAt, pasteAt, strings.Join(pasteGrid, "\n"))
	}
	if len(pasteWant) <= len(typedWant) {
		t.Fatalf("the paste block composed %d row(s) and the typed one %d: the paste leg must get MORE room",
			len(pasteWant), len(typedWant))
	}
	// Both legs must END in the counted row (never a silent cut), and the extra
	// room must be CONTINUATION rows: the blocks differ by
	// turnHeaderPasteContinuationLines − turnHeaderContinuationLines, one more
	// row of the prompt, not by a longer sentence about hiding it.
	for _, leg := range []struct {
		name  string
		block []string
	}{{"typed", typedWant}, {"paste", pasteWant}} {
		last := leg.block[len(leg.block)-1]
		if n, ok := chatHeaderBlockCount(last); !ok || n < 1 {
			t.Fatalf("the %s block's last row %q is not the counted marker row %q", leg.name, last, lineedit.TurnHeaderMoreMarker)
		}
	}
	if pasteCont, typedCont := len(pasteWant)-2, len(typedWant)-2; pasteCont <= typedCont {
		t.Fatalf("the paste block drew %d continuation row(s) and the typed one %d: the paste leg's larger budget must buy CONTENT rows\n--- paste ---\n%s\n--- typed ---\n%s",
			pasteCont, typedCont, strings.Join(pasteWant, "\n"), strings.Join(typedWant, "\n"))
	}
	ref := lineedit.PasteRefFor(lineedit.PasteRef{Runes: chatHeaderBlockPasteRunes, Path: chatHeaderBlockPastePath})
	painted := strings.Join(pasteGrid[pasteAt:pasteAt+len(pasteWant)], "\n")
	for i := pasteAt; i < pasteAt+len(pasteWant) && i < len(pasteGrid); i++ {
		t.Logf("pty %dx%d paste block row %d: %q", cols, headerBlockPtyRows, i-pasteAt, pasteGrid[i])
	}
	if !strings.Contains(painted, ref) {
		t.Fatalf("the painted header does not carry the whole reference %q:\n%s", ref, painted)
	}
	if paths := lineedit.PasteRefPaths(painted); len(paths) == 0 || paths[0] != chatHeaderBlockPastePath {
		t.Fatalf("the painted reference no longer parses (paths=%v): a reader (or a copy) cannot act on it\n%s", paths, painted)
	}
}

// TestChatTurnHeader_NarrowWidthRowsAreIntact is the narrow-width leg on the real
// screen: at 40 columns every painted header row equals the row composed for that
// width, and no row exceeds the pty's width — i.e. the block degrades without a
// damaged or wrapped row.
func TestChatTurnHeader_NarrowWidthRowsAreIntact(t *testing.T) {
	prompt := chatHeaderBlockPrompt(12)
	grid, cols := chatHeaderBlockGrid(t, headerBlockNarrowCols, headerBlockPtyRows, prompt)
	want := lineedit.TurnHeaderLines(lineedit.TurnInfo{Index: 1, Prompt: prompt}, cols)
	at := chatHeaderBlockFind(grid, want[0])
	if at < 0 {
		t.Fatalf("the header's first row %q is not on the 40-column grid.\n--- grid ---\n%s", want[0], strings.Join(grid, "\n"))
	}
	for i, row := range want {
		if w := lineedit.DisplayWidth(row); w > cols {
			t.Fatalf("composed header row %d is %d cells at cols=%d: %q", i, w, cols, row)
		}
		if i < len(want) {
			t.Logf("pty %d columns block row %d: %q", cols, i, strings.TrimRight(row, " "))
		}
		if got := grid[at+i]; got != strings.TrimRight(row, " ") {
			t.Fatalf("narrow header row %d on the grid = %q, want %q\n--- grid ---\n%s", i, got, strings.TrimRight(row, " "), strings.Join(grid, "\n"))
		}
	}
	if len(want) < 2 {
		t.Fatalf("the narrow fixture composed %d header row(s) at cols=%d: the case needs a block to assert the degradation", len(want), cols)
	}
}
