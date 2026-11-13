package main

// pty_marker_column_dump_test.go — a REAL pty at a NARROW width, a committed
// turn with CJK prose and a fenced block, dumped through the SAME sink and
// reader `gopi chat --dump-screen` / `gopi ui_state` use.
//
// TWO SHAPES, ONE TRUTH (measured 2026-10-02): a turn's body rows are not all
// claimed by the same column. The PROSE rows open at the block's column — the
// turn marker's own cell width, glyph on the first row and blank on the rows a
// line wraps onto — while a row of the fenced block opens at CELL 0, drawing no
// glyph and none of its cells, because a code row is the reader's COPYING
// material: a terminal selection of it must be the answer's line, byte for byte
// (answerRow.gutterGlyph, commit 7e92b038). Both halves are asserted here: the
// prose column as before, and the block's rows byte-exact against the answer's
// own block — the property that made the block's rows gutterless in the first
// place, and the one a later change to the column arithmetic must not break.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/uiclient"
)

// layerDumpLayers is the vocabulary this dump reads the grid with: every layer
// the table names, in table order. It is the ONE list here — a layer added to
// lineedit's table and left out of it would silently stop being measured.
var layerDumpLayers = []lineedit.AnswerLayer{
	lineedit.LayerRequestForTest,
	lineedit.LayerThinkingForTest,
	lineedit.LayerResponseForTest,
	lineedit.LayerCodeToolForTest,
}

// layerOpener answers the layer a body row OPENS, by asking the layer table for
// each layer's marker (lineedit.LayerMarkerForTest) and returning the first one
// the row starts with. "" means the row opens no layer — it is a continuation.
//
// Asking the table rather than spelling the markers here is the whole point: a
// test that hard-codes `▌ ` and `· ` is a second definition of the format (and
// the one that MISSED the request layer, measured: the request row was counted
// as a continuation and failed the block-column check).
func layerOpener(row string) string {
	for _, layer := range layerDumpLayers {
		marker := lineedit.LayerMarkerForTest(layer)
		if marker != "" && strings.HasPrefix(row, marker) {
			return marker
		}
	}
	return ""
}

const (
	markerDumpCols = 56
	markerDumpRows = 48
)

// The turn's answer, as the test drives it. The block's own text is built from
// these pieces so the byte-exact assertion below compares the GRID against the
// answer this test sent — never against a second spelling of it (Rule 14).
const (
	// markerDumpProse is the CJK paragraph that WRAPS several times at
	// markerDumpCols, so the prose column claim has rows to bite on.
	markerDumpProse = "这一段中文答案用来检查换行之后的列是否对齐，中间夹着 ASCII token 和 code 片段，然后再来一段中文以确保换行发生多次，最后以句号结束。"
	// markerDumpCode is the block's first content line: CJK and wider than the
	// band, so the block wraps too — the shape whose continuation row used to
	// carry the blank block column.
	markerDumpCode = "这是一行很长的中文代码内容，用来在窄列里强制换行，看看续行有没有标记。"
	// markerDumpCodeShort is a block line that fits one row.
	markerDumpCodeShort = "echo ok"
)

// markerDumpBlock is the fenced block as the answer spells it: the opening
// marker, its content lines, the closing marker — the block a reader selects
// and pastes. It is the WANT of the byte-exact grid assertion.
func markerDumpBlock() string {
	return lineedit.FenceMarker + markerDumpCode + markerDumpCodeShort + lineedit.FenceMarker
}

// markerDumpRuleWait bounds the ONLY wait in this test: how long the closing
// rule may take to appear in the dump's grid. EndTurn's own write paints it
// synchronously, so the poll's first pass normally already sees it — the bound
// exists so a genuinely missing rule FAILS here with that name, instead of
// surfacing later as the much less specific "the turn's body was not found in
// the dump" (Rule 1: no bare literal at the site, and Rule 27: a bound is a
// ceiling, never a load generator).
const markerDumpRuleWait = 2 * time.Second

// markerDumpRuleCols is the fewest CELLS of the closing rule's glyph that can
// only be the rule: a turn's closing rule is ONE run across the grid's full
// width (56 cells here), and this half-width bound keeps the OBSERVABLE from
// being satisfied by a row that merely ENDS in glyphs — the turn's own header
// row has exactly that shape (`──── turn 1 · … ───…`), so a plain presence test
// would report the rule as painted while EndTurn had never run (measured:
// red-proof A passed the wrong predicate and was caught only by the later body
// check). A run of this length carries no label, so it can be no other row.
const markerDumpRuleCols = markerDumpCols / 2

