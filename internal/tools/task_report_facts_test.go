package tools

// task_report_facts_test.go — the HARNESS-FACTS contract of the report seam
// (devdoc/task-report-harness-facts-agreement.md carries the measurement and the
// fix rationale this file pins):
// the skeleton's `## Harness facts` block must agree with the ledger it is
// describing.
//
// The defect this pins (measured 2026-10-12, row 01a12de6-c052-7346-9efc-bbd885387489):
// a real, merged delivery whose session report was the harness skeleton —
// stating `status: in_progress` while its own row read `completed`, and
// `branch: (unknown)` while its own row's deliverable named
// `task/01a12de6c05273469efcbbd885387489` on a branch whose ref was reachable
// in the shared repo the whole time. 14 of 14 skeletons on this host carried
// the same three contradictions.
//
// Two independent causes, one test each:
//
//	① the seal runs INSIDE writebackOutcome BEFORE TaskRunWriteback, so a row
//	  snapshot is pre-terminal by construction — the caller must state the
//	  outcome it is writing (SealTaskReportOutcome);
//	② branch/commit were resolved from the child-session ENV alone, so a run
//	  whose session JSONL is gone (or whose wrapper never carried the env) got
//	  "(unknown)" for facts the LEDGER was holding.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/taskreport"
)

// reportFactsBranch is the branch the facts fixture delivers on.
const reportFactsBranch = "feat/report-facts"

// reportFactsGit runs one git command in dir, failing the test on error (a
// missing git must never read as a pass).
func reportFactsGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=reportfacts", "GIT_AUTHOR_EMAIL=reportfacts@gopi",
		"GIT_COMMITTER_NAME=reportfacts", "GIT_COMMITTER_EMAIL=reportfacts@gopi")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// reportFactsGround builds a repo with one commit on reportFactsBranch in a
// linked worktree and returns (repo, worktree, short tip).
func reportFactsGround(t *testing.T) (repo, wt, tip string) {
	t.Helper()
	repo = t.TempDir()
	reportFactsGit(t, repo, "init", "-q", "-b", "master")
	reportFactsGit(t, repo, "commit", "-q", "--allow-empty", "-m", "root")
	wt = filepath.Join(t.TempDir(), "wt")
	reportFactsGit(t, repo, "worktree", "add", "-q", "-b", reportFactsBranch, wt, "master")
	if err := os.WriteFile(filepath.Join(wt, "work.txt"), []byte("delivered\n"), 0o644); err != nil {
		t.Fatalf("write work file: %v", err)
	}
	reportFactsGit(t, wt, "add", "work.txt")
	reportFactsGit(t, wt, "commit", "-q", "-m", "feat: the delivery")
	return repo, wt, reportFactsGit(t, wt, "rev-parse", "--short", "HEAD")
}

// reportFactsRow submits a row in the sandbox ledger with the ground the run
// recorded, and returns the task id. The env channel the pre-fix code relied on
// is deliberately EMPTY.
func reportFactsRow(t *testing.T, taskID string) (repo, wt, tip string) {
	t.Helper()
	repo, wt, tip = reportFactsGround(t)
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "report facts agree with the ledger", Repo: repo, TaskID: taskID,
	}); err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	if _, err := TaskUpdate(taskID, TaskStatusInProgress); err != nil {
		t.Fatalf("TaskUpdate: %v", err)
	}
	if err := taskBackfillDeliverableWorktree(taskID, wt, reportFactsBranch); err != nil {
		t.Fatalf("stamp the recorded ground: %v", err)
	}
	return repo, wt, tip
}

// reportFactsSeal seals taskID as the close-out does and returns the skeleton
// text (the model authored nothing) plus the digest line.
func reportFactsSeal(t *testing.T, taskID, outcome string) (string, string) {
	t.Helper()
	line, err := SealTaskReportOutcome(taskID, outcome)
	if err != nil {
		t.Fatalf("SealTaskReportOutcome: %v", err)
	}
	path, err := TaskReportPath(taskID)
	if err != nil {
		t.Fatalf("TaskReportPath: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report at %s: %v", path, err)
	}
	if !strings.Contains(string(body), taskreport.SkeletonPlaceholder) {
		t.Fatalf("no model report was written — the seal must have produced the skeleton:\n%s", body)
	}
	return string(body), line
}

