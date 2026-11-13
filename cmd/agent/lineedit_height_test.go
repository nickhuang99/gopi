package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/flaketest"
	"github.com/nick/gopi/internal/lineedit"
)

// lineedit_height_test.go — the editing line at SMALL terminal heights, asserted
// on the rendered screen.
//
// The defect: the reserved compositor could push the `you>` editing line off the
// screen at a small height (computePlan placed the editing line on a fixed row
// and let the band/status arithmetic claim rows the line needed, while the
// renderer drew whatever height the buffer wanted and never consulted the
// layout's published row bound), and the status row — longer since the tool row
// landed — could reach past the terminal's width and WRAP onto a second screen
// row, taking an editing row with it. Both halves are invisible to a test that
// asserts layout numbers: the screen is what the user loses.
//
// Every assertion below therefore reads the RENDERED CELL GRID (Rule 10.2),
// through the same harness the A5 wiring tests use: the bytes the editor wrote,
// fed to the terminal the chat REPL would really be looking at.

const (
	// heightSweepHeights are the terminal heights the acceptance names, from the
	// layout's OWN floor up: the sweep STARTS at lineedit.RowsMinUsable() so it
	// documents that the floor is HOSTABLE rather than merely accepted, and the
	// step keeps the original 4/6/8/10 sweep on a layout whose floor is 6 (the
	// older constant started at 4, which was the floor before the editing line's
	// scrolling window reserved its two chrome rows — at 4 rows the band's floor
	// and the status row leave no row for the input or its marker row, so a
	// 4-row terminal is below the layout's floor by construction and the
	// fallback is announced; see lineedit.EditorRowsMin).
	heightSweepFromFloor = 0
	heightSweepStep      = 2
	heightSweepSpan      = 3

	// heightToolOutputLines is how many rows of tool output a turn streams in the
	// sweep: far more than any of those terminals has rows, so the band is scrolled
	// and re-scrolled (the state the defect appeared in), not merely filled.
	heightToolOutputLines = 30
	// heightToolSummaryRepeat builds a realistic long digest: ToolCallSummary caps
	// one at 160 characters, several times a narrow terminal row.
	heightToolSummaryRepeat = 6
	// heightToolCols is a terminal wide enough for the tool row to be visible —
	// the width at which the tool row's own regression is asserted.
	heightToolCols = 100
	// floorTypedSegment is the typed text the layout-floor arm repeats, and the
	// head the screen must carry once the read loop has echoed it: Rule 1 keeps
	// it named, because the wait signal and the assertion are the SAME token and
	// must not be able to drift apart into two bare literals.
	floorTypedSegment = "abcd"
)

// heightSweepHeights returns the heights the screen tests sweep.
func heightSweepHeights() []int {
	floor := lineedit.RowsMinUsable()
	hs := make([]int, 0, heightSweepSpan)
	for i := 0; i < heightSweepSpan; i++ {
		hs = append(hs, floor+heightSweepFromFloor+i*heightSweepStep)
	}
	return hs
}

// longToolSummary is the tool digest a real call produces at the long end: the
// 160-character shape that used to reach past a narrow row.
func longToolSummary() string {
	s := "internal/lineedit/compositor-and-status-summary/" +
		strings.Repeat("very-long-file-name.go/", heightToolSummaryRepeat)
	if len(s) > chatToolDigestMaxChars {
		s = s[:chatToolDigestMaxChars] + "…"
	}
	return s
}

// chatToolDigestMaxChars is the digest cap the chat layer's summarizers apply
// (internal/agent.ToolCallSummary / firstLineOf): the longest digest the status
// row can be handed. Named here so this test's "long" is the real bound, not a
// number of its own.
const chatToolDigestMaxChars = 160

// heightHarness builds the chat editor path at a given geometry, with the
// reserved layout enabled and a status source carrying a long model name, and
// registers the editor's own teardown.
func heightHarness(t *testing.T, rows, cols int) (*streamHarness, *chatStatusSource) {
	t.Helper()
	return heightHarnessGated(t, rows, cols, nil)
}