// waitForRulePainted reports whether the dump's grid holds the turn's closing
// rule within budget.
//
// WHY A GRID READ AND NOT THE FILE: the file is what the sink's throttle
// (dumpMinInterval) last installed, while the grid is the truth NOW — Close
// reads the grid directly (Snapshot), so the grid is the surface the freeze
// actually depends on, and polling the file would make this assertion a race
// against the throttle it was written to avoid.
//
// THE PREDICATE IS A WHOLE ROW, and it reads the CELL-EXACT `Grid` section
// (one rune per cell, full width) rather than Text()'s human-readable Lines —
// the rule occupies an entire grid row, so a row whose every cell is the glyph
// is the rule and nothing else can be. A substring test is NOT enough: the
// turn's header row also ends in glyphs (`──── turn 1 · … ───…`), so the first
// version of this poll reported the rule as painted while EndTurn had never run
// (measured — red-proof A passed the wrong predicate and was caught only by the
// later body check).
func waitForRulePainted(t *testing.T, d *uiclient.ScreenDumper, budget time.Duration) bool {
	t.Helper()
	glyphs := strings.Repeat(lineedit.TurnRuleGlyph, markerDumpRuleCols)
	deadline := time.Now().Add(budget)
	for {
		for _, row := range d.Snapshot().Grid {
			if strings.HasPrefix(row, glyphs) && strings.Trim(row, lineedit.TurnRuleGlyph) == "" {
				return true
			}
		}
		if !time.Now().Before(deadline) {
			return false
		}
		runtime.Gosched()
	}
}

// isFenceRow reports whether a dumped row is one of the block's own delimiters.
//
// It steps over a layer's marker first, by asking the ONE layer table
// (layerOpener) rather than spelling `▌ `: the row it must recognise is the
// fence as the screen draws it, and on a tree where the code family still wears
// the block's column (the shape 7e92b038 removed) the fence row arrives with
// that marker in front of it. Recognising it there is what lets this test FAIL
// on the block's own TEXT — the paste made impossible — instead of on a search
// that never found the block at all.
func isFenceRow(row string) bool {
	body := strings.TrimSpace(row)
	if opener := layerOpener(body); opener != "" {
		body = strings.TrimSpace(strings.TrimPrefix(body, opener))
	}
	return strings.HasPrefix(body, lineedit.FenceMarker)
}

