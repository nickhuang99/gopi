package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedQueueRow inserts one pending row with an EXPLICIT priority and
// created_at — the two values the claim order is defined by. Raw SQL because
// neither is settable through the public API on purpose (created_at is
// stamped by the clock, priority is derived from tags).
// queueRowFixture is the shape the tests below seed: the claim order is
// decided by (priority, created_at), so both are explicit.
type queueRowFixture struct {
	id        string
	tags      string
	payload   string
	priority  int
	createdAt float64
}

// seedQueueRows writes the fixture rows into the ledger of the current test
// DB (GOPI_MEMORY_DB must already be set to a temp path).
func seedQueueRows(t *testing.T, rows []queueRowFixture) {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	for _, r := range rows {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO tasks (task_id, summary, status, tags, payload, source, priority, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, 'human', ?, ?, ?)`,
			r.id, "summary of "+r.id, taskStatusPending, r.tags, r.payload, r.priority, r.createdAt, r.createdAt); err != nil {
			t.Fatalf("seed %s: %v", r.id, err)
		}
	}
}

// renderedRowOrder returns each row's id in render order. The "#" column IS
// the full task id (a row no longer carries a truncated chunk beside it — epic
// full-id-no-prefix), so the column is the id with its marker stripped.
func renderedRowOrder(rendered string) []string {
	var ids []string
	for _, ln := range strings.Split(rendered, "\n") {
		if !strings.HasPrefix(ln, TaskListRowPrefix) {
			continue
		}
		fields := strings.Fields(ln)
		if len(fields) < 2 {
			continue
		}
		ids = append(ids, strings.TrimPrefix(fields[0], TaskListRowPrefix))
	}
	return ids
}

// renderedRowLine returns the rendered line whose task_id column is id.
//
// THE ROW LAYOUT IS `#<id>  [status]  <summary> …` (one identity per row, epic
// full-id-no-prefix). The id is what FOLLOWS the row prefix, not a separate
// whitespace-delimited column: this helper used to probe for `" "+id+" "`, the
// pre-full-id shape (a truncated chip in field 0 with the full id in field 1).
// Once the layout landed that probe matched NO row — so a caller read "the row
// is missing from the listing", which is exactly what the parked-child
// assertion reported (measured 2026-09-25, master: the line was present and
// began `#<child-id>  [pending] …` while this helper returned "").
//
// The match is therefore on the ID COLUMN, never on any occurrence of the id in
// the row, and that distinction is load-bearing: a row's own TAIL can carry
// another row's id (a child's "[queue-skip] parent <id> not released" suffix, a
// resumed:x tag), and a substring match would return the CHILD's line for the
// PARENT's id — see TestParentGateRowLocatorNamesTheIdColumn.
//
// Matching the column keeps working for a row whose id column ends the line (no
// trailing space to find), and the column is built from the row prefix constant
// rather than a re-typed separator (Rules 1/13).
func renderedRowLine(rendered, id string) string {
	idColumn := TaskListRowPrefix + id
	for _, ln := range strings.Split(rendered, "\n") {
		if !strings.HasPrefix(ln, TaskListRowPrefix) {
			continue
		}
		// MERGE NOTE (rework run 01a0d7a3ca337b2ea0aeefaa289db1a4): master had
		// already fixed this helper the same way the archived branch did
		// (probe the id COLUMN, never a substring — the regression pinned by
		// TestParentGateRowLocatorNamesTheIdColumn). The branch's spelling is
		// kept because it is the strictly wider one: comparing fields[0]
		// against the id column needs no len(fields) >= 2 guard, so a row
		// whose id column ENDS the line still resolves. master's comment
		// survives as this note's cross-reference.
		fields := strings.Fields(ln)
		if len(fields) == 0 || fields[0] != idColumn {
			continue
		}
		return ln
	}
	return ""
}

// TestPendingListRendersNewestFirstAndMarksTheClaimedRow pins the contract of
// the pending view after the epic task-list-order-surface (2026-09-26):
//
//	① the row ORDER is newest-first, like every other view — the order the
//	   verb's help advertises ("List tasks, newest first"), so the filtered
//	   and unfiltered channels do not silently disagree;
//	② the row the CLAIM FUNNEL takes next is marked [next] — Rule 33: the
//	   marker names the row a dispatcher really claims, which is NOT the top
//	   line once the render is chronological (the queue claims priority-first);
//	③ an unclaimable row says so instead of looking starved.
//
// The fixture makes ① and ② disagree on purpose: the newest row (t-new-bug)
// claims FIRST (bug tier, newest created_at), while the oldest row
// (t-old-feature) renders LAST. A render that put the claimed row on top —
// the previous contract, which is exactly what the help contradicted — fails
// ② below, because the top line then carries neither marker while [next] sits
// several lines down.
func TestPendingListRendersNewestFirstAndMarksTheClaimedRow(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))

	// claim order (priority, created_at): t-next-bug (1,100) → t-then-bug
	// (1,200) → … ; render order (created_at DESC): t-new-bug, t-next-bug,
	// t-then-bug, t-old-feature, t-ledger-only — the two disagree.
	seedQueueRows(t, []queueRowFixture{
		{id: "t-next-bug", tags: "bug", payload: "b", priority: taskPriorityBug, createdAt: 100},           // claims 1st
		{id: "t-then-bug", tags: "bug", payload: "b", priority: taskPriorityBug, createdAt: 200},           // claims 2nd (FIFO within tier)
		{id: "t-old-feature", tags: "feature", payload: "f", priority: taskPriorityDefault, createdAt: 50}, // OLDEST, claims last
		{id: "t-ledger-only", tags: "bug", payload: "", priority: taskPriorityBug, createdAt: 10},          // no payload → unclaimable
	})

	rendered, err := TaskList(TaskStatusPending)
	if err != nil {
		t.Fatalf("TaskList pending: %v", err)
	}

	// ① newest-first, exactly like the unfiltered verb.
	got := renderedRowOrder(rendered)
	want := []string{"t-then-bug", "t-next-bug", "t-old-feature", "t-ledger-only"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("pending view order = %v, want newest-first %v\n%s", got, want, rendered)
	}

	if !strings.Contains(rendered, TaskListOrderNote) {
		t.Errorf("the filtered view must PRINT the order it is in (%q), so the help and the output agree:\n%s",
			strings.TrimSpace(TaskListOrderNote), rendered)
	}

	// ② exactly ONE [next], on the row the FUNNEL takes — not necessarily the
	// first rendered row. Counted over the ROW lines: the order note names the
	// marker token itself, and a substring count over the whole render would
	// count that sentence as a marker (it did, first run: 2 markers for 1 row).
	nextRows := 0
	for _, id := range got {
		if strings.Contains(renderedRowLine(rendered, id), TaskQueueNextMarker) {
			nextRows++
		}
	}
	if nextRows != 1 {
		t.Fatalf("[next] row markers = %d, want exactly 1\n%s", nextRows, rendered)
	}
	if ln := renderedRowLine(rendered, "t-next-bug"); !strings.Contains(ln, TaskQueueNextMarker) {
		t.Errorf("t-next-bug must carry [next] (the funnel's row), line = %q", ln)
	}
	if head := renderedRowLine(rendered, got[0]); strings.Contains(head, TaskQueueNextMarker) {
		t.Errorf("the TOP rendered row must NOT carry [next] when the funnel claims a different row — that is the Rule 33 contradiction\n%s", rendered)
	}

	// ③ the unclaimable row says so; a claimable low-tier row does not.
	if ln := renderedRowLine(rendered, "t-ledger-only"); !strings.Contains(ln, TaskQueueSkipMarker) {
		t.Errorf("payload-less row must carry [queue-skip], line = %q", ln)
	}
	if ln := renderedRowLine(rendered, "t-old-feature"); strings.Contains(ln, TaskQueueSkipMarker) {
		t.Errorf("a claimable low-tier row must NOT be marked skippable, line = %q", ln)
	}

	// THE PIN (Rule 33): the [next] row IS the first row the claim funnel
	// returns. Walk the funnel and compare against the marker, not against a
	// position in the render.
	res, err := TaskNext("worker", "")
	if err != nil {
		t.Fatalf("TaskNext: %v", err)
	}
	if res.Empty || res.Task == nil {
		t.Fatal("queue empty — the fixture seeds a claimable row")
	}
	if res.Task.TaskID != "t-next-bug" {
		t.Fatalf("TaskNext took %s, the [next] marker named t-next-bug\n%s", res.Task.TaskID, rendered)
	}
}

