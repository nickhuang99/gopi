package tools

import (
	"os"
	"strings"
	"testing"
	"time"
)

// Regression: task list must show the running start (claimed_at wall clock)
// and elapsed duration on in_progress rows ONLY — pending (no claim) and
// terminal rows (elapsed already fixed at end) carry no such field
// (inapplicable = not rendered, per the M4.12 display-layer epic).
//
// The LABEL on that tail is honest (task-list-state-honesty): a row whose
// run cannot be shown to be alive renders "unknown (<missing signal>)" /
// "died" instead of the pre-change blanket "running". These two tests pin the
// ELAPSED STORY (claimed_at wall clock, monotonic across the stale re-arm
// bump); the live label itself is pinned in taskstate_test.go.

func TestTaskListRunningSuffixOnInProgress(t *testing.T) {
	path := t.TempDir() + "/memory.db"
	t.Setenv("GOPI_MEMORY_DB", path)

	if _, err := TaskNew("claimed work", "t-run-1", "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	// Simulate a claim started 2h13m ago: claim + move to in_progress, then
	// backdate claimed_at (the DB is the only clock the renderer sees). The
	// liveness probe is stubbed ON so the row renders the LIVE label — a
	// `go test` process is not named `gopi` for the pid-reuse guard.
	now := time.Now()
	db, err := openTasksDB(t.Context(), path)
	if err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}
	claimed := now.Add(-2*time.Hour - 13*time.Minute)
	if _, err := db.Exec(`UPDATE tasks SET status = 'in_progress', claimed_by = ?, claimed_at = ?,
		updated_at = ?, in_progress_at = ? WHERE task_id = 't-run-1'`,
		taskStateTestHolder(os.Getpid()), float64(claimed.Unix()), float64(now.Unix()), float64(claimed.Unix())); err != nil {
		t.Fatalf("backdate claimed_at: %v", err)
	}
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	restore := stubLiveHolderProbe(t, true)
	defer restore()

	out, err := TaskList("in_progress")
	if err != nil {
		t.Fatalf("TaskList in_progress: %v", err)
	}
	if !strings.Contains(out, "["+taskStateRunning+" 2h13m") || !strings.Contains(out, "since") {
		t.Errorf("in_progress row missing running start+duration:\n%s", out)
	}
	// The since stamp is the claimed_at wall clock (HH:MM:SS, local).
	wantClock := claimed.Format("15:04:05")
	if !strings.Contains(out, "since "+wantClock) {
		t.Errorf("running suffix must show claimed_at start %s:\n%s", wantClock, out)
	}
}

func TestTaskListNoRunningFieldOnPendingAndTerminal(t *testing.T) {
	path := t.TempDir() + "/memory.db"
	t.Setenv("GOPI_MEMORY_DB", path)

	for _, id := range []string{"t-done"} {
		if _, err := TaskNew("row "+id, id, "", ""); err != nil {
			t.Fatalf("TaskNew %s: %v", id, err)
		}
	}
	if _, err := TaskUpdate("t-done", "completed"); err != nil {
		t.Fatalf("TaskUpdate: %v", err)
	}
	// A real pending row: TaskNew models auto-registered session work
	// (in_progress), so seed the queue-side status directly.
	db, err := openTasksDB(t.Context(), path)
	if err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}
	nowF := float64(time.Now().Unix())
	if _, err := db.Exec(`INSERT INTO tasks (task_id, summary, status, created_at, updated_at)
		VALUES ('t-pend', 'row t-pend', 'pending', ?, ?)`, nowF, nowF); err != nil {
		t.Fatalf("seed pending row: %v", err)
	}
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	all, err := TaskList("")
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	for _, line := range strings.Split(all, "\n") {
		if strings.Contains(line, "[running") {
			t.Errorf("non-in_progress row must not render a running field: %s", line)
		}
	}
	if !strings.Contains(all, "[pending]") || !strings.Contains(all, "[completed]") {
		t.Errorf("fixture broken — want one pending and one completed row:\n%s", all)
	}
}

