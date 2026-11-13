package tools

import (
	"strings"
	"testing"
	"time"
)

// TestTaskListGroundTailOnLiveRows pins the ground tail of a rendered ledger row
// (tasklist_ground.go): a LIVE row prints the branch and the worktree its run
// works in, read off its own deliverable blob; every other row prints nothing.
//
// The operator's cost this answers: "which branch is that row running on?" used
// to be one `task show` per row plus a `git worktree list` to turn the directory
// into a ref.
func TestTaskListGroundTailOnLiveRows(t *testing.T) {
	path := t.TempDir() + "/memory.db"
	t.Setenv("GOPI_MEMORY_DB", path)

	const (
		liveBranch  = "task/01a11554b3b1731e84252ce4986786f9"
		liveWT      = "/home/agent/workspace/gopi-01a11554b3b1731e84252ce4986786f9"
		pendingWT   = "/home/agent/workspace/gopi-01a11599b3b1731e84252ce4986786f9"
		pendBranch  = "task/01a11599b3b1731e84252ce4986786f9"
		groundLabel = "[" + TaskListGroundLabel + " "
	)
	if _, err := TaskNew("live work", "t-ground-live", "", ""); err != nil {
		t.Fatalf("TaskNew live: %v", err)
	}
	db, err := openTasksDB(t.Context(), path)
	if err != nil {
		t.Fatalf("openTasksDB: %v", err)
	}
	nowF := float64(time.Now().Unix())
	if _, err := db.Exec(`UPDATE tasks SET status = ?, claimed_by = ?, claimed_at = ?, in_progress_at = ?,
		deliverable = ? WHERE task_id = ?`,
		taskStatusInProgress, taskStateTestHolder(1), nowF, nowF,
		`{"repo":"/home/agent/work/GoPi","branch":"`+liveBranch+`","worktree":"`+liveWT+`"}`,
		"t-ground-live"); err != nil {
		t.Fatalf("stamp live deliverable: %v", err)
	}
	// A PENDING row that already records a ground (an earlier attempt's stamp
	// survived a reopen): it must render NO tail — a ground is what a run is
	// standing in, not what it once did.
	if _, err := db.Exec(`INSERT INTO tasks (task_id, summary, status, created_at, updated_at, deliverable)
		VALUES ('t-ground-pend', 'pending with a stale ground', ?, ?, ?, ?)`,
		taskStatusPending, nowF, nowF,
		`{"repo":"/home/agent/work/GoPi","branch":"`+pendBranch+`","worktree":"`+pendingWT+`"}`); err != nil {
		t.Fatalf("seed pending row: %v", err)
	}
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	restore := stubLiveHolderProbe(t, true)
	defer restore()

	out, err := TaskList("")
	if err != nil {
		t.Fatalf("TaskList: %v", err)
	}
	live, pend := "", ""
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(line, "t-ground-live"):
			live = line
		case strings.Contains(line, "t-ground-pend"):
			pend = line
		}
	}
	if live == "" || pend == "" {
		t.Fatalf("fixture broken — both rows must render:\n%s", out)
	}
	wantTail := groundLabel + liveBranch + taskListGroundAt + liveWT + taskListGroundClose
	if !strings.Contains(live, wantTail) {
		t.Errorf("the live row must carry its ground (%q):\n%s", wantTail, live)
	}
	if strings.Contains(pend, groundLabel) {
		t.Errorf("a non-live row must carry no ground tail:\n%s", pend)
	}
}

// TestTaskListGroundTailAxesAreIndependent pins the two axes of the tail: each
// renders only what the row RECORDS, so a row carrying a branch and no
// directory (the claim-moment stamp) still tells the reader the branch, and a
// row carrying neither renders nothing at all rather than an empty shell.
func TestTaskListGroundTailAxesAreIndependent(t *testing.T) {
	branchOnly := &Task{
		TaskID: "t-branch-only", Status: taskStatusInProgress,
		Deliverable: `{"repo":"/r","branch":"task/01a1aaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`,
	}
	got := taskListGroundSuffix(branchOnly)
	if !strings.Contains(got, "task/01a1aaaaaaaaaaaaaaaaaaaaaaaaaaaa") || strings.Contains(got, taskListGroundAt) {
		t.Errorf("a branch-only row renders the branch and no separator: %q", got)
	}
	dirOnly := &Task{
		TaskID: "t-dir-only", Status: taskStatusInProgress,
		Deliverable: `{"repo":"/r","worktree":"/w"}`,
	}
	got = taskListGroundSuffix(dirOnly)
	if !strings.Contains(got, "/w") || strings.Contains(got, TaskListGroundLabel+" task/") {
		t.Errorf("a directory-only row renders the directory and no invented branch: %q", got)
	}
	if bare := taskListGroundSuffix(&Task{TaskID: "t-none", Status: taskStatusInProgress}); bare != "" {
		t.Errorf("a row recording no ground renders no tail, got %q", bare)
	}
}
