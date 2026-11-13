package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/uiclient"
)

// A5 at the REPL's own seam: the answer renderer and the one-line status as the
// chat input layer hands them over, asserted on the RENDERED SCREEN (Rule 10.2 /
// roadmap §5.7). internal/lineedit's tests pin the renderer's own policy; this
// file pins the WIRING — that the chat layer installs a reserved layout, that
// finalized lines reach the scrollback exactly once, that the live row carries
// only the in-flight answer, and that the status line names the model, the
// iteration and the token count from the REPL's own counters.
//
// Rows are located FROM THE LAYOUT THE EDITOR PUBLISHED (and cross-checked
// against the terminal's own scroll region), never from a copy of the layout
// arithmetic — a test that re-derives the layout would agree with a broken
// layout. The editing line is NOT screen row 0: the reserved band sits above it.

// streamHarness is the chat input layer's editor path plus the screen the
// terminal would show.
type streamHarness struct {
	in     *chatInput
	out    *recordingWriter
	screen *lineedit.Screen
	src    *scriptSource
	clock  *testClock
	// done is closed when the harness's read loop returns, so its teardown can
	// wait for the loop before mutating what the loop reads (see the cleanup).
	done chan struct{}
}

// newStreamHarness builds the editor-backed input over a source that HOLDS at
// the end of its script (so the test can type, then assert the screen), with the
// reserved layout enabled the way production enables it.
func newStreamHarness(t *testing.T) *streamHarness {
	t.Helper()
	return newStreamHarnessDumped(t, nil, lineedit.MonoPalette)
}

// newStreamHarnessDumped is newStreamHarness with the editor's paint stream TEEd
// into a screen-dump sink (`gopi chat --dump-screen`) and a palette installed.
//
// The tee is applied at the SAME seam production applies it (chatDumpWriter over
// the writer the editor paints through), not to the renderer's bytes after the
// fact: a harness that observed the answer renderer directly would exercise a
// seam no run has, so the artifact it asserted on would not be the artifact a run
// delivers. With a nil sink chatDumpWriter returns the recording writer
// UNCHANGED, so the historic harness is byte-for-byte the one it always was.
func newStreamHarnessDumped(t *testing.T, dumper *uiclient.ScreenDumper, pal lineedit.Palette) *streamHarness {
	t.Helper()
	clock := &testClock{t: time.Unix(chatTestEpoch, 0)}
	src := &scriptSource{width: completionCols, height: completionRows, clock: clock, holdAtEnd: true}
	rec := &recordingWriter{}
	out := chatDumpWriter(rec, dumper)
	in := newChatInputWithSource(src, out, chatPrompt)
	in.editor.SetPalette(pal)
	// The sink rides the INPUT as well as the writer: CloseOut is the run's
	// close-out, and it freezes the observation before handing the terminal back
	// — so a harness that tee'd the bytes but left the field nil would exercise a
	// close-out the run does not have.
	in.dumper = dumper
	in.editor.SetClock(clock.Now)
	if !in.editor.EnableRegion() {
		t.Fatalf("EnableRegion(%d rows) = false, want true", completionRows)
	}
	h := &streamHarness{
		in:     in,
		out:    rec,
		screen: lineedit.NewScreen(completionCols, completionRows),
		src:    src,
		clock:  clock,
		done:   make(chan struct{}),
	}
	t.Cleanup(func() {
		// Stop the SOURCE and wait for the read loop to RETURN before tearing the
		// editor down: Close mutates the region (Disable), and the loop reads it
		// from repaint, so closing first races the harness itself (measured under
		// -race: region.Disable vs region.eraseMenuPaint).
		src.stopSource()
		select {
		case <-h.done:
		case <-time.After(completionTimeout):
		}
		in.Close()
		if dumper != nil {
			// Freeze the sink on its last painted frame, exactly as the run's
			// teardown does — a harness that read the artifact before Close would
			// assert on a frame the run would have discarded.
			if err := dumper.Close(); err != nil {
				t.Errorf("close the dump sink: %v", err)
			}
		}
	})
	// The editor reads in the background (the REPL's shape); the test types into
	// the source and asserts the screen.
	go func() {
		defer close(h.done)
		for {
			_, ok, _, _ := in.ReadLine() //stylegate:ignore the harness's own teardown: the screen is the assertion, and a real failure surfaces as a waitFor timeout that dumps it
			if !ok {
				return
			}
		}
	}()
	// Wait for the loop to own the renderer before returning: a band write from
	// the test goroutine in the boot window (loopGoid == 0) is queued, not drawn
	// (see awaitReadLoop for the measurement).
	h.awaitReadLoop(t)
	return h
}

