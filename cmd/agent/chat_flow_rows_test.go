package main

// chat_flow_rows_test.go — the AGENT's own flow output must not tear the
// reserved layout.
//
// The defect (task 01a0c0a7, measured with the SGR-aware grid instrument and the
// attached PTY captures): the REPL's own text already streamed through the
// answer band, but the AGENT's output did not — `internal/agent` writes its live
// reasoning stream and its `[gopi]` notices to destinations of its own
// (os.Stderr), with no idea the layout exists. On the same turn that produced a
// correct answer, the reasoning block tore the band's rows and a wrapped
// `[gopi] ℹ️ thinking budget … clamped …` notice overwrote the STATUS row
// MID-LINE:
//
//	13|budget_tokens, so the harness aborts a round whose streamed thinking…
//	15|                                                    [think
//	16|ing] The user said "hello world". That's a trivial greeting…
//	29|deepseek-flash · turn 1 · tools  thinking…[gopi] ℹ️ thinking budget 16384 …
//
// The unit of truth is the RENDERED CELL GRID (Rule 10.2) — the three sources
// (reasoning, answer, notice) each occupy THEIR OWN whole rows inside the band,
// and the editing row and the status row still carry only their own text. A test
// that asserted "the callback fired" would see none of this: `internal/agent`'s
// own callback tests are green while the screen is torn.
//
// RED PROOF: revert the seam (drop the `ag.OnThinking = seam.Flow`
// installation and the logger re-point in installChatFlowSeam) and the three
// sources flow into the layout's rows — the chrome assertions here fire naming
// the row that lost its own text, and `internal/agent`'s callback tests stay
// green, which is why the assertion is on the grid.

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/lineedit"
)

const (
	// flowBandCols is wide enough for one whole flow line to arrive on one band
	// row, so a row a source spans is that source's own row and not a wrap.
	flowBandCols = 72
	// flowBandRows hosts the reserved layout with room left for real flow text.
	flowBandRows = 20
	// flowBandModel is the model the status source reports: the status row must
	// still carry it after the flow output arrived.
	flowBandModel = "deepseek-flash"

	// The three sources, as the agent and the REPL really spell them. The
	// thinking text rides between the agent's own markers (the agent hands the
	// seam the markers and the deltas as separate writes).
	flowThinkingMarker = "[thinking] "
	flowThinkingText   = "the agent's own reasoning, on one row"
	flowAnswerText     = "the answer the model streamed"
	// flowNoticeText is the `[gopi]` notice shape the epic measured tearing the
	// status row — short enough here to be a WHOLE row, so the assertion is
	// about ownership rather than about wrapping (the long shape has its own
	// test below).
	flowNoticeText = "[gopi] thinking budget 16384 clamped to 8192"
)

// flowHarness builds the chat input layer over a terminal that hosts the
// reserved layout, exactly as panelHarness does for the REPL's own text — same
// source, clock and background reader — so a flow assertion cannot pass by
// taking a different editor path.
func flowHarness(t *testing.T) *streamHarness {
	t.Helper()
	clock := &testClock{t: time.Unix(chatTestEpoch, 0)}
	src := &scriptSource{width: flowBandCols, height: flowBandRows, clock: clock, holdAtEnd: true}
	out := &recordingWriter{}
	in := newChatInputWithSource(src, out, chatPrompt)
	in.editor.SetClock(clock.Now)
	if !in.editor.EnableRegion() {
		t.Fatalf("EnableRegion(%d rows) = false, want true (the layout's floor is %d)",
			flowBandRows, lineedit.RowsMinUsable())
	}
	status := newChatStatusSource(flowBandModel, false)
	in.SetStatus(status.status)
	h := &streamHarness{in: in, out: out, screen: lineedit.NewScreen(flowBandCols, flowBandRows), src: src, clock: clock, done: make(chan struct{})}
	// The loop and its join are one contract (see streamHarness.startReadLoop):
	// this arm had the same nil `done` and the same missing join as panelHarness.
	h.startReadLoop(t, true)
	// Wait for the loop to own the renderer before returning: every write this
	// harness makes from the test goroutine is ROUTED to the loop, and in the
	// boot window (loopGoid == 0) it would be queued, not drawn (see
	// streamHarness.awaitReadLoop for the measurement).
	h.awaitReadLoop(t)
	return h
}

// bandRowOf reports the band row (index into the band's rows) that contains s as
// ONE row's text, or -1. It is the whole-row test: a source that has been
// interleaved with another one is contained in NO single row.
func bandRowOf(rows []string, s string) int {
	for i, r := range rows {
		if strings.Contains(r, s) {
			return i
		}
	}
	return -1
}

