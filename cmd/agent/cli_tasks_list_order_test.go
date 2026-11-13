package main

// cli_tasks_list_order_test.go — the `gopi task list` ORDER SURFACE contract
// (epic task-list-order-surface, 2026-09-26).
//
// THE DEFECT this file pins: the verb's help promised "List tasks, newest
// first" while `--status pending` silently rendered the CLAIM order
// (priority-first) — the usage line carrying `--status S` advertised one order
// and the flag produced another, with nothing said. The fix makes the two
// channels agree by rendering EVERY view newest-first and NAMING both orders on
// the surface (the filtered view prints its own order note, the help carries
// the prose, and the pending rows keep their [next]/[queue-skip] markers).
//
// WHY IT DRIVES THE REAL BINARY: the promise is made by `-h` and the order is
// printed by argv-driven output — a unit test on the renderer would prove
// neither channel, and the module's own standard (PROJECT.md Rule 31) says a
// surface not executed is not delivered. The fixture goes through the PUBLIC
// CLI (`task submit`), so the rows are real ledger rows with real timestamps.
// The seed is ordered so the two orders DISAGREE: the oldest row is the bug
// tier (claimed first), the newest is the docs tier (claimed last).

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// listOrderFixtureBaseCreatedAt is the epoch-second base the fixture rows are
// stamped with (listOrderFixtureStride apart, one row each). A fixture VALUE,
// never a clock read: the three rows must carry distinct created_at seconds or
// "newest first" would fall back to the rowid tiebreak and the assertion could
// not tell the two orders apart.
const (
	listOrderFixtureBaseCreatedAt = 100000
	listOrderFixtureStride        = 60
)

// listOrderFixtureRow is one seeded row: the summary/tags a user would type.
type listOrderFixtureRow struct {
	summary string
	tags    string
}

// listOrderFixture is the fixture, OLDEST FIRST. The claim order (priority
// first) and the render order (newest first) are therefore exact reverses of
// each other, which is what makes a wrong order visible: the oldest row is the
// bug tier (claimed first) and the newest is the docs tier (claimed last).
func listOrderFixture() []listOrderFixtureRow {
	return []listOrderFixtureRow{
		{summary: "order-fixture-oldest-bug", tags: "bug"},
		{summary: "order-fixture-middle-default"},
		{summary: "order-fixture-newest-docs", tags: "docs"},
	}
}

// seedListOrderLedger submits the fixture rows through the REAL CLI into dbPath,
// then stamps each row's created_at so the three are exactly one stride apart,
// and returns their task ids, oldest first.
//
// The stamp goes through the ledger directly because created_at is stamped by
// the CLOCK on the public path and the claim/render orders must be decidable
// from the fixture alone — the same reason internal/tools' queue fixtures insert
// raw SQL. Stamping (rather than sleeping between submits) keeps the test
// deterministic and clock-free.
func seedListOrderLedger(t *testing.T, bin, home, dbPath string) []string {
	t.Helper()
	rows := listOrderFixture()
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		args := []string{CliVerbTask, taskVerbSubmit, row.summary, "--db", dbPath, "--source", "human"}
		if row.tags != "" {
			args = append(args, "--tags", row.tags)
		}
		out, err := testutil.FixtureCommandEnv(bin, cliContractEnv(home), args...)
		if err != nil {
			t.Fatalf("task submit %q: %v\n%s", row.summary, err, out)
		}
		ids = append(ids, submitTaskIDFromOutput(t, string(out)))
	}
	stampListOrderCreatedAt(t, dbPath, ids)
	return ids
}

