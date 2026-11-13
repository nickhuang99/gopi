package tools

// Post-merge verify gate tests (epic merger-verify-gate, 2026-09-12). Real
// git repos + a real ledger (GOPI_MEMORY_DB → a temp file): the smallest
// sample that exercises DETECTION (the bugfixing predicate), the PRECISE
// REVERT (recorded pre-merge tip, asserted), branch preservation, the
// ledger-side event record and the rework FILING (a NEW row referencing the
// old task id, the judged row untouched).
//
// Each negative case below is written to be RED on the pre-fix behaviour:
// pre-fix there was no gate at all, so the assertions that master returns to
// the recorded pre-merge tip and that a rework row exists would fail.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// --- fixture helpers ---

// vgGit runs git in dir, failing the test on error.
func vgGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// vgRepo creates a temp repo with a master seed commit and returns its path.
func vgRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	vgGit(t, dir, "init", "-q", "-b", "master")
	vgGit(t, dir, "config", "user.email", "t@t")
	vgGit(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	vgGit(t, dir, "add", "base.txt")
	vgGit(t, dir, "commit", "-q", "-m", "base")
	return dir
}

// vgCommitFile writes a file on a fresh branch off master and returns the
// branch name.
func vgCommitFile(t *testing.T, repo, branch, file, content, msg string) {
	t.Helper()
	vgGit(t, repo, "checkout", "-q", "-b", branch, "master")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(repo, file)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	vgGit(t, repo, "add", file)
	vgGit(t, repo, "commit", "-q", "-m", msg)
	vgGit(t, repo, "checkout", "-q", "master")
}

// vgLedger points the ledger at a per-test temp DB and returns its path.
func vgLedger(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mem.db")
	t.Setenv("GOPI_MEMORY_DB", path)
	// Touch the schema so detection sees a real (empty) ledger, exactly as a
	// running merger's host has.
	if _, err := TaskStatusOf("nonexistent-row"); err == nil {
		t.Fatal("TaskStatusOf on a missing row must error (probe of the live ledger)")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("ledger not created at %s: %v", path, err)
	}
	return path
}

// vgSubmitRow submits one ledger row directly (the delivering task).
func vgSubmitRow(t *testing.T, repo, branch, tags, summary string) string {
	t.Helper()
	out, err := TaskSubmit(TaskSubmitOptions{
		Summary: summary,
		Payload: "deliver " + branch,
		Tags:    tags,
		Source:  "auto",
		Repo:    repo,
	})
	if err != nil {
		t.Fatalf("submit row: %v", err)
	}
	// TaskSubmit returns a rendered confirmation; locate the row by summary
	// and stamp the branch (the deliverable shape the gate detects on — the
	// same backfill task_deliverable_branch.go performs in production).
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var id string
	if err := db.QueryRowContext(ctx,
		`SELECT task_id FROM tasks WHERE summary = ? ORDER BY id DESC LIMIT 1`, summary).Scan(&id); err != nil {
		t.Fatalf("locate submitted row %q: %v (submit said %q)", summary, err, out)
	}
	blob := `{"repo":"` + repo + `","branch":"` + branch + `"}`
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET deliverable = ? WHERE task_id = ?`, blob, id); err != nil {
		t.Fatal(err)
	}
	return id
}

// vgFinishRow drives a row to a terminal status through the ledger's legal
// transitions (pending → in_progress → completed/failed: the state machine
// has no pending → terminal edge).
func vgFinishRow(t *testing.T, taskID, status string) {
	t.Helper()
	if status == TaskStatusCancelled {
		if _, err := TaskUpdateWithResult(taskID, TaskStatusCancelled, "cancelled by the test"); err != nil {
			t.Fatalf("cancel row %s: %v", taskID, err)
		}
		return
	}
	if _, err := TaskUpdateWithResult(taskID, TaskStatusInProgress, "the developer started the fix"); err != nil {
		t.Fatalf("start row %s: %v", taskID, err)
	}
	if _, err := TaskUpdateWithResult(taskID, status, "delivered + verified by the developer"); err != nil {
		t.Fatalf("finish row %s as %s: %v", taskID, status, err)
	}
}

// vgRowStatus reads a row's status (the byte-identity probe of the
// append-only assertion).
func vgRowStatus(t *testing.T, taskID string) string {
	t.Helper()
	st, err := TaskStatusOf(taskID)
	if err != nil {
		t.Fatalf("status of %s: %v", taskID, err)
	}
	return st
}

// vgRowCount counts rows whose summary carries the prefix.
func vgRowCount(t *testing.T, prefix string) int {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tasks WHERE summary LIKE ?`, prefix+"%").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// vgRowSnapshot renders every persisted field of a row EXCEPT the mutable
// audit-only ones — the byte-identity probe for the append-only assertion
// (the judged row's terminal record must not move at all).
func vgRowSnapshot(t *testing.T, taskID string) string {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var status, tags, summary, deliverable, reason, source string
	if err := db.QueryRowContext(ctx,
		`SELECT status, tags, summary, deliverable, reason, source FROM tasks WHERE task_id = ?`,
		taskID).Scan(&status, &tags, &summary, &deliverable, &reason, &source); err != nil {
		t.Fatal(err)
	}
	return strings.Join([]string{status, tags, summary, deliverable, reason, source}, "|")
}

