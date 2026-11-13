package lineedit

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// A7 tests — the completion menu, asserted through the RENDERED SCREEN.
//
// Every assertion below reads a vt100 cell grid fed by the bytes the editor
// actually wrote (Rule 10.2 / roadmap §5.7): "the completer was called" or "the
// menu state says Open" cannot see a menu drawn on the wrong rows, a selection
// that moved internally but not on screen, or a close that left the rows
// painted. The grid also models the scroll region, so the A5 layout's promise
// (only the band scrolls) is checked in the same pass.
//
// Candidates come from a STUB completer: the editor's contract is "compute off
// the read/redraw path and publish a list", which is what these tests pin.
// Which candidates production offers (slash commands, file paths) is cmd/agent's
// business and is covered there.

// a7 geometry, budget and stub vocabulary. Named (Rule 1) so the assertions read
// as layout arithmetic against a known screen, not as magic rows.
const (
	a7Rows = 12
	a7Cols = 40
	// menuSecondRow is the second menu row on the a7 screen. The reserved band
	// sits ABOVE the editing line (A5's scrollback condition: the band's top is
	// the screen's first row), so the editing line is NOT row 0 — the menu's rows
	// are derived from the LAYOUT at run time (a7Harness.menuTopRow /
	// screenRowOffersCandidate), never from a fixed row number.
	menuSecondRow = 2
	// a7Wait bounds a wait for the worker goroutine's effect. The read loop
	// wakes on a bounded completion poll, so this is a few poll intervals wide:
	// a deadline that turns a hang into a screen dump, never the assertion.
	a7Wait = 2 * time.Second
	// a7PollStep is that wait's polling interval.
	a7PollStep = 2 * time.Millisecond
	// a7KeyGap separates scripted keystrokes so they never look like a paste
	// burst (a burst changes what Enter means).
	a7KeyGap = 60 * time.Millisecond
	// a7WritesPerKey is the repaint budget per keystroke: the editing line, the
	// menu rows it touches, and the erase of the rows it vacates.
	a7WritesPerKey = 8
	// a7RepaintsPerKey is the same budget counted in REDRAW DECISIONS: one key
	// repaints the line and the menu, and an arrow key's selection move is one
	// more. It is the primary assertion — a repaint decision is what the editor
	// controls, while the write(2) chunk count is a transport detail that
	// varies with scheduling (the loose a7WritesPerKey budget above catches a
	// storm that "saved" writes by drawing nothing).
	a7RepaintsPerKey = 3
	// a7RepaintsOverhead is the fixed part of the repaint budget: the region's
	// re-installs and the close's own erase + cursor placement.
	a7RepaintsOverhead = 8
	// a7WritesOverhead is the fixed part of that budget (the region's own
	// re-installs and the closing cursor placement).
	a7WritesOverhead = 24
	// settleQuietMS is how long the write stream must be quiet before a write
	// count is meaningful: the completion worker publishes from its own
	// goroutine, and that publish paints on its own path. It is a few poll
	// intervals wide (a7PollStep) so a single in-flight publish is always seen,
	// and far shorter than the editor's slow completion cadence — it can never
	// wait for a repaint the editor does not owe.
	settleQuietMS = 40
	// a7SlowCompleterHold is how long the blocked completer is held before it is
	// released. It exceeds any bounded wake cadence the editor uses, so the test
	// exercises "the answer arrives long after the request" — the shape of a
	// cold filesystem or a busy host.
	a7SlowCompleterHold = 500 * time.Millisecond
	// magicOpensTab is the line the menu opens on: the prefix production uses
	// plus the three characters the stub candidates share. Spelled once here;
	// every assertion references the constant, never the literal.
	magicOpensTab = "/mag"
)

// menuStubResult is the vocabulary the stub completer offers for a line that
// starts with "/". The labels are short so a whole row fits a7Cols and the
// assertions can compare full cell content.
var menuStubResult = []Candidate{
	{Text: "/magic-one", Hint: "first"},
	{Text: "/magic-two", Hint: "second"},
	{Text: "/magic-three", Hint: "third"},
}

// stubCompleter records every call and answers with a fixed list. A non-nil
// block channel holds the answer until the test releases it — how "the completer
// is slow" is modelled without a sleep.
type stubCompleter struct {
	mu    sync.Mutex
	calls []string
	block chan struct{}
	// result is the vocabulary this stub filters. nil means menuStubResult (the
	// three-candidate list every historic test is written against); a ONE-entry
	// list is how the single-candidate case is reached at all — the menu that
	// shipped inert for "/q" was unreachable from a three-candidate stub, which
	// is exactly why the defect shipped.
	result []Candidate
}

// offers is the stub's vocabulary (menuStubResult unless the test named its own).
func (s *stubCompleter) offers() []Candidate {
	if len(s.result) > 0 {
		return s.result
	}
	return menuStubResult
}

