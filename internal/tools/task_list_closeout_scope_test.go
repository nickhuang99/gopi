package tools

// task_list_closeout_scope_test.go — THE RED PROOF for epic 01a12bc0: the
// close-out header's COUNT must be the number of marker lines the SAME render
// printed.
//
// WHAT WAS MEASURED WRONG (host-a, 2026-10-12, `gopi task list`):
//
//	--status failed     header "close-out refused on 54 row(s)" — 0 marked rows on screen
//	--status cancelled  header "close-out refused on 54 row(s)" — 1 marked row on screen
//	--all / unfiltered  header "close-out refused on 54 row(s)" — 54 marked rows
//
// The count was taken by ONE ledger-global query issued before the row cursor
// opened (taskListCloseoutSignals did not join tasks.status), while the rows
// under it were walked with `WHERE status = ?`. A header count and the rows it
// heads were two different populations, and the sentence carried no scope
// clause — so a reader had no way to learn that the number was not about the
// list below it.
//
// THE PROPERTY THESE TESTS PIN, and it is one property in three states: for a
// render, count(marker lines) == the integer in the header sentence. It is
// asserted on the RENDERED TEXT only (never on the query), so it is exactly the
// arithmetic a reader can redo by eye:
//
//	• --status failed    (a filtered view where NO row is marked)
//	• --status pending   (a filtered view where the marked row is the only one)
//	• ""                 (the unfiltered view — every row the ledger holds)
//
// RED PROOF: every test in this file compiles and runs against the PRE-CHANGE
// tree (it uses only TaskList, the seed helpers and the marker constant), and
// each fails there — see the report's `## Evidence (self-filed)`.

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// closeoutMarkedRowCount counts the rows of a rendered list carrying the
// close-out tail, matched on the TAIL'S OWN SHAPE (` [closeout-refused]: …` or
// a row ending at the bare marker) rather than on the bare token: a row whose
// SUMMARY quotes the token would otherwise be counted as a marked row, which is
// the same class of over-count this epic is about.
func closeoutMarkedRowCount(rendered string) int {
	n := 0
	for _, ln := range strings.Split(rendered, "\n") {
		if !strings.HasPrefix(ln, TaskListRowPrefix) {
			continue
		}
		if strings.HasSuffix(ln, " "+taskListCloseoutMarker) ||
			strings.Contains(ln, " "+taskListCloseoutMarker+taskListCloseoutTailSeparator) {
			n++
		}
	}
	return n
}

