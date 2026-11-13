package lineedit

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

// degradation_width_test.go — the WIDTH contract of the diagnostic channel.
//
// THE DEFECT IT GUARDS (measured 2026-11-10). The editor's diagnostics are
// written to errOut, which is stderr — the SAME terminal the reserved layout is
// drawing on, but not the layout's writer. So a sentence that does not fit the
// width was not merely ugly, it was a WRITE OVER THE LAYOUT: the suppressed-paste
// warning composed ~300 cells, which at 101 columns is three rows, and those rows
// went down from the cursor's row and covered the status row (the layout's last
// row) until the operator's next keystroke repainted it.
//
// The two assertions this file makes are the two halves of the fix, and neither
// is an assertion about the MESSAGE STRING alone:
//
//   - every row the editor writes to errOut occupies at most the layout's width,
//     and the layout-installed path spends at most the one row it can spare
//     (statusRows — the only row outside the band and the editing line);
//   - the status row READS THE SAME after the diagnostic as before it, on the
//     screen grid, because the write is followed by a repaint rather than by the
//     operator's next keystroke.
//
// The diagnostic bytes are fed to the SAME screen as the paint stream, in order,
// which is what makes the second assertion real: in production errOut and the
// layout's writer reach one terminal.

// degradationCols is the terminal width the defect was measured at (101 columns
// — the operator's console when the three-row warning covered the status row).
const degradationCols = 101

// degradationStatusRow is what the status source below publishes: a label and a
// live hint, both fixed, so the row's content is a fact the test can compare
// across the diagnostic.
const (
	degradationStatusLabel = "model sonnet-4.5 · iter 1 · in 1.2k out 340"
	degradationStatusLive  = "idle"
)

// degradationHarness is the chat layout at a fixed width, with the editor's
// diagnostics CAPTURED and simultaneously fed to the terminal model — the shape
// production has, where errOut is the same terminal as the layout's writer.
type degradationHarness struct {
	ed     *Editor
	out    *recordingWriter
	warns  *bytes.Buffer
	screen *Screen
	clock  *fakeClock
	fed    int
}

// diagnosticWriter tees one destination into the paint stream: the SAME bytes go
// to the terminal model (so the screen sees what the operator would see) and to
// the test's own buffer (so the diagnostic's rows can be measured).
type diagnosticWriter struct {
	inner *recordingWriter
	own   *bytes.Buffer
	mu    *sync.Mutex
}

func (w diagnosticWriter) Write(p []byte) (int, error) {
	if _, err := w.inner.Write(p); err != nil {
		return 0, err
	}
	w.mu.Lock()
	w.own.Write(p)
	w.mu.Unlock()
	return len(p), nil
}

// newDegradationHarness builds the layout at 101 columns and installs a status
// source, so the row the diagnostic covers is a row with known content.
func newDegradationHarness(t *testing.T) *degradationHarness {
	t.Helper()
	clock := &fakeClock{t: time.Unix(editorTestEpoch, 0)}
	src := &scriptedSource{width: degradationCols, height: a5Rows, clock: clock}
	out := &recordingWriter{}
	warns := &bytes.Buffer{}
	ed := NewChatEditor(src, out, promptForTest, nil, nil, nil)
	ed.SetClock(clock.Now)
	// The diagnostics go to the terminal AND to the record under test. errOut is
	// wired AFTER construction because NewChatEditor's writer is the paint
	// stream and the two are different streams in production too (stderr vs the
	// layout's writer) — the harness is what makes them one terminal.
	ed.errOut = diagnosticWriter{inner: out, own: warns, mu: &sync.Mutex{}}
	if !ed.EnableRegion() {
		t.Fatalf("EnableRegion(%d rows) = false, want true", a5Rows)
	}
	ed.SetStatus(func() Status {
		return Status{Label: degradationStatusLabel, Live: degradationStatusLive}
	})
	// THE READ LOOP'S IDENTITY, stood in for by the test: the warn's repaint
	// runs on the loop and only there (repaintAfterDegradation), and this test
	// drives the editor from one goroutine exactly as the loop does.
	ed.noteLoopGoroutine()
	t.Cleanup(func() { ed.loopGoid.Store(0) })
	h := &degradationHarness{ed: ed, out: out, warns: warns, clock: clock,
		screen: NewScreen(degradationCols, a5Rows)}
	return h
}

// replay feeds every recorded chunk the screen has not seen yet, in order. It is
// incremental for the reason a5Harness.replay is: the grid is stateful and
// cursor motion is relative, so re-feeding would repaint rather than reproduce.
func (h *degradationHarness) replay() {
	chunks := h.out.snapshot()
	for _, w := range chunks[h.fed:] {
		h.screen.Feed(w)
	}
	h.fed = len(chunks)
}

// draw paints the layout once and settles the screen on it, so the caller has a
// "before" reading of every row.
func (h *degradationHarness) draw(t *testing.T) {
	t.Helper()
	h.ed.PublishStatus()
	h.ed.repaint()
	h.replay()
}

// testTriggerPasteSuppression drives ONE suppressed Enter through the editor's
// REAL decode path (a size-armed read, then an Enter inside the window), which
// is the production road warnPasteSuppressed is reached by.
func (h *degradationHarness) triggerPasteSuppression(t *testing.T) {
	t.Helper()
	h.ed.applyRead([]byte("abc"))
	h.clock.advance(PasteBurstCharInterval)
	if _, done, err := h.ed.handleKey(Key{Code: KeyEnter, Origin: ReadOriginTyped}); err != nil || done {
		t.Fatalf("the Enter inside the size-armed window submitted the line: done=%v err=%v — nothing was suppressed, so nothing was announced", done, err)
	}
	h.replay()
}