// heightHarnessGated builds the same harness with the read loop PARKED on gate
// when gate is non-nil: the loop blocks before its first read, so a test can
// raise a publish and queue keystrokes behind it and then release the loop once
// — the ordering barrier TestChatPublishDoesNotOvertakeQueuedInput needs. A nil
// gate is the ordinary harness (no parking).
func heightHarnessGated(t *testing.T, rows, cols int, gate chan struct{}) (*streamHarness, *chatStatusSource) {
	t.Helper()
	clock := &testClock{t: time.Unix(chatTestEpoch, 0)}
	src := &scriptSource{width: cols, height: rows, clock: clock, holdAtEnd: true, gate: gate}
	if gate != nil {
		// parked is only meaningful for a gated source: it tells a test that the
		// boot frame is written and the loop is waiting, which is the moment an
		// ordering window may open.
		src.parked = make(chan struct{})
	}
	out := &recordingWriter{}
	in := newChatInputWithSource(src, out, chatPrompt)
	in.editor.SetClock(clock.Now)
	if !in.editor.EnableRegion() {
		t.Fatalf("EnableRegion at %d rows = false, want true (the layout's floor is %d)",
			rows, lineedit.RowsMinUsable())
	}
	src.writes = out.count
	status := newChatStatusSource("glm-5.3-flash-long-model-name", false)
	in.SetStatus(status.status)
	h := &streamHarness{in: in, out: out, screen: lineedit.NewScreen(cols, rows), src: src, clock: clock, done: make(chan struct{})}
	// The loop and its join are ONE contract, so they come from one helper
	// (streamHarness.startReadLoop) rather than a hand-rolled pair per harness:
	// that pairing is precisely what the panel/flow arms got wrong. `closeInput`
	// is false because the pre-fix cleanup here closed the editor itself.
	h.startReadLoop(t, true)
	// Wait for the loop to own the renderer before returning: a band write from
	// the test goroutine in the boot window (loopGoid == 0) is queued, not drawn
	// (see streamHarness.awaitReadLoop for the measurement).
	h.awaitReadLoop(t)
	return h, status
}

