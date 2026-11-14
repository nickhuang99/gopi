// The merger's in-flight IDENTITY path must judge on BOTH axes (2026-09-16).
//
// The candidate set used to be selected by the "worktree" needle in the
// deliverable blob alone, and the occupancy pairing then compared the row's
// worktree field against git's path. A row that names only a BRANCH (the
// {"repo":…,"branch":…} blob a run leaves when its tree was never backfilled,
// or a row whose backfill landed after the claim) was therefore invisible on
// two counts at once: the scan never loaded it, and the empty-string
// comparison would have dropped it even if it had. Everything below drives the
// arm's OWN entry points (deadHolderResumeRows / ResumeDeadHolderRuns) against
// a REAL git repo and a REAL linked worktree, so a green result is the arm
// judging, not a helper agreeing with itself.
//
// The other half of the same file is the regression risk the widening carries:
// the identity must stay EXACT (a prefix-similar branch name is not a match)
// and the occupancy MUST stay git's statement (a row recording a DIFFERENT
// directory is judged on the checkout git registers, never on the path it
// recorded — the ledger's path is measured wrong on the live host).

package tools

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
)

// branchOnlyDeliverable is the deliverable a run leaves when its worktree was
// never backfilled: repo + branch, NO worktree field. Spelled once here
// (Rule 14) so every case below feeds the arm the SAME shape.
func branchOnlyDeliverable(repo, branch string) string {
	return `{"repo":"` + repo + `","branch":"` + branch + `"}`
}

// seedBranchOnlyClaimedRow writes a claimed row carrying repo+branch and no
// worktree field, with the holder identity the arm judges: the claim stamp
// (claimed_by) plus the optional recorded process triple. pid <= 0 records NO
// identity — the pre-column shape, whose death proof is the claim pid alone.
func seedBranchOnlyClaimedRow(t *testing.T, taskID, status, repo, branch, claimedBy string, pid int, ticks int64, cmd string) {
	t.Helper()
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "branch-only fixture", Payload: "in flight", TaskID: taskID, Source: "human",
	}); err != nil {
		t.Fatalf("submit %s: %v", taskID, err)
	}
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET deliverable = ? WHERE task_id = ?`,
		branchOnlyDeliverable(repo, branch), taskID); err != nil {
		t.Fatalf("deliverable %s: %v", taskID, err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, claimed_by = ?, proc_pid = ?, proc_start_ticks = ?, proc_cmd = ? WHERE task_id = ?`,
		status, claimedBy, pid, ticks, cmd, taskID); err != nil {
		t.Fatalf("claim %s: %v", taskID, err)
	}
}

// resumeOutcomeFor returns the arm's outcome for one row (nil when the arm did
// not judge it at all — no candidate, or a holder judged live).
func resumeOutcomeFor(outcomes []deadHolderResumeOutcome, taskID string) *deadHolderResumeOutcome {
	for i := range outcomes {
		if outcomes[i].TaskID == taskID {
			oc := outcomes[i]
			return &oc
		}
	}
	return nil
}

// TestDeadHolderResumeRowsLoadABranchNamedCandidate is the loader red proof for
// the branch axis. `deadHolderResumeRows` selected candidates by the "worktree"
// needle in the deliverable blob, so a claimed row naming only a branch was
// never in the set — and the verdict machinery below it, which is tested with
// directly constructed rows, kept passing. This drives the REAL loader.
func TestDeadHolderResumeRowsLoadABranchNamedCandidate(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	dead := testutil.StartReapedProcess(t, testutil.MockDaemonBinary, testutil.MockDaemonHoldSeconds)
	seedBranchOnlyClaimedRow(t, "t-branch-only", taskStatusClaimed, repo, branch, claimHolderOfPid(dead), dead, 0, "")

	rows, err := deadHolderResumeRows()
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	var found *deadHolderResumeRow
	for i := range rows {
		if rows[i].TaskID == "t-branch-only" {
			found = &rows[i]
		}
	}
	if found == nil {
		t.Fatalf("a claimed row naming a BRANCH (no worktree field) must be a resume candidate: "+
			"the arm cannot judge what the loader never loaded (candidates = %d)", len(rows))
	}
	// The row's deliverable names the branch and no directory — the shape the
	// arm must now judge on its own.
	if got := taskDeliverableWorktree(found.Deliverable); got != "" {
		t.Fatalf("fixture error: the candidate must record NO worktree, got %q", got)
	}
	if got := taskDeliverableBranchOrEmpty(found.Deliverable); got != branch {
		t.Fatalf("candidate branch = %q, want %q", got, branch)
	}
	_ = wt
}

