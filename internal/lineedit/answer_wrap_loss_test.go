package lineedit

import (
	"fmt"
	"strings"
	"testing"
)

// answer_wrap_loss_test.go — the CELL-GRID guard for the answer band's
// cell LOSS on the wrap path (Rule 10.2: the renderer's own bytes replayed into
// the package's vt100 Screen, the painted cells read back — never a string
// assertion on the renderer's output).
//
// THE DEFECT THIS CASE IS THE MINIMAL REPRO FOR. A line wider than the band row
// wraps: the cells that do not fit are cut and handed to the next row, while the
// live row's own text (liveText) is carried forward as the head of the line the
// band is about to COMMIT. When the head fills the row, the committing row does
// not fit beside it, and the pre-fix tree took that path with an EMPTY row: it
// erased from the live column to the end of the row — which IS the head's cells,
// the only copy of them — and promoted the row carrying nothing. Measured by
// streaming the transcript at nine cadences (committed cells carried, out of 3255):
//
//	delta   1     2     3     5     7     9    11    13    64
//	before  0  3030     0  3030  2626  2245  2952  3053  3255
//	after 3255  3255  3255  3255  3255  3255  3255  3255  3255
//
// — the reported "the band loses one cell per wrapped line" is that defect read
// row-wise, and delta=1/3 (a row promoted BLANK, so the whole reading is empty) is
// its worst shape. delta=64 carried everything even before the fix: at that size no
// wrap lands on the commit seam, so it is this case's CONTROL — a red control means
// the instrument is broken, not the renderer.
//
// WHY THE SCROLLBACK IS PART OF THE READING. A committed line LEAVES the screen:
// the layout's whole mechanism is one scroll of the reserved region, which pushes
// the band's top row off the screen and into the terminal's own scrollback. A rig
// that reads only what is still visible is therefore reading a window, not the
// band's output, and a whole line that has scrolled away reads as "missing". The
// committed reading is scrollback + visible band rows, in that order.
//

// The case streams the rig's OWN transcript (dupTranscript, in
// answer_wrap_scroll_dup_test.go): the operator's sentence followed by numbered
// lines whose every token is unique — an (i, j) pair — because a shared tail would
// make "this text occurs twice" true of the TRANSCRIPT and the assertion would then
// be reporting the answer rather than the renderer. No markup characters, for the
// reason that file records: the renderer strips emphasis markers from the cells it
// paints, so a fixture carrying them asks a comparison to match a grid without
// markers against a transcript with them.

// lossCommitted returns everything the band has COMMITTED, top to bottom: the rows
// the band's scroll pushed off the screen (the screen's scrollback at this instant,
// in the order they left it) followed by the band rows still on it. Both are dense —
// spaces and breaks dropped, by the same dupDense the rest of the rig uses, because
// the renderer CONSUMES the space a wrap breaks at and trims its rows.
func lossCommitted(scroll []string, grid []string, reg *region) string {
	var b strings.Builder
	for _, l := range scroll {
		b.WriteString(dupDense(cutReadingFold(strings.TrimRight(l, " "))))
	}
	b.WriteString(strings.Join(dupBandRows(grid, reg), ""))
	return b.String()
}

// TestAnswerBand_WrappedLineKeepsEveryCommittedCell is the settled-grid half: the
// band's whole committed reading is the transcript's own cells, once, in order.
func TestAnswerBand_WrappedLineKeepsEveryCommittedCell(t *testing.T) {
	flat := dupDense(dupTranscript())
	for _, delta := range append(dupSweep(dupSweepMax), dupControlDelta) {
		t.Run(fmt.Sprintf("delta=%d", delta), func(t *testing.T) {
			sink, reg, _ := dupRig(t, delta)
			got := lossCommitted(sink.screen.Scrollback(), sink.grid(), reg)
			if got == "" {
				t.Fatalf("delta=%d: the band committed NO cells at all — every row it promoted is blank (grid:\n%s)",
					delta, dupGrid(sink.grid()))
			}
			if len(got) != len(flat) {
				t.Fatalf("delta=%d: the band committed %d of the transcript's %d cells (%d lost, reordered or duplicated on the wrap path):\n  committed: %q\n  transcript goes on %q\n  (grid:\n%s)",
					delta, len(got), len(flat), len(flat)-len(got), dupClip(got), dupClip(lossDiverges(flat, got)), dupGrid(sink.grid()))
			}
			if !strings.Contains(flat, got) {
				t.Fatalf("delta=%d: the band's committed cells are not one contiguous run of the transcript:\n  committed: %q\n  transcript goes on %q\n  (grid:\n%s)",
					delta, dupClip(got), dupClip(lossDiverges(flat, got)), dupGrid(sink.grid()))
			}
		})
	}
}

// TestAnswerBand_WrappedLineLosesNothingInAnyFrame is the FRAME-level half: a
// settled grid can be right while a frame the human watched was wrong (a whole
// committed row is wiped in one write and carried nowhere). Every write's screen is
// read back, and the invariant is monotonicity of the COMMITTED CELLS — the band
// only ever grows an answer, so a frame whose resolved band holds fewer cells than
// the frame before it lost text the reader had already been shown. Measured on the
// pre-fix tree at this cadence: 31 frames shrink (write #65 reads 136 cells and then
// 0, the whole first row), and 30 of them are a blank reading where text had been.
func TestAnswerBand_WrappedLineLosesNothingInAnyFrame(t *testing.T) {
	flat := dupDense(dupTranscript())
	for _, delta := range append(dupSweep(dupSweepMax), dupControlDelta) {
		t.Run(fmt.Sprintf("delta=%d", delta), func(t *testing.T) {
			sink, reg, _ := dupRig(t, delta)
			high, judged := 0, 0
			for _, fr := range sink.frames {
				got := lossCommitted(fr.scroll, fr.rows, reg)
				if got == "" && high == 0 {
					// Before the first committed cell there is nothing to lose:
					// the band is cleared at Begin.
					continue
				}
				judged++
				if len(got) < high {
					t.Fatalf("at write #%d the band's committed reading SHRANK from %d to %d cells — the reader had been shown text this frame no longer holds:\n  reading: %q\n  grid:\n%s",
						fr.chunk, high, len(got), dupClip(got), dupGrid(fr.rows))
				}
				high = len(got)
				if got != "" && !strings.Contains(flat, got) {
					t.Fatalf("at write #%d the band's committed cells are not one contiguous run of the transcript:\n  committed: %q\n  transcript goes on %q\n  grid:\n%s",
						fr.chunk, dupClip(got), dupClip(lossDiverges(flat, got)), dupGrid(fr.rows))
				}
			}
			if judged == 0 {
				t.Fatal("no frame had a readable band: the rig is not driving the renderer at all")
			}
			t.Logf("frames read and judged: %d (peak %d cells)", judged, high)
		})
	}
}

// lossDiverges quotes the transcript where the band's reading stopped being its
// own cells — the span the reading should have carried next.
func lossDiverges(flat, got string) string {
	at := strings.Index(flat, got)
	if at < 0 {
		// The reading does not even start on a transcript cell: quote the
		// transcript where the reading's own head is, or the opening.
		head := got
		if len(head) > 40 {
			head = head[:40]
		}
		if s := strings.Index(flat, head); s >= 0 {
			return flat[s:min(len(flat), s+120)]
		}
		return flat[:min(len(flat), 120)]
	}
	return flat[at:min(len(flat), at+len(got)+120)]
}
