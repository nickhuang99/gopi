package tools

// Regression guards for the second arm of the 2026-09-21/22 incident: the
// finish-cleanup chain removed a branch's worktree while rows were still open
// on it. The removal's standing was a statement about a MERGE ("the delivery
// chain's local sync landed"); these tests pin the completion fact it now owes.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

// TestFinishCleanupRemovalRefusesAnOpenRowOnTheBranchAxis pins the OUTCOME the
// 22:52:20 removal needed and did not get: an OPEN row naming the candidate's
// BRANCH (not its path) refuses the removal, and the refusal names that row.
//
// It asserts the outcome rather than a marker because this shape is refused by
// TWO independent gates on purpose, and which one speaks first is an
// implementation detail a test must not freeze: the ownership gate refuses a
// live holder on the branch axis, and the completion fact refuses an open row
// on it whatever the holder's state. The second is the one whose absence let
// the removal stand (the incident's rows had live holders AND the removal still
// happened, because the arm's standing was a merge statement rather than a
// liveness or completion question at all) — the DEAD-holder case below is where
// the completion rung is the only thing that can speak, and it carries the
// marker assertion.
func TestFinishCleanupRemovalRefusesAnOpenRowOnTheBranchAxis(t *testing.T) {
	repo, wt, branch := finishCleanupCompletionFixture(t, "t-regress-open-row",
		[]string{"t-regress-open-row-live"}, true, false)

	// The row is stamped with the branch ONLY (the fixture writes no worktree).
	reason := finishCleanupRemovalStateReason(t.Context(), &FinishCleanupReport{TaskID: "t-regress-other"}, wt)
	if reason == "" {
		t.Fatalf("an OPEN row naming branch %s must refuse the removal (repo %s)", branch, repo)
	}
	if !strings.Contains(reason, "t-regress-open-row-live") {
		t.Errorf("the refusal must name the OPEN row, got %q", reason)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("the refusal must leave the directory alone: %v", err)
	}
	if !worktreeListed(t, repo, wt) {
		t.Errorf("the refusal must leave the registration alone: %s", worktreeList(t, repo))
	}
}

// TestFinishCleanupRemovalIsUnblockedByAnOpenRowOnAnUnrelatedBranch is the
// guard against a wall: the branch axis must match the CANDIDATE's branch, not
// merely find some open row in the ledger.
func TestFinishCleanupRemovalIsUnblockedByAnOpenRowOnAnUnrelatedBranch(t *testing.T) {
	repo, wt, branch := finishCleanupCompletionFixture(t, "t-regress-elsewhere", []string{"t-regress-elsewhere"}, true, false)
	// Re-point the open row at an unrelated branch: nothing then names this
	// ground, and the completion fact is satisfiable, so the removal must stand.
	db := openTestTasksDB(t, memoryDBPath())
	if _, err := db.ExecContext(t.Context(),
		`UPDATE tasks SET deliverable = ? WHERE task_id = ?`,
		`{"repo":"`+repo+`","branch":"feat/somebody-else"}`, "t-regress-elsewhere"); err != nil {
		db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
		t.Fatalf("re-point the open row: %v", err)
	}
	db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor

	if reason := finishCleanupWorkCompletionReason(t.Context(), repo, wt, branch, "t-regress-other", IntegrationBranchName); reason != "" {
		t.Fatalf("an open row on an UNRELATED branch must not refuse this ground, got %q", reason)
	}
}

// TestFinishCleanupRemovalProceedsWhenNoRowNamesTheGround is the NEGATIVE half:
// the completion fact must be satisfiable, or the guard is a wall. Same
// fixture with no open row on the branch.
func TestFinishCleanupRemovalProceedsWhenNoRowNamesTheGround(t *testing.T) {
	repo, wt, branch := finishCleanupCompletionFixture(t, "t-regress-landed", nil, true, false)

	rep := &FinishCleanupReport{TaskID: "t-regress-landed"}
	if reason := finishCleanupWorkCompletionReason(t.Context(), repo, wt, branch, rep.TaskID, IntegrationBranchName); reason != "" {
		t.Fatalf("a branch no open row names must not refuse, got %q", reason)
	}
}

// TestFinishCleanupRemovalProceedsForAProvablyDeadLocalHolderOrphan pins the
// UNION of the two fixes, and it is the honest form of a case this branch first
// got WRONG (measured 2026-09-22: the first version of this test asserted the
// refusal and reddened TestDeadLocalHolderReleasesTheDirectory).
//
// The orphan epic (2026-09-12, landed on master) settled that a live-STATUS row
// whose LOCAL holder is PROVABLY DEAD is an ORPHAN: its recorded directory is
// not a live session's ground, and the whole point of that release is to free a
// ground a KILLED run left registered. "An open row is not finished work" is the
// argument for a row whose run MIGHT STILL BE THERE; applied to a row whose
// process is measured GONE it would strand the directory forever, on a fact the
// orphan epic already read the other way.
//
// So the completion fact asks about the row that is open AND STILL HELD
// (HolderLive) — which is the incident's shape — and lets the orphan through to
// the arm that owns its release. The residual is recorded in the report's
// `## Not Done`: the incident's two rows had DEAD holders and cross-host
// (`peerbox:…`) stamps this host cannot measure, so they are not this host's
// orphans; deciding that shape belongs to a policy task, not to this fix.
func TestFinishCleanupRemovalProceedsForAProvablyDeadLocalHolderOrphan(t *testing.T) {
	repo, wt, branch := finishCleanupCompletionFixture(t, "t-regress-dead-holder", []string{"t-regress-dead-holder"}, true, true)

	claims, err := finishCleanupLiveClaims(t.Context())
	if err != nil {
		t.Fatalf("load live claims: %v", err)
	}
	o := claims.anyLiveRowOf("", wt, branch)
	if o == nil {
		t.Fatalf("fixture: the open row must be FOUND by the completion rung (it must never be silent)")
	}
	if o.HolderLive {
		t.Fatalf("fixture: the row's holder must read as dead for this case")
	}
	if got := claims.deadLocalHolderExcluding("", wt, branch); got == nil || got.TaskID != o.TaskID {
		t.Fatalf("fixture: the row must be the dead-local-holder ORPHAN the report names, got %+v", got)
	}
	rep := &FinishCleanupReport{TaskID: "t-regress-dead-holder-other"}
	if reason := finishCleanupWorkCompletionReason(t.Context(), repo, wt, branch, rep.TaskID, IntegrationBranchName); reason != "" {
		t.Fatalf("a provably DEAD local holder is the orphan the release owns — it must not refuse, got %q", reason)
	}
}

