package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// MULTI-PARENT DEPENDENCY (epic multi-parent).
//
// A row may depend on SEVERAL producers. The wire shape is deliberately not a
// new one: `parent_task_id` keeps its exact meaning (parent 1) and the new
// `parent_task_ids` column carries parents 2..N, comma-joined. Everything below
// is written against that split, and the two properties that make it safe are
// the two this file's first tests pin:
//
//   - a HISTORICAL row (only the old column) reads as exactly one parent, so
//     every release before this epic behaves identically;
//   - the FIRST parent stays in the old column, so a reader that knows only
//     the old column still sees a dependency instead of a free row.
//
// The gate itself is an AND over the set (Rule 33 is otherwise untouched: same
// release set, same dangling refusal, no override, no cascade, no timeout).
// Rule 32: every ledger comes from the scaffold, never a hand-rolled path.

// multiParentDB pins a fresh per-test ledger (Rule 32).
func multiParentDB(t *testing.T) {
	t.Helper()
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))
}

// multiParentRow submits one row with an explicit id and an explicit parent
// SET, so an assertion can name exactly what it is talking about.
func multiParentRow(t *testing.T, id, summary string, parents ...string) {
	t.Helper()
	opts := TaskSubmitOptions{
		TaskID:  id,
		Summary: summary,
		Payload: "briefing for " + summary,
		Source:  "auto",
	}
	if len(parents) > 0 {
		opts.ParentTaskID = parents[0]
	}
	if len(parents) > 1 {
		opts.ParentTaskIDs = TaskParentIDsJoin(parents[1:])
	}
	if _, err := TaskSubmit(opts); err != nil {
		t.Fatalf("submit %s: %v", id, err)
	}
}

