package main

// chat_signal_notice_test.go — the SIGNAL CLOSEOUT NOTICE must not tear the
// reserved layout.
//
// The defect (measured live, chat c5f1189a-ce34-44f2-a40c-74ebcb148fc7, 117x26,
// 2026-09-29). The run took a SIGTERM, and the closeout notice was written with a
// bare `fmt.Fprintf(os.Stderr, …)` while the reserved layout had the terminal
// inside the scroll region with the cursor parked ON THE STATUS ROW. stderr is
// the SAME descriptor the editor paints on (newChatInputWithPalette passes
// os.Stderr as both the paint writer and errOut), so a flowed write landed on the
// row instead of after it. The dumped grid still holds the result — cells 0..57
// of the status row:
//
//	deeps[gopi] terminated received — finalizing chat session…hit 97% ·↑8M↓1M … · ready
//
// i.e. five surviving cells of the row's own text, the whole notice, and then the
// row's own tail from column 58. This is the same tear class that chat_flow_seam.go
// and Editor.WriteLines were built to remove — `TestChatFlow_AgentOutputTakesWholeBandRows`
// states it as a row-level property — and this call site was the one that fix
// missed.
//
// WHY THE HARNESS BINDS stderr AND the paint writer TO THE SAME OBJECT: that IS
// the production topology, and a test that split them would exercise a seam no
// run has — the tear only exists because one descriptor carries two writers. The
// swap is why the assertion is on the rendered CELL GRID (Rule 10.2) and not on
// "the notice was written": with the defect in place, the notice bytes ARE
// written, and the damage is visible only in the grid.
//
// RED PROOF: restore the bare Fprintf at the call site (make
// chatSignalCloseoutNotice print to os.Stderr instead of routing through
// WriteNotice) and the status-row assertion below fires, quoting the merged row —
// while a test that asserted only "the notice reached stderr" stays green.

import (
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/lineedit"
)

const (
	// signalNoticeCols / Rows reproduce the MEASURED geometry of the live run
	// this defect came from (117x26), so the grid this test reads is the grid the
	// operator read.
	signalNoticeCols = 117
	signalNoticeRows = 26
	// signalNoticeFragment is the part of the closeout sentence this test looks
	// for on the grid: the format's own newlines are not on a row, so the row is
	// matched on the sentence.
	signalNoticeFragment = "received — finalizing chat session"
	// signalNoticeMarker is the HEAD of the sentence — the fragment a TORN write
	// still leaves somewhere on the grid, so the wait for it cannot turn the tear
	// into a timeout (a timeout reports the wrong property).
	signalNoticeMarker = "[gopi] terminated receive"
)

