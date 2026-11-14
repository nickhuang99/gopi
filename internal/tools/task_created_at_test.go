package tools

// task_created_at_test.go — the ledger half of the gate-assertion anchor: the
// ROW's own authoring moment, read through the same read-only opener every other
// row lookup on this path uses.
//
// WHY IT MATTERS (epic 01a14242): a briefing sentence of the shape "<surface> is
// RED on master" carries NO revision, so the only anchor the dispatch seam can
// re-measure it against is the moment the row was written — created_at. A reader
// that returned a sentinel for "no value" (or invented one) would let the seam
// stamp a distance about a moment nobody wrote at, and a false positive on an
// honest briefing is worse than the silence this epic is fixing.
//
// The arms are the two silences the caller must be able to tell apart: a row
// with a value (the ordinary case) and a row whose value is not one.

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// TestTaskCreatedAt_ReadsTheRowsOwnAuthoringMoment is the ordinary arm, driven
// through the REAL ledger and the REAL setters (Rule 32: a fresh per-test DB from
// the scaffold, never a hand-rolled path).
func TestTaskCreatedAt_ReadsTheRowsOwnAuthoringMoment(t *testing.T) {
	testutil.NewTestDBPath(t)
	taskID, err := TaskNewOID(TaskNewOptions{Summary: "gate-assertion anchor fixture"})
	if err != nil {
		t.Fatalf("TaskNewOID: %v", err)
	}
	got, ok, err := TaskCreatedAt(taskID)
	if err != nil {
		t.Fatalf("TaskCreatedAt(%s): %v", taskID, err)
	}
	if !ok || got <= 0 {
		t.Fatalf("a freshly written row carries its authoring moment; got value=%v present=%v", got, ok)
	}
	// The age the seam then measures is read back from the SAME ledger the row
	// was written to (Rules 8/13: one ledger, one reading).
	db := ledgerTestDB(t)
	var stored float64
	if err := db.QueryRow(`SELECT created_at FROM tasks WHERE task_id = ?`, taskID).Scan(&stored); err != nil {
		t.Fatalf("read created_at back: %v", err)
	}
	if stored != got {
		t.Fatalf("the accessor must report the ledger's own value (%v), not a re-derived one (%v)", stored, got)
	}
}

// TestTaskCreatedAt_AnAbsentOrUnusableMomentIsReportedNotInvented is the other
// half, and it is the one the seam's honesty depends on: a row with NO usable
// authoring moment must be distinguishable from one authored at epoch 0, or the
// caller cannot state the refusal and will silently measure nothing (Rule 15).
func TestTaskCreatedAt_AnAbsentOrUnusableMomentIsReportedNotInvented(t *testing.T) {
	testutil.NewTestDBPath(t)
	taskID, err := TaskNewOID(TaskNewOptions{Summary: "gate-assertion anchor: unusable moment"})
	if err != nil {
		t.Fatalf("TaskNewOID: %v", err)
	}
	db := ledgerTestDB(t)
	if _, err := db.Exec(`UPDATE tasks SET created_at = 0 WHERE task_id = ?`, taskID); err != nil {
		t.Fatalf("blank the moment: %v", err)
	}
	got, ok, err := TaskCreatedAt(taskID)
	if err != nil {
		t.Fatalf("a row with no usable moment is not an ERROR — the caller states it; got %v", err)
	}
	if ok {
		t.Fatalf("a zero created_at must be reported as ABSENT (value=%v), never handed back as present", got)
	}
	if got != 0 {
		t.Fatalf("an absent moment must not carry a fabricated value; got %v", got)
	}
	// An unknown row is a different thing again: a LOOKUP failure the caller
	// states, not a blank moment it measures around.
	if _, _, err := TaskCreatedAt("00000000-0000-0000-0000-000000000000"); err == nil {
		t.Fatalf("an unknown row must be an error, never a silent zero")
	}
	if _, _, err := TaskCreatedAt(""); err == nil {
		t.Fatalf("an empty task id must be refused before any query runs")
	}
}
