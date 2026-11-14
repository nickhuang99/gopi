package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

// A7 at the REPL's own seam: the completion menu as the CHAT INPUT layer hands
// it over — a lineedit.Completer injected through newChatInputWithCompleter, the
// bytes the editor writes fed back through a vt100 cell grid, and the menu read
// off that grid (Rule 10.2: rendered cells, never "the callback fired").
//
// The completer here is a STUB on purpose: this test pins the editor+input-layer
// contract (typing "/" opens a menu below the line, Tab completes, Esc closes).
// Which candidates the PRODUCTION completer offers — the shared slash-command
// registry and the filesystem — is asserted separately below, against that
// registry.

// chatCompletion geometry and stub vocabulary. Named (Rule 1).
const (
	completionRows = 12
	completionCols = 40
	// completionPollStep / completionTimeout bound the waits for the completer's
	// own goroutine. A wait is not the assertion — the cell comparisons are.
	completionPollStep = 2 * time.Millisecond
	completionTimeout  = 5 * time.Second
	// completionKeyGap separates scripted keystrokes so a run never looks like a
	// paste burst (which changes what Enter means).
	completionKeyGap = 60 * time.Millisecond
	// completionTyped is the prefix the stub answers to and the test types.
	completionTyped = "/mag"
	// completionFirstCandidate is the stub candidate Tab must complete to.
	completionFirstCandidate = completionTyped + "-one"
)

// completionStub offers two candidates for completionTyped and records the lines
// it was asked about (the proof that the request really was handed off).
type completionStub struct {
	mu    sync.Mutex
	calls []string
}

// complete is the injected Completer.
func (s *completionStub) complete(line string, cursor, limit int) []lineedit.Candidate {
	s.mu.Lock()
	s.calls = append(s.calls, line)
	s.mu.Unlock()
	if !strings.HasPrefix(completionFirstCandidate, line) {
		return nil
	}
	return []lineedit.Candidate{
		{Text: completionFirstCandidate, Hint: "first"},
		{Text: completionTyped + "-two", Hint: "second"},
	}
}

// called reports the lines the completer was asked about.
func (s *completionStub) called() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

// completionHarness is the chat input layer (the editor path) plus the screen the
// terminal would show, and a source the test types into while the editor reads.
type completionHarness struct {
	in     *chatInput
	out    *recordingWriter
	screen *lineedit.Screen
	src    *scriptSource
	stub   *completionStub
	done   chan chatLine
	// loopDone is closed when the harness's read loop RETURNS — the join, which
	// is a different fact from done (the loop's one RESULT): a test may drain the
	// result while the goroutine is still inside the editor's exit write, and
	// teardown must wait for the goroutine, not for its message. See
	// joinReadLoop (chat_stream_test.go), the family's one teardown.
	loopDone chan struct{}
	// drained remembers a line already read off done, so "did this key submit?"
	// can be asked more than once without the second ask meeting an empty
	// channel.
	drained *chatLine
}

// chatLine is one ReadLine result.
type chatLine struct {
	line string
	ok   bool
}

// newCompletionHarness builds the editor-backed input over a scripted source
// that HOLDS at the end of its script (so the test can type and assert
// intermediate screens), with the stub completer injected.
func newCompletionHarness(t *testing.T, stub *completionStub) *completionHarness {
	t.Helper()
	clock := &testClock{t: time.Unix(chatTestEpoch, 0)}
	src := &scriptSource{width: completionCols, height: completionRows, clock: clock, holdAtEnd: true}
	out := &recordingWriter{}
	in := newChatInputWithSource(src, out, chatPrompt)
	// The stub arrives through the same seam production uses
	// (newChatInputWithCompleter installs NewSlashCompleter there).
	in.editor.SetCompleter(stub.complete)
	in.editor.SetClock(clock.Now)
	if !in.editor.EnableRegion() {
		t.Fatalf("EnableRegion(%d rows) = false, want true — the chat input layer must reserve the layout here", completionRows)
	}
	h := &completionHarness{
		in:       in,
		out:      out,
		screen:   lineedit.NewScreen(completionCols, completionRows),
		src:      src,
		stub:     stub,
		done:     make(chan chatLine, 1),
		loopDone: make(chan struct{}),
	}
	go func() {
		defer close(h.loopDone)
		line, ok, _, _ := in.ReadLine() //stylegate:ignore the read error is the harness's own teardown: the line/ok pair is what the assertions inspect, and a real failure surfaces as a timeout with the screen dumped
		h.done <- chatLine{line: line, ok: ok}
	}()
	t.Cleanup(func() {
		// The join BEFORE Close, through the family's one teardown: the loop is
		// the one legitimate writer of the region Close mutates (Disable), and a
		// loop left running past its test is what the suite's close-out goroutine
		// sweep reports as PARKED — failing a run whose assertions had all passed.
		// done is the loop's RESULT channel and is not the join: it is buffered,
		// so a line read before the goroutine finished releasing the terminal
		// would let teardown proceed against a live loop.
		joinReadLoopOrFail(t, src, h.loopDone, h.screen)
		in.Close()
	})
	return h
}