// TestChatEditingLine_SurvivesALargeToolOutputAtSmallHeights is the defect's
// direct guard, swept over the heights the acceptance names: at each of 4/6/8/10
// rows a long tool output (30 rows) is streamed through the answer band and the
// user's `you>` line must still be ON the rendered screen afterwards, carrying
// the text that was typed.
//
// The tool output is deliberately larger than the band: it is what scrolls the
// band and, before the fix, what the band/status arithmetic took the editing
// line's rows for.
func TestChatEditingLine_SurvivesALargeToolOutputAtSmallHeights(t *testing.T) {
	flaketest.Declare("deterministic: the test synchronized on a DRAIN counter that is not causally ordered with its own assertion — waitDraws(t, Answer().StatusPublishes()+1) returned as soon as the read loop serviced the publish's WAKE, while the scripted keystrokes the same channel had queued were still unread, so the assertion read a half-produced screen. Registered: testdata/flakes.tsv, owner 01a0c039-5a61-70f1-8857-3515610146e8.")
	const typed = "hello"
	for _, rows := range heightSweepHeights() {
		h, status := heightHarness(t, rows, completionCols)
		band := h.in.Answer()
		if band == nil {
			t.Fatalf("rows=%d: no answer band on a terminal that hosts the layout", rows)
		}
		// The status row carries the long tool digest while the answer streams:
		// the combination the acceptance calls out (long model name + long tool
		// summary) must cost the editing line nothing.
		status.setTool("read", longToolSummary())
		h.in.PublishStatus()

		h.typeKeys(strings.Split(typed, "")...)
		renderer := newChatTurnRenderer(h.in)
		for i := 0; i < heightToolOutputLines; i++ {
			renderer.Write(fmt.Sprintf("tool output row %03d %s\n", i, strings.Repeat("x", completionCols-20)))
		}
		renderer.Finish()
		publishChatStatus(h.in, status, 1, statusLiveIdleTurn, 12345)
		// The typed keys and the published counters reach the screen through the
		// editor's read loop, and they are INDEPENDENT events: a status DRAW says
		// nothing about whether the loop has read keystrokes already pushed (the
		// source serves input and the publish wake from one channel, and the draw
		// counter is incremented for the publish the loop happened to service).
		// Waiting on that counter therefore asserted a screen the read loop had
		// not finished producing — measured on pure master: 8 runs in 20, in
		// three shapes ("you> hel", "you> he", status still "turn 0"), each one a
		// different amount of unfinished work at the instant the counter tripped.
		//
		// The wait is on the ASSERTION'S OWN SUBJECT instead: the loop has read
		// every keystroke and drawn the published counters. This is not a weaker
		// assertion — the very same row contents are required immediately below,
		// and a real layout fault still fails, with the timeout dumping the
		// screen it did produce.
		h.waitFor(t, "the typed line and the published counters on the screen", func() bool {
			h.replay()
			return strings.Contains(h.screen.Line(h.layout().EditorTop), typed) &&
				strings.Contains(h.screen.Line(h.layout().StatusRow), "turn1")
		})

		l := h.layout()
		// The editing line's row is INSIDE the terminal.
		if l.EditorTop < 0 || l.EditorTop >= rows {
			t.Fatalf("rows=%d: the editing line is at row %d, outside the %d-row screen\n--- screen ---\n%s",
				rows, l.EditorTop, rows, h.screen.Contents())
		}
		// It carries the prompt and the user's text: the editing line was not
		// sacrificed to the band the tool output scrolled.
		if got := h.screen.Line(l.EditorTop); !strings.Contains(got, chatPrompt) || !strings.Contains(got, typed) {
			t.Fatalf("rows=%d: editing row %d = %q, want the prompt %q and the typed text %q\n--- screen ---\n%s",
				rows, l.EditorTop, got, chatPrompt, typed, h.screen.Contents())
		}
		if l.EditorRows < 1 {
			t.Fatalf("rows=%d: the layout gave the editing line %d rows", rows, l.EditorRows)
		}
		// The band is ABOVE the editing line (the scrollback condition) and kept
		// its minimum: the editing line survived because the BAND yielded, not
		// because the band disappeared.
		if l.BandBottom >= l.EditorTop {
			t.Fatalf("rows=%d: band ends at %d, editing line starts at %d — the band must be above it\n%s",
				rows, l.BandBottom, l.EditorTop, h.screen.Contents())
		}
		// The band's floor, from the layout's own published floor: RowsMinUsable
		// = minimum band + one editing row + the status row.
		minBand := lineedit.RowsMinUsable() - 1 - chatStatusRowsTest
		if got := l.BandBottom - l.BandTop + 1; got < minBand {
			t.Fatalf("rows=%d: the band has %d rows, want at least %d (the editing line took from the band if this shrank)",
				rows, got, minBand)
		}
		// The status row is still the last row, and still carries the counters.
		if l.StatusRow != rows-1 {
			t.Fatalf("rows=%d: status row = %d, want the last row %d", rows, l.StatusRow, rows-1)
		}
		if got := h.screen.Line(l.StatusRow); !strings.Contains(got, "turn1") {
			t.Fatalf("rows=%d: status row = %q, want the counters\n--- screen ---\n%s", rows, got, h.screen.Contents())
		}
		// The evidence the devdoc quotes: the rendered screen at each height.
		t.Logf("height=%d layout=%+v\n%s", rows, l, h.screen.Contents())
	}
}

// screenRows measures how many SCREEN rows a string occupies on a terminal of the
// given width, using the terminal model the assertions already trust (lineedit
// .Screen, the same cell grid that models a real terminal's wrap and its
// scrollback). A string that fits is one row; every wrap past the last row of a
// one-row probe screen pushes a line into that probe's scrollback, so the count
// is scrollback depth + 1. This is the "exactly one screen row" measurement: a
// display-cell count is not a row count, and a byte count is neither.
func screenRows(s string, cols int) int {
	probe := lineedit.NewScreen(cols, 1)
	probe.Feed([]byte(s))
	return 1 + len(probe.Scrollback())
}

