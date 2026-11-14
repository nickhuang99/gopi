package main

// cli_tasks_reportfacts_seam_test.go — the HARNESS-FACTS agreement, driven E2E
// through the REAL `gopi worker` wrapper (writebackOutcome → tools.SealTaskReport*),
// not merely through the seal function.
//
// WHY THIS FILE EXISTS: the unit tests in internal/tools prove what the seal
// states when it IS called. This one proves the close-out passes the outcome it
// is writing and that the operator MEETS a report agreeing with its own ledger
// (Rule 31's own standard, turned on this change: "a surface you did not
// execute is not delivered").
//
// The motivating shape is row 01a12de6-c052 (2026-10-12): a real, merged
// delivery whose session report was the harness skeleton stating
// `status: in_progress` while its own row read `completed` — because the seal
// ran BEFORE the writeback landed the status.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/taskreport"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// reportFactsSeamRun executes one row with a mock model that writes NO report
// (the skeleton arm) and returns the row's ledger status plus the report text.
func reportFactsSeamRun(t *testing.T, taskID string) (ledgerStatus, ledgerBranch, report string) {
	t.Helper()
	reportSeamBinary(t)
	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	repo := reportSeamFixtureRepo(t)
	// The executor child's ground: a real linked worktree of the fixture repo,
	// so the run HAS a branch to record (the delivered shape).
	work := filepath.Join(t.TempDir(), "wt")
	reportFactsSeamGit(t, repo, "worktree", "add", "-q", "-b", reportFactsSeamBranch, work, "HEAD")

	// The child's ONE tool call is a git round in its own ground: that is the
	// round the deliverable seam stamps the run's branch from (the primary axis),
	// and it is what makes the branch agreement non-vacuous.
	mock := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_ground",
			Name: "bash",
			Args: map[string]interface{}{"command": "git -C . status --porcelain"},
		}},
		testutil.Resp{Text: "[TASK_LIST_BEGIN]\n[]\n[TASK_LIST_END]\nTask complete."},
	)
	defer mock.Close()

	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env, "GOPI_MEMORY_DB="+dbPath)

	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"report facts agree with the ledger", "--payload", "this run writes no report",
		"--task-id", taskID, "--repo", repo}, "", env, work); code != 0 {
		t.Fatalf("task submit: code=%d stderr=%s", code, stderr)
	}
	_, workerStderr, code := runGopiMapped(t, []string{"worker", taskID}, "", env, work)
	if code != 0 {
		t.Fatalf("worker: code=%d stderr=%s", code, workerStderr)
	}
	// The report must exist at the harness-exported path (the skeleton arm is
	// only meaningful if the seal really ran).
	path := filepath.Join(gopiHome, ".gopi", config.SessionsDirName, taskID, taskreport.FileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no report at the harness path %s: %v", path, err)
	}
	// The LEDGER's own answer, read through the operator's face (`task show`) —
	// the other side of the agreement this test is about. The row must be
	// terminal, or the comparison would be vacuous.
	// `task show` runs from the FIXTURE REPO, not from the run's ground: the
	// close-out releases that worktree, so a cwd inside it is already gone.
	show, stderr, code := runGopiMapped(t, []string{"task", "show", taskID}, "", env, repo)
	if code != 0 {
		t.Fatalf("task show: code=%d stderr=%s", code, stderr)
	}
	ledgerStatus = taskShowStatus(t, show)
	if ledgerStatus == "" {
		t.Fatalf("`task show` names no status line:\n%s", show)
	}
	return ledgerStatus, taskShowDeliverableBranch(t, show), string(raw)
}

// taskShowStatus reads the status WORD out of a rendered row — the same value
// the ledger holds, so a caller may compare it to a status constant. The raw
// field carries the render-time MEASURED holder facts after the word
// (` + holder dead (pid N gone)`), and comparing THAT to a constant measures the
// marker logic as a side effect of this assertion. The split lives once, in
// statusWord (cli_tasks_statusword_split_test.go).
func taskShowStatus(t *testing.T, out string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "status:"); ok {
			return statusWord(strings.TrimSpace(v))
		}
	}
	return ""
}

// reportFactsSeamGit runs one git command in dir, failing on error — it reuses
// reportSeamGitOutput (the seam fixture's ONE git face) rather than adding a
// second one.
func reportFactsSeamGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	reportSeamGitOutput(t, dir, args...)
}

// reportFactsSeamBranch is the branch the executor child's ground carries.
const reportFactsSeamBranch = "feat/reportfacts-e2e"

// TestWorkerRunLeavesASkeletonAgreeingWithItsLedger is the acceptance on the
// REAL close-out: the run completes, the model wrote no report, and the
// harness's own skeleton states the terminal outcome its row actually carries.
func TestWorkerRunLeavesASkeletonAgreeingWithItsLedger(t *testing.T) {
	const taskID = "01a11500-0000-7000-8000-000000000016"
	ledgerStatus, recordedBranch, report := reportFactsSeamRun(t, taskID)

	if !strings.Contains(report, taskreport.SkeletonPlaceholder) {
		t.Fatalf("the model wrote nothing — the seal must have produced the skeleton:\n%s", report)
	}
	if ledgerStatus != tools.TaskStatusCompleted {
		t.Fatalf("fixture precondition: the row must close completed, got %q", ledgerStatus)
	}
	want := "- status: " + ledgerStatus
	if !strings.Contains(report, want) {
		t.Errorf("the harness's own report must state the status its own ledger carries (%q):\n%s", want, report)
	}
	if strings.Contains(report, "- status: "+tools.TaskStatusInProgress) {
		t.Errorf("the skeleton states the PRE-terminal row snapshot the writeback then replaced:\n%s", report)
	}
	// The same agreement for the BRANCH axis, asserted against the ledger rather
	// than against a name this test guessed: whatever the row records is what the
	// report must state — and when the row records a branch the report must NAME
	// it, never "(unknown)" beside a recorded one. Both arms are the property;
	// the test says which one this run took, so a shape change cannot turn the
	// assertion vacuous.
	if recordedBranch == "" {
		if !strings.Contains(report, "- branch: (unknown)") {
			t.Errorf("the row records no branch, so the report must say so explicitly (it is never inferred):\n%s", report)
		}
		return
	}
	if strings.Contains(report, "- branch: (unknown)") {
		t.Errorf("the report says the branch is unknown while its row records %q:\n%s", recordedBranch, report)
	}
	if !strings.Contains(report, "- branch: "+recordedBranch) {
		t.Errorf("the report must name the branch its row records (%q):\n%s", recordedBranch, report)
	}
}

// taskShowDeliverableBranch reads the branch out of `task show`'s deliverable
// block (the ledger's own rendering of the row), or "" when the row records
// none — which is itself a fact the test asserts against.
func taskShowDeliverableBranch(t *testing.T, show string) string {
	t.Helper()
	for _, line := range strings.Split(show, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "deliverable.branch:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