// Every view is newest-first — a status filter must not change the ORDER (the
// defect this epic fixes: `--status pending` silently rendered priority-first
// while the verb's help promised newest-first).
func TestStatusFilterDoesNotChangeOrder(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))

	seedQueueRows(t, []queueRowFixture{
		{id: "t-old", tags: "feature", payload: "f", priority: taskPriorityDefault, createdAt: 100},
		{id: "t-new", tags: "bug", payload: "b", priority: taskPriorityBug, createdAt: 300},
		{id: "t-mid", tags: "", payload: "m", priority: taskPriorityDefault, createdAt: 200},
	})

	all, err := TaskList("")
	if err != nil {
		t.Fatalf("TaskList all: %v", err)
	}
	order := renderedRowOrder(all)
	if strings.Join(order, ",") != "t-new,t-mid,t-old" {
		t.Fatalf("the unfiltered ledger must stay newest-first, got %v\n%s", order, all)
	}

	// The count header keeps its wire shape (leading integer == row count),
	// and the unfiltered view carries no order note — its order IS the verb's
	// documented one, so there is nothing to disambiguate.
	if !strings.HasPrefix(all, "3 task(s):\n") {
		t.Errorf("unfiltered header changed shape: %q", strings.SplitN(all, "\n", 2)[0])
	}
	if strings.Contains(all, TaskListOrderNote) {
		t.Errorf("the unfiltered view must not carry the filtered view's order note:\n%s", all)
	}
	pending, err := TaskList(TaskStatusPending)
	if err != nil {
		t.Fatalf("TaskList pending: %v", err)
	}
	if !strings.HasPrefix(pending, "3 task(s) with status=pending:\n") {
		t.Errorf("pending header changed shape: %q", strings.SplitN(pending, "\n", 2)[0])
	}
	// THE DEFECT'S OWN PIN: the same three rows, filtered, in the SAME order,
	// with the order named on the surface.
	pendingOrder := renderedRowOrder(pending)
	if strings.Join(pendingOrder, ",") != strings.Join(order, ",") {
		t.Fatalf("--status pending changed the row order: unfiltered %v vs filtered %v\n%s",
			order, pendingOrder, pending)
	}
}