// complete is the Completer the editor is built with.
func (s *stubCompleter) complete(line string, cursor, limit int) []Candidate {
	s.mu.Lock()
	s.calls = append(s.calls, line)
	block := s.block
	s.mu.Unlock()
	if block != nil {
		<-block
	}
	vocab := s.offers()
	out := make([]Candidate, 0, len(vocab))
	for _, c := range vocab {
		if strings.HasPrefix(c.Text, line) {
			out = append(out, c)
		}
	}
	return out
}

// called reports every line the completer was asked about (a copy).
func (s *stubCompleter) called() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

// release unblocks a blocking completer.
func (s *stubCompleter) release() {
	s.mu.Lock()
	block := s.block
	s.mu.Unlock()
	if block != nil {
		close(block)
	}
}

// a7Harness is an editor with the reserved layout and a stub completer, plus the
// screen the terminal would be showing. The scripted source HOLDS at the end of
// its script, so the editor is reading while the test types and can assert
// intermediate screen states.
type a7Harness struct {
	ed     *Editor
	out    *recordingWriter
	screen *Screen
	region *region
	stub   *stubCompleter
	src    *scriptedSource
	done   chan Result
	// fed is how many recorded writes have already been fed into the screen.
	fed int
	// exited is closed when Run returns. The tests CONSUME done (the submitted
	// line), so teardown cannot use it as the "Run is over" signal — a consumed
	// channel would look like a hang.
	exited chan struct{}
	// notifyW is the write end of the editor's notification pipe: a test can
	// wake the reader directly, with no publish behind it.
	notifyW *os.File
}

// newA7Harness builds the editor at a7Rows × a7Cols and starts Run.
func newA7Harness(t *testing.T, stub *stubCompleter) *a7Harness {
	t.Helper()
	clock := &fakeClock{t: time.Unix(a5UnixTime, 0)}
	src := &scriptedSource{width: a7Cols, height: a7Rows, clock: clock, holdAtEnd: true}
	out := &recordingWriter{}
	// The notification pipe is the terminal's job in production (the editor
	// asks the source for it); the scripted source owns it here, so a
	// completion publish really INTERRUPTS the editor's blocked read.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("notification pipe: %v", err)
	}
	src.setNotifyPipe(r, w)
	ed := NewChatEditor(src, out, promptForTest, nil, nil, stub.complete)
	ed.SetCompletionNotify(func() { _, _ = w.Write([]byte{0}) }) //stylegate:ignore best-effort notification: a pipe that refuses the byte is already unreadable and the fallback cadence covers it
	ed.SetClock(clock.Now)
	// Only the WRITE end is closed here: that unblocks the watching goroutine
	// (it owns and closes the read end), and the pipe's write is safe to call
	// concurrently with a close.
	t.Cleanup(func() { _ = w.Close() }) //stylegate:ignore teardown of a pipe the test owns: an error here has nothing left to be reported to
	if !ed.EnableRegion() {
		t.Fatalf("EnableRegion(%d rows) = false, want true", a7Rows)
	}
	h := &a7Harness{
		ed: ed, out: out, screen: NewScreen(a7Cols, a7Rows), region: ed.region,
		stub: stub, src: src, done: make(chan Result, 1), exited: make(chan struct{}),
		notifyW: w,
	}
	go func() {
		defer close(h.exited)
		res, err := ed.Run()
		if err != nil {
			h.done <- Result{Line: "ERR: " + err.Error()}
			return
		}
		h.done <- res
	}()
	// Teardown ORDER matters and is asserted by the race detector: Run's
	// goroutine READS the editor's state (the region's active flag, the buffer,
	// the source's geometry) on every loop iteration, so Close must not run
	// while it is still looping. So: stop the source (which also UNBLOCKS a
	// parked read), wait for Run to return, and only then close the editor.
	t.Cleanup(func() {
		// The teardown ORDER is the one stop() establishes and the race detector
		// asserts: stop the source (which also unblocks a parked read), wait for
		// Run to return, and only then close the editor — DisableRegion writes
		// region state the running loop reads, so closing first is a race inside
		// the HARNESS (measured under -race), and a harness race masks the
		// production one a test exists to find.
		h.stop()
		ed.Close()
	})
	return h
}

// wake signals the editor's completion notification pipe directly — the way the
// terminal would after a publish — WITHOUT publishing anything. It is how a test
// asks "what does the editor do with a wake that carries no new information".
func (h *a7Harness) wake() {
	_, _ = h.notifyW.Write([]byte{0}) //stylegate:ignore best-effort wake: the reader's own fallback cadence covers a refused byte
}

// stop ends the editor's read loop and waits for Run to return. It is
// idempotent, so a test that already stopped the harness is unaffected.
func (h *a7Harness) stop() {
	h.src.stopSource()
	select {
	case <-h.exited:
	case <-time.After(a7Wait):
		panic("a7: Run did not return after the source was stopped")
	}
}