// typeKeys types with a human-scale gap between keystrokes.
func (h *completionHarness) typeKeys(keys ...string) {
	for _, k := range keys {
		h.src.push(scriptStep{data: []byte(k), advance: completionKeyGap})
	}
}

// replay feeds every recorded write the screen has not seen yet, in order
// (idempotent — the recording writer owns the fed counter). Only writes never fed
// before are applied, so a read loop still writing cannot append an already-seen
// write a second time.
func (h *completionHarness) replay() { h.out.ReplayInto(h.screen) }

// waitFor polls cond, dumping the screen when the deadline passes.
func (h *completionHarness) waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(completionTimeout)
	for time.Now().Before(deadline) {
		if cond() {
			h.replay()
			return
		}
		time.Sleep(completionPollStep) //stylegate:allow-sleep polling the completion worker, bounded by completionTimeout
	}
	h.replay()
	t.Fatalf("timed out waiting for %s\n--- screen ---\n%s", what, h.screen.Contents())
}

// layout is the reserved layout the editor installed. The editing line is NOT
// screen row 0 — the answer band (the scroll region) is above it — so every row
// assertion goes through the layout instead of assuming a row number.
func (h *completionHarness) layout() lineedit.Layout {
	h.replay()
	l, ok := h.in.editor.Layout()
	if !ok {
		panic("the reserved layout is not installed: " + h.screen.Contents())
	}
	return l
}

// lineRow is the editing line's screen row; menuRow(i) is the i-th menu row.
func (h *completionHarness) lineRow() int      { return h.layout().EditorTop }
func (h *completionHarness) menuRow(i int) int { return h.layout().MenuTop + i }

// rowContaining is the screen row whose text holds want, or -1. The menu's rows
// are located from the LAYOUT the editor published (never from an assumed row
// number), then read off the rendered grid.
func (h *completionHarness) rowContaining(want string) int {
	h.replay()
	for row := 0; row < h.screen.Rows; row++ {
		if strings.Contains(h.screen.Line(row), want) {
			return row
		}
	}
	return -1
}

// submitted reports a line the editor has already finished, without waiting. The
// value is REMEMBERED, so a later read — the second Enter's assertion at the end
// of a test — gets the same answer instead of a drained channel's silence.
func (h *completionHarness) submitted() (string, bool) {
	if h.drained == nil {
		select {
		case res := <-h.done:
			h.drained = &res
		default:
			return "", false
		}
	}
	return h.drained.line, h.drained.ok
}

// TestChatCompletion is the acceptance test for A7, driven through the chat
// input layer with a STUB completer: the menu contents are read off the rendered
// cell grid, Tab completes the selected candidate onto the editing row, and Esc
// erases the menu rows again.
func TestChatCompletion(t *testing.T) {
	stub := &completionStub{}
	h := newCompletionHarness(t, stub)

	h.typeKeys(completionTyped)
	h.waitFor(t, "the completion menu to be drawn below the editing line", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(h.menuRow(0)), completionFirstCandidate)
	})

	// The menu is BELOW the line: the editing row keeps the typed text, the
	// first menu row offers the first candidate with the selection marker, the
	// next offers the second. The rows come from the LAYOUT (the band occupies
	// the screen's top rows), never from a row number.
	if got := h.screen.Line(h.lineRow()); !strings.Contains(got, completionTyped) {
		t.Fatalf("editing row = %q, want the typed %q\n--- screen ---\n%s", got, completionTyped, h.screen.Contents())
	}
	if !strings.HasPrefix(h.screen.Line(h.menuRow(0)), ">") {
		t.Fatalf("menu row %d = %q, want the selection marker on the first candidate", h.menuRow(0), h.screen.Line(h.menuRow(0)))
	}
	if !strings.Contains(h.screen.Line(h.menuRow(1)), completionTyped+"-two") {
		t.Fatalf("menu row %d = %q, want the second candidate\n--- screen ---\n%s", h.menuRow(1), h.screen.Line(h.menuRow(1)), h.screen.Contents())
	}
	// The menu rows are outside the scrolling region: streaming the answer can
	// never carry them away. The region is the answer band ALONE, and its top is
	// the screen's first row — the condition a terminal needs to SAVE a
	// committed line instead of discarding it (A5).
	top, bottom := h.screen.ScrollRegion()
	if top != 0 {
		t.Fatalf("scroll region top = %d, want 0 (the screen's first row) — an interior top discards committed lines", top)
	}
	if bottom >= h.lineRow() {
		t.Fatalf("scroll region = [%d,%d] reaches the editing line (row %d) — the band must end above it",
			top, bottom, h.lineRow())
	}

	// Tab completes the selection onto the editing row.
	h.typeKeys("\t")
	h.waitFor(t, "the completed candidate on the editing row", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(h.lineRow()), completionFirstCandidate)
	})
	if got := h.screen.Line(h.lineRow()); !strings.Contains(got, completionFirstCandidate) {
		t.Fatalf("editing row after Tab = %q, want %q", got, completionFirstCandidate)
	}

	// Escape closes it: the menu rows are ERASED, the typed text stays.
	h.waitFor(t, "the menu to be open again for the narrowed line", func() bool {
		h.replay()
		return h.in.editor.MenuState().Open || strings.Contains(h.screen.Line(h.menuRow(0)), completionFirstCandidate)
	})
	beforeClose := h.in.editor.MenuState()
	h.typeKeys("\x1b")
	h.src.push(scriptStep{timeout: true, advance: 100 * time.Millisecond})
	h.waitFor(t, "the menu to close on Escape", func() bool { return !h.in.editor.MenuState().Open })
	h.replay()
	// The rows the menu occupied are ERASED. A closed menu reserves no rows, so
	// the editing line has moved back up: the check is that no menu content
	// survives anywhere below the line.
	for row := h.lineRow() + 1; row < h.screen.Rows; row++ {
		if got := h.screen.Line(row); strings.Contains(got, "magic") {
			t.Fatalf("row %d still shows menu content %q after Escape (menu was %+v)\n--- screen ---\n%s",
				row, got, beforeClose, h.screen.Contents())
		}
	}
	if got := h.screen.Line(h.lineRow()); !strings.Contains(got, completionFirstCandidate) {
		t.Fatalf("editing row after Escape = %q, want the completed text kept", got)
	}

	// The completer was really asked (the hand-off happened, off the read path).
	if calls := stub.called(); len(calls) == 0 || calls[0] != completionTyped {
		t.Fatalf("completer calls = %v, want the first to be %q", calls, completionTyped)
	}
}