// chatStatusRowsTest is how many screen rows the status line occupies (one) — the
// identity the band's floor is derived from above, stated as a local named value
// so the arithmetic reads as the layout's own.
const chatStatusRowsTest = 1

// TestChatStatus_StaysOneScreenRowWithALongToolSummary is the status row's half
// of the defect, measured on the SCREEN: with a long model name and a long tool
// digest the status occupies the last row and NOTHING else — the editing line
// keeps the row above it, and the status row's own text never reaches past the
// terminal's width.
func TestChatStatus_StaysOneScreenRowWithALongToolSummary(t *testing.T) {
	const typed = "typed"
	for _, rows := range heightSweepHeights() {
		h, status := heightHarness(t, rows, completionCols)
		h.typeKeys(strings.Split(typed, "")...)
		status.setTool("read", longToolSummary())
		publishChatStatus(h.in, status, 3, 0, 12345)
		editorTop := h.editorRow(t)
		// THE WAIT COVERS BOTH SUBJECTS THE ASSERTIONS BELOW READ, because they
		// arrive as INDEPENDENT events: the status draw (PublishStatus, through the
		// publish wake) and the typed keys (through the source's input) reach the
		// screen as separate read outcomes, and the draw counter is incremented for
		// whichever the loop happened to service. Waiting on "turn 3" alone therefore
		// reads a screen the loop had not finished producing — measured: grid row 4
		// read "you> type" (four of the five keystrokes) while the status already
		// said "turn 3". The claims below are unchanged and still asserted verbatim
		// immediately after; only the instant is, and a real layout fault still fails
		// with the timeout dumping the screen it did produce.
		h.waitFor(t, "the status counters AND the typed line on the screen", func() bool {
			h.replay()
			return strings.Contains(h.screen.Line(rows-1), "turn3") &&
				strings.Contains(h.screen.Line(editorTop), typed)
		})

		l := h.layout()
		if l.StatusRow != rows-1 {
			t.Fatalf("rows=%d: status row = %d, want the last row %d\n%s", rows, l.StatusRow, rows-1, h.screen.Contents())
		}
		if l.EditorTop == l.StatusRow {
			t.Fatalf("rows=%d: the editing line and the status row are the same row %d\n%s",
				rows, l.EditorTop, h.screen.Contents())
		}
		// The editing line kept its row and its text: a status row that wrapped
		// onto a second row would have overwritten the user's own line.
		if got := h.screen.Line(editorTop); !strings.Contains(got, typed) {
			t.Fatalf("rows=%d: editing row %d = %q, want %q — the status row took it\n--- screen ---\n%s",
				rows, editorTop, got, typed, h.screen.Contents())
		}
		// One row, and one row only: the row ABOVE the status row is the editing
		// line's (already asserted above), and the status row's text fits the
		// terminal's width, so it wraps onto none.
		row := h.screen.Line(l.StatusRow)
		if got := screenRows(row, completionCols); got != 1 {
			t.Fatalf("rows=%d: the status row occupies %d screen rows on a %d-column terminal — it wraps past the row it was drawn on: %q",
				rows, got, completionCols, row)
		}
		if !strings.Contains(row, "turn3") {
			t.Fatalf("rows=%d: status row = %q, want the counters\n%s", rows, row, h.screen.Contents())
		}
		t.Logf("height=%d status row (one screen row): %q", rows, row)
	}
}

