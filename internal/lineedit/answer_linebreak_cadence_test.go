package lineedit

// answer_linebreak_cadence_test.go — the LIVE band loses the answer's LINE
// BREAKS when the deltas arrive faster than the flush window.
//
// FIXED (task 01a0e706, 2026-09-28) — the gate is REMOVED: drain commits the row
// whose line break arrived alone in its frame, so both cadences below draw the
// answer's 3 rows on the tree as it stands.
//
// MEASURED ON A REAL PTY FIRST (testsuite/chat_render_redzone_pty_test.go, task
// 01a0e44e, 2026-09-28, 64x40, mock SSE model). The scripted answer was five
// lines; the same payload was streamed twice at two cadences:
//
//	delta=1 byte per fragment, 15ms between them  → ONE band row block holding
//	  `▌ <RZ-OPENRZ-NOWRAP one plain row that ends short.RZ-WRAP HEAD t…` and a
//	  second row `▌ <RZ-CLOSE<RZ-END>` — five lines flowed into one paragraph and
//	  two markers GLUED into one row;
//	delta=4 bytes per fragment, 10ms between them  → five rows, one per line,
//	  `▌ <RZ-OPEN` / `▌ RZ-NOWRAP …` / … — every break intact.
//
// The difference between the two runs is WHEN the frame holding the break
// arrives: at 4 bytes per frame a frame carrying `\n` follows its neighbour
// within the 30ms flush window, while at 1 byte per frame the stream reaches a
// `\n` frame only after 30ms have already elapsed — the flush window fires
// first, paints the in-flight text (drain's no-break path), and the break is
// gone.
//
// THE DEFECT IS THE PRODUCT'S, NOT THE HARNESS'S, which is why this file exists:
// a reader whose model streams slowly sees the answer's structure, and the same
// answer from a model that streams fast is shown as one undivided block.
//
// The test is the SMALLEST sample that separates them (Rule 10.4): three
// three-character lines at 64 columns — no wrapping, no markup, no emphasis, no
// wide runes, so nothing but the cadence can decide the row count.

import (
	"testing"
	"time"
)

// cadenceTestGeometry is the narrow pty the real measurement used.
const (
	cadenceTestCols = 64
	cadenceTestRows = 24
)

// cadenceTestText is the minimal payload: three lines, each far shorter than the
// band, so every line owns exactly one row when its break survives.
func cadenceTestText() string { return "AAA\nBBB\nCCC\n" }

// cadenceCase is one measured cadence.
type cadenceCase struct {
	// Name is what the failure reports.
	Name string
	// DeltaBytes is the fragment width the stream is cut into.
	DeltaBytes int
	// Advance is how much the test clock moves between two deltas — i.e. how the
	// stream's rate compares with the renderer's flush window. Its two regimes
	// are what the measurement separated:
	//
	//	Advance <  answerFlushWindow → deltas ACCUMULATE, so a frame carries a
	//	  line's text together with its break (the break rides with its line);
	//	Advance >= answerFlushWindow → every delta is flushed on its own, so a
	//	  line break can arrive ALONE in its frame.
	Advance time.Duration
}

// cadenceCases are the two regimes, and they are the two runs of the real-pty
// measurement in the header: the 4-byte cadence (≈12ms/frame against a 30ms
// window — accumulating, breaks intact) and the 1-byte cadence (≈50ms/frame —
// every frame flushed on its own, breaks lost).
func cadenceCases() []cadenceCase {
	return []cadenceCase{
		{Name: "the break rides with its line (advance = a quarter window)", DeltaBytes: 1, Advance: answerFlushWindow / 4},
		{Name: "the break arrives alone in its frame (advance = two windows)", DeltaBytes: 1, Advance: answerFlushWindow * 2},
	}
}

// lineBreakCadenceDefect is the defect this file pinned, named once so the
// failure message and the report point at the same thing. It is kept AFTER the
// fix because it is what the guard's failure text is about: a run that loses a
// break still gets told which mechanism did it (the message reads in the present
// tense about the code, and the code is what answers the assertion).
const lineBreakCadenceDefect = "the live band does not commit an in-flight answer line when its line break arrives in a frame of its own: drain() splits the buffer at the last break and routes the text BEFORE it, so a frame that IS the break has an empty body — the line already painted on the live row is never committed, and the next line is painted onto the same row. One row holds many lines."

// TestAnswerLineBreaks_SurviveEveryStreamingCadence pins the rule the pty run
// measured: the number of BAND ROWS an answer occupies is a property of the
// answer's own line breaks, never of the rate the model chose to stream it.
//
// The three lines are short enough that each one owns exactly one row when its
// break survives, and the whole payload fits every row's width — so a run that
// reports three rows is a run whose breaks all survived, and one that reports
// fewer has LOST breaks. The characters are asserted too (dense, the band's
// reading is whitespace-trimmed), so a run that dropped text cannot pass by
// producing the right row count.
//
// UNGATED: the tree fixes the defect, so the guard runs unconditionally (the
// gate then in force — GOPI_LINEBREAK_CADENCE_RED — was removed in the same
// commit as the fix in drain, which is the pattern the gated form promised).
func TestAnswerLineBreaks_SurviveEveryStreamingCadence(t *testing.T) {
	const wantRows = 3
	const wantText = "AAABBBCCC"
	for _, c := range cadenceCases() {
		t.Run(c.Name, func(t *testing.T) {
			rows := cadenceRows(t, c)
			joined := dupDense(dupJoin(rows))
			t.Logf("cadence %s: %d band row(s): %q → dense %q (want %d row(s), %q)",
				c.Name, len(rows), rows, joined, wantRows, wantText)
			if joined != wantText {
				t.Fatalf("the band's reading is %q, want %q: characters were dropped or re-ordered at this cadence", joined, wantText)
			}
			if len(rows) != wantRows {
				t.Fatalf("the answer's %d line(s) occupy %d band row(s), want %d: %s",
					wantRows, len(rows), wantRows, lineBreakCadenceDefect)
			}
		})
	}
}

// cadenceRows streams the payload at one cadence and returns the band's rows off
// the settled grid, top to bottom.
func cadenceRows(t *testing.T, c cadenceCase) []string {
	t.Helper()
	sink := newFrameSink(cadenceTestCols, cadenceTestRows)
	reg := newTestRegion(sink, cadenceTestRows, cadenceTestCols)
	if !reg.Enable(cadenceTestRows, cadenceTestCols) {
		t.Fatalf("region.Enable(%d, %d) = false, want the reserved chat layout installed", cadenceTestRows, cadenceTestCols)
	}
	r := NewAnswerRenderer(reg, nil)
	r.SetCols(reg.Cols())
	now := time.Unix(emphasisChunkEpoch, 0)
	r.SetClock(func() time.Time { return now })
	r.Begin()

	text := cadenceTestText()
	for i := 0; i < len(text); i += c.DeltaBytes {
		end := i + c.DeltaBytes
		if end > len(text) {
			end = len(text)
		}
		now = now.Add(c.Advance)
		r.Delta(text[i:end])
	}
	r.Tick()
	r.Finish()
	return dupBandRows(sink.grid(), reg)
}