// type keys with a human-scale gap between keystrokes (never a paste burst).
func (h *a7Harness) typeKeys(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		h.src.push(scriptStep{data: []byte(k), advance: a7KeyGap})
	}
}

// replay feeds every recorded write into the screen that has not been fed yet,
// in order. It is INCREMENTAL — the screen is a running replay of the write
// stream — because the waits below call it on every poll: re-feeding the whole
// history each time is quadratic in the writes, which made the polls slow enough
// to consume their own deadline under -race. Only writes never fed before are
// applied, so a read loop writing concurrently with the assertions cannot append
// an already-seen write a second time.
func (h *a7Harness) replay() {
	chunks := h.out.snapshot()
	for _, w := range chunks[h.fed:] {
		h.screen.Feed(w)
	}
	h.fed = len(chunks)
}

// rows renders every screen row for a failure dump.
func (h *a7Harness) rows() string {
	h.replay()
	return h.screen.Contents()
}

// screenAtLine is the editing line as the screen currently shows it. The row is
// asked of the LAYOUT (the band above the line is part of the reserved layout),
// so this helper keeps working whatever the band's height is.
func (h *a7Harness) screenAtLine() string {
	h.replay()
	return h.screen.Line(h.region.EditorTop())
}

// editRow is the layout's editing row (0-based). Every screen assertion about
// the typed line goes through it.
func (h *a7Harness) editRow() int {
	h.replay()
	return h.region.EditorTop()
}

// menuTopRow is the first menu row as the SCREEN shows it (0-based), or -1 when
// no menu is drawn. It is derived from the rendered grid rather than from the
// editor's live layout state: the editor's read loop runs concurrently with
// these assertions, and the screen is both the race-free observation and the
// thing the user actually sees.
func (h *a7Harness) menuTopRow() int {
	h.replay()
	for row := h.region.EditorTop() + 1; row < a7Rows; row++ {
		if h.screenRowOffersCandidate(row) {
			return row
		}
	}
	return -1
}

// screenRowOffersCandidate reports whether the row shows one of the stub's
// candidates. The menu top is found by its CANDIDATES, not by the selection
// marker: after Down the marker is on the second row, so a marker-based search
// would name that row the menu's top. The vocabulary is read off the STUB (not
// a fixed list), so the single-candidate harness finds its own menu too.
func (h *a7Harness) screenRowOffersCandidate(row int) bool {
	line := h.screen.Line(row)
	for _, c := range h.stub.offers() {
		if strings.Contains(line, c.Text) {
			return true
		}
	}
	return false
}

// menuTop is menuTopRow for callers that have already waited for the menu: a
// missing menu is a test failure with the screen dumped.
func (h *a7Harness) menuTop(t *testing.T) int {
	t.Helper()
	row := h.menuTopRow()
	if row < 0 {
		t.Fatalf("no menu on screen: no row below the editing line starts with the selection marker %q\n--- screen ---\n%s",
			menuMarkerActive, h.screen.Contents())
	}
	return row
}

// waitFor waits until cond holds, dumping the screen when the deadline passes.
// It is a synchronization wait, never the assertion: the cell comparisons below
// are the assertions.
func (h *a7Harness) waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(a7Wait)
	for time.Now().Before(deadline) {
		if cond() {
			h.replay()
			return
		}
		time.Sleep(a7PollStep) //stylegate:allow-sleep polling the editor's own bounded wake, bounded by a7Wait
	}
	h.src.mu.Lock()
	to := append([]int(nil), h.src.timeouts...)
	h.src.mu.Unlock()
	if len(to) > 8 {
		to = to[len(to)-8:]
	}
	h.ed.menu.mu.Lock()
	seq, fin, rev := h.ed.menu.seq, h.ed.menu.finished, h.ed.menu.rev
	h.ed.menu.mu.Unlock()
	t.Fatalf("timed out waiting for %s [DIAG seq=%d fin=%d rev=%d drawn=%d wakes=%d open=%v ncand=%d lastTimeouts=%v]\n--- screen ---\n%s",
		what, seq, fin, rev, h.ed.menuRev, h.ed.completionWakes, h.ed.MenuState().Open, len(h.ed.MenuState().Candidates), to, h.rows())
}

// waitForMenu waits until the published menu is on screen (its first candidate
// row is visible).
func (h *a7Harness) waitForMenu(t *testing.T) {
	t.Helper()
	first := h.stub.offers()[0].Text
	h.waitFor(t, "the completion menu to be drawn below the editing line", func() bool {
		if m := h.ed.MenuState(); !m.Open || len(m.Candidates) == 0 {
			return false
		}
		top := h.menuTopRow()
		return top >= 0 && strings.Contains(h.screen.Line(top), first)
	})
}