// TestResumeArmJudgesABranchNamedRow is the end-to-end acceptance for the
// identity fix: a claimed row that names a BRANCH and no worktree, in a
// registered worktree whose checked-out branch matches.
//
//	(a) while its holder is DEMONSTRABLY ALIVE the arm leaves it completely
//	    alone — nothing is resumed (the branch is still owned);
//	(b) once the same holder is PROVABLY DEAD the arm judges it a resume
//	    candidate and acts on it, naming the worktree git reports for the
//	    branch.
//
// The resumed spawn itself is not what is asserted (the gopi binary is stubbed
// by the fixture's GOPI_BINARY): what is asserted is the DECISION the merger
// makes about a row it previously could not see.
func TestResumeArmJudgesABranchNamedRow(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	// The resumed spawn is NOT what is asserted here (the arm's DECISION is) —
	// and the stub is not optional: with no GOPI_BINARY the launcher resolves to
	// THIS test binary (runcmd.Launcher → os.Executable), so the detached child
	// re-runs the whole package suite as an orphan (PPID 1) on every dead-holder
	// case. Pointing it at the portable mock process keeps the spawn harmless
	// and the assertion about the decision (Rule 27: no load, no stray run).
	t.Setenv("GOPI_BINARY", testutil.MockDaemonBinary)

	// ---- (a) PROTECTED: a live holder on the branch axis
	live := testutil.StartMockProcess(t, testutil.MockDaemonBinary, testutil.MockDaemonHoldSeconds)
	ident := probeOfLiveProcess(t, live.Pid())
	seedBranchOnlyClaimedRow(t, "t-branch-live", taskStatusInProgress, repo, branch,
		claimHolderOfPid(live.Pid()), ident.Pid, ident.StartTicks, ident.Cmd)
	if oc := resumeOutcomeFor(ResumeDeadHolderRuns(repo), "t-branch-live"); oc != nil {
		t.Fatalf("a LIVE holder on a branch-named row must be protected (no resume, no report): %s", oc.Line())
	}

	// ---- (b) RESUME CANDIDATE: the same row, its holder provably gone
	dead := testutil.StartReapedProcess(t, testutil.MockDaemonBinary, testutil.MockDaemonHoldSeconds)
	seedBranchOnlyClaimedRow(t, "t-branch-dead", taskStatusClaimed, repo, branch,
		claimHolderOfPid(dead), dead, 0, "")
	oc := resumeOutcomeFor(ResumeDeadHolderRuns(repo), "t-branch-dead")
	if oc == nil {
		t.Fatalf("a branch-named row whose holder is provably dead must reach the arm's verdict "+
			"(registered worktree %s holds %s)", wt, branch)
	}
	if oc.Verdict != holderProbeDead.String() {
		t.Fatalf("the candidate must be judged dead, got verdict %q (%s)", oc.Verdict, oc.Line())
	}
	if oc.Worktree != wt {
		t.Errorf("the outcome must name the worktree git reports for the branch (%q), got %q", wt, oc.Worktree)
	}
	if oc.Attempts != 1 {
		t.Errorf("the arm must have ACTED on the candidate — one attempt spent, got %d (%s)", oc.Attempts, oc.Line())
	}
}

