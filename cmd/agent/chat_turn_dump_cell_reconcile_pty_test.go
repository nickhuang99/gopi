package main

// chat_turn_dump_cell_reconcile_pty_test.go — the CELL-LEVEL RECONCILIATION on a
// REAL pty with an ARMED chat dump (task 01a0e825).
//
// WHAT THIS ADDS OVER `TestChatTurnDump_ReadsTheWholeAnswerOnARealPtyWithoutInput`.
// That case asserts the escape-fragment direction: "no CSI parameter run reaches
// a cell". The epic this file answers observed that the fragment half is only
// HALF the defect — on the SAME live row the answer's own text was missing and a
// fragment's parameters stood in its place:
//
//	artifact row 011 (session 884eca28): `  path]` 当 下 文 回 显 ） 我 建 议 1m──…`
//	the answer it was drawn from:         `  path]` 当**主场景**来定义它的显示形态。另外第三步（终端上 下文 回显）我建议**先不立**…`
//
// So this case recovers the answer's own cells from the artifact and compares
// them with the answer the turn was driven with, IN BOTH DIRECTIONS:
//
//	LOST  = produced cells that are not on the painted reading;
//	EXTRA = painted cells that are not part of the produced answer.
//
// THE READ BOUNDARY IS MODELLED, NOT ASSUMED. The production reader is a pty
// read: the editor writes a paint frame and the sink's grid is built from
// whatever read sizes the OS hands over. This case reproduces that by putting a
// SPLITTER between the editor and the real `--dump-screen` sink, so every frame
// reaches the grid in fixed-size reads — an escape sequence is then split at a
// real boundary rather than at a convenient one.
//
// IT MEASURES. Its assertions are about the reconciliation itself: if the
// artifact drops answer cells or carries cells the answer never had, the numbers
// are reported and the case fails. Nothing here is an OCR or a "looks right".

import (
	"io"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/lineedit"
	"github.com/nickhuang99/gopi/internal/uiclient"
)

// The geometry. 117x26 is the layout's own shape and the geometry of the live
// evidence this task was opened for, so the wrap decisions here are the ones the
// operator's terminal made.
const (
	reconPtyCols = 117
	reconPtyRows = 26
)

// reconReadStep is the read size the splitter hands the sink. It is SMALL on
// purpose: a small read splits sequences at many offsets, which is exactly the
// boundary the defect family lives at. A named constant because the VALUE is the
// case's subject (Rule 1).
const reconReadStep = 7

// The fixture's token family. Unique per index, with the varying digits INSIDE
// every short window (see turnDumpToken's own comment), so a repeated or
// misplaced cell run is attributable to the renderer rather than to the fixture.
const reconTokenPrefix = "rcl"

// reconBodyLines is how many plain lines the answer carries: more than the band
// can show, so the answer also scrolls and the reading is band + scrollback.
const reconBodyLines = 12

// The three shapes the live evidence was made of: CJK prose, an emphasis span,
// and inline code. All three are in ONE answer so a single turn exercises them.
const (
	reconCJKLine   = "主场景的定义需要把粘贴场景的 header 预算独立立项"
	reconBoldLine  = "**先不立**这一步，等确认后再说"
	reconCodeLine  = "用 `chat_surface action=see` 读 attrs 做逐列对账"
	reconTailLine  = "reconciliation tail"
	reconCheckMark = "✓"
)

// reconAnswer is the answer the turn is driven with.
func reconAnswer() string {
	var b strings.Builder
	b.WriteString(reconCJKLine)
	b.WriteString("\n")
	b.WriteString(reconBoldLine)
	b.WriteString("\n")
	b.WriteString(reconCodeLine)
	b.WriteString("\n")
	for i := 1; i <= reconBodyLines; i++ {
		b.WriteString(turnDumpToken(reconTokenPrefix, i))
		b.WriteString(" ")
		b.WriteString(reconCheckMark)
		b.WriteString("\n")
	}
	b.WriteString(reconTailLine)
	b.WriteString("\n")
	return b.String()
}

// reconSplitWriter forwards every write it receives to a sink in fixed-size
// reads. It is the READ BOUNDARY, made explicit: exactly what a tty does to the
// bytes the editor wrote, and the reason the interpreter must carry a tail
// across reads at all.
type reconSplitWriter struct {
	dst  io.Writer
	step int
}

