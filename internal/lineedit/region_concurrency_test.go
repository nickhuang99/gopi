package lineedit

import (
	"sync"
	"testing"
)

// region_concurrency_test.go — the guard for the layout-state race round 7
// measured.
//
// The region's published layout (active/cols/plan) is WRITTEN by the editor's
// read loop and READ by a SECOND goroutine in production: the chat REPL's own
// goroutine refreshes the status row while the editor is parked in its read
// (cmd/agent StatusTick -> lineedit StatusRow/LiveRow). Republishing the layout
// and reading it concurrently is therefore a real race, not a test artefact —
// `go test -race ./cmd/agent/ -run TestChatStream_ReservedLayoutCommitsEachLineOnce`
// reported it on the branch AND on master.
//
// The assertion here is deliberately thin: the real output of this test is the
// RACE DETECTOR's verdict. Every published accessor is exercised concurrently
// with a writer that republishes the layout (the menu opening/closing, the
// editing line growing/shrinking) so a missing lock cannot hide behind an
// accessor the test forgot to call.

// TestA5B_LayoutStateIsReadableFromAnotherGoroutine drives every published
// region accessor from a second goroutine while the editor's goroutine
// republishes the layout. Under -race a missing lock is a failure; the value
// assertions catch the other half — a reader pairing the editing row of one
// layout with the status row of another.
func TestA5B_LayoutStateIsReadableFromAnotherGoroutine(t *testing.T) {
	h := newA5Harness(t, nil, nil)
	const rounds = 400

	var wg sync.WaitGroup
	wg.Add(2)

	// Writer: the editor's goroutine republishes the layout.
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			h.region.SetMenuRows(i % menuRowsCap)
			h.region.SetEditorRows(1 + i%editorRowsCap)
		}
	}()

	// Reader: a second goroutine touching every accessor a caller outside the
	// editor's loop can reach.
	go func() {
		defer wg.Done()
		for i := 0; i < rounds; i++ {
			p := h.region.Plan()
			// The plan is ONE layout: the band sits above the editing line,
			// which sits above the menu, which sits above the status row. A
			// torn read (rows from two different layouts) breaks that order.
			if !(p.BandTop <= p.BandBottom &&
				p.BandBottom < p.EditorTop &&
				p.EditorTop+p.EditorRows <= p.MenuTop &&
				p.MenuTop+p.MenuRows <= p.StatusRow) {
				t.Errorf("plan paired rows from two layouts (band %d..%d, editor %d+%d, menu %d+%d, status %d)",
					p.BandTop, p.BandBottom, p.EditorTop, p.EditorRows, p.MenuTop, p.MenuRows, p.StatusRow)
				return
			}
			_ = h.region.StatusRow()
			_ = h.region.EditorTop()
			_ = h.region.LiveRow()
			_ = h.region.BandRows()
			_ = h.region.EditingRows()
			_ = h.region.MenuCapacity()
			_ = h.region.MaxMenuRows(1)
			_ = h.region.Cols()
			_ = h.region.Active()
		}
	}()

	wg.Wait()
}
