package lineedit

import (
	"testing"
	"time"
)

// paste_cost_test.go — defect (A): the editor's buffer used to be grown ONE RUNE
// AT A TIME, so a paste cost O(N²) and froze the prompt.
//
// This file is deliberately SELF-CONTAINED and uses only API that predates the
// fix, so the same test compiles and runs on the PRE-FIX tree: that is what makes
// the RED proof below reproducible rather than asserted (see the Rule 5 report —
// `git stash` the fix, run this test, watch it fail at its budget, unstash).
//
// It measures the editor's own read loop, not a helper: a terminal delivers a
// paste as FULL read chunks (editReadBuf each), so the scripted source feeds
// exactly that shape, and the assertion is the WALL TIME of the whole paste plus
// the buffer the paste leaves behind.

const (
	// pasteCostBudget is the wall-time ceiling for one paste. The defect measured
	// 61ms at 4 KiB of one-at-a-time input, 936ms at 16 KiB and 23.0s at 64 KiB;
	// scaled to the 256 KiB this test pastes it is minutes, while the fixed path
	// does it in a fraction of a second — so a budget of seconds separates the two
	// by orders of magnitude and needs no tuning.
	pasteCostBudget = 15 * time.Second
	// pasteCostChunks / pasteChunkBytes describe the paste: 64 reads of one full
	// read buffer each (256 KiB).
	pasteCostChunks = 64
	pasteChunkBytes = spillChunkBytes
	// pasteCostCols is the terminal width the paste is drawn at.
	pasteCostCols = 80
	// pasteCostRows is the terminal height.
	pasteCostRows = 24
	// submitGap is how far the clock is advanced before a scripted submit Enter:
	// beyond PasteEnterSuppressWindow, so the Enter is the user's own and not a
	// pasted newline (the separation testsuite/chat_pty_test.go uses).
	submitGap = 2 * PasteEnterSuppressWindow
)

// shapeBytes grows a byte shape to exactly n bytes by repeating it.
func shapeBytes(shape []byte, n int) []byte {
	out := make([]byte, 0, n+len(shape))
	for len(out) < n {
		out = append(out, shape...)
	}
	return out[:n]
}

// editorRun is one editor Run's outcome, carried across the watchdog channel.
type editorRun struct {
	res Result
	err error
}

// runEditorWithinBudget runs the editor to EOF and fails the test if it does not
// finish inside budget.
//
// WHY A WATCHDOG rather than a plain elapsed-time assertion: at the pre-fix tree
// the defect makes this paste take minutes, and a test that must RUN TO
// COMPLETION to report "too slow" is a test nobody runs. The watchdog reports at
// the budget, which is what "never completes inside its budget" means — and on
// the fixed tree it never fires.
func runEditorWithinBudget(t *testing.T, ed *Editor, budget time.Duration, what string) (Result, time.Duration) {
	t.Helper()
	start := time.Now()
	done := make(chan editorRun, 1)
	go func() {
		res, err := ed.Run()
		done <- editorRun{res: res, err: err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("%s: editor Run: %v", what, r.err)
		}
		elapsed := time.Since(start)
		if elapsed > budget {
			t.Fatalf("%s: the editor spent %v inside its read loop (budget %v) — the buffer is still grown per rune "+
				"and/or repainted per key, so pasted input freezes the prompt for exactly this long", what, elapsed, budget)
		}
		return r.res, elapsed
	case <-time.After(budget):
		// The process ends with this failure, so the still-computing read loop
		// cannot leak into another test.
		t.Fatalf("%s: the editor did not finish inside its budget of %v — a paste of %d chunks (%d bytes) at one "+
			"splice per rune is O(N²); measured pre-fix: 23.0s at 64 KiB of one-at-a-time input",
			what, budget, pasteCostChunks, pasteCostChunks*pasteChunkBytes)
		return Result{}, 0
	}
}

// TestPasteBulkInsert_NoStoreStillCostsOneSplicePerRead is the COST regression
// test, and it is the one that isolates the cost from the spill: the editor here
// has NO paste store, so the pasted bytes really are typed into the buffer — the
// path every caller without a session directory keeps.
//
// Three facts are asserted, because the defect was three: the wall time (the
// per-rune splice), the buffer (the historic bound, unchanged), and the repaints
// (one per READ, not one per key — a 4 KiB read at 80 columns is 50 rows of
// re-layout, which was the second half of the freeze).
func TestPasteBulkInsert_NoStoreStillCostsOneSplicePerRead(t *testing.T) {
	data := shapeBytes([]byte("bulk-insert-cost-"), pasteCostChunks*pasteChunkBytes)
	steps := make([]scriptStep, 0, pasteCostChunks)
	for i := 0; i < pasteCostChunks; i++ {
		steps = append(steps, scriptStep{data: data[i*pasteChunkBytes : (i+1)*pasteChunkBytes]})
	}
	ed, _, _, out := newScriptedEditor(t, steps, pasteCostCols, nil)

	res, elapsed := runEditorWithinBudget(t, ed, pasteCostBudget,
		"a 256 KiB paste with no paste store")
	_ = res // the script ends at EOF; what this test measures is the cost and the buffer

	// The bytes are typed, and the buffer's own bound is what stops them — the
	// same bound that was in force before the fix, unchanged.
	if got := ed.Value().Len(); got != valueMaxRunesDefault {
		t.Fatalf("buffer holds %d runes, want the historic bound %d (no store = the bytes are typed until it)",
			got, valueMaxRunesDefault)
	}
	// A RED-PROOF READING the run reports: how long 256 KiB of input actually
	// took. It is logged (not asserted) so the number a reviewer sees is this
	// host's, not a quoted one.
	t.Logf("256 KiB of pasted input: %v through the read loop", elapsed)
	// Every key of a read is applied BEFORE the read's single redraw, so the
	// number of write chunks grows with the number of READS, not with the bytes.
	if got, limit := len(out.snapshot()), pasteCostChunks*repaintChunksPerRead; got > limit {
		t.Fatalf("the read loop emitted %d write chunks for %d reads (limit %d): the editor is still redrawing per "+
			"keystroke instead of once per read", got, pasteCostChunks, limit)
	}
}

// repaintChunksPerRead bounds the write() chunks one redraw may emit (the rows
// erased, the rows drawn and the placement — a small constant per repaint). It is
// slack for the TRANSPORT (how a redraw is split across write(2) calls varies
// with the layout), not a policy value.
const repaintChunksPerRead = 8