// waitForSettledInput waits until the editor has consumed EVERY keystroke of
// line and the completion work it triggered has been published AND drawn.
//
// It exists because a SCREEN condition is not an input condition: the menu for
// "/m" is already on screen while "ag" is still undelivered, so a measurement
// window opened on "the menu is drawn" silently includes the trailing
// keystrokes' own (legitimate) repaints and overshoots any per-key budget.
// Every input-driven effect has to be drained before a write count means
// anything: the last request must be for line, its answer must be published
// (requestPending false), and that answer must be on screen (menuUndrawn
// false). After this the editor is parked with nothing outstanding, so the
// writes that follow belong to the keys the caller is about to type.
func (h *a7Harness) waitForSettledInput(t *testing.T, line string) {
	t.Helper()
	h.waitFor(t, "the typed line's completion state to settle (consumed, published, drawn)", func() bool {
		calls := h.stub.called()
		if len(calls) == 0 || calls[len(calls)-1] != line {
			return false
		}
		if h.ed.menu == nil || h.ed.menu.requestPending() || h.ed.menuUndrawn() {
			return false
		}
		// A published answer that is drawn is not enough on its own: the
		// publisher repaints its own path, so require the write stream to have
		// been quiet for one poll interval before the window opens.
		h.replay()
		return true
	})
	// The worker publishes in a goroutine, so give the LAST publish's own
	// repaint a chance to land before the window opens.
	h.waitForQuietWrites(t)
}

// assertNotDone fails when Run has already returned (the editor was supposed to
// still be editing).
func (h *a7Harness) assertNotDone(t *testing.T) {
	t.Helper()
	select {
	case res := <-h.done:
		t.Fatalf("Run returned (%+v) while the test expected the editor to still be reading", res)
	default:
	}
}

// TestChatCompletion_MenuOpensBelowTheLine is the primary A7 assertion: typing
// "/" opens a menu of candidates DRAWN BELOW the editing line — the editing row
// keeps the typed text, the first candidate row starts directly under it, and
// the selection marker sits on the first candidate. The scroll region covers only
// the answer band, so streamed output can never carry the menu away.
func TestChatCompletion_MenuOpensBelowTheLine(t *testing.T) {
	h := newA7Harness(t, &stubCompleter{})
	h.typeKeys(t, magicOpensTab)
	h.waitForMenu(t)

	if got := h.screen.Line(h.editRow()); !strings.Contains(got, magicOpensTab) {
		t.Fatalf("editing row = %q, want the typed line %q\n--- screen ---\n%s", got, magicOpensTab, h.rows())
	}
	if menuTop := h.menuTop(t); menuTop != h.editRow()+1 {
		t.Fatalf("menu top = %d, want %d (directly below the single-row editing line on row %d)",
			menuTop, h.editRow()+1, h.editRow())
	}
	menuTop := h.menuTop(t)
	for i, want := range []string{"/magic-one", "/magic-two", "/magic-three"} {
		if got := h.screen.Line(menuTop + i); !strings.Contains(got, want) {
			t.Fatalf("menu row %d = %q, want it to offer %q\n--- screen ---\n%s", i, got, want, h.rows())
		}
	}
	if got := h.screen.Line(menuTop); !strings.HasPrefix(got, menuMarkerActive) {
		t.Fatalf("menu row 0 = %q, want the selection marker %q on the first candidate", got, menuMarkerActive)
	}
	if got := h.screen.Line(menuTop + 1); !strings.HasPrefix(got, menuMarkerIdle) {
		t.Fatalf("menu row 1 = %q, want the unselected marker %q", got, menuMarkerIdle)
	}
	// The hint column is part of the row: the menu says WHAT the candidate is.
	if got := h.screen.Line(menuTop); !strings.Contains(got, menuStubResult[0].Hint) {
		t.Fatalf("menu row 0 = %q, want the hint %q", got, menuStubResult[0].Hint)
	}
	// The scroll region is the ANSWER BAND and nothing else: it ends above the
	// editing line, so no menu row (and no editing row) can ever scroll with
	// streamed output. And its TOP is the screen's first row — the condition a
	// terminal needs to SAVE a committed line instead of discarding it (A5).
	top, bottom := h.screen.ScrollRegion()
	if bottom >= menuTop {
		t.Fatalf("scroll region = [%d,%d], want it to end above the menu's first row (%d) — the menu must not scroll with the answer",
			top, bottom, menuTop)
	}
	if top != editorTopRow {
		t.Fatalf("scroll region top = %d, want %d (the screen's first row) — an interior top discards committed lines",
			top, editorTopRow)
	}
	h.assertNotDone(t)
	// The stub was asked about the typed line, never a synthesized one.
	calls := h.stub.called()
	if len(calls) == 0 || calls[len(calls)-1] != magicOpensTab {
		t.Fatalf("completer calls = %v, want the last to be %q", calls, magicOpensTab)
	}
}