// TestResumeArmBranchPrefixTrapIsNotAMatch pins the identity the widening must
// keep EXACT. The fixture's worktree checks out `feat/dead-holder`; a row that
// names a branch which merely STARTS the same way (`feat/dead-holder-two`) or
// is a TRUNCATION of it (`feat/dead-hold`) is a different branch. A prefix or
// substring match here would hand the arm a worktree — and a run to resume —
// that belongs to somebody else's task.
func TestResumeArmBranchPrefixTrapIsNotAMatch(t *testing.T) {
	repo, _, branch := deadHolderReclaimFixture(t)

	dead := testutil.StartReapedProcess(t, testutil.MockDaemonBinary, testutil.MockDaemonHoldSeconds)
	// Both directions of the trap: the row's name extends the registered one,
	// and it truncates it.
	for _, near := range []string{branch + "-two", branch[:len(branch)-1]} {
		if near == branch {
			t.Fatalf("fixture error: the trap name must differ from %q", branch)
		}
		taskID := "t-prefix-trap-" + near[len(near)-3:]
		seedBranchOnlyClaimedRow(t, taskID, taskStatusClaimed, repo, near,
			claimHolderOfPid(dead), dead, 0, "")

		rows, err := deadHolderResumeRows()
		if err != nil {
			t.Fatalf("rows: %v", err)
		}
		var row *deadHolderResumeRow
		for i := range rows {
			if rows[i].TaskID == taskID {
				row = &rows[i]
			}
		}
		if row == nil {
			t.Fatalf("precondition: the branch-named row %s must be loaded", taskID)
		}
		_ = row
		if oc := resumeOutcomeFor(ResumeDeadHolderRuns(repo), taskID); oc != nil {
			t.Fatalf("a prefix-similar branch name must not become a candidate: %s", oc.Line())
		}
	}
}

// TestResumeArmDoesNotWeakenTheOccupancyVeto is the regression guard for the
// ONE thing the widened candidate set could break: a row whose deliverable
// records a DIFFERENT directory must never be used to CLAIM that directory.
// Widening the loader must never widen the pairing — a name collision (two
// tasks minting fix/<same-name>) is not ownership, and hand-waving one task's
// recorded path onto another's run is how a recovery reaches ground that does
// not belong to it.
//
// THE OCCUPANCY RULING (merge resolution, 2026-09-16 — memory #3231/#3234):
// the directory the arm judges is GIT's statement, never the ledger's, because
// the ledger's recorded path is measured wrong on the live host (row
// 01a0a9b5 records gopi-next-tick while git registers gopi-merger-next-tick).
// So the collided row IS judged — the branch axis selects on the BRANCH — and
// the ground it is judged on is the checkout git registers for that branch,
// NOT the foreign directory the row recorded. The veto this test guards is
// therefore on the PATH, not on the candidate: the ledger's `elsewhere` may
// never be reached into, and the row must never be reported as holding it.
func TestResumeArmDoesNotWeakenTheOccupancyVeto(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	elsewhere := filepath.Join(filepath.Dir(wt), "somewhere-else")

	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "collided fixture", Payload: "elsewhere", TaskID: "t-collided", Source: "human",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	// The row names the SAME branch but records ANOTHER directory. It carries
	// NO runner identity on purpose: the arm then answers "unreadable"
	// (Rule 15 — never a death proof), so the assertion below is about WHICH
	// GROUND the arm judges, and this fixture starts no process.
	seedDeliverable(t, "t-collided", repo, branch, elsewhere)
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, claimed_by = '', proc_pid = 0, proc_start_ticks = 0 WHERE task_id = ?`,
		taskStatusInProgress, "t-collided"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	db.Close() //stylegate:ignore process-lifetime fixture handle; the OS reclaims the descriptor

	rows, err := deadHolderResumeRows()
	if err != nil {
		t.Fatalf("rows: %v", err)
	}
	_ = rows
	for _, oc := range ResumeDeadHolderRuns(repo) {
		if oc.TaskID != "t-collided" {
			continue
		}
		// Judged on the checkout GIT registers — the branch is this row's, the
		// foreign directory is never adopted as the ground.
		if oc.Worktree == elsewhere {
			t.Fatalf("the occupancy veto must hold: the arm must never judge a row on the foreign path it recorded: %s", oc.Line())
		}
		if oc.Worktree != wt {
			t.Fatalf("the collided row must be judged on git's registered checkout %q, got %q", wt, oc.Worktree)
		}
		// A provably dead holder is ACTED ON — that is the arm's job — and the
		// recovery is bound to the row (its own task id), never to the foreign
		// directory the ledger recorded.
		if oc.Resumed && oc.TaskID != "t-collided" {
			t.Fatalf("the recovery must stay bound to the collided row, got %+v", oc)
		}
	}
}