// vgDeliverable reads a row's deliverable blob.
func vgDeliverable(t *testing.T, taskID string) string {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var d string
	if err := db.QueryRowContext(ctx, `SELECT deliverable FROM tasks WHERE task_id = ?`, taskID).Scan(&d); err != nil {
		t.Fatal(err)
	}
	return d
}

// vgShow renders a row through the real `task show` renderer (the operator's
// audit path).
func vgShow(t *testing.T, taskID string) string {
	t.Helper()
	out, err := TaskShow(taskID)
	if err != nil {
		t.Fatalf("task show %s: %v", taskID, err)
	}
	return out
}

// mergeVerifyVerdictProbe is the text a human reads on the judged row after a
// FAIL verdict (the event detail opens with the reason).
const mergeVerifyVerdictProbe = "post-merge verify FAIL"

// vgEventCount counts task_events rows for a task with the given event name —
// the audit-trail probe.
func vgEventCount(t *testing.T, taskID, event string) int {
	t.Helper()
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM task_events WHERE task_id = ? AND event = ?`, taskID, event).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// --- detection: the machine-decidable bugfixing predicate ---

// TestVerifyGateDetectsBugfixRow: the predicate is the delivering row's `bug`
// tag, matched on the (repo, branch) pair parsed from the deliverable — never
// a substring of the raw blob, never a different branch's row.
func TestVerifyGateDetectsBugfixRow(t *testing.T) {
	repo := vgRepo(t)
	vgLedger(t)
	vgCommitFile(t, repo, "fix/thing", "thing.txt", "fixed\n", "fix thing")

	bugID := vgSubmitRow(t, repo, "fix/thing", "bug", "bug: thing is broken")
	vgFinishRow(t, bugID, TaskStatusCompleted)

	rec := MergeVerifyRecordForBranch(repo, "fix/thing")
	if rec == nil {
		t.Fatal("detection must find the delivering row for fix/thing")
	}
	if rec.TaskID != bugID {
		t.Fatalf("detection picked %s, want %s", rec.TaskID, bugID)
	}
	if !rec.BugfixDetected() {
		t.Fatalf("row tags %q must read as bugfixing", rec.Tags)
	}

	// A NON-bug row for the same branch must not be bugfixing when it is the
	// only row.
	repo2 := vgRepo(t)
	vgCommitFile(t, repo2, "feat/thing", "thing.txt", "x\n", "feat thing")
	featID := vgSubmitRow(t, repo2, "feat/thing", "feature", "feat: add thing")
	vgFinishRow(t, featID, TaskStatusCompleted)
	if rec := MergeVerifyRecordForBranch(repo2, "feat/thing"); rec == nil || rec.BugfixDetected() {
		t.Fatalf("an untagged/feature delivery must NOT be bugfixing (rec=%+v)", rec)
	}

	// Prefix confusion: fix/x must not match fix/x-v2's row.
	vgCommitFile(t, repo, "fix/thing-v2", "thing2.txt", "y\n", "fix thing v2")
	vgSubmitRow(t, repo, "fix/thing-v2", "bug", "bug: thing v2")

	// No row at all → nil (the gate never fires on an unprovable predicate).
	if rec := MergeVerifyRecordForBranch(repo, "fix/never-delivered"); rec != nil {
		t.Fatalf("a branch with no delivering row must detect nil, got %+v", rec)
	}
}

// --- the revert: PRECISE, from the recorded pre-merge tip, ASSERTED ---

// TestVerifyGateRevertRestoresRecordedTip is the core negative case: after a
// landing onto master, a FAILED verify must leave master EXACTLY at the
// pre-merge tip — asserted by comparing rev-parse HEAD to the recorded tip,
// not assumed. It also proves the merged branch's work survives the unwind
// (the reverted fix is not lost).
func TestVerifyGateRevertRestoresRecordedTip(t *testing.T) {
	repo := vgRepo(t)
	vgCommitFile(t, repo, "fix/bad", "fix.txt", "the fix\n", "fix: something")
	preTip := vgGit(t, repo, "rev-parse", "HEAD")
	branchTip := vgGit(t, repo, "rev-parse", "fix/bad")

	// The landing (a fast-forward: master is an ancestor of the branch, so
	// `git merge` produces NO merge commit — the recorded tip is the only way
	// back).
	vgGit(t, repo, "merge", "-q", "fix/bad")
	if got := vgGit(t, repo, "rev-parse", "HEAD"); got != branchTip {
		t.Fatalf("setup: landing did not fast-forward to the branch tip (%s vs %s)", got, branchTip)
	}
	vgGit(t, repo, "branch", "-q", "-d", "fix/bad") // taskMergeOne prunes the merged ref

	// No merge commit exists → the mechanism is reset --hard to the RECORDED
	// tip (never an arbitrary point).
	if mc := MergeVerifyMergeCommit(repo, preTip); mc != "" {
		t.Fatalf("fast-forward landing must report no merge commit, got %q", mc)
	}
	// Pin the branch's work BEFORE the unwind (the gate's preserve step).
	if err := MergeVerifyPreserveBranch(repo, "fix/bad", branchTip, preTip); err != nil {
		t.Fatalf("preserve: %v", err)
	}
	method, err := MergeVerifyRevertTo(repo, preTip, "")
	if err != nil {
		t.Fatalf("revert: %v", err)
	}
	if !strings.Contains(method, "reset --hard") {
		t.Fatalf("a fast-forward landing must unwind via reset --hard, got %q", method)
	}
	// ASSERTED restore — the acceptance criterion, verbatim.
	if got := vgGit(t, repo, "rev-parse", "HEAD"); got != preTip {
		t.Fatalf("master = %s after the revert, want the recorded pre-merge tip %s", got, preTip)
	}
	// The branch's work is preserved at its merged tip.
	if got := MergeVerifyBranchTip(repo, "fix/bad"); got != branchTip {
		t.Fatalf("branch fix/bad = %q after the unwind, want its merged tip %q preserved", got, branchTip)
	}
	// The fix content is NOT on master anymore.
	if _, err := os.Stat(filepath.Join(repo, "fix.txt")); err == nil {
		t.Fatal("fix.txt must not be present on master after the revert")
	}
}

// TestVerifyGateRevertMergeCommitPath: when the landing did NOT fast-forward
// (a real merge commit exists), the unwind uses the epic's mandated
// `git revert -m 1 <merge-commit>` and STILL asserts the recorded tip.
func TestVerifyGateRevertMergeCommitPath(t *testing.T) {
	repo := vgRepo(t)
	preTip := vgGit(t, repo, "rev-parse", "HEAD")

	// Diverge master so the merge cannot fast-forward.
	if err := os.WriteFile(filepath.Join(repo, "master.txt"), []byte("master side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	vgGit(t, repo, "add", "master.txt")
	vgGit(t, repo, "commit", "-q", "-m", "master side work")
	mergedFrom := vgGit(t, repo, "rev-parse", "HEAD")
	// Branch off the PRE-merge master tip, so the two sides diverged.
	vgGit(t, repo, "checkout", "-q", "-b", "fix/diverged", preTip)
	if err := os.WriteFile(filepath.Join(repo, "fix.txt"), []byte("fix side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	vgGit(t, repo, "add", "fix.txt")
	vgGit(t, repo, "commit", "-q", "-m", "fix: diverged work")
	vgGit(t, repo, "checkout", "-q", "master")

	vgGit(t, repo, "merge", "--no-ff", "-m", "Merge branch 'fix/diverged'", "fix/diverged")
	mc := MergeVerifyMergeCommit(repo, mergedFrom)
	if mc == "" {
		t.Fatal("a --no-ff landing must be detected as a merge commit whose first parent is the recorded tip")
	}
	method, err := MergeVerifyRevertTo(repo, mergedFrom, mc)
	if err != nil {
		t.Fatalf("revert -m 1: %v", err)
	}
	if !strings.Contains(method, "revert -m 1") {
		t.Fatalf("expected the revert -m 1 mechanism, got %q", method)
	}
	// `git revert -m 1` necessarily ADDS a revert commit, so the assertion is
	// content-identity with the recorded tip (the function asserts exactly
	// this internally); HEAD itself is the new revert commit.
	if got := vgGit(t, repo, "rev-parse", "HEAD"); got == mergedFrom {
		t.Fatal("a revert must produce a NEW commit, not silently equal the pre-merge tip")
	}
	vgGit(t, repo, "diff", "--quiet", mergedFrom, "HEAD")
	// The revert UNDID the branch's content: fix.txt is gone from master but
	// master's own side work survives.
	if _, err := os.Stat(filepath.Join(repo, "fix.txt")); err == nil {
		t.Fatal("fix.txt must be undone by the revert commit")
	}
	if _, err := os.Stat(filepath.Join(repo, "master.txt")); err != nil {
		t.Fatal("master's own work must survive the revert")
	}
}

// TestVerifyGateRevertRefusesWithoutTip: without a RECORDED pre-merge tip the
// gate refuses to guess (it never resets to an arbitrary point).
func TestVerifyGateRevertRefusesWithoutTip(t *testing.T) {
	repo := vgRepo(t)
	if _, err := MergeVerifyRevertTo(repo, "", ""); err == nil {
		t.Fatal("a revert with no recorded pre-merge tip must be refused")
	}
}

// --- rework filing: a NEW row referencing the OLD task id ---

// TestVerifyGateFileReworkReferencesOldTask is the third mandated negative
// case: a FAILED verify files a NEW task referencing the old task id while
// the old row's terminal status stays BYTE-IDENTICAL.
func TestVerifyGateFileReworkReferencesOldTask(t *testing.T) {
	repo := vgRepo(t)
	vgCommitFile(t, repo, "fix/bad", "fix.txt", "the fix\n", "fix: something")
	vgLedger(t)

	oldID := vgSubmitRow(t, repo, "fix/bad", "bug", "bug: the thing is broken")
	vgFinishRow(t, oldID, TaskStatusCompleted)
	beforeStatus := vgRowStatus(t, oldID)
	beforeRow := vgRowSnapshot(t, oldID)

	newID, err := MergeVerifyFileRework(MergeVerifyReworkOptions{
		Repo: repo, Master: "master", Branch: "fix/bad",
		OldTaskID: oldID, OldSummary: "bug: the thing is broken",
		PreTip: "0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f",
		Revert: "git reset --hard 0f0f0f",
		Reason: MergeVerifyReasonFail,
	})
	if err != nil {
		t.Fatalf("file rework: %v", err)
	}
	if newID == "" {
		t.Fatal("filing must return the new row's task id")
	}
	if newID == oldID {
		t.Fatal("the rework must be a NEW row, never the old one re-run")
	}
	// The new row references the old task id + carries the gate vocabulary.
	show := vgShow(t, newID)
	if !strings.Contains(show, oldID) {
		t.Fatalf("the new row must REFERENCE the old task id %s; show:\n%s", oldID, show)
	}
	if !strings.Contains(show, MergeVerifyEvent) {
		t.Fatalf("the new row must carry the %s tag; show:\n%s", MergeVerifyEvent, show)
	}
	// The rework row is claimable work (pending), and its DELIVERABLE names
	// the branch so the rework lane owns it.
	if st := vgRowStatus(t, newID); st != TaskStatusPending {
		t.Fatalf("rework row status = %q, want %q", st, TaskStatusPending)
	}
	if !strings.Contains(vgDeliverable(t, newID), "fix/bad") {
		t.Fatalf("rework deliverable %q must name the branch", vgDeliverable(t, newID))
	}
	// APPEND-ONLY: the judged row's terminal status is byte-identical.
	if got := vgRowStatus(t, oldID); got != beforeStatus || got != TaskStatusCompleted {
		t.Fatalf("old row status = %q, want it untouched at %q", got, beforeStatus)
	}
	if after := vgRowSnapshot(t, oldID); after != beforeRow {
		t.Fatalf("old row changed:\n before=%s\n after =%s", beforeRow, after)
	}
}

// TestVerifyGateFailCapStopsRefiling: the anti-loop cap counts the gate's
// terminal-FAILED rows for a branch and stops at MergeVerifyFailCap.
func TestVerifyGateFailCapStopsRefiling(t *testing.T) {
	repo := vgRepo(t)
	vgCommitFile(t, repo, "fix/cursed", "c.txt", "x\n", "fix: cursed")
	vgLedger(t)

	delivering := vgSubmitRow(t, repo, "fix/cursed", "bug", "bug: cursed thing")
	vgFinishRow(t, delivering, TaskStatusCompleted)

	if got := MergeVerifyFailCount(repo, "fix/cursed", delivering); got != 0 {
		t.Fatalf("fail count before any verdict = %d, want 0", got)
	}
	for i := 0; i < MergeVerifyFailCap; i++ {
		id, err := MergeVerifyFileRework(MergeVerifyReworkOptions{
			Repo: repo, Master: "master", Branch: "fix/cursed",
			OldTaskID: delivering, OldSummary: "bug: cursed thing",
			PreTip: "aaaabbbbccccdddd", Revert: "git reset --hard aaaabbbb",
			Reason: MergeVerifyReasonFail,
		})
		if err != nil {
			t.Fatalf("file rework %d: %v", i, err)
		}
		// The rework itself failing its verification is what counts toward
		// the cap (the gate's own FAILED rows for this branch).
		vgFinishRow(t, id, TaskStatusFailed)
	}
	if got := MergeVerifyFailCount(repo, "fix/cursed", delivering); got != MergeVerifyFailCap {
		t.Fatalf("fail count = %d, want the cap %d", got, MergeVerifyFailCap)
	}
}

// --- the ledger event / notify record (the human audit path) ---

// TestVerifyGateRecordsVerdictEvent: the verdict lands on the JUDGED row's
// append-only event trail, so a human audits it with `gopi task show`.
func TestVerifyGateRecordsVerdictEvent(t *testing.T) {
	repo := vgRepo(t)
	vgLedger(t)
	vgCommitFile(t, repo, "fix/x", "x.txt", "x\n", "fix: x")
	id := vgSubmitRow(t, repo, "fix/x", "bug", "bug: x")
	vgFinishRow(t, id, TaskStatusCompleted)

	detail := MergeVerifyReasonFail + ": repo=" + repo + " branch=fix/x"
	MergeVerifyRecordEvent(id, detail)
	if got := MergeVerifyLatestEvent(id); got != detail {
		t.Fatalf("latest event = %q, want %q", got, detail)
	}
	// The event NAME is the durable, grep-able marker in task_events (the
	// audit trail an operator queries); `gopi task show` renders the event's
	// DETAIL as its `result:` line — so the verdict text is what the human
	// reads, and the name is what a query matches.
	if n := vgEventCount(t, id, MergeVerifyEvent); n != 1 {
		t.Fatalf("task_events rows named %q on %s = %d, want 1", MergeVerifyEvent, id, n)
	}
	show := vgShow(t, id)
	if !strings.Contains(show, "branch=fix/x") {
		t.Fatalf("show must render the verdict detail on the judged row; got:\n%s", show)
	}
	if !strings.Contains(show, mergeVerifyVerdictProbe) {
		t.Fatalf("show must render the verdict text; got:\n%s", show)
	}
	// The status is untouched by recording a verdict (append-only).
	if st := vgRowStatus(t, id); st != TaskStatusCompleted {
		t.Fatalf("recording a verdict must not move the row's status (got %q)", st)
	}
}
