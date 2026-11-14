package lineedit

// The answer renderer SHARES its byte stream with the editor's read loop: the
// loop repaints the editing line (`CSI <editorTop>;0H`, erase, the prompt) and
// the status row whenever it wakes, and the answer renderer writes its own
// placements and text through the SAME writer. So a placement and the text it
// positions are ONE unit of emission, or the loop can land between them — and
// the text then goes to whatever row that repaint left the cursor on.
//
// Measured on the paint stream (2026-09-27), the exact interleaving:
//
//	CSI 10;13H                                                 place at row 9, col 12
//	CSI 11;1H CSI 2K CSI 1m CSI 32m "you>" CSI 0m CR CSI 5C     read loop repaints the editing line
//	CSI 1m "beta" CSI 0m                                       the span's TEXT, on the editing line's row
//
// The frozen artifact then held `▌ alpha     beta and gamma`: its ATTRS spelled
// the COMPLETE line (the emphasis map the placement had already written) while
// its TEXT missed the 5-cell run at cols 7-11, never painted at all, and `beta`
// sat on the editing line, where the repaint erased it. That is the row whose
// text and attrs disagree — the shape the dump gate timed out on, and the
// discriminator the briefing asked for. It decides the question in favour of a
// SPLICE — but the splice is of two writers' chunk boundaries inside ONE frame,
// not of two frames, which is why an abbreviated-emphasis reading also fit the
// evidence and was wrong.
//
// These tests CONSTRUCT that interleaving — an editor repaint is spliced between
// the renderer's placement and its text — instead of sampling it: the defect is
// a race, and a sampled window costs about one hit per 360 iterations.

import (
	"fmt"

	"github.com/nickhuang99/gopi/internal/sgr"
	"strings"
	"testing"
	"time"
)

// placementInterleaver is a writer that lets a test land an interruption
// EXACTLY between a bare placement and the text that follows it.
type placementInterleaver struct {
	inner *recordingWriter
	// repaint is what the editor's read loop does when it wakes: it repaints the
	// editing line and the status row through the SAME writer. It is set at
	// construction (never swapped later), so the guard needs no `-race`
	// suppression of its own.
	repaint func()
	// pending is a bare placement held back just long enough to see whether the
	// NEXT write is the text it positions. Only that case is the production
	// window: a bare placement that draws nothing and is followed by a write
	// which places itself again is harmless.
	pending []byte
	// inRepaint is true while the injected repaint is writing, so the repaint's
	// own stream is never held (the read loop's writes are the loop's business).
	inRepaint bool
	// splices counts the windows this guard actually interrupted. Zero on a
	// fixed tree is the EXPECTED reading — it says the stream offered no window
	// at all, which is the property TestNoAnswerWriteIsABarePlacement pins.
	splices int
}

func (w *placementInterleaver) Write(p []byte) (int, error) {
	if w.inRepaint {
		return w.inner.Write(p)
	}
	if len(w.pending) > 0 {
		held := w.pending
		w.pending = nil
		if _, err := w.inner.Write(held); err != nil {
			return 0, err
		}
		// The window, in the order production delivers it: the placement reached
		// the terminal, the read loop's repaint landed, and THEN the text
		// arrived — so the text goes to whatever row the repaint left the cursor
		// on. Every window is interrupted (the loop wakes whenever it wakes), so
		// the construction does not depend on which emission happens to be first.
		if len(p) > 0 && unplacedText(p) {
			w.splices++
			w.inRepaint = true
			if w.repaint != nil {
				w.repaint()
			}
			w.inRepaint = false
		}
	}
	if len(p) > 0 && placementSeqLen(p) == len(p) {
		w.pending = append([]byte(nil), p...)
		return len(p), nil
	}
	return w.inner.Write(p)
}

// placementHarness builds the A5 chat layout with the answer renderer drawing
// through a writer the test can interleave into. It mirrors newA5HarnessSized
// except for the writer, because the interruption has to be injected where the
// REAL defect lives: the byte stream between the renderer and the terminal.
// There is no shipped seam for swapping the renderer's writer after
// construction (and there must not be one — a hot-swappable output is not a
// thing production needs), so the harness is built with the wrapper in place.
func placementHarness(t *testing.T) (*a5Harness, *recordingWriter, *placementInterleaver) {
	t.Helper()
	clock := &fakeClock{t: time.Unix(a5UnixTime, 0)}
	src := &scriptedSource{width: a5WideCols, height: a5Rows, clock: clock}
	rec := &recordingWriter{}
	inter := &placementInterleaver{inner: rec}
	ed := NewChatEditor(src, inter, promptForTest, nil, nil, nil)
	ed.SetClock(clock.Now)
	if !ed.EnableRegion() {
		t.Fatalf("EnableRegion(%d rows) = false, want true", a5Rows)
	}
	inter.repaint = func() { ed.repaint() }
	h := &a5Harness{ed: ed, out: rec, screen: NewScreen(a5WideCols, a5Rows), region: ed.region, answer: ed.Answer(), clock: clock, screenCols: a5WideCols, screenRows: a5Rows}
	h.answer.SetCols(a5WideCols)
	return h, rec, inter
}