// TestSkeletonStatesTheTerminalOutcomeNotAPreTerminalRowSnapshot is cause ①:
// the seal runs before the writeback lands the status, so the ROW still reads
// in_progress at that moment — and the report must state the outcome being
// written, never the snapshot it is about to replace.
func TestSkeletonStatesTheTerminalOutcomeNotAPreTerminalRowSnapshot(t *testing.T) {
	const taskID = "t-report-facts-outcome"
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(home, "memory.db"))
	t.Setenv(TaskChildSessionEnv, "")
	t.Setenv(TaskExecutorSessionEnv, "")

	reportFactsRow(t, taskID)

	// The row's own status at seal time — the snapshot the defect reported.
	if status, _, _ := taskRowFacts(taskID); status != TaskStatusInProgress {
		t.Fatalf("fixture precondition: row status = %q, want %q (the seal must be pre-terminal)", status, TaskStatusInProgress)
	}
	body, _ := reportFactsSeal(t, taskID, TaskStatusCompleted)
	if !strings.Contains(body, "- status: "+TaskStatusCompleted) {
		t.Errorf("the report must state the outcome the close-out is writing (%q), not the pre-terminal row snapshot:\n%s",
			TaskStatusCompleted, body)
	}
	if strings.Contains(body, "- status: "+TaskStatusInProgress) {
		t.Errorf("the report still carries the pre-terminal status it contradicts:\n%s", body)
	}
	// A seal WRITES NO LEDGER: the row is the writeback's, and it is still
	// untouched here (the report never becomes a second writer of the outcome).
	if status, _, _ := taskRowFacts(taskID); status != TaskStatusInProgress {
		t.Errorf("the seal must not write the row: status = %q, want %q", status, TaskStatusInProgress)
	}
}

// TestSkeletonStatesTheLedgerBranchAndItsRefTip is cause ②: branch and commit
// come from the ledger's own record (and from the ref in the shared repo that
// record names), so a report agrees with its row even when no child-session env
// ever reached the close-out.
func TestSkeletonStatesTheLedgerBranchAndItsRefTip(t *testing.T) {
	const taskID = "t-report-facts-branch"
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(home, "memory.db"))
	t.Setenv(TaskChildSessionEnv, "")
	t.Setenv(TaskExecutorSessionEnv, "")

	_, _, tip := reportFactsRow(t, taskID)
	body, line := reportFactsSeal(t, taskID, TaskStatusCompleted)

	if !strings.Contains(body, "- branch: "+reportFactsBranch) {
		t.Errorf("the report must state the branch the row's deliverable records (%q):\n%s", reportFactsBranch, body)
	}
	if !strings.Contains(body, "- commit: "+tip) {
		t.Errorf("the report must state the tip of that branch ref (%s):\n%s", tip, body)
	}
	if !strings.Contains(line, taskreport.DigestLinePrefix) {
		t.Errorf("digest line = %q, want it to name the report", line)
	}
}

// TestSkeletonStatesTheRunSessionIDTheRowDoesNotCarryYet is the third face of
// the same ordering defect: the row's session_id is stamped by the writeback the
// seal PRECEDES, so the skeleton said `session_id: (unknown)` — beside a ledger
// whose own `task show` named that session's JSONL. The id the run is holding
// must be stated, not rendered as unknown.
func TestSkeletonStatesTheRunSessionIDTheRowDoesNotCarryYet(t *testing.T) {
	const taskID = "t-report-facts-session"
	const runSession = "b55419ad-f0cd-47f6-9921-b3d1fb3cda13"
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(home, "memory.db"))
	t.Setenv(TaskChildSessionEnv, runSession)
	t.Setenv(TaskExecutorSessionEnv, "")

	reportFactsRow(t, taskID)
	body, _ := reportFactsSeal(t, taskID, TaskStatusCompleted)

	if strings.Contains(body, "- session_id: (unknown)") {
		t.Errorf("the run holds its own session id (%s) and the report renders it unknown:\n%s", runSession, body)
	}
	if !strings.Contains(body, "- session_id: "+runSession) {
		t.Errorf("the report must state the run's session id (%s):\n%s", runSession, body)
	}
}

// that reads the shared repo reads the BRANCH REF, never the repo's own HEAD.
// The main checkout usually stands on master, so an unqualified reading would
// name the wrong commit — a fact worse than "(unknown)" because it looks right.
// TestSkeletonCommitFollowsTheRecordedBranchNeverTheRepoHead: the commit rung
// that reads the shared repo reads the BRANCH REF, never the repo's own HEAD.
func TestSkeletonCommitFollowsTheRecordedBranchNeverTheRepoHead(t *testing.T) {
	const taskID = "t-report-facts-repohead"
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(home, "memory.db"))
	t.Setenv(TaskChildSessionEnv, "")
	t.Setenv(TaskExecutorSessionEnv, "")

	repo, wt, tip := reportFactsRow(t, taskID)
	masterTip := reportFactsGit(t, repo, "rev-parse", "--short", "HEAD")
	if masterTip == tip {
		t.Fatalf("fixture precondition: the branch tip and master's tip must differ (%s)", tip)
	}
	// The worktree is GONE — the delivered-and-cleaned-up shape, where only the
	// shared repo still holds the branch ref.
	reportFactsGit(t, repo, "worktree", "remove", "--force", wt)

	body, _ := reportFactsSeal(t, taskID, TaskStatusCompleted)
	if !strings.Contains(body, "- commit: "+tip) {
		t.Errorf("the commit must be the recorded branch's tip (%s), read from the shared repo after the worktree is gone:\n%s", tip, body)
	}
	if strings.Contains(body, "- commit: "+masterTip) {
		t.Errorf("the report named the repo's own HEAD (%s) instead of the delivered branch's tip (%s):\n%s", masterTip, tip, body)
	}
}