// flowSeamAgent installs the PRODUCTION seam (installChatFlowSeam) on a fresh
// agent over this harness, so the assertions below run through the wiring the
// chat REPL really uses — a test that called the band writer directly would stay
// green with the seam reverted, which is the one thing a regression test must
// not do.
func flowSeamAgent(t *testing.T, h *streamHarness) (*agent.Agent, *chatFlowSeam) {
	t.Helper()
	ag := &agent.Agent{}
	ag.Logger = log.New(os.Stderr, "[gopi] ", 0)
	seam, restore := installChatFlowSeam(ag, h.in)
	t.Cleanup(restore)
	return ag, seam
}

// writeFlow hands one flow chunk to the agent's own seam, the way
// `internal/agent` does — through the callback, never through the layout. It
// then FLUSHES, which is what the REPL does at a turn boundary: the seam holds
// the unfinished tail so a fragment-per-row stream reads as a sentence, so a
// test that never flushed would assert on a row that has not been committed yet.
func writeFlow(t *testing.T, ag *agent.Agent, seam *chatFlowSeam, chunk string) {
	t.Helper()
	if ag.OnThinking == nil {
		t.Fatalf("no flow seam is installed on the agent: %q would go straight to os.Stderr and tear the layout", chunk)
	}
	ag.OnThinking(chunk)
	seam.Flush()
}

// TestChatFlow_AgentOutputTakesWholeBandRows is the defect's direct guard: the
// agent's live reasoning block, the streamed answer and a `[gopi]` notice each
// reach the answer band as their OWN whole row, and the chrome (the editing row,
// the status row) keeps only its own text.
func TestChatFlow_AgentOutputTakesWholeBandRows(t *testing.T) {
	h := flowHarness(t)
	renderer := newChatTurnRenderer(h.in)
	if !renderer.Reserved() {
		t.Fatal("the turn renderer did not take the scrollback-safe path although the layout is active")
	}
	// The production wiring: the REPL's per-turn renderer takes the answer, and
	// the seam (the func installChatFlowSeam installs as ag.OnThinking) takes the
	// agent's flow output. One write per chunk is the shape `internal/agent`
	// really emits: a marker, a delta, a marker, a notice — never a composed
	// multi-line panel.
	ag, seam := flowSeamAgent(t, h)
	for _, c := range []string{flowThinkingMarker, flowThinkingText, "\n"} {
		writeFlow(t, ag, seam, c)
	}
	renderer.Write(flowAnswerText + "\n")
	writeFlow(t, ag, seam, flowNoticeText)
	renderer.Finish()

	top, bottom, band := panelBand(t, h)
	rows := strings.Split(band, "\n")
	sources := []struct {
		what string
		text string
	}{
		{"the reasoning text", flowThinkingText},
		{"the streamed answer", flowAnswerText},
		{"the [gopi] notice", flowNoticeText},
	}
	for _, s := range sources {
		if bandRowOf(rows, s.text) < 0 {
			t.Fatalf("%s is not intact on any single band row [%d,%d] — the sources are interleaved:\n--- band ---\n%s\n--- screen ---\n%s",
				s.what, top, bottom, band, h.screen.Contents())
		}
	}
	// No band row carries two sources: that is the tear, stated as a row-level
	// property rather than as a position.
	for i, r := range rows {
		carries := 0
		for _, s := range sources {
			if strings.Contains(r, s.text) {
				carries++
			}
		}
		if carries > 1 {
			t.Fatalf("band row %d carries %d sources at once: %q\n--- band ---\n%s",
				top+i, carries, r, band)
		}
	}
	// The chrome keeps its own text. This is the half the measured defect broke:
	// the notice overwrote the status row mid-line and the reasoning block landed
	// on the editing row.
	l := h.layout()
	for _, c := range []struct {
		what string
		row  int
	}{{"the editing row", l.EditorTop}, {"the status row", l.StatusRow}} {
		got := h.screen.Line(c.row)
		for _, s := range sources {
			if strings.Contains(got, s.text) {
				t.Fatalf("%s (row %d) carries %s: %q — the agent's flow output is painting on the layout's chrome\n--- screen ---\n%s",
					c.what, c.row, s.what, got, h.screen.Contents())
			}
		}
	}
	// The prompt is compared TRIMMED: a renderer trims trailing cells, so a
	// prompt with no text after it is drawn as `you>` and never as `you> `.
	if got := h.screen.Line(l.EditorTop); !strings.Contains(got, strings.TrimRight(chatPrompt, " ")) {
		t.Fatalf("the editing row %d no longer shows the prompt %q: %q\n%s", l.EditorTop, chatPrompt, got, h.screen.Contents())
	}
	if got := h.screen.Line(l.StatusRow); !strings.Contains(got, flowBandModel) {
		t.Fatalf("the status row lost its own text after the flow output: %q\n%s", got, h.screen.Contents())
	}
	t.Logf("rendered screen after a reasoning block, an answer and a [gopi] notice (band [%d,%d], editing row %d, status row %d):\n%s",
		top, bottom, l.EditorTop, l.StatusRow, h.screen.Contents())
}