// type types text into the editor, one keystroke per step.
func (h *streamHarness) typeKeys(keys ...string) {
	for _, k := range keys {
		h.src.push(scriptStep{data: []byte(k), advance: completionKeyGap})
	}
}

// replay feeds every recorded write the screen has not seen yet, in order.
//
// The screen is STATEFUL and the recorded stream uses relative cursor motion, so
// feeding the same bytes twice repaints an already-painted screen: the second
// pass lands the answer again instead of over it. Deferred to the recording
// writer's own counter, so replay() is idempotent and the screen a test dumps at
// the end is the exact screen its assertions ran against — which is also what
// answers the branch's objection to incremental feeding: only writes never fed
// before are applied, so a read loop still writing cannot append an already-seen
// write a second time.
func (h *streamHarness) replay() { h.out.ReplayInto(h.screen) }

// stopReadLoop ends the editor's read loop and reports whether it returned, so a
// test can attribute every subsequent write to the CALLER rather than to the
// loop. That attribution is the whole point: "a publish writes zero bytes" is
// only measurable once the one legitimate writer is gone. A false return means
// the loop is still holding the terminal, so the measurement below would be
// meaningless — the caller fails on it.
func (h *streamHarness) stopReadLoop() bool {
	return joinReadLoop(h.src, h.done, h.screen)
}

// joinReadLoop is THE join, stated once so every harness in this family (and
// every arm added later) waits for the same thing: stop the SOURCE, then wait
// for the read loop to actually RETURN. It reports whether the loop retired
// inside completionTimeout; a false means the caller's Close would race a live
// loop and the suite's close-out sweep would report that loop as parked.
//
// Taking the parts (source, retirement channel, screen-for-the-dump) rather than
// a *streamHarness is what lets a harness whose first result IS the artifact —
// completionHarness's line/ok pair — join on the same terms instead of growing a
// second, divergent teardown.
func joinReadLoop(src *scriptSource, done chan struct{}, screen *lineedit.Screen) bool {
	src.stopSource()
	select {
	case <-done:
		return true
	case <-time.After(completionTimeout):
		return false
	}
}

// stopReadLoopOrFail is stopReadLoop with the failure REPORTED instead of
// returned, for a harness's own t.Cleanup.
//
// WHY A CLEANUP MUST JOIN. The pre-fix shape did not merely close a window: with
// `done` nil and no `defer close(h.done)` in the loop, the goroutine could NEVER
// retire — it was parked in the source's read forever (measured; see the guard
// TestChatHarnessReadLoopsRetireOnTeardown, which fails on the pre-fix tree with
// "joining the read loop is not even expressible"). A goroutine that never
// returns is exactly what the suite's close-out sweep is for, and WHERE the
// sweep catches it depends on what the loop was last doing: on its way back to
// the next read it passes through the editing line's exit write
// (Renderer.Finish), which is the reported sighting. That is also why the
// failure is intermittent rather than constant, and why it lands AFTER the test
// body has passed — the assertions are all satisfied while the corpse is still
// warm. Close mutating the region (Disable) underneath a live loop is the
// sibling race the shared harness already measured under -race
// (region.Disable vs region.eraseMenuPaint).
//
// t.Errorf (never Fatalf, never a bare return): t.Cleanup runs after the test
// body, where a Fatalf would be a panic, and a silent miss would restore exactly
// the defect this exists to close.
func (h *streamHarness) stopReadLoopOrFail(t *testing.T) {
	t.Helper()
	joinReadLoopOrFail(t, h.src, h.done, h.screen)
}

// joinReadLoopOrFail is joinReadLoop with the failure REPORTED instead of
// returned, for a harness's own t.Cleanup: the shared reporter both harness
// shapes use, so neither can grow a silent one.
func joinReadLoopOrFail(t *testing.T, src *scriptSource, done chan struct{}, screen *lineedit.Screen) {
	t.Helper()
	if joinReadLoop(src, done, screen) {
		return
	}
	t.Errorf("the editor's read loop did not return within %v of the source stopping, so this harness's Close would race it (and the suite's close-out goroutine sweep would report it as PARKED, failing an otherwise green run)\n--- screen ---\n%s",
		completionTimeout, screen.Contents())
}