// signalNoticeHarness builds the run's own writer topology: ONE descriptor
// carries BOTH the editor's paint stream and the closeout notice. That is not a
// convenience — it is the production shape (newChatInputWithPalette hands
// os.Stderr in as the paint writer AND keeps it as errOut), and it is the only
// reason the wrong route tears anything: two writers, one descriptor.
//
// The shared descriptor is a pipe; a reader goroutine copies its bytes into the
// recorder the grid is replayed from, so both writers' output lands in the SAME
// byte stream in the order the terminal saw it.
func signalNoticeHarness(t *testing.T) *streamHarness {
	t.Helper()
	clock := &testClock{t: time.Unix(chatTestEpoch, 0)}
	src := &scriptSource{width: signalNoticeCols, height: signalNoticeRows, clock: clock, holdAtEnd: true}
	rec := &recordingWriter{}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	// os.Stderr is read at construction time by newChatInputWithSource (it becomes
	// both the editor's paint writer and the input's errOut), so the swap has to
	// precede it. testutil.SnapshotEnv covers ENV vars; this is the *os.File
	// global, hence the explicit restore pair.
	prev := os.Stderr
	os.Stderr = w
	copied := make(chan struct{})
	go func() {
		defer close(copied)
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				if _, werr := rec.Write(buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	in := newChatInputWithSource(src, w, chatPrompt)
	in.editor.SetClock(clock.Now)
	if !in.editor.EnableRegion() {
		t.Fatalf("EnableRegion(%d rows) = false, want true (the layout's floor is %d)",
			signalNoticeRows, lineedit.RowsMinUsable())
	}
	status := newChatStatusSource(flowBandModel, false)
	in.SetStatus(status.status)
	h := &streamHarness{
		in:     in,
		out:    rec,
		screen: lineedit.NewScreen(signalNoticeCols, signalNoticeRows),
		src:    src,
		clock:  clock,
		done:   make(chan struct{}),
	}
	t.Cleanup(func() {
		// The join BEFORE Close, and reported when it fails: this arm already had
		// the sequence but a timed-out wait fell through to Close silently, so a
		// parked loop reached the close-out sweep with nothing in the run's log
		// tying it to this harness. See streamHarness.stopReadLoopOrFail.
		h.stopReadLoopOrFail(t)
		in.Close()
		os.Stderr = prev
		// Closing the last write end ends the copy goroutine (both halves of the
		// file are dropped only after it stopped, or the descriptor leaks).
		//stylegate:ignore teardown of the harness's own pipe: a failure here leaves no state a later test reads
		_ = w.Close()
		select {
		case <-copied:
		case <-time.After(completionTimeout):
		}
		//stylegate:ignore teardown of the harness's own pipe: see above
		_ = r.Close()
	})
	// The editor reads in the background — the REPL's shape, and the reason a
	// queued band frame is drained at all. The loop is started here and joined by
	// the cleanup above (both halves of one contract).
	go func() {
		defer close(h.done)
		for {
			_, ok, _, _ := in.ReadLine() //stylegate:ignore the harness's own teardown: the grid is the assertion, and a real failure surfaces as a waitFor timeout that dumps it
			if !ok {
				return
			}
		}
	}()
	return h
}

// TestChatSignalNotice_CloseoutLandsWithoutTearingTheStatusRow drives the signal
// closeout through the production entry point and asserts the rendered grid: the
// status row keeps ONLY its own text, and the notice is whole on a band row of
// its own.
func TestChatSignalNotice_CloseoutLandsWithoutTearingTheStatusRow(t *testing.T) {
	h := signalNoticeHarness(t)
	// The status row must be DRAWN before it can be asserted to survive: this is
	// the publish the REPL issues at a turn boundary, and the wait is on the row
	// itself (never a draw counter, which any earlier paint satisfies).
	h.in.PublishStatus()
	h.waitFor(t, "the published model on the status row", func() bool {
		h.replay()
		return strings.Contains(h.screen.Line(h.layout().StatusRow), flowBandModel)
	})

	// The signal arrives while the layout owns the screen — the live shape.
	chatSignalCloseoutNotice(h.in, syscall.SIGTERM)

	// The wait is on the marker BOTH shapes leave (the whole sentence, or its
	// first row when the write tore): waiting on the whole sentence would report
	// the tear as a timeout instead of as the property that broke.
	h.waitFor(t, "the closeout notice on the grid", func() bool {
		h.replay()
		return strings.Contains(h.screen.Contents(), signalNoticeMarker)
	})
	h.replay()

	l := h.layout()
	statusRow := h.screen.Line(l.StatusRow)

	// THE DEFECT, stated as the property it broke. Two halves, because a bare
	// Fprintf on the shared descriptor breaks two things and only the pair
	// identifies it:
	//
	//   (a) the write lands wherever the cursor was parked — the live run read
	//       `deeps[gopi] terminated receive` ON the status row, with the sentence
	//       spilling to the row below. So no chrome row may carry the notice.
	//   (b) the write carries its own newline and scrolls the screen, so the chrome
	//       MOVES out from under the layout's own indices. Measured pre-fix on this
	//       harness: the layout still planned the editing line at row 24 and the
	//       status row at 25, while the screen had scrolled them to 21 and 22 — row
	//       25 read empty. Post-fix the chrome is exactly where the layout says.
	for row := l.EditorTop; row < h.screen.Rows; row++ {
		if got := h.screen.Line(row); strings.Contains(got, signalNoticeMarker) {
			t.Fatalf("the closeout notice landed on the layout's CHROME (row %d, the editing-line-down block) instead of through the layout's writer:\n"+
				"  row %d: %q\n--- screen ---\n%s", row, row, got, h.screen.Contents())
		}
	}
	if !strings.Contains(statusRow, flowBandModel) {
		t.Fatalf("the chrome is no longer where the layout says it is: the layout plans the status row at %d, but that row reads %q (want the model %q).\n"+
			"A bare stderr write scrolls the screen and desynchronises it from the layout — the closeout must go through the layout's writer:\n--- screen ---\n%s",
			l.StatusRow, statusRow, flowBandModel, h.screen.Contents())
	}

	// And the notice is not merely absent — it is ON the grid, whole, on a row
	// that is not the chrome (Rule 15: the closeout must not be silently dropped
	// to fix the tear).
	top, bottom, band := panelBand(t, h)
	if !strings.Contains(band, signalNoticeFragment) {
		t.Fatalf("the closeout notice is not on the band [%d,%d] — the tear was fixed by LOSING the notice:\n--- band ---\n%s\n--- screen ---\n%s",
			top, bottom, band, h.screen.Contents())
	}
	// The notice rides whole rows: a sentence split across two band rows would be
	// the wrap-tear this asserts against.
	if bandRowOf(strings.Split(band, "\n"), signalNoticeFragment) < 0 {
		t.Fatalf("the closeout notice is split across band rows (no single row carries it whole):\n--- band ---\n%s", band)
	}

	t.Logf("grid after the signal closeout (editing row %d, status row %d):\n%s",
		l.EditorTop, l.StatusRow, h.screen.Contents())
}

// TestChatSignalNotice_KeepsTheHistoricStderrBytesWithoutALayout pins the
// fallback: a run with no reserved layout on screen (a cooked terminal, a pipe,
// --no-editor) keeps the byte-identical stderr write, so the testsuite's
// "finalizing" greps and a piped run are unaffected.
func TestChatSignalNotice_KeepsTheHistoricStderrBytesWithoutALayout(t *testing.T) {
	// A chatInput with no editor at all: the pre-layout window and the cooked path
	// share this shape.
	var fallback strings.Builder
	in := &chatInput{errOut: &fallback}
	chatSignalCloseoutNotice(in, syscall.SIGTERM)
	// The leading newline is part of the historic bytes: it is what took the
	// cursor off the row it was parked on, and it is preserved verbatim.
	want := "\n[gopi] terminated received — finalizing chat session…\n"
	if got := fallback.String(); got != want {
		t.Fatalf("the closeout wrote %q, want %q (the historic stderr bytes)", got, want)
	}

	// A NIL input is legal — the signal can arrive before the input layer exists —
	// and must still take the historic path rather than swallowing the notice.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	prev := os.Stderr
	os.Stderr = w
	chatSignalCloseoutNotice(nil, syscall.SIGTERM)
	if err := w.Close(); err != nil {
		t.Fatalf("close write end: %v", err)
	}
	os.Stderr = prev
	buf := make([]byte, 256)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}
	if got := string(buf[:n]); got != want {
		t.Fatalf("a nil input wrote %q, want %q (the notice must not be dropped)", got, want)
	}
}
