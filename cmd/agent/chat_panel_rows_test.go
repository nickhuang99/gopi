package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/lineedit"
)

// chat_panel_rows_test.go — the REPL's OWN text (the /help panel, a slash
// command's table) must not be painted over the reserved layout.
//
// The defect (found by the SGR-aware capture instrument, devdoc/evidence/sgr-grid):
// with the reserved layout installed the terminal is inside a scrolling region
// and the cursor sits on the row the editor last drew, so `fmt.Println(out)`
// landed the panel's first row on the EDITING row and flowed DOWNWARD from
// there, scrolling the band up by one per line — on top of the panel's own
// previous lines. Measured on a 100×30 terminal, the `/help` panel read:
//
//	12|you> Usage: /quit
//	13|                 End the interactive session and exit (Ctrl+D …)
//	14|                                                    The session's CHAT…
//
// Three rows of one panel interleaved with the band's rows: the "torn row" the
// epic reports. The unit of truth here is the RENDERED CELL GRID (Rule 10.2),
// not the layout arithmetic — a test that re-derived the layout would agree with
// a broken layout.

const (
	// panelCols is wide enough for a help line to arrive unwrapped, so a row the
	// panel spans is the panel's own line and not a wrap artifact.
	panelCols = 76
	// panelRows hosts the reserved layout (the band's floor + the editing line +
	// the status row) with room left for a real panel.
	panelRows = 20
	// panelStatusModel is the model name the status source reports; the panel
	// tests assert the row still carries it (the panel may not take the row).
	panelStatusModel = "deepseek-flash"
)

// panelHarness builds the chat input layer over a terminal that hosts the
// reserved layout at a geometry a help line fits in. It mirrors newStreamHarness
// exactly — same source, clock and background reader — so a panel assertion
// cannot pass by taking a different editor path.
func panelHarness(t *testing.T) *streamHarness {
	t.Helper()
	clock := &testClock{t: time.Unix(chatTestEpoch, 0)}
	src := &scriptSource{width: panelCols, height: panelRows, clock: clock, holdAtEnd: true}
	out := &recordingWriter{}
	in := newChatInputWithSource(src, out, chatPrompt)
	in.editor.SetClock(clock.Now)
	if !in.editor.EnableRegion() {
		t.Fatalf("EnableRegion(%d rows) = false, want true (the layout's floor is %d)",
			panelRows, lineedit.RowsMinUsable())
	}
	status := newChatStatusSource(panelStatusModel, false)
	in.SetStatus(status.status)
	h := &streamHarness{in: in, out: out, screen: lineedit.NewScreen(panelCols, panelRows), src: src, clock: clock, done: make(chan struct{})}
	// The read loop and its JOIN are one contract, so they come from one helper:
	// this arm used to start the goroutine and leave `done` nil, with a cleanup
	// of `stopSource(); Close()` that could not join even in principle (a nil
	// channel never closes). The loop therefore NEVER retired — it was parked in
	// the source's read past the end of its test — and the suite's close-out
	// goroutine sweep caught it and failed the RUN after every assertion in the
	// test body had already passed. See streamHarness.startReadLoop.
	h.startReadLoop(t, true)
	return h
}

// panelBand reads the answer band off the SCREEN (the terminal's own scroll
// region), cross-checked against the layout the editor published: a band row is
// a row the panel's text may legitimately occupy.
func panelBand(t *testing.T, h *streamHarness) (int, int, string) {
	t.Helper()
	h.replay()
	l := h.layout()
	top, bottom := h.screen.ScrollRegion()
	if top != 0 {
		t.Fatalf("scroll region top = %d, want 0 (the screen's first row — an interior top discards every committed line)\n%s",
			top, h.screen.Contents())
	}
	if bottom >= l.EditorTop {
		t.Fatalf("scroll region [%d,%d] reaches the editing line (row %d)\n%s",
			top, bottom, l.EditorTop, h.screen.Contents())
	}
	return top, bottom, h.rows(top, bottom)
}