// heightGatedRows is the terminal height the gated arm is built at: tall enough
// that the layout is hostable, which is all that arm's ordering barrier needs
// (the gate, not the geometry, is the subject).
const heightGatedRows = 10

// streamHarnessBuilder is a harness FACTORY as the family's guards see it: it
// builds an arm and registers that arm's own teardown (each arm asserts its own
// geometry, so a row/col parameter would have to be invented per arm and the
// guard would then be testing the number it passed rather than the arm).
type streamHarnessBuilder func(*testing.T) *streamHarness

// streamHarnessArms are the harnesses this package's tests drive the editor's
// read loop through, by name, as the guards below enumerate them. A NEW harness
// that starts its own `go in.ReadLine()` is invisible to this list — which is
// exactly how panelHarness and flowHarness drifted — so
// TestChatHarnessReadLoopsAreTheOnlyReaders pins the other half: it reads the
// sources and fails on a read loop that reaches no teardown.
func streamHarnessArms() []struct {
	arm   string
	build streamHarnessBuilder
} {
	height := func(t *testing.T) *streamHarness {
		h, _ := heightHarnessGated(t, heightGatedRows, completionCols, nil)
		return h
	}
	return []struct {
		arm   string
		build streamHarnessBuilder
	}{
		{"panel", panelHarness},
		{"flow", flowHarness},
		{"height", height},
		{"signal", signalNoticeHarness},
	}
}

// startReadLoop starts the harness's background reader — the REPL's own shape —
// and registers BOTH halves of its teardown: the source is stopped and the loop
// JOINED before the editor is closed.
//
// A SHARED HELPER RATHER THAN A PER-HARNESS LOOP, because the loop and the join
// are ONE contract and four harnesses had drifted into writing only the first
// half: panelHarness, flowHarness and heightHarnessGated started the goroutine
// but left the harness's `done` channel NIL and their cleanup at
// `src.stopSource(); in.Close()` — a nil channel never closes, so no join was
// even expressible. The sibling newStreamHarnessDumped had the whole sequence.
// Whether Close CAN be swapped (a test that closes the harness itself must not
// have production Close run twice — chat_dump_test.go drives the dump sink
// through it) is the caller's decision, and it is the `closeInput` argument;
// everything else about this contract is not the caller's to get wrong.
//
// The argument order the cleanup runs in is the one the sibling harness already
// states and measured: stop the SOURCE, wait for the read loop to RETURN, and
// only then close the editor. Close mutates the region (Disable) while the loop
// reads it from repaint (measured under -race: region.Disable vs
// region.eraseMenuPaint), and the loop's own exit write (Renderer.Finish) is
// what the suite's close-out sweep catches when the join is missing.
func (h *streamHarness) startReadLoop(t *testing.T, closeInput bool) {
	t.Helper()
	// The channel is what makes the join expressible, so a harness that forgot it
	// is refused HERE, by name, instead of starting a loop nobody can wait for:
	// this is the check that makes the NEXT arm unable to漏掉 the join (the pre-fix
	// panel/flow arms differ from this one only by this field).
	if h.done == nil {
		t.Fatalf("startReadLoop requires the harness's done channel (`done: make(chan struct{})`) — without it the loop that retires when it closes would close a NIL channel, and the join this helper exists for would be inexpressible (the pre-fix panel/flow defect)")
	}
	in := h.in
	t.Cleanup(func() {
		h.stopReadLoopOrFail(t)
		if closeInput {
			in.Close()
		}
	})
	go func() {
		defer close(h.done)
		for {
			_, ok, _, _ := in.ReadLine() //stylegate:ignore the harness's own teardown: the screen is the assertion, and a real failure surfaces as a waitFor timeout that dumps it
			if !ok {
				return
			}
		}
	}()
}

// waitDraws was retired as a synchronization primitive: it waited for the answer
// renderer's DRAW counter, and that counter is incremented for whatever draw the
// read loop services — including one for an EARLIER publish, or one taken before
// the loop had read keystrokes the test had already pushed. Tests wait on their
// assertion's own subject instead (see the flake test's wait and
// TestChatPublishDoesNotOvertakeQueuedInput).