// TestChatFlow_LongNoticeScrollsTheBandNotTheChrome drives one flow chunk PAST
// the band's width: the band is what sits above the chrome, so an over-wide
// source wraps inside it — and the chrome must keep its own rows, with the
// chunk's own tail still on screen (a truncated tail would be a Rule 15 silent
// loss of the text the agent sent).
func TestChatFlow_LongNoticeScrollsTheBandNotTheChrome(t *testing.T) {
	h := flowHarness(t)
	// The status row must be DRAWN before it can be asserted to survive — the
	// same publish the REPL issues at a turn boundary (the editor's read loop
	// draws it; the publisher writes nothing).
	h.in.PublishStatus()
	// The status row must be DRAWN before it can be asserted to survive, and the
	// wait is on that row itself — never on the answer renderer's DRAW counter
	// (waitDraws, retired: see chat_stream_test.go), which is incremented for
	// whatever draw the read loop services and so can be satisfied by an earlier
	// publish, leaving this assertion to run against an undrawn status row.
	h.waitFor(t, "the published model on the status row", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(h.layout().StatusRow), flowBandModel)
	})
	editorTop := h.editorRow(t)

	long := "[gopi] " + strings.Repeat("y", flowBandCols+40) + " TAIL"
	tail := " TAIL"
	ag, seam := flowSeamAgent(t, h)
	writeFlow(t, ag, seam, long)

	top, bottom, band := panelBand(t, h)
	if bandRowOf(strings.Split(band, "\n"), tail) < 0 {
		t.Fatalf("the chunk's tail %q is not on any band row [%d,%d] — the flow text was truncated instead of wrapped:\n--- band ---\n%s",
			tail, top, bottom, band)
	}
	l := h.layout()
	if got := h.screen.Line(l.StatusRow); !strings.Contains(got, flowBandModel) {
		t.Fatalf("the status row was overwritten by the wrapping flow chunk: %q\n--- screen ---\n%s", got, h.screen.Contents())
	}
	// The prompt is compared TRIMMED: a renderer trims trailing cells, so a
	// prompt with no text after it is drawn as `you>` and never as `you> `.
	if got := h.screen.Line(editorTop); !strings.Contains(got, strings.TrimRight(chatPrompt, " ")) {
		t.Fatalf("the editing row was overwritten by the wrapping flow chunk: %q\n--- screen ---\n%s", got, h.screen.Contents())
	}
	// Every band row that carries the chunk's body carries ONE source: the two
	// halves of the wrap are the same source, so the rule is that no row mixes
	// the chunk with something else.
	for r := top; r <= bottom; r++ {
		got := h.screen.Line(r)
		if strings.Contains(got, "y") && strings.Contains(got, flowBandModel) {
			t.Fatalf("band row %d mixes the flow chunk with the status text: %q\n--- screen ---\n%s", r, got, h.screen.Contents())
		}
	}
}

// TestChatFlow_EvenOnTheLayoutsOwnStreamTheChromeSurvives reproduces the tear's
// MECHANISM rather than its symptom. In production the seam's fallback IS the
// layout's stream: with the reserved layout installed, os.Stderr is the
// descriptor the editor paints on, so "write the flow bytes to the fallback"
// means "write them wherever the cursor happens to be" — and the cursor is parked
// on the row the editor last drew. So the harness gives the seam the SAME stream
// the editor writes to (h.out) and asserts the chrome keeps its own rows: with
// the routing in place the fallback receives nothing and every flow row is drawn
// by the editor; with the routing reverted the raw bytes land in that stream and
// the chrome is overwritten, which is the failure this test reports.
func TestChatFlow_EvenOnTheLayoutsOwnStreamTheChromeSurvives(t *testing.T) {
	h := flowHarness(t)
	h.in.PublishStatus()
	// The status row must be DRAWN before it can be asserted to survive, and the
	// wait is on that row itself — never on the answer renderer's DRAW counter
	// (waitDraws, retired: see chat_stream_test.go), which is incremented for
	// whatever draw the read loop services and so can be satisfied by an earlier
	// publish, leaving this assertion to run against an undrawn status row.
	h.waitFor(t, "the published model on the status row", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(h.layout().StatusRow), flowBandModel)
	})
	l := h.layout()

	// The production seam shape: fallback == the layout's own stream.
	seam := newChatFlowSeam(h.in, h.out)
	seam.Flow(flowThinkingMarker + flowThinkingText + "\n")
	seam.Write([]byte(flowNoticeText + "\n")) // the logger's line
	seam.Flush()

	rows := []struct {
		what string
		row  int
		text string
	}{
		{"the editing row", l.EditorTop, strings.TrimRight(chatPrompt, " ")},
		{"the status row", l.StatusRow, flowBandModel},
	}
	for _, r := range rows {
		if got := h.screen.Line(r.row); !strings.Contains(got, r.text) {
			t.Fatalf("%s (row %d) lost its own text %q — the flow bytes went to the layout's own stream instead of through its single writer: %q\n--- screen ---\n%s",
				r.what, r.row, r.text, got, h.screen.Contents())
		}
	}
	// And the flow text really did arrive: a chrome that survived because nothing
	// was written would prove nothing.
	_, _, band := panelBand(t, h)
	for _, want := range []string{flowThinkingText, flowNoticeText} {
		if bandRowOf(strings.Split(band, "\n"), want) < 0 {
			t.Fatalf("%q is not on any band row — the flow text was dropped rather than routed:\n--- band ---\n%s", want, band)
		}
	}
}