// TestAnswerWriteIsAtomicWithItsPlacement is the regression guard for the
// interleaving above.
//
// RED (pre-fix renderer): the placement is one write and the span's text the
// next, so the editor's repaint lands between them and the span's characters go
// to the editing line's row — the band row keeps a blank run where they should
// have been.
// GREEN (fixed): placement and text ride ONE write, the repaint can only land
// BEFORE the pair, and every span reaches the band row.
func TestAnswerWriteIsAtomicWithItsPlacement(t *testing.T) {
	h, out, inter := placementHarness(t)

	h.answer.Begin()
	h.answer.BeginTurn(TurnInfo{Index: 1, Prompt: promptForTest})
	h.clock.advance(answerFlushWindow)
	// ONE line in the shape the failing chat-dump test streams: a bold span
	// surrounded by plain text, painted as SEVERAL pieces — each piece exactly
	// one (placement, text) unit. The break is INCLUDED because that is the
	// measured case: emphasis is decided when the line's break arrives, so the
	// spans are real by the time the pieces are written (the tap recorded
	// `CSI 1m "beta" CSI 0m`, bold applied — a decided span).
	h.answer.Delta("alpha **beta** and gamma\n")
	h.replay()

	if len(out.snapshot()) == 0 {
		t.Fatal("the harness emitted no writes at all: the assertion below would be vacuous")
	}
	// What the interruption DID, reported either way: on a fixed tree the stream
	// offers no window at all (splices == 0) and the assertion below passes by
	// the pair being one write; on the pre-fix tree every window is spliced and
	// the span's text is stolen. TestNoAnswerWriteIsABarePlacement is the guard
	// that fails RED on the window itself.
	t.Logf("placement windows interpolated by a read-loop repaint: %d", inter.splices)

	// THE OUTCOME: the line's text reaches ONE band row, contiguously. Pre-fix
	// the band row holds `alpha` + a blank run where `beta` belongs, because
	// that text went to the editing line's row instead.
	const want = "alpha beta and gamma"
	found := false
	for row := 0; row < h.screenRows; row++ {
		if strings.Contains(h.screen.Line(row), want) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no screen row carries %q — the interrupted write's text went to the row the read loop's repaint moved the cursor to\n--- screen ---\n%s",
			want, h.screen.Contents())
	}
	// And the editing line must NOT carry the answer: it owns its own row, and
	// its repaint is what erases whatever a misplaced burst left there.
	plan := h.region.Plan()
	if got := h.screen.Line(plan.EditorTop); strings.Contains(got, "beta") {
		t.Fatalf("the editing line's row %d carries the answer's span (%q): the placement and its text were NOT atomic, so the repaint separated them\n--- screen ---\n%s",
			plan.EditorTop, got, h.screen.Contents())
	}
}