// awaitReadLoop blocks until the editor's read loop has actually started
// reading — the completion signal a harness needs before it writes into (or
// reads back) a layout the loop owns.
//
// WHY THIS EXISTS, measured: every band row and answer call is ROUTED to the
// read loop, and the decision is a goroutine identity (internal/lineedit
// bandwait.go). Until the loop has entered Run, Editor.loopGoid is 0, and
// WriteBandRow's arm for that state QUEUES the row and returns true WITHOUT
// drawing it. Each of these harnesses starts the loop with `go in.ReadLine()`
// and then writes from the test goroutine — a goroutine started with `go` is
// not scheduled synchronously — so the write can land in that window and the
// test then replays a grid the loop has not painted yet. Probed on the flow
// harness: 2 chunks written at the call, 9 after the loop ran, and the row
// under assertion simply absent from the grid. The row is LATE, never lost
// (the frame stays the LEVEL the loop's next read outcome re-derives), so the
// harness must WAIT for the loop rather than SAMPLE the window.
//
// The signal is the source's own first read: scriptSource.ReadEvent has exactly
// one caller, Editor.Run, so its first invocation IS "the loop is running".
// There is no production knob here and no sleep — it is an edge from the
// goroutine under test.
func (h *streamHarness) awaitReadLoop(t *testing.T) {
	t.Helper()
	select {
	case <-h.src.loopReady():
	case <-time.After(completionTimeout):
		t.Fatalf("the editor's read loop never read from the source within %v, so nothing this case writes would be drawn\n--- screen ---\n%s",
			completionTimeout, h.screen.Contents())
	}
}

// waitFor polls cond, dumping the screen when the deadline passes.
func (h *streamHarness) waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(completionTimeout)
	for time.Now().Before(deadline) {
		if cond() {
			h.replay()
			return
		}
		time.Sleep(completionPollStep) //stylegate:allow-sleep polling the editor's read loop, bounded by completionTimeout
	}
	h.replay()
	t.Fatalf("timed out waiting for %s\n--- screen ---\n%s", what, h.screen.Contents())
}

// layout is the reserved layout the editor installed.
func (h *streamHarness) layout() lineedit.Layout {
	h.replay()
	l, ok := h.in.editor.Layout()
	if !ok {
		panic("the reserved layout is not installed: " + h.screen.Contents())
	}
	return l
}

// editorRow is the screen row the editing line occupies.
func (h *streamHarness) editorRow(t *testing.T) int {
	t.Helper()
	return h.layout().EditorTop
}

// rows joins the screen's rows [top,bottom] (0-based, inclusive).
func (h *streamHarness) rows(top, bottom int) string {
	var lines []string
	for r := top; r <= bottom; r++ {
		if r < 0 || r >= h.screen.Rows {
			continue
		}
		lines = append(lines, h.screen.Line(r))
	}
	return strings.Join(lines, "\n")
}

