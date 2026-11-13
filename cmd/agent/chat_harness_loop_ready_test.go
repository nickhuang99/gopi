package main

// chat_harness_loop_ready_test.go — the guard for the shared root cause of the
// PTY/lineedit flake family: a harness that STARTS a read loop and then writes a
// band row from the test goroutine must wait until the loop actually owns the
// renderer, or its write is queued into a window nobody drains in time.
//
// THE MECHANISM, measured (not inferred). Every band row and every answer call is
// ROUTED to the read loop, and the routing decision is a goroutine identity
// (Editor.loopGoid, internal/lineedit/bandwait.go): while the loop has not entered
// Run, loopGoid == 0, and WriteBandRow's arm for that state QUEUES the row and
// returns true WITHOUT drawing it. A goroutine started with `go in.ReadLine()` is
// not scheduled synchronously, so between the `go` and the loop's first Run the
// test goroutine has the editor to itself.
//
// The measured shape on this very harness, before the fix: 2 write chunks at the
// moment writeFlow returned, 9 only after the loop ran, and the row the case then
// asserted was ABSENT from the replayed grid. The row was LATE, never lost — the
// frame stays the LEVEL the loop's next read outcome re-derives — so the fix is to
// WAIT for the loop instead of SAMPLING the window (streamHarness.awaitReadLoop).
//
// WHY THIS GUARD IS A TEST AND NOT A COMMENT: the defect is invisible to a single
// run of any one case (it is a race with goroutine scheduling), so a regression
// that removes the wait would come back as an intermittent red in four other
// files. This test asserts the CONTRACT the harnesses now provide — "the harness
// you get back is one whose writes are drawn" — with no wait of its own, so it
// fails deterministically if the wait is dropped.

import (
	"strings"
	"testing"
)

// TestChatHarness_ReadLoopOwnsTheRendererBeforeItIsHandedOver pins the contract
// the four band-writing harnesses share: as soon as the constructor returns, a
// band write from the caller's own goroutine is DRAWN — no waitFor, no poll, no
// second chance.
//
// A harness that returned during the boot window would leave the row queued and
// this grid empty, which is exactly the pre-fix failure (the row was still a level
// the loop would re-derive on its next read, but the case had already read).
func TestChatHarness_ReadLoopOwnsTheRendererBeforeItIsHandedOver(t *testing.T) {
	h := flowHarness(t)

	const row = "harness-row-drawn-synchronously"
	if !h.in.editor.WriteBandRow(row) {
		t.Fatal("WriteBandRow refused: the layout is not active on a harness whose EnableRegion returned true")
	}
	// The decisive reading: the cells, taken IMMEDIATELY. If the constructor
	// returned before the loop owned the renderer, the row is still queued and
	// this fails.
	h.replay()
	if !strings.Contains(h.screen.Contents(), row) {
		t.Fatalf("the harness handed back a renderer the read loop does not yet own: the row written on the caller's own goroutine (%q) is not on the grid\n--- screen ---\n%s",
			row, h.screen.Contents())
	}
}

// TestChatHarness_ReadLoopOwnsTheRendererForTheStreamHarness is the same contract
// for the other constructor shape (newStreamHarness, which the stream and height
// cases use). It is a separate case rather than a table entry because the two
// harnesses are separate constructors and a fix applied to one is exactly the
// half-fix this family has produced before.
func TestChatHarness_ReadLoopOwnsTheRendererForTheStreamHarness(t *testing.T) {
	h := newStreamHarness(t)

	const row = "stream-harness-row-drawn-synchronously"
	if !h.in.editor.WriteBandRow(row) {
		t.Fatal("WriteBandRow refused: the layout is not active on a harness whose EnableRegion returned true")
	}
	h.replay()
	if !strings.Contains(h.screen.Contents(), row) {
		t.Fatalf("the stream harness handed back a renderer the read loop does not yet own: the row written on the caller's own goroutine (%q) is not on the grid\n--- screen ---\n%s",
			row, h.screen.Contents())
	}
}