func TestTaskFormatDuration(t *testing.T) {
	cases := map[time.Duration]string{
		38 * time.Second:                "38s",
		38200 * time.Millisecond:        "38.2s",
		45*time.Minute + 12*time.Second: "45m12s",
		2*time.Hour + 13*time.Minute:    "2h13m0s",
		1500 * time.Millisecond:         "1.5s",
		700 * time.Millisecond:          "700ms",
	}
	for d, want := range cases {
		if got := taskFormatDuration(d); got != want {
			t.Errorf("taskFormatDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestTaskRunningSuffixInapplicableStatuses(t *testing.T) {
	now := time.Now()
	for _, status := range []string{"pending", "completed", "failed", "verified", "reopened" /* legacy */, "blocked", "cancelled"} {
		tk := &Task{Status: status, ClaimedAt: float64(now.Unix()), HasClaim: true}
		if got := taskRunningSuffix(tk, now); got != "" {
			t.Errorf("status %s must render no running suffix, got %q", status, got)
		}
	}
}

// TestTaskRunningAnchorOrder pins the anchor ladder: claimed_at (authoritative
// claim moment) → tasklist_issued_at (harness [TASK_LIST] bookkeeping) →
// created_at (row creation == execution start for auto-registered rows).
// updated_at is NEVER an anchor — it is a write-churn column (transitions,
// backfills, sync merges, stale re-arm bump) and is not a start signal.
func TestTaskRunningAnchorOrder(t *testing.T) {
	cases := []struct {
		name string
		t    *Task
		want float64
	}{
		{"claimed_at wins", &Task{HasClaim: true, ClaimedAt: 100, HasTasklistIssue: true, TasklistIssuedAt: 200, CreatedAt: 300}, 100},
		{"tasklist_issued_at when no claim", &Task{HasTasklistIssue: true, TasklistIssuedAt: 200, CreatedAt: 300}, 200},
		{"created_at when neither", &Task{CreatedAt: 300}, 300},
		{"created_at when claimed_at zero", &Task{HasClaim: true, ClaimedAt: 0, HasTasklistIssue: true, TasklistIssuedAt: 200, CreatedAt: 300}, 200},
		{"updated_at must never anchor even when newest", &Task{HasClaim: true, ClaimedAt: 100, UpdatedAt: 999, CreatedAt: 300}, 100},
	}
	for _, c := range cases {
		if got := taskRunningAnchor(c.t); got != c.want {
			t.Errorf("%s: taskRunningAnchor = %v, want %v", c.name, got, c.want)
		}
	}
}

// The zombie fixture's age at every assertion below: the row started
// TASK_LIST zombie ageAgo and the CONTROLLED now is the same instant on both
// sides of every comparison, so the expected string is a constant the code
// under test can be held to exactly. The pre-fix test derived its expectation
// from `time.Since(start)` — a live wall-clock sample compared for STRING
// equality against a render taken at a DIFFERENT instant. `taskFormatDuration`
// truncates >=1m durations to `time.Second`, so a >1s scheduling pause between
// the two reads (routine on a loaded host, while the whole package runs
// concurrently) moved the expected "4h0m0s" to "4h0m1s" and failed the test on
// evidence about the HOST, not the renderer. Measured: a 1.1s pause inserted
// between the samples made the old form fail 3/3 deterministically.
const (
	// zombieControlledNowUnix is a FIXED instant, never a clock reading: it is
	// fed to the renderer as `now`, so every expected string below is a
	// constant and the host's clock is not an input to any assertion.
	zombieControlledNowUnix = 1791810000
	zombieRearmAge          = 4 * time.Hour
	zombieRearmAgeSec       = int64(zombieRearmAge / time.Second)
	zombieRearmAgeLabel     = "4h0m0s" // == taskFormatDuration(zombieRearmAge)
	// zombieApproxMarker is the prefix a NON-real anchor would carry. No
	// renderer emits it today; the assertion keeps it as the tripwire for an
	// anchor that degrades into an approximation.
	zombieApproxMarker = "~"
)

// TestTaskRunningSuffixMonotonicAcrossRearm is the zombie-row regression: an
// in_progress row with EMPTY claimed_at (the claim never completed — the
// runner died mid-claim) and a frozen heartbeat. The stale sweep re-arms such
// a row every ~10 min by bumping updated_at; the running display must anchor
// on the REAL start (tasklist_issued_at, else created_at) so the zombie shows
// its true age monotonically instead of "restarting" at every re-arm bump.
//
// The age is asserted twice, on two different surfaces, because the property
// has two halves:
//
//   - the DB path (TaskList) must render that age at all, and must render the
//     row's REAL start identically after each re-arm — the `created <stamp>`
//     the row carries is read back through the live list, so an anchor that
//     moved on disk shows up here (this half never consults a clock at all);
//   - the render path (taskStateSuffix) must produce the EXACT age string from
//     an anchor plus a CONTROLLED now. This half is what the re-arm bump is
//     aimed at; driving it with a fixed now keeps the assertion exact while
//     removing the host's clock from it.
func TestTaskRunningSuffixMonotonicAcrossRearm(t *testing.T) {
	path := t.TempDir() + "/memory.db"
	t.Setenv("GOPI_MEMORY_DB", path)

	db, err := openTasksDB(t.Context(), path)
	if err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}
	// A zombie: in_progress, no claimed_at/claimed_by (claim never
	// completed), execution started zombieRearmAge ago (created_at ==
	// tasklist_issued_at == the run's real start), heartbeat frozen. Its
	// label is honest ("unknown (no claim stamp)") — what this test pins is
	// that the ELAPSED AGE anchored on the real start never moves when the
	// stale sweep re-arms updated_at.
	start := time.Unix(zombieControlledNowUnix-zombieRearmAgeSec, 0)
	if _, err := db.Exec(`INSERT INTO tasks (task_id, summary, status, claimed_by, created_at, updated_at, tasklist_issued_at, last_toolcall_at)
		VALUES ('t-zombie', 'zombie row', 'in_progress', '', ?, ?, ?, ?)`,
		float64(start.Unix()), float64(start.Unix()), float64(start.Unix()), float64(start.Unix())); err != nil {
		t.Fatalf("seed zombie: %v", err)
	}
	rearm := func(minutes int) {
		if _, err := db.Exec(`UPDATE tasks SET updated_at = ? WHERE task_id = 't-zombie'`,
			float64(start.Add(time.Duration(minutes)*time.Minute).Unix())); err != nil {
			t.Fatalf("re-arm bump: %v", err)
		}
	}

	// renderedCreated reads the zombie row's `created <timestamp>` stamp off
	// the LIVE list — a value the renderer takes from the row without
	// consulting its own clock, so it is comparable exactly. Empty when the
	// stamp cannot be located, which the caller treats as a mismatch.
	renderedCreated := func() string {
		out, err := TaskList("in_progress")
		if err != nil {
			t.Fatalf("TaskList: %v", err)
		}
		for _, line := range strings.Split(out, "\n") {
			if !strings.Contains(line, "t-zombie") {
				continue
			}
			stamp := taskListRowCreatedStamp(line)
			if stamp == "" {
				t.Errorf("zombie row carries no `created <stamp>` to compare across re-arms: %s", strings.TrimSpace(line))
			}
			return stamp
		}
		t.Fatalf("zombie row missing from the in_progress list:\n%s", out)
		return ""
	}
	// zombieRow reads the row back from the DB — the anchor, never the render.
	zombieRow := func() *Task {
		row, err := taskByID(t.Context(), db, "t-zombie")
		if err != nil {
			t.Fatalf("taskByID: %v", err)
		}
		if row == nil {
			t.Fatal("zombie row vanished from the ledger")
		}
		return row
	}
	// ageOf renders the row's state suffix against a CONTROLLED now: the
	// renderer is fed (row, now) directly, so the string is compared exactly
	// and the host's clock is not an input to the assertion.
	ageOf := func(row *Task) string {
		suffix := taskStateSuffix(row, time.Unix(zombieControlledNowUnix, 0))
		if !strings.Contains(suffix, "age ") {
			t.Fatalf("zombie row must render its age framing, got %q", suffix)
		}
		if strings.Contains(suffix, zombieApproxMarker) {
			t.Errorf("zombie row must not render the %q marker (anchor is real), got %q", zombieApproxMarker, suffix)
		}
		idx := strings.Index(suffix, "age ")
		return strings.TrimSuffix(suffix[idx+len("age "):], "]")
	}

	// Pre-re-arm: the age is measured from the real start (created_at ==
	// tasklist_issued_at), never from updated_at.
	if got := ageOf(zombieRow()); got != zombieRearmAgeLabel {
		t.Fatalf("pre-re-arm age = %q, want the real-start age %q", got, zombieRearmAgeLabel)
	}
	// The LIVE list must carry the SAME real anchor, read off the row the
	// ledger holds — an anchor that moved on disk shows up here.
	wantCreated := formatTaskTime(float64(start.Unix()))
	if got := renderedCreated(); got != wantCreated {
		t.Fatalf("list created stamp = %q, want the real-start stamp %q", got, wantCreated)
	}
	wantUpdatedAt := start.Unix()
	// First stale re-arm bump: updated_at advances, the start anchors do
	// not. The display must NOT move (a reset here is the bug).
	rearm(10)
	wantUpdatedAt = start.Add(10 * time.Minute).Unix()
	if got := zombieRow().UpdatedAt; got != float64(wantUpdatedAt) {
		t.Fatalf("fixture broken — re-arm did not move updated_at: %v, want %d", got, wantUpdatedAt)
	}
	if got := ageOf(zombieRow()); got != zombieRearmAgeLabel {
		t.Fatalf("after 1st re-arm age = %q, want unchanged %q", got, zombieRearmAgeLabel)
	}
	if got := renderedCreated(); got != wantCreated {
		t.Fatalf("after 1st re-arm the list created stamp moved to %q, want %q", got, wantCreated)
	}
	// Second re-arm bump: still monotonic on the real start.
	rearm(20)
	wantUpdatedAt = start.Add(20 * time.Minute).Unix()
	if got := zombieRow().UpdatedAt; got != float64(wantUpdatedAt) {
		t.Fatalf("fixture broken — 2nd re-arm did not move updated_at: %v, want %d", got, wantUpdatedAt)
	}
	if got := ageOf(zombieRow()); got != zombieRearmAgeLabel {
		t.Fatalf("after 2nd re-arm age = %q, want unchanged %q", got, zombieRearmAgeLabel)
	}
	if got := renderedCreated(); got != wantCreated {
		t.Fatalf("after 2nd re-arm the list created stamp moved to %q, want %q", got, wantCreated)
	}
	// updated_at is NEVER the anchor: after both bumps it is 20 minutes past
	// the real start, and the age above is still the real-start age — so a
	// renderer that had anchored on updated_at would have printed
	// taskFormatDuration(zombieRearmAge - 20min) here, not zombieRearmAgeLabel.
	if staleAnchor := taskRunningAnchor(zombieRow()); staleAnchor != float64(start.Unix()) {
		t.Fatalf("anchor = %v, want the real start %d (updated_at is %d)",
			staleAnchor, start.Unix(), wantUpdatedAt)
	}
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
}

// taskListRowCreatedStamp extracts the `created <2006-01-02 15:04:05>` stamp
// from one rendered task-list row, or "" when the row carries none.
func taskListRowCreatedStamp(line string) string {
	const marker = "created "
	idx := strings.Index(line, marker)
	if idx < 0 {
		return ""
	}
	rest := line[idx+len(marker):]
	if len(rest) < len(taskCreatedStampLayout) {
		return ""
	}
	return rest[:len(taskCreatedStampLayout)]
}

// taskCreatedStampLayout is the date-time layout the list's created stamp is
// rendered in — the ledger's own spelling (formatTaskTime).
const taskCreatedStampLayout = "2006-01-02 15:04:05"