// TestChatStream_ReservedLayoutCommitsEachLineOnce drives one turn's deltas
// through the SAME renderer the turn loop builds (newChatTurnRenderer over the
// input layer's answer band) and asserts the screen: every finalized line appears
// in the answer band exactly once, the live row carries the in-flight answer and
// nothing else, and the chrome (editing row + status row) never moves.
func TestChatStream_ReservedLayoutCommitsEachLineOnce(t *testing.T) {
	h := newStreamHarness(t)
	band := h.in.Answer()
	if band == nil {
		t.Fatal("the chat input layer handed out no answer band on a terminal that hosts the layout")
	}

	// The REPL's own counters behind the status line (model from the agent,
	// iteration and tokens from the turn loop).
	status := newChatStatusSource("glm-5.3-flash", false)
	h.in.SetStatus(status.status)
	publishChatStatus(h.in, status, 0, 1, 0)

	// The user's editing row, drawn by the editor itself (not by the answer
	// renderer) — the chrome the answer must never move.
	const typed = "typed by the user"
	h.typeKeys(strings.Split(typed, "")...)
	// The editing line is NOT screen row 0: the reserved layout puts the answer
	// band above it (the scrollback condition A5 exists for). The row comes from
	// the layout, never from an assumption.
	editorTop := h.editorRow(t)
	h.waitFor(t, "the typed line on the editing row", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(editorTop), typed)
	})
	editorRow := h.screen.Line(editorTop)

	renderer := newChatTurnRenderer(h.in)
	if !renderer.Reserved() {
		t.Fatal("the turn renderer did not take the scrollback-safe path although the layout is active")
	}

	lines := []string{"first answer line", "second answer line", "third answer line", "fourth answer line"}
	for _, line := range lines {
		// Each line arrives as three deltas plus its break: the shape a token
		// stream actually has, and the shape that used to repaint the answer.
		for _, piece := range []string{line[:6], line[6:12], line[12:], "\n"} {
			renderer.Write(piece)
		}
	}
	renderer.Finish()
	// The counters a finished turn publishes (what the status line reads).
	publishChatStatus(h.in, status, 1, statusLiveIdleTurn, 12345)
	// The publish is ASYNCHRONOUS — the editor's read loop draws it on the wake —
	// so the wait is on the SCREEN showing it. Waiting on the renderer's draw
	// counter instead is the instrument this task retired: the counter is
	// incremented for whatever draw the loop services, so a counter for an
	// EARLIER publish satisfies the wait and the assertion then runs against a
	// status row that has not been drawn yet (measured: cmd/agent's small-height
	// chat test, 8 runs in 20, and this file's own status assertions follow the
	// same road). Replay is idempotent (the writer feeds only writes it has not
	// fed), so polling the screen does not double a line.
	h.waitFor(t, "the published counters on the status row", func() bool {
		h.replay()
		// BOTH halves of the final publish, because the first publish in this test
		// (turn 0, live 1) also RENDERS "turn1" — the displayed turn count is the
		// completed count plus the running one — so a wait that matched the
		// counters alone would be satisfied by that earlier draw. The idle live
		// hint is what distinguishes the finished turn.
		row := h.screen.Line(h.layout().StatusRow)
		return strings.Contains(row, "turn1") && strings.Contains(row, statusLiveIdle)
	})

	// THE ANSWER QUEUE IS THE *OTHER* HALF OF THIS FRAME, and the status row
	// above is NOT a terminator for it. The turn-side calls (Delta/Finish) are
	// ROUTED to the read loop when it owns the renderer (answerqueue.go), so the
	// commits this case counts are applied by THAT goroutine — and a frame can
	// draw the published status while ops queued behind it are still waiting
	// (drainAnswerOps applies them on the loop's NEXT pass). Measured in the
	// budget run (task 01a12ece, stream run-1791786074-500044.json): the status
	// row read the finished turn (so the wait above was satisfied) while the band
	// held only TWO of the four lines — rows 8 and 9 carried "first"/"second"
	// where the passing frame has them at 5 and 6, i.e. the screen was read
	// mid-flight. WaitAnswerDrawn is the terminator production already exposes
	// for a foreign caller that reads the screen back (the sibling case below
	// uses it), and the COMMITTED counter is the independent second half: it is
	// incremented by finalizeLiveRow, i.e. at APPLY time, so "one commit per
	// finalized line" is asserted here rather than assumed.
	if !h.in.editor.WaitAnswerDrawn() {
		t.Fatalf("the queued answer calls were not applied by the read loop, so the band below would be read mid-flight\n--- screen ---\n%s", h.screen.Contents())
	}
	if got := band.Committed(); got != len(lines) {
		t.Fatalf("the band committed %d of %d finalized lines: the read loop still owes this turn's calls, so the screen below is not the turn's frame\n--- screen ---\n%s",
			got, len(lines), h.screen.Contents())
	}
	h.replay()

	// Locate the band and the status row FROM THE SCREEN: the band is the
	// terminal's scroll region, and the status row is the row below it.
	top, bottom := h.screen.ScrollRegion()
	if top != 0 {
		t.Fatalf("scroll region top = %d, want 0 (the screen's first row) — an interior top DISCARDS every committed line instead of saving it\n%s",
			top, h.screen.Contents())
	}
	if bottom >= editorTop {
		t.Fatalf("scroll region = [%d,%d] overlaps the editing line (row %d)\n%s", top, bottom, editorTop, h.screen.Contents())
	}
	if bottom <= top {
		t.Fatalf("scroll region = [%d,%d], want a real band", top, bottom)
	}
	bandText := h.rows(top, bottom)

	// 1. Every finalized line is in the band, exactly once.
	for _, line := range lines {
		if n := strings.Count(bandText, line); n != 1 {
			t.Fatalf("the answer band shows %q %d times, want exactly 1\n--- band ---\n%s", line, n, bandText)
		}
	}
	// 2. The live row (the band's last row) carries the in-flight answer only.
	live := h.screen.Line(bottom)
	for _, line := range lines {
		if strings.Contains(live, line) {
			t.Fatalf("the live row still shows the committed line %q: %q", line, live)
		}
	}
	// 3. The chrome did not move: the editing row kept the user's text and the
	//    status row (immediately below the band) shows the counters.
	if got := h.screen.Line(editorTop); got != editorRow {
		t.Fatalf("the editing row changed under the answer: %q -> %q\n%s", editorRow, got, h.screen.Contents())
	}
	statusRow := h.screen.Line(h.layout().StatusRow)
	// The full status line (model · iteration · tokens · live) needs a row wide
	// enough for it; the narrow-terminal policy — whole fields dropped, never a
	// truncated fragment — has its own test in internal/lineedit.
	for _, want := range []string{"glm-5.3-flash", "turn1", statusLiveIdle} {
		if !strings.Contains(statusRow, want) {
			t.Fatalf("status row = %q, want it to contain %q\n%s", statusRow, want, h.screen.Contents())
		}
	}
	if strings.Contains(statusRow, "12.3k tok") || strings.Contains(statusRow, "k to") {
		t.Fatalf("status row = %q shows a truncated field fragment\n%s", statusRow, h.screen.Contents())
	}
	l := h.layout()
	// The rendered screen is the evidence the devdoc quotes (Rule 5): a raw
	// terminal picture, not a summary of assertions.
	t.Logf("rendered screen after one 4-line answer (band [%d,%d], live row %d, editing row %d, status row %d):\n%s",
		top, bottom, bottom, editorTop, l.StatusRow, h.screen.Contents())
}