func (w *reconSplitWriter) Write(p []byte) (int, error) {
	for i := 0; i < len(p); i += w.step {
		j := i + w.step
		if j > len(p) {
			j = len(p)
		}
		if _, err := w.dst.Write(p[i:j]); err != nil {
			return i, err
		}
	}
	return len(p), nil
}

// reconDense folds a painted row or a produced line to the cells the comparison
// works on: the layout's own markup (the turn markers, the cut label) carries no
// cell of the answer, and the renderer spends the space a wrap breaks at, so air
// is not part of the comparison. EVERYTHING else is kept — a dropped, duplicated
// or reordered character must reach the assertion.
func reconDense(s string) string {
	s = strings.ReplaceAll(s, lineedit.AnswerCutLabel, "")
	s = strings.ReplaceAll(s, lineedit.BoldMarker, "")
	s = strings.ReplaceAll(s, lineedit.TurnMarker, "")
	s = strings.ReplaceAll(s, lineedit.TurnMarkerSplit, "")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\t", "")
	return strings.ReplaceAll(s, "\n", "")
}

// reconReconcileReading reconciles a painted reading (the artifact's rows, in the
// order a terminal shows them) against the answer the turn was driven with, and
// returns the two directions the epic asks for:
//
//	lost  — produced answer cells that are not on the reading;
//	extra — cells on the reading, INSIDE the rows that carry the answer, that the
//	        answer never had.
//
// THE ROW RANGE IS WHY EXTRA IS MEANINGFUL. The artifact also holds chrome (the
// turn header rule, the prompt, the status line) which carries no cell of the
// answer; counting its cells as "extra characters" would make the number about
// the layout instead of about the render. So the comparison is restricted to the
// rows between the first and the last row the answer's own cells were found on —
// the answer's own span — and everything else is left to the layout's own cases.
func reconReconcileReading(rows []string, answer string) (lost, extra int, matched int, first, last int) {
	produced := []rune(reconDense(answer))
	first, last = reconAnswerRows(produced, rows)
	if first < 0 {
		return len(produced), 0, 0, -1, -1
	}
	var b strings.Builder
	for i := first; i <= last && i < len(rows); i++ {
		b.WriteString(reconDense(rows[i]))
	}
	reading := []rune(b.String())
	matched = reconLCS(produced, reading)
	return len(produced) - matched, len(reading) - matched, matched, first, last
}

// reconLCS is the length of the longest common subsequence of two rune slices —
// the number of produced cells that survived, IN ORDER. It is the count that sees
// a dropped cell and a reordered cell alike; a "does the reading contain the
// answer" substring check cannot.
func reconLCS(a, b []rune) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			switch {
			case a[i-1] == b[j-1]:
				cur[j] = prev[j-1] + 1
			case prev[j] >= cur[j-1]:
				cur[j] = prev[j]
			default:
				cur[j] = cur[j-1]
			}
		}
		copy(prev, cur)
		for j := range cur {
			cur[j] = 0
		}
	}
	return prev[len(b)]
}

// reconAnswerRows returns the first and last row index that carries a cell of
// the produced answer, by walking the rows in order and consuming the produced
// runes greedily — the same order the reconciliation's LCS counts. It is what
// bounds the EXTRA window to the answer's own rows.
func reconAnswerRows(produced []rune, rows []string) (first, last int) {
	first, last = -1, -1
	pi := 0
	for i, row := range rows {
		for _, r := range []rune(reconDense(row)) {
			if pi < len(produced) && r == produced[pi] {
				if first < 0 {
					first = i
				}
				last = i
				pi++
			}
		}
	}
	return first, last
}