// TestChatStatus_ToolRowRegression pins the tool row at the SCREEN seam — the
// display poc 1b3d54b2 introduced — on a terminal wide enough to host it: the
// name of the tool that ran reaches the status row, together with the counters,
// and the row stays exactly one screen row wide.
//
// It is the counterpart of cmd/agent's label-level tool-row tests (which pin the
// composition): this one proves the composed row is what the terminal shows.
func TestChatStatus_ToolRowRegression(t *testing.T) {
	const rows = 10
	h, status := heightHarness(t, rows, heightToolCols)
	status.setTool("keyword_search", "internal/lineedit/region.go")
	publishChatStatus(h.in, status, 2, statusLiveIdleTurn, 4200)
	h.waitFor(t, "the status row to name the tool that ran", func() bool {
		h.replay()
		row := h.screen.Line(rows - 1)
		return strings.Contains(row, toolGlyph) && strings.Contains(row, "keyword_search")
	})
	l := h.layout()
	row := h.screen.Line(l.StatusRow)
	// The tool row AND the counters share the one row (the terminal is wide
	// enough for both), which is the regression: losing the tool row to the
	// status row's own layout change would show here.
	for _, want := range []string{toolGlyph, "keyword_search", "turn2"} {
		if !strings.Contains(row, want) {
			t.Fatalf("status row = %q, want it to contain %q\n--- screen ---\n%s", row, want, h.screen.Contents())
		}
	}
	if len([]rune(row)) > heightToolCols {
		t.Fatalf("status row = %d columns wide on a %d-column terminal", len([]rune(row)), heightToolCols)
	}
	// The tool row is cleared when the turn ends: a stale "⚙ keyword_search" at
	// the prompt would claim a tool is still running.
	status.clearTool()
	publishChatStatus(h.in, status, 2, statusLiveIdleTurn, 4200)
	h.waitFor(t, "the tool row to clear at the prompt", func() bool {
		h.replay()
		return !strings.Contains(h.screen.Line(l.StatusRow), toolGlyph)
	})
}

// TestChatStatus_LongLeadingFieldIsCutWithTheEllipsis is the marker's own case at
// the SCREEN seam: when even the FIRST status field cannot be shown whole — a
// long provider/model name on a narrow terminal — the row shows a marked cut
// ("…") rather than an unmarked one, and it still occupies exactly one screen
// row.
//
// An unmarked cut is the reason for the marker: "glm-5.3-flash-long-mod" reads as
// a real model name, while "glm-5.3-flash-long-mod…" reads as a name that
// continues. The row's width bound is the other half — a cut that still wrapped
// would cost the editing line a row.
func TestChatStatus_LongLeadingFieldIsCutWithTheEllipsis(t *testing.T) {
	// Narrow enough that the model field alone exceeds the room left of the live
	// hint, wide enough for the layout to be installed.
	const rows = 8
	const narrowCols = 30
	h, status := heightHarness(t, rows, narrowCols)
	status.setTool("", "")
	publishChatStatus(h.in, status, 0, 0, 0)
	h.waitFor(t, "the status row to carry the cut model field", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(rows-1), "glm-5.3-flash-long-mod")
	})
	l := h.layout()
	row := h.screen.Line(l.StatusRow)
	if !strings.Contains(row, statusEllipsisTest) {
		t.Fatalf("status row = %q, want the %q marker on the cut model field\n--- screen ---\n%s",
			row, statusEllipsisTest, h.screen.Contents())
	}
	if got := screenRows(row, narrowCols); got != 1 {
		t.Fatalf("status row occupies %d screen rows on a %d-column terminal, want exactly 1: %q",
			got, narrowCols, row)
	}
	t.Logf("narrow terminal (%d cols) status row: %q", narrowCols, row)
}

// statusEllipsisTest is the marker the status row uses for a field it had to cut
// (internal/lineedit's own single definition is unexported; the value is part of
// the user-visible contract, so it is named here as the test's expectation).
const statusEllipsisTest = "…"

