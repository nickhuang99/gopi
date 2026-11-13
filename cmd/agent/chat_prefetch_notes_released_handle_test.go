package main

// chat_prefetch_notes_released_handle_test.go — the RELEASED-HANDLE defect, in
// two tests (the regression pin for the SIGSEGV the acceptance run of task
// 01a132e2 hit).
//
// WHAT WENT WRONG, stated as the surface this file drives:
//
//	chatPrefetch.Stop releases the session's read-only handle — (*prefetchHandle).close
//	closes the connection AND nulls the field — while a lookup that is already in
//	flight holds the POINTER it was handed. The note leg then asked "is the handle I
//	was given the one holding my index?" and the answer was PATH equality, which is
//	still true of a released handle. So the leg answered through it and handed
//	`db.db` — nil — to tools.ChatNoteSearchOn, whose first statement calls
//	(*sql.DB).QueryContext on it: a nil dereference inside database/sql, i.e. a
//	SIGSEGV that takes the WHOLE package's result down (measured: a package-level
//	fail in the run stream with no test name).
//
// THE TWO TESTS SPLIT THE CLAIM THE SAME WAY THE FIX DOES:
//
//	1. the LEG'S CHOICE — "a released handle is not a handle I will answer
//	   through" — asserted on the leg's own observable (was the shared handle
//	   reused?), because a timing would assert the machine's load instead;
//	2. the WHOLE LOOKUP — driven with the released handle exactly as the executor
//	   hands it over, asserting the leg still answered. This is the test that
//	   crashed before the fix: a nil dereference is not an assertion failure, so
//	   the old code's red is a SIGSEGV here, not a t.Fatalf.

import (
	"context"
	"testing"

	"github.com/nick/gopi/internal/memoryprefetch"
	"github.com/nick/gopi/internal/tools"
)

// prefetchNotesReleasedHandleQuery is the query both tests drive. It carries
// words the fixture's note holds, so the leg's answer is a measurement (rows
// came back) rather than an absence dressed as a pass.
const prefetchNotesReleasedHandleQuery = "released handle must not be answered through"

// releasedPrefetchHandle builds the exact state the defect needs: a handle that
// is NON-NIL, still records the path it was dialled against, and no longer holds
// a connection — the shape Stop leaves behind for any lookup that captured the
// pointer before it ran.
func releasedPrefetchHandle(t *testing.T, path string) *prefetchHandle {
	t.Helper()
	h, err := prefetchOpenHandle(context.Background(), path)
	if err != nil {
		t.Fatalf("warm the handle this test releases: %v", err)
	}
	if h.db == nil {
		t.Fatal("the freshly opened handle holds no connection — the fixture is not the defect's shape")
	}
	h.close()
	if h.db != nil {
		t.Fatal("close() left a connection on the handle: the released shape cannot be built")
	}
	if h.path != path {
		t.Fatalf("the released handle no longer records its path (%q != %q) — the path equality that caused the bug is not being reproduced", h.path, path)
	}
	return h
}

// TestPrefetchNotesLegDoesNotReuseAReleasedHandle is the leg's own half: a
// shared handle whose connection was released is not a candidate for reuse, and
// the leg dials its own instead of handing a nil connection to the search.
func TestPrefetchNotesLegDoesNotReuseAReleasedHandle(t *testing.T) {
	path := seedPrefetchLedger(t)
	t.Setenv("GOPI_MEMORY_DB", path)

	released := releasedPrefetchHandle(t, path)

	// CONTROL: a usable handle on the same path IS reused. Without this the test
	// could pass for the wrong reason (a leg that never reuses anything), and the
	// reuse this leg is built on would silently disappear.
	warmHandle, err := prefetchOpenHandle(context.Background(), path)
	if err != nil {
		t.Fatalf("open the control handle: %v", err)
	}
	defer warmHandle.close()

	control := newPrefetchNotesSource("")
	if _, err := control.handleFor(context.Background(), warmHandle, path); err != nil {
		t.Fatalf("the control leg refused a usable shared handle: %v", err)
	}
	if !control.reused {
		t.Fatal("the control leg did not reuse a usable shared handle — the reuse contract is gone")
	}
	control.close()

	// THE CLAIM: the released handle is refused, and the leg answers through a
	// handle of its own that actually carries a connection.
	src := newPrefetchNotesSource("")
	got, err := src.handleFor(context.Background(), released, path)
	if err != nil {
		t.Fatalf("the leg gave up on a released shared handle instead of dialling its own: %v", err)
	}
	defer src.close()
	if got == released {
		t.Fatal("the note leg reused a RELEASED handle: its connection is gone, and the search would dereference nothing")
	}
	if src.reused {
		t.Fatal("the leg recorded reuse for a released handle: the record would claim a connection it does not have")
	}
	if got.db == nil {
		t.Fatal("the leg answered through a handle with no connection")
	}
}

// TestPrefetchLookupDrivesAReleasedSharedHandle is the whole-lookup half, driven
// through the REAL prefetchLookupDetailed/notes path with the released handle in
// the seat the executor hands it over in. On the pre-fix code this test SIGSEGVs
// (tools.ChatNoteSearchOn's second argument is 0x0); after the fix the lookup
// returns and the note leg reports a verdict of its own.
func TestPrefetchLookupDrivesAReleasedSharedHandle(t *testing.T) {
	path := seedPrefetchLedger(t)
	t.Setenv("GOPI_MEMORY_DB", path)
	seedPrefetchNotesLedger(t, []tools.ChatNote{{
		SessionID: "an-earlier-run",
		Turn:      1,
		Note:      "a released handle must not be answered through: the leg dials its own",
		Host:      "some-other-host",
		Path:      "/home/nick/.gopi/sessions/an-earlier-run/self-record.md",
	}}, "released handle")

	released := releasedPrefetchHandle(t, path)

	res := prefetchLookupDetailed(context.Background(), prefetchNotesReleasedHandleQuery,
		memoryprefetch.DefaultConfig(), released, newPrefetchNotesSource(""))

	if res.Notes.Outcome == "" {
		t.Fatal("the note leg recorded no verdict at all: the absence of the block is not a result")
	}
	if res.Notes.HitsCount == 0 {
		t.Fatalf("the note index holds a matching row and the leg returned nothing (outcome=%q err=%v) — it did not answer through a usable handle",
			res.Notes.Outcome, res.Notes.Err)
	}
}