func TestPtyMarkerColumnDump(t *testing.T) {
	pair := chatOpenPtyPair(t, markerDumpCols, markerDumpRows)
	slave := pair.Slave
	if got := lineedit.TerminalCols(int(slave.Fd())); got != markerDumpCols {
		t.Fatalf("TerminalCols = %d, want %d", got, markerDumpCols)
	}
	outDir := os.Getenv("MARKER_DUMP_DIR")
	if outDir == "" {
		outDir = t.TempDir()
	}
	path := filepath.Join(outDir, uiclient.ChatDumpFileName)
	d, err := uiclient.NewScreenDumper(uiclient.DumpConfig{
		Path: path,
		// The grid's geometry is the PTY's, stated by the caller that measured
		// it (the fd answers TIOCGWINSZ below). Left unstated the sink falls
		// back to its 80x24 default while the editor paints the pty's 56x48 —
		// every band write then lands in the same row of a grid that is the
		// wrong shape, and the dump holds one row of a turn that rendered
		// perfectly.
		Cols:     markerDumpCols,
		Rows:     markerDumpRows,
		Geometry: uiclient.GeometrySourceTerminal,
		Identity: uiclient.DumpIdentity{
			Surface:          uiclient.SurfaceChat,
			PID:              os.Getpid(),
			Target:           chatDumpTarget(false),
			TerminalColsRows: [2]int{0, 0},
			Started:          uiDumpStartStamp(),
		},
	})
	if err != nil {
		t.Fatalf("arm dump: %v", err)
	}
	term, err := lineedit.Open(slave, d)
	if err != nil {
		t.Fatalf("open pty slave: %v", err)
	}
	defer func() { _ = term.Close() }() //stylegate:ignore teardown close of the terminal fixture on the dump path; the dump was already asserted
	ed := lineedit.NewChatEditor(term, d, chatPrompt, nil, os.Stderr, NewSlashCompleter())
	ed.SetGeometryObserver(chatGeomObserver(d))
	if !ed.EnableRegion() {
		t.Fatal("EnableRegion refused")
	}
	band := ed.Answer()
	if band == nil {
		t.Fatal("no answer band on this terminal")
	}
	band.BeginTurn(lineedit.TurnInfo{Index: 1, Prompt: "窄列中文"})
	// one CJK paragraph that wraps several times at 56 cells
	band.Delta(markerDumpProse + "\n")
	// a fenced block
	band.Delta(lineedit.FenceMarker + "\n")
	band.Delta(markerDumpCode + "\n")
	band.Delta(markerDumpCodeShort + "\n")
	band.Delta(lineedit.FenceMarker + "\n")
	band.Finish()
	// The dump is flushed on a bounded interval, so the LAST frame (the whole
	// answer) is only installed by a later write or by Close. Ending the turn
	// writes the closing rule, which is that later write.
	band.EndTurn()
	// A FIXED SLEEP USED TO SIT HERE (50ms) as if the closing rule above reached
	// the file later. It does not, and the measurement says so: the sink's write
	// count and frame count are unchanged across 0..100ms after EndTurn, and
	// Close with NO wait at all installed the same frozen grid (the answer rows
	// plus both rule rows) — because the sink's 100ms throttle (dumpMinInterval)
	// bounds only the WRITE path, while Close calls flush(true), which reads the
	// grid ITSELF through Snapshot(), so whatever the editor has already painted
	// is frozen regardless of when the throttle last installed a file. A wait
	// for nothing is not evidence: the "later write" the rule depends on is
	// EndTurn's own write, which has returned by this line.
	//
	// The poll that replaces it is the OBSERVABLE the freeze depends on — the
	// closing rule is painted into the dump's grid — bounded by
	// markerDumpRuleWait, and it is a pure in-memory Snapshot() read (no device,
	// no sleep-based progress), so the test neither races the throttle nor pays
	// a fixed delay when the rule is already there.
	if !waitForRulePainted(t, d, markerDumpRuleWait) {
		t.Fatalf("the turn's closing rule never reached the dump's grid within %v — EndTurn's write is what paints it, so the freeze below would capture a turn with no closing rule", markerDumpRuleWait)
	}
	// Freeze the sink on its last painted frame BEFORE the editor's own
	// teardown disables (and erases) the region — the run's order, and the
	// difference between a dump of the answer and a dump of an empty band.
	if err := d.Close(); err != nil {
		t.Fatalf("close dump: %v", err)
	}
	ed.Close()
	cand, err := uiclient.ReadDump(path)
	if err != nil {
		t.Fatalf("read dump: %v", err)
	}
	t.Logf("dump %s cols=%d rows=%d", path, cand.Dump.Cols, cand.Dump.Rows)
	// The turn's body starts at the header and ends at the closing rule. Only
	// the rows BETWEEN them are the turn; the blank rows above are the pty's
	// unused screen, which is not the claim.
	first, last := -1, -1
	for i, row := range cand.Dump.Lines {
		if strings.HasPrefix(row, "────") {
			if first < 0 {
				first = i + 1
				continue
			}
			last = i - 1
			break
		}
	}
	if first < 0 || last < first {
		t.Fatalf("the turn's body was not found in the dump:\n%s", cand.Dump.Text())
	}
	// Two readings of one screen, and each claim takes the one it needs:
	//
	//   - the COLUMN a row opens at is measured on `Lines` (the readable
	//     rendering), whose leading cells are 1:1 with columns — the arithmetic
	//     internal/lineedit's sibling test uses, and the one this file has always
	//     used;
	//   - the BLOCK's text is compared on `Lines` too, because `Grid` is a COLUMN
	//     MAP rather than text: it stores a wide rune's continuation cell as its
	//     own entry (screendump.go), so a CJK row's runes are not its characters.
	//
	// Lines trims trailing blanks and skips a wide rune's continuation column —
	// both of which a row's leading column and its text survive.
	lines := cand.Dump.Lines
	// blockFrom/blockTo bound the grid rows of the fenced block, read off the
	// block's own delimiters — the same reading the lineedit sibling does
	// (marker_column_narrow_test.go, markerProbe.fencedRows). blockTo < first
	// means the turn holds no block, which is refused below.
	blockFrom, blockTo := 0, -1
	for i := first; i <= last; i++ {
		if !isFenceRow(lines[i]) {
			continue
		}
		if blockTo < first {
			blockFrom = i
		}
		blockTo = i
	}
	if blockTo < first {
		t.Fatalf("the turn holds no fenced block on the grid — the block's rows are half of this test's claim:\n%s", cand.Dump.Text())
	}
	// The block's first content line is wider than the band, so the block itself
	// CARRIES a continuation row: without it, the row this claim is about — a
	// code row that WRAPPED — is not on the screen at all. Four source lines
	// plus at least one wrapped row is the floor.
	if got := blockTo - blockFrom + 1; got <= 4 {
		t.Fatalf("the block occupies %d row(s) for its 4 source lines — its content must WRAP at %d cells for the wrapped code row to exist:\n%s",
			got, markerDumpCols, cand.Dump.Text())
	}
	body := 0
	continuations := 0
	for i := first; i <= last; i++ {
		l := strings.TrimRight(lines[i], " ")
		t.Logf("body row %2d %q", i, l)
		if strings.TrimSpace(l) == "" {
			continue
		}
		body++
		// A row of the fenced block opens at CELL 0 and carries no marker column
		// at all — not the glyph, not its cells: a code row is the reader's
		// COPYING material (answerRow.gutterGlyph), so it is not claimed by the
		// block column below. It is claimed, harder, by being the answer's line
		// byte for byte (the joined comparison after this loop).
		if i >= blockFrom && i <= blockTo {
			continue
		}
		// A row that opens a LAYER carries that layer's marker; the four markers
		// are asked of the layer table itself (lineedit.LayerMarkerForTest), so
		// a fifth layer added there is measured here without this file knowing
		// its spelling. Each marker is exactly the block's gutter wide, which is
		// what puts every layer's text on the same left edge.
		if opener := layerOpener(l); opener != "" {
			if w := lineedit.DisplayWidth(opener); w != lineedit.DisplayWidth(lineedit.TurnMarker) {
				t.Errorf("body row %d opens with the marker %q, %d cells wide, want the block's gutter %d: %q",
					i, opener, w, lineedit.DisplayWidth(lineedit.TurnMarker), l)
			}
			continue
		}
		continuations++
		indent := lineedit.DisplayWidth(l) - lineedit.DisplayWidth(strings.TrimLeft(l, " "))
		if indent != lineedit.DisplayWidth(lineedit.TurnMarker) {
			t.Errorf("body row %d opens at cell %d, want the block's column %d: %q",
				i, indent, lineedit.DisplayWidth(lineedit.TurnMarker), l)
		}
	}
	// THE BLOCK IS THE ANSWER'S BLOCK. A reader selects the block off the screen
	// and pastes it into a shell, so what the screen holds there must BE the
	// answer's lines, in order, with no cell of the band's chrome among them —
	// the property 7e92b038 made possible by drawing no gutter on these rows, and
	// the one a later change to the column arithmetic would silently break (a
	// `▌ `, or two blank cells, at the head of a wrapped code row fails here).
	// The rows are read IN ORDER and joined, which is what a reader's selection
	// does across the block's own wrap. The block's text carries no trailing
	// blanks, so reading each row to its last non-blank cell drops nothing the
	// answer wrote.
	rows := make([]string, 0, blockTo-blockFrom+1)
	for i := blockFrom; i <= blockTo; i++ {
		rows = append(rows, strings.TrimRight(lines[i], " "))
	}
	joined := strings.Join(rows, "")
	if joined != markerDumpBlock() {
		t.Errorf("the block read off the screen = %q, want the answer's block verbatim %q — a code row that carries ink or cells of the band's own cannot be pasted:\n%s",
			joined, markerDumpBlock(), cand.Dump.Text())
	} else {
		t.Logf("COPIED BLOCK: %q (%d cells on %d row(s))", joined, lineedit.DisplayWidth(joined), len(rows))
	}
	if continuations < 2 {
		t.Fatalf("only %d continuation rows in the dump (%d body rows) — the CJK paragraph and the fenced block must both WRAP at %d cells for this to be the evidence it claims",
			continuations, body, markerDumpCols)
	}
	t.Logf("READ BACK: %d body rows, %d continuation rows, every one at the block's column", body, continuations)
}