// assertPanelBandOnly checks the invariant the fix exists for: the caller's
// lines are on the BAND's rows, and the chrome keeps its own text.
func assertPanelBandOnly(t *testing.T, h *streamHarness, lines []string) {
	t.Helper()
	_, _, band := panelBand(t, h)
	for _, line := range lines {
		if !strings.Contains(band, line) {
			t.Fatalf("the panel line %q is NOT in the answer band — the caller's text never reached the reserved region\n--- band ---\n%s\n--- screen ---\n%s",
				line, band, h.screen.Contents())
		}
	}
	l := h.layout()
	// The chrome rows carry none of the panel's text. This is the exact defect:
	// the panel's first row landed ON the editing row and the rest flowed down
	// over the band's rows.
	for _, c := range []struct {
		what string
		row  int
	}{{"the editing row", l.EditorTop}, {"the status row", l.StatusRow}} {
		got := h.screen.Line(c.row)
		for _, line := range lines {
			if strings.Contains(got, line) {
				t.Fatalf("%s (row %d) carries the panel line %q: %q — the panel is painting on the layout's chrome\n--- screen ---\n%s",
					c.what, c.row, line, got, h.screen.Contents())
			}
		}
	}
	// The status row still shows the status, and the editing row the prompt.
	if got := h.screen.Line(l.StatusRow); !strings.Contains(got, panelStatusModel) {
		t.Fatalf("the status row lost its own text after the panel: %q\n%s", got, h.screen.Contents())
	}
	if got := h.screen.Line(l.EditorTop); !strings.Contains(got, chatPrompt) {
		t.Fatalf("the editing row %d no longer shows the prompt %q: %q\n%s",
			l.EditorTop, chatPrompt, got, h.screen.Contents())
	}
}

// TestChatHelpPanel_RendersInTheBandNotOverTheChrome is the defect's direct
// guard: the REPL prints a multi-line panel (the /help shape) and the RENDERED
// screen must show every line inside the answer band, with the editing row and
// the status row untouched.
//
// RED PROOF: route chatInput.WriteLines's reserved branch back to the historic
// `fmt.Fprint(os.Stdout, …)` (delete the editor branch) and the panel's first
// line lands on the editing row while the rest scroll the band — the chrome
// assertion above fires naming the panel line it found there.
func TestChatHelpPanel_RendersInTheBandNotOverTheChrome(t *testing.T) {
	h := panelHarness(t)
	h.in.PublishStatus()
	// The status row must be DRAWN before it can be asserted to survive, and the
	// wait is on that row itself: waitDraws (retired, see chat_stream_test.go)
	// waited for the answer renderer's DRAW counter, which is incremented for
	// whatever draw the read loop services — an earlier publish satisfied it and
	// this assertion then ran against a status row that had not been drawn yet.
	h.waitFor(t, "the published model on the status row", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(h.layout().StatusRow), panelStatusModel)
	})

	// The user has typed at the prompt (the REPL only prints a panel after a
	// submitted line, so the editing line exists and carries a prompt).
	h.typeKeys("/", "h", "e", "l", "p")
	editorTop := h.editorRow(t)
	h.waitFor(t, "the typed line on the editing row", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(editorTop), "/help")
	})

	panel := []string{
		"Usage: /help <cmd> — detailed usage for ONE command",
		"  /quit    end the session and exit (also /exit — one command, two",
		"  /clear   forget the conversation and start fresh (also /new)",
		"  /tools   flip between tool-enabled and plain chat mid",
		"  /help    print this help plus the shared slash table",
		"Type any other line to chat with the model.",
	}
	h.in.WriteLines(strings.Join(panel, "\n"))

	assertPanelBandOnly(t, h, panel)
	l := h.layout()
	t.Logf("rendered screen after a 6-line panel (band last row %d, editing row %d, status row %d):\n%s",
		l.BandBottom, l.EditorTop, l.StatusRow, h.screen.Contents())
}