// The claim order has ONE definition (Rule 14): the claim funnel and the
// pending view both reach taskClaimOrderBy — the funnel through its own query,
// the view through the [next] resolution it renders from. A literal re-inlined
// into either site silently re-opens the display/execution drift this closed.
//
// WHY THE PENDING VIEW'S SITE IS taskNextClaimID AND NOT A SPELLING
// (re-anchored 2026-09-28, gate-anchor drift). The first version of this gate
// pinned the BYTE SHAPE of the concatenation ("query += taskClaimOrderBy"), so
// when the pending view's order became newest-first (4973fd52) the marker's
// reader moved to taskNextClaimID — the function that decides WHICH row wears
// [next] — and the gate went red while the seam itself had not moved an inch:
// a claim about a BEHAVIOUR asserted against a spelling. The behaviour is "the
// row the pending view marks is the row the funnel claims", and the sites that
// carry it are the two functions that reach the clause. Both halves are
// asserted below: the reference inside taskNextClaimID, and that TaskList
// resolves its marker THROUGH that function rather than by position in its own
// render.
func TestClaimOrderHasOneDefinition(t *testing.T) {
	b, err := os.ReadFile("tasks.go")
	if err != nil {
		t.Fatalf("read tasks.go: %v", err)
	}
	src := string(b)
	if n := strings.Count(src, "ORDER BY COALESCE(priority"); n != 1 {
		t.Fatalf("the claim-order clause must be spelled ONCE (in taskClaimOrderBy), found %d occurrences", n)
	}
	if !strings.Contains(srcAtFunction(t, src, "TaskNextFiltered"), "taskClaimOrderBy") {
		t.Error("TaskNextFiltered must reference taskClaimOrderBy (the claim funnel is its authority)")
	}
	// The pending VIEW's reader: [next] is resolved by the funnel's own order,
	// so the marker names the row the funnel takes.
	if !strings.Contains(srcAtFunction(t, src, "taskNextClaimID"), "taskClaimOrderBy") {
		t.Error("taskNextClaimID must reference taskClaimOrderBy — it is the pending view's reader of the queue order ([next] names the row the funnel takes)")
	}
	if !strings.Contains(srcAtFunction(t, src, "TaskList"), "taskNextClaimID(") {
		t.Error("TaskList's pending view must resolve its [next] marker through taskNextClaimID (the funnel's order), never by position in its own render")
	}
}

// srcAtFunction returns the source of one top-level function, from its `func`
// line to the closing brace at column 0. A gate that names a SITE must read the
// SITE, not the file: "the file contains this string" is satisfied by any other
// function carrying it, which is how a reference can rot in place while its gate
// stays green.
func srcAtFunction(t *testing.T, src, name string) string {
	t.Helper()
	head := "func " + name + "("
	at := strings.Index(src, head)
	if at < 0 {
		t.Fatalf("gate anchor: %s is not defined in tasks.go — re-anchor this gate (the seam moved)", head)
	}
	rest := src[at:]
	if end := strings.Index(rest, "\n}\n"); end >= 0 {
		return rest[:end]
	}
	return rest
}
