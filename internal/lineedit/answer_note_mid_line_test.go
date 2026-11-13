package lineedit

// answer_note_mid_line_test.go — the SECOND trigger hypothesis for the
// operator's partial repeat, MEASURED AND DISPROVEN, and kept because a
// property that holds by accident today is the one that breaks tomorrow.
//
// The guess (this task's own `## Not Done`, carried from the first half of the
// investigation): besides the layout shift fixed in answer.go, the other way a
// line's beginning could appear twice is a NOTE row — a tool call, a notice —
// landing while the answer line is still in flight. The note path does carry the
// in-flight text into the prefix before it paints (drainNotes), so a wrong carry
// there would show the same symptom by a different route.
//
// It does not. At every injection point the committed reading is the transcript
// PLUS the note's own row and nothing else: the note is inserted between the two
// rows the answer's line occupies, and not one answer cell is repeated or lost.
//
// WHY IT IS STILL A TEST AND NOT A SENTENCE IN A REPORT. The note path is the
// one that has already been wrong once in the other direction (a queue of paste
// sources must skip the prefix carry entirely — measured 2026-10-22, a `/info`
// block opening one row too low). A guard that pins the current, correct
// behaviour is what makes the next change to that carry visible.
//
// THE DRAIN IS BY HAND, DELIBERATELY. `Note` routes through the queue when the
// renderer has a loop to route to; this rig has none, and a note nobody drains
// would make every case below pass while measuring NOTHING (that false negative
// was hit while writing this file). So each case drains explicitly and FAILS if
// the drain reports it moved nothing.

import (
	"testing"
	"time"
)

// answerNoteMidLineText is the note row injected mid-answer. Its cells are the
// only ones the reading should gain over the transcript.
const answerNoteMidLineText = "· tool: bash OK"

// TestAnswerNoteMidLineNeitherRepeatsNorLosesTheLine streams the transcript and
// injects a note at every interesting position inside the line, then asserts the
// band's committed reading is EXACTLY the transcript plus the note's row.
func TestAnswerNoteMidLineNeitherRepeatsNorLosesTheLine(t *testing.T) {
	want := repeatDense(repeatTranscript()) + repeatDense(answerNoteMidLineText)
	judged := 0
	for _, at := range []int{0, 40, 100, 144, 200, 300} {
		sink := newFrameSink(repeatCols, repeatRows)
		reg := newTestRegion(sink, repeatRows, repeatCols)
		if !reg.Enable(repeatRows, repeatCols) {
			t.Fatalf("region.Enable(%d, %d) = false, want the reserved chat layout installed", repeatRows, repeatCols)
		}
		r := NewAnswerRenderer(reg, nil)
		r.SetCols(reg.Cols())
		now := time.Unix(emphasisChunkEpoch, 0)
		r.SetClock(func() time.Time { return now })
		r.Begin()
		// The editing line already gave its row back: this case asks about the
		// NOTE injection alone, so the layout must not move underneath it.
		reg.SetEditorRows(repeatEditorRowsAfter)

		text := repeatTranscript()
		for i := 0; i < len(text); i++ {
			if i == at {
				r.Note(answerNoteMidLineText, Style{})
				if !r.drainNotes() {
					t.Fatalf("at %d: the note never reached the band — the queue was not drained, so this case would be measuring nothing", at)
				}
			}
			now = now.Add(answerFlushWindow * 2)
			r.Delta(text[i : i+1])
		}
		now = now.Add(answerFlushWindow * 2)
		r.Tick()
		r.Finish()

		shown := repeatFlatten(repeatReading(sink, reg))
		if got := repeatRepeated(shown, repeatRunCells); got != "" {
			t.Fatalf("at %d: a note landing mid-line made a %d-cell run appear twice:\n  repeat: %q\n  grid:\n%s",
				at, repeatRunCells, got, repeatGrid(sink.grid()))
		}
		if len(shown) != len(want) {
			t.Fatalf("at %d: the reading is %d cells, want %d (the answer plus the note's own row) — a note landing mid-line lost or duplicated answer cells:\n  read %q\n  want %q\n  grid:\n%s",
				at, len(shown), len(want), shown, want, repeatGrid(sink.grid()))
		}
		judged++
	}
	if judged != 6 {
		t.Fatalf("judged %d injection points, want 6 — a guard that looked at nothing must not pass", judged)
	}
}
