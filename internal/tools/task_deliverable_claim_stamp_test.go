package tools

// Deliverable ground stamped at CLAIM time (epic branch↔task linkage, item
// [2/5], 2026-09-16).
//
// The acceptance for this item, in the two halves the briefing asked for:
//
//	① an IN-FLIGHT row names its ground: right after the claim (the row is
//	   in_progress, its branch still unproven, not yet completed) the
//	   deliverable carries BOTH the branch and the linked worktree;
//	② the COMPLETION-time backfill is unchanged and does not drift: the same
//	   row, written back completed through the real writeback path, keeps the
//	   ONE branch key the child's cwd resolves — not a second spelling, not a
//	   competing value.
//
// Fixtures are real git (the resolution IS git) and the ledger is the real
// ledger at a temp GOPI_MEMORY_DB, so both halves run against the same row
// the production wrapper claims.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// claimGroundRowBlob reads a row's raw deliverable blob.
func claimGroundRowBlob(t *testing.T, taskID string) string {
	t.Helper()
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	row, err := taskByID(context.Background(), db, taskID)
	if err != nil || row == nil {
		t.Fatalf("load row %s: %v", taskID, err)
	}
	return strings.TrimSpace(row.Deliverable)
}

// claimGroundRowStatus returns the row's status (the claim-time stamp must
// land on an IN-FLIGHT row, so the tests assert it explicitly).
func claimGroundRowStatus(t *testing.T, taskID string) string {
	t.Helper()
	db, err := openTasksDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	row, err := taskByID(context.Background(), db, taskID)
	if err != nil || row == nil {
		t.Fatalf("load row %s: %v", taskID, err)
	}
	return row.Status
}

// TestClaimTimeGroundStampNamesGroundInFlight is acceptance ① — and, in its
// second half, acceptance ② on the SAME row: the claim-time stamp is additive
// and the completion-time backfill agrees with it instead of competing.
func TestClaimTimeGroundStampNamesGroundInFlight(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	repo, wt := branchBackfillFixture(t)
	const branch = "feat/backfill-e2e"

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "claim-time ground stamp", Repo: repo})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := adoptionTaskID(t, out)

	// A pending row carries the submit-time repo-only placeholder — the gap
	// this item closes.
	before := claimGroundRowBlob(t, taskID)
	if !strings.Contains(before, `"repo"`) || strings.Contains(before, `"branch"`) {
		t.Fatalf("a pending row must still carry the repo-only placeholder, got %s", before)
	}

	// Claim + start = the wrapper's own transition, before any executor work.
	if err := TaskClaimExplicit(taskID, "claimstamp:1"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := TaskStart(taskID, "claimstamp:1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := claimGroundRowStatus(t, taskID); got != TaskStatusInProgress {
		t.Fatalf("row status = %q, want %q — the stamp must be exercised in flight", got, TaskStatusInProgress)
	}

	gotWT, gotBranch, written := taskWorktreeStampClaimer(taskID, repo, wt)
	if !written {
		t.Fatalf("the claim-time stamp must write the ground: got wt=%q branch=%q", gotWT, gotBranch)
	}
	if gotWT != wt || gotBranch != branch {
		t.Fatalf("resolved ground = (%q, %q), want (%q, %q)", gotWT, gotBranch, wt, branch)
	}

	// ① In flight (status in_progress, branch unproven, run unfinished), the
	// row names BOTH axes — the pair, because a branch without a worktree is
	// invisible to the merger's resume loader.
	if got := claimGroundRowStatus(t, taskID); got != TaskStatusInProgress {
		t.Fatalf("row status = %q, want %q", got, TaskStatusInProgress)
	}
	var d struct {
		Repo     string `json:"repo"`
		Branch   string `json:"branch"`
		Worktree string `json:"worktree"`
	}
	inFlight := claimGroundRowBlob(t, taskID)
	if err := json.Unmarshal([]byte(inFlight), &d); err != nil {
		t.Fatalf("in-flight blob is not valid JSON (%s): %v", inFlight, err)
	}
	if d.Repo != repo || d.Branch != branch || d.Worktree != wt {
		t.Fatalf("in-flight deliverable must name the ground: repo=%q branch=%q worktree=%q (blob %s)",
			d.Repo, d.Branch, d.Worktree, inFlight)
	}
	if n := strings.Count(inFlight, `"`+DeliverableWorktreeKey+`"`); n != 1 {
		t.Fatalf("the worktree key must appear exactly once (Rule 14: one spelling), got %d in %s", n, inFlight)
	}

	// ② The completion path is UNCHANGED: it resolves the branch at the
	// CHILD's cwd (its recorded session workdir = the worktree, memory #2371)
	// and must record the SAME branch — no drift, and no second writer
	// appending a competing value.
	//
	// The RESOLUTION is asserted explicitly, not inferred from the blob: once
	// the claim stamp has set the branch, taskBackfillDeliverableBranch's own
	// "already branch-stamped" guard makes the later write a no-op, so a blob
	// compared after the writeback could show "no drift" while the completion
	// path resolved nothing at all (a vacuous pass). taskWritebackBranch is the
	// completion path's ONE resolver — read it directly against the same
	// fixture, then drive the real writeback for the write-side assertions.
	sessID := branchBackfillSeedSession(t, wt, "glm-claim-stamp")
	t.Setenv(TaskChildSessionEnv, sessID)
	if got := taskWritebackBranch(); got != branch {
		t.Fatalf("the completion-time resolver must still name the same branch from the child's cwd: got %q, want %q (memory #2371: this path reads at the CHILD's cwd — the claim stamp must not have moved it)", got, branch)
	}
	if _, err := TaskRunWriteback(taskID, TaskStatusCompleted, "done — run: elapsed=1s"); err != nil {
		t.Fatalf("TaskRunWriteback: %v", err)
	}
	completed := claimGroundRowBlob(t, taskID)
	var c struct {
		Repo     string `json:"repo"`
		Branch   string `json:"branch"`
		Worktree string `json:"worktree"`
	}
	if err := json.Unmarshal([]byte(completed), &c); err != nil {
		t.Fatalf("completed blob is not valid JSON (%s): %v", completed, err)
	}
	if c.Branch != branch {
		t.Fatalf("completion-time backfill drifted: branch=%q, want the claim-time %q (blob %s)", c.Branch, branch, completed)
	}
	if c.Worktree != wt || c.Repo != repo {
		t.Fatalf("completion must keep the record (repo=%q worktree=%q), got %s", repo, wt, completed)
	}
	if c != d {
		t.Fatalf("completion changed the recorded ground: in-flight=%+v completed=%+v", d, c)
	}
	if n := strings.Count(completed, `"branch"`); n != 1 {
		t.Fatalf("exactly ONE branch key is allowed (no double-writer), got %d in %s", n, completed)
	}
}