// TestChatEditingLine_AWrappedInputAtTheLayoutFloorKeepsTheStatusRow is the
// REACHABLE half of the defect, at the screen seam. At the layout's floor (4
// rows) the editing line is given ONE row; a typed line longer than the terminal
// is wide wants more. Before the fix the renderer drew them all, so the surplus
// rows painted the status row (measured: `status row 3 = "wwwww"`).
//
// What must hold: the editing line's row still shows the prompt and the head of
// the text, the status row still shows the status, and the editor's cursor is on
// a row the layout actually gave it (never on the menu's row or the status row).
func TestChatEditingLine_AWrappedInputAtTheLayoutFloorKeepsTheStatusRow(t *testing.T) {
	h, status := heightHarness(t, lineedit.RowsMinUsable(), completionCols)
	// Four rows wide of input on a 40-column terminal: the buffer wants the full
	// editorRowsCap, the layout gives the editing line one row. The typed text
	// begins with floorTypedSegment, and the wait below reads that head back —
	// the signal that keystrokes have been READ and the row repainted since,
	// which is exactly what a status draw cannot state.
	h.typeKeys(strings.Split(strings.Repeat(floorTypedSegment, completionCols/4+8), "")...)
	publishChatStatus(h.in, status, 1, statusLiveIdleTurn, 0)
	// Waited on the ASSERTION'S OWN SUBJECT. A status draw says nothing about the
	// keystrokes the read loop has read — the source serves input and the publish
	// wake from ONE channel — and `you>` is on the row as soon as the FIRST
	// keystroke is buffered, so the prompt alone tripped while the echo was still
	// characters behind the text below: measured on this arm at pure master, 6
	// runs in 20 red, each dump carrying the status at "turn 1" with the editing
	// row reading "you> a", "you> ab" or "you> abc". The typed head is what the
	// assertions below require of that row, so it is what the wait requires too —
	// the same clause, waited for instead of sampled; nothing is weakened, and a
	// real layout fault still fails, with the timeout dumping the screen it
	// produced (the sibling arm carries the same wait for the same reason).
	h.waitFor(t, "the editing row carrying the prompt and the head of the typed text, and the published counters", func() bool {
		h.replay()
		l := h.layout()
		return strings.Contains(h.screen.Line(l.EditorTop), chatPrompt) &&
			strings.Contains(h.screen.Line(l.EditorTop), floorTypedSegment) &&
			strings.Contains(h.screen.Line(l.StatusRow), "turn1")
	})

	l := h.layout()
	if l.EditorRows != 1 {
		t.Fatalf("the floor terminal gave the editing line %d rows, want 1 — this test is about the wrapped case", l.EditorRows)
	}
	row := h.screen.Line(l.EditorTop)
	if !strings.Contains(row, chatPrompt) || !strings.Contains(row, floorTypedSegment) {
		t.Fatalf("editing row %d = %q, want the prompt and the head of the typed text\n--- screen ---\n%s",
			l.EditorTop, row, h.screen.Contents())
	}
	if !strings.Contains(h.screen.Line(l.StatusRow), "turn1") {
		t.Fatalf("status row %d = %q — the wrapped editing line overwrote it\n--- screen ---\n%s",
			l.StatusRow, h.screen.Line(l.StatusRow), h.screen.Contents())
	}
	cursorRow := h.screen.CurRow
	if cursorRow < l.EditorTop || cursorRow > l.EditorTop+l.EditorRows-1 {
		t.Fatalf("the editor's cursor is on row %d, outside the editing line's rows [%d..%d] — a cursor below it writes on the menu or the status row\n%s",
			cursorRow, l.EditorTop, l.EditorTop+l.EditorRows-1, h.screen.Contents())
	}
	t.Logf("floor terminal with wrapped input:\n%s", h.screen.Contents())
}