// TestChatFlow_SeamIsNotInstalledOnTheCookedPath pins the other half of the
// contract at the WIRING level: with no editor path (a pipe, --no-editor) the
// seam is not installed at all, so the agent keeps the destinations it always
// had — its reasoning on os.Stderr and its logger's writer untouched — and
// cooked bytes are unchanged BY CONSTRUCTION rather than by a second decision.
func TestChatFlow_SeamIsNotInstalledOnTheCookedPath(t *testing.T) {
	var logged bytes.Buffer
	ag := &agent.Agent{}
	ag.Logger = log.New(&logged, "[gopi] ", 0)
	in := &chatInput{errOut: os.Stderr, flowOut: os.Stdout} // cooked: no editor

	_, restore := installChatFlowSeam(ag, in)
	if ag.OnThinking != nil {
		t.Fatal("the seam was installed on a chat run with no editor path — the flow bytes would no longer be the historic ones")
	}
	ag.Logger.Print("budget clamped")
	if got, want := logged.String(), "[gopi] budget clamped\n"; got != want {
		t.Fatalf("logger wrote %q, want %q (the historic destination)", got, want)
	}
	restore()
}

// TestChatFlow_SeamFallsBackWhenNoLayoutIsOnScreen covers the other fallback: the
// seam IS installed (the editor path is live) but the layout is not on screen —
// a terminal below the layout's floor, or a layout taken down by a resize. The
// bytes then go to the fallback and the layout writes NOTHING (a write there
// would be the second writer on the descriptor the status-publish tests pin as
// the defect).
func TestChatFlow_SeamFallsBackWhenNoLayoutIsOnScreen(t *testing.T) {
	clock := &testClock{t: time.Unix(chatTestEpoch, 0)}
	src := &scriptSource{width: flowBandCols, height: flowBandRows, clock: clock, holdAtEnd: true}
	out := &recordingWriter{}
	in := newChatInputWithSource(src, out, chatPrompt) // region deliberately NOT enabled
	t.Cleanup(func() {
		src.stopSource()
		in.Close()
	})
	var fallback bytes.Buffer
	seam := newChatFlowSeam(in, &fallback)

	const notice = "[gopi] notice\n"
	if _, err := seam.Write([]byte(notice)); err != nil {
		t.Fatalf("seam write: %v", err)
	}
	if got := fallback.String(); got != notice {
		t.Fatalf("the seam wrote %q to the fallback, want %q (the historic bytes)", got, notice)
	}
	if got := out.count(); got != 0 {
		t.Fatalf("the layout wrote %d chunk(s) although it is not installed — a second writer on the terminal fd", got)
	}
}

// TestChatFlow_LogWriterKeepsTheHistoricBytesWithoutALayout pins the logger's own
// fallback: the `[gopi]` notices keep their exact bytes and their destination
// when the layout is not on screen, so the testsuite's greps and a piped run are
// unaffected.
func TestChatFlow_LogWriterKeepsTheHistoricBytesWithoutALayout(t *testing.T) {
	var fallback bytes.Buffer
	seam := newChatFlowSeam(&chatInput{errOut: os.Stderr}, &fallback)
	const line = "[gopi] ℹ️ thinking budget 16384 clamped to 8192\n"
	if _, err := seam.Write([]byte(line)); err != nil {
		t.Fatalf("logger writer: %v", err)
	}
	if got := fallback.String(); got != line {
		t.Fatalf("logger wrote %q, want %q (byte-identical to the historic stderr write)", got, line)
	}
}