// TestClaimTimeGroundStampLeavesMainCheckoutRowRepoOnly: a claimed row whose
// run starts in the repo's MAIN checkout (branch master — exempt) must stay
// repo-only. The stamp is ADDITIVE: it never invents a branch, and it never
// touches a blob it cannot pair.
func TestClaimTimeGroundStampLeavesMainCheckoutRowRepoOnly(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	repo, _ := branchBackfillFixture(t)

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "claim-time ground stamp (main checkout)", Repo: repo})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := adoptionTaskID(t, out)
	before := claimGroundRowBlob(t, taskID)

	wt, branch, written := taskWorktreeStampClaimer(taskID, repo, repo)
	if written || wt != "" || branch != "" {
		t.Fatalf("main-checkout claim must resolve no ground (wt=%q branch=%q written=%v)", wt, branch, written)
	}
	if after := claimGroundRowBlob(t, taskID); after != before {
		t.Fatalf("a no-ground claim must not touch the blob:\n before=%s\n after =%s", before, after)
	}
}

// TestClaimTimeGroundStampNeverRebindsAnExistingWorktree: the claim-time
// stamp shares the creation seam's ONE writer, so its guard applies here too
// — a row that already names a worktree keeps it. This is what keeps the
// stamp additive rather than a second source of truth (the 2026-09-12
// foreign-owner hijack).
func TestClaimTimeGroundStampNeverRebindsAnExistingWorktree(t *testing.T) {
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	repo, wt := branchBackfillFixture(t)

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "claim-time ground stamp (already bound)", Repo: repo})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := adoptionTaskID(t, out)
	if err := taskBackfillDeliverableWorktree(taskID, wt, "feat/backfill-e2e"); err != nil {
		t.Fatalf("seed existing binding: %v", err)
	}
	bound := claimGroundRowBlob(t, taskID)

	// A second, DIFFERENT worktree of the same repo — the claim-time stamp
	// must refuse to re-point the row at it.
	other := filepath.Join(t.TempDir(), "other-wt")
	if out, err := runGitForTest(repo, "worktree", "add", "-b", "feat/other-ground", other, "master"); err != nil {
		t.Fatalf("second worktree: %v: %s", err, out)
	}
	if _, _, written := taskWorktreeStampClaimer(taskID, repo, other); written {
		t.Fatalf("the claim-time stamp re-bound a row that already names a worktree")
	}
	if after := claimGroundRowBlob(t, taskID); after != bound {
		t.Fatalf("an already-bound row must keep its ground:\n before=%s\n after =%s", bound, after)
	}
}