// TestChatCompletion_ArrowMovesTheSelectionOnScreen is the render-truth half of
// the selection: Down moves the marker to the second row ON SCREEN. The buffer
// does not change, so this passes only if the menu's own change triggers a
// repaint (the defect this test was written for: the selection moved internally
// and the screen never showed it).
func TestChatCompletion_ArrowMovesTheSelectionOnScreen(t *testing.T) {
	h := newA7Harness(t, &stubCompleter{})
	h.typeKeys(t, magicOpensTab)
	h.waitForMenu(t)

	h.typeKeys(t, "\x1b[B") // Down
	// The marker is on row 1 (0-based) once the menu is open directly below a
	// single editing row — the layout the assertions below pin.
	h.waitFor(t, "the selection marker to move to the second menu row", func() bool {
		h.replay()
		return strings.HasPrefix(h.screen.Line(h.menuTop(t)+1), menuMarkerActive)
	})

	rows := []string{h.screen.Line(h.menuTop(t)), h.screen.Line(h.menuTop(t) + 1)}
	if !strings.HasPrefix(rows[1], menuMarkerActive) {
		t.Fatalf("after Down, menu row 1 = %q, want the selection marker %q", rows[1], menuMarkerActive)
	}
	if !strings.HasPrefix(rows[0], menuMarkerIdle) {
		t.Fatalf("after Down, menu row 0 = %q, want the unselected marker %q", rows[0], menuMarkerIdle)
	}
	// The editing line is untouched by a selection move.
	if got := h.screen.Line(h.editRow()); !strings.Contains(got, magicOpensTab) {
		t.Fatalf("editing row = %q after a selection move, want %q unchanged", got, magicOpensTab)
	}
}

// TestChatCompletion_TabCompletesTheSelectedCandidate is Tab's contract: the
// selected candidate replaces the word being completed, the editing line shows
// it, and the submitted line carries it.
func TestChatCompletion_TabCompletesTheSelectedCandidate(t *testing.T) {
	h := newA7Harness(t, &stubCompleter{})
	h.typeKeys(t, magicOpensTab)
	h.waitForMenu(t)

	h.typeKeys(t, "\t") // Tab
	h.waitFor(t, "the completed candidate on the editing row", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(h.editRow()), menuStubResult[0].Text)
	})

	if got := h.screen.Line(h.editRow()); !strings.HasSuffix(strings.TrimRight(got, " "), menuStubResult[0].Text) {
		t.Fatalf("editing row after Tab = %q, want the completed candidate %q ending the line\n--- screen ---\n%s",
			got, menuStubResult[0].Text, h.rows())
	}
	if got := h.screen.Line(h.editRow()); strings.Contains(got, magicOpensTab+"c") {
		t.Fatalf("editing row = %q, want the typed prefix REPLACED by the candidate, not appended", got)
	}

	h.typeKeys(t, "\r") // Enter submits what is on screen
	select {
	case res := <-h.done:
		if res.Line != menuStubResult[0].Text {
			t.Fatalf("submitted line = %q, want the completed candidate %q", res.Line, menuStubResult[0].Text)
		}
	case <-time.After(a7Wait):
		t.Fatalf("Enter never submitted the completed line\n--- screen ---\n%s", h.rows())
	}
}

// TestChatCompletion_EscapeClosesTheMenu is Esc's contract: the menu rows are
// ERASED from the screen (not merely closed in state), the editing line keeps the
// typed text, and a later candidate result cannot reopen what the user closed.
func TestChatCompletion_EscapeClosesTheMenu(t *testing.T) {
	h := newA7Harness(t, &stubCompleter{})
	h.typeKeys(t, magicOpensTab)
	h.waitForMenu(t)
	menuTop := h.menuTop(t)

	// A lone ESC: the decoder holds it until its bounded wait expires, so the
	// script delivers the expired wait (a real terminal's shape for a
	// standalone Escape key).
	h.typeKeys(t, "\x1b")
	h.src.push(scriptStep{timeout: true, advance: escWaitWindow()})
	h.waitFor(t, "the menu rows to be erased after Escape", func() bool {
		h.replay()
		return !h.ed.MenuState().Open && h.screen.Line(menuTop) == ""
	})

	for i, want := range menuStubResult {
		if got := h.screen.Line(menuTop + i); strings.Contains(got, want.Text) {
			t.Fatalf("row %d still shows %q after Escape — the menu rows must be ERASED, not just closed\n--- screen ---\n%s",
				menuTop+i, want.Text, h.rows())
		}
	}
	if got := h.screen.Line(h.editRow()); !strings.Contains(got, magicOpensTab) {
		t.Fatalf("editing row after Escape = %q, want the typed text %q kept", got, magicOpensTab)
	}
	if m := h.ed.MenuState(); m.Open {
		t.Fatalf("menu still open after Escape: %+v", m)
	}
	// And it STAYS closed: no late worker result may reopen it (the request was
	// cancelled, not merely hidden).
	time.Sleep(a7PollStep * 4) //stylegate:allow-sleep gives a stale worker result time to arrive, so a reopen would be caught
	h.replay()
	if m := h.ed.MenuState(); m.Open {
		t.Fatalf("the menu reopened after Escape: %+v\n--- screen ---\n%s", m, h.rows())
	}
	h.assertNotDone(t)
}