// TestChatHarnessReadLoopsRetireOnTeardown is the REGRESSION GUARD (Rule 5)
// for the teardown defect this family had, stated as the invariant that was
// violated: every harness that starts the editor's read loop must be able to
// join it, and the loop must actually RETIRE once the source is stopped.
//
// THE DEFECT IT GUARDS. panelHarness and flowHarness started `go in.ReadLine()`
// with the harness's `done` channel left NIL — a nil channel never closes, so
// the join in newStreamHarnessDumped's cleanup was not merely missing, it was
// not expressible — and their cleanup stopped the source and closed the editor
// without waiting. The loop therefore survived its test and was still inside
// Renderer.Finish when the suite's close-out ran; reportSuiteGoroutinesAtCloseOut
// then reported it as a PARKED repository goroutine and TestMain turned that
// count into a FAILED run — after every assertion in the test body had passed.
// A harness that fails a green test at random is the worst shape a defect has,
// which is why the guard is on the CONTRACT (joinability + retirement) rather
// than on one arm.
//
// RED PROOF: restore the pre-fix shape in either arm — drop `done:
// make(chan struct{})` from the harness literal and the `defer close(h.done)`
// from the loop, and put the `src.stopSource(); in.Close()` cleanup back — and
// this guard fails on that arm after completionTimeout, naming the loop that
// never retired. Measured on the reverted tree: `panel` FAIL, `flow` FAIL.
func TestChatHarnessReadLoopsRetireOnTeardown(t *testing.T) {
	for _, c := range streamHarnessArms() {
		t.Run(c.arm, func(t *testing.T) {
			h := c.build(t)
			if h.done == nil {
				t.Fatalf("the %s harness left its loop-retirement channel nil, so joining the read loop is not even expressible — the loop would outlive this test and the suite's close-out goroutine sweep would report it as parked", c.arm)
			}
			if !h.stopReadLoop() {
				t.Fatalf("the %s harness's read loop did not retire within %v of the source stopping: it outlives its test (still inside Renderer.Finish), and the suite's close-out goroutine sweep turns that into a FAILED run after a green test body",
					c.arm, completionTimeout)
			}
		})
	}
}

// chatReaderHarnessArms are the harnesses whose first ReadLine RESULT is the
// artifact under assertion (so their retirement channel is the loop's own
// return, not a result channel) — enumerated here beside the streamHarness arms
// for the same reason: a guard on one shape must not read as a guard on the
// family.
func chatReaderHarnessArms() []struct {
	arm   string
	build func(*testing.T) (*scriptSource, chan struct{}, *lineedit.Screen)
} {
	return []struct {
		arm   string
		build func(*testing.T) (*scriptSource, chan struct{}, *lineedit.Screen)
	}{
		{"completion", func(t *testing.T) (*scriptSource, chan struct{}, *lineedit.Screen) {
			h := newCompletionHarness(t, &completionStub{})
			return h.src, h.loopDone, h.screen
		}},
	}
}

// readerHarnessGlob is the pattern the completeness guard reads the package's
// test sources through. It is package-relative because `go test` runs each test
// binary with its working directory set to the package being tested — the
// directory that holds these very files.
const readerHarnessGlob = "*_test.go"

