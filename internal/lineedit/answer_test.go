package lineedit

import (
	"io"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// A5 tests — scrollback-safe streaming and the one-line status.
//
// Every assertion below reads the RENDERED SCREEN (a vt100 cell grid fed by the
// bytes the components actually wrote), never a callback: "the renderer fired"
// cannot see a repainted row, a misplaced live row, or a status line drawn on
// top of the editing line. The grid models the scroll region too, so the
// scrollback invariant is asserted by the terminal's own rule (a region scroll
// leaves the rows outside it untouched).

// Screen shapes used below. They are named because the layout's arithmetic
// (editor row + menu + band + status) is only meaningful against a known
// geometry.
const (
	a5Rows     = 12
	a5Cols     = 40
	a5WideCols = 100 // wide enough for the whole status line (model · iter · tokens · live)
	a5SmallRow = 3   // one below rowsMinUsable: the layout must refuse to install
	a5UnixTime = 1_700_000_000
	// a5Wait / a5PollStep bound a wait for the editor's read loop to apply an
	// asynchronous event (a resize). The wait is not the assertion — the screen
	// is — so the deadline only turns a hang into a screen dump.
	a5Wait     = 2 * time.Second
	a5PollStep = 2 * time.Millisecond
)

// a5Harness is an editor over a scripted source WITH the reserved layout, plus
// the screen the terminal would show. Production wires exactly this
// (NewChatEditor + EnableRegion); only the source and the writer are scripted.
type a5Harness struct {
	ed     *Editor
	out    *recordingWriter
	screen *Screen
	region *region
	answer *AnswerRenderer
	clock  *fakeClock
	// fed counts the writes already replayed into screen. The screen is a
	// STATEFUL grid fed a stream of RELATIVE cursor motion, so replaying the
	// whole history again would not reproduce the screen — it would repaint it
	// (the answer lands a second time, the scrollback doubles). Every replay
	// is therefore incremental, and a NEW screen resets the counter. The
	// counter also answers the branch's objection to incremental feeding: only
	// writes never fed before are applied, so a read loop writing concurrently
	// with the assertions cannot append an already-seen write a second time.
	fed int

	// screenCols / screenRows are the geometry a REBUILT screen is created at
	// (replayDroppingLast starts from an empty grid). They follow a resize.
	screenCols int
	screenRows int
}

// newA5Harness builds the editor + screen at a5Rows × a5Cols.
func newA5Harness(t *testing.T, steps []scriptStep, hist *History) *a5Harness {
	t.Helper()
	h := newA5HarnessSized(t, steps, hist, a5Rows, a5Cols)
	return h
}

// newA5HarnessSized builds the editor + screen at an arbitrary geometry.
func newA5HarnessSized(t *testing.T, steps []scriptStep, hist *History, rows, cols int) *a5Harness {
	t.Helper()
	clock := &fakeClock{t: time.Unix(a5UnixTime, 0)}
	src := &scriptedSource{steps: steps, width: cols, height: rows, clock: clock}
	out := &recordingWriter{}
	ed := NewChatEditor(src, out, promptForTest, hist, nil, nil)
	ed.SetClock(clock.Now)
	if !ed.EnableRegion() {
		t.Fatalf("EnableRegion(%d rows) = false, want true", rows)
	}
	return &a5Harness{ed: ed, out: out, screen: NewScreen(cols, rows), region: ed.region, answer: ed.Answer(), clock: clock, screenCols: cols, screenRows: rows}
}

// replay feeds every recorded write into the screen that has not been fed yet,
// in order. It is INCREMENTAL, because it is called more than once per test (and
// from helpers): the screen is stateful and the stream moves the cursor
// relatively, so re-feeding the whole history repaints rather than reproduces —
// measured as a DOUBLED scrollback, which is the very thing A5 asserts on.
func (h *a5Harness) replay() {
	chunks := h.out.snapshot()
	for _, w := range chunks[h.fed:] {
		h.screen.Feed(w)
	}
	h.fed = len(chunks)
}

// resizeScreen replaces the terminal's grid (a resize changes what the terminal
// holds, not just what the renderer believes) and resets the replay counter: the
// new grid starts empty, so it must be fed the whole history.
func (h *a5Harness) resizeScreen(cols, rows int) {
	h.screen = NewScreen(cols, rows)
	h.screenCols, h.screenRows = cols, rows
	h.fed = 0
}

// replayDroppingLast feeds every write except the final one — the state the user
// looks at while the editor is still on screen (the finish write is separate).
func (h *a5Harness) replayDroppingLast() {
	n := h.out.count()
	if n > 0 {
		n--
	}
	h.screen = NewScreen(h.screenCols, h.screenRows)
	for _, w := range h.out.snapshot()[:n] {
		h.screen.Feed(w)
	}
	h.fed = n
}

// TestA5B_RegionInstallsScrollableBand is the layout's ground truth: the region
// confines the answer band, the band ends one row above the status row, and the
// scrolling region the terminal received matches the band exactly.
func TestA5B_RegionInstallsScrollableBand(t *testing.T) {
	h := newA5Harness(t, nil, nil)
	p := h.region.Plan()
	if p.StatusRow != a5Rows-1 {
		t.Fatalf("status row = %d, want %d (the last row)", p.StatusRow, a5Rows-1)
	}
	// THE SCROLLBACK CONDITION (A5's whole point): the band's top margin is the
	// screen's FIRST row, so a line pushed off it is SAVED by the terminal
	// instead of discarded at an interior margin. This assertion is what fails
	// if the layout is re-inverted.
	if p.BandTop != editorTopRow {
		t.Fatalf("band top = %d, want %d (the screen's first row) — an interior region top discards every committed line",
			p.BandTop, editorTopRow)
	}
	// The band ends above the chrome, and the scroll region the terminal
	// received matches it exactly.
	if p.BandBottom >= p.EditorTop {
		t.Fatalf("band bottom = %d, want it above the editing line (row %d)", p.BandBottom, p.EditorTop)
	}
	h.replay()
	top, bottom := h.screen.ScrollRegion()
	if top != p.BandTop || bottom != p.BandBottom {
		t.Fatalf("terminal scroll region = rows [%d,%d], want the band [%d,%d]", top, bottom, p.BandTop, p.BandBottom)
	}
	if top != editorTopRow {
		t.Fatalf("terminal scroll region top = %d, want %d (the screen's first row)", top, editorTopRow)
	}
	if h.region.BandRows() < regionRowsMin {
		t.Fatalf("band rows = %d, want at least %d", h.region.BandRows(), regionRowsMin)
	}
	// The chrome is BELOW the band: the editing line's rows start where the
	// band ends, the menu follows it, and the status row ends the screen.
	if p.EditorTop <= p.BandBottom || p.MenuTop < p.EditorTop {
		t.Fatalf("layout is not band → editor → menu: band [%d,%d] editor top %d menu top %d",
			p.BandTop, p.BandBottom, p.EditorTop, p.MenuTop)
	}
}

// TestA5B_DeltasNeverRepaintCommittedText is the increment's central claim: for
// a fixed delta stream, the finalized lines are written ONCE. The screen shows
// every line exactly once (so nothing was lost by scrolling), and the number of
// writes is bounded by the number of lines the stream produced — not by the
// number of deltas.
func TestA5B_DeltasNeverRepaintCommittedText(t *testing.T) {
	h := newA5Harness(t, nil, nil)
	band := h.answer
	band.SetCols(a5Cols)
	band.Begin()

	lines := []string{"alpha", "bravo", "charlie", "delta"}
	for _, line := range lines {
		// Each line arrives as three deltas (the in-flight text) followed by a
		// newline delta — the shape a token stream has.
		for _, piece := range []string{line[:2], line[2:], "\n"} {
			band.Delta(piece)
		}
	}
	band.Finish()
	h.replay()

	if got := band.Committed(); got != len(lines) {
		t.Fatalf("committed lines = %d, want %d (each finalized line exactly once)", got, len(lines))
	}
	// The repaint budget: one write per streamed delta (3 per line) plus the
	// commit scrolls (1 per line) plus Begin/Finish and the status rows. A
	// renderer that repainted the whole answer per delta would be quadratic;
	// this bound is linear.
	bound := len(lines)*4 + a5RepaintOverhead
	if band.Writes() > bound {
		t.Fatalf("writes = %d for %d deltas over %d lines, want <= %d — a repaint storm (the whole answer must never be redrawn)",
			band.Writes(), len(lines)*3, len(lines), bound)
	}
	// Screen truth: each line appears exactly once in the answer band.
	answer := h.bandContents()
	for _, line := range lines {
		if n := strings.Count(answer, line); n != 1 {
			t.Fatalf("the answer band shows %q %d times, want exactly 1\n--- band ---\n%s", line, n, answer)
		}
	}
	// The editing row and the status row are untouched by the answer.
	if strings.Contains(h.editorLine(), "alpha") {
		t.Fatalf("the answer was drawn on the editing row:\n%s", h.screen.Contents())
	}
}

// a5RepaintOverhead is the non-delta part of the write budget: the begin
// (clear + status), the finish (status) and the status repaint per line commit
// is NOT counted per line above, so a small allowance is named here rather than
// hidden as a literal.
const a5RepaintOverhead = 8

// TestA5B_LiveRowShowsOnlyInFlightAnswer is the "live region = the in-flight
// answer only" half: after a finalized line, the live row holds the NEXT line's
// partial text and nothing else — the committed line is not rewritten there.
func TestA5B_LiveRowShowsOnlyInFlightAnswer(t *testing.T) {
	h := newA5Harness(t, nil, nil)
	band := h.answer
	band.SetCols(a5Cols)
	band.Begin()

	band.Delta("first line\n")
	// The in-flight text reaches the live row when the cadence window elapses
	// (the same rule production follows): advance the fake clock, then give the
	// line its trailing space.
	band.Delta("second")
	h.clock.advance(answerFlushWindow)
	band.Delta(" ")
	h.replay()

	live := h.screen.Line(h.region.LiveRow())
	if strings.Contains(live, "first line") {
		t.Fatalf("the live row still shows the committed line: %q\n%s", live, h.screen.Contents())
	}
	if !strings.Contains(live, "second") {
		t.Fatalf("the live row = %q, want the in-flight text %q\n%s", live, "second", h.screen.Contents())
	}
	// The committed line is visible in the band, exactly once.
	bandText := h.bandContents()
	if n := strings.Count(bandText, "first line"); n != 1 {
		t.Fatalf("committed line appears %d times, want 1\n--- band ---\n%s", n, bandText)
	}
}

// TestA5B_ScrollRegionNeverMovesTheChrome is the scrollback-safety invariant
// stated as the terminal states it: the editing line and the status row are
// OUTSIDE the scroll region, so committing answer lines (which scroll the band)
// cannot move them.
func TestA5B_ScrollRegionNeverMovesTheChrome(t *testing.T) {
	h := newA5Harness(t, nil, nil)
	h.answer.SetCols(a5Cols)
	h.answer.Begin()
	// Draw a status line so the row below the band has content to check.
	h.ed.prompt = promptForTest
	h.ed.render.Prompt(promptForTest)
	h.ed.val.SetText("typing here")
	h.ed.repaint()
	h.answer.SetStatus(func() Status { return Status{Label: "model-x", Live: "ready"} })
	h.answer.Tick()
	h.replay()

	editorBefore := h.editorLine()
	statusBefore := h.screen.Line(h.region.Plan().StatusRow)
	if !strings.Contains(editorBefore, "typing here") {
		t.Fatalf("editing row = %q, want the typed text before the answer runs", editorBefore)
	}
	if !strings.Contains(statusBefore, "model-x") {
		t.Fatalf("status row = %q, want the status line", statusBefore)
	}

	// Now stream enough answer lines to scroll the band many times over.
	for i := 0; i < h.region.BandRows()*3; i++ {
		h.answer.Delta("answer row\n")
	}
	h.replay()

	if got := h.editorLine(); got != editorBefore {
		t.Fatalf("the editing row moved/changed under the answer: %q -> %q", editorBefore, got)
	}
	if got := h.screen.Line(h.region.Plan().StatusRow); got != statusBefore {
		t.Fatalf("the status row changed under the answer: %q -> %q", statusBefore, got)
	}
}

// TestA5B_AnswerLongerThanBandKeepsTheNewestRows is the honest bound of the
// layout: the band shows the newest answer text (the region's scroll rule), and
// the rows above it — the chrome — stay put. The doc claims exactly this.
func TestA5B_AnswerLongerThanBandKeepsTheNewestRows(t *testing.T) {
	h := newA5Harness(t, nil, nil)
	h.answer.SetCols(a5Cols)
	h.answer.Begin()
	bandRows := h.region.BandRows()
	for i := 0; i < bandRows*2; i++ {
		h.answer.Delta("row-" + string(rune('a'+i%26)) + "\n")
	}
	h.answer.Delta("newest\n")
	h.replay()

	bandText := h.bandContents()
	if !strings.Contains(bandText, "newest") {
		t.Fatalf("the newest answer line is not visible in the band:\n%s", bandText)
	}
	if got := h.editorLine(); strings.TrimSpace(got) != "" {
		t.Fatalf("the editing row carries answer text: %q", got)
	}
}

// TestA5B_CommittedLineSurvivesInScrollback is THE A5 assertion, and the one the
// previous layout could not pass.
//
// A terminal saves a line into its scrollback only when the line is pushed off
// the SCREEN's first row. The band is anchored there (BandTop == 0) and only its
// height varies, so every finalized line must be pushed off the screen's top row
// and must therefore be readable in the scrollback — for as long as the answer
// runs, however many lines it has.
//
// The negative control lives beside it (TestA5B_InteriorRegionTopDiscardsLines):
// the same bytes through a region whose top is one row lower lose the lines —
// which is exactly what the inverted layout was measured doing on real xterm 372
// before this change.
func TestA5B_CommittedLineSurvivesInScrollback(t *testing.T) {
	h := newA5Harness(t, nil, nil)
	h.answer.SetCols(a5Cols)
	// The editing line the answer must not disturb (drawn the way the editor
	// draws it: the user's text on the layout's editing row).
	h.ed.prompt = promptForTest
	h.ed.render.Prompt(promptForTest)
	h.ed.val.SetText("typing here")
	h.ed.repaint()
	h.answer.Begin()

	const first = "PROBE-LINE-01"
	const lines = 24 // far more than the band can show at once
	h.answer.Delta(first + "\n")
	for i := 2; i <= lines; i++ {
		h.answer.Delta("PROBE-LINE-" + twoDigits(i) + "\n")
	}
	h.answer.Finish()
	h.replay()

	// 1. The scrollback carries the whole answer, oldest line first — the first
	//    line is STILL reachable after 23 later lines.
	back := h.screen.ScrollbackText()
	if !strings.Contains(back, first) {
		t.Fatalf("the first committed line %q is not in the scrollback after %d lines — a committed line was destroyed\n--- scrollback (%d lines) ---\n%s\n--- screen ---\n%s",
			first, lines, len(h.screen.Scrollback()), back, h.screen.Contents())
	}
	// 2. Every line is still REACHABLE exactly once — in the scrollback if it
	//    has already scrolled out, on the band if it is still visible. A line
	//    that reached neither (dropped at an interior margin) or both (painted
	//    twice) fails here.
	visible := h.bandContents()
	for i := 1; i <= lines; i++ {
		want := "PROBE-LINE-" + twoDigits(i)
		if n := strings.Count(back, want) + strings.Count(visible, want); n != 1 {
			t.Fatalf("PROBE-LINE-%s is reachable %d times across scrollback+band, want exactly 1 (0 = destroyed, 2+ = duplicated)\n--- scrollback ---\n%s\n--- band ---\n%s",
				twoDigits(i), n, back, visible)
		}
	}
	// 3. The live row carries no committed text: the answer's last line is the
	//    only thing that ever sits there, and Finish leaves it blank.
	p := h.region.Plan()
	if got := h.screen.Line(p.BandBottom); strings.Contains(got, "PROBE-LINE") {
		t.Fatalf("the live row still shows committed text: %q", got)
	}
	// 4. The chrome did not scroll away with the answer: the editing line is
	//    still where the layout put it and still carries the user's text.
	if got, want := h.editorLine(), "typing here"; !strings.Contains(got, want) {
		t.Fatalf("the editing line did not survive %d streamed lines: %q, want %q\n--- screen ---\n%s",
			lines, got, want, h.screen.Contents())
	}
}

// TestA5B_InteriorRegionTopDiscardsLines is the NEGATIVE CONTROL for the rule
// the layout rests on: the identical byte stream through a scroll region whose
// top margin is NOT the screen's first row loses every line that scrolls off
// that interior margin. It is what makes the positive test meaningful — without
// it, a cell grid that agrees with any layout could not tell survival from
// destruction.
//
// The probe drives the answer renderer against a region whose top margin was
// then moved one row down ON THE WIRE (a later DECSTBM wins), so the only
// difference between the two runs is the margin the terminal is scrolling.
func TestA5B_InteriorRegionTopDiscardsLines(t *testing.T) {
	const lines = 10
	run := func(interior bool) (string, string) {
		var out strings.Builder
		reg := newTestRegion(&out, a5Rows, a5Cols)
		if !reg.Enable(a5Rows, a5Cols) {
			t.Fatalf("Enable() = false, want true")
		}
		if interior {
			// The pre-fix geometry: the region's top margin one row BELOW the
			// screen's first row (the band used to sit under the editing line).
			out.WriteString(cursorTo(editorTopRow+1, 1))
			out.WriteString(escCSI + strconv.Itoa(editorTopRow+2) + ";" + strconv.Itoa(reg.Plan().BandBottom+1) + "r")
		}
		band := NewAnswerRenderer(reg, nil)
		band.SetCols(a5Cols)
		band.Begin()
		for i := 1; i <= lines; i++ {
			band.Delta("PROBE-LINE-" + twoDigits(i) + "\n")
		}
		band.Finish()
		s := NewScreen(a5Cols, a5Rows)
		s.Feed([]byte(out.String()))
		return s.ScrollbackText(), s.Contents()
	}
	anchored, _ := run(false)
	interior, screen := run(true)
	if !strings.Contains(anchored, "PROBE-LINE-01") {
		t.Fatalf("a band anchored at the screen's first row lost the first line:\n%s", anchored)
	}
	if strings.Contains(interior, "PROBE-LINE-01") {
		t.Fatalf("a band whose top margin is an INTERIOR row kept the first line — the model does not implement the terminal's rule, so the positive test proves nothing:\n%s", interior)
	}
	if len(interior) >= len(anchored) {
		t.Fatalf("the interior-top run saved %d lines, the anchored run %d — the interior case must save FEWER (that is the defect this layout fixes)\n--- screen ---\n%s",
			len(interior), len(anchored), screen)
	}
}

// TestA5B_RepaintCountIsBoundedByTheAnswerShape keeps the streaming path honest:
// the work is linear in the answer's LINES, not in the number of deltas. A
// regression that repainted the whole answer per delta would still render
// correctly — only the write count would explode — so the screen assertions
// above cannot catch it; this one can.
func TestA5B_RepaintCountIsBoundedByTheAnswerShape(t *testing.T) {
	const deltasPerLine = 4
	h := newA5Harness(t, nil, nil)
	h.answer.SetCols(a5Cols)
	h.answer.Begin()
	const lines = 13
	for i := 1; i <= lines; i++ {
		line := "PROBE-LINE-" + twoDigits(i)
		for piece := 0; piece < deltasPerLine; piece++ {
			lo := piece * len(line) / deltasPerLine
			hi := (piece + 1) * len(line) / deltasPerLine
			h.answer.Delta(line[lo:hi])
		}
		h.answer.Delta("\n")
	}
	h.answer.Finish()
	h.replay()
	if got, want := h.answer.Committed(), lines; got != want {
		t.Fatalf("committed lines = %d, want %d", got, want)
	}
	// Each line costs one in-place live-row write per delta plus one scroll for
	// the break — bounded by the deltas it arrived in, never by the answer's
	// grown length.
	budget := lines*deltasPerLine + lines + a5RepaintOverhead
	if got := h.answer.Writes(); got > budget {
		t.Fatalf("the band wrote %d times for %d lines of %d deltas, want at most %d — the repaint count is growing with the answer, not with the deltas",
			got, lines, lines*deltasPerLine, budget)
	}
}

// twoDigits formats n (1..99) as a two-digit number.
func twoDigits(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

// newTestRegion builds a region over an io.Writer (the region's own wire path).
func newTestRegion(w io.Writer, rows, cols int) *region {
	return &region{rows: rows, cols: cols, outFn: w}
}

// TestA5B_StatusLineShowsModelIterTokens asserts the status line's CONTENT from
// the counters the REPL keeps (model name, iteration, running tokens).
func TestA5B_StatusLineShowsModelIterTokens(t *testing.T) {
	// A terminal WIDE enough for the whole status line: the assertion below is
	// that the three counters are all shown, which only holds when the row can
	// hold them (the narrow case has its own test, and it drops WHOLE fields).
	h := newA5HarnessSized(t, nil, nil, a5Rows, a5WideCols)
	h.answer.SetCols(a5WideCols)
	src := &chatStatusSourceProbe{model: "glm-5.3-flash", iter: 3, tokens: 12345}
	h.ed.SetStatus(src.status)
	// Begin PUBLISHES (it no longer draws: its caller is the turn's own
	// goroutine, and the read loop owns every write — see Begin's doc). This
	// harness has no read loop, so the TEST takes the loop's role and draws the
	// publish with Tick, exactly as the editor's repaint does in production.
	h.answer.Begin()
	h.answer.Tick()
	h.replay()

	row := h.screen.Line(h.region.Plan().StatusRow)
	for _, want := range []string{"glm-5.3-flash", "iter 3", "12.3k"} {
		if !strings.Contains(row, want) {
			t.Fatalf("status row = %q, want it to contain %q", row, want)
		}
	}

	// The live hint changes while a turn runs, and does not move the row.
	src.live = 4
	h.answer.Tick()
	h.replay()
	row2 := h.screen.Line(h.region.Plan().StatusRow)
	if !strings.Contains(row2, "iter 4") {
		t.Fatalf("status row during a turn = %q, want the live iteration", row2)
	}
	if !strings.Contains(row2, a5StatusLive) {
		t.Fatalf("status row during a turn = %q, want %q", row2, a5StatusLive)
	}
}

// TestA5B_NarrowStatusDropsWholeFields is the narrow-terminal policy: when the
// status line does not fit, trailing FIELDS are dropped whole. The alternative —
// truncating the string — rendered "· 12.3k to", which reads as corrupted data
// rather than as a narrow terminal. The live hint is never dropped (it is the
// part that changes), and what survives is always a prefix of complete fields.
func TestA5B_NarrowStatusDropsWholeFields(t *testing.T) {
	// A width that fits the model name and the iteration plus the live hint, but
	// not the token field.
	const narrowCols = 34
	h := newA5HarnessSized(t, nil, nil, a5Rows, narrowCols)
	h.answer.SetCols(narrowCols)
	src := &chatStatusSourceProbe{model: "glm-5.3-flash", iter: 3, tokens: 12345}
	h.ed.SetStatus(src.status)
	h.answer.Begin()
	h.answer.Tick() // the read loop's draw (see the test above): Begin publishes only
	h.replay()

	row := h.screen.Line(h.region.Plan().StatusRow)
	if !strings.Contains(row, "glm-5.3-flash") || !strings.Contains(row, "iter 3") {
		t.Fatalf("status row = %q, want the leading fields kept", row)
	}
	if !strings.Contains(row, a5StatusIdle) {
		t.Fatalf("status row = %q, want the live hint kept", row)
	}
	if strings.Contains(row, "12.3k tokens") {
		t.Fatalf("status row = %q — the token field fits after all; the test's width is not narrow", row)
	}
	// No fragment of a dropped field is left behind.
	for _, fragment := range []string{"12.3k", "k to", "tokens"} {
		if strings.Contains(row, fragment) {
			t.Fatalf("status row = %q shows a fragment %q of a dropped field", row, fragment)
		}
	}

	// One cell narrower and even the iteration field goes — still no fragment.
	h2 := newA5HarnessSized(t, nil, nil, a5Rows, len("glm-5.3-flash")+len(a5StatusIdle)+3)
	h2.answer.SetCols(len("glm-5.3-flash") + len(a5StatusIdle) + 3)
	h2.ed.SetStatus(src.status)
	h2.answer.Begin()
	h2.answer.Tick() // the read loop's draw (Begin publishes only — see Begin's doc)
	h2.replay()
	row2 := h2.screen.Line(h2.region.Plan().StatusRow)
	if !strings.Contains(row2, "glm-5.3-flash") || strings.Contains(row2, "turn") {
		t.Fatalf("status row at the narrower width = %q, want only the leading field", row2)
	}
}

// chatStatusSourceProbe is the test's status source (the production one lives in
// cmd/agent; the renderer must not care where the numbers come from).
type chatStatusSourceProbe struct {
	model  string
	iter   int
	live   int
	tokens int
}

// status renders the probe as a line (same shape as the production source).
func (p *chatStatusSourceProbe) status() Status {
	label := p.model + " · " + "iter " + a5Itoa(p.iterOrLive())
	if p.tokens > 0 {
		label += " · " + formatTestCount(p.tokens) + " tokens"
	}
	live := a5StatusIdle
	if p.live > 0 {
		live = a5StatusLive
	}
	return Status{Label: label, Live: live}
}

// iterOrLive prefers the live turn when one is running.
func (p *chatStatusSourceProbe) iterOrLive() int {
	if p.live > 0 {
		return p.live
	}
	return p.iter
}

// a5Itoa is a tiny int formatter (the tests avoid importing strconv for one
// call, and history_test.go already owns `itoa`).
func a5Itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

// formatTestCount mirrors the production compact count (12345 → 12.3k) so the
// assertion reads the same string the user sees.
func formatTestCount(n int) string {
	if n < 1000 {
		return a5Itoa(n)
	}
	whole := n / 1000
	frac := (n % 1000) / 100
	return a5Itoa(whole) + "." + a5Itoa(frac) + "k"
}

// a5StatusIdle / a5StatusLive mirror the production live hints (cmd/agent), so
// the probe's label matches what the user's terminal would show.
const (
	a5StatusIdle = "ready"
	a5StatusLive = "thinking…"
)

// TestA5B_TooSmallTerminalRefusesTheLayout is the degradation half: a terminal
// that cannot host the layout must NOT install a broken one — the caller then
// keeps full-screen output and says so (Rule 15, asserted here at the source).
func TestA5B_TooSmallTerminalRefusesTheLayout(t *testing.T) {
	clock := &fakeClock{t: time.Unix(a5UnixTime, 0)}
	src := &scriptedSource{width: a5Cols, height: a5SmallRow, clock: clock}
	out := &recordingWriter{}
	ed := NewChatEditor(src, out, promptForTest, nil, nil, nil)
	if ed.EnableRegion() {
		t.Fatalf("EnableRegion on a %d-row terminal = true, want false (the layout needs %d rows)", a5SmallRow, RowsMinUsable())
	}
	if ed.RegionActive() {
		t.Fatal("region active on a too-small terminal")
	}
	if ed.Answer() != nil {
		t.Fatal("an answer band was handed out on a too-small terminal")
	}
	for _, w := range out.snapshot() {
		if strings.Contains(string(w), escCSI+"r") || strings.Contains(string(w), "H") {
			t.Fatalf("a refused layout still wrote placement/region sequences: %q", string(w))
		}
	}
}

// TestA5B_RegionClipKeepsTheEditingLineVisible is the display budget's contract:
// a long editing line is clipped to the terminal's DERIVED row budget
// (editorRowsBudget), so it can never push the band out of the terminal — the
// bound is the terminal's room, not a fixed number (U1: a fixed 4 clipped the
// user's own typing off a terminal with room to show it).
func TestA5B_RegionClipKeepsTheEditingLineVisible(t *testing.T) {
	h := newA5Harness(t, nil, nil)
	// A single word so the layout wraps deterministically at the terminal
	// width, then more rows than the budget allows.
	h.ed.val.SetText(strings.Repeat("x", a5Cols*editorRowsCap*2))
	h.ed.repaint()
	h.replay()
	rows, _, _ := layoutText(h.ed.render.prompt, h.ed.val.Text(), h.ed.val.Cursor(), a5Cols)
	budget := editorRowsBudget(a5Rows)
	if len(rows) <= budget {
		t.Fatalf("the test needs a line taller than the terminal's row budget (rows=%d, budget=%d)", len(rows), budget)
	}
	if got := h.region.Plan().EditorRows; got > budget {
		t.Fatalf("the layout reserved %d editing rows, want at most the terminal's budget %d", got, budget)
	}
	// The band still exists (the cap did not eat it).
	if h.region.BandRows() < regionRowsMin {
		t.Fatalf("band rows = %d, want at least %d after a clipped edit line", h.region.BandRows(), regionRowsMin)
	}
}

// TestA5B_SplitRuneIsHeldBackUntilWhole is the reader-safety half of the
// streaming policy: a delta boundary and a UTF-8 rune boundary are independent,
// so the renderer must not write half a rune onto the live row — a half rune
// renders as a replacement character, and once committed it is in the
// scrollback forever. The assertion is on the RENDERED CELLS.
//
// The case exercised is the one where a split rune is actually REACHABLE: a
// plain cadence flush cannot emit one (a partial rune never ends in a space, so
// it is never "ready"), but the buffer-size flush can — it fires on byte count
// alone, whatever the tail looks like.
func TestA5B_SplitRuneIsHeldBackUntilWhole(t *testing.T) {
	h := newA5Harness(t, nil, nil)
	band := h.answer
	band.SetCols(a5Cols)
	band.Begin()

	// Fill the buffer to one byte short of the size flush, then push the FIRST
	// byte of a 3-byte rune: the cap trips with a partial rune at the tail.
	band.Delta(strings.Repeat("y", answerMaxBufferedBytes-1))
	raw := []byte("答")
	for i := 0; i < len(raw); i++ {
		band.Delta(string(raw[i : i+1]))
	}
	h.clock.advance(answerFlushWindow)
	band.Delta(" ")
	band.Delta("\n")
	h.replay()

	seen := h.screen.Contents()
	if strings.ContainsRune(seen, utf8.RuneError) {
		t.Fatalf("a replacement character reached the screen — half a rune was written onto the live row:\n%s", seen)
	}
	bandText := h.bandContents()
	if !strings.Contains(bandText, string(raw)) {
		t.Fatalf("the committed line does not carry %q — the rune was lost or split:\n%s", string(raw), bandText)
	}
	if strings.ContainsRune(bandText, utf8.RuneError) {
		t.Fatalf("the band shows a replacement character:\n%s", bandText)
	}
}

// TestA5B_ChromeHoldsUnderALongAnswer is the scrollback-safety claim as the
// terminal's own rule states it (the region's DECSTBM contract): the editing row
// and the status row are OUTSIDE the scroll region, so no amount of answer text
// can move them. The same geometry is asserted once per row count here because a
// band of a different height is a different scroll region.
func TestA5B_ChromeHoldsUnderALongAnswer(t *testing.T) {
	for _, rows := range []int{a5Rows, a5Rows + 4, a5Rows + 9} {
		h := newA5HarnessSized(t, nil, nil, rows, a5Cols)
		h.ed.val.SetText("line being edited")
		h.ed.repaint()
		h.ed.SetStatus(func() Status { return Status{Label: "glm-5.3-flash", Live: a5StatusIdle} })
		h.answer.SetCols(a5Cols)
		h.answer.Begin()
		h.answer.Tick()
		h.replay()
		p := h.region.Plan()
		editorBefore, statusBefore := h.editorLine(), h.screen.Line(p.StatusRow)

		for i := 0; i < p.BandBottom-p.BandTop+1+3; i++ {
			h.answer.Delta("pushed answer row\n")
		}
		h.answer.Finish()
		h.replay()

		if got := h.editorLine(); !strings.Contains(got, "line being edited") {
			t.Fatalf("rows=%d: the editing row lost the typed text under a long answer: %q (was %q)", rows, got, editorBefore)
		}
		if got := h.screen.Line(p.StatusRow); !strings.Contains(got, "glm-5.3-flash") {
			t.Fatalf("rows=%d: the status row lost the status line: %q (was %q)", rows, got, statusBefore)
		}
		if top, bottom := h.screen.ScrollRegion(); top != p.BandTop || bottom != p.BandBottom {
			t.Fatalf("rows=%d: scroll region [%d,%d] != band [%d,%d]", rows, top, bottom, p.BandTop, p.BandBottom)
		}
	}
}

// TestA5B_NoBreakDeltasReachTheLiveRowOnTheCadence is the regression for the
// measured defect this test was written against (task 01a0d9ab, PTY evidence:
// a word-sized delta stream painted the answer only at the line breaks, and a
// single long no-break delta painted NOTHING until Finish).
//
// The shape under test is the provider's own: a carrier that sends text_delta
// frames with no newline in them. `ready` gates the cadence on the buffer
// ending in a SPACE, so a buffer whose tail is still the middle of a word is
// held for as long as the model keeps streaming that word — and with no newline
// in the whole delta stream, nothing is painted until Finish. The fix flushes
// on the CADENCE WINDOW once the window's worth of text has arrived, whatever
// its last byte is.
//
// The assertion is on the SCREEN while the answer is still streaming, not at the
// end: a renderer that paints everything only at Finish passes an end-of-turn
// assertion while showing the user nothing for the whole turn — that is exactly
// the defect, so the check is made BEFORE Finish.
//
// The case the old (word-boundary) gate got wrong is the one the provider
// produces whenever a word is not yet complete: a delta whose tail is mid-word
// is held until the NEXT space arrives, so the reader sees nothing while the
// model streams. This test therefore drives a burst that is ENTIRELY inside one
// word — the shape a split token has — and asserts the text is on screen for it.
//
// The cadence is consulted by drain, i.e. WHEN A DELTA ARRIVES: the window
// decides whether the text already in flight is painted by the next burst. The
// assertion is placed accordingly — text that was in flight for more than a
// window must be on screen by the time the next burst lands.
func TestA5B_NoBreakDeltasReachTheLiveRowOnTheCadence(t *testing.T) {
	h := newA5Harness(t, nil, nil)
	band := h.answer
	band.SetCols(a5Cols)
	band.Begin()

	// A word-sized delta with no newline anywhere — the provider's own shape,
	// and DELIBERATELY mid-word: there is no space at its tail for the old gate
	// to grab hold of.
	band.Delta("streaming")
	h.clock.advance(answerFlushWindow)
	band.Delta(" still")
	h.replay()

	live := strings.TrimRight(h.screen.Line(h.region.LiveRow()), " ")
	if live != "streaming still" {
		t.Fatalf("after a cadence window the live row = %q, want %q — a no-break delta is held until a line break or the next space (nothing reached the screen while the model would be streaming)\n%s",
			live, "streaming still", h.screen.Contents())
	}
	if n := band.Committed(); n != 0 {
		t.Fatalf("committed = %d, want 0 — nothing has been finalized yet (the line is still in flight)", n)
	}
	// And it keeps advancing, burst by burst, with no line break anywhere.
	band.Delta(" arrives")
	h.clock.advance(answerFlushWindow)
	band.Delta(" now")
	h.replay()
	if got := strings.TrimRight(h.screen.Line(h.region.LiveRow()), " "); got != "streaming still arrives now" {
		t.Fatalf("live row = %q, want %q — the in-flight line stopped advancing\n%s",
			got, "streaming still arrives now", h.screen.Contents())
	}
	// The tail of the answer must still be painted by Finish, not dropped. On
	// Finish the live line is COMMITTED into the band (the A5 contract: the
	// final line stays live for the whole answer and is finalized once), so the
	// assertion is on the band — the live row is deliberately left empty.
	band.Finish()
	h.replay()
	if got := h.bandContents(); !strings.Contains(got, "streaming still arrives now") {
		t.Fatalf("after Finish the band = %q, want the whole streamed line %q", got, "streaming still arrives now")
	}
}

// TestA5B_InFlightBurstsAppendToTheLiveRow is the regression for a defect this
// increment shipped and then fixed: the live row was written with an absolute
// location at column 0 on EVERY cadence burst, so a line that arrived as several
// bursts (the normal case — "alpha " then "bravo ") had each burst overwrite the
// one before it. The screen showed "charlie avo" and no test that only counted
// writes could see it. The assertion is on the rendered row.
func TestA5B_InFlightBurstsAppendToTheLiveRow(t *testing.T) {
	h := newA5Harness(t, nil, nil)
	band := h.answer
	band.SetCols(a5Cols)
	band.Begin()
	for _, word := range []string{"alpha ", "bravo ", "charlie "} {
		band.Delta(word)
		h.clock.advance(answerFlushWindow)
	}
	h.replay()

	live := h.screen.Line(h.region.LiveRow())
	if got := strings.TrimRight(live, " "); got != "alpha bravo charlie" {
		t.Fatalf("live row = %q, want the whole in-flight line %q\n%s", got, "alpha bravo charlie", h.screen.Contents())
	}
	if n := band.Committed(); n != 0 {
		t.Fatalf("committed = %d, want 0 — nothing has been finalized yet", n)
	}
}

// TestA5B_WideInFlightLineIsFinalizedNotDropped pins the other half of the same
// policy: a single answer line longer than the terminal must lose NOTHING. A
// burst that fills the live row is finalized with one scroll and writing
// continues on the fresh row, so every character still reaches the band (the
// pre-fix behaviour truncated at the row's end).
func TestA5B_WideInFlightLineIsFinalizedNotDropped(t *testing.T) {
	h := newA5Harness(t, nil, nil)
	band := h.answer
	band.SetCols(a5Cols)
	band.Begin()

	// Four terminal rows of one unbroken word, then a break to flush it.
	oneRow := strings.Repeat("w", a5Cols)
	band.Delta(oneRow + oneRow + oneRow + oneRow + "\n")
	h.replay()

	if got := band.Committed(); got < 4 {
		t.Fatalf("committed rows = %d, want at least 4 — a line wider than the terminal must wrap, not truncate", got)
	}
	// Every cell is the word: nothing was dropped at the wrap boundary.
	//
	// The rows are read with the cut label folded away (cutReadingFold): this line
	// is 160 word characters of ONE word, so every row's cut lands between two of
	// them, and a row that ran out of room spends its last CELL on the label and
	// hands that character to the row below (that is the cut vocabulary, not a
	// loss). The label's cell is the only thing the marked row does not spend on
	// the word, which is why the total is asserted rather than each row's width: a
	// row is full either way (a5Cols cells), and the band must still hold all 160
	// characters of the word.
	total := 0
	for r := h.region.Plan().BandTop - 1; r <= h.region.Plan().BandBottom; r++ {
		row := strings.TrimRight(h.screen.Line(r), " ")
		if row == "" {
			continue
		}
		if strings.Trim(cutReadingFold(row), "w") != "" {
			t.Fatalf("band row %d = %q, want only the wrapped word", r, row)
		}
		if len([]rune(row)) != a5Cols && len([]rune(row)) != len([]rune(cutReadingFold(row))) {
			t.Fatalf("band row %d holds %d cells, want a full row of %d — text was dropped at the wrap boundary", r, len([]rune(row)), a5Cols)
		}
		total += len([]rune(cutReadingFold(row)))
	}
	if want := len([]rune(oneRow)) * 4; total != want {
		t.Fatalf("the band holds %d cells of the word, want %d — text was dropped at the wrap boundary", total, want)
	}
}

// TestA5B_AnswerWidthFollowsAResize is the resize half of the width wiring: the
// renderer's drawing width is the terminal's, and it follows a signal-driven
// resize. A stale width would wrap the answer at the OLD size — and since a
// too-wide burst is finalized at the row boundary, a stale NARROW width would
// split answer rows a wider terminal had room for. Asserted on the screen: after
// the resize, one full new-width row is committed as ONE row.
func TestA5B_AnswerWidthFollowsAResize(t *testing.T) {
	const newCols = 60
	// A real SIGWINCH delivered to the running editor: the resize reaches the
	// renderer only if the EDITOR propagates it (the test never calls SetCols
	// itself — that would prove only that the setter works).
	h := newA5Harness(t, []scriptStep{{winch: true, width: newCols}}, nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := h.ed.Run(); err != nil {
			t.Errorf("editor run: %v", err)
		}
	}()
	// Wait for the loop to RETURN, not for a value it writes into the region:
	// reading h.region.Cols() while Run() is still resizing it is a race inside
	// the HARNESS (measured under -race). The script is one step long, so the run
	// ends as soon as the resize has been applied — the same assertion, taken
	// without a concurrent read. The deadline only turns a hang into a dump.
	select {
	case <-done:
	case <-time.After(a5Wait):
		t.Fatalf("timed out waiting for the editor to apply the resize (region cols still %d, want %d)", h.region.Cols(), newCols)
	}
	if got := h.region.Cols(); got != newCols {
		t.Fatalf("region cols after the resize = %d, want %d — the editor did not apply it", got, newCols)
	}
	if !h.region.Active() {
		t.Fatal("the resize deactivated the region — the assertions below need the reserved layout")
	}
	// The editor's read loop has ended (the script is exhausted, and the wait
	// above proved it RETURNED); the region is left installed on purpose, so the
	// renderer really draws through it.
	// The terminal's own grid grew too (a resize changes what the terminal
	// holds, not just what the renderer believes).
	h.resizeScreen(newCols, a5Rows)

	// The renderer must now draw at the NEW width. A line longer than the old
	// width but shorter than the new one commits as ONE row: a renderer left at
	// the old width splits it, which is exactly the defect this pins.
	h.answer.Begin()
	oneRow := strings.Repeat("r", a5Cols+5)
	h.answer.Delta(oneRow + "\n")
	h.replay()
	if got := h.answer.Committed(); got != 1 {
		t.Fatalf("committed rows = %d for one %d-cell line on a %d-col terminal, want 1 — the renderer kept the OLD width",
			got, len(oneRow), newCols)
	}
	found := false
	for r := h.region.Plan().BandTop - 1; r <= h.region.Plan().BandBottom; r++ {
		if strings.TrimRight(h.screen.Line(r), " ") == oneRow {
			found = true
		}
	}
	if !found {
		t.Fatalf("no band row holds the whole %d-cell line — it was split at the old width\n%s", len(oneRow), h.screen.Contents())
	}
}

// editorLine is the editing row as the screen currently shows it. The reserved
// band sits ABOVE the editing line (that is the inversion A5 needs), so no
// assertion may assume the editing line is screen row 0 — it asks the layout.
func (h *a5Harness) editorLine() string {
	return h.screen.Line(h.region.EditorTop())
}

// bandContents renders the answer band's rows as text (the scrollback-safety
// assertions read this, never the whole screen — the chrome must not be able to
// satisfy an answer assertion by accident).
func (h *a5Harness) bandContents() string {
	p := h.region.Plan()
	var lines []string
	for r := p.BandTop; r <= p.BandBottom; r++ {
		if r < 0 || r >= h.screen.Rows {
			continue
		}
		lines = append(lines, cutReadingFold(h.screen.Line(r)))
	}
	return strings.Join(lines, "\n")
}

// TestA5B_BreakDeltaWithATailDoesNotDuplicateTheTail is the regression guard
// for a defect the live-reasoning routing exposed, and it uses the exact delta
// shapes a real endpoint sent (captured 2026-09-19):
//
//	delta 1: "…the read too"                       (no line break: in flight)
//	delta 2: "l resolves … wasted work.\nThen I answer briefly.\n"
//
// Delta 2 carries a line break AND the next line's beginning. The tail behind
// the break was PAINTED on the live row and then left in the pending buffer, so
// the next drain appended to it and committed it a second time. The screen
// showed the whole head twice, joined mid-row:
//
//	The path is devdoc/chat-mode-report.md, relative to the repo root, and the read tooThe path is devdo
//
// c/chat-mode-report.md, …
//
// The property under test is the one a reader can see: every character the model
// sent appears exactly ONCE, in order, across the committed answer.
func TestA5B_BreakDeltaWithATailDoesNotDuplicateTheTail(t *testing.T) {
	h := newA5HarnessSized(t, nil, nil, 30, 100)
	band := h.answer
	band.SetCols(100)
	band.Begin()

	head := "The path is devdoc/chat-mode-report.md, relative to the repo root, and the read too"
	rest := "l resolves relative paths against the working directory, so one call is enough and a search would be wasted work."
	// The real shapes, verbatim from the captured stream: the FIRST delta
	// carries the break AND the next line's head (that is what makes the head
	// both painted and queued), and the second carries the rest of the line.
	band.Delta("The user asks what is in the file. I should read it.\n" + head)
	band.Delta(rest + "\nThen I answer briefly.\n")
	band.Finish()
	h.replay()

	// Screen truth: fold the band to single spaces and compare against the same
	// fold of what the model sent. A duplicated tail or head makes the two
	// differ, and the difference is the failure message.
	// Whitespace is folded AWAY, not to single spaces: a line that fills the row
	// is continued on the next screen row, so the terminal's own wrap inserts a
	// boundary that is not in the text (`…resolves relati` / `ve paths…`). What
	// is compared is the character sequence, which is exactly what a duplicated
	// head or tail changes.
	got := strings.Join(strings.Fields(h.bandContents()), "")
	want := strings.Join(strings.Fields("The user asks what is in the file. I should read it. "+head+rest+" Then I answer briefly."), "")
	if got != want {
		t.Fatalf("the answer band does not match the streamed text.\nwant: %q\ngot : %q\n--- band ---\n%s", want, got, h.bandContents())
	}
	// The specific symptom, named so a future regression is unambiguous.
	if strings.Contains(got, "read tooThe path") {
		t.Fatalf("the in-flight line's head was committed twice — a break-carrying delta re-committed text the live row already displayed\n--- band ---\n%s", h.bandContents())
	}
}