// stampListOrderCreatedAt writes the fixture's created_at values: row i gets
// listOrderFixtureBaseCreatedAt + i*listOrderFixtureStride.
func stampListOrderCreatedAt(t *testing.T, dbPath string, idsOldestFirst []string) {
	t.Helper()
	db, err := sql.Open("sqlite", tools.LedgerDSN(dbPath))
	if err != nil {
		t.Fatalf("open fixture ledger: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close fixture ledger: %v", err)
		}
	}()
	for i, id := range idsOldestFirst {
		at := float64(listOrderFixtureBaseCreatedAt + i*listOrderFixtureStride)
		if _, err := db.Exec(`UPDATE tasks SET created_at = ?, updated_at = ? WHERE task_id = ?`, at, at, id); err != nil {
			t.Fatalf("stamp created_at for %s: %v", id, err)
		}
	}
}

// submitTaskIDFromOutput reads the queued row's id off `task submit`'s stdout.
func submitTaskIDFromOutput(t *testing.T, out string) string {
	t.Helper()
	const marker = "Task queued: "
	i := strings.Index(out, marker)
	if i < 0 {
		t.Fatalf("task submit printed no %q line:\n%s", marker, out)
	}
	id, _, ok := strings.Cut(out[i+len(marker):], " ")
	if !ok || strings.TrimSpace(id) == "" {
		t.Fatalf("task submit's queued line carries no task id:\n%s", out)
	}
	return strings.TrimSpace(id)
}

// taskListRowIDs returns the task ids of the RENDERED row lines, in printed
// order. Row lines start with the ledger's row prefix; the count header and the
// order note are not rows.
func taskListRowIDs(rendered string) []string {
	var ids []string
	for _, ln := range strings.Split(rendered, "\n") {
		if !strings.HasPrefix(ln, tools.TaskListRowPrefix) {
			continue
		}
		fields := strings.Fields(ln)
		if len(fields) == 0 {
			continue
		}
		ids = append(ids, strings.TrimPrefix(fields[0], tools.TaskListRowPrefix))
	}
	return ids
}

// taskListMarkedRow returns the id of the row whose line carries marker.
func taskListMarkedRow(rendered, marker string) string {
	for _, ln := range strings.Split(rendered, "\n") {
		if !strings.HasPrefix(ln, tools.TaskListRowPrefix) || !strings.Contains(ln, marker) {
			continue
		}
		fields := strings.Fields(ln)
		if len(fields) > 0 {
			return strings.TrimPrefix(fields[0], tools.TaskListRowPrefix)
		}
	}
	return ""
}

