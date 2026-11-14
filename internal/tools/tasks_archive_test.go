package tools

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// seedTaskRow is the test helper for the archive tests: TaskNew starts a row
// in_progress; update moves it along the state machine.
func seedTaskRow(t *testing.T, taskID, summary string) {
	t.Helper()
	if _, err := TaskNew(summary, taskID, "", ""); err != nil {
		t.Fatalf("TaskNew %s: %v", taskID, err)
	}
}

// TestTaskArchiveResolved: M4.5 ② — a terminal row gains the "resolved" tag
// (idempotent), a non-terminal row is REFUSED, and the archived row stays
// fully showable/searchable (tombstone, not a delete).
func TestTaskArchiveResolved(t *testing.T) {
	testutil.NewTestDBPath(t)

	seedTaskRow(t, "t-arch-1", "failed tombstone one")
	if _, err := TaskUpdate("t-arch-1", TaskStatusFailed); err != nil {
		t.Fatalf("fail t-arch-1: %v", err)
	}
	seedTaskRow(t, "t-arch-live", "still running")
	seedTaskRow(t, "t-arch-pend", "queued")

	// Happy path: failed → archived with resolved tag.
	out, err := TaskArchive("t-arch-1")
	if err != nil {
		t.Fatalf("TaskArchive: %v", err)
	}
	if !strings.Contains(out, "Task archived: t-arch-1") || !strings.Contains(out, "resolved") {
		t.Errorf("archive out = %q", out)
	}
	show, err := TaskShow("t-arch-1")
	if err != nil {
		t.Fatalf("TaskShow archived: %v", err)
	}
	if !strings.Contains(show, "resolved") {
		t.Errorf("archived row must keep the resolved tag:\n%s", show)
	}
	// Tombstone stays searchable.
	res, err := TaskSearch("tombstone")
	if err != nil || !strings.Contains(res, "t-arch-1") {
		t.Errorf("archived row must remain searchable, got %q err=%v", res, err)
	}
	// Idempotent: archiving again is a no-op.
	out, err = TaskArchive("t-arch-1")
	if err != nil || !strings.Contains(out, "already archived") {
		t.Errorf("second archive must be a no-op, got %q err=%v", out, err)
	}
	// Non-terminal rows are refused.
	if _, err := TaskArchive("t-arch-live"); err == nil || !strings.Contains(err.Error(), "not terminal") {
		t.Errorf("in_progress archive must be refused, got err=%v", err)
	}
	if _, err := TaskArchive("t-arch-pend"); err == nil || !strings.Contains(err.Error(), "not terminal") {
		t.Errorf("pending archive must be refused, got err=%v", err)
	}
	if _, err := TaskArchive("t-ghost"); err == nil || !strings.Contains(err.Error(), "no task") {
		t.Errorf("missing row archive must error, got err=%v", err)
	}
}