// TestChatHarnessReadLoopsAreTheOnlyReaders pins the OTHER half of the contract,
// and it is what makes "the family" a mechanical fact rather than a claim: the
// arms above can only be enumerated by hand, so a NEW harness that starts its own
// `go in.ReadLine()` — exactly the way panelHarness and flowHarness drifted, and
// exactly how this session found the sixth one — would be invisible to that guard
// and would reintroduce the flake it exists to prevent.
//
// So this counts the call sites. Every ReadLine goroutine in this package's tests
// must live in a file that reaches the family's teardown.
func TestChatHarnessReadLoopsAreTheOnlyReaders(t *testing.T) {
	paths, err := filepath.Glob(readerHarnessGlob)
	if err != nil {
		t.Fatalf("glob the package's test files: %v", err)
	}
	if len(paths) == 0 {
		t.Fatalf("no test files matched %q from %s — this guard would pass vacuously", readerHarnessGlob, cwdForTest(t))
	}
	// reader is the shape a background read loop has: a goroutine that owns the
	// line reading. It is matched against the file with COMMENTS REMOVED — the
	// first version matched them, and this family discusses its own defect in prose
	// constantly: lineedit_wire_test.go mentions `go in.ReadLine()` inside a
	// comment explaining the boot window and was reported as a read loop it does
	// not have (measured — a false positive that would have made the guard
	// unusable, and that a "just add it to the list" fix would have hidden).
	reader := regexp.MustCompile(`\bgo func\(\)`)
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		src := stripGoComments(string(b))
		if !strings.Contains(src, readLineCall) || !reader.MatchString(src) {
			continue
		}
		if reachesAny(src, familyTeardownNames) {
			continue
		}
		t.Errorf("%s starts a background `%s` but never reaches the family's teardown (%s): the read loop and its join are one contract, and a harness that starts a loop without joining it outlives its test — the suite's close-out goroutine sweep then reports the loop as a PARKED repository goroutine and turns an otherwise green run into a FAILED one (the panel/flow defect). Route the loop through streamHarness.startReadLoop, or join through joinReadLoop",
			p, readLineCall, strings.Join(familyTeardownNames, " / "))
	}
}

// stripGoComments removes line and block comments from Go source so a guard can
// ask what the code DOES rather than what it says — this file's family documents
// its own defect in comments, so the distinction is not academic (the comment in
// lineedit_wire_test.go naming `go in.ReadLine()` was reported as a loop).
//
// It is deliberately a scanner, not a parser: quotes and backticks are honoured so
// a comment marker inside a string literal is not mistaken for a comment, which is
// all the precision an answer to "does this file start a read loop" needs.
func stripGoComments(src string) string {
	var out strings.Builder
	out.Grow(len(src))
	const (
		plain = iota
		inLineComment
		inBlockComment
		inString
		inRawString
		inRune
	)
	state := plain
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch state {
		case plain:
			switch {
			case c == '/' && i+1 < len(src) && src[i+1] == '/':
				state = inLineComment
				i++
			case c == '/' && i+1 < len(src) && src[i+1] == '*':
				state = inBlockComment
				i++
			case c == '"':
				state = inString
				out.WriteByte(c)
			case c == '`':
				state = inRawString
				out.WriteByte(c)
			case c == '\'':
				state = inRune
				out.WriteByte(c)
			default:
				out.WriteByte(c)
			}
		case inLineComment:
			if c == '\n' {
				state = plain
				out.WriteByte(c)
			}
		case inBlockComment:
			if c == '*' && i+1 < len(src) && src[i+1] == '/' {
				state = plain
				i++
			}
		case inString:
			out.WriteByte(c)
			if c == '\\' && i+1 < len(src) {
				i++
				out.WriteByte(src[i])
				continue
			}
			if c == '"' {
				state = plain
			}
		case inRawString:
			out.WriteByte(c)
			if c == '`' {
				state = plain
			}
		case inRune:
			out.WriteByte(c)
			if c == '\\' && i+1 < len(src) {
				i++
				out.WriteByte(src[i])
				continue
			}
			if c == '\'' {
				state = plain
			}
		}
	}
	return out.String()
}

// cwdForTest reports the directory a test binary is running in — which is the
// package's own directory — for a guard that must READ the package's sources
// (the shape TestChatHarnessReadLoopsAreTheOnlyReaders needs, and the reason it
// is package-relative rather than repo-relative: `go test` sets the working
// directory per package, and a repo-relative glob silently matched NOTHING here,
// which the guard's own non-empty check then reported instead of passing
// vacuously — measured).
func cwdForTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("resolve the test binary's working directory: %v", err)
	}
	return dir
}

// readLineCall is the expression a background read loop is built from — the shape
// the panel/flow defect had, matched as SOURCE rather than asked of a compiler.
const readLineCall = "in.ReadLine()"