// TestChatCompletion_SlowCompleterNeverBlocksTheLine is the hermes
// ThreadedCompleter rationale asserted end to end: the completer is BLOCKED
// (released only by the test) while the user keeps typing, and every typed
// character still reaches the editing row. A completer on the read/redraw path
// would freeze the line here.
func TestChatCompletion_SlowCompleterNeverBlocksTheLine(t *testing.T) {
	stub := &stubCompleter{block: make(chan struct{})}
	h := newA7Harness(t, stub)
	h.typeKeys(t, "/")
	h.waitFor(t, "the opening / to be drawn while the completer is blocked", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(h.editRow()), "/")
	})
	h.typeKeys(t, "a", "b", "c")
	h.waitFor(t, "the typed characters to be drawn while the completer is blocked", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(h.editRow()), "/abc")
	})
	if m := h.ed.MenuState(); m.Open {
		t.Fatalf("a blocked completer published a menu: %+v", m)
	}

	// Release it: the answer arrives (empty here — no candidate matches "/abc")
	// and nothing about the line changed underneath.
	stub.release()
	h.waitFor(t, "the released completer's answer to be taken", func() bool { return !h.ed.MenuState().Open })
	if got := h.screen.Line(h.editRow()); !strings.Contains(got, "/abc") {
		t.Fatalf("editing row = %q, want the typed /abc preserved across the completer's answer", got)
	}
}

// TestChatCompletion_MenuRepaintsStayBounded is the other half of "off the read
// path": a menu must not turn keystrokes into a repaint storm. For a fixed key
// sequence both the number of REDRAW DECISIONS and the number of write(2)
// chunks are bounded by a small multiple of the keystrokes.
//
// The window is opened on the SETTLED input state (every keystroke of the typed
// line consumed, its answer published and drawn), never on a screen condition:
// the menu for "/m" is already drawn while "ag" is still being delivered, so a
// window opened on "the menu is visible" silently includes the trailing
// keystrokes' own legitimate repaints. That is a harness defect, not a product
// one — and it is exactly the flake this test used to have.
func TestChatCompletion_MenuRepaintsStayBounded(t *testing.T) {
	h := newA7Harness(t, &stubCompleter{})
	h.typeKeys(t, magicOpensTab)
	// The measurement window MUST NOT open on a screen condition alone: the menu
	// for "/m" is already drawn while "ag" is still being delivered, so a window
	// opened there silently includes the trailing keystrokes' own repaints and
	// the budget below would be measuring the harness, not the menu. Settle the
	// input first — every keystroke consumed, its answer published and drawn.
	h.waitForSettledInput(t, magicOpensTab)
	beforeRepaints := h.ed.Repaints() //screen-ok: measures the repaint budget the test's own keys earned — a count read after the settled window, never a wait on a screen
	beforeWrites := h.out.count()

	// Down, Up, then Escape: three keys, each of which may repaint the menu once
	// (plus the erase of the rows it vacates).
	h.typeKeys(t, "\x1b[B", "\x1b[A")
	h.src.push(scriptStep{data: []byte("\x1b")})
	h.src.push(scriptStep{timeout: true, advance: escWaitWindow()})
	h.waitFor(t, "the menu to close", func() bool { return !h.ed.MenuState().Open })
	// The close's own paint (and any publish still in flight for it) must land
	// before the window is measured, or the count below races the worker.
	h.waitForQuietWrites(t)

	// The budget is on REPAINTS, not on write(2) chunks: how a redraw is split
	// across writes is a transport detail (one bufio flush per line vs one per
	// region move) that varies with scheduling, so a chunk-count budget
	// measures the host, not the menu. The decision count is the claim.
	const keys = 3
	if got := h.ed.Repaints() - beforeRepaints; got > a7RepaintsPerKey*keys+a7RepaintsOverhead { //screen-ok: measures that redraw DECISIONS stay under the per-key budget (a storm bound), not that a draw finished
		t.Fatalf("%d repaints for %d keys (budget %d) — a completion repaint storm; the menu must repaint per key, not per poll\n--- screen ---\n%s",
			got, keys, a7RepaintsPerKey*keys+a7RepaintsOverhead, h.rows())
	}
	// And the write stream stays in the same order of magnitude: a repaint
	// budget that a repaint storm could satisfy by writing nothing would prove
	// nothing, so the chunk count is bounded too — loosely, because its exact
	// split per redraw is a transport detail.
	if got := h.out.count() - beforeWrites; got > a7WritesPerKey*keys+a7WritesOverhead {
		t.Fatalf("%d writes for %d keys (loose budget %d) — the menu repainted per poll, not per key\n--- screen ---\n%s",
			got, keys, a7WritesPerKey*keys+a7WritesOverhead, h.rows())
	}
}