// TestTaskListVisibleHidesArchivedInEveryView is the minimal repro of the
// 2026-09-29 defect (archive-visibility): the same archived column had TWO
// verdicts depending on which surface rendered it. The CLI's `task list`
// filtered the tombstone out, while a caller that called TaskList bare —
// the conversational `/task list` — rendered it. The rule now is one
// function, tools.TaskListVisible, and this test pins BOTH halves of it:
// the DEFAULT view and an explicit --status view agree about one row, and
// includeArchived (`--all`) is the only spelling that brings it back.
//
// It also stands as the invariant the CLI and the slash command share: both
// call this function, so an assertion here is an assertion about both faces
// (the end-to-end halves live in cmd/agent — cli_tasks_test.go for the verb,
// commands_test.go for the slash view).
func TestTaskListVisibleHidesArchivedInEveryView(t *testing.T) {
	testutil.NewTestDBPath(t)

	seedTaskRow(t, "t-vis-live", "unarchived pending row")
	seedTaskRow(t, "t-vis-done", "archived completed row")
	if _, err := TaskUpdate("t-vis-done", TaskStatusInProgress); err != nil {
		t.Fatalf("in_progress: %v", err)
	}
	if _, err := TaskUpdate("t-vis-done", TaskStatusCompleted); err != nil {
		t.Fatalf("completed: %v", err)
	}
	if _, err := TaskArchive("t-vis-done"); err != nil {
		t.Fatalf("archive: %v", err)
	}

	// The raw render IS the datum: it carries the [resolved] marker the
	// filter reads (show/search keep rendering it — the tombstone is not
	// invisible to the ledger, only to the list views).
	raw, err := TaskList("")
	if err != nil {
		t.Fatalf("raw list: %v", err)
	}
	if !strings.Contains(raw, "t-vis-done") || !strings.Contains(raw, taskResolvedRowMarker) {
		t.Fatalf("the raw render must still carry the archived row and its marker:\n%s", raw)
	}

	// The DEFAULT view (no --status at all) — the view that used to render
	// the tombstone when it was reached without the CLI's filter.
	plain, err := TaskListVisible("", false)
	if err != nil {
		t.Fatalf("visible list: %v", err)
	}
	if strings.Contains(plain, taskResolvedRowMarker) {
		t.Errorf("the default view must not render an archived row:\n%s", plain)
	}
	if !strings.Contains(plain, "t-vis-live") {
		t.Errorf("an unarchived row must stay visible:\n%s", plain)
	}
	if got, rows := visibleHeaderCount(t, plain), countRenderedTaskListRows(strings.Split(plain, "\n")); got != rows || rows != 1 {
		t.Errorf("default view header=%d rows=%d, want 1/1 (post-filter count):\n%s", got, rows, plain)
	}

	// The SAME row through its own status filter: hidden for the same reason.
	// THIS pair is the defect's own acceptance criterion — one column, one
	// verdict, whichever view asks.
	byStatus, err := TaskListVisible(TaskStatusCompleted, false)
	if err != nil {
		t.Fatalf("visible completed list: %v", err)
	}
	if strings.Contains(byStatus, taskResolvedRowMarker) {
		t.Errorf("--status completed must not render the archived row:\n%s", byStatus)
	}
	if !strings.Contains(byStatus, taskListEmptyResult) {
		t.Errorf("the all-archived completed view must degrade to the empty text, got:\n%s", byStatus)
	}

	// includeArchived (`--all`) is the ONE spelling that keeps them.
	all, err := TaskListVisible("", true)
	if err != nil {
		t.Fatalf("visible --all list: %v", err)
	}
	if !strings.Contains(all, "t-vis-done") || !strings.Contains(all, taskResolvedRowMarker) {
		t.Errorf("includeArchived must keep the archived row with its marker:\n%s", all)
	}
}

// visibleHeaderCount parses the leading integer of a rendered TaskList block
// (the "<n> task(s)…" header) — the number the header claims.
func visibleHeaderCount(t *testing.T, rendered string) int {
	t.Helper()
	header, _, _ := strings.Cut(rendered, "\n")
	n, _, ok := taskListCountPrefix(header)
	if !ok {
		t.Fatalf("render does not open with a count header:\n%s", rendered)
	}
	return n
}

// TestFailedViewHidesArchived: M4.5 ② — `task list --status failed` renders
// archived rows with the [resolved] marker and the CLI filter drops them;
// --all keeps everything and unarchived failures stay visible.
func TestFailedViewHidesArchived(t *testing.T) {
	testutil.NewTestDBPath(t)

	seedTaskRow(t, "t-fail-1", "unresolved failure")
	if _, err := TaskUpdate("t-fail-1", TaskStatusFailed); err != nil {
		t.Fatalf("fail 1: %v", err)
	}
	seedTaskRow(t, "t-fail-2", "handled failure")
	if _, err := TaskUpdate("t-fail-2", TaskStatusFailed); err != nil {
		t.Fatalf("fail 2: %v", err)
	}
	if _, err := TaskArchive("t-fail-2"); err != nil {
		t.Fatalf("archive 2: %v", err)
	}

	failed, err := TaskList(TaskStatusFailed)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if !strings.Contains(failed, "t-fail-1") || !strings.Contains(failed, "t-fail-2") {
		t.Fatalf("engine list must render both rows (filter is CLI-side):\n%s", failed)
	}
	// The CLI-side default view (no --all) hides the resolved row AND its
	// header must count only the surviving rows (the 68-vs-7 defect).
	filtered := FilterArchivedRows(failed)
	if !strings.Contains(filtered, "t-fail-1") {
		t.Errorf("unresolved failure must stay visible:\n%s", filtered)
	}
	if strings.Contains(filtered, "t-fail-2") {
		t.Errorf("archived failure must leave the default view:\n%s", filtered)
	}
	// --all skips the filter entirely (cli_tasks.go) → the raw render keeps
	// both rows AND the unfiltered count.
	if got, rows := renderedHeaderCount(t, failed), countRenderedTaskListRows(strings.Split(failed, "\n")); got != rows || rows != 2 {
		t.Errorf("--all (unfiltered render) header=%d rows=%d, want 2/2:\n%s", got, rows, failed)
	}
	assertHeaderMatchesRows(t, filtered)
}