// TestTaskListFilteredOrderMatchesHelp is THE acceptance test of the epic. On
// the REAL binary it asserts:
//
//	① `--status pending` prints the rows newest-first (created_at DESC);
//	② the unfiltered verb prints the SAME order — the two channels agree;
//	③ the printed surface NAMES both orders, so the claim order a dispatcher
//	   uses cannot be mistaken for the render order in silence;
//	④ the row marked [next] is the row the CLAIM FUNNEL takes (Rule 33), which
//	   is NOT the top printed line — the fixture's whole point.
func TestTaskListFilteredOrderMatchesHelp(t *testing.T) {
	bin := gopiTestBinPath(t)
	home := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")

	ids := seedListOrderLedger(t, bin, home, dbPath)

	// ids is oldest-first, so the expected newest-first order IS the reverse.
	wantNewestFirst := []string{ids[2], ids[1], ids[0]}

	pendingOut, err := testutil.FixtureCommandEnv(bin, cliContractEnv(home),
		CliVerbTask, taskVerbList, "--status", tools.TaskStatusPending, "--db", dbPath)
	if err != nil {
		t.Fatalf("task list --status pending: %v\n%s", err, pendingOut)
	}
	pending := string(pendingOut)
	if got := taskListRowIDs(pending); strings.Join(got, ",") != strings.Join(wantNewestFirst, ",") {
		t.Fatalf("`task list --status pending` printed order = %v, want newest-first %v\n%s",
			got, wantNewestFirst, pending)
	}

	// ② the unfiltered channel prints the same order — the promise the help
	// makes for the VERB, whose own usage line carries --status.
	allOut, err := testutil.FixtureCommandEnv(bin, cliContractEnv(home), CliVerbTask, taskVerbList, "--db", dbPath)
	if err != nil {
		t.Fatalf("task list: %v\n%s", err, allOut)
	}
	all := string(allOut)
	if got := taskListRowIDs(all); strings.Join(got, ",") != strings.Join(wantNewestFirst, ",") {
		t.Fatalf("unfiltered `task list` printed order = %v, want newest-first %v\n%s",
			got, wantNewestFirst, all)
	}

	// ③ the surface SAYS SO — the defect was the SILENCE, not the ordering.
	for _, want := range []string{tools.TaskListOrderNewestFirstPhrase, tools.TaskListOrderClaimPhrase} {
		if !strings.Contains(pending, want) {
			t.Errorf("the filtered view must name %q on its rendered surface:\n%s", want, pending)
		}
	}

	// ④ Rule 33: [next] names the row the claim funnel takes. The claim order
	// here is the REVERSE of the printed order, so the marker must NOT sit on
	// the top line — that contradiction is what this fixture catches.
	nextID := taskListMarkedRow(pending, tools.TaskQueueNextMarker)
	if nextID == "" {
		t.Fatalf("no row carries %q:\n%s", tools.TaskQueueNextMarker, pending)
	}
	if nextID != ids[0] {
		t.Fatalf("%q names %s, want the oldest (bug-tier) row %s\n%s",
			strings.TrimSpace(tools.TaskQueueNextMarker), nextID, ids[0], pending)
	}
	gotPending := taskListRowIDs(pending)
	if gotPending[0] == nextID {
		t.Errorf("the [next] row must not be the TOP printed line once the render is chronological:\n%s", pending)
	}
	if got := claimsFirstTask(t, dbPath); got != nextID {
		t.Fatalf("the claim funnel takes %s while the [next] marker names %s\n%s", got, nextID, pending)
	}
}

// claimsFirstTask returns the task_id a dispatcher claims next, read through the
// funnel the worker round itself calls (tools.TaskNext) against the same ledger
// — the marker is compared with the funnel, not with a second render.
//
// GOPI_MEMORY_DB is set for the ENGINE call only; the process env is restored
// through the reviewed os.Setenv pair (internal/testutil.SnapshotEnv), because
// t.Setenv is banned in a helper that a subprocess-driving test also uses.
func claimsFirstTask(t *testing.T, dbPath string) string {
	t.Helper()
	restore := testutil.SnapshotEnv(t, memoryDBEnv)
	if err := os.Setenv(memoryDBEnv, dbPath); err != nil { //stylegate:ignore the flip must undo when THIS CALL returns, not at test cleanup // env-leak-ok: setter half of the reviewed testutil.SnapshotEnv pair above (restores absence), and this helper is used by a subprocess-driving test where t.Setenv's cleanup scope is wrong
		t.Fatalf("pin ledger: %v", err)
	}
	defer restore()
	res, err := tools.TaskNext("worker", "")
	if err != nil {
		t.Fatalf("TaskNext: %v", err)
	}
	if res.Empty || res.Task == nil {
		t.Fatalf("the claim funnel reports an EMPTY queue while %s holds three pending rows", dbPath)
	}
	return res.Task.TaskID
}

// TestTaskListHelpNamesBothOrders pins the help SURFACE itself: the help text is
// the oracle a user reads before typing, so it must name the render order, the
// claim order, and both queue markers.
func TestTaskListHelpNamesBothOrders(t *testing.T) {
	for _, want := range []string{
		tools.TaskListOrderNewestFirstPhrase,
		tools.TaskListOrderClaimPhrase,
		tools.TaskQueueNextMarker,
		tools.TaskQueueSkipMarker,
	} {
		if !strings.Contains(tasksUsage, want) {
			t.Errorf("tasksUsage must name %q — the help is the promise the render must keep", want)
		}
	}
}