// TestChatCompletion_ProductionCompleterOffersCommandsAndFiles checks the
// completer production installs, against the SAME registry the REPL dispatches
// through: every slash command is offered (so the menu can never suggest a
// command that does not exist), and a file-path prefix offers real entries from
// the working directory.
func TestChatCompletion_ProductionCompleterOffersCommandsAndFiles(t *testing.T) {
	comp := NewSlashCompleter()

	// Every command in the shared table is offered for "/".
	got := comp("/", 1, completionCandidateLimitForTest())
	offered := map[string]bool{}
	for _, c := range got {
		offered[c.Text] = true
	}
	for _, name := range slashCommandNames() {
		if !offered["/"+name] {
			t.Fatalf("the menu does not offer /%s; offered: %v", name, offered)
		}
	}
	// A narrow prefix narrows the list. The prefix is taken from a command that
	// SURVIVES (`/text2img`) rather than a fixed word: `/mem` was the old probe
	// and it retired on 2026-11-07 with the /memory slash command, so a hard-coded
	// word makes this case measure a menu that no longer has it.
	const narrowPrefix = "/text"
	narrow := comp(narrowPrefix, len(narrowPrefix), completionCandidateLimitForTest())
	if len(narrow) == 0 {
		t.Fatalf("no candidate for a known command prefix %s", narrowPrefix)
	}
	for _, c := range narrow {
		if !strings.HasPrefix(c.Text, narrowPrefix) {
			t.Fatalf("candidate %q does not match the typed prefix %s", c.Text, narrowPrefix)
		}
	}
	// An unknown prefix offers nothing (no invented commands).
	if extra := comp("/zzz-not-a-command", 18, completionCandidateLimitForTest()); len(extra) != 0 {
		t.Fatalf("candidates for an unknown command = %v, want none", extra)
	}

	// A file prefix offers real entries: the test's own working directory holds
	// at least the file it was written from.
	dir := t.TempDir()
	const probe = "completion-probe.txt"
	if err := os.WriteFile(filepath.Join(dir, probe), []byte("probe\n"), 0o600); err != nil {
		t.Fatalf("write probe file: %v", err)
	}
	t.Chdir(dir)
	files := comp(completionFilePrefix+"comp", 7, completionCandidateLimitForTest())
	if len(files) == 0 {
		t.Fatalf("no file candidate for %scomp in %s", completionFilePrefix, dir)
	}
	if got := files[0].Text; got != completionFilePrefix+probe {
		t.Fatalf("file candidate = %q, want %q", got, completionFilePrefix+probe)
	}
}

// completionCandidateLimitForTest mirrors the menu's own candidate cap (the
// editor's constant), so the assertions ask for as many as a real menu shows.
func completionCandidateLimitForTest() int { return lineedit.CompletionCandidateLimit() }