// TestChatStream_RepaintsStayLinearInTheAnswer is the bounded-repaint assertion
// at the wiring level: for a FIXED delta stream the number of writes is bounded by
// the number of LINES, not by the number of deltas. A renderer that repainted the
// accumulated answer per delta would be quadratic — that is the repaint storm this
// increment exists to remove.
func TestChatStream_RepaintsStayLinearInTheAnswer(t *testing.T) {
	const lineCount = 40
	const deltasPerLine = 5

	h := newStreamHarness(t)
	band := h.in.Answer()
	renderer := newChatTurnRenderer(h.in)
	before := h.out.count()

	for i := 0; i < lineCount; i++ {
		for d := 0; d < deltasPerLine-1; d++ {
			renderer.Write("part")
		}
		renderer.Write("\n")
	}
	renderer.Finish()
	// THE COMPLETION SIGNAL, not a delay: Finish is ROUTED to the read loop
	// (routed(), internal/lineedit/answerqueue.go), so the commits this case
	// counts are applied by THAT goroutine — reading band.Committed() the
	// instant Finish returns samples the queue, not the work. Measured (both
	// here and on pristine master): committed lines = 39 of 40, one short of
	// the line whose commit was still queued. WaitAnswerDrawn is the terminator
	// production already exposes for a foreign caller that reads the screen (or
	// the counters) back; it returns at once when no loop is running, so it
	// states a fact about this call rather than adding a delay.
	if !h.in.editor.WaitAnswerDrawn() {
		t.Fatalf("the answer renderer's queued calls were not applied by the read loop, so the commit counter below would be read mid-flight\n--- screen ---\n%s", h.screen.Contents())
	}
	writes := h.out.count() - before

	// The bound: one write per delta (the live text, which is never re-sent)
	// plus a small, named allowance per line for the commit's own row write and
	// the region scroll it triggers. The claim is the SHAPE (linear in lines),
	// so the constants below are stated, not tuned.
	bound := lineCount*deltasPerLine + lineCount*chatStreamRepaintAllowance
	if writes > bound {
		t.Fatalf("the stream cost %d writes for %d deltas over %d lines, want <= %d — the answer is being repainted",
			writes, lineCount*deltasPerLine, lineCount, bound)
	}
	if writes < lineCount {
		t.Fatalf("the stream cost %d writes for %d finalized lines — text is being dropped", writes, lineCount)
	}
	// The commit counter is the independent half of the same claim: one commit
	// per finalized line, so no line was committed twice (which is what a
	// duplicated paint would mean).
	if got := band.Committed(); got != lineCount {
		t.Fatalf("committed lines = %d for %d finalized lines, want exactly %d (one commit each)", got, lineCount, lineCount)
	}
}

// chatStreamRepaintAllowance is the per-line non-delta write allowance: the
// commit's own row write and the region scroll it triggers. Named, not inline
// (Rule 1).
const chatStreamRepaintAllowance = 4