// renderedHeaderCount parses the leading integer of a rendered TaskList
// block's count header (the "<n> task(s)…" line) — the number the header
// claims.
func renderedHeaderCount(t *testing.T, rendered string) int {
	t.Helper()
	header, _, _ := strings.Cut(rendered, "\n")
	n, _, ok := taskListCountPrefix(header)
	if !ok {
		t.Fatalf("render does not open with a count header:\n%s", rendered)
	}
	return n
}

// assertHeaderMatchesRows pins the defect this suite exists for: the header
// count equals the number of "#"-prefixed row lines actually rendered.
func assertHeaderMatchesRows(t *testing.T, rendered string) {
	t.Helper()
	rows := countRenderedTaskListRows(strings.Split(rendered, "\n"))
	if got := renderedHeaderCount(t, rendered); got != rows {
		t.Errorf("header claims %d task(s) but %d rows render — the count must be post-archive-filter:\n%s", got, rows, rendered)
	}
}

// TestFilterArchivedRowsHeaderMatchesRows: the 68-vs-7 defect — a MIXED
// view (archived + live rows) must not keep TaskList's PRE-filter count in
// the header after FilterArchivedRows drops the archived rows.
func TestFilterArchivedRowsHeaderMatchesRows(t *testing.T) {
	testutil.NewTestDBPath(t)

	seedTaskRow(t, "t-mix-live", "still failing")
	if _, err := TaskUpdate("t-mix-live", TaskStatusFailed); err != nil {
		t.Fatalf("fail live: %v", err)
	}
	for _, id := range []string{"t-mix-a", "t-mix-b", "t-mix-c"} {
		seedTaskRow(t, id, "handled failure")
		if _, err := TaskUpdate(id, TaskStatusFailed); err != nil {
			t.Fatalf("fail %s: %v", id, err)
		}
		if _, err := TaskArchive(id); err != nil {
			t.Fatalf("archive %s: %v", id, err)
		}
	}
	failed, err := TaskList(TaskStatusFailed)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// Engine render: 4 rows, 4 in the header (the filter is CLI-side).
	assertHeaderMatchesRows(t, failed)

	filtered := FilterArchivedRows(failed)
	if rows := countRenderedTaskListRows(strings.Split(filtered, "\n")); rows != 1 {
		t.Fatalf("want exactly the live row to survive, got %d rows:\n%s", rows, filtered)
	}
	if !strings.Contains(filtered, "t-mix-live") {
		t.Errorf("live row must survive the filter:\n%s", filtered)
	}
	// THE regression: header == rendered rows (was 4 above 1 row).
	assertHeaderMatchesRows(t, filtered)
}

// TestFilterArchivedRowsHeaderNoStaleCount: a FULLY archived view must not
// leave a stale "N task(s) with status=…" header above nothing — it degrades
// to the standard empty text.
func TestFilterArchivedRowsHeaderNoStaleCount(t *testing.T) {
	testutil.NewTestDBPath(t)

	for _, id := range []string{"t-all-a", "t-all-b"} {
		seedTaskRow(t, id, "handled failure")
		if _, err := TaskUpdate(id, TaskStatusFailed); err != nil {
			t.Fatalf("fail %s: %v", id, err)
		}
		if _, err := TaskArchive(id); err != nil {
			t.Fatalf("archive %s: %v", id, err)
		}
	}
	failed, err := TaskList(TaskStatusFailed)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got := strings.TrimSpace(FilterArchivedRows(failed))
	// The empty text leads; the DISCLOSURE rides under it (the 2026-09-29
	// addition — an all-hidden view must not be byte-identical to an empty
	// ledger). The header itself must still be gone.
	if !strings.HasPrefix(got, taskListEmptyResult) {
		t.Errorf("fully archived view must render %q first, got:\n%s", taskListEmptyResult, got)
	}
	if !strings.Contains(got, TaskListArchiveHiddenNote(2)) {
		t.Errorf("fully archived view must disclose the rows it hid (want %q), got:\n%s", TaskListArchiveHiddenNote(2), got)
	}
	if strings.Contains(got, taskListCountMarker) {
		t.Errorf("stale count header survived the filter:\n%s", got)
	}
}