// familyTeardownNames are this family's entry points to joining a read loop: the
// per-harness path (startReadLoop registers stopReadLoopOrFail), and the two
// direct helpers a harness with its own result channel uses (joinReadLoop for
// the bare join, stopReadLoopOrFail when the harness carries a streamHarness's
// screen).
var familyTeardownNames = []string{"startReadLoop", "joinReadLoop", "stopReadLoopOrFail"}

// reachesAny reports whether the source mentions any of the family's teardown
// entry points.
//
// WHAT THIS IS AND IS NOT: it is a COVERAGE check, not a proof that a particular
// loop is joined — the join and the loop it joins are ~150 lines apart, so the
// check is whole-file by construction, and a file could mention the name and
// still start an unjoined loop. What closes that gap is the pair of retirement
// guards above: they build each enumerated arm and assert the loop actually
// RETIRES within completionTimeout, which no mention can fake. This side exists
// for the harness nobody enumerated yet — the way this family's sixth arm was
// found.
func reachesAny(src string, names []string) bool {
	for _, n := range names {
		if strings.Contains(src, n) {
			return true
		}
	}
	return false
}

// TestChatHarnessReaderArmsRetireOnTeardown is the same contract for the arms
// whose first ReadLine RESULT is the artifact (chatReaderHarnessArms): their
// retirement channel is the loop's own return, so the assertion is the join
// itself rather than a streamHarness field.
func TestChatHarnessReaderArmsRetireOnTeardown(t *testing.T) {
	for _, c := range chatReaderHarnessArms() {
		t.Run(c.arm, func(t *testing.T) {
			src, loopDone, screen := c.build(t)
			if loopDone == nil {
				t.Fatalf("the %s harness left its loop-retirement channel nil, so joining the read loop is not even expressible", c.arm)
			}
			if !joinReadLoop(src, loopDone, screen) {
				t.Fatalf("the %s harness's read loop did not retire within %v of the source stopping, so its Close runs against a live loop and the suite's close-out goroutine sweep reports it as PARKED after a green test body",
					c.arm, completionTimeout)
			}
		})
	}
}

// TestChatHelpPanel_LongPanelScrollsWithoutTearing drives the panel PAST the
// band's capacity: the band is what is left above the chrome, so a panel taller
// than it scrolls — and the invariant is that each written line survives as its
// OWN row, never interleaved with the line written before it (which is what
// per-line flow output produced).
func TestChatHelpPanel_LongPanelScrollsWithoutTearing(t *testing.T) {
	h := panelHarness(t)
	h.in.PublishStatus()
	// The status row must be DRAWN before it can be asserted to survive, and the
	// wait is on that row itself: waitDraws (retired, see chat_stream_test.go)
	// waited for the answer renderer's DRAW counter, which is incremented for
	// whatever draw the read loop services — an earlier publish satisfied it and
	// this assertion then ran against a status row that had not been drawn yet.
	h.waitFor(t, "the published model on the status row", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(h.layout().StatusRow), panelStatusModel)
	})
	h.typeKeys("/", "h")
	editorTop := h.editorRow(t)
	h.waitFor(t, "the typed line on the editing row", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(editorTop), "/h")
	})

	// Three times the terminal's height: the panel MUST scroll the band.
	var panel []string
	for i := 0; i < panelRows*3; i++ {
		panel = append(panel, fmt.Sprintf("panel row %02d with its own distinguishable body", i))
	}
	h.in.WriteLines(strings.Join(panel, "\n"))

	top, bottom, _ := panelBand(t, h)
	// The last panel line is on screen (the band scrolled to it), as ONE row.
	last := panel[len(panel)-1]
	found := false
	for r := top; r <= bottom; r++ {
		if h.screen.Line(r) == last {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("the last panel line %q is not intact on any band row [%d,%d]\n--- screen ---\n%s",
			last, top, bottom, h.screen.Contents())
	}
	// No band row mixes two written lines: every non-empty band row is exactly
	// ONE of the panel's lines, verbatim.
	known := make(map[string]bool, len(panel))
	for _, p := range panel {
		known[p] = true
	}
	for r := top; r <= bottom; r++ {
		got := h.screen.Line(r)
		if got == "" {
			continue
		}
		if !known[got] {
			t.Fatalf("band row %d is not any written panel line — the panel's rows are interleaved: %q\n--- screen ---\n%s",
				r, got, h.screen.Contents())
		}
	}
	// The chrome survives the scrolling.
	l := h.layout()
	if got := h.screen.Line(l.StatusRow); !strings.Contains(got, panelStatusModel) {
		t.Fatalf("the status row was overwritten by the scrolling panel: %q\n%s", got, h.screen.Contents())
	}
	if got := h.screen.Line(editorTop); !strings.Contains(got, chatPrompt) {
		t.Fatalf("the editing row was overwritten by the scrolling panel: %q\n%s", got, h.screen.Contents())
	}
}