// TestChatPublishDoesNotOvertakeQueuedInput is the flake's REGRESSION GUARD
// (Rule 5), stated as the invariant that was violated: a publish the read loop
// drew must not have been drawn ahead of input that was already queued.
//
// The defect it guards was NOT a layout off-by-one. Measured on pure master (8
// runs in 20, in three shapes — the editing row reading "you> hel", "you> he",
// and a status row still reading "turn 0"), the read loop serviced a publish's
// wake while the scripted keystrokes the harness had already pushed were still
// unread: the source serves input and the wake from ONE channel, so a single
// wake can report both.
//
// It is DETERMINISTIC, not a sampled race. The read loop is parked on the
// harness's gate while the publish is RAISED and the keys are queued behind it;
// the source then reports that pending publish ahead of the queued steps (the
// ambiguous single wake), and releasing the gate makes the loop decide — the one
// instant the ordering is observable. On the pre-fix code the loop draws the
// publish with zero of the keys read; after the fix it reads the queued input
// first and the status draw follows, which is exactly why the flake's wait on a
// draw counter could be satisfied by an editor that had not read its input.
func TestChatPublishDoesNotOvertakeQueuedInput(t *testing.T) {
	const typed = "hello"
	for _, rows := range heightSweepHeights() {
		// The loop is PARKED on this gate before its first read, so the publish
		// below is raised and the keys are queued while it waits.
		gate := make(chan struct{})
		h, status := heightHarnessGated(t, rows, completionCols, gate)
		// The loop must be PARKED before the window opens: its boot frame is
		// written before its first read, and a publish raised earlier would be
		// drawn by that frame — legitimately, and before input could be read.
		select {
		case <-h.src.parked:
		case <-time.After(completionTimeout):
			t.Fatalf("rows=%d: the read loop never reached its park — the guard's ordering barrier is not in place", rows)
		}

		// The ordering is read off the WRITE STREAM, so the window starts here:
		// the boot frame is dropped and the recorded stream is restarted from
		// this index.
		h.out.mu.Lock()
		h.out.writes = nil
		h.out.fed = 0
		h.out.mu.Unlock()
		h.screen = lineedit.NewScreen(completionCols, rows)

		// The next read must be the AMBIGUOUS one the flake was about: a publish
		// raised while input is queued, reported as ONE wake.
		h.src.mu.Lock()
		h.src.publishFirst = true
		h.src.raised = make(chan struct{}, 1)
		h.src.mu.Unlock()

		publishChatStatus(h.in, status, 1, statusLiveIdleTurn, 0)
		h.typeKeys(strings.Split(typed, "")...)

		close(gate) // a publish wake and the queued keys are both pending now
		h.waitFor(t, "the published counters to reach the status row", func() bool {
			h.replay()
			return strings.Contains(h.screen.Line(h.layout().StatusRow), "turn1")
		})

		// Where the draw landed in the write stream, and what the editor had
		// written when it first chose to READ input. If the draw overtook the
		// input, the read starts after that chunk.
		chunks := h.out.snapshot()
		statusIdx := -1
		for i, c := range chunks {
			if strings.Contains(string(c), "turn1") {
				statusIdx = i
				break
			}
		}
		if statusIdx < 0 {
			t.Fatalf("rows=%d: the status row shows the counters but no chunk carries them — the guard cannot locate the draw\n--- screen ---\n%s",
				rows, h.screen.Contents())
		}
		if got := h.src.consumed(); got < 1 {
			t.Fatalf("rows=%d: the status was drawn with none of the %d queued keystrokes read\n--- screen ---\n%s",
				rows, len(typed), h.screen.Contents())
		}
		if h.src.firstStepWrites > statusIdx {
			t.Fatalf("rows=%d: the status draw overtook queued input — the %d-th recorded chunk drew the status, and the read loop only read its first keystroke after %d chunk(s) had been written\n--- screen ---\n%s",
				rows, statusIdx, h.src.firstStepWrites, h.screen.Contents())
		}
		// The draw must not be the whole story either: the input really arrives.
		h.waitFor(t, "the typed line on the editing row", func() bool {
			h.replay()
			return strings.Contains(h.screen.Line(h.layout().EditorTop), typed)
		})
		if got := h.src.consumed(); got != len(typed) {
			t.Fatalf("rows=%d: the editing row shows the typed line with only %d of %d keystrokes consumed",
				rows, got, len(typed))
		}
	}
}