// TestRewriteTaskListCountPreservesBody: the count rewrite touches ONLY the
// header's leading integer — a body line that happens to contain the header
// shape (a task summary, say) is never mangled, and an unexpected render is
// passed through untouched.
func TestRewriteTaskListCountPreservesBody(t *testing.T) {
	body := "3 x task(s): a summary that looks like a header\n"
	rendered := "9 task(s) with status=failed:\n" + body
	got := rewriteTaskListCount(rendered, 7)
	if !strings.HasPrefix(got, "7 task(s) with status=failed:\n") {
		t.Errorf("header not rewritten:\n%s", got)
	}
	if !strings.HasSuffix(got, body) {
		t.Errorf("body must stay byte-identical, got:\n%s", got)
	}
	if out := rewriteTaskListCount("not a ledger block\n", 2); out != "not a ledger block\n" {
		t.Errorf("unexpected render must pass through, got %q", out)
	}
	if out := rewriteTaskListCount("no newline at all", 2); out != "no newline at all" {
		t.Errorf("headerless render must pass through, got %q", out)
	}
}

// TestFilterArchivedRowsEmptyResult: filtering the last resolved row leaves
// the "No tasks found." header intact rather than an empty block.
func TestFilterArchivedRowsEmptyResult(t *testing.T) {
	testutil.NewTestDBPath(t)

	seedTaskRow(t, "t-only-1", "sole failure")
	if _, err := TaskUpdate("t-only-1", TaskStatusFailed); err != nil {
		t.Fatalf("fail: %v", err)
	}
	if _, err := TaskArchive("t-only-1"); err != nil {
		t.Fatalf("archive: %v", err)
	}
	failed, err := TaskList(TaskStatusFailed)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got := strings.TrimSpace(FilterArchivedRows(failed))
	if !strings.Contains(got, "No tasks") {
		t.Errorf("all-resolved view must degrade to the empty header, got:\n%s", got)
	}
}

// ---------------------------------------------------------------------------
// The archived-row DISCLOSURE (2026-09-29): a filtered view must SAY how many
// rows it dropped (Rule 15).
// ---------------------------------------------------------------------------

// TestFilterArchivedRowsDisclosesHiddenCount is the repro of the SECOND half of
// the archive-visibility defect: the filter dropped rows SILENTLY, so
// `task list --status completed` over an archived ledger printed exactly
// "No tasks found." — byte-identical to a ledger that never had a completed
// row (measured 2026-09-29, docs/archive/demo/materials/01-list/05-finding.md F1.2).
// A read command that under-reports without saying so is the defect; the
// remedy is the disclosure line, not a different filter.
//
// Three shapes are pinned, because they are three different readers:
//
//	(a) a MIXED view — the disclosure rides under the surviving rows and is
//	    counted, so "1 task(s)" above one row still explains the 2 missing;
//	(b) a FULLY hidden view — the empty text must NOT stand alone; the reader
//	    is told the ledger has rows and the filter is why they are gone;
//	(c) an UNFILTERED render (nothing dropped) — no line at all: a view that
//	    hid nothing must not claim it did (the other direction of the same
//	    Rule 15 duty).
func TestFilterArchivedRowsDisclosesHiddenCount(t *testing.T) {
	testutil.NewTestDBPath(t)

	seedTaskRow(t, "t-hid-live", "still failing")
	if _, err := TaskUpdate("t-hid-live", TaskStatusFailed); err != nil {
		t.Fatalf("fail live: %v", err)
	}
	hidden := []string{"t-hid-a", "t-hid-b"}
	for _, id := range hidden {
		seedTaskRow(t, id, "handled failure")
		if _, err := TaskUpdate(id, TaskStatusFailed); err != nil {
			t.Fatalf("fail %s: %v", id, err)
		}
		if _, err := TaskArchive(id); err != nil {
			t.Fatalf("archive %s: %v", id, err)
		}
	}

	failed, err := TaskList(TaskStatusFailed)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// (c) the raw render (--all's view) hid nothing → no disclosure.
	if strings.Contains(failed, TaskListArchiveHiddenPrefix) {
		t.Errorf("an unfiltered view must not claim it hid rows:\n%s", failed)
	}

	// (a) the mixed view.
	filtered := FilterArchivedRows(failed)
	if !strings.Contains(filtered, TaskListArchiveHiddenNote(len(hidden))) {
		t.Errorf("a filtered view must publish the count it dropped (want %q):\n%s",
			TaskListArchiveHiddenNote(len(hidden)), filtered)
	}
	if !strings.Contains(filtered, "t-hid-live") {
		t.Errorf("the surviving row must still render:\n%s", filtered)
	}
	assertHeaderMatchesRows(t, filtered)

	// (b) everything archived: the empty text PLUS the disclosure, with the
	// raw header's status filter still named so the reader knows WHICH view
	// came back empty.
	for _, id := range []string{"t-hid-live"} {
		if _, err := TaskArchive(id); err != nil {
			t.Fatalf("archive %s: %v", id, err)
		}
	}
	raw, err := TaskList(TaskStatusFailed)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	empty := FilterArchivedRows(raw)
	if !strings.HasPrefix(empty, taskListEmptyResult) {
		t.Errorf("an all-filtered view must still say %q:\n%s", taskListEmptyResult, empty)
	}
	if !strings.Contains(empty, TaskListArchiveHiddenNote(len(hidden)+1)) {
		t.Errorf("the empty view must name the rows it hid (want %q):\n%s",
			TaskListArchiveHiddenNote(len(hidden)+1), empty)
	}
}

