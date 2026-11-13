package lineedit

import (
	"strings"
	"testing"
	"time"
)

// note_row_test.go — the CALLER-ROW contract (AnswerRenderer.Note / Editor.Note).
//
// The defect these tests pin: a caller-reported activity (cmd/agent's "⚙ read …"
// tool row) lived ONLY on the status row, which is CLEARED when the turn ends —
// so the transcript kept no record that a tool had ever run, and a reader could
// not tell which commands produced the answer. The reference TUI prints one
// durable row per call and keeps it; Note is the band's way to do the same.
//
// The three properties asserted here, in the order they can break:
//
//  1. The PRODUCER writes nothing. Note is called from the same goroutine that
//     streams the answer into the band, so a write there would interleave its
//     cursor placement with the read loop's repaint — the two-writer defect this
//     package's publish path removes for the status row.
//  2. The READ LOOP commits the row into the BAND (durable, scrolling), under
//     the caller's own style, and it stays there.
//  3. The row never lands ON the answer's in-flight line: the answer's text is
//     promoted into the band first, so the reading order is answer-then-note.

const (
	// noteRowText is the caller row the tests commit: the glyph and shape
	// cmd/agent's own tool line has (termToolLine), short enough for the
	// harness's 48-column band.
	noteRowText = ToolGlyph + " call read /tmp/x"
	// noteRowText2 is a second row, so the tests can prove the queue drains in
	// ORDER rather than keeping only the last row.
	noteRowText2 = ToolGlyph + " call bash make test"
	// noteWait bounds waiting for the read loop to commit a note. Generous: it is
	// a synchronization wait, and a passing run never comes near it.
	noteWait = 3 * time.Second
	// noteStep is the poll interval of that wait.
	noteStep = time.Millisecond
)

// waitContents waits until the replayed screen carries `want`, or fails with the
// grid — the observation, not a guess about scheduling.
func (h *statusHarness) waitContents(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(noteWait)
	for {
		if strings.Contains(h.contents(), want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the band never carried %q within %s\n--- screen ---\n%s", want, noteWait, h.contents())
		}
		//stylegate:allow-sleep bounded poll interval for the editor's change-detecting note band; the loop's deadline owns the failure and it returns on the first hit
		time.Sleep(noteStep)
	}
}

// TestNoteWritesNothingFromTheProducer is the single-writer half: a caller that
// NOTES a row emits ZERO bytes itself. The read loop is stopped first, so the
// only goroutine that could have written is the producer.
func TestNoteWritesNothingFromTheProducer(t *testing.T) {
	h := newStatusHarness(t)
	h.stopAndWait()
	h.drainWrites()

	if !h.ed.Note(noteRowText, BasicPalette.ToolCall) {
		t.Fatal("Editor.Note = false with the reserved layout installed, want true")
	}
	if got := h.out.count(); got != 0 {
		t.Fatalf("noting a row wrote %d chunk(s) from the producer — the turn goroutine is a second writer on the terminal fd (want 0)", got)
	}
	// …and the row is not DROPPED either: it is queued for the read loop. This is
	// what makes the zero above "one owner" rather than "the note vanished".
	if !h.answer.Noting() {
		t.Fatal("the queued row left nothing pending — the read loop would never commit it")
	}
}

// TestNoteIsCommittedIntoTheBandByTheReadLoop is the other half: the read loop
// commits the row into the BAND — a durable row of the answer surface, not a
// repaint of the chrome — with no keystroke to trigger it.
func TestNoteIsCommittedIntoTheBandByTheReadLoop(t *testing.T) {
	h := newStatusHarness(t)
	h.ed.SetPalette(BasicPalette)

	before := h.answer.Committed()
	h.ed.Note(noteRowText, BasicPalette.ToolCall)
	h.waitContents(t, noteRowText)

	if got := h.answer.Committed(); got != before+1 {
		t.Fatalf("committed rows = %d after one note, want %d — the note did not take the band's own one-scroll-per-row path",
			got, before+1)
	}
	// The status row is BELOW the band and must be untouched by a band write (the
	// row is the chrome, and the note is transcript).
	if last := h.lastRow(); strings.Contains(last, noteRowText) {
		t.Fatalf("the note landed on the status row (%q) — it belongs in the band", last)
	}
	// A SECOND note drains in order, and the first one is still on screen: the
	// band is a transcript, not a single mutable slot.
	h.ed.Note(noteRowText2, BasicPalette.ToolCall)
	h.waitContents(t, noteRowText2)
	grid := h.contents()
	if i, j := strings.Index(grid, noteRowText), strings.Index(grid, noteRowText2); i < 0 || j < 0 || i > j {
		t.Fatalf("the two notes are not both in the band in order (first=%d second=%d):\n%s", i, j, grid)
	}
}

// TestNoteCarriesTheCallersStyle is the emphasis half: the row reaches the
// screen under the style the CALLER passed (cmd/agent passes the palette's
// ToolCall), and a caller that passes the zero style commits the same TEXT with
// no sequence — the colour-off invariant, asserted on the bytes.
func TestNoteCarriesTheCallersStyle(t *testing.T) {
	h := newStatusHarness(t)
	h.ed.SetPalette(BasicPalette)

	h.ed.Note(noteRowText, BasicPalette.ToolCall)
	h.waitContents(t, noteRowText)
	h.replay()

	var b strings.Builder
	for _, w := range h.out.snapshot() {
		b.Write(w)
	}
	stream := b.String()
	if !strings.Contains(stream, BasicPalette.ToolCall.Wrap(noteRowText)) {
		t.Fatalf("the committed row is not wrapped in the caller's style %q:\n%q", BasicPalette.ToolCall.Open(), stream)
	}
	// The text is there ONCE: a row that the commit wrote twice would be the
	// duplicated-head defect wearing a note's clothes.
	if got := strings.Count(stripSGR(stream), noteRowText); got != 1 {
		t.Fatalf("the row's text appears %d time(s) in the paint stream, want 1:\n%q", got, stripSGR(stream))
	}
}