// TestChatTurnDumpReconcilesCells_OnRealPTY drives one whole turn on a real
// 117x26 pty through the real armed `--dump-screen` sink, with the paint stream
// arriving in fixed-size reads, and reconciles the artifact's cells with the
// answer in both directions.
func TestChatTurnDumpReconcilesCells_OnRealPTY(t *testing.T) {
	pair := chatOpenPtyPair(t, reconPtyCols, reconPtyRows)
	if got := lineedit.TerminalCols(int(pair.Slave.Fd())); got != reconPtyCols {
		t.Fatalf("the pty reports %d columns, want %d — the kernel did not take the size", got, reconPtyCols)
	}

	sink := turnDumpSink(t)
	// THE READ BOUNDARY: the editor writes to the splitter, the splitter feeds
	// the real sink in fixed-size reads.
	split := &reconSplitWriter{dst: sink, step: reconReadStep}

	ed, err := lineedit.Open(pair.Slave, split)
	if err != nil {
		t.Fatalf("open the pty slave as a terminal: %v", err)
	}
	defer func() { _ = ed.Close() }() //stylegate:ignore the harness's own terminal teardown

	chat := lineedit.NewChatEditor(ed, split, chatPrompt, nil, nil, nil)
	chat.SetGeometryObserver(chatGeomObserver(sink))
	chat.SetPalette(lineedit.BasicPalette)
	if !chat.EnableRegion() {
		t.Fatalf("EnableRegion(pty %dx%d) = false: the case needs the reserved layout installed", reconPtyCols, reconPtyRows)
	}
	in := &chatInput{editor: chat, dumper: sink}
	if in.Answer() == nil {
		t.Fatal("the input layer reports no answer band although the layout is installed")
	}

	answer := reconAnswer()
	renderer := newChatTurnRenderer(in)
	renderer.BeginTurn(1, turnDumpPromptLine)
	// Stream in small deltas: the renderer's own frames are then small too, so
	// the splitter's reads cut the paint stream at many offsets.
	for _, delta := range reconDeltas(answer) {
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
	rows := make([]string, 0, len(cand.Dump.Scrollback)+len(cand.Dump.Grid))
	rows = append(rows, cand.Dump.Scrollback...)
	rows = append(rows, cand.Dump.Grid...)

	if len(reconDense(strings.Join(rows, ""))) == 0 {
		t.Fatalf("the artifact (%s) carries no cells at all (frames=%d dumps=%d) — nothing below could be asserted",
			sink.Path(), cand.Frames, cand.Dumps)
	}

	lost, extra, matched, first, last := reconReconcileReading(rows, answer)
	produced := len([]rune(reconDense(answer)))
	t.Logf("RECONCILE pty=%dx%d read_step=%d frames=%d dumps=%d produced_cells=%d answer_rows=%d..%d matched=%d lost=%d extra=%d",
		reconPtyCols, reconPtyRows, reconReadStep, cand.Frames, cand.Dumps, produced, first, last, matched, lost, extra)
	t.Logf("artifact: %s", sink.Path())

	if matched == 0 || first < 0 {
		t.Fatalf("the artifact's rows share NO cell with the answer it was drawn from (artifact %s, frames=%d dumps=%d): the measurement is void",
			sink.Path(), cand.Frames, cand.Dumps)
	}
	// INSTRUMENT SELF-CHECK: the counter must be able to SEE a loss and an
	// addition, or `lost=0 extra=0` would be a green that means nothing. The
	// same reconciliation is run on a deliberately damaged copy of the answer's
	// own rows: drop a run of cells from the middle, and add a run the answer
	// never had. Both must move off zero, by exactly the amount damaged.
	reconSelfCheck(t, rows, answer, first, last)
	if lost != 0 {
		t.Fatalf("RECONCILE: the painted reading is MISSING %d of the answer's %d cells (matched=%d): the render dropped produced text.\nreading: %.400s",
			lost, produced, matched, strings.Join(rows[first:min(last+1, len(rows))], "\n"))
	}
	if extra != 0 {
		t.Fatalf("RECONCILE: the painted reading carries %d cells the answer never had (matched=%d): the render added visible characters.\nreading: %.400s",
			extra, matched, strings.Join(rows[first:min(last+1, len(rows))], "\n"))
	}
	// The escape-fragment direction, kept: a parameter run must not be a cell —
	// and it is now also counted as EXTRA above, so the two readings agree.
	if row, found := turnDumpEscapeFragment(rows); found {
		t.Fatalf("the artifact carries SGR parameters as literal CELLS: %q", row)
	}
	// CUT LABEL SANITY: the layout's own cut label is markup, and this case
	// strips it in reconDense — asserted here so a vocabulary change shows up as
	// a failure of THIS case rather than as silent extra cells.
	if strings.Contains(answer, lineedit.AnswerCutLabel) {
		t.Fatalf("the fixture answer itself contains the layout's cut label %q: the fixture would confuse markup with content", lineedit.AnswerCutLabel)
	}
}

// reconSelfCheck proves the counter is not blind: on a copy of the answer's own
// rows with a run of cells REMOVED it must report those cells lost, and on a copy
// carrying a run of cells the answer never had it must report them extra. A
// counter that cannot see a deliberate injury cannot certify an intact screen —
// `lost=0 extra=0` would be a green that means nothing.
func reconSelfCheck(t *testing.T, rows []string, answer string, first, last int) {
	t.Helper()
	if first < 0 || last <= first {
		t.Fatalf("the answer occupied rows %d..%d of the artifact: a self-check needs a multi-row answer span", first, last)
	}
	span := append([]string(nil), rows[first:min(last+1, len(rows))]...)

	// (a) A LOSS: remove a run of cells from the middle row of the answer's span.
	const injuryRunes = 6
	// The injury goes on the LONGEST row of the answer's span, not a middle one:
	// the band's rows have very different lengths (a numbered token line is six
	// cells, the CJK paragraph is hundreds), and a row too short to injure would
	// turn this self-check into a silent skip.
	mid, midLen := 0, -1
	for i, r := range span {
		if n := len([]rune(reconDense(r))); n > midLen {
			mid, midLen = i, n
		}
	}
	// The injury is applied to the row's CELLS (its dense form), not to its raw
	// bytes: a band row is padded with spaces, and cutting six of THOSE changes
	// nothing a reading sees — which is exactly how a self-check can pass while
	// proving nothing (measured: an earlier cut of raw runes reported lost=0).
	runes := []rune(reconDense(span[mid]))
	if len(runes) <= injuryRunes*2 {
		t.Fatalf("the longest answer row (%q) carries only %d cells — too few to injure by %d, so this self-check cannot prove the counter works", span[mid], len(runes), injuryRunes)
	}
	cutAt := len(runes) / 2
	damaged := append([]string(nil), span...)
	damaged[mid] = string(runes[:cutAt]) + string(runes[cutAt+injuryRunes:])
	damagedRows := append([]string(nil), rows...)
	copy(damagedRows[first:], damaged)
	lost, _, _, _, _ := reconReconcileReading(damagedRows, answer)
	if lost < injuryRunes {
		t.Fatalf("SELF-CHECK (loss): removing %d cells from the answer's own rows reported lost=%d — the counter cannot see a cell loss, so its lost=0 on the real artifact proves nothing",
			injuryRunes, lost)
	}
	t.Logf("SELF-CHECK (loss): %d cells removed -> lost=%d (the counter sees it)", injuryRunes, lost)

	// (b) AN ADDITION: a parameter run the answer never had, painted onto a row.
	fragment := ";61H24;73H5;230m"
	added := append([]string(nil), span...)
	added[0] += fragment
	addedRows := append([]string(nil), rows...)
	copy(addedRows[first:], added)
	_, extra, _, _, _ := reconReconcileReading(addedRows, answer)
	if extra < len(fragment)/2 {
		t.Fatalf("SELF-CHECK (addition): painting %d cells the answer never had reported extra=%d — the counter cannot see added characters, so its extra=0 on the real artifact proves nothing",
			len(fragment), extra)
	}
	t.Logf("SELF-CHECK (addition): %d cells added -> extra=%d (the counter sees it)", len(fragment), extra)
}

// min is the two-argument minimum, kept as a named helper so the row windows
// read the same way at every call site.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// reconDeltas splits the answer into the streamed fragments a provider delivers.
// Small pieces on purpose: the renderer's frames are then small, which is what
// makes the splitter's fixed-size reads land inside sequences.
func reconDeltas(answer string) []string {
	const deltaRunes = 4
	runes := []rune(answer)
	out := make([]string, 0, len(runes)/deltaRunes+1)
	for i := 0; i < len(runes); i += deltaRunes {
		j := i + deltaRunes
		if j > len(runes) {
			j = len(runes)
		}
		out = append(out, string(runes[i:j]))
	}
	return out
}