// TestTaskListArchiveHiddenNoteIsZeroSafe: the note is a DISCLOSURE, so it has
// no rendering for "nothing was dropped" — a caller that appends it blindly
// must get an empty string, never a line claiming zero rows were hidden.
func TestTaskListArchiveHiddenNoteIsZeroSafe(t *testing.T) {
	if got := TaskListArchiveHiddenNote(0); got != "" {
		t.Errorf("TaskListArchiveHiddenNote(0) = %q, want \"\" (a count of nothing is not a disclosure)", got)
	}
	if got := TaskListArchiveHiddenNote(3); !strings.HasPrefix(got, TaskListArchiveHiddenPrefix) {
		t.Errorf("TaskListArchiveHiddenNote(3) = %q, want the %q prefix", got, TaskListArchiveHiddenPrefix)
	}
}

// TestTaskListVisibleHidesArchivedInEveryStatusView drives the family help's
// own claim mechanically: "An ARCHIVED row … is hidden from EVERY list view,
// whatever --status says, and whether or not a status was given at all".
//
// The help is the promise; this is the promise checked against the predicate,
// for EVERY status the verb accepts plus the unfiltered view — one archived
// row per status, and the assertion that NO status view can render it while
// each unarchived sibling in the same view still does. A doc claim about
// "every" that is tested on one status is how the original defect survived
// (the help named `--status failed` while the filter ran everywhere); a table
// over the vocabulary is the shape that cannot.
func TestTaskListVisibleHidesArchivedInEveryStatusView(t *testing.T) {
	testutil.NewTestDBPath(t)

	// The statuses a row can be ARCHIVED in — task archive refuses
	// non-terminal rows, so these are exactly the views that can hold a
	// tombstone. Each gets a live sibling so the view is never empty for the
	// wrong reason.
	terminal := []string{TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled}
	for i, st := range terminal {
		live := "t-every-live-" + st
		gone := "t-every-gone-" + st
		seedTaskRow(t, live, "live "+st)
		seedTaskRow(t, gone, "archived "+st)
		// Both rows reach the terminal status through the state machine
		// (pending → in_progress → terminal).
		for _, id := range []string{live, gone} {
			if _, err := TaskUpdate(id, TaskStatusInProgress); err != nil {
				t.Fatalf("%s → in_progress: %v", id, err)
			}
			if _, err := TaskUpdate(id, st); err != nil {
				t.Fatalf("%s → %s: %v", id, st, err)
			}
		}
		if _, err := TaskArchive(gone); err != nil {
			t.Fatalf("archive %s (%d): %v", gone, i, err)
		}

		view, err := TaskListVisible(st, false)
		if err != nil {
			t.Fatalf("TaskListVisible(%s): %v", st, err)
		}
		if !strings.Contains(view, live) {
			t.Errorf("--status %s must render its unarchived row:\n%s", st, view)
		}
		if strings.Contains(view, gone) {
			t.Errorf("--status %s rendered an archived row — the help says EVERY view hides it:\n%s", st, view)
		}
		if !strings.Contains(view, TaskListArchiveHiddenNote(1)) {
			t.Errorf("--status %s must disclose the row it hid (want %q):\n%s", st, TaskListArchiveHiddenNote(1), view)
		}
	}

	// "whether or not a status was given at all": the UNFILTERED view hides
	// them too, and discloses all three.
	plain, err := TaskListVisible("", false)
	if err != nil {
		t.Fatalf("TaskListVisible default: %v", err)
	}
	for _, st := range terminal {
		if strings.Contains(plain, "t-every-gone-"+st) {
			t.Errorf("the unfiltered view rendered an archived row:\n%s", plain)
		}
	}
	if !strings.Contains(plain, TaskListArchiveHiddenNote(len(terminal))) {
		t.Errorf("the unfiltered view must disclose all %d hidden rows:\n%s", len(terminal), plain)
	}
}