// TestNoteWithoutLayoutCommitsNothing is the degradation contract: with no
// reserved layout (the cooked path, or a terminal too small for the band) a note
// emits NOTHING — the cooked path's byte stream IS its contract, and cmd/agent
// keeps writing the historic flow bytes there (WriteToolRow reports false).
func TestNoteWithoutLayoutCommitsNothing(t *testing.T) {
	out := &recordingWriter{}
	reg := newRegion(out)
	r := NewAnswerRenderer(reg, nil)
	r.SetCols(statusTestCols)

	r.Note(noteRowText, BasicPalette.ToolCall)
	if r.Noting() {
		t.Fatal("a note was queued on a renderer with no active region — the cooked path would then commit a row the historic stream never had")
	}
	if r.drainNotes() {
		t.Fatal("drainNotes committed a row with no active region")
	}
	if got := out.count(); got != 0 {
		t.Fatalf("a note with no layout wrote %d chunk(s), want 0", got)
	}
}

// TestEditorNoteWithoutLayoutReportsFalse pins the CALLER's side of the same
// contract: Editor.Note reports false so cmd/agent can keep the historic bytes
// rather than believe a row was drawn (Rule 15 — never a silent drop).
func TestEditorNoteWithoutLayoutReportsFalse(t *testing.T) {
	out := &recordingWriter{}
	src := &scriptedSource{width: statusTestCols, height: statusTestRows, clock: &fakeClock{t: time.Unix(a5UnixTime, 0)}}
	ed := NewChatEditor(src, out, promptForTest, nil, nil, nil)

	if ed.Note(noteRowText, BasicPalette.ToolCall) {
		t.Fatal("Editor.Note = true with no reserved layout, want false")
	}
	if got := out.count(); got != 0 {
		t.Fatalf("a note with no layout wrote %d chunk(s), want 0", got)
	}
}

// TestNotePromotesTheInFlightAnswerRowFirst is the ordering contract: a note
// arrives while the answer is mid-line, and the answer's in-flight text must be
// committed ABOVE the note — not overwritten by it, and not dropped. The grid
// shows both, in the order the reader reads them.
func TestNotePromotesTheInFlightAnswerRowFirst(t *testing.T) {
	h := newStatusHarness(t)
	h.ed.SetPalette(BasicPalette)

	// An answer line with NO terminator: it lives on the live row, uncommitted.
	const inFlight = "the answer is still being written"
	h.answer.Begin()
	h.answer.Delta(inFlight)

	h.ed.Note(noteRowText, BasicPalette.ToolCall)
	h.waitContents(t, noteRowText)

	grid := h.contents()
	ai, ni := strings.Index(grid, inFlight), strings.Index(grid, noteRowText)
	if ai < 0 {
		t.Fatalf("the in-flight answer line was DROPPED when the note was committed:\n%s", grid)
	}
	if ni < 0 {
		t.Fatalf("the note never reached the band:\n%s", grid)
	}
	if ai > ni {
		t.Fatalf("the note landed ABOVE the answer text it interrupted — the reading order is wrong:\n%s", grid)
	}
	// The answer continues BELOW the note (the live row is fresh again).
	h.answer.Delta("\ncontinued after the note\n")
	h.answer.Finish()
	h.waitContents(t, "continued after the note")
	grid = h.contents()
	if i, j := strings.Index(grid, noteRowText), strings.Index(grid, "continued after the note"); i < 0 || j < 0 || i > j {
		t.Fatalf("the answer's continuation is not below the note (note=%d cont=%d):\n%s", i, j, grid)
	}
}

// TestNoteRowCarriesNoTurnGutter is the alignment contract: a note row is a CALLER
// row with its own glyph at column 0, so it must not be indented by the turn's
// two-cell marker column — while the answer's own lines inside the same turn keep
// that column. Two left edges would read as two blocks; one glyph column is the
// reference TUI's own shape.
func TestNoteRowCarriesNoTurnGutter(t *testing.T) {
	h := newStatusHarness(t)
	h.ed.SetPalette(BasicPalette)

	h.answer.BeginTurn(TurnInfo{Index: 1, Prompt: promptForTest})
	h.answer.Delta("an answer line of the turn\n")
	h.ed.Note(noteRowText, BasicPalette.ToolCall)
	h.waitContents(t, noteRowText)
	h.replay()

	noteRow := gridRowContaining(h.screen, noteRowText)
	answerRow := gridRowContaining(h.screen, "an answer line of the turn")
	if noteRow < 0 || answerRow < 0 {
		t.Fatalf("both rows must be on the grid (note=%d answer=%d):\n%s", noteRow, answerRow, h.contents())
	}
	if got := h.screen.Line(noteRow); !strings.HasPrefix(got, ToolGlyph) {
		t.Fatalf("the note row = %q, want it to START with its own glyph %q at column 0 (no turn gutter)", got, ToolGlyph)
	}
	if got := h.screen.Line(answerRow); !strings.HasPrefix(got, TurnMarker) {
		t.Fatalf("the answer row = %q, want the turn's own marker column %q", got, TurnMarker)
	}
}

// gridRowContaining returns the index of the grid row carrying want, or -1.
func gridRowContaining(s *Screen, want string) int {
	for i := 0; i < s.Rows; i++ {
		if strings.Contains(s.Line(i), want) {
			return i
		}
	}
	return -1
}