// TestChatStream_CookedPathKeepsTheBufferedRenderer is the degradation half at
// the wiring level: with no reserved layout (piped stdin, --no-editor, or a
// terminal too small) the turn loop takes the historic buffered renderer — the
// answer in flow, byte-for-byte as before A5. The assertion is on the BYTES
// WRITTEN, because that path's contract IS its byte stream.
func TestChatStream_CookedPathKeepsTheBufferedRenderer(t *testing.T) {
	var out bytes.Buffer
	renderer := newChatTurnRenderer(&chatInput{flowOut: &out})
	if renderer.Reserved() {
		t.Fatal("the turn renderer claimed the reserved path with no answer band")
	}
	renderer.Write("full line one\n")
	renderer.Write("partial word")
	renderer.Finish()
	if got := out.String(); got != "full line one\npartial word" {
		t.Fatalf("buffered renderer wrote %q, want the historic flow output", got)
	}
	// A writer is where the cooked path puts a turn's answer, and the status
	// source is not consulted at all: the cooked REPL has no status row.
	status := newChatStatusSource("m", false)
	status.update(2, statusLiveIdleTurn, 0, "")
	if got := status.status().Text(); !strings.Contains(got, "m") {
		t.Fatalf("status text = %q, want the model name", got)
	}
}

// TestRecordingWriterReplayIsIdempotent is the regression guard for the harness
// defect that made this file's quoted evidence wrong (Rule 5: every fix ships
// a minimal repro).
//
// recordingWriter.ReplayInto feeds a STATEFUL cell grid a stream of RELATIVE
// cursor motion. Feeding the same bytes twice does not reproduce the screen — it
// repaints it, so the answer lands a second time below itself. The harness used
// to do exactly that (replay() re-fed the whole history on every poll, and once
// more when a test dumped the screen), which meant the A5 wiring test asserted
// against a correct screen and then quoted a corrupted one as its evidence.
func TestRecordingWriterReplayIsIdempotent(t *testing.T) {
	out := &recordingWriter{}
	// A stream with no absolute cursor home: whatever the screen shows after it
	// depends on what the screen already showed.
	out.Write([]byte("first answer line\nsecond answer line\n"))

	screen := lineedit.NewScreen(completionCols, completionRows)
	if n := out.ReplayInto(screen); n != 1 {
		t.Fatalf("first ReplayInto fed %d writes, want 1", n)
	}
	once := screen.Contents()
	if got := strings.Count(once, "first answer line"); got != 1 {
		t.Fatalf("after one replay the screen shows %q %d times, want 1\n%s", "first answer line", got, once)
	}
	for i := 0; i < 3; i++ {
		if n := out.ReplayInto(screen); n != 0 {
			t.Fatalf("replay %d fed %d already-seen writes, want 0 (the counter is the guard)", i+2, n)
		}
	}
	if got := screen.Contents(); got != once {
		t.Fatalf("the screen moved under repeated replay — assertions and quoted evidence would disagree:\n--- once ---\n%s\n--- now ---\n%s", once, got)
	}
}

// TestChatStatus_ToolActivityRidesTheStatusRow pins the tool-activity display
// added for the text-UI POC: a tool CALL prints "⚙ <name> <summary>" on the
// status line, its RESULT keeps the row (so the completed activity stays
// readable), and the row is CLEARED when the turn ends — a stale "⚙ bash" at
// the prompt would claim a tool is still running.
//
// This is the in-process counterpart of the web UI's `event: tool` rows: the
// REPL now shows what the agent is doing with its tools, instead of silently
// producing an answer that merely looks like it appeared from nowhere.
func TestChatStatus_ToolActivityRidesTheStatusRow(t *testing.T) {
	s := newChatStatusSource("deepseek-chat", false)

	// At the prompt, before any tool runs: no tool row.
	if got := s.status().Label; strings.Contains(got, toolGlyph) {
		t.Fatalf("status before any tool = %q, want no %q row", got, toolGlyph)
	}

	// A call in flight names the tool and its argument digest.
	s.setTool("bash", "head -3 go.mod")
	got := s.status().Label
	if !strings.Contains(got, toolGlyph+" bash head -3 go.mod") {
		t.Fatalf("status during call = %q, want it to contain %q", got, toolGlyph+" bash head -3 go.mod")
	}

	// The result keeps the row (the activity is still the last thing that ran).
	s.setToolDone("bash")
	if got := s.status().Label; !strings.Contains(got, toolGlyph+" bash head -3 go.mod") {
		t.Fatalf("status after result = %q, want the tool row to persist", got)
	}

	// A SECOND tool replaces the row rather than appending. The digest is
	// capped (see TestChatStatus_ToolRowIsWidthBounded), so assert on the
	// leading, always-present part rather than the whole path.
	s.setTool("read", "internal/agent/chat_ops.go")
	got = s.status().Label
	if !strings.Contains(got, toolGlyph+" read internal/agent/chat_ops") {
		t.Fatalf("status during second call = %q, want the new tool", got)
	}
	if strings.Contains(got, "bash") {
		t.Fatalf("status during second call = %q, want the previous tool row gone", got)
	}

	// The turn ended: the row goes away.
	s.clearTool()
	if got := s.status().Label; strings.Contains(got, toolGlyph) {
		t.Fatalf("status after clearTool = %q, want no %q row", got, toolGlyph)
	}
}