// waitForQuietWrites blocks until the write stream has been quiet for
// settleQuietMS — the point after which an in-flight publish's own repaint has
// certainly landed. A stream that never goes quiet fails loudly (bounded by
// a7Wait) instead of hanging.
func (h *a7Harness) waitForQuietWrites(t *testing.T) {
	t.Helper()
	quiet := time.Duration(settleQuietMS) * time.Millisecond
	deadline := time.Now().Add(a7Wait)
	last := time.Now()
	writes := h.out.count()
	for time.Since(last) < quiet {
		if time.Now().After(deadline) {
			t.Fatalf("the write stream never went quiet for %v (still repainting) — no measurement window can be opened", quiet)
		}
		time.Sleep(a7PollStep) //stylegate:allow-sleep waiting out the completion worker's own publish repaint, bounded by a7Wait
		if n := h.out.count(); n != writes {
			writes, last = n, time.Now()
		}
	}
	h.replay()
}

// escWaitWindow is the editor's lone-ESC bound, referenced (never copied) so a
// change to the policy keeps the script in step.
func escWaitWindow() time.Duration { return time.Duration(escWaitMS) * time.Millisecond }

// TestChatCompletion_IdleWaitsDoNotRepaint is the regression for a repaint the
// user cannot see the point of: an idle bounded wait of the read loop (a wait
// that expired with no input and nothing to classify) redrew the editing line
// unconditionally. That is a repaint PER POLL — on a source without a wake pipe
// the bounded cadence is the fallback, so typing bought hundreds of full-line
// redraws, and the flake it caused was a repaint-budget overshoot of exactly
// those extra writes.
//
// The assertion is DIFFERENTIAL and therefore independent of any budget: the
// same input script with three idle waits must cost exactly as much as the same
// script with none. The screen cannot have changed, so nothing may be drawn.
func TestChatCompletion_IdleWaitsDoNotRepaint(t *testing.T) {
	steps := []scriptStep{{data: []byte("a")}}
	baseline := chatWritesForScript(t, steps)

	for i := 0; i < 3; i++ {
		steps = append(steps, scriptStep{timeout: true})
	}
	idle := chatWritesForScript(t, steps)
	if idle != baseline {
		t.Fatalf("%d writes with three idle waits vs %d without — an idle wait repainted the line (%d extra writes); nothing it draws can have changed",
			idle, baseline, idle-baseline)
	}
}

// chatWritesForScript runs one chat-mode editor over a script and reports how
// many chunks it wrote. It is the fixture the differential assertion above is
// built on: the SAME input, with and without idle waits.
func chatWritesForScript(t *testing.T, steps []scriptStep) int {
	t.Helper()
	ed, _, _, out := newScriptedEditor(t, steps, a7Cols, nil)
	// The CHAT constructor: the reserved layout is what puts bounded completion
	// waits on the read path at all (the historic editor has none).
	chat := NewChatEditor(ed.src, out, promptForTest, nil, nil, nil)
	chat.SetClock(ed.now)
	if !chat.EnableRegion() {
		t.Fatalf("EnableRegion(%d rows) = false, want true", a7Rows)
	}
	runScript(t, chat)
	return out.count()
}

// TestChatCompletion_ClosedMenuIsNotRepaintedByAWake is the second half of the
// same defect, on the notification path: after Escape closes the menu, a wake
// (the terminal's notification, or a stale one left behind by the publish that
// was already drawn) must not repaint. A "the menu changed" flag that outlives
// the redraw it asked for is a standing permission for one redundant full
// redraw per wake — invisible to the user, and a repaint storm by count.
func TestChatCompletion_ClosedMenuIsNotRepaintedByAWake(t *testing.T) {
	h := newA7Harness(t, &stubCompleter{})
	h.typeKeys(t, magicOpensTab)
	h.waitForMenu(t)

	h.src.push(scriptStep{data: []byte("\x1b")})
	h.src.push(scriptStep{timeout: true, advance: escWaitWindow()})
	h.waitFor(t, "the menu to close", func() bool { return !h.ed.MenuState().Open })

	// Let the close settle completely (its own repaint is expected), then wake
	// the editor with no new information at all.
	h.waitFor(t, "the closed menu's paint to be erased", func() bool { return h.menuTopRow() < 0 })
	before := h.out.count()
	readsBefore := h.src.timeoutCount()
	h.wake()
	h.waitFor(t, "the wake to be consumed", func() bool { return h.src.timeoutCount() > readsBefore })

	if got := h.out.count(); got != before {
		t.Fatalf("%d writes for a single wake after the menu closed (want 0) — a closed menu's pending repaint was still armed\n--- screen ---\n%s",
			got-before, h.rows())
	}
}