// TestNoAnswerWriteIsABarePlacement pins the mechanism at its own seam, so the
// guard above cannot pass by accident on a cadence that happens to splice
// nothing: NO write may be a bare placement that positions text arriving in a
// DIFFERENT write. A bare placement that draws nothing and is followed by a
// write which places itself again (the menu's cursor hand-back) is fine — the
// interleaving then falls between two self-positioning units and is harmless.
// A bare placement followed by UNPLACED text is the defect: the read loop lands
// between them and that text goes to whatever row it left the cursor on.
func TestNoAnswerWriteIsABarePlacement(t *testing.T) {
	clock := &fakeClock{t: time.Unix(a5UnixTime, 0)}
	src := &scriptedSource{width: a5WideCols, height: a5Rows, clock: clock}
	rec := &recordingWriter{}
	ed := NewChatEditor(src, rec, promptForTest, nil, nil, nil)
	ed.SetClock(clock.Now)
	if !ed.EnableRegion() {
		t.Fatalf("EnableRegion(%d rows) = false, want true", a5Rows)
	}
	h := &a5Harness{ed: ed, out: rec, screen: NewScreen(a5WideCols, a5Rows), region: ed.region, answer: ed.Answer(), clock: clock, screenCols: a5WideCols, screenRows: a5Rows}
	h.answer.SetCols(a5WideCols)
	// A status source, so the status row's own placement+text pair is exercised
	// too — it shares this seam. And a menu, so the draw path (erase + line, the
	// rows placed absolutely below the editing line) is exercised.
	h.answer.SetStatus(func() Status { return Status{Label: "m", Live: "streaming"} })

	h.answer.Begin()
	h.answer.BeginTurn(TurnInfo{Index: 1, Prompt: promptForTest})
	h.clock.advance(answerFlushWindow)
	// Long enough to fill a row and WRAP it — the wrap path finalizes and
	// re-opens the live row, which is where the gutter and the row clear are
	// emitted — and with a span, so the emphasis pieces are exercised.
	h.answer.Delta("alpha **beta** and gamma " + strings.Repeat("tail ", a5WideCols))
	h.replay()

	// The completion menu, drawn through the SAME region, renderer and writer —
	// this is the very call the editor's repaint makes (Editor.repaint →
	// Menu.draw). It is driven directly because the menu's rows are placed
	// absolutely below the editing line, and that placement is one of the seams
	// this guard covers; letting a tab produce no candidates would leave the row
	// unexercised while the test still passed.
	menu := Menu{
		Candidates: []Candidate{{Text: "alpha"}, {Text: "beta"}, {Text: "gamma"}},
		Open:       true,
		Prefix:     "al",
		To:         2,
	}
	if drawn := menu.draw(h.region, h.ed.render, a5WideCols); len(drawn) == 0 {
		t.Fatal("the menu drew no rows: its placement+text seam would go unexercised")
	}
	h.replay()

	writes := rec.snapshot()
	if len(writes) == 0 {
		t.Fatal("the harness emitted no writes at all: the assertion below would be vacuous")
	}
	var torn []int
	var pair []string
	for i := 0; i+1 < len(writes); i++ {
		if len(writes[i]) == 0 || placementSeqLen(writes[i]) != len(writes[i]) {
			continue
		}
		next := writes[i+1]
		if len(next) == 0 {
			continue
		}
		// The next write must place itself BEFORE it draws anything, or it is
		// text positioned by a write the read loop can outrun.
		if unplacedText(next) {
			row, _ := placementSeqRow(writes[i])
			torn = append(torn, row)
			pair = append(pair, fmt.Sprintf("placement %q then %q", writes[i], next))
		}
	}
	if len(torn) != 0 {
		t.Fatalf("%d bare placement(s) position text that arrives in a DIFFERENT write (rows: %v)\n%v\nthe read loop can land between them and that text goes to whatever row it left the cursor on\n--- screen ---\n%s",
			len(torn), torn, pair, h.screen.Contents())
	}
}

// countBarePlacements counts the writes that are exactly one absolute cursor
// placement.
func countBarePlacements(writes [][]byte) int {
	var n int
	for _, w := range writes {
		if len(w) > 0 && placementSeqLen(w) == len(w) {
			n++
		}
	}
	return n
}

// unplacedText reports whether a write draws cells at a cell it does NOT name.
//
// "Draws cells" is the screen parser's own rule: a control sequence draws no
// cell (sgr.EscapeSeqLen), and a carriage return or a line feed moves without
// drawing. Anything else — a space included — puts a cell on the row the cursor
// happens to be on, and that is exactly the text an interleaving can steal.
//
// The safe shapes are the two the seam fix produces: a write that leads with an
// ABSOLUTE placement (it names its cell before drawing into it, whatever follows)
// and a write that draws nothing at all (erase, scroll, styling, newlines — there
// is no cell to misplace). A write that only STYLES is therefore not safe: an SGR
// chooses a rendition, never a position, so `ESC[1mbold ESC[0m` draws 16 cells
// wherever the cursor was left. That is shape (b) exactly — the frozen artifact's
// row lost the span's cells AND their attributes, because the whole piece, SGR
// bytes included, went to a row the read loop's repaint owned.
func unplacedText(p []byte) bool {
	i := 0
	for i < len(p) {
		if i == 0 && placementSeqLen(p) > 0 {
			return false // names the cell it draws into
		}
		if n := sgr.EscapeSeqLen(string(p[i:])); n > 0 {
			i += n // draws no cell: a rendition, an erase, a scroll
			continue
		}
		switch p[i] {
		case '\r', '\n':
			i++
			continue
		}
		return true // a cell-drawing byte at a cell nobody named
	}
	return false
}