// runGitForTest runs one git command in dir and returns its combined output.
func runGitForTest(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestClaimTimeGroundStampRefusesWhenOwnershipIsUnproven pins the FAIL-CLOSED
// half of the ownership guard (Rule 15): "no owner could be proven" must never
// be read as "there is no owner". The scan is made to FAIL through the
// package seam (an unreadable ledger cannot prove anything), and the stamp
// must then REFUSE to bind the row to the worktree it resolved, leaving the
// blob byte-identical — the same direction as every other unresolved ground.
//
// A/B GRADEABLE: the body runs twice over the SAME fixture and the SAME
// forced scan failure, once against the fail-closed guard and once against the
// pre-fix "swallow the error" shape (owner=""), and asserts the two verdicts
// DIFFER. A test that only asserted "not written" would pass with the guard
// removed — the ledger is unreadable, so the blob writer refuses anyway — and
// prove nothing; binding the outcome to the guard is what makes this an
// acceptance rather than a no-op assertion.
func TestClaimTimeGroundStampRefusesWhenOwnershipIsUnproven(t *testing.T) {
	repo, wt := branchBackfillFixture(t)
	const branch = "feat/backfill-e2e"

	out, err := TaskSubmit(TaskSubmitOptions{Summary: "claim-time ground stamp (blind scan)", Repo: repo})
	if err != nil {
		t.Fatalf("TaskSubmit: %v", err)
	}
	taskID := adoptionTaskID(t, out)
	before := claimGroundRowBlob(t, taskID)

	// ARM 1 — the guard must refuse: an ownership scan that cannot answer is
	// not an unowned worktree.
	restoreScan := taskWorktreeOwnerScanStub(t, "", errClaimGroundScanUnproven)
	gotWT, gotBranch, written := taskWorktreeStampClaimer(taskID, repo, wt)
	restoreScan()
	if gotWT != wt || gotBranch != branch {
		t.Fatalf("the guard must refuse AFTER resolving the ground: got (%q, %q), want (%q, %q)", gotWT, gotBranch, wt, branch)
	}
	if written {
		t.Fatalf("an unprovable ownership scan must REFUSE the stamp (fail-closed), got written=true")
	}
	if after := claimGroundRowBlob(t, taskID); after != before {
		t.Fatalf("a refused stamp must not touch the blob:\n before=%s\n after =%s", before, after)
	}

	// ARM 2 — the PRE-FIX shape (swallow the error, proceed as "unowned"), on
	// the same row and the same forced failure. If this arm did NOT write, the
	// two shapes would be indistinguishable and arm 1 would assert nothing
	// about the guard (Rule 15: no silent pass).
	restoreScan = taskWorktreeOwnerScanStub(t, "", nil)
	_, _, writtenPreFix := taskWorktreeStampClaimer(taskID, repo, wt)
	restoreScan()
	if !writtenPreFix {
		t.Fatalf("A/B BROKEN: the swallow-error shape must WRITE the ground, else arm 1 cannot separate the guard from a no-op (both arms returned written=false)")
	}
	if after := claimGroundRowBlob(t, taskID); !strings.Contains(after, `"`+DeliverableWorktreeKey+`"`) {
		t.Fatalf("the A/B control arm must have bound the row (it is the pre-fix behaviour): %s", after)
	}
}

// errClaimGroundScanUnproven stands in for any failure of the ownership scan
// (unreadable ledger, wedged query, truncated read) — the guard's contract is
// about the DIRECTION it refuses in, not about which error arrived.
var errClaimGroundScanUnproven = errors.New("claim-time ground stamp: ownership scan unavailable (test)")

// taskWorktreeOwnerScanStub rebinds the ownership-scan seam for the duration
// of one arm and returns the restore func. Serial by construction: the seam is
// package state, so the arms run one after another (no t.Parallel here).
func taskWorktreeOwnerScanStub(t *testing.T, owner string, err error) func() {
	t.Helper()
	prev := taskWorktreeOwnerScan
	taskWorktreeOwnerScan = func(string, string) (string, error) { return owner, err }
	return func() { taskWorktreeOwnerScan = prev }
}