// TestChatCompletion_SlowCompleterStillGetsItsMenu is the regression for the
// defect that made typing "/" show nothing: the editor spent a fixed polling
// budget and then gave up, so a completer that answered AFTER that budget left
// its menu published but UNDRAWN until the next keystroke. Here the completer is
// held past the whole fast-poll phase and released with no further input — the
// menu must still reach the screen.
func TestChatCompletion_SlowCompleterStillGetsItsMenu(t *testing.T) {
	stub := &stubCompleter{block: make(chan struct{})}
	h := newA7Harness(t, stub)
	h.typeKeys(t, magicOpensTab)
	h.waitFor(t, "the typed prefix on the editing row", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(h.editRow()), magicOpensTab)
	})

	// Hold the answer well past any fixed polling budget the editor might have:
	// however it schedules its wakes, the request is still outstanding when the
	// answer finally arrives.
	time.Sleep(a7SlowCompleterHold) //stylegate:allow-sleep the point of this test is that REAL time passes with the request outstanding (a poll would make the hold collapse and stop covering the fixed-budget defect)
	if m := h.ed.MenuState(); m.Open {
		t.Fatalf("the blocked completer published a menu early: %+v", m)
	}
	stub.release()

	// No further keystroke: the released answer must draw the menu by itself.
	h.waitForMenu(t)
	if got := h.screen.Line(h.editRow()); !strings.Contains(got, magicOpensTab) {
		t.Fatalf("editing row = %q, want %q kept while the menu drew", got, magicOpensTab)
	}
}

// TestChatCompletion_EnterTakesTheSelectedCandidate is ENTER's contract on an
// open menu, and it is the operator-reported defect this test was written for.
//
// The `/`-menu could be opened and walked with Up/Down, and pressing Enter sent
// the RAW typed prefix ("/") while the highlighted candidate was discarded — the
// menu looked like a control and behaved like a photograph. Measured on the real
// PTY (termgrid.py, 100x30, master): typing "/", Down, Enter left the editing row
// as `you> /` and submitted nothing.
//
// The contract asserted here: the FIRST Enter takes the selection into the input
// slot (the editing line shows the candidate, the raw prefix is gone) and does
// NOT submit; the SECOND Enter submits what is now on the line. Up/Down are both
// exercised, so the assertion is about the SELECTED candidate and not about
// "whatever was first in the list".
func TestChatCompletion_EnterTakesTheSelectedCandidate(t *testing.T) {
	h := newA7Harness(t, &stubCompleter{})
	h.typeKeys(t, magicOpensTab)
	h.waitForMenu(t)

	h.typeKeys(t, "\x1b[B") // Down: the selection is now the SECOND candidate
	h.waitFor(t, "the selection marker on the second menu row", func() bool {
		h.replay()
		return strings.HasPrefix(h.screen.Line(h.menuTop(t)+1), menuMarkerActive)
	})

	h.typeKeys(t, "\r") // Enter takes the selection
	h.waitFor(t, "the accepted candidate on the editing row", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(h.editRow()), menuStubResult[1].Text)
	})

	got := strings.TrimRight(h.screen.Line(h.editRow()), " ")
	if !strings.HasSuffix(got, menuStubResult[1].Text) {
		t.Fatalf("editing row after Enter = %q, want the SELECTED candidate %q at the end\n--- screen ---\n%s",
			got, menuStubResult[1].Text, h.rows())
	}
	if strings.Contains(got, magicOpensTab+"c") {
		t.Fatalf("editing row = %q, want the typed prefix REPLACED by the candidate, not kept", got)
	}
	if res, ok := h.submitted(); ok {
		t.Fatalf("Enter submitted %q together with taking the selection, want the menu's Enter to only put the choice in the input slot", res)
	}

	h.typeKeys(t, "\r") // the now-unambiguous Enter submits
	select {
	case res := <-h.done:
		if res.Line != menuStubResult[1].Text {
			t.Fatalf("submitted line = %q, want the accepted candidate %q", res.Line, menuStubResult[1].Text)
		}
	case <-time.After(a7Wait):
		t.Fatalf("Enter never submitted the accepted line\n--- screen ---\n%s", h.rows())
	}
}

// submitted reports a line the editor has already finished, without waiting for
// one. It is how a test asks "did this key submit?" as a fact rather than as a
// timeout.
func (h *a7Harness) submitted() (string, bool) {
	select {
	case res := <-h.done:
		return res.Line, true
	default:
		return "", false
	}
}
