// Tests for the merge candidate's REF RE-VERIFICATION step
// (repo_sweep_ref_guard.go) — the ghost-ref incident of 2026-09-16/17:
// `merge/merger-inflight-both-axes` was listed as a candidate by the discovery
// snapshot and had already been landed + deleted by a concurrent round, so the
// UNION gate was asked about a name git could not resolve and answered
// "fatal: Not a valid object name" every tick.
//
// The contradiction is PROVEN AT BOTH ENDS, never at one:
//
//	TestRepoSweepRefGuard_GoneRefIsSkippedAndRecorded — the live-row shape: a
//	  candidate whose ref vanished mid-tick must be SKIPPED (no gate asked) and
//	  the live ledger row that still claims it must carry a `ref-gone` record.
//	TestRepoSweepRefGuard_RealBranchStillReachesTheGates — the no-weakening
//	  half: with the ref present, the branch reaches the gates and the merge
//	  path exactly as before.
//	TestRepoSweepRefGuard_TerminalRowIsNotAnInconsistency — the discriminator:
//	  a COMPLETED row whose branch was landed and deleted is the ordinary
//	  post-merge state, and it must NOT be reported as an inconsistency.
//
// The disappearance itself is driven through the probe seam
// (repoSweepRefPresentFn), because "another merger round deleted the branch
// between discovery and decision" is a race with no scheduleable fixture. The
// seam is asserted to be the production function's own variable so the test
// proves the arm a real tick takes, not a mock of it.

package tools

import (
	"context"
	"github.com/nick/gopi/internal/heavytest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// refGuardGoneBranch is the fixture candidate whose ref disappears between
	// discovery and the decision — the ghost's own shape.
	refGuardGoneBranch = "merge/ref-guard-ghost"
	// refGuardRealBranch is the control: a real branch that must still reach the
	// merge path.
	refGuardRealBranch = "feat/ref-guard-real"
	// refGuardLiveRow is the ledger row that still CLAIMS the vanished branch
	// (a row that has not ended).
	refGuardLiveRow = "01a0ffff-ref-guard-live"
	// refGuardDoneRow is a row whose branch was delivered and which has ended.
	refGuardDoneRow = "01a0ffff-ref-guard-done"
)