// TestFinishCleanupRemovalRefusesAnOpenRowWhoseHolderIsAlive is the half that
// matters and that this fix is FOR: the incident's shape is a row that is open
// AND still held. The holder pid is this test process's own host stamp, so
// "alive" is measured, not asserted.
func TestFinishCleanupRemovalRefusesAnOpenRowWhoseHolderIsAlive(t *testing.T) {
	repo, wt, branch := finishCleanupCompletionFixture(t, "t-regress-live-holder", []string{"t-regress-live-holder"}, true, false)

	claims, err := finishCleanupLiveClaims(t.Context())
	if err != nil {
		t.Fatalf("load live claims: %v", err)
	}
	o := claims.anyLiveRowOf("", wt, branch)
	if o == nil || !o.HolderLive {
		t.Fatalf("fixture: the row must be found AND read as a live holder, got %+v", o)
	}
	rep := &FinishCleanupReport{TaskID: "t-regress-live-holder-other"}
	reason := finishCleanupWorkCompletionReason(t.Context(), repo, wt, branch, rep.TaskID, IntegrationBranchName)
	if reason == "" {
		t.Fatalf("an OPEN row with a LIVE holder is unfinished work — the removal must refuse")
	}
	if !strings.Contains(reason, "t-regress-live-holder") {
		t.Errorf("the refusal must name the open row, got %q", reason)
	}
}

// finishCleanupCompletionFixture builds a repo with an integration branch, a
// feature branch (landed or not) and a linked worktree, plus ONE live row
// naming the branch (when taskID is non-empty). holderDead stamps the row's
// claim at a pid this host cannot find, so "the holder is provably dead" is a
// measured property of the fixture rather than a claim.
func finishCleanupCompletionFixture(t *testing.T, taskID string, liveIDs []string, landed, holderDead bool) (repo, wt, branch string) {
	t.Helper()
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"sync_peers":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.SettingsEnv, settings)
	t.Setenv(config.SyncPeersEnv, "")

	root := t.TempDir()
	repo = filepath.Join(root, "repo")
	wt = filepath.Join(root, "wt")
	branch = "feat/regress-completion"
	gitRun(t, root, "init", repo)
	gitRun(t, repo, "config", "user.email", "t@t")
	gitRun(t, repo, "config", "user.name", "t")
	gitRun(t, repo, "checkout", "-b", IntegrationBranchName)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "a.txt")
	gitRun(t, repo, "commit", "-m", "base")
	gitRun(t, repo, "checkout", "-b", branch)
	if err := os.WriteFile(filepath.Join(repo, "deliverable.txt"), []byte("the delivery"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "deliverable.txt")
	gitRun(t, repo, "commit", "-m", "the deliverable")
	gitRun(t, repo, "checkout", IntegrationBranchName)
	if landed {
		// The completion fact: every commit of branch is reachable from the
		// integration branch (a real fast-forward, not a hopeful assertion).
		gitRun(t, repo, "merge", "--ff-only", branch)
	}
	gitRun(t, repo, finishCleanupWorktreeVerb, "add", wt, branch)

	for _, id := range liveIDs {
		if _, err := TaskSubmit(TaskSubmitOptions{Summary: "live owner", Payload: "p", TaskID: id, Source: "human"}); err != nil {
			t.Fatalf("submit %s: %v", id, err)
		}
		holder := reclaimSelfStamp()
		if holder == "" {
			t.Skip("no hostname on this machine — the live-row shape is untestable here")
		}
		if holderDead {
			holder = claimStampForTest(freePIDForTest(t))
		}
		db := openTestTasksDB(t, memoryDBPath())
		_, err := db.ExecContext(t.Context(),
			`UPDATE tasks SET status = ?, claimed_by = ?, claimed_at = ?, `+tasksInProgressAtColumnName+
				` = ?, updated_at = ?, deliverable = ? WHERE task_id = ?`,
			taskStatusInProgress, holder, 1000.0, 2000.0, 2000.0,
			`{"repo":"`+repo+`","branch":"`+branch+`"}`, id)
		db.Close() //stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
		if err != nil {
			t.Fatalf("stamp %s: %v", id, err)
		}
	}
	return repo, wt, branch
}