// diagnosticRows returns the rows the editor wrote to errOut, split on the line
// breaks it emitted (CRLF while the layout is installed, LF otherwise), with the
// trailing empty row dropped.
func diagnosticRows(raw string) []string {
	rows := strings.Split(strings.ReplaceAll(raw, escCarriage+escNewline, escNewline), escNewline)
	for len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	return rows
}

// TestDegradationFoldsToTheLayoutWidth is assertion (a).
//
// RED (pre-fix): warnPasteSuppressed composed one ~300-cell sentence and wrote it
// verbatim, which at 101 columns is three rows on a fixed-height screen.
// GREEN: the sentence is folded by the package's own wrap policy and spends at
// most the one row the layout can spare — and the RECORD still receives the whole
// sentence, because a diagnostic that is truncated where it is grepped is the
// silent failure Rule 15 forbids.
func TestDegradationFoldsToTheLayoutWidth(t *testing.T) {
	h := newDegradationHarness(t)
	h.draw(t)

	var record bytes.Buffer
	h.ed.SetDiagSink(func(s string) { record.WriteString(s) })

	h.triggerPasteSuppression(t)

	raw := h.warns.String()
	if raw == "" {
		t.Fatal("a suppressed Enter wrote no diagnostic at all — Rule 15's silent failure is back")
	}
	rows := diagnosticRows(raw)
	if len(rows) > statusRows {
		t.Fatalf("the diagnostic spent %d row(s) at %d columns, want at most %d — every row past the first is a row the layout's chrome is printed on:\n%s",
			len(rows), degradationCols, statusRows, strings.Join(rows, "\n"))
	}
	for i, row := range rows {
		if w := displayWidth(row); w > degradationCols {
			t.Errorf("row %d is %d display columns wide, want <= %d:\n%q", i, w, degradationCols, row)
		}
		if strings.Contains(row, escCarriage) && i > 0 {
			t.Errorf("row %d carries a bare carriage return instead of a line break:\n%q", i, row)
		}
	}
	// THE ONE-ROW FORM IS THE POINT, not an accident of this particular sentence:
	// the short message must FIT, otherwise the cap above is what is keeping the
	// row count down and the operator reads a diagnostic that stops mid-word.
	if len(rows) != 1 {
		t.Errorf("the paste diagnostic did not fit one %d-column row (it took %d) — shorten the sentence rather than relying on the row cap:\n%s",
			degradationCols, len(rows), strings.Join(rows, "\n"))
	}

	// RULE 15: the record is the whole sentence, untouched by the fold.
	got := record.String()
	if got == "" {
		t.Fatal("the diagnostic reached the screen but not the record")
	}
	if strings.Contains(got, escCarriage) {
		t.Errorf("the record inherited the screen's carriage returns:\n%q", got)
	}
	if !strings.Contains(got, "suppressed") {
		t.Errorf("the record does not carry the suppression count, so a reader cannot tell a burst from a stray Enter:\n%q", got)
	}
}

// TestDegradationRestoresTheStatusRow is assertion (b).
//
// The status row is the layout's LAST row and lives outside the band's scrolling
// region, which is why it is the row a diagnostic lands on. The editor used to
// recover on the operator's next keystroke; it now repaints the row itself, and
// this asserts the READING — the grid's row content — is the same before and
// after, on the same screen, with the diagnostic's own bytes fed through it.
func TestDegradationRestoresTheStatusRow(t *testing.T) {
	h := newDegradationHarness(t)
	layout, ok := h.ed.Layout()
	if !ok {
		t.Fatal("the layout is not installed, so there is no status row to defend")
	}
	h.draw(t)

	before := h.screen.Line(layout.StatusRow)
	if want := strings.TrimSpace(statusLine(Status{Label: degradationStatusLabel, Live: degradationStatusLive}, degradationCols)); !strings.Contains(before, strings.TrimSpace(want)) {
		t.Fatalf("the status row does not hold the published status before the diagnostic, so this test could not detect a loss:\n row %d: %q\n   want: %q",
			layout.StatusRow, before, want)
	}

	h.triggerPasteSuppression(t)

	after := h.screen.Line(layout.StatusRow)
	if after != before {
		t.Fatalf("the status row changed across the diagnostic — it was not repainted:\nbefore: %q\n after: %q", before, after)
	}
	// AND THE EDITING LINE IS ON THE ROW THE LAYOUT GIVES IT, carrying the
	// prompt and nothing else: a diagnostic still printed through that row would
	// leave the sentence's tail on it. The layout is re-read because the row it
	// gives the editing line is a property of the CURRENT frame.
	now, ok := h.ed.Layout()
	if !ok {
		t.Fatal("the layout was taken down by the diagnostic")
	}
	if got := h.screen.Line(now.EditorTop); !strings.Contains(got, promptForTest) {
		t.Fatalf("the editing line's row does not hold the prompt after the diagnostic:\n row %d: %q\n   want it to contain %q",
			now.EditorTop, got, promptForTest)
	}
	if got, diag := h.screen.Line(now.EditorTop), strings.TrimSpace(diagnosticRows(h.warns.String())[0]); diag != "" && strings.Contains(got, diag) {
		t.Fatalf("the diagnostic is still printed through the editing line's row %d: %q", now.EditorTop, got)
	}
}