// closeoutHeaderCount reads the integer the header sentence carries, and the
// header's presence. ok=false means the render carried no close-out header at
// all (the zero contract), which the callers below treat as a count of 0.
func closeoutHeaderCount(rendered string) (int, bool) {
	for _, ln := range strings.Split(rendered, "\n") {
		if !strings.HasPrefix(ln, "close-out refused on ") {
			continue
		}
		rest := strings.TrimPrefix(ln, "close-out refused on ")
		digits := rest
		if i := strings.Index(rest, " "); i >= 0 {
			digits = rest[:i]
		}
		n, err := strconv.Atoi(digits)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// closeoutSeedStatus writes one row, its trail events (in order) and its
// STATUS — the third thing the pre-change reader never joined on.
func closeoutSeedStatus(t *testing.T, dbPath, id, status string, events ...[2]string) {
	t.Helper()
	closeoutSeedRow(t, dbPath, id, events...)
	ctx := context.Background()
	db, err := openTasksDB(ctx, dbPath)
	if err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET status = ? WHERE task_id = ?`, status, id); err != nil {
		t.Fatalf("set status of %s: %v", id, err)
	}
}

// TestTaskListCloseoutHeaderCountMatchesItsOwnRows is the epic's property, in
// the three states the briefing names. The seeded ledger holds four rows: one
// `failed` (NOT refused — the measured 54/0 shape), one `cancelled` (refused),
// one `pending` (refused) and one `completed` (refused) — so each view's OWN
// population differs from the ledger's, which is what makes a ledger-global
// count visible as a wrong number.
func TestTaskListCloseoutHeaderCountMatchesItsOwnRows(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	closeoutSeedStatus(t, path, "t-failed-plain", taskStatusFailed,
		[2]string{taskStatusCompleted, "gopi run completed"})
	closeoutSeedStatus(t, path, "t-cancelled-refused", taskStatusCancelled,
		[2]string{finishCleanupRemovalRefusedEvent, closeoutRefusal})
	closeoutSeedStatus(t, path, "t-pending-refused", taskStatusPending,
		[2]string{finishCleanupRemovalRefusedEvent, closeoutRefusal})
	closeoutSeedStatus(t, path, "t-completed-refused", taskStatusCompleted,
		[2]string{finishCleanupRemovalRefusedEvent, closeoutRefusal})

	cases := []struct {
		name           string
		statusFilter   string
		wantMarkers    int
		wantHeaderData bool
	}{
		// A filtered view where NOTHING is marked: the pre-change render put a
		// count of 3 here (every refused row in the ledger) over an empty
		// screen — the `--status failed` shape, measured 54/0 on this host.
		{"filtered view with no marked row", taskStatusFailed, 0, false},
		{"filtered view whose only marked row is its own", taskStatusPending, 1, true},
		{"unfiltered view counts every row it walks", "", 3, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := TaskList(tc.statusFilter)
			if err != nil {
				t.Fatalf("TaskList(%q): %v", tc.statusFilter, err)
			}
			markers := closeoutMarkedRowCount(out)
			if markers != tc.wantMarkers {
				t.Fatalf("seeded ledger rendered %d marked rows, want %d:\n%s", markers, tc.wantMarkers, out)
			}
			header, ok := closeoutHeaderCount(out)
			if ok != tc.wantHeaderData || header != markers {
				t.Errorf("header says %d row(s) (present=%v) while %d marker line(s) are on screen — the count and its rows are different populations:\n%s",
					header, ok, markers, out)
			}
		})
	}
}

// TestTaskListCloseoutHeaderCountIsScopedToTheViewNotTheLedger is the same
// property stated as the DEFECT: the filtered view must NOT report the ledger's
// number. It seeds five refused rows in a status the view does not walk, so a
// ledger-global count (5) is unmistakably larger than the view's own (0 — and
// the render then carries no header at all, the zero contract).
func TestTaskListCloseoutHeaderCountIsScopedToTheViewNotTheLedger(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	for _, id := range []string{"t-a", "t-b", "t-c", "t-d", "t-e"} {
		closeoutSeedStatus(t, path, id, taskStatusCancelled,
			[2]string{finishCleanupRemovalRefusedEvent, closeoutRefusal})
	}
	closeoutSeedStatus(t, path, "t-view", taskStatusFailed,
		[2]string{taskStatusCompleted, "gopi run completed"})

	out, err := TaskList(taskStatusFailed)
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	if n, ok := closeoutHeaderCount(out); ok {
		t.Errorf("a view with no refused row of its own rendered a close-out header of %d — it counted the ledger:\n%s", n, out)
	}
	if n := closeoutMarkedRowCount(out); n != 0 {
		t.Fatalf("the failed view rendered %d marked rows, want 0:\n%s", n, out)
	}
	// The counterpart: the view that DOES hold them reports exactly its own.
	out, err = TaskList(taskStatusCancelled)
	if err != nil {
		t.Fatalf("TaskList(cancelled): %v", err)
	}
	n, ok := closeoutHeaderCount(out)
	if !ok || n != 5 {
		t.Errorf("the cancelled view says %d row(s) (present=%v), want 5:\n%s", n, ok, out)
	}
	if got := closeoutMarkedRowCount(out); got != 5 {
		t.Errorf("the cancelled view rendered %d marked rows, want 5:\n%s", got, out)
	}
}

// TestTaskListCloseoutHeaderSurvivesTheArchivedFilter pins the boundary the fix
// does NOT move: TaskListVisible drops archived (resolved) rows AFTER the
// render, and a dropped row takes its marker with it. The header is stamped
// inside TaskList from what TaskList printed, so a `--all` render with a
// resolved marked row is the one shape where the printed count legitimately
// exceeds the visible rows — and this test records that it is the ARCHIVE
// filter (a row the view says it hid), not the status scope, that must explain
// the difference.
func TestTaskListCloseoutHeaderSurvivesTheArchivedFilter(t *testing.T) {
	path := testutil.NewTestDBPath(t)
	closeoutSeedStatus(t, path, "t-resolved-refused", taskStatusCompleted,
		[2]string{finishCleanupRemovalRefusedEvent, closeoutRefusal})
	ctx := context.Background()
	db, err := openTasksDB(ctx, path)
	if err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET tags = ? WHERE task_id = ?`, taskTagResolved, "t-resolved-refused"); err != nil {
		db.Close() //stylegate:ignore closes the fixture's ledger handle on the already-failing t.Fatalf path below; the tag edit that failed is the reported error
		t.Fatalf("tag resolved: %v", err)
	}
	db.Close() //stylegate:ignore closes the same handle after the assertions on the tag edit were made

	all, err := TaskListVisible("", true)
	if err != nil {
		t.Fatalf("TaskListVisible(all): %v", err)
	}
	if n, ok := closeoutHeaderCount(all); !ok || n != 1 {
		t.Errorf("--all says %d (present=%v), want 1 — the row IS walked and IS marked:\n%s", n, ok, all)
	}
	if n := closeoutMarkedRowCount(all); n != 1 {
		t.Errorf("--all rendered %d marked rows, want 1:\n%s", n, all)
	}
	// The archived-filtered view hides the row AND its marker; the close-out
	// header goes with them (0 marked → no header), which is the binding this
	// fix keeps: the number never outlives the rows it counted.
	visible, err := TaskListVisible("", false)
	if err != nil {
		t.Fatalf("TaskListVisible(): %v", err)
	}
	if n := closeoutMarkedRowCount(visible); n != 0 {
		t.Fatalf("the archived row was rendered, want it hidden:\n%s", visible)
	}
}