// TestChatPanelWrite_KeepsTheFlowPathByteIdentical pins the other half of the
// contract: with no reserved layout (the cooked path, a pipe, --no-editor) the
// caller's text goes to the writer exactly as the historic `fmt.Fprintln` did,
// so a piped script sees no change.
func TestChatPanelWrite_KeepsTheFlowPathByteIdentical(t *testing.T) {
	var buf strings.Builder
	in := &chatInput{flowOut: &buf} // cooked: no editor, so no layout to protect
	in.WriteLines("Usage: /quit")
	if got, want := buf.String(), "Usage: /quit\n"; got != want {
		t.Fatalf("flow path wrote %q, want %q (byte-identical to the historic Fprintln)", got, want)
	}
	buf.Reset()
	in.WriteLines("already terminated\n")
	if got, want := buf.String(), "already terminated\n"; got != want {
		t.Fatalf("flow path wrote %q, want %q (no doubled newline)", got, want)
	}
}

// TestChatNotice_ReachesTheBandWhereTheUserReadsIt is the guard for the
// REPL's OWN one-line notices (`/new`, `/tools`) — the sibling family of the
// panel, and the one whose defect is SILENT: those notices were written to
// stderr with the historic `fmt.Fprint`, so under the reserved layout the write
// landed on the row the editor last drew and the editor's repaint erased it.
// Measured with the SGR-aware instrument on a fixed checkout: the raw PTY stream
// carried `[gopi] conversation cleared.` while the photographed screen showed a
// bare `you>` — the user toggled a mode and read nothing.
//
// The unit of truth is the RENDERED CELL GRID (Rule 10.2): the notice's text
// must be on a band row after the editor's next repaint, and the chrome rows
// must still carry their own content.
func TestChatNotice_ReachesTheBandWhereTheUserReadsIt(t *testing.T) {
	h := panelHarness(t)
	h.in.PublishStatus()
	// The status row must be DRAWN before it can be asserted to survive, and the
	// wait is on that row itself: waitDraws (retired, see chat_stream_test.go)
	// waited for the answer renderer's DRAW counter, which is incremented for
	// whatever draw the read loop services — an earlier publish satisfied it and
	// this assertion then ran against a status row that had not been drawn yet.
	h.waitFor(t, "the published model on the status row", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(h.layout().StatusRow), panelStatusModel)
	})
	h.typeKeys("/", "n", "e", "w")
	editorTop := h.editorRow(t)
	h.waitFor(t, "the typed line on the editing row", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(editorTop), "/new")
	})

	const notice = "[gopi] conversation cleared."
	h.in.WriteNotice(notice)

	// The notice is on the band — not merely written to the stream.
	_, _, band := panelBand(t, h)
	if !strings.Contains(band, notice) {
		t.Fatalf("the notice %q never reached the answer band — the user cannot read it:\n--- band ---\n%s\n--- screen ---\n%s",
			notice, band, h.screen.Contents())
	}
	l := h.layout()
	if got := h.screen.Line(l.EditorTop); strings.Contains(got, notice) {
		t.Fatalf("the notice was painted on the editing row %d: %q — the editor's repaint erases it there\n--- screen ---\n%s",
			l.EditorTop, got, h.screen.Contents())
	}
	// The chrome survives the notice's row.
	if got := h.screen.Line(l.StatusRow); !strings.Contains(got, panelStatusModel) {
		t.Fatalf("the status row lost its own text after the notice: %q\n%s", got, h.screen.Contents())
	}
}