// TestChatStatus_ToolRowSurvivesASummarylessCall guards the degenerate call: a
// tool whose digest is empty must still name the tool (a bare "⚙ bash" beats
// dropping the row and leaving the user with no signal at all).
func TestChatStatus_ToolRowSurvivesASummarylessCall(t *testing.T) {
	s := newChatStatusSource("deepseek-chat", false)
	s.setTool("bash", "")
	got := s.status().Label
	if !strings.Contains(got, toolGlyph+" bash") {
		t.Fatalf("status for a summary-less call = %q, want it to name the tool", got)
	}
	if strings.Contains(got, "bash ") {
		t.Fatalf("status for a summary-less call = %q, want no trailing separator", got)
	}
}

// TestChatStatus_ToolRowIsRaceFree exercises setTool/setToolDone/clearTool and
// status() concurrently under -race: the REPL's turn loop fires the tool
// callback while the editor's read loop composes the status, and the snapshot
// must never be torn (the same discipline the counters already follow).
func TestChatStatus_ToolRowIsRaceFree(t *testing.T) {
	s := newChatStatusSource("deepseek-chat", false)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			s.setTool("bash", "echo hi")
			_ = s.status()
			s.setToolDone("bash")
			_ = s.status()
			s.clearTool()
		}
	}()
	for i := 0; i < 200; i++ {
		_ = s.status().Label
	}
	<-done
}

// TestChatStatus_ToolRowIsWidthBounded pins the fix for the "I lost input focus"
// report: an unbounded tool digest wrapped the status line to a SECOND terminal
// row, which cost the editing line a row — on a short terminal the input line
// was pushed off-screen entirely, and typing into an invisible line reads as
// "the UI lost my focus".
//
// The row must therefore be bounded by statusToolRowMax, ellipsized (never
// wrapped), and cut on a RUNE boundary so a multi-byte summary cannot shear into
// an invalid sequence.
func TestChatStatus_ToolRowIsWidthBounded(t *testing.T) {
	s := newChatStatusSource("deepseek-flash", true)

	// A deliberately hostile digest: far longer than any terminal row.
	long := "head -3 go.mod && grep -rn attachment --include=*.go cmd/agent/ internal/tools/"
	s.setTool("bash", long)
	got := s.status().Label
	row := got[strings.Index(got, toolGlyph):]
	if n := utf8.RuneCountInString(row); n > statusToolRowMax {
		t.Fatalf("tool row is %d runes, want <= %d (it would wrap and steal the editing line):\n%q",
			n, statusToolRowMax, row)
	}
	if !strings.Contains(row, "…") {
		t.Fatalf("tool row = %q, want an ellipsis marking the truncation", row)
	}

	// A multi-byte summary must not be cut mid-rune (invalid UTF-8 in a
	// terminal write corrupts the row and can shear the layout).
	s.setTool("read", strings.Repeat("目录/文件", 12))
	got = s.status().Label
	row = got[strings.Index(got, toolGlyph):]
	if !utf8.ValidString(row) {
		t.Fatalf("tool row = %q, want valid UTF-8", row)
	}
	if n := utf8.RuneCountInString(row); n > statusToolRowMax {
		t.Fatalf("tool row is %d runes, want <= %d", n, statusToolRowMax)
	}

	// A short digest is NOT padded or elided — the cap must not mangle the
	// common case.
	s.setTool("bash", "head -3 go.mod")
	got = s.status().Label
	if !strings.Contains(got, toolGlyph+" bash head -3 go.mod") {
		t.Fatalf("short tool row = %q, want it intact", got)
	}
}