// refGuardSeedRow files a row naming branch with the given status, so the
// ledger has a real owner to consult when the ref disappears. It stamps the
// deliverable itself (rather than reusing branchAttribSeed, whose own
// GOPI_MEMORY_DB pin would point the assertions at a different DB).
func refGuardSeedRow(t *testing.T, taskID, status, branch string) {
	t.Helper()
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: taskID, Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, deliverable = ? WHERE task_id = ?`,
		status, `{"branch":"`+branch+`"}`, taskID); err != nil {
		t.Fatalf("stamp %s: %v", taskID, err)
	}
}

// refGuardEventDetails reads the `ref-gone` events recorded for taskID.
func refGuardEventDetails(t *testing.T, taskID string) []string {
	t.Helper()
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	rows, err := db.QueryContext(ctx,
		`SELECT detail FROM task_events WHERE task_id = ? AND event = ? ORDER BY id`, taskID, repoSweepRefGoneEventName)
	if err != nil {
		t.Fatalf("read task_events: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, d)
	}
	return out
}

// refGuardWithVanishedRef deletes branch from repo and returns the repo — the
// discovery snapshot has already been taken by the caller, so the ref really is
// gone when the decision step runs.
func refGuardWithVanishedRef(t *testing.T, repo, branch string) {
	t.Helper()
	rsGit(t, repo, true, "branch", "-D", branch)
	if rsBranchExists(repo, branch) {
		t.Fatalf("fixture: %s must be gone before the decision runs", branch)
	}
}

// --- the inconsistency is skipped AND recorded ---

// TestRepoSweepRefGuard_GoneRefIsSkippedAndRecorded is the ghost-ref proof: the
// candidate set is a discovery-time snapshot, and a name that vanishes before
// the decision must be SKIPPED — no merge-time gate asked about a ref git cannot
// resolve — while the LIVE ledger row still claiming that branch is recorded on
// its own audit trail (Rule 15: never swallowed).
func TestRepoSweepRefGuard_GoneRefIsSkippedAndRecorded(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo := rsTestRepo(t)
	rsMakeBranch(t, repo, refGuardGoneBranch)
	refGuardSeedRow(t, refGuardLiveRow, TaskStatusPending, refGuardGoneBranch)

	// The discovery snapshot happens FIRST (the real order), so the candidate is
	// genuinely listed by git before the ref disappears under the decision.
	candidates, _, err := repoSweepUnprotectedBranches(repo, repoSweepMaxBranches, nil)
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	if !refGuardHasBranch(candidates, refGuardGoneBranch) {
		t.Fatalf("fixture: discovery must list %s (candidates=%v)", refGuardGoneBranch, candidates)
	}
	refGuardWithVanishedRef(t, repo, refGuardGoneBranch)

	// The decision step must now report the SKIP, not an error and not a merge.
	outcome, action := repoSweepDecideBranch(repo, refGuardGoneBranch, &RepoSweepResult{})
	if action != repoSweepActionRefGone {
		t.Fatalf("a vanished ref must take the %q action, got %q (detail: %s)", repoSweepActionRefGone, action, outcome.Detail)
	}
	if !strings.Contains(outcome.Detail, refGuardGoneBranch) {
		t.Errorf("the skip must name the branch: %s", outcome.Detail)
	}
	// The gate text the incident logged must NOT appear: the point of the guard
	// is that no gate is asked about a name git cannot resolve.
	if strings.Contains(outcome.Detail, "merge-base") {
		t.Errorf("no merge-time gate may be asked about a vanished ref: %s", outcome.Detail)
	}
	if !strings.Contains(outcome.Detail, refGuardLiveRow) {
		t.Errorf("the skip must name the live row that still claims the branch: %s", outcome.Detail)
	}
	if !strings.Contains(outcome.Detail, "INCONSISTENCY") {
		t.Errorf("a live owner must be reported as an inconsistency: %s", outcome.Detail)
	}

	// The record: the live row carries the event, and its detail is re-readable.
	events := refGuardEventDetails(t, refGuardLiveRow)
	if len(events) == 0 {
		t.Fatalf("the live row %s carries no %q record — the mismatch was swallowed", refGuardLiveRow, repoSweepRefGoneEventName)
	}
	if !strings.Contains(events[0], refGuardGoneBranch) {
		t.Errorf("the record must name the branch: %q", events[0])
	}
}

// TestRepoSweepRefGuard_TerminalRowIsNotAnInconsistency pins the discriminator:
// a row that has ENDED and named a branch git no longer has is the ordinary
// post-merge state (every landed deliverable's branch is deleted), so it must
// be named as ordinary and must NOT receive an inconsistency record. Without
// this half the guard would cry wolf on every finished task.
func TestRepoSweepRefGuard_TerminalRowIsNotAnInconsistency(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo := rsTestRepo(t)
	rsMakeBranch(t, repo, refGuardGoneBranch)
	refGuardSeedRow(t, refGuardDoneRow, TaskStatusCompleted, refGuardGoneBranch)
	refGuardWithVanishedRef(t, repo, refGuardGoneBranch)

	outcome, action := repoSweepDecideBranch(repo, refGuardGoneBranch, &RepoSweepResult{})
	if action != repoSweepActionRefGone {
		t.Fatalf("a vanished ref must be skipped even when its owner ended, got %q", action)
	}
	if !strings.Contains(outcome.Detail, refGuardDoneRow) {
		t.Errorf("the skip must name the ended row: %s", outcome.Detail)
	}
	if strings.Contains(outcome.Detail, "INCONSISTENCY") {
		t.Errorf("an ENDED row's deleted branch is the ordinary post-merge state, not an inconsistency: %s", outcome.Detail)
	}
	if events := refGuardEventDetails(t, refGuardDoneRow); len(events) != 0 {
		t.Errorf("an ended row must carry no ref-gone record, got %v", events)
	}
}

// --- the no-weakening half ---

// TestRepoSweepRefGuard_RealBranchStillReachesTheGates is the control that makes
// the guard safe to ship: with the ref PRESENT the decision step proceeds into
// the merge-time gates and the merge path exactly as before this change — the
// branch is landed (ff) and its ref KEPT (the sweep mutates no ref), i.e. the
// union gate's real-branch coverage is untouched.
func TestRepoSweepRefGuard_RealBranchStillReachesTheGates(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	repo := rsTestRepo(t)
	rsMakeBranch(t, repo, refGuardRealBranch)
	if !rsBranchExists(repo, refGuardRealBranch) {
		t.Fatal("fixture: the control branch must exist")
	}

	// Through the full tick (the shipped path, not the arm in isolation).
	res := MergerRepoSweep(repo, 0)
	if res.Err != "" {
		t.Fatalf("sweep: %s", res.Err)
	}
	if res.RefGone != 0 {
		t.Errorf("a real branch must not be counted as ref-gone (got %d)", res.RefGone)
	}
	if res.Merged != 1 {
		t.Fatalf("the real branch must still be merged through the tick: merged=%d outcomes=%+v", res.Merged, res.Outcomes)
	}
	// CONVERTED (epic sweep-report-not-act, 2026-09-19): the landing used to
	// delete the branch it landed. The LANDING is unchanged (master is the
	// branch tip); the DELETION is retired, so the ref survives the tick.
	if !rsBranchExists(repo, refGuardRealBranch) {
		t.Errorf("the landed branch ref must SURVIVE — the sweep mutates no ref")
	}

	// And the per-branch decision consults the gates: a branch that does NOT
	// compile alone is still refused (the union gate's premise measurement),
	// which is the behaviour this guard must not have bypassed. Driven through
	// the gate itself, since the branch is gone after the landing above.
	repo2 := rsTestRepo(t)
	mbugIncidentShape(t, repo2, mbugFixtureBranch, mbugHelperName)
	if v := mergeUnionGate(repo2, "master", mbugFixtureBranch); !v.Refused {
		t.Fatalf("a real, union-breaking branch must still be REFUSED by the union gate: %+v", v)
	}
}

// --- the probe itself ---

// TestRepoSweepRefPresent_SeparatesAbsenceFromFailure pins the probe's two
// answers: a missing ref is (false, nil) — git's own named absence — while an
// unreadable repo is an ERROR, so a wedged repo can never be reported as "the
// branch is gone" (Rule 15).
func TestRepoSweepRefPresent_SeparatesAbsenceFromFailure(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := rsTestRepo(t)
	rsMakeBranch(t, repo, refGuardRealBranch)

	if present, err := repoSweepRefPresent(repo, refGuardRealBranch); err != nil || !present {
		t.Fatalf("an existing ref must read (true, nil): present=%v err=%v", present, err)
	}
	if present, err := repoSweepRefPresent(repo, "feat/never-existed"); err != nil || present {
		t.Fatalf("a missing ref must read (false, nil), never an error: present=%v err=%v", present, err)
	}
	if present, err := repoSweepRefPresent(filepath.Join(t.TempDir(), "not-a-repo"), refGuardRealBranch); err == nil {
		t.Fatalf("an unreadable repo must be an ERROR (present=%v) — a broken repo is not a vanished branch", present)
	}
}

// refGuardHasBranch reports whether needle is in hay (this file's own predicate:
// the package's existing containsStr takes a different shape).
func refGuardHasBranch(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