// TestChatNotice_KeepsTheFlowPathByteIdentical pins the cooked/flow half for
// the notice family: with no reserved layout the text goes to the REPL's own
// stream (stderr) as the historic `fmt.Fprint` did, newline included, so the
// testsuite's grep and a piped run see exactly the old bytes.
func TestChatNotice_KeepsTheFlowPathByteIdentical(t *testing.T) {
	var errBuf, flowBuf strings.Builder
	in := &chatInput{errOut: &errBuf, flowOut: &flowBuf} // cooked: no layout to protect
	in.WriteNotice("[gopi] tools disabled — plain conversational Q&A.\n")
	if got, want := errBuf.String(), "[gopi] tools disabled — plain conversational Q&A.\n"; got != want {
		t.Fatalf("notice path wrote %q to the REPL stream, want %q (byte-identical to the historic Fprint)", got, want)
	}
	if flowBuf.Len() != 0 {
		t.Fatalf("the notice leaked into the script-readable stream: %q", flowBuf.String())
	}
	errBuf.Reset()
	in.WriteNotice("no trailing break")
	if got, want := errBuf.String(), "no trailing break\n"; got != want {
		t.Fatalf("notice path wrote %q, want %q (a trailing break is added, never doubled)", got, want)
	}
}

// TestChatPanelWrite_BlankLineReservesARow pins the REPL's inter-turn
// separator: the two paths must agree that a turns-separating blank line is a
// ROW, not a no-op — otherwise the reserved layout runs two turns together while
// the piped path separates them (a difference visible only on a terminal, i.e.
// exactly the class of defect this file guards).
func TestChatPanelWrite_BlankLineReservesARow(t *testing.T) {
	h := panelHarness(t)
	h.in.PublishStatus()
	// The status row must be DRAWN before it can be asserted to survive, and the
	// wait is on that row itself: waitDraws (retired, see chat_stream_test.go)
	// waited for the answer renderer's DRAW counter, which is incremented for
	// whatever draw the read loop services — an earlier publish satisfied it and
	// this assertion then ran against a status row that had not been drawn yet.
	h.waitFor(t, "the published model on the status row", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(h.layout().StatusRow), panelStatusModel)
	})
	h.typeKeys("/", "h")
	editorTop := h.editorRow(t)
	h.waitFor(t, "the typed line on the editing row", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(editorTop), "/h")
	})

	h.in.WriteLines("first turn line")
	h.in.WriteLines("\n") // the REPL's inter-turn separator
	h.in.WriteLines("second turn line")

	top, bottom, band := panelBand(t, h)
	rows := strings.Split(band, "\n")
	// The two lines are on their own rows with a BLANK row between them, and the
	// blank row is a band row (the band scrolled for it).
	first, second := -1, -1
	for i, r := range rows {
		switch r {
		case "first turn line":
			first = i
		case "second turn line":
			second = i
		}
	}
	if first < 0 || second < 0 {
		t.Fatalf("both turn lines must be in the band [%d,%d]\n--- band ---\n%s\n--- screen ---\n%s",
			top, bottom, band, h.screen.Contents())
	}
	if second != first+2 {
		t.Fatalf("the separator row is missing: %q at band row %d, %q at %d (want a blank row between them)\n--- band ---\n%s",
			"first turn line", first, "second turn line", second, band)
	}
	if rows[first+1] != "" {
		t.Fatalf("band row %d between the turns is %q, want blank\n--- band ---\n%s", first+1, rows[first+1], band)
	}
}