// multiParentCells reads the two RAW columns — the only reading that can show
// the split itself (taskParentIDsOf deliberately cannot).
func multiParentCells(t *testing.T, id string) (first, rest string) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(parent_task_id,''), COALESCE(`+taskParentIDsColumnName+`,'') FROM tasks WHERE task_id = ?`,
		id).Scan(&first, &rest); err != nil {
		t.Fatalf("read parent cells of %s: %v", id, err)
	}
	return first, rest
}

// multiParentOf reads a row's parent LIST through the ONE reader, off the
// ledger (not from the in-memory options), so a test cannot pass on what it
// asked for instead of on what was stored.
func multiParentOf(t *testing.T, id string) []string {
	t.Helper()
	first, rest := multiParentCells(t, id)
	return taskParentIDsOf(first, rest)
}

// TestHistoricalParentRowReadsAsOneParent is the COMPATIBILITY floor: a row
// written by any release before this epic carries only parent_task_id, and it
// must read back as exactly that one parent — not as zero (which would un-park
// it) and not as two.
func TestHistoricalParentRowReadsAsOneParent(t *testing.T) {
	if got := taskParentIDsOf("p-old", ""); len(got) != 1 || got[0] != "p-old" {
		t.Fatalf("historical row must read as exactly its one parent, got %v", got)
	}
	if got := taskParentIDsOf("", ""); len(got) != 0 {
		t.Fatalf("a parentless row must read as no parents, got %v", got)
	}
	// Tolerances that must not change the answer: whitespace around a cell and
	// the same id appearing in both cells (a hand-edited ledger, or a row that
	// passed through a build that wrote the whole set to the new column).
	if got := taskParentIDsOf(" p-a ", " p-b , "); len(got) != 2 || got[0] != "p-a" || got[1] != "p-b" {
		t.Fatalf("cells must be trimmed and split, got %v", got)
	}
	if got := taskParentIDsOf("p-a", "p-a,p-b"); len(got) != 2 || got[1] != "p-b" {
		t.Fatalf("a repeated id must be deduplicated, got %v", got)
	}
}

// TestFirstParentStaysInTheOldColumn is the WRITER contract: parent 1 to
// parent_task_id, parents 2..N to parent_task_ids — which is what lets an
// OLDER binary on another node still see a dependency.
func TestFirstParentStaysInTheOldColumn(t *testing.T) {
	first, rest := taskParentsForWrite([]string{"p-a", "p-b", "p-c"})
	if first != "p-a" {
		t.Fatalf("parent 1 must land in parent_task_id, got %q", first)
	}
	if rest != "p-b,p-c" {
		t.Fatalf("parents 2..N must land in parent_task_ids in order, got %q", rest)
	}
	if f, r := taskParentsForWrite(nil); f != "" || r != "" {
		t.Fatalf("no parents must write neither cell, got %q/%q", f, r)
	}
	if f, r := taskParentsForWrite([]string{"p-solo"}); f != "p-solo" || r != "" {
		t.Fatalf("a single parent must write only the old column (byte-identical to pre-epic), got %q/%q", f, r)
	}
}

// TestSubmitStoresEveryParentAndKeepsTheSplit drives the REAL submit path on a
// real ledger and then reads the two cells back: three parents in, parent 1 in
// parent_task_id, the other two in parent_task_ids.
func TestSubmitStoresEveryParentAndKeepsTheSplit(t *testing.T) {
	multiParentDB(t)
	multiParentRow(t, "mp-a", "producer a")
	multiParentRow(t, "mp-b", "producer b")
	multiParentRow(t, "mp-c", "producer c")
	multiParentRow(t, "mp-kid", "consumer of three", "mp-b", "mp-a", "mp-c")

	first, rest := multiParentCells(t, "mp-kid")
	if first != "mp-b" {
		t.Fatalf("parent 1 must be the FIRST typed parent, got %q", first)
	}
	if rest != "mp-a,mp-c" {
		t.Fatalf("parents 2..N must preserve typed order, got %q", rest)
	}
	if got := multiParentOf(t, "mp-kid"); len(got) != 3 {
		t.Fatalf("the row must read back all three parents, got %v", got)
	}
}

// TestSubmitRefusesADanglingSecondParent is the Rule 15 arm: a SECOND parent
// that names no row must be refused BY NAME, never dropped. A silently dropped
// parent is a row that claims to wait for one producer while it actually waits
// for two — and Rule 33 has no repair path for the row it would un-park.
func TestSubmitRefusesADanglingSecondParent(t *testing.T) {
	multiParentDB(t)
	multiParentRow(t, "mp-real", "a real producer")

	_, err := TaskSubmit(TaskSubmitOptions{
		Summary:       "child with a typo in parent 2",
		Payload:       "briefing",
		ParentTaskID:  "mp-real",
		ParentTaskIDs: "mp-doesnotexist",
		Source:        "auto",
	})
	if err == nil {
		t.Fatal("a dangling SECOND parent must be refused, not silently dropped")
	}
	if !strings.Contains(err.Error(), "mp-doesnotexist") {
		t.Fatalf("the refusal must name the dangling parent, got: %v", err)
	}
}

// TestParentGateIsAnANDOverEveryParent is the heart of the epic: with three
// parents, releasing ONE is not enough — the row stays parked until every
// parent is released, and the verdict names the parents that still block.
func TestParentGateIsAnANDOverEveryParent(t *testing.T) {
	multiParentDB(t)
	multiParentRow(t, "mp-p1", "producer one")
	multiParentRow(t, "mp-p2", "producer two")
	multiParentRow(t, "mp-child", "waits for both", "mp-p1", "mp-p2")

	idx := multiParentStatusIndex(t)
	parents := multiParentOf(t, "mp-child")

	denied, reason := taskParentGateDenied("", "", "", parents, idx)
	if !denied {
		t.Fatal("two pending parents must park the child")
	}
	if !strings.Contains(reason, "mp-p1") || !strings.Contains(reason, "mp-p2") {
		t.Fatalf("the verdict must name EVERY blocking parent, got %q", reason)
	}

	// Release ONE: still parked, and the verdict now names only the other.
	releaseParent(t, "mp-p1")
	idx = multiParentStatusIndex(t)
	denied, reason = taskParentGateDenied("", "", "", parents, idx)
	if !denied {
		t.Fatal("one released parent must NOT release the row (the gate is an AND)")
	}
	if strings.Contains(reason, "mp-p1") {
		t.Fatalf("a released parent must not be named as blocking, got %q", reason)
	}
	if !strings.Contains(reason, "mp-p2") {
		t.Fatalf("the still-pending parent must be named, got %q", reason)
	}

	// Release the last one: the row becomes claimable.
	releaseParent(t, "mp-p2")
	idx = multiParentStatusIndex(t)
	if denied, reason := taskParentGateDenied("", "", "", parents, idx); denied {
		t.Fatalf("every parent released must release the row, got %q", reason)
	}
}

// multiParentStatusIndex reads the parent-status index the gate consumes,
// off the pinned ledger (Rule 32 — the scaffold owns the path).
func multiParentStatusIndex(t *testing.T) map[string]string {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	idx, err := taskParentStatuses(ctx, db)
	if err != nil {
		t.Fatalf("parent statuses: %v", err)
	}
	return idx
}

// TestReparentReplacesTheWholeParentSet: the update face is the SET, not one
// edge — a second parent added later must land in the new column, and
// --clear-parent must clear EVERY parent.
func TestReparentReplacesTheWholeParentSet(t *testing.T) {
	multiParentDB(t)
	multiParentRow(t, "mp-r1", "producer one")
	multiParentRow(t, "mp-r2", "producer two")
	multiParentRow(t, "mp-r3", "producer three")
	multiParentRow(t, "mp-move", "moves its parents", "mp-r1")

	if _, err := TaskReparentParents("mp-move", []string{"mp-r1", "mp-r2", "mp-r3"}, "two producers were missing"); err != nil {
		t.Fatalf("reparent onto three parents: %v", err)
	}
	if got := multiParentOf(t, "mp-move"); len(got) != 3 || got[0] != "mp-r1" || got[1] != "mp-r2" || got[2] != "mp-r3" {
		t.Fatalf("the reparent must store the whole set in typed order, got %v", got)
	}

	// The single-parent face must still be the single-parent face.
	if _, err := TaskReparent("mp-move", "mp-r2", "back to one producer"); err != nil {
		t.Fatalf("single-parent reparent: %v", err)
	}
	if got := multiParentOf(t, "mp-move"); len(got) != 1 || got[0] != "mp-r2" {
		t.Fatalf("a single-parent reparent must leave exactly that parent, got %v", got)
	}

	// Clearing removes the LIST.
	if _, err := TaskReparentParents("mp-move", nil, "the dependency was a mistake"); err != nil {
		t.Fatalf("clear the parent set: %v", err)
	}
	first, rest := multiParentCells(t, "mp-move")
	if first != "" || rest != "" {
		t.Fatalf("clearing must empty BOTH cells, got %q/%q", first, rest)
	}
}

// TestReparentRefusesADanglingSecondParent mirrors the submit arm on the update
// face: every new parent is validated, and the refusal names the bad one.
func TestReparentRefusesADanglingSecondParent(t *testing.T) {
	multiParentDB(t)
	multiParentRow(t, "mp-ok", "a real producer")
	multiParentRow(t, "mp-target", "the row being moved", "mp-ok")

	_, err := TaskReparentParents("mp-target", []string{"mp-ok", "mp-ghost"}, "add a second producer")
	if err == nil {
		t.Fatal("a dangling SECOND parent must be refused on the update face too")
	}
	if !strings.Contains(err.Error(), "mp-ghost") {
		t.Fatalf("the refusal must name the dangling parent, got: %v", err)
	}
	// Nothing was written: the row still carries exactly its original parent.
	if got := multiParentOf(t, "mp-target"); len(got) != 1 || got[0] != "mp-ok" {
		t.Fatalf("a refused reparent must leave the row untouched, got %v", got)
	}
}

// TestCycleThroughTheSecondParentIsRefused is the arm the single-chain walk
// could not see: the ring closes through parent 2 (a row whose PARENT #2 is a
// descendant of the row being moved). Before the walk became a graph search it
// followed only parent_task_id, so this move would have been written — and
// Rule 33 has no override, no cascade and no timeout, so every row on the ring
// would have parked forever with no repair path.
func TestCycleThroughTheSecondParentIsRefused(t *testing.T) {
	multiParentDB(t)
	multiParentRow(t, "cy-root", "root producer")
	multiParentRow(t, "cy-mid", "middle", "cy-root")
	// cy-leaf's SECOND parent is cy-mid; cy-mid's ancestor is cy-root.
	multiParentRow(t, "cy-leaf", "leaf", "cy-root", "cy-mid")

	// Making cy-root depend on cy-leaf closes the ring ONLY through cy-leaf's
	// second parent: root → leaf → mid → root.
	_, err := TaskReparentParents("cy-root", []string{"cy-leaf"}, "this would close a ring through parent 2")
	if err == nil {
		t.Fatal("a cycle closed through a SECOND parent must be refused")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("the refusal must say it is a cycle, got: %v", err)
	}
	// Nothing was written.
	if got := multiParentOf(t, "cy-root"); len(got) != 0 {
		t.Fatalf("a refused move must leave the row untouched, got %v", got)
	}
}
